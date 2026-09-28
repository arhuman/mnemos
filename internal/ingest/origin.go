package ingest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/arhuman/mnemos/internal/chunk"
	"github.com/arhuman/mnemos/internal/storage"
)

// Origin registration errors. They are distinguished so a CLI can explain what
// to change rather than printing a generic failure.
var (
	// ErrOriginPathNotDir means the path is not an existing directory.
	ErrOriginPathNotDir = errors.New("origin: path is not a directory")
	// ErrOriginPrefixInvalid means the prefix is not a single usable uri segment.
	ErrOriginPrefixInvalid = errors.New("origin: invalid prefix")
	// ErrOriginPrefixTaken means the prefix collides with existing kb content, so
	// registering it would put two different files under one uri namespace.
	ErrOriginPrefixTaken = errors.New("origin: prefix collides with kb content")
	// ErrOriginInsideKB means the path is inside the kb, where content is already
	// indexed and writable; registering it would create a second, read-only view
	// of the same bytes under a different uri.
	ErrOriginInsideKB = errors.New("origin: path is inside the kb")
)

// RegisterOrigin validates and records an external origin (ADR-0013). It does not
// index anything: registration and indexing are separate so a mistyped path fails
// before any content is written.
//
// The path is resolved (symlinks evaluated) before storage, so a later swap of an
// intermediate symlink cannot silently redirect the origin somewhere else.
func RegisterOrigin(ctx context.Context, db *sql.DB, kbRoot, prefix, path, collection string) (storage.Origin, error) {
	if err := ValidateOriginPrefix(prefix); err != nil {
		return storage.Origin{}, err
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return storage.Origin{}, fmt.Errorf("origin: resolve %q: %w", path, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return storage.Origin{}, fmt.Errorf("%w: %q: %w", ErrOriginPathNotDir, path, err)
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return storage.Origin{}, fmt.Errorf("%w: %q", ErrOriginPathNotDir, path)
	}
	if err := checkOutsideKB(kbRoot, resolved); err != nil {
		return storage.Origin{}, err
	}
	if err := checkPrefixFree(kbRoot, prefix); err != nil {
		return storage.Origin{}, err
	}

	o := storage.Origin{
		Prefix:       prefix,
		Path:         resolved,
		Collection:   collection,
		RegisteredAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := storage.InsertOrigin(ctx, db, o); err != nil {
		return storage.Origin{}, err
	}

	return o, nil
}

// ValidateOriginPrefix accepts a single, plain uri segment. A multi-segment or
// dotted prefix would make the namespace ambiguous against real subdirectories
// and against the "." / ".." the confinement guard rejects everywhere else.
func ValidateOriginPrefix(prefix string) error {
	switch {
	case prefix == "":
		return fmt.Errorf("%w: empty", ErrOriginPrefixInvalid)
	case strings.ContainsAny(prefix, `/\`):
		return fmt.Errorf("%w: %q must be a single path segment", ErrOriginPrefixInvalid, prefix)
	case prefix == "." || prefix == "..":
		return fmt.Errorf("%w: %q", ErrOriginPrefixInvalid, prefix)
	case strings.TrimSpace(prefix) != prefix:
		return fmt.Errorf("%w: %q has surrounding whitespace", ErrOriginPrefixInvalid, prefix)
	}

	return nil
}

// checkOutsideKB rejects an origin whose resolved path is the kb or sits inside
// it. Such content is already indexed and writable in place; a second read-only
// view of the same bytes under a different uri would make two citations for one
// file, and `origin remove` would then evict documents the kb still owns.
func checkOutsideKB(kbRoot, resolved string) error {
	kbReal, err := filepath.EvalSymlinks(kbRoot)
	if err != nil {
		kbReal = kbRoot
	}
	rel, err := filepath.Rel(kbReal, resolved)
	if err != nil {
		// Unrelatable paths (different volumes on Windows) mean the containment
		// question cannot be answered. Refuse rather than register: a silent pass
		// here would be a check that looks enforced and is not.
		return fmt.Errorf("origin: cannot compare %q against the kb: %w", resolved, err)
	}
	if rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
		return fmt.Errorf("%w: %q; content already under the kb is indexed by 'mnemos ingest'", ErrOriginInsideKB, resolved)
	}

	return nil
}

// checkPrefixFree rejects a prefix that already names something in the kb, so a
// registered namespace and a real kb path can never mint the same uri.
func checkPrefixFree(kbRoot, prefix string) error {
	if _, err := os.Lstat(filepath.Join(kbRoot, prefix)); err == nil {
		return fmt.Errorf("%w: %q already exists under the kb", ErrOriginPrefixTaken, prefix)
	}

	return nil
}

// OriginReindexSummary reports one origin's reindex.
type OriginReindexSummary struct {
	Prefix  string
	Scanned int
	Indexed int
	Skipped int
	Removed int
	Chunks  int
}

// ReindexOrigin indexes a registered origin in place, under its namespace, and
// evicts documents in that namespace whose file is gone. It never writes to the
// origin tree.
//
// A missing or unreadable origin root fails with ErrReconcileRootMissing and
// evicts nothing: an unmounted volume and an emptied tree look identical from
// inside the walk, and losing a namespace is not recoverable by re-running.
func (p *Pipeline) ReindexOrigin(ctx context.Context, o storage.Origin, rules Rules, cfg chunk.Config) (OriginReindexSummary, error) {
	sum := OriginReindexSummary{Prefix: o.Prefix}
	if _, err := os.Stat(o.Path); err != nil {
		return sum, fmt.Errorf("%w: origin %q at %q: %w", ErrReconcileRootMissing, o.Prefix, o.Path, err)
	}

	run, err := p.Run(ctx, Options{
		Root:       o.Path,
		URIBase:    o.Path,
		URIPrefix:  o.Prefix,
		Collection: o.Collection,
		Rules:      rules,
		Chunking:   cfg,
		Reconcile:  true,
	})
	if err != nil {
		return sum, err
	}

	sum.Scanned, sum.Indexed = run.FilesScanned, run.FilesIngested
	sum.Skipped, sum.Removed, sum.Chunks = run.FilesSkipped, run.FilesRemoved, run.ChunksWritten

	if err := storage.TouchOriginIndexed(ctx, p.db, o.Prefix, time.Now().UTC().Format(time.RFC3339)); err != nil {
		return sum, err
	}

	return sum, nil
}

// RemoveOrigin unregisters an origin and evicts every document in its namespace,
// so a removed origin stops being citable. The files themselves are never
// touched: they are canonical under their own tooling, which is the whole point
// of a read-only origin.
func RemoveOrigin(ctx context.Context, db *sql.DB, prefix string) (int, error) {
	if _, err := storage.GetOrigin(ctx, db, prefix); err != nil {
		return 0, err
	}
	uris, err := storage.ListURIs(ctx, db)
	if err != nil {
		return 0, err
	}
	var owned []string
	for _, uri := range uris {
		if uriUnderPrefix(uri, prefix) {
			owned = append(owned, uri)
		}
	}
	if len(owned) > 0 {
		if err := deleteURIBatch(ctx, db, owned); err != nil {
			return 0, err
		}
	}
	if err := storage.DeleteOrigin(ctx, db, prefix); err != nil {
		return len(owned), err
	}

	return len(owned), nil
}
