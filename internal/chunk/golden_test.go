package chunk_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/sebdah/goldie/v2"
	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/chunk"
	"github.com/arhuman/mnemos/internal/model"
	"github.com/arhuman/mnemos/internal/parse"
)

// goldenChunk is the deterministic projection of a chunk asserted by the golden
// tests: it omits volatile fields (ids are assigned later by the pipeline) and
// keeps exactly the chunk-shape contract — ordinal, heading path, line range,
// token count, and content.
type goldenChunk struct {
	Ordinal     int    `json:"ordinal"`
	HeadingPath string `json:"heading_path"`
	StartLine   int    `json:"start_line"`
	EndLine     int    `json:"end_line"`
	TokenCount  int    `json:"token_count"`
	Content     string `json:"content"`
}

// TestChunkersGolden parses and chunks each testdata fixture and compares the
// chunk shape against a golden file. Run with -update to refresh goldens. A
// small target_tokens forces the markdown "Oversized" section to split so the
// window/overlap behavior is covered.
func TestChunkersGolden(t *testing.T) {
	cfg := chunk.Config{TargetTokens: 40, OverlapTokens: 8}

	cases := []struct {
		name string
		file string
	}{
		{"markdown", "nested.md"},
		{"code", "sample.go"},
		{"text", "plain.txt"},
	}

	g := goldie.New(t, goldie.WithFixtureDir("testdata/golden"))

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join("testdata", tc.file)
			content, err := os.ReadFile(path)
			require.NoError(t, err)

			src := model.Source{AbsPath: path, URI: tc.file, Collection: "test", Content: content}
			parsed, err := parse.For(path).Parse(context.Background(), src)
			require.NoError(t, err)

			chunks := chunk.Dispatch(parsed, cfg, chunk.WordEstimator{})

			projected := make([]goldenChunk, 0, len(chunks))
			for _, c := range chunks {
				projected = append(projected, goldenChunk{
					Ordinal:     c.Ordinal,
					HeadingPath: c.HeadingPath,
					StartLine:   c.StartLine,
					EndLine:     c.EndLine,
					TokenCount:  c.TokenCount,
					Content:     c.Content,
				})
			}

			out, err := json.MarshalIndent(projected, "", "  ")
			require.NoError(t, err)
			g.Assert(t, tc.name, out)
		})
	}
}
