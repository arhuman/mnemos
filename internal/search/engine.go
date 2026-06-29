package search

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/arhuman/mnemos/internal/model"
)

// Retriever is the retrieval seam: a query in, ranked results out. The FTS5
// engine implements it now; a hybrid (vector + bm25) retriever can implement
// the same interface later without touching callers.
type Retriever interface {
	// Search runs q against the index and returns ranked results.
	Search(ctx context.Context, q Query) ([]model.Result, error)
}

// Engine is the FTS5-backed Retriever. It MATCHes a sanitized query against
// chunks_fts, ranks with bm25() column weights, then applies a small
// deterministic heading boost and re-sorts so higher score = better.
type Engine struct {
	db     *sql.DB
	logger *slog.Logger
}

// NewEngine builds an FTS5 Engine over db. A nil logger is replaced with a
// discard logger so the engine never panics on a missing dependency.
func NewEngine(db *sql.DB, logger *slog.Logger) *Engine {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	return &Engine{db: db, logger: logger}
}

// Search implements Retriever. It sanitizes q.Text into a safe FTS5 MATCH
// expression, runs the ranked join with q's exact filters applied, converts the
// negative bm25 rank into a positive score, layers the heading boost, and
// returns results sorted best-first. An empty query (after sanitization) is a
// friendly ErrEmptyQuery rather than an FTS5 syntax error.
func (e *Engine) Search(ctx context.Context, q Query) ([]model.Result, error) {
	match, err := sanitizeMatch(q.Text)
	if err != nil {
		return nil, err
	}

	conds, filterArgs := filterClause(q)
	query := searchSQL(conds)

	limit := normalizeLimit(q.Limit)
	// Over-fetch before the Go-side heading boost: the SQL orders on bm25 alone,
	// so a chunk the boost would promote into the top `limit` must be pulled here
	// or it is truncated away before the boost is ever applied. We fetch a small
	// multiple, boost, re-sort, then cut back to `limit` below.
	fetch := limit * overFetchFactor

	args := make([]any, 0, len(filterArgs)+2)
	args = append(args, match)
	args = append(args, filterArgs...)
	args = append(args, fetch)

	e.logger.Debug("search executing", "match", match, "filters", len(conds), "limit", limit, "fetch", fetch)

	rows, err := e.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("search: query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	terms := queryTerms(q.Text)
	results := make([]model.Result, 0, fetch)
	for rows.Next() {
		var r model.Result
		var rank float64
		if err := rows.Scan(
			&r.ID, &r.DocumentID, &r.URI, &r.Collection,
			&r.HeadingPath, &r.StartLine, &r.EndLine, &r.Snippet, &rank,
		); err != nil {
			return nil, fmt.Errorf("search: scan row: %w", err)
		}
		// SQLite bm25() is negative with more-negative = better; flip it so the
		// displayed score is positive and higher = better.
		r.Score = -rank + headingScore(r.HeadingPath, terms)
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search: iterate rows: %w", err)
	}

	// Re-sort: the Go-side boost can reorder rows that the SQL ORDER BY ranked
	// purely on bm25. Stable sort keeps DB order for equal scores.
	slices.SortStableFunc(results, func(a, b model.Result) int {
		return cmp.Compare(b.Score, a.Score)
	})

	// Truncate the over-fetched candidate pool back to the requested limit, now
	// that the boost has had its chance to promote rows into the top results.
	if len(results) > limit {
		results = results[:limit]
	}

	return results, nil
}

// headingScore returns the boost for a result whose heading_path contains any
// of the query terms. The boost is granted once (not once per term) to keep it a
// gentle tie-breaker rather than a dominant signal.
func headingScore(headingPath string, terms []string) float64 {
	if headingPath == "" || len(terms) == 0 {
		return 0
	}
	hp := strings.ToLower(headingPath)
	for _, t := range terms {
		if strings.Contains(hp, t) {
			return headingBoost
		}
	}

	return 0
}

// compile-time assertion that Engine satisfies Retriever.
var _ Retriever = (*Engine)(nil)
