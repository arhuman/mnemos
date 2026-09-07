package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/config"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))

	return p
}

func exists(string) bool  { return true }
func missing(string) bool { return false }

func TestDefaultTOMLParsesToDefaults(t *testing.T) {
	require.NotEmpty(t, config.DefaultTOML())

	cfg, err := config.Load("", missing)
	require.NoError(t, err)
	require.Equal(t, 700, cfg.Chunking.TargetTokens)
	require.Equal(t, 80, cfg.Chunking.OverlapTokens)
	require.Equal(t, 12, cfg.Search.DefaultLimit)
	require.Equal(t, "stdio", cfg.MCP.Transport)
	require.False(t, cfg.MCP.AllowWrite)
	require.True(t, cfg.Security.ExcludeSecrets)
	require.Contains(t, cfg.Indexing.Include, "**/*.md")
}

func TestEmbeddingModel(t *testing.T) {
	t.Run("defaults to all-MiniLM-L6-v2", func(t *testing.T) {
		cfg, err := config.Load("", missing)
		require.NoError(t, err)
		require.Equal(t, "all-MiniLM-L6-v2", cfg.Embedding.Model)
	})

	t.Run("user file overrides the model", func(t *testing.T) {
		dir := t.TempDir()
		p := writeFile(t, dir, "mnemos.toml", "[embedding]\nmodel = \"paraphrase-multilingual-MiniLM-L12-v2\"\n")
		cfg, err := config.Load(p, exists)
		require.NoError(t, err)
		require.Equal(t, "paraphrase-multilingual-MiniLM-L12-v2", cfg.Embedding.Model)
	})
}

func TestHiddenCollectionsDefaultsEmpty(t *testing.T) {
	cfg, err := config.Load("", missing)
	require.NoError(t, err)
	require.Empty(t, cfg.HiddenCollections(), "no collection is hidden by default")
}

func TestHiddenCollectionsFromVisibilityDeny(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "mnemos.toml", `
[security.visibility]
deny = ["perso", "epfl"]
`)
	cfg, err := config.Load(path, exists)
	require.NoError(t, err)
	require.Equal(t, []string{"perso", "epfl"}, cfg.HiddenCollections())
}

func TestLoadOverlaysFile(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "mnemos.toml", `
[search]
default_limit = 5

[mcp]
allow_write = true
`)

	cfg, err := config.Load(path, exists)
	require.NoError(t, err)
	// Overridden values win.
	require.Equal(t, 5, cfg.Search.DefaultLimit)
	require.True(t, cfg.MCP.AllowWrite)
	// Unspecified keys keep their defaults.
	require.Equal(t, 700, cfg.Chunking.TargetTokens)
	require.Equal(t, "stdio", cfg.MCP.Transport)
}

func TestLoadMissingFileFallsBackToDefaults(t *testing.T) {
	cfg, err := config.Load("/does/not/exist.toml", missing)
	require.NoError(t, err)
	require.Equal(t, 12, cfg.Search.DefaultLimit)
}

func TestLoadEmptyPathUsesDefaults(t *testing.T) {
	cfg, err := config.Load("", missing)
	require.NoError(t, err)
	require.Equal(t, 700, cfg.Chunking.TargetTokens)
}

func TestLoadMalformedFileErrors(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "bad.toml", "this = = not valid toml")
	_, err := config.Load(path, exists)
	require.Error(t, err)
}

func TestDefaultResultModeIsText(t *testing.T) {
	cfg, err := config.Load("", missing)
	require.NoError(t, err)
	require.Equal(t, "text", cfg.MCP.ResultMode)
}

func TestLoadAcceptsValidResultModes(t *testing.T) {
	for _, mode := range []string{"text", "structured", "both"} {
		dir := t.TempDir()
		path := writeFile(t, dir, "mnemos.toml", "[mcp]\nresult_mode = \""+mode+"\"\n")
		cfg, err := config.Load(path, exists)
		require.NoError(t, err)
		require.Equal(t, mode, cfg.MCP.ResultMode)
	}
}

func TestLoadRejectsInvalidResultMode(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "mnemos.toml", "[mcp]\nresult_mode = \"xml\"\n")
	_, err := config.Load(path, exists)
	require.Error(t, err)
}
