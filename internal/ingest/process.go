package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/arhuman/mnemos/internal/chunk"
	"github.com/arhuman/mnemos/internal/model"
	"github.com/arhuman/mnemos/internal/parse"
	"github.com/arhuman/mnemos/internal/storage"
)

// IngestPath ingests a single explicit file using the same prepare→write path
// the directory pipeline uses, then returns the document id and the number of
// chunks written. absPath is the file to read; uri is the stable, project-root-
// relative identifier stored as documents.uri (capture passes the path relative
// to the project root so the captured note is a stable citation). A file whose
// content hash is unchanged is a no-op: it returns the existing document id with
// zero chunks. This is the one-shot ingest seam used by mnemos.remember; it adds
// no scan and no pipeline behavior of its own beyond reusing prepare/write.
func (p *Pipeline) IngestPath(ctx context.Context, absPath, uri, collection string, cfg chunk.Config) (docID string, chunks int, err error) {
	r, err := p.prepare(ctx, scanned{absPath: absPath, uri: uri}, Options{
		Collection: collection,
		Chunking:   cfg,
	})
	if err != nil {
		return "", 0, err
	}
	if r.skip {
		return documentID(collection, uri), 0, nil
	}
	if err := p.write(ctx, r); err != nil {
		return "", 0, err
	}

	return r.doc.ID, len(r.chunks), nil
}

// prepare reads, hashes, skip-checks, parses, and chunks a single file. The
// returned result is handed to the writer; result.skip is true when the file's
// content hash matches the stored document (no re-parse, no rewrite).
func (p *Pipeline) prepare(ctx context.Context, f scanned, opts Options) (result, error) {
	info, err := os.Stat(f.absPath)
	if err != nil {
		return result{}, fmt.Errorf("ingest: stat %q: %w", f.absPath, err)
	}
	// Skip oversize files before reading them whole: prepare runs in parallel
	// across GOMAXPROCS workers and each read is held in memory (plus its line
	// split, AST, and chunks), so an unbounded large file would balloon memory.
	if p.maxFileBytes > 0 && info.Size() > p.maxFileBytes {
		p.logger.Warn("ingest skip oversize file", "uri", f.uri, "bytes", info.Size(), "limit", p.maxFileBytes)

		return result{skip: true}, nil
	}

	content, err := os.ReadFile(f.absPath)
	if err != nil {
		return result{}, fmt.Errorf("ingest: read %q: %w", f.absPath, err)
	}

	// Skip binary content: there is no extractor for non-text files (e.g. PDFs
	// matched by an include glob), so reading their raw bytes would feed control
	// characters into chunking, search, and the embedder's tokenizer. Treat a
	// NUL byte or any invalid UTF-8 as binary.
	if isBinary(content) {
		p.logger.Warn("ingest skip binary file", "uri", f.uri)

		return result{skip: true}, nil
	}

	hash := hashContent(content)

	existing, ok, err := storage.DocumentHashByURI(ctx, p.db, f.uri)
	if err != nil {
		return result{}, fmt.Errorf("ingest: hash lookup %q: %w", f.uri, err)
	}
	if ok && existing == hash {
		p.logger.Debug("ingest skip unchanged", "uri", f.uri)

		return result{skip: true}, nil
	}

	modTime := info.ModTime().UTC().Format(time.RFC3339)
	src := model.Source{
		AbsPath:     f.absPath,
		URI:         f.uri,
		Collection:  opts.Collection,
		Content:     content,
		ContentHash: hash,
		ModTime:     modTime,
	}

	parsed, err := parse.For(f.absPath).Parse(ctx, src)
	if err != nil {
		return result{}, fmt.Errorf("ingest: parse %q: %w", f.uri, err)
	}

	docID := documentID(opts.Collection, f.uri)
	chunks := assignIDs(docID, chunk.Dispatch(parsed, opts.Chunking, p.tc))

	modifiedAt := modTime
	if parsed.ModifiedAt != "" {
		modifiedAt = parsed.ModifiedAt
	}

	doc := model.Document{
		ID:              docID,
		URI:             f.uri,
		Collection:      opts.Collection,
		ContentHash:     hash,
		Title:           parsed.Title,
		MimeType:        mimeType(f.absPath),
		SizeBytes:       int64(len(content)),
		ModifiedAt:      modifiedAt,
		IndexedAt:       time.Now().UTC().Format(time.RFC3339),
		FrontmatterJSON: parsed.FrontmatterJSON,
	}

	return result{doc: doc, chunks: chunks, links: buildLinks(docID, parsed)}, nil
}

// write persists one prepared document in a single transaction: upsert the
// document, replace its chunks (FTS triggers cascade), replace its links, and
// append an "ingested" event.
func (p *Pipeline) write(ctx context.Context, r result) error {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("ingest: begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after a successful Commit

	if err := storage.UpsertDocument(ctx, tx, r.doc); err != nil {
		return fmt.Errorf("ingest: upsert %q: %w", r.doc.URI, err)
	}
	if err := storage.ReplaceChunks(ctx, tx, r.doc.ID, r.chunks); err != nil {
		return fmt.Errorf("ingest: replace chunks %q: %w", r.doc.URI, err)
	}
	if err := storage.ReplaceLinks(ctx, tx, r.doc.ID, r.links); err != nil {
		return fmt.Errorf("ingest: replace links %q: %w", r.doc.URI, err)
	}
	if err := storage.AppendEvent(ctx, tx, eventID(r.doc), r.doc.ID, "ingested", eventPayload(r), r.doc.IndexedAt); err != nil {
		return fmt.Errorf("ingest: append event %q: %w", r.doc.URI, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("ingest: commit %q: %w", r.doc.URI, err)
	}
	p.logger.Debug("ingest wrote document", "uri", r.doc.URI, "chunks", len(r.chunks), "links", len(r.links))

	return nil
}

// assignIDs stamps deterministic ids onto chunks and binds them to docID.
func assignIDs(docID string, chunks []model.Chunk) []model.Chunk {
	for i := range chunks {
		chunks[i].DocumentID = docID
		chunks[i].ID = chunkID(docID, chunks[i].Ordinal)
	}

	return chunks
}

// buildLinks resolves the parsed link URIs into edges from docID.
func buildLinks(docID string, parsed model.ParsedDoc) []model.Link {
	if len(parsed.Links) == 0 {
		return nil
	}
	links := make([]model.Link, 0, len(parsed.Links))
	for _, dst := range parsed.Links {
		links = append(links, model.Link{SrcDoc: docID, DstDoc: dst})
	}

	return links
}

// eventID returns a unique id for an append-only event row. Events are a log,
// not content-addressed, so a nanosecond timestamp guards against collisions
// when the same document is re-ingested within the same RFC3339 second.
func eventID(d model.Document) string {
	return d.ID + ":" + strconv.FormatInt(time.Now().UTC().UnixNano(), 10)
}

// eventPayload renders the event payload JSON with the uri and counts.
func eventPayload(r result) string {
	payload := map[string]any{
		"uri":    r.doc.URI,
		"chunks": len(r.chunks),
		"links":  len(r.links),
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return `{"uri":""}`
	}

	return string(b)
}

// mimeType guesses a MIME type from the file extension, or "" when unknown.
func mimeType(path string) string {
	return mime.TypeByExtension(filepath.Ext(path))
}

// isBinary reports whether content looks like a non-text file. A NUL byte never
// appears in valid UTF-8 text and is the classic binary marker; invalid UTF-8
// catches the rest (e.g. PDF streams, images). Text files — including UTF-8 with
// accents, CJK, or emoji — pass.
func isBinary(content []byte) bool {
	return bytes.IndexByte(content, 0) >= 0 || !utf8.Valid(content)
}
