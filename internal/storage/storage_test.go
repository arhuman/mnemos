package storage_test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/model"
	"github.com/arhuman/mnemos/internal/storage"
)

// TestMigrateCreatesSchema opens a temp DB, migrates it, and asserts the V0
// tables plus the FTS virtual table exist and are queryable.
func TestMigrateCreatesSchema(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "mnemos.db")

	db, err := storage.Open(context.Background(), dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	require.NoError(t, storage.Migrate(db))

	t.Run("expected tables exist", func(t *testing.T) {
		tables := []string{"documents", "chunks", "chunks_fts", "links", "events"}
		for _, name := range tables {
			t.Run(name, func(t *testing.T) {
				var got string
				err := db.QueryRowContext(context.Background(),
					`SELECT name FROM sqlite_master WHERE name = ? AND type IN ('table','view')`,
					name,
				).Scan(&got)
				require.NoError(t, err, "table %q should exist", name)
				require.Equal(t, name, got)
			})
		}
	})

	t.Run("fts is queryable", func(t *testing.T) {
		var n int
		require.NoError(t, db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM chunks_fts`).Scan(&n))
		require.Equal(t, 0, n)
	})

	t.Run("migrate is idempotent", func(t *testing.T) {
		require.NoError(t, storage.Migrate(db))
	})
}

// TestPragmasHoldOnEveryPooledConnection pins the reason the pragmas travel in
// the DSN rather than through ExecContext. Setting them with Exec configures only
// whichever pooled connection served that call, so the rest of the pool runs
// without foreign_keys (silently breaking ON DELETE CASCADE) and without
// busy_timeout. Holding several connections open at once forces the pool to
// create distinct physical connections and asserts each one is configured.
func TestPragmasHoldOnEveryPooledConnection(t *testing.T) {
	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "mnemos.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	const conns = 4
	held := make([]*sql.Conn, 0, conns)
	t.Cleanup(func() {
		for _, c := range held {
			_ = c.Close()
		}
	})

	// Acquire and hold every connection before reading, so the pool cannot serve
	// them all from one reused physical connection.
	for range conns {
		c, cerr := db.Conn(context.Background())
		require.NoError(t, cerr)
		held = append(held, c)
	}

	for i, c := range held {
		var journal, synchronous string
		var foreignKeys, busyTimeout int
		require.NoError(t, c.QueryRowContext(context.Background(), "PRAGMA journal_mode").Scan(&journal))
		require.NoError(t, c.QueryRowContext(context.Background(), "PRAGMA foreign_keys").Scan(&foreignKeys))
		require.NoError(t, c.QueryRowContext(context.Background(), "PRAGMA busy_timeout").Scan(&busyTimeout))
		require.NoError(t, c.QueryRowContext(context.Background(), "PRAGMA synchronous").Scan(&synchronous))

		require.Equal(t, "wal", journal, "connection %d", i)
		require.Equal(t, 1, foreignKeys, "connection %d: ON DELETE CASCADE depends on this", i)
		require.Equal(t, 5000, busyTimeout, "connection %d", i)
		require.Equal(t, "1", synchronous, "connection %d: NORMAL under WAL", i)
	}
}

// TestCascadeDeleteAcrossPool guards the consequence rather than the setting: an
// unconfigured connection silently skips ON DELETE CASCADE, orphaning chunks
// instead of failing loudly. Deleting through one connection while others are
// held open is the shape that regresses if the pragmas ever stop being applied
// per-connection.
func TestCascadeDeleteAcrossPool(t *testing.T) {
	ctx := context.Background()
	db, err := storage.Open(ctx, filepath.Join(t.TempDir(), "mnemos.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, storage.Migrate(db))

	held := make([]*sql.Conn, 0, 3)
	t.Cleanup(func() {
		for _, c := range held {
			_ = c.Close()
		}
	})
	for range 3 {
		c, cerr := db.Conn(ctx)
		require.NoError(t, cerr)
		held = append(held, c)
	}

	_, err = db.ExecContext(ctx,
		`INSERT INTO documents (id, uri, collection, content_hash) VALUES ('d1', 'a.md', 'c', 'h')`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx,
		`INSERT INTO chunks (id, document_id, ordinal, content) VALUES ('c1', 'd1', 0, 'body')`)
	require.NoError(t, err)

	_, err = db.ExecContext(ctx, `DELETE FROM documents WHERE uri = 'a.md'`)
	require.NoError(t, err)

	var orphans int
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM chunks WHERE document_id = 'd1'`).Scan(&orphans))
	require.Zero(t, orphans, "cascade must evict chunks whichever pooled connection serves the delete")
}

// TestConcurrentReadThenWriteDoesNotFailToUpgrade guards the lock ordering, not a
// setting. UpsertDocument deletes a superseded row before inserting, so a write
// transaction reads before it writes. Under BEGIN DEFERRED two such transactions
// each take a read snapshot and the second fails to upgrade with
// SQLITE_BUSY_SNAPSHOT (517), which busy_timeout cannot absorb because no wait can
// resolve it. _txlock=immediate takes the write lock at BEGIN so the second writer
// waits instead. See ADR-0012.
func TestConcurrentReadThenWriteDoesNotFailToUpgrade(t *testing.T) {
	ctx := context.Background()
	db := openMigrated(t)

	const writers = 4
	var wg sync.WaitGroup
	errs := make([]error, writers)
	start := make(chan struct{})

	for i := range writers {
		n := i
		wg.Go(func() {
			<-start
			errs[n] = func() error {
				tx, err := db.BeginTx(ctx, nil)
				if err != nil {
					return err
				}
				defer tx.Rollback() //nolint:errcheck // no-op after a successful Commit

				// The read that makes this a read-then-write transaction.
				var seen int
				if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM documents`).Scan(&seen); err != nil {
					return err
				}
				doc := model.Document{
					ID:          fmt.Sprintf("id%d", n),
					URI:         fmt.Sprintf("note%d.md", n),
					Collection:  "c",
					ContentHash: "h",
					IndexedAt:   "t",
				}
				if err := storage.UpsertDocument(ctx, tx, doc); err != nil {
					return err
				}

				return tx.Commit()
			}()
		})
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		require.NoError(t, err, "writer %d must not fail to upgrade its lock", i)
	}

	var n int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM documents`).Scan(&n))
	require.Equal(t, writers, n, "every writer's document must be committed")
}
