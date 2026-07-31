package storage_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/model"
	"github.com/arhuman/mnemos/internal/storage"
)

// benchNeighborsDB builds a corpus of `docs` documents plus one "hub" document
// with `fan` outbound links and `fan` inbound links, so the neighbor queries run
// against a realistic mix rather than an empty table. The result is the marginal
// cost read/context neighborhood injection would add per call. Returns the db and
// the hub uri.
func benchNeighborsDB(b *testing.B, docs, fan int) (*sql.DB, string) {
	b.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(b.TempDir(), "bench.db"))
	require.NoError(b, err)
	b.Cleanup(func() { _ = db.Close() })
	require.NoError(b, storage.Migrate(db))

	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(b, err)

	require.NoError(b, storage.UpsertDocument(ctx, tx, doc("hub", "hub.md", "Hub")))
	for i := range docs {
		require.NoError(b, storage.UpsertDocument(ctx, tx, doc(fmt.Sprintf("d%d", i), fmt.Sprintf("docs/d%d.md", i), fmt.Sprintf("Doc %d", i))))
	}

	// Hub links out to the first `fan` docs.
	outs := make([]model.Link, 0, fan)
	for i := range fan {
		outs = append(outs, model.Link{SrcDoc: "hub", DstDoc: fmt.Sprintf("docs/d%d.md", i)})
	}
	require.NoError(b, storage.ReplaceLinks(ctx, tx, "hub", outs))

	// The next `fan` docs each link back to the hub (inbound edges).
	for i := fan; i < 2*fan && i < docs; i++ {
		id := fmt.Sprintf("d%d", i)
		require.NoError(b, storage.ReplaceLinks(ctx, tx, id, []model.Link{{SrcDoc: id, DstDoc: "hub.md"}}))
	}
	require.NoError(b, tx.Commit())

	return db, "hub.md"
}

func doc(id, uri, title string) model.Document {
	return model.Document{
		ID:          id,
		URI:         uri,
		Collection:  "demo",
		ContentHash: "hash-" + id,
		Title:       title,
		IndexedAt:   "2026-01-01T00:00:00Z",
	}
}

func BenchmarkOutboundNeighbors(b *testing.B) {
	db, uri := benchNeighborsDB(b, 500, 25)
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := storage.OutboundNeighbors(ctx, db, uri); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkInboundNeighbors(b *testing.B) {
	db, uri := benchNeighborsDB(b, 500, 25)
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := storage.InboundNeighbors(ctx, db, uri); err != nil {
			b.Fatal(err)
		}
	}
}
