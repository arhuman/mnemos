package chunk

import "strings"

// TokenCounter estimates the token length of a string. It is the seam that lets
// a real model tokenizer drop in at Phase 4 without touching the chunkers.
type TokenCounter interface {
	// Count returns the estimated number of tokens in text.
	Count(text string) int
}

// WordEstimator is the default offline TokenCounter. It approximates token
// count as ceil(words * 1.3), where words are whitespace-separated fields. The
// 1.3 factor reflects that sub-word tokenizers (BPE/wordpiece) typically split
// natural-language words into slightly more than one token on average. It needs
// no vocabulary file and is fully deterministic, satisfying the local-first
// constraint; a model-matched tokenizer replaces it at Phase 4.
type WordEstimator struct{}

// Count implements TokenCounter.
func (WordEstimator) Count(text string) int {
	words := len(strings.Fields(text))
	if words == 0 {
		return 0
	}
	// ceil(words * 1.3) == (words*13 + 9) / 10
	return (words*13 + 9) / 10
}
