//go:build !embed

package eval

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/storage"
)

// TestBuildRetrieverSemanticNotSupported verifies that buildRetriever returns a
// descriptive error when Semantic is true but the binary was built without
// -tags embed (embed.Supported == false).  This file is excluded from embed
// builds via the build tag because the semantic path succeeds (or fails
// differently) when embedding is compiled in.
func TestBuildRetrieverSemanticNotSupported(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "sem.db")
	db, err := storage.Open(ctx, dbPath)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, storage.Migrate(db))

	r, err := buildRetriever(ctx, db, quietLogger(), Options{Semantic: true})
	require.Error(t, err)
	require.Nil(t, r)
	require.Contains(t, err.Error(), "embed-tagged build")
}

// TestRunSemanticWithoutEmbedTag verifies that Run propagates the
// "embed-tagged build" error from buildRetriever when Semantic is true in a
// default (non-embed) binary.
func TestRunSemanticWithoutEmbedTag(t *testing.T) {
	_, err := Run(context.Background(), quietLogger(), Options{
		Bundle:   filepath.Join("testdata", "bundle"),
		Include:  []string{"**/*.md"},
		Semantic: true,
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "embed-tagged build")
}
