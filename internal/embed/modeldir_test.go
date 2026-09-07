package embed

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModelName(t *testing.T) {
	t.Run("recovers the name from a model dir", func(t *testing.T) {
		dir, err := ModelDir("paraphrase-multilingual-MiniLM-L12-v2")
		require.NoError(t, err)
		require.Equal(t, "paraphrase-multilingual-MiniLM-L12-v2", ModelName(dir))
	})

	t.Run("round-trips every installed name", func(t *testing.T) {
		for _, name := range []string{DefaultModel, "some-other-model", "a"} {
			dir, err := ModelDir(name)
			require.NoError(t, err)
			require.Equal(t, name, ModelName(dir))
		}
	})

	t.Run("tolerates a trailing separator", func(t *testing.T) {
		require.Equal(t, "m", ModelName(filepath.Join("x", "m")+string(filepath.Separator)))
	})

	t.Run("degenerate dirs fall back to the default", func(t *testing.T) {
		require.Equal(t, DefaultModel, ModelName(""))
		require.Equal(t, DefaultModel, ModelName("."))
		require.Equal(t, DefaultModel, ModelName(string(filepath.Separator)))
	})
}

func TestResolveModel(t *testing.T) {
	require.Equal(t, DefaultModel, ResolveModel(""), "empty config must mean the default")
	require.Equal(t, "custom", ResolveModel("custom"))
}

func TestModelDir(t *testing.T) {
	dir, err := ModelDir(DefaultModel)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(".mnemos", "models", DefaultModel), filepath.Join(".mnemos", "models", filepath.Base(dir)))
	require.True(t, filepath.IsAbs(dir), "model dir must be absolute")
	require.Equal(t, DefaultModel, filepath.Base(dir))
}
