package mcp_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/config"
)

// okfyConfig builds a write-enabled (but delete-disabled) config rooted at
// treeRoot: enough for mnemos.okfy, which only creates and indexes.
func okfyConfig(treeRoot string) srvCfg {
	return srvCfg{
		cfg: &config.Config{
			MCP:      config.MCPConfig{AllowWrite: true},
			Chunking: config.ChunkingConfig{TargetTokens: 700, OverlapTokens: 80},
		},
		treeRoot: treeRoot,
	}
}

func TestOkfyAbsentWhenWriteDisabled(t *testing.T) {
	db := emptyStore(t)
	cs := connectWith(t, db, srvCfg{})

	tools, err := cs.ListTools(context.Background(), &mcpsdk.ListToolsParams{})
	require.NoError(t, err)
	for _, tool := range tools.Tools {
		require.NotEqual(t, "mnemos.okfy", tool.Name,
			"mnemos.okfy must not be advertised when allow_write is false")
	}
}

func TestOkfyPresentWhenWriteEnabled(t *testing.T) {
	db := emptyStore(t)
	cs := connectWith(t, db, okfyConfig(t.TempDir()))

	tools, err := cs.ListTools(context.Background(), &mcpsdk.ListToolsParams{})
	require.NoError(t, err)

	var found bool
	for _, tool := range tools.Tools {
		if tool.Name == "mnemos.okfy" {
			found = true
		}
	}
	require.True(t, found, "mnemos.okfy must be advertised when allow_write is true")
}

func TestOkfyConvertsTxtAndIndexes(t *testing.T) {
	db := emptyStore(t)
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "note.txt"),
		[]byte("Plain text body without a heading.\n"), 0o644))

	cs := connectWith(t, db, okfyConfig(root))
	var out struct {
		URI        string `json:"uri"`
		DocumentID string `json:"document_id"`
		Chunks     int    `json:"chunks"`
	}
	res := callTool(t, cs, "mnemos.okfy", map[string]any{"source": "note.txt"}, &out)
	require.False(t, res.IsError)
	require.Equal(t, "note.md", out.URI)
	require.NotEmpty(t, out.DocumentID)
	require.Positive(t, out.Chunks)

	// OKF output written, source kept, document indexed.
	require.FileExists(t, filepath.Join(root, "note.md"))
	require.FileExists(t, filepath.Join(root, "note.txt"))
	require.True(t, docExists(t, db, "note.md"))
}

func TestOkfyRejectsTraversalOut(t *testing.T) {
	db := emptyStore(t)
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "source.txt"), []byte("Body.\n"), 0o644))

	cs := connectWith(t, db, okfyConfig(root))
	res := callTool(t, cs, "mnemos.okfy", map[string]any{
		"source": "source.txt",
		"out":    "../escape.md",
	}, nil)
	require.True(t, res.IsError)
	require.NoFileExists(t, filepath.Join(filepath.Dir(root), "escape.md"))
}

func TestOkfyRejectsNonTextSource(t *testing.T) {
	db := emptyStore(t)
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "data.json"), []byte("{}\n"), 0o644))

	cs := connectWith(t, db, okfyConfig(root))
	res := callTool(t, cs, "mnemos.okfy", map[string]any{"source": "data.json"}, nil)
	require.True(t, res.IsError)
	require.Zero(t, countDocuments(t, db))
}

func TestOkfyRejectsSecretInSource(t *testing.T) {
	db := emptyStore(t)
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "leak.txt"),
		[]byte("deploy key AKIAQYLPMN5HXYZ12345 do not commit\n"), 0o644))

	cs := connectWith(t, db, okfyConfig(root))
	res := callTool(t, cs, "mnemos.okfy", map[string]any{"source": "leak.txt"}, nil)
	require.True(t, res.IsError, "a secret in the source must be rejected")

	// Nothing indexed and no OKF output written.
	require.Zero(t, countDocuments(t, db))
	require.NoFileExists(t, filepath.Join(root, "leak.md"))
}

func TestOkfyMdSourceWithoutOutErrors(t *testing.T) {
	db := emptyStore(t)
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "doc.md"), []byte("# Doc\n\nBody.\n"), 0o644))

	cs := connectWith(t, db, okfyConfig(root))
	res := callTool(t, cs, "mnemos.okfy", map[string]any{"source": "doc.md"}, nil)
	require.True(t, res.IsError, "an .md source without --out resolves to the source and must be rejected")

	// With out it succeeds and keeps the source.
	var ok struct {
		URI string `json:"uri"`
	}
	res = callTool(t, cs, "mnemos.okfy", map[string]any{
		"source": "doc.md",
		"out":    "doc.okf.md",
	}, &ok)
	require.False(t, res.IsError)
	require.Equal(t, "doc.okf.md", ok.URI)
	require.FileExists(t, filepath.Join(root, "doc.md"))
	require.FileExists(t, filepath.Join(root, "doc.okf.md"))
}
