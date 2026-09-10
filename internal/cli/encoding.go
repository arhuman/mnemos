package cli

import (
	"github.com/arhuman/mnemos/internal/config"
	"github.com/arhuman/mnemos/internal/ingest"
)

// encodingRules adapts the config's [[indexing.encoding]] declarations to the
// pipeline's own rule type, preserving order (first match wins). The two types
// are deliberately separate so package ingest does not import config; this is
// the single conversion point every command uses.
func encodingRules(rules []config.EncodingRule) []ingest.EncodingRule {
	if len(rules) == 0 {
		return nil
	}

	out := make([]ingest.EncodingRule, 0, len(rules))
	for _, r := range rules {
		out = append(out, ingest.EncodingRule{Match: r.Match, Charset: r.Charset})
	}

	return out
}
