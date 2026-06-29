package storage_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/model"
	"github.com/arhuman/mnemos/internal/storage"
)

// seedDoc upserts a document and replaces its chunks/links in one transaction,
// returning nothing — callers assert via the read accessors under test.
func seedDoc(t *testing.T, db *sql.DB, doc model.Document, chunks []model.Chunk, links []model.Link) {
	t.Helper()
	inTx(t, db, func(tx *sql.Tx) {
		require.NoError(t, storage.UpsertDocument(context.Background(), tx, doc))
		require.NoError(t, storage.ReplaceChunks(context.Background(), tx, doc.ID, chunks))
		require.NoError(t, storage.ReplaceLinks(context.Background(), tx, doc.ID, links))
	})
}

func TestGetDocumentByURIAndID(t *testing.T) {
	db := openMigrated(t)
	doc := model.Document{
		ID: "d1", URI: "a.md", Collection: "c", ContentHash: "h",
		Title: "Title A", MimeType: "text/markdown", SizeBytes: 42,
		ModifiedAt: "2024-01-02T03:04:05Z", IndexedAt: "2024-01-02T03:04:06Z",
		FrontmatterJSON: `{"k":"v"}`,
	}
	seedDoc(t, db, doc, nil, nil)

	t.Run("by uri present", func(t *testing.T) {
		got, err := storage.GetDocumentByURI(context.Background(), db, "a.md")
		require.NoError(t, err)
		require.NotNil(t, got)
		require.Equal(t, doc, *got)
	})

	t.Run("by id present", func(t *testing.T) {
		got, err := storage.GetDocumentByID(context.Background(), db, "d1")
		require.NoError(t, err)
		require.NotNil(t, got)
		require.Equal(t, "a.md", got.URI)
	})

	t.Run("by uri missing returns nil,nil", func(t *testing.T) {
		got, err := storage.GetDocumentByURI(context.Background(), db, "ghost.md")
		require.NoError(t, err)
		require.Nil(t, got)
	})

	t.Run("by id missing returns nil,nil", func(t *testing.T) {
		got, err := storage.GetDocumentByID(context.Background(), db, "nope")
		require.NoError(t, err)
		require.Nil(t, got)
	})
}

// TestGetDocumentNullableFieldsRoundTrip checks that empty optional columns are
// stored as SQL NULL and read back as "" (the scan helper maps NULL->"").
func TestGetDocumentNullableFieldsRoundTrip(t *testing.T) {
	db := openMigrated(t)
	doc := model.Document{ID: "d", URI: "min.md", Collection: "c", ContentHash: "h", IndexedAt: "t"}
	seedDoc(t, db, doc, nil, nil)

	got, err := storage.GetDocumentByURI(context.Background(), db, "min.md")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Empty(t, got.Title)
	require.Empty(t, got.MimeType)
	require.Empty(t, got.ModifiedAt)
	require.Empty(t, got.FrontmatterJSON)

	// The optional columns must actually be NULL, not "".
	var nulls int
	require.NoError(t, db.QueryRowContext(context.Background(), `
		SELECT COUNT(*) FROM documents
		WHERE uri = 'min.md' AND title IS NULL AND mime_type IS NULL
		  AND modified_at IS NULL AND frontmatter_json IS NULL
	`).Scan(&nulls))
	require.Equal(t, 1, nulls)
}

func TestGetChunkByID(t *testing.T) {
	db := openMigrated(t)
	doc := model.Document{ID: "d", URI: "a.md", Collection: "c", ContentHash: "h", IndexedAt: "t"}
	chunks := []model.Chunk{
		{ID: "c0", DocumentID: "d", Ordinal: 0, HeadingPath: "A > B", Content: "alpha",
			Tags: "x", DocType: "note", TokenCount: 1, StartLine: 1, EndLine: 2, MetadataJSON: `{"r":"1"}`},
	}
	seedDoc(t, db, doc, chunks, nil)

	t.Run("present", func(t *testing.T) {
		got, err := storage.GetChunkByID(context.Background(), db, "c0")
		require.NoError(t, err)
		require.NotNil(t, got)
		require.Equal(t, chunks[0], *got)
	})

	t.Run("missing returns nil,nil", func(t *testing.T) {
		got, err := storage.GetChunkByID(context.Background(), db, "absent")
		require.NoError(t, err)
		require.Nil(t, got)
	})
}

func TestGetChunkContentsByIDs(t *testing.T) {
	db := openMigrated(t)
	doc := model.Document{ID: "d", URI: "a.md", Collection: "c", ContentHash: "h", IndexedAt: "t"}
	seedDoc(t, db, doc, []model.Chunk{
		{ID: "c0", DocumentID: "d", Ordinal: 0, Content: "alpha"},
		{ID: "c1", DocumentID: "d", Ordinal: 1, Content: "beta"},
		{ID: "c2", DocumentID: "d", Ordinal: 2, Content: "gamma"},
	}, nil)

	t.Run("empty ids short-circuits", func(t *testing.T) {
		got, err := storage.GetChunkContentsByIDs(context.Background(), db, nil)
		require.NoError(t, err)
		require.Empty(t, got)
	})

	t.Run("subset with a missing id", func(t *testing.T) {
		got, err := storage.GetChunkContentsByIDs(context.Background(), db, []string{"c0", "c2", "ghost"})
		require.NoError(t, err)
		require.Equal(t, map[string]string{"c0": "alpha", "c2": "gamma"}, got)
	})
}

func TestGetChunksByDocURIOrdered(t *testing.T) {
	db := openMigrated(t)
	doc := model.Document{ID: "d", URI: "a.md", Collection: "c", ContentHash: "h", IndexedAt: "t"}
	// Insert out of ordinal order; the accessor must return them ordered.
	seedDoc(t, db, doc, []model.Chunk{
		{ID: "c2", DocumentID: "d", Ordinal: 2, Content: "third"},
		{ID: "c0", DocumentID: "d", Ordinal: 0, Content: "first"},
		{ID: "c1", DocumentID: "d", Ordinal: 1, Content: "second"},
	}, nil)

	t.Run("ordered by ordinal", func(t *testing.T) {
		got, err := storage.GetChunksByDocURI(context.Background(), db, "a.md")
		require.NoError(t, err)
		require.Len(t, got, 3)
		require.Equal(t, []int{0, 1, 2}, []int{got[0].Ordinal, got[1].Ordinal, got[2].Ordinal})
		require.Equal(t, "first", got[0].Content)
	})

	t.Run("uri with no chunks is empty", func(t *testing.T) {
		got, err := storage.GetChunksByDocURI(context.Background(), db, "missing.md")
		require.NoError(t, err)
		require.Empty(t, got)
	})
}

func TestListURIsByCollection(t *testing.T) {
	db := openMigrated(t)
	seedDoc(t, db, model.Document{ID: "d1", URI: "a.md", Collection: "alpha", ContentHash: "h", IndexedAt: "t"}, nil, nil)
	seedDoc(t, db, model.Document{ID: "d2", URI: "b.md", Collection: "alpha", ContentHash: "h", IndexedAt: "t"}, nil, nil)
	seedDoc(t, db, model.Document{ID: "d3", URI: "c.md", Collection: "beta", ContentHash: "h", IndexedAt: "t"}, nil, nil)

	t.Run("filters by collection", func(t *testing.T) {
		got, err := storage.ListURIsByCollection(context.Background(), db, "alpha")
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"a.md", "b.md"}, got)
	})

	t.Run("unknown collection is empty", func(t *testing.T) {
		got, err := storage.ListURIsByCollection(context.Background(), db, "gamma")
		require.NoError(t, err)
		require.Empty(t, got)
	})
}

func TestDeleteByURICascades(t *testing.T) {
	db := openMigrated(t)
	doc := model.Document{ID: "d", URI: "a.md", Collection: "c", ContentHash: "h", IndexedAt: "t"}
	seedDoc(t, db, doc,
		[]model.Chunk{
			{ID: "c0", DocumentID: "d", Ordinal: 0, Content: "alpha unique"},
			{ID: "c1", DocumentID: "d", Ordinal: 1, Content: "beta"},
		},
		[]model.Link{{SrcDoc: "d", DstDoc: "b.md"}},
	)

	// Sanity: everything is present and searchable before the delete.
	require.Equal(t, 1, scalar(t, db, `SELECT COUNT(*) FROM chunks_fts WHERE chunks_fts MATCH 'alpha'`))

	require.NoError(t, storage.DeleteByURI(context.Background(), db, "a.md"))

	require.Equal(t, 0, scalar(t, db, `SELECT COUNT(*) FROM documents WHERE uri = 'a.md'`))
	require.Equal(t, 0, scalar(t, db, `SELECT COUNT(*) FROM chunks WHERE document_id = 'd'`))
	require.Equal(t, 0, scalar(t, db, `SELECT COUNT(*) FROM links WHERE src_doc = 'd'`))
	require.Equal(t, 0, scalar(t, db, `SELECT COUNT(*) FROM chunks_fts`))
	require.Equal(t, 0, scalar(t, db, `SELECT COUNT(*) FROM chunks_fts WHERE chunks_fts MATCH 'alpha'`))
}

func TestDeleteByURIMissingIsNoOp(t *testing.T) {
	db := openMigrated(t)
	require.NoError(t, storage.DeleteByURI(context.Background(), db, "never-existed.md"))
}

// TestMigrateIdempotent confirms re-running migrations on an already-migrated
// database is a no-op (the non-regression guard for migration ordering).
func TestMigrateIdempotent(t *testing.T) {
	db := openMigrated(t)
	require.NoError(t, storage.Migrate(db))
	require.NoError(t, storage.Migrate(db))
}

// TestAccessorsErrorOnClosedDB exercises the error-wrapping branches of the
// read/delete accessors by querying a real, closed database — no mock, just a
// genuinely unusable *sql.DB.
func TestAccessorsErrorOnClosedDB(t *testing.T) {
	db := openMigrated(t)
	require.NoError(t, db.Close()) // the t.Cleanup double-close is a no-op.

	_, err := storage.GetDocumentByURI(context.Background(), db, "a.md")
	require.Error(t, err)
	_, err = storage.GetDocumentByID(context.Background(), db, "d")
	require.Error(t, err)
	_, err = storage.GetChunkByID(context.Background(), db, "c")
	require.Error(t, err)
	_, _, err = storage.DocumentHashByURI(context.Background(), db, "a.md")
	require.Error(t, err)
	_, err = storage.GetChunkContentsByIDs(context.Background(), db, []string{"c0"})
	require.Error(t, err)
	_, err = storage.GetChunksByDocURI(context.Background(), db, "a.md")
	require.Error(t, err)
	_, err = storage.ListURIsByCollection(context.Background(), db, "c")
	require.Error(t, err)
	require.Error(t, storage.DeleteByURI(context.Background(), db, "a.md"))
}

// scalar runs a COUNT-style query returning a single int.
func scalar(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, db.QueryRowContext(context.Background(), query, args...).Scan(&n), query)

	return n
}
