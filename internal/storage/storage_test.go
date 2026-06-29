package storage_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

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
