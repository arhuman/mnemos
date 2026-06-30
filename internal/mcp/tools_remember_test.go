package mcp_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/config"
	"github.com/arhuman/mnemos/internal/storage"
)

// emptyStore returns an open, migrated database with no documents.
func emptyStore(t *testing.T) *sql.DB {
	t.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "mnemos.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, storage.Migrate(db))

	return db
}

// writeConfig builds an enabled write config. Capture is the fixed kb/capture
// subdir of the tree root, derived from treeRoot (not configured).
func writeConfig(t *testing.T) srvCfg {
	t.Helper()

	root := t.TempDir()

	return srvCfg{
		cfg: &config.Config{
			MCP:      config.MCPConfig{AllowWrite: true},
			Chunking: config.ChunkingConfig{TargetTokens: 700, OverlapTokens: 80},
		},
		treeRoot: root,
	}
}

// captureDirFor returns the capture directory for a test config's tree root.
func captureDirFor(c srvCfg) string {
	return filepath.Join(c.treeRoot, "capture")
}

func countDocuments(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM documents").Scan(&n))

	return n
}

func TestRememberCapturesAndIndexes(t *testing.T) {
	db := emptyStore(t)
	cs := connectWith(t, db, writeConfig(t))

	var out struct {
		URI        string `json:"uri"`
		DocumentID string `json:"document_id"`
		Chunks     int    `json:"chunks"`
		Type       string `json:"type"`
	}
	res := callTool(t, cs, "mnemos.remember", map[string]any{
		"type":       "idea",
		"text":       "The rules engine must stay pure with no I/O side effects.",
		"collection": "asheeve",
		"tags":       []string{"architecture", "rules"},
	}, &out)
	require.False(t, res.IsError)
	require.NotEmpty(t, out.URI)
	require.NotEmpty(t, out.DocumentID)
	require.Positive(t, out.Chunks)
	require.Equal(t, "idea", out.Type)

	// A subsequent mnemos.search finds the captured note.
	type searchHit struct {
		URI string `json:"uri"`
	}
	var search struct {
		Results []searchHit `json:"results"`
	}
	sres := callTool(t, cs, "mnemos.search", map[string]any{
		"query":      "rules engine pure no side effects",
		"collection": "asheeve",
	}, &search)
	require.False(t, sres.IsError)
	require.NotEmpty(t, search.Results)
	require.Equal(t, out.URI, search.Results[0].URI)
}

func TestRememberDeferToWatcherWritesButSkipsIngest(t *testing.T) {
	db := emptyStore(t)
	cfg := writeConfig(t)
	cfg.cfg.Capture.DeferToWatcher = true
	cs := connectWith(t, db, cfg)

	var out struct {
		URI        string `json:"uri"`
		DocumentID string `json:"document_id"`
		Chunks     int    `json:"chunks"`
		Type       string `json:"type"`
		Deferred   bool   `json:"deferred"`
	}
	res := callTool(t, cs, "mnemos.remember", map[string]any{
		"type": "idea",
		"text": "Defer ingestion to the watcher in strict mode.",
	}, &out)
	require.False(t, res.IsError)
	require.NotEmpty(t, out.URI)
	require.True(t, out.Deferred)
	require.Empty(t, out.DocumentID)
	require.Zero(t, out.Chunks)

	// The concept file was written (alongside its directory log.md), but nothing
	// was ingested: the watcher owns that.
	require.Zero(t, countDocuments(t, db))
	entries, err := os.ReadDir(captureDirFor(cfg))
	require.NoError(t, err)
	var conceptCount int
	var sawLog bool
	for _, e := range entries {
		if e.Name() == "log.md" {
			sawLog = true

			continue
		}
		conceptCount++
	}
	require.Equal(t, 1, conceptCount, "OKF concept file must be written even when deferred")
	require.True(t, sawLog, "a log.md changelog entry must be recorded for the captured concept")
}

func TestRememberRejectsSecretAndWritesNothing(t *testing.T) {
	db := emptyStore(t)
	cfg := writeConfig(t)
	cs := connectWith(t, db, cfg)

	require.Zero(t, countDocuments(t, db))

	res := callTool(t, cs, "mnemos.remember", map[string]any{
		"type": "document",
		"text": "deploy key AKIAQYLPMN5HXYZ12345 do not commit",
	}, nil)
	require.True(t, res.IsError, "a secret must be rejected as a tool error")

	// No document was indexed and no file was written. A missing capture dir
	// (never created) is treated as empty.
	require.Zero(t, countDocuments(t, db))
	entries, _ := os.ReadDir(captureDirFor(cfg))
	require.Empty(t, entries, "capture dir must contain no file after a rejected secret")
}

func TestRememberRejectsOversizedText(t *testing.T) {
	db := emptyStore(t)
	cfg := writeConfig(t)
	cs := connectWith(t, db, cfg)

	require.Zero(t, countDocuments(t, db))

	// 64 KiB + 1 byte: just over the cap.
	oversized := strings.Repeat("a", 64*1024+1)
	res := callTool(t, cs, "mnemos.remember", map[string]any{
		"type": "document",
		"text": oversized,
	}, nil)
	require.True(t, res.IsError, "oversized text must be rejected as a tool error")

	// Nothing was indexed and nothing was written.
	require.Zero(t, countDocuments(t, db))
	entries, _ := os.ReadDir(captureDirFor(cfg))
	require.Empty(t, entries, "capture dir must contain no file after rejected oversized text")
}

func TestRememberAcceptsFreeFormType(t *testing.T) {
	db := emptyStore(t)
	cs := connectWith(t, db, writeConfig(t))

	// OKF types are free-form: remember must accept any non-empty type, not just
	// idea/document, so concepts like Task or Playbook can be captured.
	for _, typ := range []string{"memo", "Task", "Playbook"} {
		res := callTool(t, cs, "mnemos.remember", map[string]any{
			"type": typ,
			"text": "some valid text content here for " + typ,
		}, nil)
		require.False(t, res.IsError, "free-form type %q must be accepted", typ)
	}

	// An empty/whitespace-only type is still rejected.
	res := callTool(t, cs, "mnemos.remember", map[string]any{
		"type": "   ",
		"text": "some valid text content here",
	}, nil)
	require.True(t, res.IsError, "an empty type must be rejected")
}

func TestRememberAbsentWhenWriteDisabled(t *testing.T) {
	db := emptyStore(t)
	cs := connectWith(t, db, srvCfg{})

	tools, err := cs.ListTools(context.Background(), &mcpsdk.ListToolsParams{})
	require.NoError(t, err)

	for _, tool := range tools.Tools {
		require.NotEqual(t, "mnemos.remember", tool.Name,
			"mnemos.remember must not be advertised when allow_write is false")
	}
}
