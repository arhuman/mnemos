package parse

import "testing"

// FuzzExtractFrontmatter asserts frontmatter extraction never panics on
// arbitrary bytes and upholds its structural invariants on success: the body is
// never longer than the input, and the reported line offset is non-negative.
func FuzzExtractFrontmatter(f *testing.F) {
	for _, s := range []string{
		"", "no frontmatter\n", "---\ntitle: X\ntags: [a, b]\n---\nbody\n",
		"---\nresource: r\ntype: note\n---\n", "---\n", "--- not yaml ---",
		"---\n\t: : :\n---\n",
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, content []byte) {
		res, err := extractFrontmatter(content)
		if err != nil {
			return // malformed frontmatter is a valid error, not a crash
		}
		if len(res.body) > len(content) {
			t.Fatalf("body (%d) longer than content (%d)", len(res.body), len(content))
		}
		if res.lineOffset < 0 {
			t.Fatalf("negative lineOffset %d", res.lineOffset)
		}
	})
}
