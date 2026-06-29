package parse

import (
	"context"
	"strings"

	"github.com/arhuman/mnemos/internal/model"
)

// TextParser is the V0 fallback for any non-markdown, non-Go file (.txt, .sql,
// .html, .json, .yaml/.yml, .toml). It emits a single whole-document section;
// the text chunker handles paragraph/line windowing. Rich SQL/HTML structure is
// deliberately deferred past V0.
type TextParser struct{}

// Parse implements parse.Parser.
func (TextParser) Parse(_ context.Context, src model.Source) (model.ParsedDoc, error) {
	content := string(src.Content)
	lineCount := strings.Count(strings.TrimSuffix(content, "\n"), "\n") + 1
	if content == "" {
		lineCount = 1
	}

	return model.ParsedDoc{
		Kind:  model.KindText,
		Title: titleFromBody(src.Content),
		Sections: []model.Section{{
			HeadingPath: "",
			StartLine:   1,
			EndLine:     lineCount,
			Content:     content,
		}},
	}, nil
}
