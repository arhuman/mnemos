package search_test

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/chunk"
	"github.com/arhuman/mnemos/internal/ingest"
	"github.com/arhuman/mnemos/internal/search"
	"github.com/arhuman/mnemos/internal/storage"
)

// bundleDir returns the absolute path to the vendored real OKF bundle used as a
// realistic ingest+search fixture (examples/onpage-seo/bundle, see its
// NOTICE.md). It is resolved relative to this test file so the test is
// cwd-independent.
func bundleDir(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)

	return filepath.Join(filepath.Dir(thisFile), "..", "..", "examples", "onpage-seo", "bundle")
}

// TestVendoredBundleIngestsAndSearches guards the vendored On-Page SEO bundle: it
// must ingest cleanly through the real pipeline and stay retrievable by a lexical
// keyword query, exercising mnemos over genuine OKF content rather than a
// hand-rolled fixture. It also pins the bundle's shape so an accidental edit that
// drops or corrupts a file is caught.
func TestVendoredBundleIngestsAndSearches(t *testing.T) {
	ctx := context.Background()

	dbPath := filepath.Join(t.TempDir(), "bundle.db")
	db, err := storage.Open(ctx, dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, storage.Migrate(db))

	sum, err := ingest.New(db, discardLogger()).Run(ctx, ingest.Options{
		Root:       bundleDir(t),
		Collection: "seo",
		Rules:      ingest.Rules{Include: []string{"**/*.md"}},
		Chunking:   chunk.Config{TargetTokens: 700, OverlapTokens: 80},
	})
	require.NoError(t, err)
	require.Equal(t, 16, sum.FilesIngested)
	require.Greater(t, sum.ChunksWritten, 0)

	results, err := search.NewEngine(db, discardLogger()).Search(ctx, search.Query{
		Text:       "core web vitals",
		Collection: "seo",
		Limit:      5,
	})
	require.NoError(t, err)
	require.NotEmpty(t, results)
	require.Contains(t, results[0].URI, "core-web-vitals")
}
