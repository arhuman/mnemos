package storage_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/storage"
	"github.com/arhuman/mnemos/internal/testutil"
)

func origin(prefix, path string) storage.Origin {
	return storage.Origin{
		Prefix: prefix, Path: path, Collection: "c",
		RegisteredAt: "2026-01-01T00:00:00Z",
	}
}

func TestOriginRoundTrip(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()

	require.NoError(t, storage.InsertOrigin(ctx, db, origin("spec", "/vol/spec")))

	got, err := storage.GetOrigin(ctx, db, "spec")
	require.NoError(t, err)
	require.Equal(t, "/vol/spec", got.Path)
	require.Equal(t, "c", got.Collection)
	require.Empty(t, got.LastIndexedAt, "a fresh registration has never been indexed")
}

// TestOriginPrefixIsUnique proves a namespace cannot be silently rebound: existing
// document URIs depend on it.
func TestOriginPrefixIsUnique(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()

	require.NoError(t, storage.InsertOrigin(ctx, db, origin("spec", "/vol/a")))
	err := storage.InsertOrigin(ctx, db, origin("spec", "/vol/b"))
	require.ErrorIs(t, err, storage.ErrOriginExists)
}

// TestOriginPathIsUnique proves one tree cannot be registered under two prefixes,
// which would index it twice and return the same content as two citations.
func TestOriginPathIsUnique(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()

	require.NoError(t, storage.InsertOrigin(ctx, db, origin("spec", "/vol/a")))
	err := storage.InsertOrigin(ctx, db, origin("other", "/vol/a"))
	require.ErrorIs(t, err, storage.ErrOriginExists)
}

func TestGetOriginUnknownPrefix(t *testing.T) {
	db := testutil.NewDB(t)
	_, err := storage.GetOrigin(context.Background(), db, "nope")
	require.ErrorIs(t, err, storage.ErrOriginNotFound)
}

func TestListOriginsOrderedByPrefix(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()

	require.NoError(t, storage.InsertOrigin(ctx, db, origin("zeta", "/vol/z")))
	require.NoError(t, storage.InsertOrigin(ctx, db, origin("alpha", "/vol/a")))

	got, err := storage.ListOrigins(ctx, db)
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, "alpha", got[0].Prefix, "listings are ordered so exports are diffable")
	require.Equal(t, "zeta", got[1].Prefix)
}

func TestTouchOriginIndexed(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	require.NoError(t, storage.InsertOrigin(ctx, db, origin("spec", "/vol/spec")))

	require.NoError(t, storage.TouchOriginIndexed(ctx, db, "spec", "2026-02-02T00:00:00Z"))

	got, err := storage.GetOrigin(ctx, db, "spec")
	require.NoError(t, err)
	require.Equal(t, "2026-02-02T00:00:00Z", got.LastIndexedAt)

	require.ErrorIs(t, storage.TouchOriginIndexed(ctx, db, "nope", "t"), storage.ErrOriginNotFound)
}

func TestDeleteOrigin(t *testing.T) {
	db := testutil.NewDB(t)
	ctx := context.Background()
	require.NoError(t, storage.InsertOrigin(ctx, db, origin("spec", "/vol/spec")))

	require.NoError(t, storage.DeleteOrigin(ctx, db, "spec"))
	_, err := storage.GetOrigin(ctx, db, "spec")
	require.ErrorIs(t, err, storage.ErrOriginNotFound)

	require.ErrorIs(t, storage.DeleteOrigin(ctx, db, "spec"), storage.ErrOriginNotFound)
}
