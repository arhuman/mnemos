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
// it. Citation is set only for a chunk read. Truncated reports that a size budget
// clipped Content, signalling the caller to read deeper if it needs the rest.
type ReadResult struct {
	URI        string
	Collection string
	Title      string
	Content    string
	Truncated  bool
	Citation   *Citation
}

// ReadOptions narrows and bounds a read. Section and Lines scope a document read
// to a subset of its chunks (both empty reads the whole document); they are
// ignored for a single-chunk read, which is already a fragment. MaxChars and
// MaxTokens cap the returned Content (see charBudget); the zero value of every
// field reproduces the unbounded whole-document/whole-chunk read.
type ReadOptions struct {
	// Section keeps only chunks whose heading path contains this text
	// (case-insensitive), e.g. "Definition of done".
	Section string
	// Lines keeps only chunks overlapping this 1-based inclusive source-line span,
	// written "start-end" or a bare "start".
	Lines     string
	MaxChars  int
	MaxTokens int
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

// ReadDocument reconstructs a whole document from its stored chunks. It is the
// unbounded, unscoped read; ReadDocumentOpts adds section/line scoping.
func (s *Service) ReadDocument(ctx context.Context, uri string) (ReadResult, error) {
	return s.ReadDocumentOpts(ctx, uri, ReadOptions{})
}

// ReadDocumentOpts reconstructs a document from its stored chunks ordered by
// ordinal, de-duplicating the overlapping line ranges the windowed chunker emits.
// When opts.Section or opts.Lines is set it first narrows to the matching chunks,
// so a caller can pull one heading or line span instead of the whole document. An
// unknown uri is a not-found error; a scope that matches no chunk is an error too,
// so an empty read is never silently returned. Size budgets are applied by the
// ReadOneOpts caller, uniformly with chunk reads.
func (s *Service) ReadDocumentOpts(ctx context.Context, uri string, opts ReadOptions) (ReadResult, error) {
	chunks, err := storage.GetChunksByDocURI(ctx, s.db, uri)
	if err != nil {
		return ReadResult{}, fmt.Errorf("memory: read document: %w", err)
	}
	if len(chunks) == 0 {
		return ReadResult{}, fmt.Errorf("unknown uri %q", uri)
	}

	chunks, err = scopeChunks(chunks, opts, uri)
	if err != nil {
		return ReadResult{}, err
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

// scopeChunks narrows chunks to those matching opts.Section and/or opts.Lines,
// preserving order. Both empty returns chunks unchanged. A scope that matches no
// chunk is an error (naming uri) rather than an empty document.
func scopeChunks(chunks []model.Chunk, opts ReadOptions, uri string) ([]model.Chunk, error) {
	if opts.Section == "" && opts.Lines == "" {
		return chunks, nil
	}
	var lr lineRange
	if opts.Lines != "" {
		var err error
		if lr, err = parseLineRange(opts.Lines); err != nil {
			return nil, fmt.Errorf("memory: read %q: %w", uri, err)
		}
	}
	section := strings.ToLower(opts.Section)

	out := make([]model.Chunk, 0, len(chunks))
	for _, c := range chunks {
		if section != "" && !strings.Contains(strings.ToLower(c.HeadingPath), section) {
			continue
		}
		// Skip chunks whose [StartLine, EndLine] does not intersect the request.
		if opts.Lines != "" && (c.StartLine > lr.End || c.EndLine < lr.Start) {
			continue
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no chunk in %q matches the requested section/lines", uri)
	}

	return out, nil
}

// ErrAmbiguousRead and ErrEmptyRead document the two invalid read selectors a
// surface may pass; surfaces validate their own input shape, but ReadOne lets a
// surface delegate the choice too.
var (
	ErrAmbiguousRead = errors.New("provide exactly one of uri or chunk_id, not both")
	ErrEmptyRead     = errors.New("provide exactly one of uri or chunk_id")
)

// ReadOne dispatches an unbounded read of a document or chunk. It is
// ReadOneOpts with the zero ReadOptions.
func (s *Service) ReadOne(ctx context.Context, uri, chunkID string) (ReadResult, error) {
	return s.ReadOneOpts(ctx, uri, chunkID, ReadOptions{})
}

// ReadOneOpts dispatches to ReadDocumentOpts or ReadChunk based on which of
// uri/chunkID is set, rejecting the both-set and neither-set cases, then applies
// the size budget to the returned content uniformly (section/line scoping applies
// to document reads only; a chunk is already a fragment). It is the single entry
// the MCP read tool delegates to.
func (s *Service) ReadOneOpts(ctx context.Context, uri, chunkID string, opts ReadOptions) (ReadResult, error) {
	hasURI := uri != ""
	hasChunk := chunkID != ""

	var (
		res ReadResult
		err error
	)
	switch {
	case hasURI && hasChunk:
		return ReadResult{}, ErrAmbiguousRead
	case !hasURI && !hasChunk:
		return ReadResult{}, ErrEmptyRead
	case hasChunk:
		res, err = s.ReadChunk(ctx, chunkID)
	default:
		res, err = s.ReadDocumentOpts(ctx, uri, opts)
	}
	if err != nil {
		return ReadResult{}, err
	}

	res.Content, res.Truncated = truncateContent(res.Content, charBudget(opts.MaxChars, opts.MaxTokens))

	return res, nil
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
