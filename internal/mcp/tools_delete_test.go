package mcp_test

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/chunk"
	"github.com/arhuman/mnemos/internal/config"
	"github.com/arhuman/mnemos/internal/ingest"
)

// deleteConfig builds a write+delete-enabled config rooted at treeRoot.
func deleteConfig(treeRoot string) srvCfg {
	return srvCfg{
		cfg: &config.Config{
			MCP:      config.MCPConfig{AllowWrite: true, AllowDelete: true},
			Capture:  config.CaptureConfig{Dir: filepath.Join(treeRoot, ".mnemos", "capture")},
			Chunking: config.ChunkingConfig{TargetTokens: 700, OverlapTokens: 80},
		},
		treeRoot: treeRoot,
	}
}

// seedFile writes rel under treeRoot, ingests it, and returns the absolute path.
func seedFile(t *testing.T, db *sql.DB, treeRoot, rel, content, collection string) string {
	t.Helper()
	abs := filepath.Join(treeRoot, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
	require.NoError(t, os.WriteFile(abs, []byte(content), 0o644))

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	_, _, err := ingest.File(context.Background(), db, logger, abs, filepath.ToSlash(rel), collection, chunk.Config{TargetTokens: 700, OverlapTokens: 80})
	require.NoError(t, err)

	return abs
}

func docExists(t *testing.T, db *sql.DB, uri string) bool {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM documents WHERE uri = ?", uri).Scan(&n))

	return n > 0
}

func TestRememberCustomPath(t *testing.T) {
	db := emptyStore(t)
	root := t.TempDir()
	cs := connectWith(t, db, deleteConfig(root))

	var out struct {
		URI        string `json:"uri"`
		DocumentID string `json:"document_id"`
		Chunks     int    `json:"chunks"`
	}
	res := callTool(t, cs, "mnemos.remember", map[string]any{
		"type": "idea",
		"text": "Keep the rules engine pure with no I/O.",
		"path": "perso/note-x.md",
	}, &out)
	require.False(t, res.IsError)
	require.Equal(t, "perso/note-x.md", out.URI)
	require.NotEmpty(t, out.DocumentID)
	require.Positive(t, out.Chunks)

	require.FileExists(t, filepath.Join(root, "perso", "note-x.md"))
	require.True(t, docExists(t, db, "perso/note-x.md"))
}

func TestRememberCustomPathRejectsNonMarkdown(t *testing.T) {
	db := emptyStore(t)
	root := t.TempDir()
	cs := connectWith(t, db, deleteConfig(root))

	res := callTool(t, cs, "mnemos.remember", map[string]any{
		"type": "idea",
		"text": "body",
		"path": "perso/note.txt",
	}, nil)
	require.True(t, res.IsError)
}

func TestRememberCustomPathRejectsTraversal(t *testing.T) {
	db := emptyStore(t)
	root := t.TempDir()
	cs := connectWith(t, db, deleteConfig(root))

	res := callTool(t, cs, "mnemos.remember", map[string]any{
		"type": "idea",
		"text": "body",
		"path": "../escape.md",
	}, nil)
	require.True(t, res.IsError)
	require.NoFileExists(t, filepath.Join(filepath.Dir(root), "escape.md"))
}

func TestRememberCustomPathWriteFailsWhenParentIsFile(t *testing.T) {
	db := emptyStore(t)
	root := t.TempDir()
	// blocker.md is a regular file; using it as a parent directory must fail the
	// atomic write rather than corrupting anything.
	require.NoError(t, os.WriteFile(filepath.Join(root, "blocker.md"), []byte("x"), 0o644))

	cs := connectWith(t, db, deleteConfig(root))
	res := callTool(t, cs, "mnemos.remember", map[string]any{
		"type": "idea",
		"text": "body",
		"path": "blocker.md/child.md",
	}, nil)
	require.True(t, res.IsError)
	require.Zero(t, countDocuments(t, db))
}

func TestForgetRemovesFileAndIndex(t *testing.T) {
	db := emptyStore(t)
	root := t.TempDir()
	abs := seedFile(t, db, root, "tech/note.md", "# Note\n\nSome content.\n", "tech")
	require.True(t, docExists(t, db, "tech/note.md"))

	cs := connectWith(t, db, deleteConfig(root))
	var out struct {
		URI     string `json:"uri"`
		Deleted bool   `json:"deleted"`
	}
	res := callTool(t, cs, "mnemos.forget", map[string]any{"path": "tech/note.md"}, &out)
	require.False(t, res.IsError)
	require.Equal(t, "tech/note.md", out.URI)
	require.True(t, out.Deleted)

	require.NoFileExists(t, abs)
	require.False(t, docExists(t, db, "tech/note.md"))
}

func TestForgetIsIdempotent(t *testing.T) {
	db := emptyStore(t)
	root := t.TempDir()
	cs := connectWith(t, db, deleteConfig(root))

	var out struct {
		Deleted bool `json:"deleted"`
	}
	res := callTool(t, cs, "mnemos.forget", map[string]any{"path": "missing/none.md"}, &out)
	require.False(t, res.IsError, "forgetting a non-existent file must not error")
	require.False(t, out.Deleted)
}

func TestForgetRejectsTraversal(t *testing.T) {
	db := emptyStore(t)
	root := t.TempDir()
	cs := connectWith(t, db, deleteConfig(root))

	res := callTool(t, cs, "mnemos.forget", map[string]any{"path": "/etc/passwd"}, nil)
	require.True(t, res.IsError)
}

func TestMovePreservesCollectionAndReindexes(t *testing.T) {
	db := emptyStore(t)
	root := t.TempDir()
	seedFile(t, db, root, "perso/note.md", "# Note\n\nMovable content here.\n", "perso")

	cs := connectWith(t, db, deleteConfig(root))
	var out struct {
		From       string `json:"from"`
		To         string `json:"to"`
		DocumentID string `json:"document_id"`
	}
	res := callTool(t, cs, "mnemos.move", map[string]any{
		"from": "perso/note.md",
		"to":   "tech/note.md",
	}, &out)
	require.False(t, res.IsError)
	require.Equal(t, "perso/note.md", out.From)
	require.Equal(t, "tech/note.md", out.To)
	require.NotEmpty(t, out.DocumentID)

	require.NoFileExists(t, filepath.Join(root, "perso", "note.md"))
	require.FileExists(t, filepath.Join(root, "tech", "note.md"))
	require.False(t, docExists(t, db, "perso/note.md"))
	require.True(t, docExists(t, db, "tech/note.md"))

	// Collection is preserved across the move.
	var collection string
	require.NoError(t, db.QueryRowContext(context.Background(), "SELECT collection FROM documents WHERE uri = ?", "tech/note.md").Scan(&collection))
	require.Equal(t, "perso", collection)
}

func TestMoveDirectoryReindexesSubtree(t *testing.T) {
	db := emptyStore(t)
	root := t.TempDir()
	seedFile(t, db, root, "adr/one.md", "# One\n\nbody.\n", "arch")
	seedFile(t, db, root, "adr/sub/two.md", "# Two\n\nbody.\n", "arch")

	cs := connectWith(t, db, deleteConfig(root))
	var out struct {
		From  string `json:"from"`
		To    string `json:"to"`
		IsDir bool   `json:"is_dir"`
		Files int    `json:"files"`
	}
	res := callTool(t, cs, "mnemos.move", map[string]any{"from": "adr", "to": "archive"}, &out)
	require.False(t, res.IsError)
	require.True(t, out.IsDir)
	require.Equal(t, 2, out.Files)

	require.NoDirExists(t, filepath.Join(root, "adr"))
	require.FileExists(t, filepath.Join(root, "archive", "one.md"))
	require.FileExists(t, filepath.Join(root, "archive", "sub", "two.md"))
	require.False(t, docExists(t, db, "adr/one.md"))
	require.True(t, docExists(t, db, "archive/one.md"))
	require.True(t, docExists(t, db, "archive/sub/two.md"))
}

func TestRememberCustomPathRejectsExcluded(t *testing.T) {
	db := emptyStore(t)
	root := t.TempDir()
	cfg := deleteConfig(root)
	cfg.cfg.Security.Exclude = []string{"**/secrets/**"}
	cs := connectWith(t, db, cfg)

	res := callTool(t, cs, "mnemos.remember", map[string]any{
		"type": "idea",
		"text": "body",
		"path": "secrets/note.md",
	}, nil)
	require.True(t, res.IsError)
	require.NoFileExists(t, filepath.Join(root, "secrets", "note.md"))
}

func TestMoveSourceNotIndexedUsesDefaultCollection(t *testing.T) {
	db := emptyStore(t)
	root := t.TempDir()

	// File exists on disk but was never ingested: move still relocates and
	// indexes it, falling back to the "default" collection.
	abs := filepath.Join(root, "loose.md")
	require.NoError(t, os.WriteFile(abs, []byte("# Loose\n\nNot yet indexed.\n"), 0o644))

	cs := connectWith(t, db, deleteConfig(root))
	var out struct {
		DocumentID string `json:"document_id"`
	}
	res := callTool(t, cs, "mnemos.move", map[string]any{"from": "loose.md", "to": "kept.md"}, &out)
	require.False(t, res.IsError)
	require.NotEmpty(t, out.DocumentID)

	require.True(t, docExists(t, db, "kept.md"))
	var collection string
	require.NoError(t, db.QueryRowContext(context.Background(), "SELECT collection FROM documents WHERE uri = ?", "kept.md").Scan(&collection))
	require.Equal(t, "default", collection)
}

func TestMoveRejectsBadDestination(t *testing.T) {
	db := emptyStore(t)
	root := t.TempDir()
	seedFile(t, db, root, "a.md", "# A\n\nbody.\n", "c")

	cs := connectWith(t, db, deleteConfig(root))
	res := callTool(t, cs, "mnemos.move", map[string]any{"from": "a.md", "to": "../escape.md"}, nil)
	require.True(t, res.IsError)
	// Source untouched after a rejected destination.
	require.FileExists(t, filepath.Join(root, "a.md"))
}

func TestMoveWarnsOnInboundLinks(t *testing.T) {
	db := emptyStore(t)
	root := t.TempDir()

	// target.md is the move subject; linker.md has a markdown link to it, so the
	// move leaves a dangling inbound link (logged as a warning, accepted in V0).
	seedFile(t, db, root, "target.md", "# Target\n\nbody.\n", "c")
	seedFile(t, db, root, "linker.md", "# Linker\n\nSee [target](target.md).\n", "c")

	cs := connectWith(t, db, deleteConfig(root))
	var out struct {
		DanglingLinks int `json:"dangling_links"`
	}
	res := callTool(t, cs, "mnemos.move", map[string]any{"from": "target.md", "to": "moved.md"}, &out)
	require.False(t, res.IsError)
	require.True(t, docExists(t, db, "moved.md"))
	require.False(t, docExists(t, db, "target.md"))
	require.Equal(t, 1, out.DanglingLinks, "the inbound link from linker.md must be reported")
}

func TestForgetAndMoveAbsentWhenDeleteDisabled(t *testing.T) {
	db := emptyStore(t)
	// allow_write true, allow_delete false: remember is offered, forget/move are not.
	cs := connectWith(t, db, srvCfg{cfg: &config.Config{MCP: config.MCPConfig{AllowWrite: true}}, treeRoot: t.TempDir()})

	tools, err := cs.ListTools(context.Background(), &mcpsdk.ListToolsParams{})
	require.NoError(t, err)
	for _, tool := range tools.Tools {
		require.NotEqual(t, "mnemos.forget", tool.Name, "forget must not be advertised when allow_delete is false")
		require.NotEqual(t, "mnemos.move", tool.Name, "move must not be advertised when allow_delete is false")
	}
}

func TestForgetAndMovePresentWhenDeleteEnabled(t *testing.T) {
	db := emptyStore(t)
	cs := connectWith(t, db, deleteConfig(t.TempDir()))

	tools, err := cs.ListTools(context.Background(), &mcpsdk.ListToolsParams{})
	require.NoError(t, err)

	names := make(map[string]bool)
	for _, tool := range tools.Tools {
		names[tool.Name] = true
	}
	require.True(t, names["mnemos.forget"], "forget must be advertised when allow_delete is true")
	require.True(t, names["mnemos.move"], "move must be advertised when allow_delete is true")
}
