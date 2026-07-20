package cli

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSalientTermsStripsStopwordsAndShortTokens(t *testing.T) {
	got := salientTerms("Why did we choose staging on the VPS?")
	// why/did/the are stopwords; "we"/"on" are shorter than 3 runes; the rest are
	// lowercased content words in first-seen order.
	require.Equal(t, []string{"choose", "staging", "vps"}, got)
}

func TestSalientTermsDeduplicatesAndCaps(t *testing.T) {
	require.Equal(t, []string{"alpha", "beta"}, salientTerms("alpha beta alpha BETA alpha"))

	long := strings.Repeat("aaa bbb ccc ddd eee fff ggg hhh iii jjj ", 1)
	require.Len(t, salientTerms(long), 8, "capped at maxTerms")
}

func TestShouldRecall(t *testing.T) {
	cases := []struct {
		name   string
		prompt string
		want   bool
	}{
		{"empty", "   ", false},
		{"plain imperative", "add a csv export button", false},
		{"question", "which port does the gateway bind?", true},
		{"cue phrase", "the usual staging setup please", true},
		{"french cue", "c'était quoi le port déjà", true},
		{"oversized paste", strings.Repeat("x", hookRecallMaxPromptChars+1) + "?", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, shouldRecall(tc.prompt))
		})
	}
}

func TestParsePrompt(t *testing.T) {
	require.Equal(t, "hello there", parsePrompt([]byte(`{"prompt":"  hello there  ","hook_event_name":"UserPromptSubmit"}`)))
	require.Equal(t, "", parsePrompt([]byte(`not json at all`)))
	require.Equal(t, "", parsePrompt([]byte(`{"cwd":"/tmp"}`)))
}

func TestCapTokens(t *testing.T) {
	small := "line one\nline two\n"
	require.Equal(t, small, capTokens(small, 100), "under budget is unchanged")
	require.Equal(t, small, capTokens(small, 0), "zero budget is uncapped")

	big := strings.Repeat("abcd\n", 200) // 1000 bytes
	out := capTokens(big, 10)            // ~40 char budget
	require.Less(t, len(out), len(big))
	require.Contains(t, out, "truncated to ~10 tokens")
	require.True(t, strings.HasSuffix(strings.TrimRight(out, "\n"), "tokens]_"))
}
