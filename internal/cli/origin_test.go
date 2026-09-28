package cli_test

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

// externalTree creates a directory outside the kb with one markdown file.
func externalTree(t *testing.T, rel, content string) string {
	t.Helper()
	dir := t.TempDir()
	writeTree(t, dir, rel, content)

	return dir
}

// TestOriginAddIndexesInPlace covers the whole point of the feature: an external
// tree is indexed under its namespace and nothing is copied into the kb.
func TestOriginAddIndexesInPlace(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")
	ext := externalTree(t, "rule.md", "# Rule\n\nReferral must be validated.\n")

	out := runCmd(t, "origin", "add", ext, "--prefix", "spec", "--collection", "s")
	require.Contains(t, out, "registered:")
	require.Contains(t, out, "files indexed:   1")

	db, err := sql.Open("sqlite", filepath.Join(".mnemos", "state", "index.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.Equal(t, 1, count(t, db, `SELECT COUNT(*) FROM documents WHERE uri = 'spec/rule.md'`))

	entries, err := os.ReadDir(filepath.Join(".mnemos", "kb"))
	require.NoError(t, err)
	for _, e := range entries {
		require.NotEqual(t, "spec", e.Name(), "an origin is indexed in place, never copied")
	}
}

// TestOriginListAndJSON covers the table and the export used to replay a
// registration set after losing the database.
func TestOriginListAndJSON(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")

	require.Contains(t, runCmd(t, "origin", "list"), "no registered origins")

	ext := externalTree(t, "a.md", "# A\n\nbody\n")
	runCmd(t, "origin", "add", ext, "--prefix", "spec")

	table := runCmd(t, "origin", "list")
	require.Contains(t, table, "PREFIX")
	require.Contains(t, table, "spec")
	require.Contains(t, table, "ok")

	var got []struct {
		Prefix     string `json:"prefix"`
		Path       string `json:"path"`
		Collection string `json:"collection"`
		Status     string `json:"status"`
	}
	require.NoError(t, json.Unmarshal([]byte(runCmd(t, "origin", "list", "--json")), &got))
	require.Len(t, got, 1)
	require.Equal(t, "spec", got[0].Prefix)
	require.Equal(t, "ok", got[0].Status)
	require.NotEmpty(t, got[0].Path)
}

// TestOriginReindexPicksUpChanges proves the on-demand refresh finds new files
// and evicts deleted ones, which is what makes a registration converge.
func TestOriginReindexPicksUpChanges(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")
	ext := externalTree(t, "a.md", "# A\n\nbody\n")
	runCmd(t, "origin", "add", ext, "--prefix", "spec")

	writeTree(t, ext, "b.md", "# B\n\nbody\n")
	require.NoError(t, os.Remove(filepath.Join(ext, "a.md")))

	out := runCmd(t, "origin", "reindex", "spec")
	require.Contains(t, out, "files indexed:   1")
	require.Contains(t, out, "files removed:   1")

	db, err := sql.Open("sqlite", filepath.Join(".mnemos", "state", "index.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.Equal(t, 0, count(t, db, `SELECT COUNT(*) FROM documents WHERE uri = 'spec/a.md'`))
	require.Equal(t, 1, count(t, db, `SELECT COUNT(*) FROM documents WHERE uri = 'spec/b.md'`))
}

// TestOriginReindexAllWithNoArg covers the no-argument form, which walks every
// registered origin.
func TestOriginReindexAllWithNoArg(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")
	runCmd(t, "origin", "add", externalTree(t, "a.md", "# A\n\nbody\n"), "--prefix", "one")
	runCmd(t, "origin", "add", externalTree(t, "b.md", "# B\n\nbody\n"), "--prefix", "two")

	out := runCmd(t, "origin", "reindex")
	require.Contains(t, out, "one")
	require.Contains(t, out, "two")
}

// TestOriginRemoveEvictsNamespaceNotFiles proves removal stops the documents
// being citable while leaving the canonical tree alone.
func TestOriginRemoveEvictsNamespaceNotFiles(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")
	ext := externalTree(t, "a.md", "# A\n\nbody\n")
	runCmd(t, "origin", "add", ext, "--prefix", "spec")

	out := runCmd(t, "origin", "remove", "spec")
	require.Contains(t, out, "1 documents evicted")

	db, err := sql.Open("sqlite", filepath.Join(".mnemos", "state", "index.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.Equal(t, 0, count(t, db, `SELECT COUNT(*) FROM documents`))
	require.Equal(t, 0, count(t, db, `SELECT COUNT(*) FROM origins`))

	require.FileExists(t, filepath.Join(ext, "a.md"), "the origin's files are never deleted")
}

// TestOriginMissingRootIsReported proves an unmounted root refuses the reindex
// and surfaces in the listing instead of silently emptying the namespace.
func TestOriginMissingRootIsReported(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")
	ext := externalTree(t, "a.md", "# A\n\nbody\n")
	runCmd(t, "origin", "add", ext, "--prefix", "spec")
	require.NoError(t, os.RemoveAll(ext))

	_, err := runCmdErr(t, "origin", "reindex", "spec")
	require.Error(t, err)

	require.Contains(t, runCmd(t, "origin", "list"), "MISSING")

	db, dbErr := sql.Open("sqlite", filepath.Join(".mnemos", "state", "index.db"))
	require.NoError(t, dbErr)
	t.Cleanup(func() { _ = db.Close() })
	require.Equal(t, 1, count(t, db, `SELECT COUNT(*) FROM documents`), "a missing root evicts nothing")
}

func TestOriginAddRejections(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")
	ext := externalTree(t, "a.md", "# A\n\nbody\n")

	t.Run("prefix is required", func(t *testing.T) {
		_, err := runCmdErr(t, "origin", "add", ext)
		require.Error(t, err)
	})

	t.Run("path must be a directory", func(t *testing.T) {
		_, err := runCmdErr(t, "origin", "add", filepath.Join(ext, "a.md"), "--prefix", "f")
		require.Error(t, err)
	})

	t.Run("duplicate prefix", func(t *testing.T) {
		runCmd(t, "origin", "add", ext, "--prefix", "spec")
		_, err := runCmdErr(t, "origin", "add", externalTree(t, "b.md", "# B\n\nb\n"), "--prefix", "spec")
		require.Error(t, err)
	})

	t.Run("unknown prefix on reindex and remove", func(t *testing.T) {
		_, err := runCmdErr(t, "origin", "reindex", "nope")
		require.Error(t, err)
		_, err = runCmdErr(t, "origin", "remove", "nope")
		require.Error(t, err)
	})
}

// TestWatchRefusesOrigin proves watch says so rather than appearing to work: it
// is single-root and its walk does not descend symlinked directories.
func TestWatchRefusesOrigin(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")
	ext := externalTree(t, "a.md", "# A\n\nbody\n")
	runCmd(t, "origin", "add", ext, "--prefix", "spec")

	_, err := runCmdErr(t, "watch", ext)
	require.Error(t, err)
	require.Contains(t, err.Error(), "origin")
}
