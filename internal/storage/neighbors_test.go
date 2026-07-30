package storage_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/model"
	"github.com/arhuman/mnemos/internal/storage"
	"github.com/arhuman/mnemos/internal/testutil"
)

func insertDoc(t *testing.T, db *sql.DB, id, uri, collection, title string) {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	require.NoError(t, storage.UpsertDocument(context.Background(), tx, model.Document{
		ID:          id,
		URI:         uri,
		Collection:  collection,
		ContentHash: "hash-" + id,
		Title:       title,
		IndexedAt:   "2026-01-01T00:00:00Z",
	}))
	require.NoError(t, tx.Commit())
}

func insertLinks(t *testing.T, db *sql.DB, srcID string, dstURIs ...string) {
	t.Helper()
	links := make([]model.Link, 0, len(dstURIs))
	for _, d := range dstURIs {
		links = append(links, model.Link{SrcDoc: srcID, DstDoc: d})
	}
	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	require.NoError(t, storage.ReplaceLinks(context.Background(), tx, srcID, links))
	require.NoError(t, tx.Commit())
}

func TestOutboundAndInboundNeighbors(t *testing.T) {
	ctx := context.Background()
	db := testutil.NewDB(t)

	// A -> B (resolved), A -> ghost.md (dangling); B -> A (resolved backlink).
	insertDoc(t, db, "id-a", "a.md", "default", "Doc A")
	insertDoc(t, db, "id-b", "b.md", "default", "Doc B")
	insertLinks(t, db, "id-a", "b.md", "ghost.md")
	insertLinks(t, db, "id-b", "a.md")

	t.Run("outbound resolves targets and flags dangling", func(t *testing.T) {
		out, err := storage.OutboundNeighbors(ctx, db, "a.md")
		require.NoError(t, err)
		require.Len(t, out, 2)

		// Ordered by dst_doc: "b.md" before "ghost.md".
		require.Equal(t, "b.md", out[0].URI)
		require.True(t, out[0].Resolved)
		require.Equal(t, "Doc B", out[0].Title)
		require.Equal(t, "default", out[0].Collection)
		require.Equal(t, storage.DirOutbound, out[0].Direction)

		require.Equal(t, "ghost.md", out[1].URI)
		require.False(t, out[1].Resolved)
		require.Empty(t, out[1].Title)
		require.Empty(t, out[1].Collection)
	})

	t.Run("inbound returns backlinks, always resolved", func(t *testing.T) {
		in, err := storage.InboundNeighbors(ctx, db, "a.md")
		require.NoError(t, err)
		require.Len(t, in, 1)
		require.Equal(t, "b.md", in[0].URI)
		require.True(t, in[0].Resolved)
		require.Equal(t, "Doc B", in[0].Title)
		require.Equal(t, storage.DirInbound, in[0].Direction)
	})

	t.Run("dangling target has no inbound resolution but is reachable inbound by uri", func(t *testing.T) {
		in, err := storage.InboundNeighbors(ctx, db, "ghost.md")
		require.NoError(t, err)
		require.Len(t, in, 1)
		require.Equal(t, "a.md", in[0].URI)
	})

	t.Run("unknown source has no outbound edges", func(t *testing.T) {
		out, err := storage.OutboundNeighbors(ctx, db, "nope.md")
		require.NoError(t, err)
		require.Empty(t, out)
	})
}
