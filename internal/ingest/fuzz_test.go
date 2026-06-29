package ingest

import "testing"

// FuzzHashContent asserts content hashing is deterministic and always a
// lowercase-hex string of 1..16 characters (xxhash64 rendered without zero
// padding) — the invariants the change-detection skip relies on.
func FuzzHashContent(f *testing.F) {
	for _, s := range []string{"", "a", "hello world", "café", "\x00\x01\x02"} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		h1 := hashContent(b)
		if h1 != hashContent(b) {
			t.Fatalf("hashContent not deterministic for %q", b)
		}
		if len(h1) == 0 || len(h1) > 16 {
			t.Fatalf("hash length out of range: %d (%q)", len(h1), h1)
		}
		for _, r := range h1 {
			if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
				t.Fatalf("non-hex char %q in hash %q", r, h1)
			}
		}
	})
}
