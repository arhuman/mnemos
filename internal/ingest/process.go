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
	"strings"
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
	if p.encodingErr != nil {
		return "", 0, p.encodingErr
	}
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

	content, ok := p.textContent(content, f.uri)
	if !ok {
		return result{skip: true}, nil
	}

	hash := hashContent(content)

	// Force re-parses even an unchanged file, so skip the hash short-circuit; the
	// oversize/binary/unparseable skips above still guard the read and parse.
	if !opts.Force {
		existing, ok, lookupErr := storage.DocumentHashByURI(ctx, p.db, f.uri)
		if lookupErr != nil {
			return result{}, fmt.Errorf("ingest: hash lookup %q: %w", f.uri, lookupErr)
		}
		if ok && existing == hash {
			p.logger.Debug("ingest skip unchanged", "uri", f.uri)

			return result{skip: true}, nil
		}
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
		// A single malformed file (e.g. broken YAML frontmatter) must not abort a
		// batch ingest of many documents; skip it with a warning, as with binary
		// files. The batch then indexes everything it can.
		p.logger.Warn("ingest skip unparseable file", "uri", f.uri, "error", err)

		return result{skip: true}, nil
	}

	// An OKF document's own `collection:` frontmatter is authoritative; the
	// --collection flag is the fallback for files that don't declare one. This
	// keeps a document's collection stable across re-ingests and lets a
	// re-index (e.g. mnemos migrate) preserve the original collections.
	collection := opts.Collection
	if fc, ok := parsed.Frontmatter["collection"].(string); ok && strings.TrimSpace(fc) != "" {
		collection = strings.TrimSpace(fc)
	}

	docID := documentID(collection, f.uri)
	chunks := assignIDs(docID, chunk.Dispatch(parsed, opts.Chunking, p.tc))

	modifiedAt := modTime
	if parsed.ModifiedAt != "" {
		modifiedAt = parsed.ModifiedAt
	}

	doc := model.Document{
		ID:              docID,
		URI:             f.uri,
		Collection:      collection,
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

// hasNUL reports whether content carries a NUL byte, the classic binary marker:
// it never appears in valid text, in UTF-8 or in any legacy single-byte charset.
// This check is unconditional, so a declared [[indexing.encoding]] charset can
// never admit binary content (e.g. a binary Delphi .dfm, which is NUL-dense).
func hasNUL(content []byte) bool {
	return bytes.IndexByte(content, 0) >= 0
}

// textContent returns content as UTF-8 text, reporting false when the file is
// not ingestible text (and logging why). There is no extractor for non-text
// files (e.g. a PDF matched by an include glob), so feeding their raw bytes into
// chunking, search, and the embedder's tokenizer must be prevented.
//
// The three cases are ordered deliberately:
//
//   - A NUL byte is the binary marker; it never appears in valid text, in UTF-8
//     or in any legacy single-byte charset. Checked first and unconditionally, so
//     a declared charset can only ever relax the UTF-8 requirement below, never
//     admit binary content.
//   - A declared [[indexing.encoding]] charset decodes legacy text (Windows-125x,
//     ISO-8859-x) that is valid source but not valid UTF-8.
//   - Otherwise content must already be UTF-8. Invalid UTF-8 here is NUL-free and
//     may well be legacy text, but nothing in the bytes says which charset, so it
//     is skipped rather than guessed at.
//
// Decoding happens before the caller hashes, so the content hashed, parsed,
// chunked, and stored is always UTF-8. That also makes a charset correction
// self-healing: the decoded bytes change, so the hash changes, so the file is
// re-ingested instead of surviving as a mis-decode behind the unchanged-hash skip.
func (p *Pipeline) textContent(content []byte, uri string) ([]byte, bool) {
	if hasNUL(content) {
		p.logger.Warn("ingest skip binary file", "uri", uri)

		return nil, false
	}

	if dec, ok := p.encodings.decoderFor(uri); ok {
		decoded, err := dec.Decode(content)
		if err != nil {
			p.logger.Warn("ingest skip undecodable file", "uri", uri, "charset", dec.Name(), "error", err)

			return nil, false
		}

		return decoded, true
	}

	if !utf8.Valid(content) {
		// Name the offset and the remedy so this is not mistaken for a true binary.
		p.logger.Warn("ingest skip non-UTF-8 file", "uri", uri,
			"offset", firstInvalidUTF8(content),
			"hint", "declare its charset in [[indexing.encoding]] to ingest it")

		return nil, false
	}

	return content, true
}

// firstInvalidUTF8 returns the byte offset of the first invalid UTF-8 sequence
// in content, or -1 when content is valid. It turns "this file is not UTF-8"
// into a location an operator can inspect to identify the real charset.
func firstInvalidUTF8(content []byte) int {
	for i := 0; i < len(content); {
		r, size := utf8.DecodeRune(content[i:])
		if r == utf8.RuneError && size <= 1 {
			return i
		}
		i += size
	}

	return -1
}
