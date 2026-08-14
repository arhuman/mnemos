package memory_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/memory"
	"github.com/arhuman/mnemos/internal/storage"
)

// editableTask is a hand-authored Task document: aligned inline comments and a
// blank line, so a patch is proven not to reformat what it did not target.
const editableTask = `---
type: task
title: Ship the thing

status: todo               # backlog | todo | in_progress | done | cancelled
priority: high             # low | medium | high | critical
tags: [auth, bug]
---

## Goal

Ship it.
`

func readTree(t *testing.T, treeRoot, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(treeRoot, filepath.FromSlash(rel)))
	require.NoError(t, err)

	return string(b)
}

// TestEditFrontmatterPatchesAndReindexes proves the whole save path: fields are
// patched in place, the rest of the file is untouched, and the index reflects
// the new content afterwards.
func TestEditFrontmatterPatchesAndReindexes(t *testing.T) {
	f := newFixture(t, true, false)
	uri := f.seed(t, "tasks/ship.md", editableTask, "proj")
	ctx := context.Background()

	res, err := f.svc.EditFrontmatter(ctx, memory.EditFrontmatterInput{
		URI: uri,
		Fields: []memory.FieldEdit{
			{Key: "status", Value: "in_progress"},
			{Key: "title", Value: "Ship the other thing"},
			{Key: "tags", Items: []string{"auth", "cookie"}, List: true},
		},
	})
	require.NoError(t, err)
	require.Equal(t, uri, res.URI)
	require.NotEmpty(t, res.DocumentID)
	require.Positive(t, res.Chunks)

	onDisk := readTree(t, f.treeRoot, uri)
	// Only the value's bytes change: the padding that followed "todo" is carried
	// over untouched rather than realigned.
	require.Contains(t, onDisk, "status: in_progress               # backlog | todo | in_progress | done | cancelled")
	require.Contains(t, onDisk, "priority: high             # low | medium | high | critical")
	require.Contains(t, onDisk, "tags: [auth, cookie]")
	require.Contains(t, onDisk, "title: Ship the other thing\n\nstatus:", "the blank line survives the patch")
	require.Contains(t, onDisk, "\n## Goal\n\nShip it.\n")

	doc, err := storage.GetDocumentByURI(ctx, f.db, uri)
	require.NoError(t, err)
	require.NotNil(t, doc)
	require.Equal(t, "Ship the other thing", doc.Title, "the reindex picked up the patched title")
	require.Equal(t, "proj", doc.Collection, "an edit never moves the document's collection")
}

// TestEditBodyKeepsFrontmatter proves a body replacement carries the frontmatter
// block over byte for byte and reindexes the new body.
func TestEditBodyKeepsFrontmatter(t *testing.T) {
	f := newFixture(t, true, false)
	uri := f.seed(t, "tasks/ship.md", editableTask, "proj")
	ctx := context.Background()

	res, err := f.svc.EditBody(ctx, uri, "\n## Goal\n\nShipped already.\n")
	require.NoError(t, err)
	require.Positive(t, res.Chunks)

	onDisk := readTree(t, f.treeRoot, uri)
	require.Contains(t, onDisk, "status: todo               # backlog")
	require.Contains(t, onDisk, "Shipped already.")
	require.NotContains(t, onDisk, "Ship it.")
}

// TestEditRefusesWhenWriteDisabled proves the gate refuses before anything
// touches disk.
func TestEditRefusesWhenWriteDisabled(t *testing.T) {
	f := newFixture(t, false, false)
	uri := f.seed(t, "tasks/ship.md", editableTask, "proj")
	ctx := context.Background()

	_, err := f.svc.EditFrontmatter(ctx, memory.EditFrontmatterInput{
		URI:    uri,
		Fields: []memory.FieldEdit{{Key: "status", Value: "done"}},
	})
	require.ErrorContains(t, err, "allow_write")

	_, err = f.svc.EditBody(ctx, uri, "changed")
	require.ErrorContains(t, err, "allow_write")

	require.Equal(t, editableTask, readTree(t, f.treeRoot, uri), "a refused edit leaves the file alone")
}

// TestEditReindexFailureKeepsTheWrite proves the two failure severities stay
// distinguishable: with the store gone the edit is still durable on disk, and
// the error says so.
func TestEditReindexFailureKeepsTheWrite(t *testing.T) {
	f := newFixture(t, true, false)
	uri := f.seed(t, "tasks/ship.md", editableTask, "proj")
	require.NoError(t, f.db.Close())

	res, err := f.svc.EditFrontmatter(context.Background(), memory.EditFrontmatterInput{
		URI:    uri,
		Fields: []memory.FieldEdit{{Key: "status", Value: "done"}},
	})
	require.ErrorIs(t, err, memory.ErrReindexAfterWrite)
	require.Equal(t, uri, res.URI)
	require.Contains(t, readTree(t, f.treeRoot, uri), "status: done", "the edit reached disk before the reindex ran")
}

// TestEditRejectsUneditableAndUnknownTargets covers the refusals that happen
// before any write: a document with no patchable frontmatter, an unknown key,
// a missing file, and a path outside the tree.
func TestEditRejectsUneditableAndUnknownTargets(t *testing.T) {
	f := newFixture(t, true, false)
	ctx := context.Background()
	plain := f.seed(t, "docs/plain.md", "# Plain\n\nNo frontmatter here.\n", "proj")
	task := f.seed(t, "tasks/ship.md", editableTask, "proj")

	cases := []struct {
		name  string
		in    memory.EditFrontmatterInput
		check func(t *testing.T, err error)
	}{
		{
			name: "no frontmatter",
			in:   memory.EditFrontmatterInput{URI: plain, Fields: []memory.FieldEdit{{Key: "status", Value: "done"}}},
			check: func(t *testing.T, err error) {
				t.Helper()
				require.ErrorIs(t, err, memory.ErrNotEditable)
			},
		},
		{
			name: "unknown key",
			in:   memory.EditFrontmatterInput{URI: task, Fields: []memory.FieldEdit{{Key: "nope", Value: "x"}}},
			check: func(t *testing.T, err error) {
				t.Helper()
				require.ErrorContains(t, err, "unknown frontmatter key")
			},
		},
		{
			name: "missing file",
			in:   memory.EditFrontmatterInput{URI: "tasks/gone.md"},
			check: func(t *testing.T, err error) {
				t.Helper()
				require.ErrorContains(t, err, "edit read")
			},
		},
		{
			name: "outside the tree",
			in:   memory.EditFrontmatterInput{URI: "../escape.md"},
			check: func(t *testing.T, err error) {
				t.Helper()
				require.ErrorContains(t, err, "edit path")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := f.svc.EditFrontmatter(ctx, tc.in)
			require.Error(t, err)
			tc.check(t, err)
		})
	}

	require.Equal(t, editableTask, readTree(t, f.treeRoot, task), "a refused edit leaves the file alone")
}

// TestEditHonorsContextCancellation covers the ctx guard at the head of the verb.
func TestEditHonorsContextCancellation(t *testing.T) {
	f := newFixture(t, true, false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := f.svc.EditFrontmatter(ctx, memory.EditFrontmatterInput{URI: "tasks/ship.md"})
	require.ErrorIs(t, err, context.Canceled)
}

// TestOpenForEditSplitsFrontmatter proves an editor sees the document's own
// bytes, its fields in file order, and its type.
func TestOpenForEditSplitsFrontmatter(t *testing.T) {
	f := newFixture(t, true, false)
	uri := f.seed(t, "tasks/ship.md", editableTask, "proj")

	src, err := f.svc.OpenForEdit(context.Background(), uri)
	require.NoError(t, err)
	require.True(t, src.Editable)
	require.Equal(t, "task", src.Type)
	require.Equal(t, "proj", src.Collection)
	require.Equal(t, editableTask, string(src.Content))
	require.Equal(t, "\n## Goal\n\nShip it.\n", src.Body)

	keys := make([]string, len(src.Fields))
	for i, fl := range src.Fields {
		keys[i] = fl.Key
	}
	require.Equal(t, []string{"type", "title", "status", "priority", "tags"}, keys)
}

// TestOpenForEditDegradesToReadOnly proves a document without patchable
// frontmatter opens read-only rather than failing.
func TestOpenForEditDegradesToReadOnly(t *testing.T) {
	f := newFixture(t, true, false)
	uri := f.seed(t, "docs/plain.md", "# Plain\n\nBody.\n", "proj")

	src, err := f.svc.OpenForEdit(context.Background(), uri)
	require.NoError(t, err)
	require.False(t, src.Editable)
	require.Empty(t, src.Fields)
	require.Equal(t, "# Plain\n\nBody.\n", src.Body)
}

// TestOpenForEditRejectsMissingAndEscapingPaths covers the two refusals.
func TestOpenForEditRejectsMissingAndEscapingPaths(t *testing.T) {
	f := newFixture(t, true, false)
	ctx := context.Background()

	_, err := f.svc.OpenForEdit(ctx, "tasks/gone.md")
	require.ErrorContains(t, err, "edit read")

	_, err = f.svc.OpenForEdit(ctx, "../escape.md")
	require.ErrorContains(t, err, "edit path")
}

// TestSimilarIsEmptyWithoutEmbeddings proves the un-embedded corpus degrades to
// "unavailable" rather than erroring, which is what the editor renders.
func TestSimilarIsEmptyWithoutEmbeddings(t *testing.T) {
	f := newFixture(t, false, false)
	uri := f.seed(t, "docs/plain.md", "# Plain\n\nBody.\n", "proj")

	got, err := f.svc.Similar(context.Background(), uri, 5)
	require.NoError(t, err)
	require.Empty(t, got)
}
