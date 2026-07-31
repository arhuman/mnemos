package memory_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/chunk"
	"github.com/arhuman/mnemos/internal/config"
	"github.com/arhuman/mnemos/internal/ingest"
	"github.com/arhuman/mnemos/internal/memory"
	"github.com/arhuman/mnemos/internal/storage"
)

// benchInjectionService seeds a hub document with `fan` outbound links and `fan`
// inbound backlinks, then returns a service over it. It lets the benchmarks
// compare a plain read against a read plus the neighbor lookup that read/context
// injection would add.
func benchInjectionService(b *testing.B, fan int) *memory.Service {
	b.Helper()
	ctx := context.Background()
	dir := b.TempDir()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	db, err := storage.Open(ctx, filepath.Join(dir, "bench.db"))
	require.NoError(b, err)
	b.Cleanup(func() { _ = db.Close() })
	require.NoError(b, storage.Migrate(db))

	chunking := chunk.Config{TargetTokens: 700, OverlapTokens: 80}
	writeAndIngest := func(rel, content string) {
		p := filepath.Join(dir, rel)
		require.NoError(b, os.MkdirAll(filepath.Dir(p), 0o750))
		require.NoError(b, os.WriteFile(p, []byte(content), 0o600))
		_, _, ferr := ingest.File(ctx, db, logger, p, filepath.ToSlash(rel), "demo", chunking)
		require.NoError(b, ferr)
	}

	// A hub with real body text (so the read has work) plus `fan` outbound links.
	var hub strings.Builder
	_, _ = hub.WriteString("# Hub\n\nThis hub document links out to many detail docs.\n\n")
	for i := range fan {
		_, _ = fmt.Fprintf(&hub, "- see [detail %d](t%d.md)\n", i, i)
	}
	writeAndIngest("hub.md", hub.String())

	// `fan` detail docs, each linking back to the hub (inbound edges).
	for i := range fan {
		writeAndIngest(fmt.Sprintf("t%d.md", i), fmt.Sprintf("# Detail %d\n\nBack to [hub](hub.md).\n", i))
	}

	cfg, err := config.Load("", func(string) bool { return false })
	require.NoError(b, err)

	return memory.New(db, cfg, dir, nil, logger)
}

func BenchmarkReadDocument(b *testing.B) {
	svc := benchInjectionService(b, 25)
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := svc.ReadDocument(ctx, "hub.md"); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkReadDocumentThenRelated measures a read plus the neighbor lookup that
// automatic injection would add; the delta versus BenchmarkReadDocument is the
// per-call cost of injection.
func BenchmarkReadDocumentThenRelated(b *testing.B) {
	svc := benchInjectionService(b, 25)
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := svc.ReadDocument(ctx, "hub.md"); err != nil {
			b.Fatal(err)
		}
		if _, err := svc.Related(ctx, "hub.md", memory.DirectionBoth, 0); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRelated(b *testing.B) {
	svc := benchInjectionService(b, 25)
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		if _, err := svc.Related(ctx, "hub.md", memory.DirectionBoth, 0); err != nil {
			b.Fatal(err)
		}
	}
}
