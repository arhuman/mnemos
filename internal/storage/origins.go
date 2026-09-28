package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Origin is a registered external tree indexed in place, read-only (ADR-0013).
// Prefix is the URI namespace its documents are stored under; Path is the
// resolved absolute directory on disk. Collection is the fallback label for
// documents that do not declare a `collection:` frontmatter.
type Origin struct {
	Prefix        string
	Path          string
	Collection    string
	RegisteredAt  string
	LastIndexedAt string
}

// ErrOriginExists means the prefix, or the path, is already registered. Both are
// unique: one prefix per namespace, and one namespace per tree, so the same
// content is never indexed twice under two citations.
var ErrOriginExists = errors.New("storage: origin already registered")

// ErrOriginNotFound means no origin is registered under that prefix.
var ErrOriginNotFound = errors.New("storage: origin not found")

// InsertOrigin registers an origin. It returns ErrOriginExists if the prefix or
// the path is taken, so a caller can report the conflict rather than silently
// rebinding a namespace that existing document URIs depend on.
func InsertOrigin(ctx context.Context, db *sql.DB, o Origin) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO origins (prefix, path, collection, registered_at) VALUES (?, ?, ?, ?)`,
		o.Prefix, o.Path, o.Collection, o.RegisteredAt)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("%w: prefix %q or path %q", ErrOriginExists, o.Prefix, o.Path)
		}

		return fmt.Errorf("storage: insert origin %q: %w", o.Prefix, err)
	}

	return nil
}

// GetOrigin returns the origin registered under prefix, or ErrOriginNotFound.
func GetOrigin(ctx context.Context, db *sql.DB, prefix string) (Origin, error) {
	var o Origin
	var lastIndexed sql.NullString
	err := db.QueryRowContext(ctx,
		`SELECT prefix, path, collection, registered_at, last_indexed_at FROM origins WHERE prefix = ?`,
		prefix).Scan(&o.Prefix, &o.Path, &o.Collection, &o.RegisteredAt, &lastIndexed)
	if errors.Is(err, sql.ErrNoRows) {
		return Origin{}, fmt.Errorf("%w: %q", ErrOriginNotFound, prefix)
	}
	if err != nil {
		return Origin{}, fmt.Errorf("storage: get origin %q: %w", prefix, err)
	}
	o.LastIndexedAt = lastIndexed.String

	return o, nil
}

// ListOrigins returns every registered origin, ordered by prefix so listings and
// JSON exports are diffable.
func ListOrigins(ctx context.Context, db *sql.DB) ([]Origin, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT prefix, path, collection, registered_at, last_indexed_at FROM origins ORDER BY prefix`)
	if err != nil {
		return nil, fmt.Errorf("storage: list origins: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Origin
	for rows.Next() {
		var o Origin
		var lastIndexed sql.NullString
		if err := rows.Scan(&o.Prefix, &o.Path, &o.Collection, &o.RegisteredAt, &lastIndexed); err != nil {
			return nil, fmt.Errorf("storage: scan origin: %w", err)
		}
		o.LastIndexedAt = lastIndexed.String
		out = append(out, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("storage: iterate origins: %w", err)
	}

	return out, nil
}

// TouchOriginIndexed records when an origin was last indexed, so a stale
// registration is diagnosable without walking its tree.
func TouchOriginIndexed(ctx context.Context, db *sql.DB, prefix, at string) error {
	res, err := db.ExecContext(ctx, `UPDATE origins SET last_indexed_at = ? WHERE prefix = ?`, at, prefix)
	if err != nil {
		return fmt.Errorf("storage: touch origin %q: %w", prefix, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("storage: touch origin %q: %w", prefix, err)
	}
	if n == 0 {
		return fmt.Errorf("%w: %q", ErrOriginNotFound, prefix)
	}

	return nil
}

// DeleteOrigin removes a registration. It does not touch the documents indexed
// under that prefix: eviction is the caller's explicit step, so an accidental
// unregister never silently discards indexed content.
func DeleteOrigin(ctx context.Context, db *sql.DB, prefix string) error {
	res, err := db.ExecContext(ctx, `DELETE FROM origins WHERE prefix = ?`, prefix)
	if err != nil {
		return fmt.Errorf("storage: delete origin %q: %w", prefix, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("storage: delete origin %q: %w", prefix, err)
	}
	if n == 0 {
		return fmt.Errorf("%w: %q", ErrOriginNotFound, prefix)
	}

	return nil
}

// isUniqueViolation reports whether err is a SQLite UNIQUE constraint failure.
// The driver is imported anonymously (only the database/sql interface is used),
// so there is no exported error type to match on and the check is textual. It is
// used only to turn a conflict into a clearer message: a false negative degrades
// to the raw driver error, never to a wrong write.
func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}
