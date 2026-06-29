package search

import (
	"errors"
	"strconv"
	"strings"

	"github.com/arhuman/mnemos/internal/storage"
)

// bm25 column weights, in the FTS5 column order of chunks_fts
// (content, heading_path, tags, doc_type). Heading and tags are boosted so a
// term appearing in a section title or tag set outranks the same term buried in
// body prose. They are named consts so ranking stays tunable in one place.
const (
	weightContent     = 1.0
	weightHeadingPath = 2.0
	weightTags        = 1.5
	weightDocType     = 1.0
)

// headingBoost is added to a result's score when a query term also appears in
// its heading_path. It is a small, deterministic nudge layered on top of the
// bm25 rank (the plan's "title/heading match boost"); kept modest so bm25
// remains the dominant signal.
const headingBoost = 1.0

// ErrEmptyQuery is returned when a query reduces to no searchable terms after
// sanitization (e.g. it was empty or pure punctuation).
var ErrEmptyQuery = errors.New("search: empty query after sanitization")

// sanitizeMatch turns an arbitrary user query into a safe FTS5 MATCH expression.
// FTS5 treats characters like ", *, :, (, ), -, AND/OR/NOT as syntax; a raw user
// string ("scim: provisioning (entra)") would raise a syntax error. We defend by
// tokenizing into barewords (alphanumerics plus a few safe inner characters),
// dropping everything else, double-quoting each token (so even a token that
// happens to be an FTS operator is treated as a literal), and joining with
// spaces. Space is implicit AND in FTS5, so the result is an AND-of-terms query
// that can never be a syntax error. Returns ErrEmptyQuery when nothing remains.
func sanitizeMatch(query string) (string, error) {
	tokens := tokenize(query)
	if len(tokens) == 0 {
		return "", ErrEmptyQuery
	}
	quoted := make([]string, 0, len(tokens))
	for _, t := range tokens {
		// Double any embedded quote so the token stays a single FTS5 string.
		quoted = append(quoted, `"`+strings.ReplaceAll(t, `"`, `""`)+`"`)
	}

	return strings.Join(quoted, " "), nil
}

// tokenize splits s into barewords. A bareword run is a maximal sequence of
// letters, digits, and the inner-safe characters '_', '-', '.' (so identifiers,
// versions, and dotted names survive); every other rune is a separator. Leading
// and trailing '-'/'.' are trimmed so a stray separator never starts a token.
func tokenize(s string) []string {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return !isBareword(r)
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.Trim(f, "-.")
		if f != "" {
			out = append(out, f)
		}
	}

	return out
}

// isBareword reports whether r may appear inside an FTS5 bareword token.
func isBareword(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	case r == '_' || r == '-' || r == '.':
		return true
	default:
		// Allow non-ASCII letters/digits so accented words remain searchable.
		return r > 127
	}
}

// queryTerms returns the lowercased bareword terms of a query, used by the
// Go-side heading boost. It mirrors tokenize but normalizes case for matching.
func queryTerms(query string) []string {
	tokens := tokenize(query)
	for i := range tokens {
		tokens[i] = strings.ToLower(tokens[i])
	}

	return tokens
}

// filterClause builds the additional WHERE conditions and their bound arguments
// for the exact filters on a Query. The MATCH predicate and its argument are
// added by the engine; this only contributes the document-level filters so
// values are always parameterized (never string-interpolated).
func filterClause(q Query) (conds []string, args []any) {
	if q.Collection != "" {
		conds = append(conds, "d.collection = ?")
		args = append(args, q.Collection)
	}
	if q.PathPrefix != "" {
		conds = append(conds, `d.uri LIKE ? ESCAPE '\'`)
		args = append(args, storage.EscapeLike(q.PathPrefix)+"%")
	}
	if q.FileType != "" {
		conds = append(conds, `d.uri LIKE ? ESCAPE '\'`)
		args = append(args, "%."+storage.EscapeLike(strings.TrimPrefix(q.FileType, ".")))
	}
	if q.ModifiedSince != "" {
		conds = append(conds, "d.modified_at >= ?")
		args = append(args, q.ModifiedSince)
	}

	return conds, args
}

// searchSQL assembles the FTS5 retrieval statement. It joins the FTS virtual
// table back to chunks and documents, ranks with bm25() column weights, exposes
// a highlighted snippet, applies any filter conditions, and orders by rank
// (ascending: SQLite bm25 returns negative values where more-negative = better).
// The leading "?" placeholder is the MATCH argument; filterArgs follow, then the
// trailing limit. score is computed Go-side as -rank so higher = better.
func searchSQL(filterConds []string) string {
	var b strings.Builder
	_, _ = b.WriteString(`SELECT c.id, c.document_id, d.uri, d.collection, ` +
		`COALESCE(c.heading_path, ''), c.start_line, c.end_line, ` +
		`snippet(chunks_fts, 0, '[', ']', ' … ', 12) AS snip, ` +
		`bm25(chunks_fts, `)
	_, _ = b.WriteString(formatWeights())
	_, _ = b.WriteString(`) AS rank ` +
		`FROM chunks_fts ` +
		`JOIN chunks c ON c.rowid = chunks_fts.rowid ` +
		`JOIN documents d ON d.id = c.document_id ` +
		`WHERE chunks_fts MATCH ?`)
	for _, cond := range filterConds {
		_, _ = b.WriteString(" AND ")
		_, _ = b.WriteString(cond)
	}
	_, _ = b.WriteString(" ORDER BY rank LIMIT ?")

	return b.String()
}

// formatWeights renders the bm25 column-weight arguments in column order. The
// weights are compile-time constants, so embedding them in the SQL text is safe
// (no user input is interpolated).
func formatWeights() string {
	return strings.Join([]string{
		formatFloat(weightContent),
		formatFloat(weightHeadingPath),
		formatFloat(weightTags),
		formatFloat(weightDocType),
	}, ", ")
}

// formatFloat renders a weight constant without a trailing exponent.
func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}
