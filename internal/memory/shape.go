package memory

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// approxCharsPerToken is the rough characters-per-token ratio used to translate a
// max_tokens budget into a character budget. mnemos runs no tokenizer at the
// response boundary (that would pull a model dependency into the hot path), so a
// token budget is honored approximately at ~4 chars/token — the common average
// for English and code — and documented as approximate on the tool schema.
const approxCharsPerToken = 4

// charBudget resolves the effective character budget from a max_chars and a
// max_tokens limit. A zero or negative value means "unbounded" for that axis;
// when both are set the tighter one wins. It returns 0 when neither constrains.
func charBudget(maxChars, maxTokens int) int {
	budget := max(0, maxChars)
	if maxTokens > 0 {
		t := maxTokens * approxCharsPerToken
		if budget == 0 || t < budget {
			budget = t
		}
	}

	return budget
}

// truncateContent cuts content to a character budget on a rune boundary and
// appends an ellipsis marker, reporting whether it truncated. A zero budget, or
// content already within budget, is returned unchanged with truncated=false so
// the caller can decide whether to advertise a deeper read.
func truncateContent(content string, budget int) (string, bool) {
	if budget <= 0 || len(content) <= budget {
		return content, false
	}
	// Step back to a rune start so a multi-byte rune is never split.
	cut := budget
	for cut > 0 && !utf8.RuneStart(content[cut]) {
		cut--
	}

	return content[:cut] + " …", true
}

// lineRange is an inclusive 1-based [Start, End] source-line span parsed from a
// "start-end" request string.
type lineRange struct {
	Start int
	End   int
}

// parseLineRange parses a "start-end" span (both 1-based, inclusive) or a bare
// "start" (a single line). It rejects malformed input and Start > End rather than
// silently returning everything, so a typo does not read the whole document.
func parseLineRange(s string) (lineRange, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return lineRange{}, errors.New("empty line range")
	}
	start, end, found := strings.Cut(s, "-")
	lo, err := strconv.Atoi(strings.TrimSpace(start))
	if err != nil || lo < 1 {
		return lineRange{}, fmt.Errorf("invalid line range %q (want e.g. 10-42)", s)
	}
	if !found {
		return lineRange{Start: lo, End: lo}, nil
	}
	hi, err := strconv.Atoi(strings.TrimSpace(end))
	if err != nil || hi < lo {
		return lineRange{}, fmt.Errorf("invalid line range %q (want e.g. 10-42)", s)
	}

	return lineRange{Start: lo, End: hi}, nil
}
