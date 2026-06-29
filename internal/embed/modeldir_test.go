package embed

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModelDir(t *testing.T) {
	dir, err := ModelDir(DefaultModel)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(".mnemos", "models", DefaultModel), filepath.Join(".mnemos", "models", filepath.Base(dir)))
	require.True(t, filepath.IsAbs(dir), "model dir must be absolute")
	require.Equal(t, DefaultModel, filepath.Base(dir))
}
