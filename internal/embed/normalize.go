package embed

import (
	"math"
	"strings"
)

// normalizeNewlines rewrites CRLF and bare CR line endings to LF. The sugarme
// tokenizer's Precompiled normalizer panics (index out of range in
// TransformRange) on any input containing "\r\n", so CRLF corpora would
// otherwise abort a whole reindex. LF-only text tokenizes identically, so this
// costs nothing on corpora that never had CR.
func normalizeNewlines(s string) string {
	if !strings.ContainsRune(s, '\r') {
		return s
	}
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}

// L2Normalize scales vec in place to unit Euclidean length. A zero vector is
// left unchanged (no division by zero). After normalization the cosine
// similarity between two vectors equals their dot product, which is what the
// vector search relies on.
func L2Normalize(vec []float32) {
	var norm float64
	for _, v := range vec {
		norm += float64(v) * float64(v)
	}
	norm = math.Sqrt(norm)
	if norm == 0 {
		return
	}
	inv := float32(1.0 / norm)
	for i := range vec {
		vec[i] *= inv
	}
}
