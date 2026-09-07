//go:build !embed

package embed

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNoopEmbedder(t *testing.T) {
	require.False(t, Supported, "default build must report no embedding support")

	e, err := New(filepath.Join("some", "models", DefaultModel))
	require.NoError(t, err)
	require.Equal(t, Dim, e.Dim())
	require.Equal(t, DefaultModel, e.Model())

	// The name follows the directory rather than a constant, in both builds.
	other, err := New(filepath.Join("some", "models", "paraphrase-multilingual-MiniLM-L12-v2"))
	require.NoError(t, err)
	require.Equal(t, "paraphrase-multilingual-MiniLM-L12-v2", other.Model())

	vecs, err := e.Embed(context.Background(), []string{"hello"})
	require.Nil(t, vecs)
	require.True(t, errors.Is(err, ErrNotSupported), "noop Embed must return ErrNotSupported")
}
