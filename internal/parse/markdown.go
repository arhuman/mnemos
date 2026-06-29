package parse

import (
	"cmp"
	"context"
	"path/filepath"
	"slices"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"

	"github.com/arhuman/mnemos/internal/model"
)

// md is the shared goldmark instance. It is configured for CommonMark defaults;
// frontmatter is stripped before parsing so goldmark never sees it.
var md = goldmark.New()

// MarkdownParser parses markdown via goldmark. It extracts YAML frontmatter,
// builds heading-scoped sections with exact 1-based line ranges, and collects
// link destinations from the AST (not regex, so fenced code blocks don't
// produce false-positive links). OKF index.md files are marked structure-only.
type MarkdownParser struct{}

// Parse implements parse.Parser.
func (MarkdownParser) Parse(_ context.Context, src model.Source) (model.ParsedDoc, error) {
	fm, err := extractFrontmatter(src.Content)
	if err != nil {
		return model.ParsedDoc{}, err
	}

	doc := model.ParsedDoc{
		Kind:            model.KindMarkdown,
		Frontmatter:     fm.matter,
		FrontmatterJSON: fm.rawJSON,
		Tags:            fm.tags,
		DocType:         fm.docType,
		ModifiedAt:      fm.modifiedAt,
		Metadata:        fm.metadata,
	}

	// OKF index.md: structure only — a documents row, no chunks/links/FTS.
	if strings.EqualFold(filepath.Base(src.URI), "index.md") {
		doc.StructureOnly = true
		doc.Title = titleFromBody(fm.body)

		return doc, nil
	}

	body := fm.body
	idx := newLineIndex(body)
	root := md.Parser().Parse(text.NewReader(body))

	doc.Sections = offsetSections(buildSections(root, body, idx), fm.lineOffset)
	doc.Links = extractLinks(root, body, src.URI)
	doc.Title = firstHeadingText(root, body)
	if doc.Title == "" {
		doc.Title = titleFromBody(body)
	}

	return doc, nil
}

// offsetSections shifts every section's line range by delta so ranges computed
// against the frontmatter-stripped body map back onto the original file lines.
func offsetSections(sections []model.Section, delta int) []model.Section {
	if delta == 0 {
		return sections
	}
	for i := range sections {
		sections[i].StartLine += delta
		sections[i].EndLine += delta
	}

	return sections
}

// headingMark records a heading occurrence: its byte offset, level, and text.
type headingMark struct {
	offset int
	level  int
	text   string
}

// buildSections turns the document into heading-scoped sections. The preamble
// before the first heading (if any) becomes a leading section with an empty
// heading path. Each subsequent section spans from its heading line to the line
// before the next heading. Heading paths are built from a level stack so a
// nested "## H2 / ### H3" yields "H2 > H3".
func buildSections(root ast.Node, source []byte, idx lineIndex) []model.Section {
	var heads []headingMark
	for c := root.FirstChild(); c != nil; c = c.NextSibling() {
		h, ok := c.(*ast.Heading)
		if !ok {
			continue
		}
		off, ok := nodeOffset(c, source)
		if !ok {
			continue
		}
		heads = append(heads, headingMark{offset: off, level: h.Level, text: nodeText(c, source)})
	}
	slices.SortStableFunc(heads, func(a, b headingMark) int { return cmp.Compare(a.offset, b.offset) })

	totalLines := idx.lineCount()
	var sections []model.Section

	// Preamble before the first heading.
	if len(heads) == 0 {
		sections = append(sections, model.Section{
			HeadingPath: "",
			StartLine:   1,
			EndLine:     totalLines,
			Content:     string(source),
		})

		return sections
	}
	if firstLine := idx.line(heads[0].offset); firstLine > 1 {
		end := firstLine - 1
		sections = append(sections, model.Section{
			HeadingPath: "",
			StartLine:   1,
			EndLine:     end,
			Content:     idx.slice(source, 1, end),
		})
	}

	stack := newHeadingStack()
	for i, h := range heads {
		startLine := idx.line(h.offset)
		endLine := totalLines
		if i+1 < len(heads) {
			endLine = idx.line(heads[i+1].offset) - 1
		}
		stack.push(h.level, h.text)
		sections = append(sections, model.Section{
			HeadingPath: stack.path(),
			StartLine:   startLine,
			EndLine:     endLine,
			Content:     idx.slice(source, startLine, endLine),
		})
	}

	return sections
}

// extractLinks walks the AST for link destinations and resolves relative ones
// against the source file's directory, returning portable URI strings.
func extractLinks(root ast.Node, _ []byte, srcURI string) []string {
	dir := filepath.Dir(srcURI)
	var links []string
	seen := make(map[string]struct{})

	_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		l, ok := n.(*ast.Link)
		if !ok {
			return ast.WalkContinue, nil
		}
		dst := string(l.Destination)
		if dst == "" || isExternal(dst) {
			return ast.WalkContinue, nil
		}
		resolved := dst
		if !filepath.IsAbs(dst) {
			resolved = filepath.ToSlash(filepath.Join(dir, dst))
		}
		if _, dup := seen[resolved]; dup {
			return ast.WalkContinue, nil
		}
		seen[resolved] = struct{}{}
		links = append(links, resolved)

		return ast.WalkContinue, nil
	})

	return links
}

// isExternal reports whether a link destination points outside the vault
// (absolute URL or in-page anchor) and should not become a links edge.
func isExternal(dst string) bool {
	if strings.HasPrefix(dst, "#") {
		return true
	}
	if i := strings.Index(dst, "://"); i > 0 {
		return true
	}

	return strings.HasPrefix(dst, "mailto:")
}

// firstHeadingText returns the text of the first top-level heading, or "".
func firstHeadingText(root ast.Node, source []byte) string {
	for c := root.FirstChild(); c != nil; c = c.NextSibling() {
		if _, ok := c.(*ast.Heading); ok {
			return nodeText(c, source)
		}
	}

	return ""
}

// titleFromBody falls back to the first non-empty line, stripped of a leading
// markdown heading marker.
func titleFromBody(body []byte) string {
	for line := range strings.SplitSeq(string(body), "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}

		return strings.TrimSpace(strings.TrimLeft(t, "#"))
	}

	return ""
}

// nodeOffset returns the byte offset of a node's first text segment.
func nodeOffset(n ast.Node, _ []byte) (int, bool) {
	if n.Type() == ast.TypeBlock {
		if lines := n.Lines(); lines != nil && lines.Len() > 0 {
			return lines.At(0).Start, true
		}
	}
	// Headings hold their text in inline children whose segments carry offsets.
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if t, ok := c.(*ast.Text); ok {
			return t.Segment.Start, true
		}
	}

	return 0, false
}

// nodeText returns the concatenated text of a node's descendant text segments.
func nodeText(n ast.Node, source []byte) string {
	var b strings.Builder
	_ = ast.Walk(n, func(c ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if t, ok := c.(*ast.Text); ok {
			_, _ = b.Write(t.Segment.Value(source))
		}

		return ast.WalkContinue, nil
	})

	return strings.TrimSpace(b.String())
}
