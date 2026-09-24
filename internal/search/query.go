// Package search implements V0 retrieval over the SQLite FTS5 index. It exposes
// a Retriever seam (bm25 now, hybrid later) and an FTS5-backed engine that
// MATCHes a sanitized query, ranks with bm25() column weights, applies small
// deterministic Go-side boosts, and enforces exact document filters.
package search

import "time"

// Query is a single retrieval request. Text is the raw user query (sanitized
// into FTS5 barewords by the engine). The remaining fields are exact document
// filters compiled into the SQL WHERE clause; the zero value of each means "no
// constraint". Limit caps the number of returned results.
type Query struct {
	// Text is the raw user query string. It may contain arbitrary punctuation;
	// the engine sanitizes it before handing it to FTS5 MATCH.
	Text string
	// Collection, when set, restricts results to documents.collection = it.
	Collection string
	// PathPrefix, when set, restricts results to documents whose uri starts
	// with it (LIKE prefix match).
	PathPrefix string
	// FileType, when set, restricts results to documents whose uri ends with a
	// matching extension (e.g. "md" or ".md").
	FileType string
	// ModifiedSince, when set, restricts results to documents.modified_at >= it.
	// It is compared lexically, so an RFC3339 timestamp sorts correctly.
	ModifiedSince string
	// ExcludeCollections, when non-empty, drops every result whose
	// documents.collection is in the list. It is the server-side visibility
	// boundary ([security].visibility.deny): the memory service sets it from config
	// on every query so a hidden collection can never surface, independent of the
	// caller-supplied Collection filter.
	ExcludeCollections []string
	// Limit caps the result count. The caller supplies the configured default
	// when it is not overridden on the command line.
	Limit int
	// TemporalWeight, in [0,1], is how much a document's age discounts its score.
	// Zero (the default) disables temporal ranking entirely, so an unset Query
	// scores exactly as it did before recency existed: the feature can never
	// silently reorder a caller that did not ask for it. One means a document
	// older than several halflives is discounted to near nothing.
	TemporalWeight float64
	// TemporalHalflife is the age at which a document keeps half its recency
	// factor. Zero uses defaultTemporalHalflife. Ignored when TemporalWeight is 0.
	TemporalHalflife time.Duration
	// Now is the instant ages are measured against. The zero value means
	// time.Now(); tests set it so a decay assertion does not depend on the clock.
	Now time.Time
}

// overFetchFactor is how many candidates beyond the requested limit each
// retriever pulls before re-ranking and truncating. The lexical engine needs the
// surplus so the Go-side heading boost can promote a chunk the bm25 ORDER BY
// ranked just outside the top-N; the hybrid retriever needs it so RRF has enough
// overlap between the lexical and vector candidate lists to reinforce. Same
// purpose — rank on a wider pool, then cut to limit — so one factor governs both.
const overFetchFactor = 4

// defaultTemporalHalflife is the age at which a document keeps half its recency
// factor when a caller enables temporal ranking without naming a halflife. One
// week matches the rhythm of project memory: a decision from this week is live
// context, one from last month is history worth finding but not worth ranking
// above today's.
const defaultTemporalHalflife = 168 * time.Hour

// normalizeLimit returns a usable result limit: the caller's value, or 1 when it
// is unset (zero or negative). Every retriever applies the same floor so an
// omitted limit never produces an empty or negative fetch.
func normalizeLimit(limit int) int {
	if limit <= 0 {
		return 1
	}

	return limit
}
