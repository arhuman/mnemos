package eval

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/chunk"
	"github.com/arhuman/mnemos/internal/storage"
)

// TestIsMarkdown table-tests the isMarkdown predicate.  The existing suite only
// exercises ".md" files through extractPairs, leaving the "return false" branch
// and the ".markdown" extension uncovered.
func TestIsMarkdown(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"doc.md", true},
		{"doc.markdown", true},
		{"DOC.MD", true},       // case-insensitive
		{"DOC.MARKDOWN", true}, // case-insensitive, alternate extension
		{"doc.txt", false},
		{"doc.go", false},
		{"noextension", false},
		{"", false},
		{"/abs/path/file.md", true},
		{"/abs/path/file.rst", false},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			require.Equal(t, tc.want, isMarkdown(tc.path))
		})
	}
}

// TestSaveBaselineSuccess verifies that saveBaseline writes valid JSON and that
// loadBaseline can reload it with exact field equality.
func TestSaveBaselineSuccess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baseline.json")
	m := Metrics{N: 5, K: 12, HitAt1: 0.8, RecallAtK: 0.9, MRRAtK: 0.85, ExactChunk: 0.6}

	require.NoError(t, saveBaseline(path, m))
	require.FileExists(t, path)

	loaded, err := loadBaseline(path)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	require.Equal(t, m, *loaded)
}

// TestSaveBaselineWriteError verifies that saveBaseline wraps and returns the
// OS error when the target directory does not exist.
func TestSaveBaselineWriteError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing_dir", "baseline.json")
	err := saveBaseline(path, Metrics{N: 1, K: 12})
	require.Error(t, err)
	require.Contains(t, err.Error(), "write baseline")
}

// TestRunNilLogger verifies that Run accepts a nil logger (substitutes a
// discard logger) and still produces the expected metrics.
func TestRunNilLogger(t *testing.T) {
	m, err := Run(context.Background(), nil, Options{
		Bundle:   filepath.Join("testdata", "bundle"),
		Include:  []string{"**/*.md"},
		Chunking: chunk.Config{TargetTokens: 700, OverlapTokens: 80},
	})
	require.NoError(t, err)
	require.Equal(t, 2, m.N)
	require.Equal(t, defaultK, m.K)
}

// TestRunEmptyBundle verifies that Run returns zero-query Metrics with the
// correct K when the bundle contains no eligible fenced code block pairs.
// index.md is skipped by extractPairs; a concept file with no code block
// produces no pairs either.
func TestRunEmptyBundle(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.md"),
		[]byte("# Index\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "prose.md"),
		[]byte("# Prose\n\nNo code blocks here.\n"), 0o644))

	m, err := Run(context.Background(), quietLogger(), Options{
		Bundle:  dir,
		Include: []string{"**/*.md"},
		K:       7,
	})
	require.NoError(t, err)
	require.Equal(t, 0, m.N)
	require.Equal(t, 7, m.K)
}

// TestRunInvalidBundle verifies that Run returns a wrapped error (containing
// "extract pairs") when the bundle directory does not exist.
func TestRunInvalidBundle(t *testing.T) {
	_, err := Run(context.Background(), quietLogger(), Options{
		Bundle: filepath.Join(t.TempDir(), "does_not_exist"),
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "extract pairs")
}

// TestRunCustomK verifies that a positive opts.K propagates into Metrics.K
// rather than being replaced by defaultK.
func TestRunCustomK(t *testing.T) {
	m, err := Run(context.Background(), quietLogger(), Options{
		Bundle:   filepath.Join("testdata", "bundle"),
		Include:  []string{"**/*.md"},
		K:        3,
		Chunking: chunk.Config{TargetTokens: 700, OverlapTokens: 80},
	})
	require.NoError(t, err)
	require.Equal(t, 3, m.K)
	require.Equal(t, 2, m.N)
}

// TestBuildRetrieverLexical tests buildRetriever directly with Semantic=false.
// It must return a non-nil Retriever and no error in both default and embed
// builds.
func TestBuildRetrieverLexical(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "lex.db")
	db, err := storage.Open(ctx, dbPath)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, storage.Migrate(db))

	r, err := buildRetriever(ctx, db, quietLogger(), Options{Semantic: false})
	require.NoError(t, err)
	require.NotNil(t, r)
}

// TestBuildRetrieverGraphExpansion asserts buildRetriever wraps the base
// retriever in a GraphRetriever when Options.GraphExpansion is set.
func TestBuildRetrieverGraphExpansion(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "graphexp.db")
	db, err := storage.Open(ctx, dbPath)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, storage.Migrate(db))

	r, err := buildRetriever(ctx, db, quietLogger(), Options{GraphExpansion: true, GraphSeedDepth: 3})
	require.NoError(t, err)
	require.NotNil(t, r)
}

// TestReportRunError asserts Report propagates a Run failure without
// touching the baseline.
func TestReportRunError(t *testing.T) {
	var buf strings.Builder
	baseline := filepath.Join(t.TempDir(), "baseline.json")

	_, err := Report(context.Background(), quietLogger(), &buf, Options{
		Bundle: filepath.Join(t.TempDir(), "does_not_exist"),
	}, baseline, true)
	require.Error(t, err)
	require.NoFileExists(t, baseline)
}

// TestReportLoadBaselineError asserts Report propagates a malformed-baseline
// decode error rather than silently ignoring it.
func TestReportLoadBaselineError(t *testing.T) {
	baseline := filepath.Join(t.TempDir(), "baseline.json")
	require.NoError(t, os.WriteFile(baseline, []byte("{not json"), 0o644))

	var buf strings.Builder
	_, err := Report(context.Background(), quietLogger(), &buf, evalOptions(t), baseline, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "decode baseline")
}

// TestReportSaveBaselineError asserts Report propagates a saveBaseline
// failure (target directory does not exist) when save is requested.
func TestReportSaveBaselineError(t *testing.T) {
	baseline := filepath.Join(t.TempDir(), "missing_dir", "baseline.json")

	var buf strings.Builder
	_, err := Report(context.Background(), quietLogger(), &buf, evalOptions(t), baseline, true)
	require.Error(t, err)
	require.Contains(t, err.Error(), "write baseline")
}
