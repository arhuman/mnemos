package cli_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// writeTree creates a file with content under dir, making parent directories.
func writeTree(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
}

// TestIngestPopulatesStore ingests a tiny tree and asserts documents/chunks/
// links counts, that index.md yields a document with zero chunks, and that a
// re-ingest skips unchanged files.
func TestIngestPopulatesStore(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")

	// Content lives inside the kb (managed store); ingest "src" scans kb/src.
	seedKB(t, filepath.Join("src", "a.md"), "---\ntype: note\ntags: [x]\n---\n\n# Title\n\nBody. [link](b.md)\n")
	seedKB(t, filepath.Join("src", "index.md"), "# Bundle\n\nstructure only [hub](a.md)\n")
	seedKB(t, filepath.Join("src", "note.txt"), "plain text\n\nsecond para\n")

	out := runCmd(t, "ingest", "src", "--collection", "demo")
	require.Contains(t, out, "files scanned:   3")
	require.Contains(t, out, "files ingested:  3")

	db, err := sql.Open("sqlite", filepath.Join(".mnemos", "state", "index.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	t.Run("document count", func(t *testing.T) {
		require.Equal(t, 3, count(t, db, `SELECT COUNT(*) FROM documents`))
	})

	t.Run("index.md has a doc but zero chunks", func(t *testing.T) {
		var docID string
		require.NoError(t, db.QueryRowContext(context.Background(), `SELECT id FROM documents WHERE uri = 'index.md'`).Scan(&docID))
		require.Equal(t, 0, count(t, db,
			`SELECT COUNT(*) FROM chunks WHERE document_id = ?`, docID))
	})

	t.Run("chunks were written", func(t *testing.T) {
		require.Positive(t, count(t, db, `SELECT COUNT(*) FROM chunks`))
	})

	t.Run("links exclude index.md hub", func(t *testing.T) {
		// Only a.md's [link](b.md) should be recorded; index.md links are dropped.
		require.Equal(t, 1, count(t, db, `SELECT COUNT(*) FROM links`))
		var dst string
		require.NoError(t, db.QueryRowContext(context.Background(), `SELECT dst_doc FROM links`).Scan(&dst))
		require.Equal(t, "b.md", dst)
	})

	t.Run("event per ingested document", func(t *testing.T) {
		require.Equal(t, 3, count(t, db, `SELECT COUNT(*) FROM events WHERE type = 'ingested'`))
	})

	t.Run("fts is populated", func(t *testing.T) {
		require.Positive(t, count(t, db, `SELECT COUNT(*) FROM chunks_fts`))
	})

	t.Run("re-ingest skips unchanged", func(t *testing.T) {
		out := runCmd(t, "ingest", "src", "--collection", "demo")
		require.Contains(t, out, "files skipped:   3")
		require.Contains(t, out, "files ingested:  0")
	})
}

// TestIngestRejectsOutsideTreeRoot asserts that a scan root outside the tree
// root is refused (it would mint URIs whose files read/ls/move cannot resolve).
func TestIngestRejectsOutsideTreeRoot(t *testing.T) {
	workdir := t.TempDir()
	chdir(t, workdir)
	runCmd(t, "init")

	outside := t.TempDir()
	writeTree(t, outside, "a.md", "# Outside\n")

	_, err := runCmdErr(t, "ingest", outside, "--collection", "demo")
	require.Error(t, err)
	require.Contains(t, err.Error(), "outside the tree root")
}

// TestIngestHonorsFrontmatterCollection asserts a document's own collection:
// frontmatter wins over the --collection flag, so a re-index preserves it.
func TestIngestHonorsFrontmatterCollection(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")

	seedKB(t, "doc.md", "---\ntype: note\ncollection: epfl\n---\n# Doc\n\nfindable body content\n")
	runCmd(t, "ingest", ".", "--collection", "flagcoll")

	// The document is filed under epfl (frontmatter), not flagcoll (flag).
	hit := runCmd(t, "search", "findable body content", "--collection", "epfl")
	require.Contains(t, hit, "doc.md")

	miss := runCmd(t, "search", "findable body content", "--collection", "flagcoll")
	require.NotContains(t, miss, "doc.md")
}

// TestIngestSkipsUnparseableFile asserts a single malformed-frontmatter file is
// skipped with a warning rather than aborting the whole batch.
func TestIngestSkipsUnparseableFile(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")

	seedKB(t, "good.md", "# Good\n\nindexable body\n")
	seedKB(t, "bad.md", "---\nfoo: bar: baz\n---\n# Bad\n\nbody\n") // broken YAML frontmatter

	out := runCmd(t, "ingest", ".", "--collection", "demo") // must not error
	require.Contains(t, out, "files ingested:  1")           // only good.md
}

// count runs a scalar COUNT query and returns the integer result.
func count(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRowContext(context.Background(), query, args...).Scan(&n))

	return n
}
