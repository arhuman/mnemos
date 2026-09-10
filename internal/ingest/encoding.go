package ingest

import (
	"fmt"

	"github.com/arhuman/mnemos/internal/encoding"
)

// EncodingRule binds a charset to the root-relative globs it applies to. It
// mirrors config's [[indexing.encoding]] without importing it, keeping the
// pipeline free of config types as Rules already does for the selection globs.
type EncodingRule struct {
	Match   []string
	Charset string
}

// encodingRules resolves a file's declared charset, if any. The zero value
// declares nothing, which is the UTF-8-only default.
type encodingRules struct {
	rules []resolvedEncoding
}

type resolvedEncoding struct {
	match   []string
	decoder encoding.Decoder
}

// newEncodingRules resolves every rule's charset up front so an unknown name
// fails once at construction rather than per file. Callers loading from config
// have already validated these; this re-check keeps the pipeline safe for
// direct API users who build rules by hand.
func newEncodingRules(rules []EncodingRule) (encodingRules, error) {
	if len(rules) == 0 {
		return encodingRules{}, nil
	}

	resolved := make([]resolvedEncoding, 0, len(rules))
	for i, r := range rules {
		dec, err := encoding.Lookup(r.Charset)
		if err != nil {
			return encodingRules{}, fmt.Errorf("ingest: encoding rule #%d: %w", i+1, err)
		}
		resolved = append(resolved, resolvedEncoding{match: r.Match, decoder: dec})
	}

	return encodingRules{rules: resolved}, nil
}

// decoderFor returns the decoder declared for the root-relative path rel, and
// whether one matched. The first matching rule wins, so a narrower rule listed
// earlier overrides a broader one.
func (e encodingRules) decoderFor(rel string) (encoding.Decoder, bool) {
	for _, r := range e.rules {
		if anyMatch(r.match, rel) {
			return r.decoder, true
		}
	}

	return encoding.Decoder{}, false
}
