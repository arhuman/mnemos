package storage_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/model"
	"github.com/arhuman/mnemos/internal/storage"
)

func TestCollectionByURI(t *testing.T) {
	db := openMigrated(t)

	doc := model.Document{
		ID: "id1", URI: "perso/note.md", Collection: "perso", ContentHash: "h1",
		IndexedAt: "2024-01-01T00:00:00Z",
	}
	inTx(t, db, func(tx *sql.Tx) { require.NoError(t, storage.UpsertDocument(context.Background(), tx, doc)) })

	collection, ok, err := storage.CollectionByURI(context.Background(), db, "perso/note.md")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "perso", collection)
}

func TestCollectionByURIMissing(t *testing.T) {
	db := openMigrated(t)

	_, ok, err := storage.CollectionByURI(context.Background(), db, "nope.md")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestDeleteByURITxCommit(t *testing.T) {
	db := openMigrated(t)

	docs := []model.Document{
		{ID: "id1", URI: "a.md", Collection: "c", ContentHash: "h1", IndexedAt: "2024-01-01T00:00:00Z"},
		{ID: "id2", URI: "b.md", Collection: "c", ContentHash: "h2", IndexedAt: "2024-01-01T00:00:00Z"},
	}
	inTx(t, db, func(tx *sql.Tx) {
		for _, d := range docs {
			require.NoError(t, storage.UpsertDocument(context.Background(), tx, d))
		}
	})

	// Delete the whole batch in one transaction, including a uri that does not
	// exist (a no-op, no error) — this is the watcher's vanished-file reconcile.
	inTx(t, db, func(tx *sql.Tx) {
		require.NoError(t, storage.DeleteByURITx(context.Background(), tx, "a.md"))
		require.NoError(t, storage.DeleteByURITx(context.Background(), tx, "b.md"))
		require.NoError(t, storage.DeleteByURITx(context.Background(), tx, "ghost.md"))
	})

	for _, uri := range []string{"a.md", "b.md"} {
		_, ok, err := storage.CollectionByURI(context.Background(), db, uri)
		require.NoError(t, err)
		require.False(t, ok, "%s should be deleted after commit", uri)
	}
}

func TestDeleteByURITxRollback(t *testing.T) {
	db := openMigrated(t)

	doc := model.Document{
		ID: "id1", URI: "a.md", Collection: "c", ContentHash: "h1", IndexedAt: "2024-01-01T00:00:00Z",
	}
	inTx(t, db, func(tx *sql.Tx) { require.NoError(t, storage.UpsertDocument(context.Background(), tx, doc)) })

	// A delete that is rolled back must leave the document intact: the batch is
	// atomic, so a mid-reconcile failure never half-prunes the index.
	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	require.NoError(t, storage.DeleteByURITx(context.Background(), tx, "a.md"))
	require.NoError(t, tx.Rollback())

	_, ok, err := storage.CollectionByURI(context.Background(), db, "a.md")
	require.NoError(t, err)
	require.True(t, ok, "rolled-back delete must leave the document in place")
}

func TestCountInboundLinks(t *testing.T) {
	db := openMigrated(t)

	doc := model.Document{
		ID: "src", URI: "src.md", Collection: "c", ContentHash: "h", IndexedAt: "2024-01-01T00:00:00Z",
	}
	inTx(t, db, func(tx *sql.Tx) {
		require.NoError(t, storage.UpsertDocument(context.Background(), tx, doc))
		require.NoError(t, storage.ReplaceLinks(context.Background(), tx, "src", []model.Link{
			{SrcDoc: "src", DstDoc: "target.md"},
		}))
	})

	n, err := storage.CountInboundLinks(context.Background(), db, "target.md")
	require.NoError(t, err)
	require.Equal(t, 1, n)

	n, err = storage.CountInboundLinks(context.Background(), db, "unlinked.md")
	require.NoError(t, err)
	require.Zero(t, n)
}
