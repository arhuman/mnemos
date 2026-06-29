package search_test

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/model"
	"github.com/arhuman/mnemos/internal/search"
	"github.com/arhuman/mnemos/internal/storage"
)

// benchDim matches the production embedding width (384, all-MiniLM-style) so the
// per-row decode and dot-product cost in the benchmarks reflects real vectors,
// not the toy widths used by the correctness tests.
const benchDim = 384

// benchVec builds a deterministic dense, L2-normalized vector of width dim. Real
// embeddings are dense and normalized (so dot == cosine); a one-hot vector would
// understate both the decode allocation and the dot-product work the scan does.
func benchVec(dim, seed int) []float32 {
	v := make([]float32, dim)
	var norm float64
	for j := range v {
		x := float32((j*7+seed*13)%17) - 8
		v[j] = x
		norm += float64(x) * float64(x)
	}
	if norm == 0 {
		return v
	}
	inv := 1 / math.Sqrt(norm)
	for j := range v {
		v[j] = float32(float64(v[j]) * inv)
	}

	return v
}

// benchDB builds a single-document corpus of `chunks` chunks, each with an FTS
// row and a dense `dim`-dimensional embedding, for the retrieval benchmarks. It
// backs the "measure before adding an ANN index" decision in ADR 0003.
func benchDB(b *testing.B, chunks, dim int) *sql.DB {
	b.Helper()
	db, err := storage.Open(context.Background(), filepath.Join(b.TempDir(), "bench.db"))
	require.NoError(b, err)
	b.Cleanup(func() { _ = db.Close() })
	require.NoError(b, storage.Migrate(db))

	cs := make([]model.Chunk, chunks)
	for i := range cs {
		cs[i] = model.Chunk{
			ID: fmt.Sprintf("c%d", i), DocumentID: "d", Ordinal: i,
			Content: fmt.Sprintf("alpha beta gamma entra scim provisioning chunk %d", i),
		}
	}

	tx, err := db.BeginTx(context.Background(), nil)
	require.NoError(b, err)
	require.NoError(b, storage.UpsertDocument(context.Background(), tx, model.Document{
		ID: "d", URI: "a.md", Collection: "c", ContentHash: "h", IndexedAt: "t",
	}))
	require.NoError(b, storage.ReplaceChunks(context.Background(), tx, "d", cs))
	require.NoError(b, tx.Commit())

	vtx, err := db.BeginTx(context.Background(), nil)
	require.NoError(b, err)
	for i := range cs {
		require.NoError(b, storage.UpsertEmbedding(
			context.Background(), vtx, cs[i].ID, "m", dim, storage.EncodeVector(benchVec(dim, i))))
	}
	require.NoError(b, vtx.Commit())

	return db
}

func BenchmarkLexicalSearch(b *testing.B) {
	db := benchDB(b, 2000, benchDim)
	eng := search.NewEngine(db, nil)
	q := search.Query{Text: "entra scim provisioning", Limit: 12}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := eng.Search(context.Background(), q); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkVectorSearch sweeps corpus size against the ADR-0003 threshold
// (~10k chunks) and runs both an unfiltered scan and a collection-filtered one,
// so the cost of the linear scan — and of the document JOINs only the filtered
// path needs — is visible. ReportAllocs surfaces the per-row vector decode.
func BenchmarkVectorSearch(b *testing.B) {
	for _, n := range []int{2000, 10000} {
		b.Run(fmt.Sprintf("chunks=%d/unfiltered", n), func(b *testing.B) {
			runVectorBench(b, n, search.Query{Text: "x", Limit: 12})
		})
		b.Run(fmt.Sprintf("chunks=%d/filtered", n), func(b *testing.B) {
			runVectorBench(b, n, search.Query{Text: "x", Limit: 12, Collection: "c"})
		})
	}
}

func runVectorBench(b *testing.B, n int, q search.Query) {
	db := benchDB(b, n, benchDim)
	vr := search.NewVectorRetriever(db, stubEmbedder{vec: benchVec(benchDim, 0)}, nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := vr.Search(context.Background(), q); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkHybridSearch measures the fused lexical+vector path end to end, so the
// effect of running the two retrievers concurrently (rather than back to back) is
// visible at the same corpus sizes as the vector-only sweep.
func BenchmarkHybridSearch(b *testing.B) {
	for _, n := range []int{2000, 10000} {
		b.Run(fmt.Sprintf("chunks=%d", n), func(b *testing.B) {
			db := benchDB(b, n, benchDim)
			eng := search.NewEngine(db, nil)
			vr := search.NewVectorRetriever(db, stubEmbedder{vec: benchVec(benchDim, 0)}, nil)
			h := search.NewHybridRetriever(eng, vr, nil)
			q := search.Query{Text: "entra scim provisioning", Limit: 12}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := h.Search(context.Background(), q); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
