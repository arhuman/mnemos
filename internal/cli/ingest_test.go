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
	workdir := t.TempDir()
	chdir(t, workdir)

	src := filepath.Join(workdir, "src")
	writeTree(t, src, "a.md", "---\ntype: note\ntags: [x]\n---\n\n# Title\n\nBody. [link](b.md)\n")
	writeTree(t, src, "index.md", "# Bundle\n\nstructure only [hub](a.md)\n")
	writeTree(t, src, "note.txt", "plain text\n\nsecond para\n")

	runCmd(t, "init")

	out := runCmd(t, "ingest", "src", "--collection", "demo")
	require.Contains(t, out, "files scanned:   3")
	require.Contains(t, out, "files ingested:  3")

	db, err := sql.Open("sqlite", filepath.Join(".mnemos", "mnemos.db"))
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

// count runs a scalar COUNT query and returns the integer result.
func count(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRowContext(context.Background(), query, args...).Scan(&n))

	return n
}
