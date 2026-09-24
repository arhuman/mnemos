package ingest_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/chunk"
	"github.com/arhuman/mnemos/internal/ingest"
	"github.com/arhuman/mnemos/internal/storage"
)

// TestIngestStampsJournalFromDeclaredType is the end-to-end half of ADR-0011's
// decision 2: the flag comes from the type the document declares, so a document
// is classified by what it is rather than by where it sits.
func TestIngestStampsJournalFromDeclaredType(t *testing.T) {
	ctx := context.Background()
	src := t.TempDir()

	// Declares the journal type, but lives outside any journal directory.
	write(t, src, "elsewhere/turn.md",
		"---\ntype: Journal\n---\n\n# Turn\n\nthe user asked about rate limits.\n")
	// Sits in a journal-looking directory but declares a citable type.
	write(t, src, "journal/decision.md",
		"---\ntype: decision\n---\n\n# Decision\n\nwe rate-limit at the edge.\n")
	// No type at all: the common case, and it must not be journal.
	write(t, src, "plain.md", "# Plain\n\nno frontmatter here.\n")

	db := newDB(t)
	_, err := ingest.New(db, slog.New(slog.NewTextHandler(io.Discard, nil))).Run(ctx, ingest.Options{
		Root:       src,
		Collection: "c",
		Rules:      ingest.Rules{Include: []string{"**/*.md"}},
		Chunking:   chunk.Config{TargetTokens: 700, OverlapTokens: 80},
	})
	require.NoError(t, err)

	isJournal, err := storage.IsJournalURI(ctx, db, "elsewhere/turn.md")
	require.NoError(t, err)
	require.True(t, isJournal, "a declared Journal type is a journal entry wherever it lives")

	isJournal, err = storage.IsJournalURI(ctx, db, "journal/decision.md")
	require.NoError(t, err)
	require.False(t, isJournal, "a citable type in a journal directory is not a journal entry")

	isJournal, err = storage.IsJournalURI(ctx, db, "plain.md")
	require.NoError(t, err)
	require.False(t, isJournal, "an untyped document is not a journal entry")

	n, err := storage.CountJournal(ctx, db)
	require.NoError(t, err)
	require.Equal(t, 1, n)
}
