package memory

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/model"
)

// TestReconstruct exercises every branch of the overlap-dedup walk in a single
// chunk slice: the no-line-metadata fallback, the empty-block skip, the
// separator between blocks, a wholly-covered chunk, an overlapping chunk whose
// leading lines are trimmed, and a chunk whose claimed range outruns its actual
// line count.
func TestReconstruct(t *testing.T) {
	chunks := []model.Chunk{
		{StartLine: 0, Content: ""},                       // empty fallback: appendBlock no-ops
		{StartLine: 0, Content: "frontmatter-raw"},        // no line metadata: raw append, no separator yet
		{StartLine: 1, EndLine: 3, Content: "L1\nL2\nL3"}, // normal append with separator
		{StartLine: 2, EndLine: 3, Content: "L2\nL3"},     // wholly covered (EndLine <= written): skipped
		{StartLine: 3, EndLine: 5, Content: "L3\nL4\nL5"}, // overlaps: drop leading "L3", append "L4\nL5"
		{StartLine: 4, EndLine: 6, Content: "single"},     // firstNew >= len(lines): advances written, appends nothing
	}

	require.Equal(t, "frontmatter-raw\nL1\nL2\nL3\nL4\nL5", reconstruct(chunks))
}

// TestReconstructEndBeforeStart covers the EndLine < StartLine half of the
// no-line-metadata guard (degenerate range falls back to raw concatenation).
func TestReconstructEndBeforeStart(t *testing.T) {
	require.Equal(t, "weird", reconstruct([]model.Chunk{{StartLine: 5, EndLine: 2, Content: "weird"}}))
	require.Equal(t, "", reconstruct(nil))
}

// TestDocTitle covers the stored-title path and the heading-path fallback.
func TestDocTitle(t *testing.T) {
	require.Equal(t, "Real", docTitle(&model.Document{Title: "Real"}, "A > B"))
	require.Equal(t, "B", docTitle(&model.Document{}, "A > B"))
	require.Equal(t, "", docTitle(&model.Document{}, ""))
}

// TestResolveLimit covers both arms: an explicit positive limit is honored, a
// non-positive limit falls back to the configured default.
func TestResolveLimit(t *testing.T) {
	s := &Service{defaultLimit: 7}
	require.Equal(t, 3, s.resolveLimit(3))
	require.Equal(t, 7, s.resolveLimit(0))
	require.Equal(t, 7, s.resolveLimit(-1))
}
