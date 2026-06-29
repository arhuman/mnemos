// Package search implements V0 retrieval over the SQLite FTS5 index. It exposes
// a Retriever seam (bm25 now, hybrid later) and an FTS5-backed engine that
// MATCHes a sanitized query, ranks with bm25() column weights, applies small
// deterministic Go-side boosts, and enforces exact document filters.
package search

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
	// Limit caps the result count. The caller supplies the configured default
	// when it is not overridden on the command line.
	Limit int
}

// overFetchFactor is how many candidates beyond the requested limit each
// retriever pulls before re-ranking and truncating. The lexical engine needs the
// surplus so the Go-side heading boost can promote a chunk the bm25 ORDER BY
// ranked just outside the top-N; the hybrid retriever needs it so RRF has enough
// overlap between the lexical and vector candidate lists to reinforce. Same
// purpose — rank on a wider pool, then cut to limit — so one factor governs both.
const overFetchFactor = 4

// normalizeLimit returns a usable result limit: the caller's value, or 1 when it
// is unset (zero or negative). Every retriever applies the same floor so an
// omitted limit never produces an empty or negative fetch.
func normalizeLimit(limit int) int {
	if limit <= 0 {
		return 1
	}

	return limit
}
