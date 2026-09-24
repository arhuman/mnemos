package mcp_test

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/chunk"
	"github.com/arhuman/mnemos/internal/ingest"
	"github.com/arhuman/mnemos/internal/storage"
)

// journalCorpus ingests one citable document and one journal entry matching the
// same term, so only the exclusion can tell them apart over the wire.
func journalCorpus(t *testing.T) *sql.DB {
	t.Helper()

	src := t.TempDir()
	mustWrite(t, src, "policy.md",
		"---\ntype: decision\n---\n\n# Policy\n\nquorum is three replicas.\n")
	mustWrite(t, src, "turn.md",
		"---\ntype: Journal\n---\n\n# Turn\n\nquorum came up in conversation.\n")

	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "journal.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, storage.Migrate(db))

	_, err = ingest.New(db, slog.New(slog.NewTextHandler(io.Discard, nil))).Run(
		context.Background(), ingest.Options{
			Root:       src,
			Collection: "kb",
			Rules:      ingest.Rules{Include: []string{"**/*.md"}},
			Chunking:   chunk.Config{TargetTokens: 700, OverlapTokens: 80},
		})
	require.NoError(t, err)

	return db
}

// TestSearchToolExcludesJournalByDefault is the agent-facing half of ADR-0011:
// an MCP caller that does not ask for the journal never receives one.
func TestSearchToolExcludesJournalByDefault(t *testing.T) {
	cs := connect(t, journalCorpus(t))

	var out struct {
		Results []searchHitJSON `json:"results"`
	}
	res := callTool(t, cs, "mnemos.search", map[string]any{"query": "quorum"}, &out)
	require.False(t, res.IsError)
	require.Len(t, out.Results, 1, "mnemos.search must not offer a journal entry unasked")
	require.Equal(t, "policy.md", out.Results[0].URI)
}

// TestSearchToolIncludeJournalOptIn covers the parameter: the entry is reachable
// when the caller explicitly asks for it.
func TestSearchToolIncludeJournalOptIn(t *testing.T) {
	cs := connect(t, journalCorpus(t))

	var out struct {
		Results []searchHitJSON `json:"results"`
	}
	res := callTool(t, cs, "mnemos.search",
		map[string]any{"query": "quorum", "include_journal": true}, &out)
	require.False(t, res.IsError)
	require.Len(t, out.Results, 2, "include_journal must surface the entry over the wire")
}
