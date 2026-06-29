package storage

import (
	"database/sql"
	"embed"
	"fmt"

	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrate runs all pending goose migrations against db using the embedded SQL
// files. It is idempotent: applying it to an already-migrated database is a
// no-op. The "sqlite3" dialect name is goose's identifier for the SQLite
// dialect and is independent of the underlying driver.
func Migrate(db *sql.DB) error {
	// Silence goose's own stderr logger; diagnostics flow through slog.
	goose.SetLogger(goose.NopLogger())
	goose.SetBaseFS(migrationsFS)

	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("storage: set goose dialect: %w", err)
	}

	if err := goose.Up(db, "migrations"); err != nil {
		return fmt.Errorf("storage: run migrations: %w", err)
	}

	return nil
}
