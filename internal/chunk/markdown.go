package chunk

import "github.com/arhuman/mnemos/internal/model"

// MarkdownChunker emits one chunk per heading section, splitting oversized
// sections into overlapping line windows. Line ranges are preserved exactly so
// citations can point at the source lines.
type MarkdownChunker struct {
	cfg Config
	tc  TokenCounter
}

// Chunk implements Chunker. Sections within the token budget stay whole; larger
// sections are split into windows of ~TargetTokens with OverlapTokens overlap,
// each window carrying the precise 1-based line range it covers.
func (m MarkdownChunker) Chunk(doc model.ParsedDoc) []model.Chunk {
	return buildChunks(doc, m.tc, func(sec model.Section) []window {
		return wholeOrWindows(sec, m.tc, m.cfg)
	})
}
