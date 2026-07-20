package memory

import "testing"

func TestCharBudget(t *testing.T) {
	cases := []struct {
		name             string
		maxChars, maxTok int
		want             int
	}{
		{"neither", 0, 0, 0},
		{"chars only", 100, 0, 100},
		{"tokens only", 0, 10, 40},
		{"tokens tighter", 100, 10, 40},
		{"chars tighter", 20, 10, 20},
		{"negative ignored", -5, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := charBudget(tc.maxChars, tc.maxTok); got != tc.want {
				t.Fatalf("charBudget(%d,%d) = %d, want %d", tc.maxChars, tc.maxTok, got, tc.want)
			}
		})
	}
}

func TestTruncateContent(t *testing.T) {
	s, trunc := truncateContent("hello world", 0)
	if trunc || s != "hello world" {
		t.Fatalf("zero budget must not truncate: %q trunc=%v", s, trunc)
	}

	s, trunc = truncateContent("hello world", 100)
	if trunc || s != "hello world" {
		t.Fatalf("within budget must not truncate: %q trunc=%v", s, trunc)
	}

	s, trunc = truncateContent("hello world", 5)
	if !trunc {
		t.Fatal("over budget must truncate")
	}
	if s != "hello …" {
		t.Fatalf("truncated to %q, want %q", s, "hello …")
	}
}

// TestTruncateContentRuneBoundary ensures a multi-byte rune is never split.
func TestTruncateContentRuneBoundary(t *testing.T) {
	// "héllo" — the é is two bytes (0xC3 0xA9) at bytes 1-2.
	s, trunc := truncateContent("héllo", 2)
	if !trunc {
		t.Fatal("expected truncation")
	}
	// Budget 2 falls inside the é; it must step back to the rune start (byte 1).
	if s != "h …" {
		t.Fatalf("rune-split: got %q, want %q", s, "h …")
	}
}

func TestParseLineRange(t *testing.T) {
	ok := []struct {
		in         string
		start, end int
	}{
		{"10-42", 10, 42},
		{"5", 5, 5},
		{" 3 - 7 ", 3, 7},
	}
	for _, tc := range ok {
		lr, err := parseLineRange(tc.in)
		if err != nil {
			t.Fatalf("parseLineRange(%q) errored: %v", tc.in, err)
		}
		if lr.Start != tc.start || lr.End != tc.end {
			t.Fatalf("parseLineRange(%q) = %+v, want %d-%d", tc.in, lr, tc.start, tc.end)
		}
	}

	bad := []string{"", "abc", "0-5", "10-3", "-4", "3-"}
	for _, in := range bad {
		if _, err := parseLineRange(in); err == nil {
			t.Fatalf("parseLineRange(%q) should have errored", in)
		}
	}
}
