package chunk_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/chunk"
	"github.com/arhuman/mnemos/internal/model"
	"github.com/arhuman/mnemos/internal/parse"
)

// benchCfg mirrors the production default chunk budget ([chunking] target 700 /
// overlap 80) so the windowing and overlap cost in the benchmarks reflects real
// ingest, not the tiny budgets the correctness tests use to force splits.
var benchCfg = chunk.Config{TargetTokens: 700, OverlapTokens: 80}

// parseFixture parses a chunk testdata fixture once so BenchmarkDispatch isolates
// chunk.Dispatch — the per-document chunking hot path — from the upstream parse
// cost, which has its own package.
func parseFixture(b *testing.B, file string) model.ParsedDoc {
	b.Helper()
	path := filepath.Join("testdata", file)
	content, err := os.ReadFile(path)
	require.NoError(b, err)

	parsed, err := parse.For(path).Parse(context.Background(), model.Source{
		AbsPath: path, URI: file, Collection: "bench", Content: content,
	})
	require.NoError(b, err)

	return parsed
}

// BenchmarkDispatch measures the per-document chunking cost for each chunker
// family (markdown by heading, Go by declaration, text by paragraph). Parsing
// runs once outside the loop, so only Dispatch is timed; ReportAllocs surfaces
// the per-chunk allocation the ingest worker pool pays once per file.
func BenchmarkDispatch(b *testing.B) {
	cases := []struct {
		name string
		file string
	}{
		{"markdown", "nested.md"},
		{"code", "sample.go"},
		{"text", "plain.txt"},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			doc := parseFixture(b, tc.file)
			tokenizer := chunk.WordEstimator{}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = chunk.Dispatch(doc, benchCfg, tokenizer)
			}
		})
	}
}

// BenchmarkDispatchLarge chunks a large markdown document whose sections exceed
// the token budget, so the windowByTokens overlap path — the most expensive
// branch and the one the small fixtures never reach — dominates the measurement.
// It is the regression guard for the windowing hot path.
func BenchmarkDispatchLarge(b *testing.B) {
	doc := parseLargeMarkdown(b, 40, 60) // 40 sections of 60 lines each
	tokenizer := chunk.WordEstimator{}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = chunk.Dispatch(doc, benchCfg, tokenizer)
	}
}

// parseLargeMarkdown builds and parses a markdown document of `sections` headings,
// each with `linesPer` body lines, sized so every section is well over the 700-
// token budget and forces windowing with overlap.
func parseLargeMarkdown(b *testing.B, sections, linesPer int) model.ParsedDoc {
	b.Helper()
	var sb strings.Builder
	for s := range sections {
		_, _ = fmt.Fprintf(&sb, "# Section %d\n\n", s)
		for range linesPer {
			_, _ = sb.WriteString("alpha beta gamma delta epsilon zeta eta theta iota kappa\n")
		}
		_, _ = sb.WriteString("\n")
	}

	content := []byte(sb.String())
	parsed, err := parse.For("large.md").Parse(context.Background(), model.Source{
		AbsPath: "large.md", URI: "large.md", Collection: "bench", Content: content,
	})
	require.NoError(b, err)

	return parsed
}
