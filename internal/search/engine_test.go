package search_test

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/chunk"
	"github.com/arhuman/mnemos/internal/ingest"
	"github.com/arhuman/mnemos/internal/model"
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

// newTemporalCorpus ingests two documents that match the same term with the same
// strength but carry timestamps a year apart, so only recency can separate them.
func newTemporalCorpus(ctx context.Context, t *testing.T) *sql.DB {
	t.Helper()
	src := t.TempDir()
	write(t, src, "docs/fresh.md",
		"---\ntimestamp: 2026-09-01T00:00:00Z\n---\n\n# Fresh\n\nratelimit policy.\n")
	write(t, src, "docs/stale.md",
		"---\ntimestamp: 2025-09-01T00:00:00Z\n---\n\n# Stale\n\nratelimit policy.\n")

	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "temporal.db"))
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

// TestTemporalWeightZeroLeavesScoresUntouched is the compatibility guard: the
// default Query must score exactly as it did before recency existed. If this
// fails, every shipped eval number silently changed meaning.
func TestTemporalWeightZeroLeavesScoresUntouched(t *testing.T) {
	ctx := context.Background()
	db := newTemporalCorpus(ctx, t)
	engine := search.NewEngine(db, discardLogger())

	got, err := engine.Search(ctx, search.Query{Text: "ratelimit", Limit: 10})
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.InDelta(t, got[0].Score, got[1].Score, 1e-9,
		"with no temporal weight, two equally-matching docs must score identically regardless of age")
}

// TestTemporalWeightFavoursRecentDocument checks the feature does what it says:
// same term, same strength, newer document ranks first.
func TestTemporalWeightFavoursRecentDocument(t *testing.T) {
	ctx := context.Background()
	db := newTemporalCorpus(ctx, t)
	engine := search.NewEngine(db, discardLogger())

	now, err := time.Parse(time.RFC3339, "2026-09-02T00:00:00Z")
	require.NoError(t, err)

	got, err := engine.Search(ctx, search.Query{
		Text:           "ratelimit",
		Limit:          10,
		TemporalWeight: 1,
		Now:            now,
	})
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Contains(t, got[0].URI, "fresh.md", "the recent document must rank first")
	require.Greater(t, got[0].Score, got[1].Score)
}

// TestTemporalHalflifeControlsDecayRate pins the halflife to the score: a
// document exactly one halflife old keeps half its score at full weight.
func TestTemporalHalflifeControlsDecayRate(t *testing.T) {
	ctx := context.Background()
	db := newTemporalCorpus(ctx, t)
	engine := search.NewEngine(db, discardLogger())

	base, err := engine.Search(ctx, search.Query{Text: "ratelimit", Limit: 10})
	require.NoError(t, err)
	require.Len(t, base, 2)

	now, err := time.Parse(time.RFC3339, "2026-09-01T00:00:00Z")
	require.NoError(t, err)

	// stale.md is exactly 365 days older than the query instant.
	got, err := engine.Search(ctx, search.Query{
		Text:             "ratelimit",
		Limit:            10,
		TemporalWeight:   1,
		TemporalHalflife: 365 * 24 * time.Hour,
		Now:              now,
	})
	require.NoError(t, err)
	require.Len(t, got, 2)

	byURI := make(map[string]float64)
	for _, r := range got {
		byURI[r.URI] = r.Score
	}
	var unweighted float64
	for _, r := range base {
		if strings.Contains(r.URI, "stale.md") {
			unweighted = r.Score
		}
	}
	require.NotZero(t, unweighted)

	var stale float64
	for uri, score := range byURI {
		if strings.Contains(uri, "stale.md") {
			stale = score
		}
	}
	// bm25 scores here are ~7e-7, so an absolute tolerance larger than the values
	// themselves would pass for any decay rate. Assert on the ratio instead.
	require.InEpsilon(t, unweighted*0.5, stale, 1e-6,
		"a document one halflife old must keep half its score")
}

// TestTemporalRankingIgnoresUndatedDocument guards the honesty rule: a document
// the indexer could not date must not be treated as infinitely old and buried.
func TestTemporalRankingIgnoresUndatedDocument(t *testing.T) {
	ctx := context.Background()
	src := t.TempDir()
	write(t, src, "docs/undated.md", "# Undated\n\nratelimit policy.\n")

	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "undated.db"))
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

	engine := search.NewEngine(db, discardLogger())
	base, err := engine.Search(ctx, search.Query{Text: "ratelimit", Limit: 10})
	require.NoError(t, err)
	require.Len(t, base, 1)

	got, err := engine.Search(ctx, search.Query{
		Text: "ratelimit", Limit: 10, TemporalWeight: 1,
	})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.InDelta(t, base[0].Score, got[0].Score, 1e-9,
		"an undated document must keep its score rather than decay to nothing")
}

// TestTemporalRankingTreatsFutureAsCurrent covers clock skew and hand-edited
// frontmatter: a future timestamp must not out-boost a current document.
func TestTemporalRankingTreatsFutureAsCurrent(t *testing.T) {
	ctx := context.Background()
	db := newTemporalCorpus(ctx, t)
	engine := search.NewEngine(db, discardLogger())

	past, err := time.Parse(time.RFC3339, "2020-01-01T00:00:00Z")
	require.NoError(t, err)

	got, err := engine.Search(ctx, search.Query{
		Text: "ratelimit", Limit: 10, TemporalWeight: 1, Now: past,
	})
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.InDelta(t, got[0].Score, got[1].Score, 1e-9,
		"documents dated in the future are all treated as current, so neither wins")
}

// newSupersessionCorpus ingests three documents that match one term equally:
// a current document, one superseded by the current one, and one superseded by
// a uri that was never ingested (a dangling pointer).
func newSupersessionCorpus(ctx context.Context, t *testing.T) *sql.DB {
	t.Helper()
	src := t.TempDir()
	write(t, src, "adr/current.md",
		"# Current\n\nquorum policy.\n")
	write(t, src, "adr/replaced.md",
		"---\nsuperseded_by: adr/current.md\n---\n\n# Replaced\n\nquorum policy.\n")
	write(t, src, "adr/dangling.md",
		"---\nsuperseded_by: adr/never-ingested.md\n---\n\n# Dangling\n\nquorum policy.\n")

	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "supersede.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, storage.Migrate(db))

	_, err = ingest.New(db, discardLogger()).Run(ctx, ingest.Options{
		Root:       src,
		Collection: "adr",
		Rules:      ingest.Rules{Include: []string{"**/*.md"}},
		Chunking:   chunk.Config{TargetTokens: 700, OverlapTokens: 80},
	})
	require.NoError(t, err)

	return db
}

// resultFor returns the result whose uri contains want, failing if absent.
func resultFor(t *testing.T, results []model.Result, want string) model.Result {
	t.Helper()
	for _, r := range results {
		if strings.Contains(r.URI, want) {
			return r
		}
	}
	t.Fatalf("no result for %q in %d results", want, len(results))

	return model.Result{}
}

// TestSupersededDocumentIsDemotedNotHidden is the core of ADR-0010: a superseded
// document still returns and still cites, but ranks below its replacement.
func TestSupersededDocumentIsDemotedNotHidden(t *testing.T) {
	ctx := context.Background()
	db := newSupersessionCorpus(ctx, t)
	engine := search.NewEngine(db, discardLogger())

	got, err := engine.Search(ctx, search.Query{Text: "quorum", Limit: 10})
	require.NoError(t, err)
	require.Len(t, got, 3, "a superseded document must still be returned, never filtered out")
	require.Contains(t, got[0].URI, "current.md", "the current document outranks both superseded ones")

	replaced := resultFor(t, got, "replaced.md")
	require.Greater(t, replaced.Score, 0.0, "a demoted document keeps a usable score, it is not zeroed")
	require.Less(t, replaced.Score, got[0].Score)
	require.NotEmpty(t, replaced.Snippet, "a demoted document stays citable")
}

// TestSupersededByTravelsWithTheResult covers ADR-0010 decision 1: the caller
// that cites a superseded document receives the replacement in the same payload.
func TestSupersededByTravelsWithTheResult(t *testing.T) {
	ctx := context.Background()
	db := newSupersessionCorpus(ctx, t)
	engine := search.NewEngine(db, discardLogger())

	got, err := engine.Search(ctx, search.Query{Text: "quorum", Limit: 10})
	require.NoError(t, err)

	replaced := resultFor(t, got, "replaced.md")
	require.Equal(t, "adr/current.md", replaced.SupersededBy)
	require.True(t, replaced.SupersededByResolved)

	current := resultFor(t, got, "current.md")
	require.Empty(t, current.SupersededBy, "a current document carries no pointer")
	require.False(t, current.SupersededByResolved)
}

// TestSupersededByDanglingReportsUnresolved pins the degradation rule: a
// replacement that is not in the index reports unresolved rather than erroring
// or dropping the marker.
func TestSupersededByDanglingReportsUnresolved(t *testing.T) {
	ctx := context.Background()
	db := newSupersessionCorpus(ctx, t)
	engine := search.NewEngine(db, discardLogger())

	got, err := engine.Search(ctx, search.Query{Text: "quorum", Limit: 10})
	require.NoError(t, err)

	dangling := resultFor(t, got, "dangling.md")
	require.Equal(t, "adr/never-ingested.md", dangling.SupersededBy,
		"the marker survives even when its target does not resolve")
	require.False(t, dangling.SupersededByResolved)

	// A dangling pointer still demotes: a human recorded that this was replaced,
	// and that judgment is not void because the replacement is missing.
	current := resultFor(t, got, "current.md")
	require.Less(t, dangling.Score, current.Score)
}

// TestSupersededSelfReferenceReportsUnresolved covers the cycle guard: a
// document naming itself must not be marked its own resolved replacement.
func TestSupersededSelfReferenceReportsUnresolved(t *testing.T) {
	ctx := context.Background()
	src := t.TempDir()
	write(t, src, "adr/loop.md",
		"---\nsuperseded_by: adr/loop.md\n---\n\n# Loop\n\nquorum policy.\n")

	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "selfref.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, storage.Migrate(db))
	_, err = ingest.New(db, discardLogger()).Run(ctx, ingest.Options{
		Root:       src,
		Collection: "adr",
		Rules:      ingest.Rules{Include: []string{"**/*.md"}},
		Chunking:   chunk.Config{TargetTokens: 700, OverlapTokens: 80},
	})
	require.NoError(t, err)

	engine := search.NewEngine(db, discardLogger())
	got, err := engine.Search(ctx, search.Query{Text: "quorum", Limit: 10})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, "adr/loop.md", got[0].SupersededBy)
	require.False(t, got[0].SupersededByResolved,
		"a self-reference is a data error: reported, never obeyed as a resolved replacement")
}

// TestSupersededDemotionComposesWithRecency checks the two multipliers stack
// rather than one overriding the other: a recent-but-superseded document must
// lose to a current one of the same age.
func TestSupersededDemotionComposesWithRecency(t *testing.T) {
	ctx := context.Background()
	src := t.TempDir()
	write(t, src, "adr/fresh-current.md",
		"---\ntimestamp: 2026-09-01T00:00:00Z\n---\n\n# Fresh Current\n\nquorum policy.\n")
	write(t, src, "adr/fresh-superseded.md",
		"---\ntimestamp: 2026-09-01T00:00:00Z\nsuperseded_by: adr/fresh-current.md\n---\n\n"+
			"# Fresh Superseded\n\nquorum policy.\n")

	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "compose.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, storage.Migrate(db))
	_, err = ingest.New(db, discardLogger()).Run(ctx, ingest.Options{
		Root:       src,
		Collection: "adr",
		Rules:      ingest.Rules{Include: []string{"**/*.md"}},
		Chunking:   chunk.Config{TargetTokens: 700, OverlapTokens: 80},
	})
	require.NoError(t, err)

	now, err := time.Parse(time.RFC3339, "2026-09-02T00:00:00Z")
	require.NoError(t, err)

	engine := search.NewEngine(db, discardLogger())
	got, err := engine.Search(ctx, search.Query{
		Text: "quorum", Limit: 10, TemporalWeight: 1, Now: now,
	})
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Contains(t, got[0].URI, "fresh-current.md",
		"equally recent, the superseded one must still lose")
}

// newJournalCorpus ingests a citable document and a journal entry that match the
// same term equally, so only the exclusion can separate them. The journal entry
// deliberately sits outside any journal-looking directory: exclusion must key on
// the stored class, not the uri (ADR-0011).
func newJournalCorpus(ctx context.Context, t *testing.T) *sql.DB {
	t.Helper()
	src := t.TempDir()
	write(t, src, "adr/rate-limit.md",
		"---\ntype: decision\n---\n\n# Rate limit\n\nthrottle policy at the edge.\n")
	write(t, src, "elsewhere/turn-7.md",
		"---\ntype: Journal\n---\n\n# Turn 7\n\nthrottle policy came up in conversation.\n")

	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "journal.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, storage.Migrate(db))

	_, err = ingest.New(db, discardLogger()).Run(ctx, ingest.Options{
		Root:       src,
		Collection: "kb",
		Rules:      ingest.Rules{Include: []string{"**/*.md"}},
		Chunking:   chunk.Config{TargetTokens: 700, OverlapTokens: 80},
	})
	require.NoError(t, err)

	return db
}

// TestJournalExcludedByDefault is the core of ADR-0011: a default query returns
// citable knowledge only, however well a journal entry matches.
func TestJournalExcludedByDefault(t *testing.T) {
	ctx := context.Background()
	db := newJournalCorpus(ctx, t)
	engine := search.NewEngine(db, discardLogger())

	got, err := engine.Search(ctx, search.Query{Text: "throttle", Limit: 10})
	require.NoError(t, err)
	require.Len(t, got, 1, "the journal entry must not appear in a default search")
	require.Contains(t, got[0].URI, "adr/rate-limit.md")
}

// TestJournalReturnedWhenIncluded covers the opt-in: the entry is indexed and
// retrievable, it is merely not offered unasked.
func TestJournalReturnedWhenIncluded(t *testing.T) {
	ctx := context.Background()
	db := newJournalCorpus(ctx, t)
	engine := search.NewEngine(db, discardLogger())

	got, err := engine.Search(ctx, search.Query{Text: "throttle", Limit: 10, IncludeJournal: true})
	require.NoError(t, err)
	require.Len(t, got, 2)

	var sawJournal bool
	for _, r := range got {
		if strings.Contains(r.URI, "turn-7.md") {
			sawJournal = true
		}
	}
	require.True(t, sawJournal, "include_journal must surface the entry, not merely permit it")
}

// TestJournalExclusionIgnoresURIShape pins decision 2: a document is excluded
// for what it declares, not for where it sits. The journal entry here is outside
// any journal directory, and a decision inside one is still returned.
func TestJournalExclusionIgnoresURIShape(t *testing.T) {
	ctx := context.Background()
	src := t.TempDir()
	write(t, src, "journal/decision.md",
		"---\ntype: decision\n---\n\n# Decision\n\nthrottle policy at the edge.\n")
	write(t, src, "adr/turn.md",
		"---\ntype: Journal\n---\n\n# Turn\n\nthrottle policy came up.\n")

	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "shape.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, storage.Migrate(db))
	_, err = ingest.New(db, discardLogger()).Run(ctx, ingest.Options{
		Root:       src,
		Collection: "kb",
		Rules:      ingest.Rules{Include: []string{"**/*.md"}},
		Chunking:   chunk.Config{TargetTokens: 700, OverlapTokens: 80},
	})
	require.NoError(t, err)

	engine := search.NewEngine(db, discardLogger())
	got, err := engine.Search(ctx, search.Query{Text: "throttle", Limit: 10})
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Contains(t, got[0].URI, "journal/decision.md",
		"a citable document in a journal-shaped path is still returned")
}

// TestJournalNotLaunderedThroughGraphExpansion guards the hole the base filter
// cannot see: graph expansion fetches neighbors on its own, so a journal entry
// linked from a top hit would re-enter a result set that excluded it.
func TestJournalNotLaunderedThroughGraphExpansion(t *testing.T) {
	ctx := context.Background()
	src := t.TempDir()
	write(t, src, "adr/seed.md",
		"---\ntype: decision\n---\n\n# Seed\n\nthrottle policy. See [turn](../journal/turn.md).\n")
	write(t, src, "journal/turn.md",
		"---\ntype: Journal\n---\n\n# Turn\n\nunrelated wording entirely.\n")

	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "launder.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, storage.Migrate(db))
	_, err = ingest.New(db, discardLogger()).Run(ctx, ingest.Options{
		Root:       src,
		Collection: "kb",
		Rules:      ingest.Rules{Include: []string{"**/*.md"}},
		Chunking:   chunk.Config{TargetTokens: 700, OverlapTokens: 80},
	})
	require.NoError(t, err)

	// Expansion only fires when the base query under-fills the limit, which a
	// limit of 5 against a single lexical hit guarantees.
	graph := search.NewGraphRetriever(search.NewEngine(db, discardLogger()), db, 3, 0.5, discardLogger())
	got, err := graph.Search(ctx, search.Query{Text: "throttle", Limit: 5})
	require.NoError(t, err)

	for _, r := range got {
		require.NotContains(t, r.URI, "journal/turn.md",
			"graph expansion must honour the journal exclusion the base query applied")
	}
}
