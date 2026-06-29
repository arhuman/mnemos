package search_test

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/chunk"
	"github.com/arhuman/mnemos/internal/ingest"
	"github.com/arhuman/mnemos/internal/search"
	"github.com/arhuman/mnemos/internal/storage"
)

// discardLogger is a slog logger that drops output, used to keep test logs quiet.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// newCorpus writes a small markdown corpus to a temp dir and ingests it into a
// temp SQLite database via the real ingest pipeline. It returns the open
// *sql.DB, which is the shared fixture for the engine tests.
func newCorpus(t *testing.T) *sql.DB {
	t.Helper()

	src := t.TempDir()
	write(t, src, "docs/security/scim.md",
		"---\ntype: guide\ntags: [security, identity]\n---\n\n"+
			"# SCIM\n\n## Provisioning\n\n"+
			"SCIM provisioning with Entra synchronizes users automatically.\n")
	write(t, src, "docs/notes/cooking.md",
		"# Cooking\n\n## Pasta\n\nBoil water, add salt, cook the pasta until al dente.\n")
	write(t, src, "adr/004-entra.md",
		"# Entra Integration\n\n## Decision\n\nWe adopt Entra ID for identity.\n")

	dbPath := filepath.Join(t.TempDir(), "search.db")
	db, err := storage.Open(context.Background(), dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, storage.Migrate(db))

	_, err = ingest.New(db, discardLogger()).Run(context.Background(), ingest.Options{
		Root:       src,
		Collection: "docs",
		Rules:      ingest.Rules{Include: []string{"**/*.md"}},
		Chunking:   chunk.Config{TargetTokens: 700, OverlapTokens: 80},
	})
	require.NoError(t, err)

	return db
}

// newWildcardCorpus ingests two docs whose uris differ only at the spot a LIKE
// wildcard would match: "docs/a_b.md" (literal underscore) and "docs/axb.md".
// Both share a search term so only the path filter can distinguish them.
func newWildcardCorpus(ctx context.Context, t *testing.T) *sql.DB {
	t.Helper()
	src := t.TempDir()
	write(t, src, "docs/a_b.md", "# A_B\n\nwildcardterm here.\n")
	write(t, src, "docs/axb.md", "# AXB\n\nwildcardterm here.\n")

	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "wild.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, storage.Migrate(db))

	_, err = ingest.New(db, discardLogger()).Run(ctx, ingest.Options{
		Root:       src,
		Collection: "docs",
		Rules:      ingest.Rules{Include: []string{"**/*.md"}},
		Chunking:   chunk.Config{TargetTokens: 700, OverlapTokens: 80},
	})
	require.NoError(t, err)

	return db
}

// write creates a file with content under dir, making parent directories.
func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
}

// TestSearchTruncatesOverfetchToLimit guards the heading-boost over-fetch: the
// engine pulls limit*overFetchFactor candidates so the Go-side boost can
// promote a row across the bm25 LIMIT boundary, but it must still return no more
// than the requested limit.
func TestSearchTruncatesOverfetchToLimit(t *testing.T) {
	src := t.TempDir()
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		write(t, src, "docs/"+name+".md",
			"# Doc "+name+"\n\n## Section\n\nalpha alpha alpha matches here.\n")
	}

	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "trunc.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, storage.Migrate(db))
	_, err = ingest.New(db, discardLogger()).Run(context.Background(), ingest.Options{
		Root:       src,
		Collection: "docs",
		Rules:      ingest.Rules{Include: []string{"**/*.md"}},
		Chunking:   chunk.Config{TargetTokens: 700, OverlapTokens: 80},
	})
	require.NoError(t, err)

	engine := search.NewEngine(db, discardLogger())
	for _, limit := range []int{1, 2, 3} {
		results, err := engine.Search(context.Background(), search.Query{Text: "alpha", Limit: limit})
		require.NoError(t, err)
		require.Len(t, results, limit, "limit=%d must cap results despite over-fetch", limit)
	}
}

// TestSearchRanksExpectedDoc asserts a known query returns the right document at
// rank 1, that filters narrow the result set, and that a punctuation-heavy query
// never raises an FTS5 syntax error.
func TestSearchRanksExpectedDoc(t *testing.T) {
	db := newCorpus(t)
	engine := search.NewEngine(db, discardLogger())
	ctx := context.Background()

	t.Run("known query ranks expected doc first", func(t *testing.T) {
		results, err := engine.Search(ctx, search.Query{Text: "SCIM provisioning Entra", Limit: 5})
		require.NoError(t, err)
		require.NotEmpty(t, results)
		require.Equal(t, "docs/security/scim.md", results[0].URI)
		require.Positive(t, results[0].Score)
		require.LessOrEqual(t, results[0].StartLine, results[0].EndLine)
	})

	t.Run("collection filter narrows results", func(t *testing.T) {
		all, err := engine.Search(ctx, search.Query{Text: "Entra", Limit: 10})
		require.NoError(t, err)
		require.NotEmpty(t, all)

		none, err := engine.Search(ctx, search.Query{Text: "Entra", Collection: "no-such-collection", Limit: 10})
		require.NoError(t, err)
		require.Empty(t, none)
	})

	t.Run("path prefix filter narrows results", func(t *testing.T) {
		results, err := engine.Search(ctx, search.Query{Text: "Entra", PathPrefix: "adr/", Limit: 10})
		require.NoError(t, err)
		for _, r := range results {
			require.Contains(t, r.URI, "adr/")
		}
		require.NotEmpty(t, results)
	})

	t.Run("file type filter narrows results", func(t *testing.T) {
		results, err := engine.Search(ctx, search.Query{Text: "Entra", FileType: "md", Limit: 10})
		require.NoError(t, err)
		require.NotEmpty(t, results)
	})

	t.Run("path prefix treats LIKE wildcards literally", func(t *testing.T) {
		// A "_" in the prefix is a LIKE wildcard; with ESCAPE it must match a
		// literal underscore only, not any character.
		wdb := newWildcardCorpus(ctx, t)
		eng := search.NewEngine(wdb, discardLogger())

		results, err := eng.Search(ctx, search.Query{Text: "wildcardterm", PathPrefix: "docs/a_b", Limit: 10})
		require.NoError(t, err)
		require.Len(t, results, 1)
		require.Equal(t, "docs/a_b.md", results[0].URI)
	})

	t.Run("punctuation-heavy query does not error", func(t *testing.T) {
		// The raw query contains FTS5 syntax (quotes, parens, *, --, ?) that
		// would be a syntax error if passed through; sanitization must defuse it.
		// The core contract is "never an FTS5 syntax error".
		results, err := engine.Search(ctx, search.Query{
			Text:  `SCIM: provisioning (Entra)? -- * "" !!`,
			Limit: 5,
		})
		require.NoError(t, err)
		require.NotEmpty(t, results)
		require.Equal(t, "docs/security/scim.md", results[0].URI)
	})

	t.Run("empty query is a friendly error", func(t *testing.T) {
		_, err := engine.Search(ctx, search.Query{Text: "!!! ??? ...", Limit: 5})
		require.ErrorIs(t, err, search.ErrEmptyQuery)
	})
}
