package search

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/arhuman/mnemos/internal/storage"
)

// SimilarResult is one document semantically near another, scored by the cosine
// similarity of the best-matching chunk pair.
type SimilarResult struct {
	URI        string
	Title      string
	Collection string
	Score      float64
}

// Similar returns the documents nearest to the one at uri, ranked by cosine
// similarity, excluding the source document itself. It mean-pools the source
// document's own already-stored chunk vectors into a single query vector rather
// than re-embedding it, so it needs no embedder and no ONNX model — only vectors
// that `reindex --embeddings` already wrote.
//
// A source document with no stored vectors yields (nil, nil), not an error: that
// is the ordinary state of a corpus that has never been embedded, and a caller
// should render it as "unavailable" rather than as a failure.
func Similar(ctx context.Context, db *sql.DB, modelName, uri string, limit int) ([]SimilarResult, error) {
	doc, err := storage.GetDocumentByURI(ctx, db, uri)
	if err != nil {
		return nil, fmt.Errorf("search: similar document: %w", err)
	}
	if doc == nil {
		return nil, fmt.Errorf("search: similar: unknown uri %q", uri)
	}

	vectors, err := storage.DocChunkVectors(ctx, db, doc.ID, modelName)
	if err != nil {
		return nil, err
	}
	qvec := meanPool(vectors)
	if qvec == nil {
		return nil, nil
	}

	rows, err := db.QueryContext(ctx, `
		SELECT d.uri, COALESCE(d.title, ''), d.collection, e.vector
		FROM embeddings e
		JOIN chunks c ON c.id = e.chunk_id
		JOIN documents d ON d.id = c.document_id
		WHERE e.model = ? AND c.document_id != ?
	`, modelName, doc.ID)
	if err != nil {
		return nil, fmt.Errorf("search: similar scan: %w", err)
	}
	defer func() { _ = rows.Close() }()

	// A document's chunks each score separately; the document's rank is its best
	// chunk, so a long document does not outrank a focused one on volume alone.
	best := make(map[string]SimilarResult)
	for rows.Next() {
		var r SimilarResult
		var blob []byte
		if err := rows.Scan(&r.URI, &r.Title, &r.Collection, &blob); err != nil {
			return nil, fmt.Errorf("search: scan similar row: %w", err)
		}
		score, ok, err := dotBlob(qvec, blob)
		if err != nil {
			return nil, err
		}
		// Dimension drift (a stale model): skip rather than corrupt the ranking.
		if !ok {
			continue
		}
		r.Score = score
		if prev, seen := best[r.URI]; !seen || score > prev.Score {
			best[r.URI] = r
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("search: iterate similar rows: %w", err)
	}

	out := make([]SimilarResult, 0, len(best))
	for _, r := range best {
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b SimilarResult) int {
		if a.Score != b.Score {
			return cmp.Compare(b.Score, a.Score)
		}

		return strings.Compare(a.URI, b.URI)
	})

	if n := normalizeLimit(limit); len(out) > n {
		out = out[:n]
	}

	return out, nil
}

// meanPool averages a document's chunk vectors into one L2-normalized document
// vector, so the cosine scan can treat it exactly like an embedded query. It
// returns nil when there is nothing to pool, or when the average degenerates to
// the zero vector (no meaningful direction to search along).
func meanPool(vectors []storage.ChunkVector) []float32 {
	if len(vectors) == 0 {
		return nil
	}
	dim := len(vectors[0].Vector)
	if dim == 0 {
		return nil
	}

	sum := make([]float64, dim)
	pooled := 0
	for _, cv := range vectors {
		// Dimension drift within one document: pool only the consistent vectors.
		if len(cv.Vector) != dim {
			continue
		}
		for i, v := range cv.Vector {
			sum[i] += float64(v)
		}
		pooled++
	}
	if pooled == 0 {
		return nil
	}

	var norm float64
	for _, v := range sum {
		norm += v * v
	}
	norm = math.Sqrt(norm)
	if norm == 0 {
		return nil
	}

	out := make([]float32, dim)
	for i, v := range sum {
		out[i] = float32(v / norm)
	}

	return out
}
