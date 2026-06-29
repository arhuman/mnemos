package search

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strings"
	"sync"

	"github.com/arhuman/mnemos/internal/embed"
	"github.com/arhuman/mnemos/internal/model"
	"github.com/arhuman/mnemos/internal/storage"
)

// reindexBatch is the number of chunks embedded per forward pass during Reindex.
// It trades graph-recompilation overhead against peak memory; 32 matches the
// POC's batch sizing.
const reindexBatch = 32

// reindexCommitEvery bounds how many chunks a single Reindex transaction holds
// before committing and reopening a fresh one. Without this, a large corpus
// would lock SQLite's single writer for the whole reindex. Reindex is
// idempotent (upserts), so a mid-run failure that leaves earlier batches
// committed is safe — a re-run completes the remainder.
const reindexCommitEvery = 512

// VectorRetriever implements Retriever via a linear cosine scan over the stored
// embeddings. It embeds the query with an Embedder, then walks every vector for
// the embedder's model (cosine = dot, because vectors are L2-normalized) and
// returns the top-K. When the embedder is the no-op (default build) it degrades
// to returning no results so a HybridRetriever falls back to lexical-only.
type VectorRetriever struct {
	db     *sql.DB
	emb    embed.Embedder
	logger *slog.Logger
}

// NewVectorRetriever builds a VectorRetriever over db using emb. A nil logger is
// replaced with a discard logger.
func NewVectorRetriever(db *sql.DB, emb embed.Embedder, logger *slog.Logger) *VectorRetriever {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	return &VectorRetriever{db: db, emb: emb, logger: logger}
}

// Search implements Retriever. It returns the top-K chunks by cosine similarity
// to the embedded query, applying the same exact document filters as the lexical
// engine. A query the embedder cannot handle (no-op build) yields no results and
// no error, so semantic search degrades gracefully to lexical-only.
func (v *VectorRetriever) Search(ctx context.Context, q Query) ([]model.Result, error) {
	vecs, err := v.emb.Embed(ctx, []string{q.Text})
	if errors.Is(err, embed.ErrNotSupported) {
		v.logger.Debug("vector search unavailable: no embedding support")

		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("search: embed query: %w", err)
	}
	if len(vecs) == 0 {
		return nil, nil
	}
	qvec := vecs[0]

	limit := normalizeLimit(q.Limit)

	conds, filterArgs := filterClause(q)
	query := vectorScanSQL(conds)
	args := make([]any, 0, len(filterArgs)+1)
	args = append(args, v.emb.Model())
	args = append(args, filterArgs...)

	rows, err := v.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("search: vector scan: %w", err)
	}
	defer func() { _ = rows.Close() }()

	// Brute-force KNN must score every candidate vector — there is no SQL LIMIT,
	// because SQLite cannot rank by cosine, so a LIMIT would truncate to arbitrary
	// rows and silently drop the true nearest neighbours. To keep that scan cheap
	// we fetch only the id and the (small, fixed-size) vector here; the large
	// chunk content is fetched once, for the top-K winners only, in hydrate.
	//
	// Corpus-size envelope (dim=384, approximate): ~2ms at 10k chunks, ~20ms at
	// 100k, ~200ms at 1M. Above ~50k chunks, consider switching to an approximate
	// nearest-neighbour index (e.g. sqlite-vec or an external ANN library).
	scored := make([]idScore, 0, 64)
	for rows.Next() {
		var id string
		var blob []byte
		if err := rows.Scan(&id, &blob); err != nil {
			return nil, fmt.Errorf("search: scan vector row: %w", err)
		}
		score, ok, err := dotBlob(qvec, blob)
		if err != nil {
			return nil, err
		}
		if !ok {
			// Dimension drift (e.g. a stale model): skip rather than corrupt the
			// ranking.
			continue
		}
		scored = append(scored, idScore{id: id, score: score})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search: iterate vector rows: %w", err)
	}

	sortByScore(scored)
	if len(scored) > limit {
		scored = scored[:limit]
	}

	return v.hydrate(ctx, scored)
}

// idScore pairs a chunk id with its cosine score during the vector scan, before
// the heavy chunk fields are fetched for the top-K.
type idScore struct {
	id    string
	score float64
}

// sortByScore orders scores descending, breaking ties by id so the ranking is
// deterministic regardless of scan order.
func sortByScore(s []idScore) {
	slices.SortFunc(s, func(a, b idScore) int {
		if a.score != b.score {
			return cmp.Compare(b.score, a.score)
		}

		return strings.Compare(a.id, b.id)
	})
}

// hydrate fetches the full result fields (and content, for the snippet) for the
// already-ranked top-K chunk ids in a single IN query, re-attaches each score,
// and restores descending-score order (the IN query returns rows in arbitrary
// order). The id count is bounded by the search limit, well under SQLite's
// bound-parameter cap.
func (v *VectorRetriever) hydrate(ctx context.Context, scored []idScore) ([]model.Result, error) {
	if len(scored) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(scored))
	args := make([]any, len(scored))
	scoreByID := make(map[string]float64, len(scored))
	for i, s := range scored {
		placeholders[i] = "?"
		args[i] = s.id
		scoreByID[s.id] = s.score
	}

	query := `SELECT c.id, c.document_id, d.uri, d.collection, ` +
		`COALESCE(c.heading_path, ''), c.start_line, c.end_line, c.content ` +
		`FROM chunks c JOIN documents d ON d.id = c.document_id ` +
		`WHERE c.id IN (` + strings.Join(placeholders, ",") + `)`

	rows, err := v.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("search: hydrate vector results: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]model.Result, 0, len(scored))
	for rows.Next() {
		var r model.Result
		var content string
		if err := rows.Scan(
			&r.ID, &r.DocumentID, &r.URI, &r.Collection,
			&r.HeadingPath, &r.StartLine, &r.EndLine, &content,
		); err != nil {
			return nil, fmt.Errorf("search: scan hydrated row: %w", err)
		}
		r.Score = scoreByID[r.ID]
		r.Snippet = snippet(content)
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search: iterate hydrated rows: %w", err)
	}

	resultsByScore(out)

	return out, nil
}

// resultsByScore orders results descending by score, breaking ties by id to
// match sortByScore so hydration preserves the ranked order.
func resultsByScore(r []model.Result) {
	slices.SortFunc(r, func(a, b model.Result) int {
		if a.Score != b.Score {
			return cmp.Compare(b.Score, a.Score)
		}

		return strings.Compare(a.ID, b.ID)
	})
}

// HybridRetriever fuses a lexical (bm25) Retriever and a vector Retriever with
// Reciprocal Rank Fusion. It implements Retriever, so MCP/CLI callers are
// unchanged. When the vector side yields nothing (no embeddings, or a no-op
// embedder), fusion of a single non-empty list reproduces the lexical ranking —
// so hybrid degrades gracefully to bm25-only.
type HybridRetriever struct {
	lexical Retriever
	vector  Retriever
	logger  *slog.Logger
}

// NewHybridRetriever builds a HybridRetriever from a lexical and a vector
// Retriever. A nil logger is replaced with a discard logger.
func NewHybridRetriever(lexical, vector Retriever, logger *slog.Logger) *HybridRetriever {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	return &HybridRetriever{lexical: lexical, vector: vector, logger: logger}
}

// Search implements Retriever. It runs both retrievers at a widened candidate
// depth, fuses them with RRF, and truncates to q.Limit. A lexical query that
// sanitizes to empty is tolerated when the vector side still returns hits;
// ErrEmptyQuery surfaces only when neither side can produce results.
func (h *HybridRetriever) Search(ctx context.Context, q Query) ([]model.Result, error) {
	limit := normalizeLimit(q.Limit)
	pool := q
	pool.Limit = limit * overFetchFactor

	// Run the two retrievers concurrently: they are independent, and overlapping
	// them hides the query-embedding inference and the vector scan's Go-side
	// scoring behind the lexical query. (The two SQL statements still serialize on
	// the single SQLite connection; the win is the CPU work either side of them.)
	var (
		lexResults, vecResults []model.Result
		lexErr, vecErr         error
		wg                     sync.WaitGroup
	)
	wg.Go(func() {
		lexResults, lexErr = h.lexical.Search(ctx, pool)
	})
	wg.Go(func() {
		vecResults, vecErr = h.vector.Search(ctx, pool)
	})
	wg.Wait()

	lexEmpty := errors.Is(lexErr, ErrEmptyQuery)
	if lexErr != nil && !lexEmpty {
		return nil, lexErr
	}
	if vecErr != nil {
		return nil, vecErr
	}

	if lexEmpty && len(vecResults) == 0 {
		return nil, ErrEmptyQuery
	}

	// Lexical first so its highlighted snippet wins on merged chunks.
	fused := reciprocalRankFusion([][]model.Result{lexResults, vecResults}, rrfK)
	h.logger.Debug("hybrid fused", "lexical", len(lexResults), "vector", len(vecResults), "fused", len(fused))
	if len(fused) > limit {
		fused = fused[:limit]
	}

	return fused, nil
}

// Reindex (re)computes and stores a vector for every chunk in db using emb. It
// embeds in batches and commits every reindexCommitEvery chunks so the single
// SQLite writer is not held for the whole run; the ON DELETE CASCADE on
// embeddings means re-ingesting a document already drops its stale vectors, so
// Reindex only needs to (re)write current chunks. It returns the number of
// vectors written.
func Reindex(ctx context.Context, db *sql.DB, emb embed.Embedder, logger *slog.Logger) (int, error) {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	refs, err := storage.AllChunkRefs(ctx, db)
	if err != nil {
		return 0, err
	}
	if len(refs) == 0 {
		return 0, nil
	}

	modelName := emb.Model()
	dim := emb.Dim()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("search: reindex begin tx: %w", err)
	}

	written := 0
	sinceCommit := 0
	for start := 0; start < len(refs); start += reindexBatch {
		if err := ctx.Err(); err != nil {
			_ = tx.Rollback()

			return 0, err
		}
		end := min(start+reindexBatch, len(refs))
		batch := refs[start:end]

		texts := make([]string, len(batch))
		for i, r := range batch {
			texts[i] = r.Content
		}
		vecs, err := emb.Embed(ctx, texts)
		if err != nil {
			_ = tx.Rollback()

			return 0, fmt.Errorf("search: reindex embed batch: %w", err)
		}
		for i, ref := range batch {
			blob := storage.EncodeVector(vecs[i])
			if uerr := storage.UpsertEmbedding(ctx, tx, ref.ID, modelName, dim, blob); uerr != nil {
				_ = tx.Rollback()

				return 0, uerr
			}
			written++
			sinceCommit++
		}
		logger.Debug("reindex batch", "done", end, "total", len(refs))

		// Commit periodically and reopen a fresh transaction so the single
		// writer lock is released between super-batches, not held for the run.
		if sinceCommit < reindexCommitEvery || end >= len(refs) {
			continue
		}
		if cerr := tx.Commit(); cerr != nil {
			return 0, fmt.Errorf("search: reindex commit: %w", cerr)
		}
		sinceCommit = 0
		tx, err = db.BeginTx(ctx, nil)
		if err != nil {
			return 0, fmt.Errorf("search: reindex begin tx: %w", err)
		}
	}

	if cerr := tx.Commit(); cerr != nil {
		return 0, fmt.Errorf("search: reindex commit: %w", cerr)
	}

	return written, nil
}

// vectorScanSQL builds the linear-scan query: every embedding for the requested
// model, selecting only the chunk id and its vector. When document filters are
// present the query joins back to chunks and documents so the exact filters can
// be applied; when there are none it scans the embeddings table alone, since the
// chunk id and vector both live there. There is no LIMIT — the top-K is selected
// in Go after computing cosine for each row (SQLite cannot rank by cosine). The
// heavy chunk content is deliberately not selected here; it is fetched for the
// top-K only, in hydrate. The leading "?" is the model argument; filter args
// follow.
func vectorScanSQL(filterConds []string) string {
	// Unfiltered scan (the common recall path): the chunk id and the vector both
	// live in the embeddings row, so neither join is needed — every row is scored
	// regardless. The joins exist only to reach documents for the filter clause.
	if len(filterConds) == 0 {
		return `SELECT e.chunk_id, e.vector FROM embeddings e WHERE e.model = ?`
	}

	var b strings.Builder
	_, _ = b.WriteString(`SELECT e.chunk_id, e.vector ` +
		`FROM embeddings e ` +
		`JOIN chunks c ON c.id = e.chunk_id ` +
		`JOIN documents d ON d.id = c.document_id ` +
		`WHERE e.model = ?`)
	for _, cond := range filterConds {
		_, _ = b.WriteString(" AND ")
		_, _ = b.WriteString(cond)
	}

	return b.String()
}

// dotBlob computes the dot product of query vector q with a little-endian
// float32 vector blob (the storage encoding) without first decoding the blob
// into a []float32. For L2-normalized vectors this is their cosine similarity.
// Fusing decode and dot into one pass avoids allocating a per-row slice for
// every candidate in the linear scan — the scan's dominant heap cost.
//
// ok is false when the blob's dimension does not match q (dimension drift from a
// stale model), so the caller can skip the row; err is returned only for a
// structurally corrupt blob whose length is not a multiple of four.
func dotBlob(q []float32, blob []byte) (score float64, ok bool, err error) {
	if len(blob)%4 != 0 {
		return 0, false, fmt.Errorf("search: vector blob length %d is not a multiple of 4", len(blob))
	}
	if len(blob)/4 != len(q) {
		return 0, false, nil
	}

	var sum float64
	for i, qi := range q {
		f := math.Float32frombits(binary.LittleEndian.Uint32(blob[i*4:]))
		sum += float64(qi) * float64(f)
	}

	return sum, true, nil
}

// snippet renders a short single-line preview from chunk content for vector hits
// (which have no FTS-highlighted snippet). It collapses whitespace and truncates.
func snippet(content string) string {
	const maxLen = 200
	s := strings.Join(strings.Fields(content), " ")
	if len(s) > maxLen {
		s = s[:maxLen] + " …"
	}

	return s
}

// compile-time assertions that the retrievers satisfy Retriever.
var (
	_ Retriever = (*VectorRetriever)(nil)
	_ Retriever = (*HybridRetriever)(nil)
)
