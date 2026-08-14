package okfyaml_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/okfyaml"
)

// taskFixture mirrors the shape of a hand-authored Task document (aligned inline
// comments, a blank line, a flow list and a block list) so a patch is proven
// against the formatting a human actually writes.
const taskFixture = `---
type: task
title: Fix session cookie not cleared on logout

status: in_progress        # backlog | todo | in_progress | done | cancelled
priority: high             # low | medium | high | critical
completed:
tags: [auth, bug]
refs:                      # citations
  - decisions/0003-session-handling.md
  - code/auth.go
---

## Goal

Ship it.
`

func parseFixture(t *testing.T, content string) okfyaml.Doc {
	t.Helper()
	doc, ok, err := okfyaml.Parse([]byte(content))
	require.NoError(t, err)
	require.True(t, ok)

	return doc
}

// TestPatchScalarPreservesCommentsAndOrder proves a scalar patch rewrites only
// the targeted value: every other byte of the document, including the aligned
// inline comments and the blank line, is untouched.
func TestPatchScalarPreservesCommentsAndOrder(t *testing.T) {
	out, err := parseFixture(t, taskFixture).PatchScalar("status", "done")
	require.NoError(t, err)

	want := strings.Replace(taskFixture, "status: in_progress ", "status: done ", 1)
	require.Equal(t, want, string(out))
}

// TestPatchScalarShapes covers the value shapes a hand-authored frontmatter can
// carry: a null field, a quoted field, and a value that needs quoting.
func TestPatchScalarShapes(t *testing.T) {
	cases := []struct {
		name  string
		src   string
		key   string
		value string
		want  string
	}{
		{
			name:  "null field gains a separator",
			src:   "---\ncompleted:\n---\nbody\n",
			key:   "completed",
			value: "2026-08-05",
			want:  "---\ncompleted: 2026-08-05\n---\nbody\n",
		},
		{
			name:  "null field with a comment keeps its padding",
			src:   "---\ncompleted:            # set when done\n---\nbody\n",
			key:   "completed",
			value: "2026-08-05",
			want:  "---\ncompleted: 2026-08-05            # set when done\n---\nbody\n",
		},
		{
			name:  "quoted field stays quoted",
			src:   "---\ntitle: \"a # b\"\n---\nbody\n",
			key:   "title",
			value: "c",
			want:  "---\ntitle: \"c\"\n---\nbody\n",
		},
		{
			name:  "value needing quotes is quoted",
			src:   "---\ntitle: plain\n---\nbody\n",
			key:   "title",
			value: "a: b",
			want:  "---\ntitle: 'a: b'\n---\nbody\n",
		},
		{
			name:  "closing delimiter without a trailing newline",
			src:   "---\nstatus: todo\n---",
			key:   "status",
			value: "done",
			want:  "---\nstatus: done\n---",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := parseFixture(t, tc.src).PatchScalar(tc.key, tc.value)
			require.NoError(t, err)
			require.Equal(t, tc.want, string(out))
		})
	}
}

// TestPatchScalarRejects covers the refusals: an absent key and a value shape
// this package will not rewrite positionally.
func TestPatchScalarRejects(t *testing.T) {
	doc := parseFixture(t, taskFixture)

	_, err := doc.PatchScalar("nope", "x")
	require.ErrorIs(t, err, okfyaml.ErrUnknownKey)

	_, err = doc.PatchScalar("refs", "x")
	require.ErrorContains(t, err, "not a scalar")

	block := parseFixture(t, "---\nnotes: |\n  line one\n  line two\n---\nbody\n")
	_, err = block.PatchScalar("notes", "x")
	require.ErrorContains(t, err, "not editable")
}

// TestPatchTagsReplacesWholeList proves both list shapes are retyped in place
// without disturbing the key line or the rest of the document.
func TestPatchTagsReplacesWholeList(t *testing.T) {
	t.Run("flow list", func(t *testing.T) {
		out, err := parseFixture(t, taskFixture).PatchTags("tags", []string{"auth", "cookie", "fix"})
		require.NoError(t, err)
		require.Equal(t, strings.Replace(taskFixture, "tags: [auth, bug]", "tags: [auth, cookie, fix]", 1), string(out))
	})

	t.Run("block list keeps its key comment and indent", func(t *testing.T) {
		out, err := parseFixture(t, taskFixture).PatchTags("refs", []string{"decisions/0004.md"})
		require.NoError(t, err)
		want := strings.Replace(taskFixture,
			"  - decisions/0003-session-handling.md\n  - code/auth.go\n",
			"  - decisions/0004.md\n", 1)
		require.Equal(t, want, string(out))
	})

	t.Run("null key becomes a flow list", func(t *testing.T) {
		out, err := parseFixture(t, "---\ntags:\n---\nbody\n").PatchTags("tags", []string{"a", "b"})
		require.NoError(t, err)
		require.Equal(t, "---\ntags: [a, b]\n---\nbody\n", string(out))
	})

	t.Run("empty flow list", func(t *testing.T) {
		out, err := parseFixture(t, "---\ntags: [a]\n---\nbody\n").PatchTags("tags", nil)
		require.NoError(t, err)
		require.Equal(t, "---\ntags: []\n---\nbody\n", string(out))
	})

	t.Run("non-list key is refused", func(t *testing.T) {
		_, err := parseFixture(t, taskFixture).PatchTags("title", []string{"a"})
		require.ErrorContains(t, err, "not a list")
	})
}

// TestFieldsPreservesOrder proves the metadata pane sees keys in file order,
// with lists flagged and rendered for display.
func TestFieldsPreservesOrder(t *testing.T) {
	fields := parseFixture(t, taskFixture).Fields()

	keys := make([]string, len(fields))
	for i, f := range fields {
		keys[i] = f.Key
	}
	require.Equal(t, []string{"type", "title", "status", "priority", "completed", "tags", "refs"}, keys)

	byKey := make(map[string]okfyaml.Field, len(fields))
	for _, f := range fields {
		byKey[f.Key] = f
	}
	require.Equal(t, "in_progress", byKey["status"].Value)
	require.Empty(t, byKey["completed"].Value)
	require.True(t, byKey["tags"].IsList)
	require.Equal(t, "auth, bug", byKey["tags"].Value)
	require.Equal(t, "decisions/0003-session-handling.md, code/auth.go", byKey["refs"].Value)
	require.False(t, byKey["title"].IsList)
}

// TestParseDeclinesUneditableShapes proves Parse reports ok=false (rather than
// erroring, or worse, accepting) for documents whose source spans it cannot
// track, so a caller degrades to read-only.
func TestParseDeclinesUneditableShapes(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"no frontmatter", "# Title\n\nBody\n"},
		{"anchors", "---\nbase: &b value\nother: *b\n---\nbody\n"},
		{"multi document", "---\na: 1\n--- \nb: 2\n---\nbody\n"},
		{"sequence root", "---\n- a\n- b\n---\nbody\n"},
		{"crlf delimiters", "---\r\ntype: task\r\n---\r\nbody\r\n"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, ok, err := okfyaml.Parse([]byte(tc.src))
			require.NoError(t, err)
			require.False(t, ok)
		})
	}
}

// TestParseMalformedYAMLErrors proves genuinely broken frontmatter surfaces as
// an error rather than a silent read-only fallback.
func TestParseMalformedYAMLErrors(t *testing.T) {
	_, ok, err := okfyaml.Parse([]byte("---\na: [1, 2\n---\nbody\n"))
	require.Error(t, err)
	require.False(t, ok)
}

// TestBodyAndReplaceBody proves the body round-trips verbatim and a body swap
// carries the frontmatter block over unchanged.
func TestBodyAndReplaceBody(t *testing.T) {
	doc := parseFixture(t, taskFixture)
	require.Equal(t, "\n## Goal\n\nShip it.\n", string(doc.Body()))

	out := doc.ReplaceBody([]byte("\n## Goal\n\nShipped.\n"))
	require.Equal(t, strings.Replace(taskFixture, "Ship it.", "Shipped.", 1), string(out))
}
