package eval

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/chunk"
)

// quietLogger discards all log output for the duration of a test.
func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// evalOptions builds Options for the testdata bundle with markdown indexing and
// the standard chunk budget.
func evalOptions(t *testing.T) Options {
	t.Helper()

	return Options{
		Bundle:   filepath.Join("testdata", "bundle"),
		Include:  []string{"**/*.md"},
		Chunking: chunk.Config{TargetTokens: 700, OverlapTokens: 80},
	}
}

// TestExtractPairs asserts the example-query derivation: one pair per fenced
// block, index.md skipped, expected uri == host file.
func TestExtractPairs(t *testing.T) {
	pairs, err := extractPairs(filepath.Join("testdata", "bundle"))
	require.NoError(t, err)
	require.Len(t, pairs, 2)

	byURI := make(map[string]pair)
	for _, p := range pairs {
		byURI[p.expectedURI] = p
	}
	require.Contains(t, byURI, "scim.md")
	require.Contains(t, byURI, "entra.md")
	require.NotContains(t, byURI, "index.md")
	require.Contains(t, byURI["scim.md"].queryText, "scim provisioning")
}

// TestBuildHeldOutStripsBlocks asserts the held-out copy removes the exact
// example-query block from each host file while keeping prose and headings.
func TestBuildHeldOutStripsBlocks(t *testing.T) {
	bundle := filepath.Join("testdata", "bundle")
	pairs, err := extractPairs(bundle)
	require.NoError(t, err)

	dir := t.TempDir()
	_, err = buildHeldOut(bundle, dir, pairs)
	require.NoError(t, err)

	scim, err := os.ReadFile(filepath.Join(dir, "scim.md"))
	require.NoError(t, err)
	body := string(scim)

	// The verbatim query line must be gone from the held-out copy.
	require.NotContains(t, body, "scim provisioning synchronize user accounts identity provider")
	// Prose and headings must remain.
	require.Contains(t, body, "# SCIM Provisioning")
	require.Contains(t, body, "synchronizes user accounts")
}

// TestRunComputesSaneMetrics runs the full held-out evaluation and asserts all
// metrics are computed and within [0,1].
func TestRunComputesSaneMetrics(t *testing.T) {
	m, err := Run(context.Background(), quietLogger(), evalOptions(t))
	require.NoError(t, err)

	require.Equal(t, 2, m.N)
	require.Equal(t, 12, m.K)
	for _, v := range []float64{m.HitAt1, m.RecallAtK, m.MRRAtK, m.ExactChunk} {
		require.GreaterOrEqual(t, v, 0.0)
		require.LessOrEqual(t, v, 1.0)
	}
}

// TestReportBaselineRoundTrip asserts saving then reloading a baseline yields
// delta annotations in the report.
func TestReportBaselineRoundTrip(t *testing.T) {
	baseline := filepath.Join(t.TempDir(), "baseline.json")
	opts := evalOptions(t)

	// First run saves the baseline.
	var buf strings.Builder
	_, err := Report(context.Background(), quietLogger(), &buf, opts, baseline, true)
	require.NoError(t, err)
	require.FileExists(t, baseline)
	require.Contains(t, buf.String(), "saved baseline")

	// Second run loads it and prints deltas.
	var buf2 strings.Builder
	_, err = Report(context.Background(), quietLogger(), &buf2, opts, baseline, false)
	require.NoError(t, err)
	require.Contains(t, buf2.String(), "vs baseline")
}
