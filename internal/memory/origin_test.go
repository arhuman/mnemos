package memory_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/ingest"
	"github.com/arhuman/mnemos/internal/memory"
	"github.com/arhuman/mnemos/internal/storage"
)

// registerOrigin registers an external tree with one file against the fixture.
func registerOrigin(t *testing.T, f fixture, prefix string) string {
	t.Helper()
	ext := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(ext, "note.md"),
		[]byte("---\ntype: Reference\n---\n\n# Note\n\nbody\n"), 0o600))

	_, err := ingest.RegisterOrigin(context.Background(), f.db, f.treeRoot, prefix, ext, "c")
	require.NoError(t, err)

	return ext
}

// TestWriteVerbsRefuseOriginURIs is the read-only boundary (ADR-0013). It is a
// regression test for a real defect: an origin uri is a plain relative path, so
// the confinement guard accepts it and resolves to a file that does not exist
// under the kb, which Forget treated as an index-only deletion. Without the
// explicit refusal in front of the guard, a registered origin's index is
// silently deletable through an ordinary verb.
func TestWriteVerbsRefuseOriginURIs(t *testing.T) {
	f := newFixture(t, true, true)
	ext := registerOrigin(t, f, "spec")
	ctx := context.Background()

	t.Run("forget", func(t *testing.T) {
		_, err := f.svc.Forget(ctx, "spec/note.md")
		require.ErrorIs(t, err, memory.ErrOriginReadOnly)
		require.ErrorContains(t, err, "spec", "the error names the owning origin")
	})

	t.Run("move source", func(t *testing.T) {
		_, err := f.svc.Move(ctx, "spec/note.md", "elsewhere.md")
		require.ErrorIs(t, err, memory.ErrOriginReadOnly)
	})

	t.Run("move destination", func(t *testing.T) {
		require.NoError(t, os.WriteFile(filepath.Join(f.treeRoot, "own.md"), []byte("# Own\n\nbody\n"), 0o600))
		_, err := f.svc.Move(ctx, "own.md", "spec/moved.md")
		require.ErrorIs(t, err, memory.ErrOriginReadOnly, "an origin must not be a move destination either")
	})

	t.Run("remember into an origin", func(t *testing.T) {
		_, err := f.svc.Remember(ctx, memory.RememberInput{
			Type: "idea", Text: "text", Path: "spec/new.md",
		})
		require.ErrorIs(t, err, memory.ErrOriginReadOnly)
	})

	t.Run("edit", func(t *testing.T) {
		_, err := f.svc.OpenForEdit(ctx, "spec/note.md")
		require.ErrorIs(t, err, memory.ErrOriginReadOnly)
	})

	require.FileExists(t, filepath.Join(ext, "note.md"), "the origin's file is never touched")
}

// TestKBWritesStillWork proves the refusal is scoped to registered namespaces and
// does not become a blanket denial.
func TestKBWritesStillWork(t *testing.T) {
	f := newFixture(t, true, true)
	registerOrigin(t, f, "spec")
	ctx := context.Background()

	res, err := f.svc.Remember(ctx, memory.RememberInput{Type: "idea", Text: "a kb note"})
	require.NoError(t, err)
	require.NotEmpty(t, res.URI)
}

// TestPrefixLookaleIsSegmentAware proves "spec" does not capture "specs-archive".
func TestPrefixLookaleIsSegmentAware(t *testing.T) {
	f := newFixture(t, true, true)
	registerOrigin(t, f, "spec")
	ctx := context.Background()

	_, err := f.svc.Remember(ctx, memory.RememberInput{
		Type: "idea", Text: "text", Path: "specs-archive/new.md",
	})
	require.NotErrorIs(t, err, memory.ErrOriginReadOnly)
}

// TestOriginListedInStore is a guard on the fixture itself.
func TestOriginListedInStore(t *testing.T) {
	f := newFixture(t, true, true)
	registerOrigin(t, f, "spec")

	origins, err := storage.ListOrigins(context.Background(), f.db)
	require.NoError(t, err)
	require.Len(t, origins, 1)
	require.Equal(t, "spec", origins[0].Prefix)
}
