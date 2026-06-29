package mcp_test

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/chunk"
	"github.com/arhuman/mnemos/internal/config"
	"github.com/arhuman/mnemos/internal/ingest"
	"github.com/arhuman/mnemos/internal/storage"
)

type listEntry struct {
	URI        string `json:"uri"`
	Indexed    bool   `json:"indexed"`
	Collection string `json:"collection"`
	Type       string `json:"type"`
}

// seedListTree ingests two files under root and leaves one indexable file
// un-indexed, returning the db and root for a list-tool test.
func seedListTree(t *testing.T) (*sql.DB, string) {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, root, "adr/0001.md", "---\ntype: adr\n---\n# One\n\nbody\n")
	mustWrite(t, root, "readme.md", "# Readme\n\nbody\n")

	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "mnemos.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, storage.Migrate(db))

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	_, err = ingest.New(db, logger).Run(context.Background(), ingest.Options{
		Root: root, Collection: "demo",
		Rules:    ingest.Rules{Include: []string{"**/*.md", "**/*.txt"}},
		Chunking: chunk.Config{TargetTokens: 700, OverlapTokens: 80},
	})
	require.NoError(t, err)

	mustWrite(t, root, "draft.md", "# Draft\n\nnot ingested\n")

	return db, root
}

func TestListTool(t *testing.T) {
	db, root := seedListTree(t)
	cs := connectWith(t, db, srvCfg{
		cfg:      &config.Config{Indexing: config.IndexingConfig{Include: []string{"**/*.md", "**/*.txt"}}},
		treeRoot: root,
	})

	var out struct {
		Entries []listEntry `json:"entries"`
	}
	res := callTool(t, cs, "mnemos.list", make(map[string]any), &out)
	require.False(t, res.IsError)
	require.Len(t, out.Entries, 3) // adr/0001.md, readme.md, draft.md

	byURI := make(map[string]listEntry)
	for _, e := range out.Entries {
		byURI[e.URI] = e
	}
	require.True(t, byURI["adr/0001.md"].Indexed)
	require.Equal(t, "adr", byURI["adr/0001.md"].Type)
	require.Equal(t, "demo", byURI["adr/0001.md"].Collection)
	require.False(t, byURI["draft.md"].Indexed)
}

func TestListToolUnindexedAndPrefix(t *testing.T) {
	db, root := seedListTree(t)
	cs := connectWith(t, db, srvCfg{
		cfg:      &config.Config{Indexing: config.IndexingConfig{Include: []string{"**/*.md", "**/*.txt"}}},
		treeRoot: root,
	})

	var unindexed struct {
		Entries []listEntry `json:"entries"`
	}
	callTool(t, cs, "mnemos.list", map[string]any{"unindexed_only": true}, &unindexed)
	require.Len(t, unindexed.Entries, 1)
	require.Equal(t, "draft.md", unindexed.Entries[0].URI)

	var prefixed struct {
		Entries []listEntry `json:"entries"`
	}
	callTool(t, cs, "mnemos.list", map[string]any{"path": "adr/"}, &prefixed)
	require.Len(t, prefixed.Entries, 1)
	require.Equal(t, "adr/0001.md", prefixed.Entries[0].URI)
}

func TestListToolMutuallyExclusive(t *testing.T) {
	db, root := seedListTree(t)
	cs := connectWith(t, db, srvCfg{
		cfg:      &config.Config{Indexing: config.IndexingConfig{Include: []string{"**/*.md"}}},
		treeRoot: root,
	})

	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name:      "mnemos.list",
		Arguments: map[string]any{"indexed_only": true, "unindexed_only": true},
	})
	require.NoError(t, err)
	require.True(t, res.IsError)
}
