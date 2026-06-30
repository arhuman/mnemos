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

func TestDefaultTOMLParsesToDefaults(t *testing.T) {
	require.NotEmpty(t, config.DefaultTOML())

	cfg, err := config.Load(nil, func(string) bool { return false })
	require.NoError(t, err)
	require.Equal(t, ".mnemos/mnemos.db", cfg.Storage.Path)
	require.Equal(t, 700, cfg.Chunking.TargetTokens)
	require.Equal(t, 80, cfg.Chunking.OverlapTokens)
	require.Equal(t, 12, cfg.Search.DefaultLimit)
	require.Equal(t, "stdio", cfg.MCP.Transport)
	require.False(t, cfg.MCP.AllowWrite)
	require.True(t, cfg.Security.ExcludeSecrets)
	require.Contains(t, cfg.Indexing.Include, "**/*.md")
}

func TestLoadOverlaysUserFile(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, config.FileName, `
[storage]
path = "/custom/mnemos.db"

[search]
default_limit = 5
`)

	cfg, err := config.Load([]string{path}, func(string) bool { return true })
	require.NoError(t, err)
	// Overridden values win.
	require.Equal(t, "/custom/mnemos.db", cfg.Storage.Path)
	require.Equal(t, 5, cfg.Search.DefaultLimit)
	// Unspecified keys keep their defaults.
	require.Equal(t, 700, cfg.Chunking.TargetTokens)
	require.Equal(t, "stdio", cfg.MCP.Transport)
}

// TestLoadLaterFileOverridesEarlier exercises the home-then-project layering:
// the project file (later) wins on conflicts while the home file's other keys
// survive.
func TestLoadLaterFileOverridesEarlier(t *testing.T) {
	dir := t.TempDir()
	home := writeFile(t, dir, "home.toml", `
[storage]
path = "/home/mnemos.db"

[search]
default_limit = 99
`)
	project := writeFile(t, dir, "project.toml", `
[search]
default_limit = 3
`)

	cfg, err := config.Load([]string{home, project}, func(string) bool { return true })
	require.NoError(t, err)
	// Project file overrides the home value.
	require.Equal(t, 3, cfg.Search.DefaultLimit)
	// Home-only keys persist since the project file is silent on them.
	require.Equal(t, "/home/mnemos.db", cfg.Storage.Path)
}

func TestLoadMissingFileFallsBackToDefaults(t *testing.T) {
	cfg, err := config.Load([]string{"/does/not/exist.toml"}, func(string) bool { return false })
	require.NoError(t, err)
	require.Equal(t, ".mnemos/mnemos.db", cfg.Storage.Path)
}

func TestLoadMalformedFileErrors(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "bad.toml", "this = = not valid toml")
	_, err := config.Load([]string{path}, func(string) bool { return true })
	require.Error(t, err)
}

func TestResolveExplicitWins(t *testing.T) {
	paths, treeRoot := config.Resolve(filepath.Join("sub", "x.toml"), "/home/me")
	// Explicit path replaces auto-discovery entirely.
	require.Equal(t, []string{filepath.Join("sub", "x.toml")}, paths)
	require.Equal(t, "sub", treeRoot)
}

func TestResolveAutoDiscover(t *testing.T) {
	paths, treeRoot := config.Resolve("", "/home/me")
	require.Equal(t, []string{filepath.Join("/home/me", config.FileName), config.FileName}, paths)
	// Order is precedence: home first, project last (wins). Tree root is cwd.
	require.Equal(t, ".", treeRoot)
}

func TestResolveAutoDiscoverNoHome(t *testing.T) {
	paths, treeRoot := config.Resolve("", "")
	require.Equal(t, []string{config.FileName}, paths)
	require.Equal(t, ".", treeRoot)
}

func captureCfg(dir string) *config.Config {
	return &config.Config{Capture: config.CaptureConfig{Dir: dir}}
}

func TestCaptureLocationDefaultRelative(t *testing.T) {
	root := t.TempDir()

	absDir, relDir, err := captureCfg(".mnemos/capture").CaptureLocation(root)
	require.NoError(t, err)
	require.Equal(t, filepath.Join(root, ".mnemos", "capture"), absDir)
	require.Equal(t, filepath.Join(".mnemos", "capture"), relDir)
}

func TestCaptureLocationAbsoluteInsideTree(t *testing.T) {
	root := t.TempDir()
	abs := filepath.Join(root, ".mnemos", "capture")

	absDir, relDir, err := captureCfg(abs).CaptureLocation(root)
	require.NoError(t, err)
	require.Equal(t, abs, absDir)
	require.Equal(t, filepath.Join(".mnemos", "capture"), relDir)
}

func TestCaptureLocationRejectsEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	_, _, err := captureCfg(outside).CaptureLocation(root)
	require.Error(t, err)

	_, _, err = captureCfg("../escape").CaptureLocation(root)
	require.Error(t, err)
}

func TestValidateRejectsEscapingCaptureDir(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	require.NoError(t, captureCfg(".mnemos/capture").Validate(root))
	require.Error(t, captureCfg(outside).Validate(root))
}
