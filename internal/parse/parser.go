// Package parse turns raw file bytes into a normalized model.ParsedDoc. A small
// extension registry routes each file to its Parser: markdown, Go source, or the
// plain-text fallback. Richer SQL/HTML parsers are deliberately deferred — for
// V0 those extensions fall through to the text parser.
package parse

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/arhuman/mnemos/internal/model"
)

// Parser converts a Source into a ParsedDoc. Implementations must be
// deterministic and side-effect free.
type Parser interface {
	// Parse reads src.Content and returns the normalized document.
	Parse(ctx context.Context, src model.Source) (model.ParsedDoc, error)
}

// registry maps a lowercase file extension to its Parser. Extensions absent
// here but present in the include set fall back to the text parser (V0 keeps
// SQL/HTML/JSON/YAML/TOML as structured text rather than shipping rich parsers).
var registry = map[string]Parser{
	".md":       MarkdownParser{},
	".markdown": MarkdownParser{},
	".go":       GoParser{},
}

// For returns the Parser for the given file path, defaulting to the text parser
// for any extension without a dedicated entry.
func For(path string) Parser {
	ext := strings.ToLower(filepath.Ext(path))
	if p, ok := registry[ext]; ok {
		return p
	}

	return TextParser{}
}
