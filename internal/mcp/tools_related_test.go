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

type relatedNeighborJSON struct {
	URI        string `json:"uri"`
	Title      string `json:"title"`
	Collection string `json:"collection"`
	Direction  string `json:"direction"`
	Resolved   bool   `json:"resolved"`
}

// linkedCorpus ingests a two-document vault where a.md links to b.md and back, so
// the link table has one edge each way.
func linkedCorpus(t *testing.T) *sql.DB {
	t.Helper()
	src := t.TempDir()
	mustWrite(t, src, "a.md", "# A\n\nSee [B](b.md).\n")
	mustWrite(t, src, "b.md", "# B\n\nBack to [A](a.md).\n")

	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "mnemos.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, storage.Migrate(db))

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	_, err = ingest.New(db, logger).Run(context.Background(), ingest.Options{
		Root:       src,
		Collection: "demo",
		Rules:      ingest.Rules{Include: []string{"**/*.md"}},
		Chunking:   chunk.Config{TargetTokens: 50, OverlapTokens: 10},
	})
	require.NoError(t, err)

	return db
}

func neighborSet(ns []relatedNeighborJSON) map[string]bool {
	out := make(map[string]bool, len(ns))
	for _, n := range ns {
		out[n.URI] = true
	}

	return out
}

func TestRelatedTool(t *testing.T) {
	cs := connect(t, linkedCorpus(t))

	var out struct {
		URI      string                `json:"uri"`
		Outbound []relatedNeighborJSON `json:"outbound"`
		Inbound  []relatedNeighborJSON `json:"inbound"`
	}
	callTool(t, cs, "mnemos.related", map[string]any{"uri": "a.md"}, &out)

	require.Equal(t, "a.md", out.URI)
	require.True(t, neighborSet(out.Outbound)["b.md"], "a.md links out to b.md")
	require.True(t, neighborSet(out.Inbound)["b.md"], "b.md links back to a.md")

	// Direction filter: inbound only.
	var inbound struct {
		Outbound []relatedNeighborJSON `json:"outbound"`
		Inbound  []relatedNeighborJSON `json:"inbound"`
	}
	callTool(t, cs, "mnemos.related", map[string]any{"uri": "a.md", "direction": "inbound"}, &inbound)
	require.Empty(t, inbound.Outbound)
	require.True(t, neighborSet(inbound.Inbound)["b.md"])
}

func TestReadToolFollowLinks(t *testing.T) {
	cs := connect(t, linkedCorpus(t))

	var plain struct {
		URI       string                `json:"uri"`
		Neighbors []relatedNeighborJSON `json:"neighbors"`
	}
	callTool(t, cs, "mnemos.read", map[string]any{"uri": "a.md"}, &plain)
	require.Empty(t, plain.Neighbors, "read without follow_links omits neighbors")

	var linked struct {
		URI       string                `json:"uri"`
		Neighbors []relatedNeighborJSON `json:"neighbors"`
	}
	callTool(t, cs, "mnemos.read", map[string]any{"uri": "a.md", "follow_links": true}, &linked)
	require.True(t, neighborSet(linked.Neighbors)["b.md"], "follow_links attaches b.md")
}

type contextBlockNeighborsJSON struct {
	Source    string                `json:"source"`
	Neighbors []relatedNeighborJSON `json:"neighbors"`
}

type contextNeighborsJSON struct {
	Context []contextBlockNeighborsJSON `json:"context"`
}

func TestContextToolFollowLinks(t *testing.T) {
	cs := connect(t, linkedCorpus(t))

	var out contextNeighborsJSON
	callTool(t, cs, "mnemos.context", map[string]any{"query": "See", "follow_links": true}, &out)

	require.NotEmpty(t, out.Context)
	require.Contains(t, out.Context[0].Source, "a.md:")
	require.True(t, neighborSet(out.Context[0].Neighbors)["b.md"])
}
