package chunk

import "github.com/arhuman/mnemos/internal/model"

// CodeChunker emits one chunk per top-level declaration. The Go parser produces
// one Section per func/method/type (with its doc comment) and exact line spans;
// when parsing failed upstream the parser falls back to a single whole-file
// Section, which wholeOrWindows splits by lines.
type CodeChunker struct {
	cfg Config
	tc  TokenCounter
}

// Chunk implements Chunker. Declaration sections map to one chunk each; an
// oversized section (or the parse-failure fallback) is split into line windows.
func (c CodeChunker) Chunk(doc model.ParsedDoc) []model.Chunk {
	return buildChunks(doc, c.tc, func(sec model.Section) []window {
		return wholeOrWindows(sec, c.tc, c.cfg)
	})
}
