package search

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/arhuman/mnemos/internal/model"
	"github.com/arhuman/mnemos/internal/okfschema"
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
	now := q.Now
	if now.IsZero() {
		now = time.Now()
	}
	results := make([]model.Result, 0, fetch)
	for rows.Next() {
		var r model.Result
		var rank float64
		if err := rows.Scan(
			&r.ID, &r.DocumentID, &r.URI, &r.Collection, &r.Title, &r.ModifiedAt,
			&r.HeadingPath, &r.StartLine, &r.EndLine, &r.Snippet,
			&r.SupersededBy, &r.SupersededByResolved, &rank,
		); err != nil {
			return nil, fmt.Errorf("search: scan row: %w", err)
		}
		// snippet() covers column 0 (content) only, so a chunk that matched on its
		// heading, tags, or doc_type — but whose content column is empty — yields an
		// empty snippet. Fall back to the heading path so the hit still carries the
		// signal that ranked it rather than a blank line.
		if r.Snippet == "" {
			r.Snippet = r.HeadingPath
		}
		// SQLite bm25() is negative with more-negative = better; flip it so the
		// displayed score is positive and higher = better.
		r.Score = (-rank + headingScore(r.HeadingPath, terms)) *
			recencyFactor(r.ModifiedAt, q, now) * supersededFactor(r.SupersededBy)
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

// recencyFactor returns the multiplier a document's age applies to its score:
// 1.0 for a document modified now, decaying by half every halflife. It is a
// multiplier rather than an additive term so it scales with bm25 instead of
// competing with it: a weak old hit and a weak new hit stay close together,
// while a strong hit keeps its lead over a strong-but-stale one.
//
// It returns exactly 1 (no effect) whenever temporal ranking cannot be applied
// honestly: weight 0, an unparseable timestamp, or a document with no
// modified_at. Guessing an age for an undated document would rank it as
// infinitely old, silently burying every document the indexer could not date.
func recencyFactor(modifiedAt string, q Query, now time.Time) float64 {
	if q.TemporalWeight <= 0 {
		return 1
	}
	mod, err := time.Parse(time.RFC3339, modifiedAt)
	if err != nil {
		return 1
	}

	halflife := q.TemporalHalflife
	if halflife <= 0 {
		halflife = defaultTemporalHalflife
	}

	// A document dated in the future (clock skew, a hand-edited frontmatter) is
	// treated as current rather than boosted above everything else.
	age := max(now.Sub(mod), 0)

	decay := math.Pow(0.5, age.Seconds()/halflife.Seconds())

	// Blend toward 1 by the weight: at weight 1 the raw decay applies, at 0.5 a
	// document of any age keeps at least half its score. This is what makes the
	// weight a dial rather than a switch.
	w := math.Min(q.TemporalWeight, 1)

	return 1 - w + w*decay
}

// supersededDemotion is the multiplier applied to a document that names a
// replacement. It is well below 1 so a superseded document loses to its
// replacement on any comparable match, and strictly above 0 so it still ranks,
// still returns, and stays citable: ADR-0010 demotes, it never hides.
const supersededDemotion = 0.25

// supersededFactor returns the multiplier a document's supersession applies to
// its score. It keys on the pointer being present rather than on it resolving:
// a dangling superseded_by still means a human recorded that this document was
// replaced, and that judgment does not become void because the replacement is
// missing from the index.
func supersededFactor(supersededBy string) float64 {
	if supersededBy == "" {
		return 1
	}

	return supersededDemotion
}

// compile-time assertion that Engine satisfies Retriever.
var _ Retriever = (*Engine)(nil)

// supersededByOf extracts the superseded_by uri from a document's raw
// frontmatter JSON, returning "" when the frontmatter is absent, not valid JSON,
// or carries no such key. It exists because the graph retriever builds results
// from stored documents rather than from the lexical SQL, and ADR-0010 requires
// supersession to apply on that path too: a superseded document must not be able
// to launder its demotion by arriving as a link neighbor.
func supersededByOf(frontmatterJSON string) string {
	if frontmatterJSON == "" {
		return ""
	}
	var fm map[string]any
	if err := json.Unmarshal([]byte(frontmatterJSON), &fm); err != nil {
		return ""
	}
	v, ok := fm[okfschema.SupersededByKey].(string)
	if !ok {
		return ""
	}

	return strings.TrimSpace(v)
}
