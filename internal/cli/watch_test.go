package cli_test

import (
	"bytes"
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/arhuman/mnemos/internal/cli"
)

// runCmdCtx runs the root command under ctx (via ExecuteContext) and returns a
// channel that receives the command's error when it returns. Used for the
// long-running watch command, which blocks until ctx is cancelled.
func runCmdCtx(ctx context.Context, args ...string) <-chan error {
	done := make(chan error, 1)
	go func() {
		root := cli.NewRootCmd() //nolint:contextcheck // test drives a blocking command via ExecuteContext; OpenStore's own ctx threading is out of scope
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&out)
		root.SetArgs(args)
		done <- root.ExecuteContext(ctx)
	}()

	return done
}

func TestWatchReconcilesThenStopsCleanly(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")
	seedKB(t, "w.md", "# Watched\n\nwatchable body content\n")

	ctx, cancel := context.WithCancel(context.Background())
	done := runCmdCtx(ctx, "watch", filepath.Join(".mnemos", "kb"), "--collection", "demo")

	db, err := sql.Open("sqlite", filepath.Join(".mnemos", "state", "index.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	// The startup reconcile indexes the seeded file; wait for it, then stop.
	require.Eventually(t, func() bool {
		var n int
		_ = db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM documents`).Scan(&n)

		return n >= 1
	}, 5*time.Second, 50*time.Millisecond, "watch reconcile should index the seeded file")

	cancel()
	select {
	case err := <-done:
		require.NoError(t, err, "watch must shut down cleanly on cancellation")
	case <-time.After(5 * time.Second):
		t.Fatal("watch did not stop within 5s of cancellation")
	}
}
