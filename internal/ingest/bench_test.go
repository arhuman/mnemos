package ingest_test

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/chunk"
	"github.com/arhuman/mnemos/internal/ingest"
	"github.com/arhuman/mnemos/internal/storage"
)

// benchOpts mirrors a realistic directory ingest: markdown corpus, production
// chunk budget. Root is filled in per benchmark.
func benchOpts(root string) ingest.Options {
	return ingest.Options{
		Root:       root,
		Collection: "bench",
		Rules:      ingest.Rules{Include: []string{"**/*.md"}},
		Chunking:   chunk.Config{TargetTokens: 700, OverlapTokens: 80},
	}
}

// buildCorpus writes n markdown files (one heading, five sections, eight body
// lines each) under a fresh temp dir once, so the file I/O is shared across
// benchmark iterations and only the pipeline work is measured.
func buildCorpus(b *testing.B, n int) string {
	b.Helper()
	root := b.TempDir()
	for i := range n {
		var sb strings.Builder
		_, _ = fmt.Fprintf(&sb, "# Document %d\n\n", i)
		for s := range 5 {
			_, _ = fmt.Fprintf(&sb, "## Section %d\n\n", s)
			for range 8 {
				_, _ = sb.WriteString("alpha beta gamma delta epsilon zeta eta theta\n")
			}
			_, _ = sb.WriteString("\n")
		}
		path := filepath.Join(root, fmt.Sprintf("doc-%03d.md", i))
		require.NoError(b, os.WriteFile(path, []byte(sb.String()), 0o600))
	}

	return root
}

// freshDB opens and migrates a new SQLite store at path. Each cold-ingest
// iteration needs a fresh store so every file is actually parsed, chunked, and
// written rather than hitting the unchanged-hash skip path.
func freshDB(b *testing.B, path string) *sql.DB {
	b.Helper()
	db, err := storage.Open(context.Background(), path)
	require.NoError(b, err)
	require.NoError(b, storage.Migrate(db))

	return db
}

// BenchmarkPipelineRun measures the directory ingest pipeline end to end:
//
//	fresh              cold ingest — scan, hash, parse, chunk, and write every
//	                   file into an empty store (the worker pool + single writer).
//	reingest-unchanged warm re-run over an already-ingested corpus — the
//	                   content-hash skip path, which should be far cheaper.
//
// Store open/migrate and close are excluded from the timer so only pipeline work
// is measured.
func BenchmarkPipelineRun(b *testing.B) {
	const files = 50
	root := buildCorpus(b, files)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	opts := benchOpts(root)

	b.Run("fresh", func(b *testing.B) {
		dir := b.TempDir()
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			db := freshDB(b, filepath.Join(dir, fmt.Sprintf("bench-%d.db", i)))
			b.StartTimer()

			if _, err := ingest.New(db, logger).Run(context.Background(), opts); err != nil {
				b.Fatal(err)
			}

			b.StopTimer()
			_ = db.Close()
			b.StartTimer()
		}
	})

	b.Run("reingest-unchanged", func(b *testing.B) {
		db := freshDB(b, filepath.Join(b.TempDir(), "bench.db"))
		defer func() { _ = db.Close() }()
		p := ingest.New(db, logger)
		if _, err := p.Run(context.Background(), opts); err != nil { // warm the store once
			b.Fatal(err)
		}

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			if _, err := p.Run(context.Background(), opts); err != nil { // every file skipped
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkIngestPath measures the one-shot single-file ingest seam used by
// mnemos.remember. A fresh store per iteration keeps every write on the real
// ingest path rather than the skip path.
func BenchmarkIngestPath(b *testing.B) {
	root := buildCorpus(b, 1)
	absPath := filepath.Join(root, "doc-000.md")
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := chunk.Config{TargetTokens: 700, OverlapTokens: 80}
	dir := b.TempDir()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		db := freshDB(b, filepath.Join(dir, fmt.Sprintf("bench-%d.db", i)))
		p := ingest.New(db, logger)
		b.StartTimer()

		if _, _, err := p.IngestPath(context.Background(), absPath, "doc-000.md", "bench", cfg); err != nil {
			b.Fatal(err)
		}

		b.StopTimer()
		_ = db.Close()
		b.StartTimer()
	}
}
