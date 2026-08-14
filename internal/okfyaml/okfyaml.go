// Package okfyaml patches an OKF document's YAML frontmatter in place. A patch
// rewrites only the bytes of the one value it targets, so key order, inline
// comments, blank lines, quoting and flow/block styles all survive verbatim —
// re-encoding a mutated yaml.Node tree does not (it collapses comment padding
// and drops blank lines), and an editor must never reformat a hand-authored
// document as a side effect of changing one field.
package okfyaml

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/adrg/frontmatter"
	"gopkg.in/yaml.v3"
)

// ErrUnknownKey reports a patch aimed at a key the frontmatter does not define.
var ErrUnknownKey = errors.New("okfyaml: unknown frontmatter key")

// Field is one top-level frontmatter key as an editor displays it. Value is the
// scalar text, or the ", "-joined items of a sequence; it is empty for a nested
// mapping (a shape v1 does not edit).
type Field struct {
	Key    string
	Value  string
	IsList bool
}

// Doc is a parsed OKF document: the raw frontmatter YAML, the node tree that
// locates each value within it, and the body verbatim.
type Doc struct {
	inner   []byte
	lines   []int
	root    *yaml.Node
	body    []byte
	closing []byte
}

// Parse splits content into frontmatter and body and decodes the frontmatter
// into a node tree. ok is false when content is not in the canonical
// "---\n...\n---\n" form this codebase's writers produce, when the frontmatter
// is not a plain mapping, or when it uses anchors, aliases or a multi-document
// stream: a caller that gets ok=false must fall back to read-only rather than
// risk a corrupting rewrite. err is returned only for frontmatter that is
// genuinely malformed YAML.
func Parse(content []byte) (Doc, bool, error) {
	if !bytes.HasPrefix(content, []byte("---\n")) {
		return Doc{}, false, nil
	}

	// The decoded value is discarded: frontmatter.Parse is used only to find the
	// exact byte where the body starts, and `any` accepts shapes decodeMapping
	// then declines deliberately (a sequence root) rather than erroring here.
	var matter any
	rest, err := frontmatter.Parse(bytes.NewReader(content), &matter)
	if err != nil {
		return Doc{}, false, fmt.Errorf("okfyaml: split frontmatter: %w", err)
	}

	inner, closing, ok := splitDelimiters(content[:len(content)-len(rest)])
	if !ok {
		return Doc{}, false, nil
	}

	root, ok, err := decodeMapping(inner)
	if err != nil || !ok {
		return Doc{}, false, err
	}

	return Doc{inner: inner, lines: lineOffsets(inner), root: root, body: rest, closing: closing}, true, nil
}

// Body returns the document body, verbatim and excluding the frontmatter block.
func (d Doc) Body() []byte { return d.body }

// Fields returns the top-level frontmatter keys in file order.
func (d Doc) Fields() []Field {
	fields := make([]Field, 0, len(d.root.Content)/2)
	for i := 0; i+1 < len(d.root.Content); i += 2 {
		k, v := d.root.Content[i], d.root.Content[i+1]
		f := Field{Key: k.Value, IsList: v.Kind == yaml.SequenceNode}
		switch v.Kind {
		case yaml.ScalarNode:
			f.Value = v.Value
		case yaml.SequenceNode:
			f.Value = strings.Join(seqValues(v), ", ")
		default:
		}
		fields = append(fields, f)
	}

	return fields
}

// ReplaceBody returns the document with body swapped in and the frontmatter
// block carried over byte for byte.
func (d Doc) ReplaceBody(body []byte) []byte {
	return d.assemble(d.inner, body)
}

// PatchScalar returns the document with key's scalar value replaced by value.
// It errors when key is absent, when its value is not a single-line scalar, or
// when the located span does not re-parse to the value it replaced.
func (d Doc) PatchScalar(key, value string) ([]byte, error) {
	node, err := d.find(key)
	if err != nil {
		return nil, err
	}
	if node.Kind != yaml.ScalarNode {
		return nil, fmt.Errorf("okfyaml: %q is not a scalar field", key)
	}
	start, end, err := d.span(node)
	if err != nil {
		return nil, err
	}
	rendered, err := renderScalar(node.Style, value)
	if err != nil {
		return nil, err
	}

	return d.applySpan(node, start, end, rendered)
}

// PatchTags returns the document with key's sequence replaced by items. v1
// retypes the whole list rather than editing individual entries. A key whose
// current value is null is rewritten as a flow sequence; an empty items on a
// block sequence clears the entries, leaving the key null.
func (d Doc) PatchTags(key string, items []string) ([]byte, error) {
	node, err := d.find(key)
	if err != nil {
		return nil, err
	}

	isNull := node.Kind == yaml.ScalarNode && node.Tag == "!!null"
	switch {
	case node.Kind != yaml.SequenceNode && !isNull:
		return nil, fmt.Errorf("okfyaml: %q is not a list field", key)
	case isNull || node.Style&yaml.FlowStyle != 0:
		start, end, serr := d.span(node)
		if serr != nil {
			return nil, serr
		}
		rendered, rerr := renderFlowSeq(items)
		if rerr != nil {
			return nil, rerr
		}

		return d.applySpan(node, start, end, rendered)
	default:
		return d.patchBlockSeq(node, items)
	}
}

// find returns the value node for a top-level key.
func (d Doc) find(key string) (*yaml.Node, error) {
	for i := 0; i+1 < len(d.root.Content); i += 2 {
		if d.root.Content[i].Value == key {
			return d.root.Content[i+1], nil
		}
	}

	return nil, fmt.Errorf("%w: %q", ErrUnknownKey, key)
}

// applySpan replaces a located source span with rendered text. The span is
// verified to re-parse to the node it replaces, so a span this package located
// wrongly refuses the patch instead of corrupting the file.
func (d Doc) applySpan(node *yaml.Node, start, end int, rendered string) ([]byte, error) {
	if !spanMatches(d.inner[start:end], node) {
		return nil, fmt.Errorf("okfyaml: cannot locate the value of a %s node at line %d", node.Tag, node.Line)
	}
	// A null value leaves an empty span that may sit flush against the colon;
	// without a separator the patched line would read "key:value".
	if start == end && (start == 0 || !isSpace(d.inner[start-1])) {
		rendered = " " + rendered
	}

	return d.assemble(replaceSpan(d.inner, start, end, rendered), d.body), nil
}

// patchBlockSeq rewrites the item lines of a block sequence, preserving the
// key line (and any comment on it) and the sequence's indentation.
func (d Doc) patchBlockSeq(node *yaml.Node, items []string) ([]byte, error) {
	start, end, err := d.blockSeqSpan(node)
	if err != nil {
		return nil, err
	}
	if !spanMatches(d.inner[start:end], node) {
		return nil, fmt.Errorf("okfyaml: cannot locate the list at line %d", node.Line)
	}

	indent := strings.Repeat(" ", node.Column-1)
	var b strings.Builder
	for _, item := range items {
		rendered, rerr := renderScalar(0, item)
		if rerr != nil {
			return nil, rerr
		}
		_, _ = fmt.Fprintf(&b, "%s- %s\n", indent, rendered)
	}

	return d.assemble(replaceSpan(d.inner, start, end, b.String()), d.body), nil
}

// span locates the source bytes of a single-line node within the frontmatter.
func (d Doc) span(node *yaml.Node) (start, end int, err error) {
	lineStart, lineEnd, err := d.lineBounds(node.Line)
	if err != nil {
		return 0, 0, err
	}
	start = lineStart + node.Column - 1
	if start < lineStart || start > lineEnd {
		return 0, 0, fmt.Errorf("okfyaml: node at line %d column %d is outside its line", node.Line, node.Column)
	}
	end, err = valueEnd(d.inner[start:lineEnd], node)
	if err != nil {
		return 0, 0, err
	}

	return start, start + end, nil
}

// blockSeqSpan returns the whole-line span covering a block sequence's items.
func (d Doc) blockSeqSpan(node *yaml.Node) (start, end int, err error) {
	if len(node.Content) == 0 {
		return 0, 0, errors.New("okfyaml: cannot locate an empty block list")
	}
	first, last := node.Content[0], node.Content[len(node.Content)-1]
	for _, item := range node.Content {
		if item.Kind != yaml.ScalarNode {
			return 0, 0, fmt.Errorf("okfyaml: list at line %d has a non-scalar item", node.Line)
		}
	}
	start, _, err = d.lineBounds(first.Line)
	if err != nil {
		return 0, 0, err
	}
	_, lastEnd, err := d.lineBounds(last.Line)
	if err != nil {
		return 0, 0, err
	}
	if lastEnd < len(d.inner) {
		lastEnd++ // include the item's newline; the replacement supplies its own
	}

	return start, lastEnd, nil
}

// lineBounds returns the byte offsets of a 1-based line's first byte and of its
// newline (or of end-of-input for an unterminated last line).
func (d Doc) lineBounds(line int) (start, end int, err error) {
	if line < 1 || line > len(d.lines) {
		return 0, 0, fmt.Errorf("okfyaml: line %d is outside the frontmatter", line)
	}
	start = d.lines[line-1]
	end = len(d.inner)
	if idx := bytes.IndexByte(d.inner[start:], '\n'); idx >= 0 {
		end = start + idx
	}

	return start, end, nil
}

// assemble rebuilds the file from a frontmatter body and a document body,
// reusing the original delimiters.
func (d Doc) assemble(inner, body []byte) []byte {
	out := make([]byte, 0, 4+len(inner)+len(d.closing)+len(body))
	out = append(out, "---\n"...)
	out = append(out, inner...)
	out = append(out, d.closing...)

	return append(out, body...)
}

// splitDelimiters strips the opening and closing "---" lines from a frontmatter
// block, returning the YAML between them and the exact closing bytes. ok is
// false for any other delimiter shape.
func splitDelimiters(block []byte) (inner, closing []byte, ok bool) {
	rest := block[len("---\n"):]
	switch {
	case bytes.HasSuffix(rest, []byte("---\n")):
		closing = []byte("---\n")
	case bytes.Equal(rest, []byte("---")), bytes.HasSuffix(rest, []byte("\n---")):
		closing = []byte("---")
	default:
		return nil, nil, false
	}

	return rest[:len(rest)-len(closing)], closing, true
}

// decodeMapping decodes frontmatter YAML into its mapping node. ok is false for
// a multi-document stream, a non-mapping root, or any use of anchors/aliases —
// shapes whose source spans this package does not track.
func decodeMapping(inner []byte) (*yaml.Node, bool, error) {
	dec := yaml.NewDecoder(bytes.NewReader(inner))
	var doc yaml.Node
	if err := dec.Decode(&doc); err != nil {
		return nil, false, fmt.Errorf("okfyaml: parse frontmatter: %w", err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); err == nil {
		return nil, false, nil
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, false, nil
	}
	if hasAnchors(doc.Content[0]) {
		return nil, false, nil
	}

	return doc.Content[0], true, nil
}

// hasAnchors reports whether any node in the tree is an alias or carries an
// anchor.
func hasAnchors(node *yaml.Node) bool {
	if node.Kind == yaml.AliasNode || node.Anchor != "" {
		return true
	}

	return slices.ContainsFunc(node.Content, hasAnchors)
}

// valueEnd returns the offset within line (which starts at the node's first
// byte and ends at the newline) just past the node's own text, leaving any
// padding and trailing comment to the caller.
func valueEnd(line []byte, node *yaml.Node) (int, error) {
	switch {
	case node.Style&yaml.DoubleQuotedStyle != 0:
		return quotedEnd(line, '"', true)
	case node.Style&yaml.SingleQuotedStyle != 0:
		return quotedEnd(line, '\'', false)
	case node.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0:
		return 0, fmt.Errorf("okfyaml: multi-line block scalar at line %d is not editable", node.Line)
	case node.Style&yaml.FlowStyle != 0:
		return flowEnd(line)
	}

	return len(bytes.TrimRight(line[:commentStart(line)], " \t")), nil
}

// commentStart returns the offset of a trailing "#" comment within line, or
// len(line) when there is none. A "#" only opens a comment when it starts the
// text or follows whitespace, which is exactly where a plain scalar ends.
func commentStart(line []byte) int {
	for i, c := range line {
		if c == '#' && (i == 0 || isSpace(line[i-1])) {
			return i
		}
	}

	return len(line)
}

// quotedEnd returns the offset just past a quoted scalar opening at line[0].
// Double quotes escape with a backslash; single quotes escape by doubling.
func quotedEnd(line []byte, quote byte, backslash bool) (int, error) {
	if len(line) == 0 || line[0] != quote {
		return 0, errors.New("okfyaml: quoted value does not start with its quote")
	}
	for i := 1; i < len(line); i++ {
		switch {
		case backslash && line[i] == '\\':
			i++
		case line[i] != quote:
		case !backslash && i+1 < len(line) && line[i+1] == quote:
			i++
		default:
			return i + 1, nil
		}
	}

	return 0, errors.New("okfyaml: quoted value is not closed on its line")
}

// flowEnd returns the offset just past a flow collection opening at line[0],
// tracking nesting and quoted items.
func flowEnd(line []byte) (int, error) {
	depth := 0
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '[', '{':
			depth++
		case ']', '}':
			depth--
			if depth == 0 {
				return i + 1, nil
			}
		case '"', '\'':
			n, err := quotedEnd(line[i:], line[i], line[i] == '"')
			if err != nil {
				return 0, err
			}
			i += n - 1
		default:
		}
	}

	return 0, errors.New("okfyaml: flow collection is not closed on its line")
}

// spanMatches reports whether src re-parses to the same value as node. It is
// the guard that makes a positional patch safe: a span this package located
// wrongly will not round-trip, and the patch is refused instead of applied.
func spanMatches(src []byte, node *yaml.Node) bool {
	if len(src) == 0 {
		return node.Kind == yaml.ScalarNode && node.Tag == "!!null"
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(src, &doc); err != nil {
		return false
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 {
		return false
	}
	got := doc.Content[0]
	if got.Kind != node.Kind {
		return false
	}
	if got.Kind == yaml.ScalarNode {
		return got.Value == node.Value
	}

	return slicesEqual(seqValues(got), seqValues(node))
}

// seqValues returns the scalar items of a sequence node, or nil when any item
// is not a scalar.
func seqValues(node *yaml.Node) []string {
	out := make([]string, 0, len(node.Content))
	for _, item := range node.Content {
		if item.Kind != yaml.ScalarNode {
			return nil
		}
		out = append(out, item.Value)
	}

	return out
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

// renderScalar renders value as a single-line YAML scalar in style, quoting it
// when YAML requires it.
func renderScalar(style yaml.Style, value string) (string, error) {
	out, err := yaml.Marshal(&yaml.Node{Kind: yaml.ScalarNode, Style: style, Value: value})
	if err != nil {
		return "", fmt.Errorf("okfyaml: render value: %w", err)
	}
	text := strings.TrimSuffix(string(out), "\n")
	if text == "" || strings.Contains(text, "\n") {
		return "", fmt.Errorf("okfyaml: %q cannot be written as a single-line value", value)
	}

	return text, nil
}

// renderFlowSeq renders items as a flow sequence ("[a, b]").
func renderFlowSeq(items []string) (string, error) {
	rendered := make([]string, 0, len(items))
	for _, item := range items {
		text, err := renderScalar(0, item)
		if err != nil {
			return "", err
		}
		rendered = append(rendered, text)
	}

	return "[" + strings.Join(rendered, ", ") + "]", nil
}

// lineOffsets returns the byte offset at which each line of b starts.
func lineOffsets(b []byte) []int {
	offsets := []int{0}
	for i, c := range b {
		if c == '\n' && i+1 < len(b) {
			offsets = append(offsets, i+1)
		}
	}

	return offsets
}

// replaceSpan returns src with [start,end) replaced by text.
func replaceSpan(src []byte, start, end int, text string) []byte {
	out := make([]byte, 0, len(src)-(end-start)+len(text))
	out = append(out, src[:start]...)
	out = append(out, text...)

	return append(out, src[end:]...)
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' }
