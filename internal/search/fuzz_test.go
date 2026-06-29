package search

import (
	"errors"
	"testing"
)

// FuzzSanitizeMatch asserts the query sanitizer can turn any input into either a
// safe FTS5 MATCH expression or ErrEmptyQuery — never a panic, never another
// error. The key property: a successful result is itself a stable input (feeding
// it back through the sanitizer succeeds), which is what guarantees the FTS5
// MATCH can never be a syntax error.
func FuzzSanitizeMatch(f *testing.F) {
	for _, s := range []string{
		"", "   ", "scim: provisioning (entra)", `"drop table"`,
		"a-b.c_d", "AND OR NOT", "café déjà", "***", "v1.2.3", "-.-", "()[]{}",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, q string) {
		got, err := sanitizeMatch(q)
		if err != nil {
			if !errors.Is(err, ErrEmptyQuery) {
				t.Fatalf("unexpected error for %q: %v", q, err)
			}
			if got != "" {
				t.Fatalf("ErrEmptyQuery must yield empty string, got %q", got)
			}

			return
		}
		if got == "" {
			t.Fatalf("nil error must yield a non-empty match expression for %q", q)
		}
		// Re-sanitizing a successful result must also succeed (stable closure).
		if _, err2 := sanitizeMatch(got); err2 != nil {
			t.Fatalf("re-sanitizing %q (from %q) failed: %v", got, q, err2)
		}
	})
}
