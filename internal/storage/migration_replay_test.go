package storage_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/storage"
)

// TestMigrationChainReplaysOntoEmpty applies every migration in order to a
// brand-new database and asserts the final schema, including 0006's column and
// partial index. It is the gate's replay check for a migration-touching change.
func TestMigrationChainReplaysOntoEmpty(t *testing.T) {
	ctx := context.Background()
	db := openMigrated(t)

	var n int
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM pragma_table_info('documents') WHERE name='journal'`).Scan(&n))
	require.Equal(t, 1, n, "0006 must add documents.journal")

	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_documents_journal'`).Scan(&n))
	require.Equal(t, 1, n, "0006 must create the partial journal index")

	// Every prior migration's artifacts must still be present after 0006.
	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_chunks_document_id'`).Scan(&n))
	require.Equal(t, 1, n, "0005's index must survive the chain")

	require.NoError(t, db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='chunks_fts'`).Scan(&n))
	require.Equal(t, 1, n, "0001's FTS table must survive the chain")

	// Idempotence: re-running the chain on an already-migrated database is a no-op.
	require.NoError(t, storage.Migrate(db))
}
