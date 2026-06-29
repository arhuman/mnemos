package ingest

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/adrg/frontmatter"
	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/chunk"
	"github.com/arhuman/mnemos/internal/storage"
)

func TestRenderOKF(t *testing.T) {
	in := CaptureInput{
		Type:       "idea",
		Body:       "the rules engine must stay pure with no I/O",
		Tags:       []string{"architecture", "rules"},
		Collection: "asheeve",
		Timestamp:  time.Date(2026, 6, 25, 10, 30, 0, 0, time.UTC),
	}

	filename, content := RenderOKF(in)

	require.Equal(t, "idea-20260625", filename[:len("idea-20260625")])
	require.True(t, strings.HasSuffix(filename, ".md"))

	// Frontmatter parses and carries the captured fields.
	var fm struct {
		Type       string   `yaml:"type"`
		Tags       []string `yaml:"tags"`
		Timestamp  string   `yaml:"timestamp"`
		Collection string   `yaml:"collection"`
	}
	body, err := frontmatter.Parse(strings.NewReader(string(content)), &fm)
	require.NoError(t, err)
	require.Equal(t, "idea", fm.Type)
	require.Equal(t, []string{"architecture", "rules"}, fm.Tags)
	require.Equal(t, "2026-06-25T10:30:00Z", fm.Timestamp)
	require.Equal(t, "asheeve", fm.Collection)
	require.Equal(t, in.Body, strings.TrimSpace(string(body)))
}

func TestRenderOKFNoTags(t *testing.T) {
	_, content := RenderOKF(CaptureInput{
		Type:       "document",
		Body:       "a note",
		Collection: "default",
		Timestamp:  time.Now(),
	})
	require.Contains(t, string(content), "tags: []")
}

func TestWriteCaptureAtomic(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "capture")
	path, err := WriteCapture(dir, "note.md", []byte("hello\n"))
	require.NoError(t, err)
	require.Equal(t, filepath.Join(dir, "note.md"), path)

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "hello\n", string(got))

	// No leftover temp files.
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

func TestIngestFilePopulatesStore(t *testing.T) {
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "mnemos.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, storage.Migrate(db))

	dir := filepath.Join(t.TempDir(), "capture")
	_, content := RenderOKF(CaptureInput{
		Type:       "idea",
		Body:       "# Rules\n\nThe rules engine stays pure with no side effects.",
		Tags:       []string{"architecture"},
		Collection: "default",
		Timestamp:  time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC),
	})
	abs, err := WriteCapture(dir, "idea-20260625-deadbeef.md", content)
	require.NoError(t, err)

	uri := ".mnemos/capture/idea-20260625-deadbeef.md"
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	docID, chunks, err := File(context.Background(), db, logger, abs, uri, "default", chunk.Config{TargetTokens: 700, OverlapTokens: 80})
	require.NoError(t, err)
	require.NotEmpty(t, docID)
	require.Positive(t, chunks)

	// Document row exists at the project-relative uri.
	doc, err := storage.GetDocumentByURI(context.Background(), db, uri)
	require.NoError(t, err)
	require.NotNil(t, doc)
	require.Equal(t, "default", doc.Collection)

	// Chunks were written and are reachable by uri.
	stored, err := storage.GetChunksByDocURI(context.Background(), db, uri)
	require.NoError(t, err)
	require.Len(t, stored, chunks)

	// Re-ingesting the unchanged file is a no-op (skip path).
	_, again, err := File(context.Background(), db, logger, abs, uri, "default", chunk.Config{TargetTokens: 700, OverlapTokens: 80})
	require.NoError(t, err)
	require.Zero(t, again)
}
