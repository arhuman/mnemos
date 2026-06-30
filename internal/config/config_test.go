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
