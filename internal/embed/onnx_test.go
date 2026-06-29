//go:build embed

package embed

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// testModelDir resolves the model directory for the embed end-to-end tests. It
// honors MNEMOS_TEST_MODEL_DIR, then falls back to the canonical install path
// (~/.mnemos/models/all-MiniLM-L6-v2). The tests skip when no model is present
// so a checkout without weights still passes `go test -tags embed`.
func testModelDir(t *testing.T) string {
	t.Helper()
	if dir := os.Getenv("MNEMOS_TEST_MODEL_DIR"); dir != "" {
		return dir
	}
	dir, err := ModelDir(DefaultModel)
	require.NoError(t, err)
	return dir
}

func newTestEmbedder(t *testing.T) Embedder {
	t.Helper()
	dir := testModelDir(t)
	if _, err := os.Stat(filepath.Join(dir, "model.onnx")); err != nil {
		t.Skipf("model not present at %s (run: mnemos models install %s): %v", dir, DefaultModel, err)
	}
	e, err := New(dir)
	require.NoError(t, err)
	return e
}

func cosine(a, b []float32) float64 {
	var d float64
	for i := range a {
		d += float64(a[i]) * float64(b[i])
	}
	return d // both vectors are L2-normalized
}

func TestEmbedDimAndNorm(t *testing.T) {
	e := newTestEmbedder(t)
	vecs, err := e.Embed(context.Background(), []string{"a dog is running"})
	require.NoError(t, err)
	require.Len(t, vecs, 1)
	require.Len(t, vecs[0], Dim, "embedding must be 384-dim")

	var n float64
	for _, v := range vecs[0] {
		n += float64(v) * float64(v)
	}
	require.InDelta(t, 1.0, math.Sqrt(n), 1e-4, "L2 norm must be ~1.0")
}

func TestEmbedSemanticSanity(t *testing.T) {
	e := newTestEmbedder(t)
	vecs, err := e.Embed(context.Background(), []string{
		"a dog is running",
		"a puppy runs",
		"the stock market crashed",
	})
	require.NoError(t, err)
	require.Len(t, vecs, 3)

	simRelated := cosine(vecs[0], vecs[1])
	simUnrelated := cosine(vecs[0], vecs[2])
	t.Logf("cosine(dog running, puppy runs)     = %.4f", simRelated)
	t.Logf("cosine(dog running, market crashed) = %.4f", simUnrelated)
	require.Greater(t, simRelated, simUnrelated,
		"related sentences must be more similar than unrelated")
	require.Greater(t, simRelated-simUnrelated, 0.1,
		"semantic gap should be clear (>0.1)")
}

func TestEmbedEmptyInput(t *testing.T) {
	e := newTestEmbedder(t)
	vecs, err := e.Embed(context.Background(), nil)
	require.NoError(t, err)
	require.Nil(t, vecs)
}
