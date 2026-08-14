package eval

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/chunk"
)

// graphBundle is the shipped graph-answerability bundle, evaluated as the real
// artifact rather than a private testdata copy.
func graphBundle() string {
	return filepath.Join("..", "..", "examples", "graph-answerability", "bundle")
}

func graphOptions() GraphOptions {
	return GraphOptions{
		Bundle:   graphBundle(),
		Include:  []string{"**/*.md"},
		Chunking: chunk.Config{TargetTokens: 700, OverlapTokens: 80},
	}
}

// TestLoadGraphCases asserts the curated cases parse and carry both fields.
func TestLoadGraphCases(t *testing.T) {
	cases, err := loadGraphCases(graphBundle())
	require.NoError(t, err)
	require.Len(t, cases, 5)
	for _, c := range cases {
		require.NotEmpty(t, c.Query)
		require.NotEmpty(t, c.ExpectedURI)
	}
}

// TestRunGraphInclusionShowsLift is the core measurement: neighborhood injection
// must recover answer docs that plain lexical search misses, so inclusion beats
// direct hit. The per-case invariants are structural: the "secrets" case is a
// direct hit (no lift there), the three 1-hop cases are recovered, and the 2-hop
// case is the expected ceiling miss for 1-hop injection.
func TestRunGraphInclusionShowsLift(t *testing.T) {
	m, results, err := RunGraphInclusion(context.Background(), quietLogger(), graphOptions())
	require.NoError(t, err)

	require.Equal(t, 5, m.N)
	require.Equal(t, defaultGraphK, m.K)
	for _, v := range []float64{m.DirectHitAtK, m.NeighborInclusion, m.RecoveredMisses} {
		require.GreaterOrEqual(t, v, 0.0)
		require.LessOrEqual(t, v, 1.0)
	}

	// The whole point: injection adds value over plain search.
	require.Greater(t, m.NeighborInclusion, m.DirectHitAtK, "inclusion should beat direct hit")
	require.Greater(t, m.Lift, 0.0, "injection should lift answerability")
	require.Greater(t, m.RecoveredMisses, 0.0, "injection should recover some search misses")

	// The actual GraphRetriever must realize the proxy: graph hit@K beats plain
	// direct hit@K, confirming Phase 2 recovers link-only answers.
	require.Greater(t, m.GraphHitAtK, m.DirectHitAtK, "graph retriever should beat plain search")

	byURI := make(map[string]GraphCaseResult, len(results))
	for _, r := range results {
		byURI[r.ExpectedURI] = r
	}

	// Control: the answer is the direct lexical hit, so injection is not needed.
	secrets := byURI["security/secrets.md"]
	require.True(t, secrets.DirectHit, "secrets case should be a direct hit")

	// 1-hop: search misses the answer, injection recovers it.
	revert := byURI["runbooks/revert-release.md"]
	require.False(t, revert.DirectHit, "revert answer should not be a direct lexical hit")
	require.True(t, revert.Included, "revert answer should be recovered via a 1-hop link")

	// 2-hop ceiling: 1-hop injection cannot reach it.
	throttle := byURI["runbooks/throttle-tenant.md"]
	require.False(t, throttle.Included, "2-hop answer should be beyond 1-hop injection")
}

// TestGraphSeedDepthDefaultsToK asserts the seed depth defaults to K when unset.
func TestGraphSeedDepthDefaultsToK(t *testing.T) {
	m, _, err := RunGraphInclusion(context.Background(), quietLogger(), graphOptions())
	require.NoError(t, err)
	require.Equal(t, m.K, m.SeedDepth)
}

// TestLoadGraphCasesMissingFile asserts a bundle without cases.json fails with
// a wrapped read error.
func TestLoadGraphCasesMissingFile(t *testing.T) {
	_, err := loadGraphCases(t.TempDir())
	require.Error(t, err)
	require.Contains(t, err.Error(), "read cases.json")
}

// TestLoadGraphCasesMalformedJSON asserts invalid JSON in cases.json fails
// with a wrapped parse error.
func TestLoadGraphCasesMalformedJSON(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cases.json"), []byte("{not json"), 0o644))

	_, err := loadGraphCases(dir)
	require.Error(t, err)
	require.Contains(t, err.Error(), "parse cases.json")
}

// TestLoadGraphCasesMissingFields asserts a case missing query or expected_uri
// fails with the "needs both" validation error.
func TestLoadGraphCasesMissingFields(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cases.json"),
		[]byte(`{"cases":[{"query":"","expected_uri":"foo.md"}]}`), 0o644))

	_, err := loadGraphCases(dir)
	require.Error(t, err)
	require.Contains(t, err.Error(), "needs both query and expected_uri")
}

// TestReportGraph asserts ReportGraph renders the aggregate metrics and a
// per-case breakdown covering all three outcome labels, exercising
// writeGraphReport and graphCaseLabel end to end.
func TestReportGraph(t *testing.T) {
	var buf bytes.Buffer
	m, err := ReportGraph(context.Background(), quietLogger(), &buf, graphOptions())
	require.NoError(t, err)
	require.Equal(t, 5, m.N)

	out := buf.String()
	require.Contains(t, out, "graph-answerability")
	require.Contains(t, out, "direct hit@K")
	require.Contains(t, out, "neighbor inclusion")
	require.Contains(t, out, "graph hit@K")
	require.Contains(t, out, "recovered misses")
	require.Contains(t, out, "[direct]")
	require.Contains(t, out, "[recovered]")
	require.Contains(t, out, "[missed]")
}

// TestRunGraphInclusionNilLogger verifies RunGraphInclusion substitutes a
// discard logger when logger is nil.
func TestRunGraphInclusionNilLogger(t *testing.T) {
	m, _, err := RunGraphInclusion(context.Background(), nil, graphOptions())
	require.NoError(t, err)
	require.Equal(t, 5, m.N)
}

// TestRunGraphInclusionBadBundle asserts a bundle missing cases.json makes
// RunGraphInclusion propagate the loadGraphCases error.
func TestRunGraphInclusionBadBundle(t *testing.T) {
	opts := graphOptions()
	opts.Bundle = t.TempDir()

	_, _, err := RunGraphInclusion(context.Background(), quietLogger(), opts)
	require.Error(t, err)
	require.Contains(t, err.Error(), "read cases.json")
}

// TestRunGraphInclusionEmptyCases asserts a bundle with an empty cases list
// returns zero metrics carrying K/SeedDepth without ingesting anything.
func TestRunGraphInclusionEmptyCases(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "cases.json"), []byte(`{"cases":[]}`), 0o644))

	opts := graphOptions()
	opts.Bundle = dir
	opts.K = 4

	m, results, err := RunGraphInclusion(context.Background(), quietLogger(), opts)
	require.NoError(t, err)
	require.Nil(t, results)
	require.Equal(t, 0, m.N)
	require.Equal(t, 4, m.K)
	require.Equal(t, 4, m.SeedDepth)
}

// TestIngestBundleError asserts ingestBundle wraps the underlying ingest
// failure when the bundle root does not exist.
func TestIngestBundleError(t *testing.T) {
	db, cleanup, err := ingestBundle(context.Background(), quietLogger(),
		filepath.Join(t.TempDir(), "does_not_exist"), []string{"**/*.md"}, nil,
		chunk.Config{TargetTokens: 700, OverlapTokens: 80})
	require.Error(t, err)
	require.Nil(t, db)
	require.Nil(t, cleanup)
	require.Contains(t, err.Error(), "ingest bundle")
}
