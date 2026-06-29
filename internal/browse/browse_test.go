package browse_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/browse"
	"github.com/arhuman/mnemos/internal/chunk"
	"github.com/arhuman/mnemos/internal/ingest"
	"github.com/arhuman/mnemos/internal/testutil"
)

// seedTree builds a tree under a fresh root with two indexed files (one carrying
// type/tags frontmatter), one indexable-but-unindexed file, and one
// non-indexable file. It returns the migrated db and the tree root.
func seedTree(t *testing.T) (*sql.DB, string) {
	t.Helper()
	db := testutil.NewDB(t)
	root := t.TempDir()
	log := testutil.DiscardLogger()
	cfg := chunk.Config{TargetTokens: 700, OverlapTokens: 80}

	p1 := testutil.WriteFile(t, root, "adr/0001.md", "---\ntype: adr\ntags: [a, b]\n---\n# One\n\nbody\n")
	p2 := testutil.WriteFile(t, root, "notes/idea.md", "# Idea\n\nbody\n")
	_, _, err := ingest.File(context.Background(), db, log, p1, "adr/0001.md", "arch", cfg)
	require.NoError(t, err)
	_, _, err = ingest.File(context.Background(), db, log, p2, "notes/idea.md", "notes", cfg)
	require.NoError(t, err)

	testutil.WriteFile(t, root, "notes/draft.md", "# Draft\n\nnot ingested\n")
	testutil.WriteFile(t, root, "image.png", "binary")

	return db, root
}

func TestListHybrid(t *testing.T) {
	db, root := seedTree(t)
	include := []string{"**/*.md", "**/*.txt"}

	list := func(o browse.Options) []browse.Entry {
		entries, err := browse.List(context.Background(), db, root, include, nil, nil, o)
		require.NoError(t, err)

		return entries
	}

	t.Run("annotates indexed and flags unindexed", func(t *testing.T) {
		entries := list(browse.Options{})
		require.Len(t, entries, 3) // png excluded by include globs
		byURI := indexByURI(entries)

		adr := byURI["adr/0001.md"]
		require.True(t, adr.Indexed)
		require.Equal(t, "adr", adr.Type)
		require.Equal(t, []string{"a", "b"}, adr.Tags)
		require.Equal(t, "arch", adr.Collection)

		require.False(t, byURI["notes/draft.md"].Indexed)
		require.Empty(t, byURI["notes/draft.md"].Collection)
	})

	t.Run("All includes non-indexable files", func(t *testing.T) {
		require.Len(t, list(browse.Options{All: true}), 4)
	})

	t.Run("path prefix narrows to a subtree", func(t *testing.T) {
		require.Len(t, list(browse.Options{PathPrefix: "notes/"}), 2)
		require.Len(t, list(browse.Options{PathPrefix: "notes"}), 2) // trailing slash optional
	})

	t.Run("path prefix matches at segment boundaries only", func(t *testing.T) {
		// "note" must not match the "notes/" directory: the prefix is path-aware,
		// not a raw string prefix.
		require.Empty(t, list(browse.Options{PathPrefix: "note"}))
	})

	t.Run("unindexed only", func(t *testing.T) {
		entries := list(browse.Options{UnindexedOnly: true})
		require.Len(t, entries, 1)
		require.Equal(t, "notes/draft.md", entries[0].URI)
	})

	t.Run("collection filter drops unindexed", func(t *testing.T) {
		entries := list(browse.Options{Collection: "notes"})
		require.Len(t, entries, 1)
		require.Equal(t, "notes/idea.md", entries[0].URI)
	})

	t.Run("limit caps the result", func(t *testing.T) {
		require.Len(t, list(browse.Options{Limit: 2}), 2)
	})

	t.Run("BuildTree groups by directory", func(t *testing.T) {
		tree := browse.BuildTree(list(browse.Options{}))
		require.Len(t, tree.Children, 2)
		require.True(t, tree.Children[0].IsDir)
		require.ElementsMatch(t, []string{"adr", "notes"},
			[]string{tree.Children[0].Name, tree.Children[1].Name})

		var notes *browse.TreeNode
		for _, c := range tree.Children {
			if c.Name == "notes" {
				notes = c
			}
		}
		require.NotNil(t, notes)
		require.Len(t, notes.Children, 2) // idea.md + draft.md
	})
}

func TestListPrunesExcludedDirectories(t *testing.T) {
	db := testutil.NewDB(t)
	root := t.TempDir()
	testutil.WriteFile(t, root, "doc.md", "# Doc\n\nbody\n")
	testutil.WriteFile(t, root, ".git/config.md", "# git\n\nnoise\n")
	testutil.WriteFile(t, root, "node_modules/pkg/readme.md", "# pkg\n\nnoise\n")

	entries, err := browse.List(context.Background(), db, root,
		[]string{"**/*.md"}, []string{".git/**", "node_modules/**"}, nil, browse.Options{})
	require.NoError(t, err)
	require.Len(t, entries, 1)
	require.Equal(t, "doc.md", entries[0].URI)

	// With All, the caller asked for everything on disk: pruning is off.
	all, err := browse.List(context.Background(), db, root,
		[]string{"**/*.md"}, []string{".git/**", "node_modules/**"}, nil, browse.Options{All: true})
	require.NoError(t, err)
	require.Len(t, all, 3)
}

// TestListRejectsTraversalPrefix guards the path-confinement boundary: a
// PathPrefix that escapes the tree root must never surface files from outside
// it. Regression test for the mnemos.list traversal bypass where "../" made the
// walk descend the parent of the tree root.
func TestListRejectsTraversalPrefix(t *testing.T) {
	db := testutil.NewDB(t)
	base := t.TempDir()
	root := filepath.Join(base, "tree")
	require.NoError(t, os.MkdirAll(root, 0o755))

	// One file inside the tree (must remain listable) ...
	testutil.WriteFile(t, root, "inside.md", "# Inside\n\nbody\n")
	// ... and secrets OUTSIDE the tree root that must never leak.
	require.NoError(t, os.WriteFile(filepath.Join(base, "secret.md"), []byte("# Secret\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(base, "sibling"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(base, "sibling", "creds.md"), []byte("# Creds\n"), 0o644))

	include := []string{"**/*.md"}
	list := func(o browse.Options) []browse.Entry {
		entries, err := browse.List(context.Background(), db, root, include, nil, nil, o)
		require.NoError(t, err)

		return entries
	}

	// Sanity: with no prefix only the in-tree file is listed.
	require.Len(t, list(browse.Options{}), 1)

	escaping := []string{
		"..",
		"../",
		"../sibling",
		"../../",
		filepath.Join(base, "sibling"), // absolute path outside the tree
	}
	for _, p := range escaping {
		t.Run("prefix "+p, func(t *testing.T) {
			entries := list(browse.Options{PathPrefix: p})
			require.Empty(t, entries, "escaping prefix %q must return no entries", p)
		})
		t.Run("prefix "+p+" with all", func(t *testing.T) {
			// All mode bypasses the include globs, so the leak (if any) would be
			// widest here.
			entries := list(browse.Options{PathPrefix: p, All: true})
			require.Empty(t, entries, "escaping prefix %q must return no entries even with All", p)
			for _, e := range entries {
				require.NotContains(t, e.URI, "..", "entry %q escapes the tree root", e.URI)
			}
		})
	}
}

func indexByURI(entries []browse.Entry) map[string]browse.Entry {
	m := make(map[string]browse.Entry, len(entries))
	for _, e := range entries {
		m[e.URI] = e
	}

	return m
}
