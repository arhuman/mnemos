//go:build !embed

package embed

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNoopEmbedder(t *testing.T) {
	require.False(t, Supported, "default build must report no embedding support")

	e, err := New("ignored")
	require.NoError(t, err)
	require.Equal(t, Dim, e.Dim())
	require.Equal(t, DefaultModel, e.Model())

	vecs, err := e.Embed(context.Background(), []string{"hello"})
	require.Nil(t, vecs)
	require.True(t, errors.Is(err, ErrNotSupported), "noop Embed must return ErrNotSupported")
}
