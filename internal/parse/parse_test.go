package parse

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/model"
)

func parseMarkdown(t *testing.T, uri, content string) model.ParsedDoc {
	t.Helper()
	doc, err := MarkdownParser{}.Parse(context.Background(), model.Source{
		URI: uri, Content: []byte(content),
	})
	require.NoError(t, err)

	return doc
}

func TestMarkdownFrontmatterMapping(t *testing.T) {
	content := "---\ntype: note\ntags: [a, b]\ntimestamp: 2024-01-02T00:00:00Z\nresource: r1\n---\n\n# H\n\nbody\n"
	doc := parseMarkdown(t, "docs/x.md", content)

	require.Equal(t, "note", doc.DocType)
	require.Equal(t, []string{"a", "b"}, doc.Tags)
	require.Equal(t, "2024-01-02T00:00:00Z", doc.ModifiedAt)
	require.Equal(t, map[string]any{"resource": "r1"}, doc.Metadata)
	require.NotEmpty(t, doc.FrontmatterJSON)
}

func TestMarkdownTitlePrecedence(t *testing.T) {
	cases := []struct {
		name    string
		uri     string
		content string
		want    string
	}{
		{
			name:    "frontmatter title wins over heading",
			uri:     "docs/x.md",
			content: "---\ntitle: Real Title\n---\n\n## Goal\n\nbody\n",
			want:    "Real Title",
		},
		{
			name:    "heading used when no frontmatter title",
			uri:     "docs/x.md",
			content: "---\ntype: task\n---\n\n## Goal\n\nbody\n",
			want:    "Goal",
		},
		{
			name:    "body line used when neither title nor heading",
			uri:     "docs/x.md",
			content: "just a plain line\nmore\n",
			want:    "just a plain line",
		},
		{
			name:    "blank frontmatter title falls back to heading",
			uri:     "docs/x.md",
			content: "---\ntitle: \"   \"\n---\n\n## Goal\n\nbody\n",
			want:    "Goal",
		},
		{
			name:    "index.md honours frontmatter title",
			uri:     "docs/index.md",
			content: "---\ntitle: Bundle Title\n---\n\n# Heading\n",
			want:    "Bundle Title",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc := parseMarkdown(t, tc.uri, tc.content)
			require.Equal(t, tc.want, doc.Title)
		})
	}
}

func TestMarkdownLineRangesAreFileAbsolute(t *testing.T) {
	// 6 frontmatter lines + blank line 7; "# H" is file line 8.
	content := "---\ntype: note\ntags: [a]\nresource: r\ntimestamp: t\n---\n\n# H\n\nbody\n"
	doc := parseMarkdown(t, "docs/x.md", content)

	var headSec *model.Section
	for i := range doc.Sections {
		if doc.Sections[i].HeadingPath == "H" {
			headSec = &doc.Sections[i]
		}
	}
	require.NotNil(t, headSec, "expected an H heading section")
	require.Equal(t, 8, headSec.StartLine, "heading must map to original file line")
}

func TestMarkdownHeadingStack(t *testing.T) {
	content := "# A\n\ntop\n\n## B\n\nmid\n\n### C\n\nleaf\n\n## D\n\nsibling\n"
	doc := parseMarkdown(t, "x.md", content)

	paths := make([]string, 0, len(doc.Sections))
	for _, s := range doc.Sections {
		paths = append(paths, s.HeadingPath)
	}
	require.Equal(t, []string{"A", "A > B", "A > B > C", "A > D"}, paths)
}

func TestMarkdownLinksResolvedAndFiltered(t *testing.T) {
	content := "# H\n\n[rel](concept.md) [ext](https://x.com) [anchor](#frag) [up](../other/y.md)\n"
	doc := parseMarkdown(t, "docs/sub/x.md", content)

	require.Equal(t, []string{"docs/sub/concept.md", "docs/other/y.md"}, doc.Links,
		"external and anchor links must be dropped; relative links resolved against source dir")
}

func TestMarkdownLinksIgnoreCodeBlocks(t *testing.T) {
	content := "# H\n\n```\n[notalink](evil.md)\n```\n\n[real](good.md)\n"
	doc := parseMarkdown(t, "x.md", content)
	require.Equal(t, []string{"good.md"}, doc.Links)
}

func TestIndexMarkdownIsStructureOnly(t *testing.T) {
	content := "# Bundle\n\n[hub](a.md)\n"
	doc := parseMarkdown(t, "docs/index.md", content)

	require.True(t, doc.StructureOnly)
	require.Empty(t, doc.Sections)
	require.Empty(t, doc.Links)
}

func TestGoParserDeclarations(t *testing.T) {
	content := "package p\n\n// Hi greets.\nfunc Hi() {}\n\ntype T struct{}\n"
	doc, err := GoParser{}.Parse(context.Background(), model.Source{URI: "p.go", Content: []byte(content)})
	require.NoError(t, err)
	require.Equal(t, model.KindGoCode, doc.Kind)

	paths := make([]string, 0, len(doc.Sections))
	for _, s := range doc.Sections {
		paths = append(paths, s.HeadingPath)
	}
	require.Equal(t, []string{"func Hi", "type T"}, paths)
	require.Equal(t, 3, doc.Sections[0].StartLine, "doc comment line included")
}

func TestGoParserFallbackOnInvalidSource(t *testing.T) {
	content := "this is not go code at all { ] }\n"
	doc, err := GoParser{}.Parse(context.Background(), model.Source{URI: "bad.go", Content: []byte(content)})
	require.NoError(t, err)
	require.Len(t, doc.Sections, 1, "fallback to a single whole-file section")
	require.Equal(t, 1, doc.Sections[0].StartLine)
}

func TestRegistryDispatch(t *testing.T) {
	require.IsType(t, MarkdownParser{}, For("a.md"))
	require.IsType(t, MarkdownParser{}, For("a.markdown"))
	require.IsType(t, GoParser{}, For("a.go"))
	require.IsType(t, TextParser{}, For("a.sql"))
	require.IsType(t, TextParser{}, For("a.unknown"))
}
