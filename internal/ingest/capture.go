package ingest

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/cespare/xxhash/v2"

	"github.com/arhuman/mnemos/internal/chunk"
)

// CaptureInput is the content an agent hands to mnemos.remember, normalized for
// serialization. Type is the OKF document type ("idea" or "document"), Body the
// note text, Tags the optional frontmatter tags, and Collection the logical
// space the captured note belongs to.
type CaptureInput struct {
	Type       string
	Body       string
	Tags       []string
	Collection string
	// Timestamp is the capture time. The program may use time.Now(); only the
	// eval/workflow scripts are forbidden wall-clock access, not the server.
	Timestamp time.Time
}

// RenderOKF serializes a capture into an OKF markdown document: a YAML
// frontmatter block (type, tags, timestamp in RFC3339, collection) followed by a
// blank line and the body. The returned filename is collision-safe and readable
// — "<type>-<YYYYMMDD>-<shorthash>.md" — where the short hash is derived from the
// body so identical notes captured the same day reuse a filename (idempotent
// re-ingest) while distinct notes diverge.
func RenderOKF(in CaptureInput) (filename string, content []byte) {
	ts := in.Timestamp.UTC()

	var b strings.Builder
	_, _ = b.WriteString("---\n")
	_, _ = fmt.Fprintf(&b, "type: %s\n", in.Type)
	_, _ = b.WriteString("tags:")
	if len(in.Tags) == 0 {
		_, _ = b.WriteString(" []\n")
	} else {
		_ = b.WriteByte('\n')
		for _, tag := range in.Tags {
			_, _ = fmt.Fprintf(&b, "  - %s\n", tag)
		}
	}
	_, _ = fmt.Fprintf(&b, "timestamp: %s\n", ts.Format(time.RFC3339))
	_, _ = fmt.Fprintf(&b, "collection: %s\n", in.Collection)
	_, _ = b.WriteString("---\n\n")
	_, _ = b.WriteString(in.Body)
	if !strings.HasSuffix(in.Body, "\n") {
		_ = b.WriteByte('\n')
	}

	short := strconv.FormatUint(xxhash.Sum64String(in.Body), 16)
	if len(short) > 8 {
		short = short[:8]
	}
	filename = fmt.Sprintf("%s-%s-%s.md", in.Type, ts.Format("20060102"), short)

	return filename, []byte(b.String())
}

// WriteCapture writes content to dir/filename atomically. It is the auto-named
// capture wrapper: it joins filename under dir and delegates the atomic write to
// WriteFileAtomic. It returns the absolute path of the written file.
func WriteCapture(dir, filename string, content []byte) (string, error) {
	return WriteFileAtomic(filepath.Join(dir, filename), content)
}

// WriteFileAtomic writes content to absPath atomically: it creates the parent
// directory if missing, writes to a temp file in the same directory, then
// renames it over the destination. The same-directory temp file guarantees the
// rename is atomic (no cross-device copy). It returns absPath on success.
func WriteFileAtomic(absPath string, content []byte) (string, error) {
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", fmt.Errorf("capture: mkdir %q: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(absPath)+".tmp-*")
	if err != nil {
		return "", fmt.Errorf("capture: temp file in %q: %w", dir, err)
	}
	tmpPath := tmp.Name()

	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)

		return "", fmt.Errorf("capture: write %q: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)

		return "", fmt.Errorf("capture: close %q: %w", tmpPath, err)
	}

	if err := os.Rename(tmpPath, absPath); err != nil {
		_ = os.Remove(tmpPath)

		return "", fmt.Errorf("capture: rename %q -> %q: %w", tmpPath, absPath, err)
	}

	return absPath, nil
}

// File performs a one-shot ingest of a single explicit file, reusing the
// existing parse→chunk→write path via Pipeline.IngestPath. It does not scan a
// directory and changes no pipeline behavior. uri is the stable, project-root-
// relative identifier stored as documents.uri (the captured file's path relative
// to the project root, e.g. ".mnemos/capture/idea-20260625-ab12cd34.md"), which
// is what mnemos.search and mnemos.read cite.
//
// One-shot ingest is the default: remember both writes and ingests, and the
// call is intentionally retained. Setting defer_to_watcher=true makes remember
// write-only, leaving capture_dir ingestion to a running watcher (the watcher
// then owns capture). The two modes stay safe together because Phase 1
// hash-skip turns the watcher's re-sighting of an already-ingested file into a
// no-op, so there is no double index either way.
func File(ctx context.Context, db *sql.DB, logger *slog.Logger, absPath, uri, collection string, cfg chunk.Config) (docID string, chunks int, err error) {
	docID, chunks, err = New(db, logger).IngestPath(ctx, absPath, uri, collection, cfg)
	if err != nil {
		return "", 0, fmt.Errorf("capture: ingest %q: %w", uri, err)
	}

	return docID, chunks, nil
}
