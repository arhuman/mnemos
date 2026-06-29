package parse

import (
	"context"
	"testing"

	gast "github.com/yuin/goldmark/ast"
	gtext "github.com/yuin/goldmark/text"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/model"
)

// --- TextParser.Parse (text.go:17) ---

func TestTextParserParse(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		wantTitle string
		wantLines int
	}{
		{
			name:      "multi-line",
			content:   "hello\nworld\n",
			wantTitle: "hello",
			wantLines: 2,
		},
		{
			name:      "empty",
			content:   "",
			wantTitle: "",
			wantLines: 1,
		},
		{
			name:      "single line no newline",
			content:   "single",
			wantTitle: "single",
			wantLines: 1,
		},
		{
			name:      "single line with newline",
			content:   "only\n",
			wantTitle: "only",
			wantLines: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := TextParser{}.Parse(context.Background(), model.Source{
				URI:     "test.txt",
				Content: []byte(tc.content),
			})
			require.NoError(t, err)
			require.Equal(t, model.KindText, doc.Kind)
			require.Equal(t, tc.wantTitle, doc.Title)
			require.Len(t, doc.Sections, 1)
			require.Equal(t, 1, doc.Sections[0].StartLine)
			require.Equal(t, tc.wantLines, doc.Sections[0].EndLine)
			require.Equal(t, tc.content, doc.Sections[0].Content)
		})
	}
}

// --- code_go.go: receiverName (0%), declName (66%) ---

func TestGoParserMethodReceiver(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		wantSection string
	}{
		{
			name:        "value receiver",
			content:     "package p\n\ntype T struct{}\n\nfunc (T) Method() {}\n",
			wantSection: "method T.Method",
		},
		{
			name:        "pointer receiver",
			content:     "package p\n\ntype T struct{}\n\nfunc (*T) Method() {}\n",
			wantSection: "method T.Method",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := GoParser{}.Parse(context.Background(), model.Source{
				URI:     "p.go",
				Content: []byte(tc.content),
			})
			require.NoError(t, err)

			var found bool
			for _, s := range doc.Sections {
				if s.HeadingPath == tc.wantSection {
					found = true

					break
				}
			}
			require.True(t, found, "expected section %q among %v", tc.wantSection, doc.Sections)
		})
	}
}

// --- code_go.go: genDeclName (57%) ---

func TestGoParserGenDecl(t *testing.T) {
	// Exercises the ImportSpec default branch and ValueSpec branches in genDeclName.
	content := "package p\n\nimport \"fmt\"\n\nvar x = fmt.Sprintf(\"%d\", 1)\n\nconst y = 42\n"
	doc, err := GoParser{}.Parse(context.Background(), model.Source{
		URI:     "p.go",
		Content: []byte(content),
	})
	require.NoError(t, err)

	paths := make(map[string]bool, len(doc.Sections))
	for _, s := range doc.Sections {
		paths[s.HeadingPath] = true
	}
	require.True(t, paths["import"], "expected 'import' section from ImportSpec default branch")
	require.True(t, paths["var x"], "expected 'var x' section from ValueSpec branch")
	require.True(t, paths["const y"], "expected 'const y' section from ValueSpec branch")
}

// --- code_go.go: sliceLines (62%) ---

func TestSliceLinesEdgeCases(t *testing.T) {
	// No trailing newline so len(lines) equals actual line count.
	content := []byte("line1\nline2\nline3")

	tests := []struct {
		name  string
		start int
		end   int
		want  string
	}{
		{
			name:  "start below 1 clamped to 1",
			start: -1,
			end:   2,
			want:  "line1\nline2",
		},
		{
			name:  "end beyond content clamped to len",
			start: 2,
			end:   100,
			want:  "line2\nline3",
		},
		{
			name:  "end less than start returns empty",
			start: 3,
			end:   1,
			want:  "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, sliceLines(content, tc.start, tc.end))
		})
	}
}

// --- lines.go: slice (66%) ---

func TestLineIndexSliceEdgeCases(t *testing.T) {
	source := []byte("line1\nline2\nline3\n")
	idx := newLineIndex(source)

	tests := []struct {
		name  string
		start int
		end   int
		want  string
	}{
		{
			name:  "start below 1 clamped to 1",
			start: 0,
			end:   2,
			want:  "line1\nline2",
		},
		{
			name:  "end beyond total clamped to total",
			start: 2,
			end:   100,
			want:  "line2\nline3",
		},
		{
			name:  "end less than start returns empty",
			start: 3,
			end:   1,
			want:  "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, idx.slice(source, tc.start, tc.end))
		})
	}
}

// --- markdown.go: titleFromBody (66%) ---

func TestTitleFromBodyEdgeCases(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "blank lines before content skipped",
			body: "\n\nhello world\n",
			want: "hello world",
		},
		{
			name: "all blank lines returns empty",
			body: "\n\n\n",
			want: "",
		},
		{
			name: "empty body returns empty",
			body: "",
			want: "",
		},
		{
			name: "markdown heading marker stripped",
			body: "## Section Title\n",
			want: "Section Title",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, titleFromBody([]byte(tc.body)))
		})
	}
}

// --- frontmatter.go: stringList (63%) ---

func TestStringListEdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  []string
	}{
		{
			name:  "[]string passthrough",
			input: []string{"a", "b", "c"},
			want:  []string{"a", "b", "c"},
		},
		{
			name:  "comma-separated string",
			input: "foo,bar,baz",
			want:  []string{"foo", "bar", "baz"},
		},
		{
			name:  "space-separated string",
			input: "alpha beta gamma",
			want:  []string{"alpha", "beta", "gamma"},
		},
		{
			name:  "nil value returns nil",
			input: nil,
			want:  nil,
		},
		{
			name:  "unknown type returns nil",
			input: 42,
			want:  nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, stringList(tc.input))
		})
	}
}

// --- markdown.go: nodeOffset (43%) ---

// TestNodeOffsetChildSearch tests the child-walk code path in nodeOffset, which
// is reached when a block node has no raw Lines (e.g. a freshly constructed
// heading node). Two sub-cases: a Text child is found (returns offset, true) and
// no children exist (returns 0, false).
func TestNodeOffsetChildSearch(t *testing.T) {
	t.Run("text child found returns segment start", func(t *testing.T) {
		heading := gast.NewHeading(1)
		textNode := gast.NewText()
		textNode.Segment = gtext.NewSegment(10, 20)
		heading.AppendChild(heading, textNode)

		off, ok := nodeOffset(heading, nil)

		require.True(t, ok)
		require.Equal(t, 10, off)
	})

	t.Run("no children returns 0 false", func(t *testing.T) {
		heading := gast.NewHeading(1)

		off, ok := nodeOffset(heading, nil)

		require.False(t, ok)
		require.Equal(t, 0, off)
	})
}
