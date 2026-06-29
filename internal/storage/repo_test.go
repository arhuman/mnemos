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

// openMigrated opens a fresh migrated temp database.
func openMigrated(t *testing.T) *sql.DB { return testutil.NewDB(t) }

// inTx runs fn inside a committed transaction.
func inTx(t *testing.T, db *sql.DB, fn func(tx *sql.Tx)) {
	t.Helper()
	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(t, err)
	fn(tx)
	require.NoError(t, tx.Commit())
}

func TestUpsertDocumentByURI(t *testing.T) {
	db := openMigrated(t)

	doc := model.Document{
		ID: "id1", URI: "a.md", Collection: "c", ContentHash: "h1",
		Title: "T", IndexedAt: "2024-01-01T00:00:00Z",
	}
	inTx(t, db, func(tx *sql.Tx) { require.NoError(t, storage.UpsertDocument(context.Background(), tx, doc)) })

	hash, ok, err := storage.DocumentHashByURI(context.Background(), db, "a.md")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "h1", hash)

	// Re-upsert with the same id+uri updates the hash, keeps the row count at 1.
	doc.ContentHash = "h2"
	inTx(t, db, func(tx *sql.Tx) { require.NoError(t, storage.UpsertDocument(context.Background(), tx, doc)) })

	var n int
	require.NoError(t, db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM documents`).Scan(&n))
	require.Equal(t, 1, n)
	hash, _, _ = storage.DocumentHashByURI(context.Background(), db, "a.md")
	require.Equal(t, "h2", hash)
}

func TestDocumentHashMissing(t *testing.T) {
	db := openMigrated(t)
	_, ok, err := storage.DocumentHashByURI(context.Background(), db, "nope.md")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestReplaceChunksCascadesFTS(t *testing.T) {
	db := openMigrated(t)
	doc := model.Document{ID: "d", URI: "a.md", Collection: "c", ContentHash: "h", IndexedAt: "t"}
	inTx(t, db, func(tx *sql.Tx) {
		require.NoError(t, storage.UpsertDocument(context.Background(), tx, doc))
		require.NoError(t, storage.ReplaceChunks(context.Background(), tx, "d", []model.Chunk{
			{ID: "c0", DocumentID: "d", Ordinal: 0, Content: "alpha", Tags: "x", DocType: "note"},
			{ID: "c1", DocumentID: "d", Ordinal: 1, Content: "beta"},
		}))
	})

	var ftsN int
	require.NoError(t, db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM chunks_fts`).Scan(&ftsN))
	require.Equal(t, 2, ftsN)

	// Replacing with a single chunk must leave FTS consistent (old rows gone).
	inTx(t, db, func(tx *sql.Tx) {
		require.NoError(t, storage.ReplaceChunks(context.Background(), tx, "d", []model.Chunk{
			{ID: "c0", DocumentID: "d", Ordinal: 0, Content: "gamma"},
		}))
	})
	require.NoError(t, db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM chunks_fts`).Scan(&ftsN))
	require.Equal(t, 1, ftsN)

	var hits int
	require.NoError(t, db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM chunks_fts WHERE chunks_fts MATCH 'gamma'`).Scan(&hits))
	require.Equal(t, 1, hits)
}

func TestReplaceLinks(t *testing.T) {
	db := openMigrated(t)
	doc := model.Document{ID: "d", URI: "a.md", Collection: "c", ContentHash: "h", IndexedAt: "t"}
	inTx(t, db, func(tx *sql.Tx) {
		require.NoError(t, storage.UpsertDocument(context.Background(), tx, doc))
		require.NoError(t, storage.ReplaceLinks(context.Background(), tx, "d", []model.Link{
			{SrcDoc: "d", DstDoc: "b.md"},
			{SrcDoc: "d", DstDoc: "c.md"},
		}))
	})
	var n int
	require.NoError(t, db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM links WHERE src_doc = 'd'`).Scan(&n))
	require.Equal(t, 2, n)

	inTx(t, db, func(tx *sql.Tx) {
		require.NoError(t, storage.ReplaceLinks(context.Background(), tx, "d", nil))
	})
	require.NoError(t, db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM links WHERE src_doc = 'd'`).Scan(&n))
	require.Equal(t, 0, n)
}

func TestAppendEvent(t *testing.T) {
	db := openMigrated(t)
	inTx(t, db, func(tx *sql.Tx) {
		require.NoError(t, storage.AppendEvent(context.Background(), tx, "e1", "", "ingested", `{"uri":"a.md"}`, "t"))
	})
	var typ string
	require.NoError(t, db.QueryRowContext(context.Background(), `SELECT type FROM events WHERE id = 'e1'`).Scan(&typ))
	require.Equal(t, "ingested", typ)
}

// TestAppendEventCascadesOnDocumentDelete is the behavioral contract of
// migration 0003: deleting a document removes the events tied to it via the
// document_id FK (ON DELETE CASCADE), while document-less events (NULL
// document_id) survive.
func TestAppendEventCascadesOnDocumentDelete(t *testing.T) {
	db := openMigrated(t)
	doc := model.Document{ID: "d", URI: "a.md", Collection: "c", ContentHash: "h", IndexedAt: "t"}
	inTx(t, db, func(tx *sql.Tx) {
		require.NoError(t, storage.UpsertDocument(context.Background(), tx, doc))
		// e1 is tied to the document; e2 is document-less (NULL document_id).
		require.NoError(t, storage.AppendEvent(context.Background(), tx, "e1", "d", "ingested", `{"uri":"a.md"}`, "t"))
		require.NoError(t, storage.AppendEvent(context.Background(), tx, "e2", "", "system", `{}`, "t"))
	})

	var total int
	require.NoError(t, db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM events`).Scan(&total))
	require.Equal(t, 2, total)

	// Deleting the document cascades its tied event away...
	require.NoError(t, storage.DeleteByURI(context.Background(), db, "a.md"))

	var tied int
	require.NoError(t, db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM events WHERE id = 'e1'`).Scan(&tied))
	require.Equal(t, 0, tied)

	// ...but the document-less event remains untouched.
	var orphan int
	require.NoError(t, db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM events WHERE id = 'e2'`).Scan(&orphan))
	require.Equal(t, 1, orphan)
}
