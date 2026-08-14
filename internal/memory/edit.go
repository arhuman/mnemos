package memory

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/arhuman/mnemos/internal/chunk"
	"github.com/arhuman/mnemos/internal/ingest"
	"github.com/arhuman/mnemos/internal/okfyaml"
	"github.com/arhuman/mnemos/internal/search"
	"github.com/arhuman/mnemos/internal/security"
	"github.com/arhuman/mnemos/internal/storage"
)

// ErrReindexAfterWrite tags a reindex failure that followed a successful write.
// It separates two outcomes a caller must not conflate: the edit is durable on
// disk and only the index is stale, versus the edit was lost.
var ErrReindexAfterWrite = errors.New("edit: write ok, reindex failed")

// ErrNotEditable reports frontmatter this codebase declines to rewrite (missing,
// malformed, or using a shape whose source bytes cannot be patched in place).
var ErrNotEditable = errors.New("edit: frontmatter is not editable in place")

// FieldEdit is one frontmatter key to rewrite. Items applies when List is set
// (the whole list is retyped); otherwise Value replaces the scalar.
type FieldEdit struct {
	Key   string
	Value string
	Items []string
	List  bool
}

// EditFrontmatterInput is a single atomic edit of the document at URI: any
// number of frontmatter field patches plus, when Body is non-nil, a replacement
// body. Batching them is deliberate — an editor session that changed both a
// field and the body saves once, so the two can never be written from stale
// copies of each other.
type EditFrontmatterInput struct {
	URI    string
	Fields []FieldEdit
	Body   *string
}

// EditResult reports the edited document's uri, its document id and the number
// of chunks the reindex wrote.
type EditResult struct {
	URI        string
	DocumentID string
	Chunks     int
}

// EditSource is a document as an editor opens it: the bytes on disk, the
// frontmatter split out for display, and whether those fields can be patched in
// place. Editable is false for a document whose frontmatter this codebase
// declines to rewrite, so an editor shows it read-only instead of risking a
// corrupting save.
type EditSource struct {
	URI        string
	Collection string
	Type       string
	Content    []byte
	Body       string
	Fields     []okfyaml.Field
	Editable   bool
}

// OpenForEdit reads the document at uri from disk and splits its frontmatter for
// display. It reads the file rather than the index because an editor edits the
// source of truth, not the reconstruction of it.
func (s *Service) OpenForEdit(ctx context.Context, uri string) (EditSource, error) {
	if err := ctx.Err(); err != nil {
		return EditSource{}, err
	}

	abs, resolved, err := security.ResolveWithin(s.treeRoot, uri, s.cfg.ConfinementExclude())
	if err != nil {
		return EditSource{}, fmt.Errorf("edit path: %w", err)
	}
	content, err := os.ReadFile(abs) //nolint:gosec // abs is confined to the tree by ResolveWithin
	if err != nil {
		return EditSource{}, fmt.Errorf("edit read %q: %w", resolved, err)
	}

	src := EditSource{URI: resolved, Collection: s.collectionFor(ctx, resolved), Content: content, Body: string(content)}
	// Frontmatter this codebase declines to patch (absent, malformed, or an
	// untrackable shape) is not a failure to open: the document opens read-only.
	doc, ok, _ := okfyaml.Parse(content)
	if !ok {
		return src, nil
	}

	src.Editable = true
	src.Fields = doc.Fields()
	src.Body = string(doc.Body())
	for _, f := range src.Fields {
		if f.Key == "type" {
			src.Type = f.Value
		}
	}

	return src, nil
}

// Similar returns the documents semantically nearest to uri, honoring the
// server-side visibility boundary. It returns nil (no error) when the corpus
// carries no embeddings or the source document has none of its own: "not
// embedded yet" is an expected state, not a failure.
func (s *Service) Similar(ctx context.Context, uri string, limit int) ([]search.SimilarResult, error) {
	modelName, found, err := storage.AnyEmbeddingModel(ctx, s.db)
	if err != nil {
		return nil, fmt.Errorf("memory: similar model: %w", err)
	}
	if !found {
		return nil, nil
	}

	results, err := search.Similar(ctx, s.db, modelName, uri, s.resolveLimit(limit))
	if err != nil {
		return nil, err
	}

	visible := make([]search.SimilarResult, 0, len(results))
	for _, r := range results {
		if s.hiddenCollection(r.Collection) {
			continue
		}
		visible = append(visible, r)
	}
	if len(visible) == 0 {
		return nil, nil
	}

	return visible, nil
}

// EditFrontmatter applies field patches (and optionally a new body) to an
// existing OKF document and reindexes it. It is gated by [mcp].allow_write.
//
// The file is written before the reindex runs, deliberately: a write that lands
// makes the edit durable whatever happens next, so a failing reindex costs a
// stale index rather than the user's work. That case returns an error wrapping
// ErrReindexAfterWrite so a caller can say which of the two happened.
func (s *Service) EditFrontmatter(ctx context.Context, in EditFrontmatterInput) (EditResult, error) {
	if err := ctx.Err(); err != nil {
		return EditResult{}, err
	}
	if !s.cfg.MCP.AllowWrite {
		return EditResult{}, errWriteDisabled
	}

	abs, uri, err := security.ResolveWithin(s.treeRoot, in.URI, s.cfg.ConfinementExclude())
	if err != nil {
		return EditResult{}, fmt.Errorf("edit path: %w", err)
	}

	current, err := os.ReadFile(abs) //nolint:gosec // abs is confined to the tree by ResolveWithin
	if err != nil {
		return EditResult{}, fmt.Errorf("edit read %q: %w", uri, err)
	}

	content, err := applyEdits(current, in)
	if err != nil {
		return EditResult{}, err
	}

	if _, err = ingest.WriteFileAtomic(abs, content); err != nil {
		return EditResult{}, fmt.Errorf("edit write %q: %w", uri, err)
	}

	docID, chunks, err := ingest.ReindexOne(ctx, s.db, s.logger, abs, uri, s.collectionFor(ctx, uri),
		chunk.ConfigFrom(s.cfg.Chunking.TargetTokens, s.cfg.Chunking.OverlapTokens))
	if err != nil {
		return EditResult{URI: uri}, fmt.Errorf("%w: %w", ErrReindexAfterWrite, err)
	}

	s.logger.Info("edit saved document", "uri", uri, "fields", len(in.Fields), "chunks", chunks)

	return EditResult{URI: uri, DocumentID: docID, Chunks: chunks}, nil
}

// EditBody replaces the document body at uri, leaving its frontmatter block byte
// for byte. It is gated by [mcp].allow_write.
func (s *Service) EditBody(ctx context.Context, uri, newBody string) (EditResult, error) {
	return s.EditFrontmatter(ctx, EditFrontmatterInput{URI: uri, Body: &newBody})
}

// applyEdits patches content one field at a time, re-parsing after each patch so
// every patch is located against the bytes the previous one produced.
func applyEdits(content []byte, in EditFrontmatterInput) ([]byte, error) {
	for _, f := range in.Fields {
		doc, ok, err := okfyaml.Parse(content)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrNotEditable, err)
		}
		if !ok {
			return nil, ErrNotEditable
		}
		if f.List {
			content, err = doc.PatchTags(f.Key, f.Items)
		} else {
			content, err = doc.PatchScalar(f.Key, f.Value)
		}
		if err != nil {
			return nil, fmt.Errorf("edit field %q: %w", f.Key, err)
		}
	}

	if in.Body == nil {
		return content, nil
	}

	doc, ok, err := okfyaml.Parse(content)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNotEditable, err)
	}
	if !ok {
		return nil, ErrNotEditable
	}

	return doc.ReplaceBody([]byte(*in.Body)), nil
}

// collectionFor returns the collection the document is already indexed under, so
// an edit never silently moves it. An unindexed file falls back to the default
// collection; its own `collection:` frontmatter still wins during the reindex.
func (s *Service) collectionFor(ctx context.Context, uri string) string {
	doc, err := storage.GetDocumentByURI(ctx, s.db, uri)
	if err != nil || doc == nil || doc.Collection == "" {
		return "default"
	}

	return doc.Collection
}
