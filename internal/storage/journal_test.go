package storage_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/model"
	"github.com/arhuman/mnemos/internal/storage"
)

// putDoc upserts one document, defaulting the fields a journal test does not
// care about so each case states only what it is testing.
func putDoc(t *testing.T, db *sql.DB, d model.Document) {
	t.Helper()
	if d.ContentHash == "" {
		d.ContentHash = "h-" + d.ID
	}
	if d.IndexedAt == "" {
		d.IndexedAt = "2026-09-01T00:00:00Z"
	}
	inTx(t, db, func(tx *sql.Tx) {
		require.NoError(t, storage.UpsertDocument(context.Background(), tx, d))
	})
}

// TestIsJournalTypeMatchesCaseInsensitively pins the single spelling authority:
// ingest, promotion and listing must agree on what counts as a journal entry.
func TestIsJournalTypeMatchesCaseInsensitively(t *testing.T) {
	for _, in := range []string{"journal", "Journal", "JOURNAL", "  Journal  "} {
		require.True(t, storage.IsJournalType(in), "%q must name the journal tier", in)
	}
	for _, in := range []string{"", "decision", "journalist", "task"} {
		require.False(t, storage.IsJournalType(in), "%q must not name the journal tier", in)
	}
}

// TestJournalFlagRoundTrips is the migration's contract: the column persists the
// flag through an upsert and reads back unchanged.
func TestJournalFlagRoundTrips(t *testing.T) {
	ctx := context.Background()
	db := openMigrated(t)

	putDoc(t, db, model.Document{ID: "j1", URI: "journal/2026-09-24/turn-1.md", Collection: "journal", Journal: true})
	putDoc(t, db, model.Document{ID: "k1", URI: "adr/0001-choice.md", Collection: "adr"})

	isJournal, err := storage.IsJournalURI(ctx, db, "journal/2026-09-24/turn-1.md")
	require.NoError(t, err)
	require.True(t, isJournal)

	isJournal, err = storage.IsJournalURI(ctx, db, "adr/0001-choice.md")
	require.NoError(t, err)
	require.False(t, isJournal, "a kb document must not be flagged journal")
}

// TestJournalDefaultsToFalseOnExistingRows is what makes migration 0006 safe on
// a populated database: every pre-existing row reads as non-journal with no
// backfill, so retrieval behaviour is unchanged until something is stamped.
func TestJournalDefaultsToFalseOnExistingRows(t *testing.T) {
	ctx := context.Background()
	db := openMigrated(t)

	// Insert without naming the journal column at all, the shape a row written
	// before this migration has.
	_, err := db.ExecContext(ctx, `
		INSERT INTO documents (id, uri, collection, content_hash, indexed_at)
		VALUES ('old', 'adr/legacy.md', 'adr', 'h', '2020-01-01T00:00:00Z')`)
	require.NoError(t, err)

	isJournal, err := storage.IsJournalURI(ctx, db, "adr/legacy.md")
	require.NoError(t, err)
	require.False(t, isJournal)
}

// TestIsJournalURIUnknownDocumentIsNotJournal pins the absent case: a uri with
// no row is not a journal entry, and is not an error either.
func TestIsJournalURIUnknownDocumentIsNotJournal(t *testing.T) {
	db := openMigrated(t)

	isJournal, err := storage.IsJournalURI(context.Background(), db, "nothing/here.md")
	require.NoError(t, err)
	require.False(t, isJournal)
}

// TestListJournalReturnsOnlyJournalNewestFirst covers the listing consolidation
// walks: journal rows only, most recent first.
func TestListJournalReturnsOnlyJournalNewestFirst(t *testing.T) {
	ctx := context.Background()
	db := openMigrated(t)

	putDoc(t, db, model.Document{ID: "j1", URI: "journal/a.md", Collection: "journal", Journal: true, ModifiedAt: "2026-09-01T00:00:00Z"})
	putDoc(t, db, model.Document{ID: "j2", URI: "journal/b.md", Collection: "journal", Journal: true, ModifiedAt: "2026-09-03T00:00:00Z"})
	putDoc(t, db, model.Document{ID: "j3", URI: "journal/c.md", Collection: "journal", Journal: true, ModifiedAt: "2026-09-02T00:00:00Z"})
	putDoc(t, db, model.Document{ID: "k1", URI: "adr/keep.md", Collection: "adr", ModifiedAt: "2026-09-04T00:00:00Z"})

	got, err := storage.ListJournal(ctx, db, 0)
	require.NoError(t, err)
	require.Len(t, got, 3, "the kb document must not appear in a journal listing")

	require.Equal(t, "journal/b.md", got[0].URI)
	require.Equal(t, "journal/c.md", got[1].URI)
	require.Equal(t, "journal/a.md", got[2].URI)
	require.True(t, got[0].Journal, "a listed entry reports itself as journal")
}

// TestListJournalRespectsLimit checks the cap, and that a non-positive limit
// means "all" rather than "none" — the difference between a paged consolidation
// pass and one that silently does nothing.
func TestListJournalRespectsLimit(t *testing.T) {
	ctx := context.Background()
	db := openMigrated(t)

	putDoc(t, db, model.Document{ID: "j1", URI: "journal/a.md", Collection: "journal", Journal: true, ModifiedAt: "2026-09-01T00:00:00Z"})
	putDoc(t, db, model.Document{ID: "j2", URI: "journal/b.md", Collection: "journal", Journal: true, ModifiedAt: "2026-09-02T00:00:00Z"})

	got, err := storage.ListJournal(ctx, db, 1)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "journal/b.md", got[0].URI)

	got, err = storage.ListJournal(ctx, db, 0)
	require.NoError(t, err)
	require.Len(t, got, 2, "a non-positive limit returns every entry, never an empty page")
}

// TestListJournalEmptyIsNotAnError guards the first-run case: a workspace with
// no journal yet lists nothing and succeeds.
func TestListJournalEmptyIsNotAnError(t *testing.T) {
	got, err := storage.ListJournal(context.Background(), openMigrated(t), 0)
	require.NoError(t, err)
	require.Empty(t, got)
}

// TestCountJournalCountsOnlyJournal backs the stats surface.
func TestCountJournalCountsOnlyJournal(t *testing.T) {
	ctx := context.Background()
	db := openMigrated(t)

	putDoc(t, db, model.Document{ID: "j1", URI: "journal/a.md", Collection: "journal", Journal: true})
	putDoc(t, db, model.Document{ID: "k1", URI: "adr/a.md", Collection: "adr"})
	putDoc(t, db, model.Document{ID: "k2", URI: "adr/b.md", Collection: "adr"})

	n, err := storage.CountJournal(ctx, db)
	require.NoError(t, err)
	require.Equal(t, 1, n)
}

// TestUpsertClearsJournalOnReingest covers the flag following the document's
// declared type: a file that stops declaring the journal type stops being one,
// rather than keeping a stale flag from its first ingest.
func TestUpsertClearsJournalOnReingest(t *testing.T) {
	ctx := context.Background()
	db := openMigrated(t)

	putDoc(t, db, model.Document{ID: "j1", URI: "journal/a.md", Collection: "journal", Journal: true})
	isJournal, err := storage.IsJournalURI(ctx, db, "journal/a.md")
	require.NoError(t, err)
	require.True(t, isJournal)

	putDoc(t, db, model.Document{ID: "j1", URI: "journal/a.md", Collection: "journal", Journal: false})
	isJournal, err = storage.IsJournalURI(ctx, db, "journal/a.md")
	require.NoError(t, err)
	require.False(t, isJournal, "re-ingest must refresh the flag, not leave it stale")
}
