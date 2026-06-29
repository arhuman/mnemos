package chunk

import "github.com/arhuman/mnemos/internal/model"

// TextChunker splits plain text into paragraph-aware, token-budgeted windows.
// Paragraphs (blank-line separated) are packed into windows of ~TargetTokens;
// a single oversized paragraph is itself line-windowed. Line ranges stay exact.
type TextChunker struct {
	cfg Config
	tc  TokenCounter
}

// Chunk implements Chunker. Paragraph packing reduces to line windowing: lines
// already include the blank separators, so wholeOrWindows keeps coherent
// paragraph groups while never splitting mid-line, preserving exact ranges.
func (t TextChunker) Chunk(doc model.ParsedDoc) []model.Chunk {
	return buildChunks(doc, t.tc, func(sec model.Section) []window {
		return wholeOrWindows(sec, t.tc, t.cfg)
	})
}
