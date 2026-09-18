// Package storage provides SQLite persistence for mnemos: opening the
// database with sane PRAGMAs and running embedded goose migrations.
package storage

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"

	_ "modernc.org/sqlite" // pure-Go SQLite driver, registered as "sqlite"
)

// pragmas are applied through the DSN, which the driver replays on every
// physical connection it opens (applyQueryParams runs from newConn). Setting
// them with ExecContext instead would configure only whichever pooled connection
// happened to serve the call, so the rest of the pool would silently run without
// them: measured on v1.58.0, an Exec-configured pool of 4 reports
// foreign_keys=0 and busy_timeout=0 on every connection.
//
//   - journal_mode=WAL lets readers proceed alongside the single writer.
//   - foreign_keys=ON enforces ON DELETE CASCADE, which is how deleting a
//     document evicts its chunks, links and FTS rows.
//   - busy_timeout avoids spurious "database is locked" errors under contention.
//   - synchronous=NORMAL is the standard setting under WAL: it can lose the last
//     commits on power loss but never corrupts the file, and it removes the
//     per-commit fsync that dominates a bulk ingest (one transaction per
//     document).
var pragmas = []string{
	"journal_mode(WAL)",
	"foreign_keys(1)",
	"busy_timeout(5000)",
	"synchronous(NORMAL)",
}

// dsn builds the connection string carrying the pragmas. The path is escaped as a
// URI so a database whose path contains '?' or '#' cannot truncate or inject
// query parameters.
func dsn(path string) string {
	q := url.Values{}
	for _, p := range pragmas {
		q.Add("_pragma", p)
	}

	return "file:" + (&url.URL{Path: path}).EscapedPath() + "?" + q.Encode()
}

// Open opens (creating if needed) the SQLite database at path with the standard
// PRAGMAs applied to every pooled connection. The returned *sql.DB is ready for
// migrations and queries, and is safe for concurrent use.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, fmt.Errorf("storage: open %q: %w", path, err)
	}
	// Kept at one connection: the DSN makes the pragmas hold on every connection,
	// but enabling the pool is a separate change (it regresses the watcher's
	// moved-in directory sweep) and is tracked on its own.
	db.SetMaxOpenConns(1)

	// sql.Open is lazy: it validates neither the path nor the pragmas. Force one
	// connection now so a bad path or a rejected pragma is reported here rather
	// than at the first unrelated query.
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()

		return nil, fmt.Errorf("storage: open %q: %w", path, err)
	}

	return db, nil
}
