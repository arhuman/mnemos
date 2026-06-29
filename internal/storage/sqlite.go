// Package storage provides SQLite persistence for mnemos: opening the
// database with sane PRAGMAs and running embedded goose migrations.
package storage

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite" // pure-Go SQLite driver, registered as "sqlite"
)

// pragmas applied on every freshly opened connection. WAL enables concurrent
// readers alongside the single writer; foreign_keys enforces ON DELETE CASCADE;
// busy_timeout avoids spurious "database is locked" errors under contention.
var pragmas = []string{
	"PRAGMA journal_mode=WAL;",
	"PRAGMA foreign_keys=ON;",
	"PRAGMA busy_timeout=5000;",
}

// Open opens (creating if needed) the SQLite database at path and applies the
// standard PRAGMAs. The returned *sql.DB is ready for migrations and queries.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("storage: open %q: %w", path, err)
	}

	// modernc applies PRAGMAs per-connection, so keep a single connection to
	// guarantee WAL/foreign_keys hold for the whole *sql.DB lifetime.
	db.SetMaxOpenConns(1)

	for _, p := range pragmas {
		if _, err := db.ExecContext(ctx, p); err != nil {
			_ = db.Close()

			return nil, fmt.Errorf("storage: apply %q: %w", p, err)
		}
	}

	return db, nil
}
