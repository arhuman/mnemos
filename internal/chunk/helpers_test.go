package chunk

import (
	"strings"
	"testing"

	"github.com/arhuman/mnemos/internal/model"
	"github.com/stretchr/testify/require"
)

// TestSplitLines covers the unexported splitLines helper directly.
// The function is never exercised by golden tests because plain.txt sections
// all fit within the 40-token budget, so the split path is never taken.
func TestSplitLines(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", []string{""}},
		{"single line", "hello", []string{"hello"}},
		{"two lines", "hello\nworld", []string{"hello", "world"}},
		{"trailing newline", "a\nb\n", []string{"a", "b", ""}},
		{"blank line in middle", "a\n\nb", []string{"a", "", "b"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, splitLines(tc.in))
		})
	}
}

// TestTextChunkerSplitOversized drives TextChunker.split into the line-windowing
// branch by supplying a section whose token count exceeds TargetTokens. This also
// exercises splitLines, which plain.txt never reaches.
func TestTextChunkerSplitOversized(t *testing.T) {
	// 4 lines × 5 words = 20 words → ceil(20 × 1.3) = 26 tokens, well above budget=5.
	content := strings.Join([]string{
		"alpha beta gamma delta epsilon",
		"zeta eta theta iota kappa",
		"lambda mu nu xi omicron",
		"pi rho sigma tau upsilon",
	}, "\n")

	sec := model.Section{Content: content, StartLine: 1, EndLine: 4}
	tc := TextChunker{
		cfg: Config{TargetTokens: 5, OverlapTokens: 0},
		tc:  WordEstimator{},
	}

	windows := wholeOrWindows(sec, tc.tc, tc.cfg)
	require.Greater(t, len(windows), 1, "oversized section must produce multiple windows")
	for _, w := range windows {
		require.NotEmpty(t, w.content)
		require.GreaterOrEqual(t, w.startLine, 1)
		require.GreaterOrEqual(t, w.endLine, w.startLine)
	}
}

// TestTextChunkerSplitFitsInBudget verifies the no-split early return (content
// within budget returns a single window preserving the section's line range).
func TestTextChunkerSplitFitsInBudget(t *testing.T) {
	content := "one two"
	sec := model.Section{Content: content, StartLine: 3, EndLine: 3}
	tc := TextChunker{
		cfg: Config{TargetTokens: 50, OverlapTokens: 0},
		tc:  WordEstimator{},
	}

	windows := wholeOrWindows(sec, tc.tc, tc.cfg)
	require.Len(t, windows, 1)
	require.Equal(t, content, windows[0].content)
	require.Equal(t, 3, windows[0].startLine)
	require.Equal(t, 3, windows[0].endLine)
}

// TestCodeChunkerSplitOversized drives CodeChunker.split into the line-windowing
// branch, mirroring the text chunker test but for code sections.
func TestCodeChunkerSplitOversized(t *testing.T) {
	content := strings.Join([]string{
		"func foo() { return }",
		"func bar() { return }",
		"func baz() { return }",
		"func qux() { return }",
	}, "\n")

	sec := model.Section{Content: content, StartLine: 1, EndLine: 4}
	cc := CodeChunker{
		cfg: Config{TargetTokens: 5, OverlapTokens: 0},
		tc:  WordEstimator{},
	}

	windows := wholeOrWindows(sec, cc.tc, cc.cfg)
	require.Greater(t, len(windows), 1, "oversized code section must produce multiple windows")
	for _, w := range windows {
		require.NotEmpty(t, w.content)
	}
}

// TestCodeChunkerSplitFitsInBudget verifies CodeChunker.split's early return
// when the section is within the token budget.
func TestCodeChunkerSplitFitsInBudget(t *testing.T) {
	content := "func f() {}"
	sec := model.Section{Content: content, StartLine: 5, EndLine: 5}
	cc := CodeChunker{
		cfg: Config{TargetTokens: 50, OverlapTokens: 0},
		tc:  WordEstimator{},
	}

	windows := wholeOrWindows(sec, cc.tc, cc.cfg)
	require.Len(t, windows, 1)
	require.Equal(t, content, windows[0].content)
	require.Equal(t, 5, windows[0].startLine)
}
