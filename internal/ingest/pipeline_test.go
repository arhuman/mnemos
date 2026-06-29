package ingest_test

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/chunk"
	"github.com/arhuman/mnemos/internal/ingest"
	"github.com/arhuman/mnemos/internal/testutil"
)

// newDB returns a fresh migrated database for pipeline tests.
func newDB(t *testing.T) *sql.DB { return testutil.NewDB(t) }

// write creates a file with content under dir, making parent directories.
func write(t *testing.T, dir, rel, content string) { testutil.WriteFile(t, dir, rel, content) }

func TestPipelineRun(t *testing.T) {
	src := t.TempDir()
	write(t, src, "a.md", "# Title\n\nBody [l](b.md)\n")
	write(t, src, "index.md", "# Bundle\n\n[hub](a.md)\n")
	write(t, src, "n.txt", "plain\n")

	db := newDB(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	p := ingest.New(db, logger)

	opts := ingest.Options{
		Root:       src,
		Collection: "c",
		Rules: ingest.Rules{
			Include: []string{"**/*.md", "**/*.txt"},
		},
		Chunking: chunk.Config{TargetTokens: 700, OverlapTokens: 80},
	}

	sum, err := p.Run(context.Background(), opts)
	require.NoError(t, err)
	require.Equal(t, 3, sum.FilesScanned)
	require.Equal(t, 3, sum.FilesIngested)
	require.Equal(t, 0, sum.FilesSkipped)
	require.Positive(t, sum.ChunksWritten)

	// Second run skips every unchanged file.
	sum2, err := p.Run(context.Background(), opts)
	require.NoError(t, err)
	require.Equal(t, 0, sum2.FilesIngested)
	require.Equal(t, 3, sum2.FilesSkipped)
	require.Equal(t, 0, sum2.ChunksWritten)

	// Mutating a file makes it ingest again (and only it).
	write(t, src, "a.md", "# Title\n\nChanged body\n")
	sum3, err := p.Run(context.Background(), opts)
	require.NoError(t, err)
	require.Equal(t, 1, sum3.FilesIngested)
	require.Equal(t, 2, sum3.FilesSkipped)
}

func TestPipelineSkipsOversizeFile(t *testing.T) {
	src := t.TempDir()
	write(t, src, "small.md", "# Small\n\nbody\n")
	write(t, src, "big.md", "# Big\n\n"+strings.Repeat("x", 4096))

	db := newDB(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	// Cap below big.md but above small.md: the big file is skipped, not read.
	p := ingest.New(db, logger, ingest.WithMaxFileBytes(1024))

	opts := ingest.Options{
		Root:       src,
		Collection: "c",
		Rules:      ingest.Rules{Include: []string{"**/*.md"}},
		Chunking:   chunk.Config{TargetTokens: 700, OverlapTokens: 80},
	}

	sum, err := p.Run(context.Background(), opts)
	require.NoError(t, err)
	require.Equal(t, 2, sum.FilesScanned)
	require.Equal(t, 1, sum.FilesIngested, "only the small file is ingested")
	require.Equal(t, 1, sum.FilesSkipped, "the oversize file is skipped")

	// With the cap disabled (0), the same big file is ingested.
	pNoCap := ingest.New(db, logger, ingest.WithMaxFileBytes(0))
	sum2, err := pNoCap.Run(context.Background(), opts)
	require.NoError(t, err)
	require.Equal(t, 1, sum2.FilesIngested, "big file now ingests; small file is unchanged")
}

func TestPipelineEmptyRoot(t *testing.T) {
	src := t.TempDir()
	db := newDB(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	sum, err := ingest.New(db, logger).Run(context.Background(), ingest.Options{
		Root:     src,
		Rules:    ingest.Rules{Include: []string{"**/*.md"}},
		Chunking: chunk.Config{TargetTokens: 700, OverlapTokens: 80},
	})
	require.NoError(t, err)
	require.Equal(t, 0, sum.FilesScanned)
	require.Equal(t, 0, sum.FilesIngested)
}
