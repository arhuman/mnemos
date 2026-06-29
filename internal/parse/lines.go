package parse

import "strings"

// lineIndex maps byte offsets in a source buffer to 1-based line numbers and
// extracts inclusive line ranges. It is built once per document.
type lineIndex struct {
	// lineStart[i] is the byte offset where 1-based line (i+1) begins.
	lineStart []int
	total     int
}

// newLineIndex builds a lineIndex over source. Line N (1-based) starts at
// lineStart[N-1]. A trailing newline does not add a spurious empty final line
// for range purposes.
func newLineIndex(source []byte) lineIndex {
	starts := []int{0}
	for i, b := range source {
		if b == '\n' {
			starts = append(starts, i+1)
		}
	}
	// A trailing newline produced a final start past EOF; drop it so line
	// counts match strings.Split-style line content.
	total := len(starts)
	if len(source) > 0 && source[len(source)-1] == '\n' {
		total--
	}
	if total < 1 {
		total = 1
	}

	return lineIndex{lineStart: starts, total: total}
}

// line returns the 1-based line number containing byte offset off.
func (l lineIndex) line(off int) int {
	// Binary search for the greatest lineStart <= off.
	lo, hi := 0, len(l.lineStart)-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if l.lineStart[mid] <= off {
			lo = mid
		} else {
			hi = mid - 1
		}
	}

	return lo + 1
}

// lineCount returns the number of content lines.
func (l lineIndex) lineCount() int { return l.total }

// slice returns the content of 1-based inclusive line range [start, end].
func (l lineIndex) slice(source []byte, start, end int) string {
	if start < 1 {
		start = 1
	}
	if end > l.total {
		end = l.total
	}
	if end < start {
		return ""
	}
	lines := strings.Split(string(source), "\n")
	if len(source) > 0 && source[len(source)-1] == '\n' && len(lines) > 0 {
		lines = lines[:len(lines)-1]
	}
	if end > len(lines) {
		end = len(lines)
	}

	return strings.Join(lines[start-1:end], "\n")
}

// headingStack tracks the active heading hierarchy to build "A > B > C" paths.
type headingStack struct {
	levels []int
	texts  []string
}

func newHeadingStack() *headingStack { return &headingStack{} }

// push records a heading at the given level, popping any deeper-or-equal
// headings first so the path reflects the current hierarchy.
func (s *headingStack) push(level int, text string) {
	for len(s.levels) > 0 && s.levels[len(s.levels)-1] >= level {
		s.levels = s.levels[:len(s.levels)-1]
		s.texts = s.texts[:len(s.texts)-1]
	}
	s.levels = append(s.levels, level)
	s.texts = append(s.texts, text)
}

// path renders the current heading hierarchy as "A > B > C".
func (s *headingStack) path() string {
	return strings.Join(s.texts, " > ")
}
