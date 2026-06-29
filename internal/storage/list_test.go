package storage_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/model"
	"github.com/arhuman/mnemos/internal/storage"
)

func TestListDocuments(t *testing.T) {
	db := openMigrated(t)
	seedDoc(t, db, model.Document{
		ID: "d1", URI: "adr/0001-x.md", Collection: "arch", ContentHash: "h1",
		Title: "ADR 1", SizeBytes: 10, ModifiedAt: "2024-01-01T00:00:00Z",
		IndexedAt: "2024-01-01T00:00:01Z", FrontmatterJSON: `{"type":"adr"}`,
	}, nil, nil)
	seedDoc(t, db, model.Document{
		ID: "d2", URI: "adr/0002-y.md", Collection: "arch", ContentHash: "h2",
		Title: "ADR 2", SizeBytes: 20, IndexedAt: "2024-01-02T00:00:01Z",
	}, nil, nil)
	seedDoc(t, db, model.Document{
		ID: "d3", URI: "notes/idea.txt", Collection: "notes", ContentHash: "h3",
		SizeBytes: 30, IndexedAt: "2024-01-03T00:00:01Z",
	}, nil, nil)

	t.Run("all, ordered by uri", func(t *testing.T) {
		rows, err := storage.ListDocuments(context.Background(), db, storage.ListFilter{})
		require.NoError(t, err)
		require.Len(t, rows, 3)
		require.Equal(t, []string{"adr/0001-x.md", "adr/0002-y.md", "notes/idea.txt"},
			[]string{rows[0].URI, rows[1].URI, rows[2].URI})
		require.Equal(t, "arch", rows[0].Collection)
		require.Equal(t, int64(10), rows[0].SizeBytes)
		require.Equal(t, `{"type":"adr"}`, rows[0].FrontmatterJSON)
	})

	t.Run("by collection", func(t *testing.T) {
		rows, err := storage.ListDocuments(context.Background(), db, storage.ListFilter{Collection: "notes"})
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Equal(t, "notes/idea.txt", rows[0].URI)
	})

	t.Run("by path prefix", func(t *testing.T) {
		rows, err := storage.ListDocuments(context.Background(), db, storage.ListFilter{PathPrefix: "adr/"})
		require.NoError(t, err)
		require.Len(t, rows, 2)
	})

	t.Run("by file type", func(t *testing.T) {
		rows, err := storage.ListDocuments(context.Background(), db, storage.ListFilter{FileType: "txt"})
		require.NoError(t, err)
		require.Len(t, rows, 1)
		require.Equal(t, "notes/idea.txt", rows[0].URI)
	})

	t.Run("limit", func(t *testing.T) {
		rows, err := storage.ListDocuments(context.Background(), db, storage.ListFilter{Limit: 2})
		require.NoError(t, err)
		require.Len(t, rows, 2)
	})

	t.Run("prefix treats wildcards literally", func(t *testing.T) {
		// "_" is a LIKE wildcard; the ESCAPE clause must match it literally, so a
		// prefix containing it returns nothing rather than matching every uri.
		rows, err := storage.ListDocuments(context.Background(), db, storage.ListFilter{PathPrefix: "ad_/"})
		require.NoError(t, err)
		require.Empty(t, rows)
	})
}
