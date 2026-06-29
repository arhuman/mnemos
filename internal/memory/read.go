package memory

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/arhuman/mnemos/internal/model"
	"github.com/arhuman/mnemos/internal/storage"
)

// Citation locates a chunk for an agent: the owning document uri, the heading
// path, and the 1-based inclusive line range.
type Citation struct {
	URI         string
	HeadingPath string
	StartLine   int
	EndLine     int
}

// ReadResult is the outcome of reading a document or a single chunk. Content is
// the chunk text or the reconstructed document body; the metadata fields locate
// it. Citation is set only for a chunk read.
type ReadResult struct {
	URI        string
	Collection string
	Title      string
	Content    string
	Citation   *Citation
}

// ReadChunk returns a single chunk's content and its citation. An unknown
// chunk_id is a not-found error, not a crash.
func (s *Service) ReadChunk(ctx context.Context, chunkID string) (ReadResult, error) {
	c, err := storage.GetChunkByID(ctx, s.db, chunkID)
	if err != nil {
		return ReadResult{}, fmt.Errorf("memory: read chunk: %w", err)
	}
	if c == nil {
		return ReadResult{}, fmt.Errorf("unknown chunk_id %q", chunkID)
	}

	doc, err := storage.GetDocumentByID(ctx, s.db, c.DocumentID)
	if err != nil {
		return ReadResult{}, fmt.Errorf("memory: read chunk document: %w", err)
	}

	uri, collection, title := "", "", ""
	if doc != nil {
		uri, collection, title = doc.URI, doc.Collection, docTitle(doc, c.HeadingPath)
	}

	return ReadResult{
		URI:        uri,
		Collection: collection,
		Title:      title,
		Content:    c.Content,
		Citation: &Citation{
			URI:         uri,
			HeadingPath: c.HeadingPath,
			StartLine:   c.StartLine,
			EndLine:     c.EndLine,
		},
	}, nil
}

// ReadDocument reconstructs a document from its stored chunks ordered by
// ordinal, de-duplicating the overlapping line ranges the windowed chunker
// emits. An unknown uri is a not-found error.
func (s *Service) ReadDocument(ctx context.Context, uri string) (ReadResult, error) {
	chunks, err := storage.GetChunksByDocURI(ctx, s.db, uri)
	if err != nil {
		return ReadResult{}, fmt.Errorf("memory: read document: %w", err)
	}
	if len(chunks) == 0 {
		return ReadResult{}, fmt.Errorf("unknown uri %q", uri)
	}

	doc, err := storage.GetDocumentByURI(ctx, s.db, uri)
	if err != nil {
		return ReadResult{}, fmt.Errorf("memory: read document metadata: %w", err)
	}

	collection, title := "", ""
	if doc != nil {
		collection, title = doc.Collection, docTitle(doc, "")
	}

	return ReadResult{
		URI:        uri,
		Collection: collection,
		Title:      title,
		Content:    reconstruct(chunks),
	}, nil
}

// ErrAmbiguousRead and ErrEmptyRead document the two invalid read selectors a
// surface may pass; surfaces validate their own input shape, but ReadOne lets a
// surface delegate the choice too.
var (
	ErrAmbiguousRead = errors.New("provide exactly one of uri or chunk_id, not both")
	ErrEmptyRead     = errors.New("provide exactly one of uri or chunk_id")
)

// ReadOne dispatches to ReadDocument or ReadChunk based on which of uri/chunkID
// is set, rejecting the both-set and neither-set cases. It is the single entry
// the MCP read tool delegates to.
func (s *Service) ReadOne(ctx context.Context, uri, chunkID string) (ReadResult, error) {
	hasURI := uri != ""
	hasChunk := chunkID != ""
	switch {
	case hasURI && hasChunk:
		return ReadResult{}, ErrAmbiguousRead
	case !hasURI && !hasChunk:
		return ReadResult{}, ErrEmptyRead
	case hasChunk:
		return s.ReadChunk(ctx, chunkID)
	default:
		return s.ReadDocument(ctx, uri)
	}
}

// reconstruct stitches ordinal-ordered chunks back into the source body while
// de-duplicating overlapping windows. The windowed chunker overlaps chunks by
// `overlap_tokens`, so adjacent chunks repeat their boundary lines; emitting
// each chunk verbatim would duplicate those lines. Because every chunk carries a
// 1-based inclusive [StartLine, EndLine] range, we track the highest line
// already written and, for each chunk, append only the lines beyond it. Chunks
// fully covered by earlier ones contribute nothing.
func reconstruct(chunks []model.Chunk) string {
	var b strings.Builder
	written := 0 // highest source line number already appended
	for _, c := range chunks {
		// No line metadata: fall back to raw concatenation for this chunk.
		if c.StartLine <= 0 || c.EndLine < c.StartLine {
			appendBlock(&b, c.Content)

			continue
		}
		if c.EndLine <= written {
			continue // wholly covered by an earlier chunk
		}

		lines := strings.Split(c.Content, "\n")
		// firstNew is the count of leading lines in this chunk that fall at or
		// before `written` and must be dropped to avoid duplication.
		firstNew := 0
		if c.StartLine <= written {
			firstNew = written - c.StartLine + 1
		}
		if firstNew >= len(lines) {
			written = c.EndLine

			continue
		}
		appendBlock(&b, strings.Join(lines[firstNew:], "\n"))
		written = c.EndLine
	}

	return b.String()
}

// appendBlock writes block to b, inserting a newline separator between blocks.
func appendBlock(b *strings.Builder, block string) {
	if block == "" {
		return
	}
	if b.Len() > 0 {
		_ = b.WriteByte('\n')
	}
	_, _ = b.WriteString(block)
}

// docTitle returns the document title, falling back to the last heading-path
// segment when the document has no stored title.
func docTitle(doc *model.Document, headingPath string) string {
	if doc.Title != "" {
		return doc.Title
	}

	return model.LastHeading(headingPath)
}
