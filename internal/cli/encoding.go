package cli

import (
	"github.com/arhuman/mnemos/internal/app"
	"github.com/arhuman/mnemos/internal/config"
	"github.com/arhuman/mnemos/internal/ingest"
	"github.com/arhuman/mnemos/internal/security"
)

// pipelineOptions builds the ingest options every indexing command shares, so a
// new option is wired once rather than in each command. Keeping this in one
// place is what stops `add`, `ingest`, `reindex`, `watch`, and `migrate` from
// drifting into indexing the same tree under different rules.
//
// Secret screening is gated on [security].exclude_secrets, the same flag that
// governs the path-exclusion globs: both answer "keep credentials out of the
// index", and splitting them across two switches would let a user disable one
// while believing the other still applied.
func pipelineOptions(a *app.App) []ingest.Option {
	opts := []ingest.Option{
		ingest.WithMaxFileBytes(a.Config.Indexing.MaxFileBytes),
		ingest.WithEncodings(encodingRules(a.Config.EncodingRules())),
	}
	if a.Config.Security.ExcludeSecrets {
		opts = append(opts, ingest.WithSecretScanner(security.NewRegexScanner()))
	}

	return opts
}

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
