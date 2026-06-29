package parse

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/adrg/frontmatter"
)

// frontmatterResult holds the extracted YAML frontmatter, the body that follows
// it, and the well-known keys mapped onto ParsedDoc fields.
type frontmatterResult struct {
	matter     map[string]any
	body       []byte
	rawJSON    string
	tags       []string
	docType    string
	modifiedAt string
	// lineOffset is the number of source lines the frontmatter block consumed
	// (including delimiters and the trailing blank line). Body line N maps to
	// source line N+lineOffset, so citations stay exact against the original
	// file.
	lineOffset int
	// metadata stashes keys propagated into chunk metadata_json (e.g.
	// frontmatter `resource`).
	metadata map[string]any
}

// HasFrontmatter reports whether content opens with a YAML frontmatter block
// (a leading `---` delimiter line). It lets callers distinguish "no frontmatter
// at all" from "frontmatter present but missing a field".
func HasFrontmatter(content []byte) bool {
	return bytes.HasPrefix(content, []byte("---\n")) ||
		bytes.HasPrefix(content, []byte("---\r\n"))
}

// extractFrontmatter splits YAML frontmatter from content, returning the
// remaining body when no frontmatter is present (body == content). Well-known
// keys are mapped: `tags` (list), `type`→doc_type, `timestamp`→modified_at
// fallback, `resource`→chunk metadata. The full matter is re-encoded as JSON
// for documents.frontmatter_json.
func extractFrontmatter(content []byte) (frontmatterResult, error) {
	var matter map[string]any
	rest, err := frontmatter.Parse(bytes.NewReader(content), &matter)
	if err != nil {
		return frontmatterResult{}, fmt.Errorf("parse frontmatter: %w", err)
	}

	res := frontmatterResult{matter: matter, body: rest, lineOffset: lineDelta(content, rest)}
	if len(matter) == 0 {
		return res, nil
	}

	if b, err := json.Marshal(matter); err == nil {
		res.rawJSON = string(b)
	}
	res.tags = stringList(matter["tags"])
	res.docType = asString(matter["type"])
	res.modifiedAt = asString(matter["timestamp"])
	if r, ok := matter["resource"]; ok {
		res.metadata = map[string]any{"resource": r}
	}

	return res, nil
}

// lineDelta returns how many leading source lines were removed to produce body
// from content. It counts newlines in the prefix that content has over body.
func lineDelta(content, body []byte) int {
	prefixLen := len(content) - len(body)
	if prefixLen <= 0 {
		return 0
	}

	return bytes.Count(content[:prefixLen], []byte{'\n'})
}

// stringList coerces a frontmatter value into a slice of strings, accepting
// either a YAML list or a single comma/space-separated string.
func stringList(v any) []string {
	switch t := v.(type) {
	case []any:
		out := make([]string, 0, len(t))
		for _, e := range t {
			if s := asString(e); s != "" {
				out = append(out, s)
			}
		}

		return out
	case []string:
		return t
	case string:
		fields := strings.FieldsFunc(t, func(r rune) bool { return r == ',' || r == ' ' })

		return fields
	default:
		return nil
	}
}

// asString renders a scalar frontmatter value as a string.
func asString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	default:
		return fmt.Sprintf("%v", t)
	}
}
