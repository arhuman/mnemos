package workspace_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/workspace"
)

func noEnv(string) string { return "" }

func TestNewDerivesFixedSubpaths(t *testing.T) {
	l := workspace.New("/home/u/.mnemos")

	require.Equal(t, "/home/u/.mnemos", l.MnemosDir)
	require.Equal(t, "/home/u/.mnemos/mnemos.toml", l.Config)
	require.Equal(t, "/home/u/.mnemos/kb", l.KB)
	require.Equal(t, "/home/u/.mnemos/kb/capture", l.Capture)
	require.Equal(t, "/home/u/.mnemos/state/index.db", l.DB)
	require.Equal(t, "/home/u/.mnemos/models", l.Models)
}

func TestResolveConfigWins(t *testing.T) {
	cfg := filepath.Join(t.TempDir(), "custom.toml")

	l, err := workspace.Resolve(workspace.Options{
		ConfigPath:  cfg,
		ExplicitDir: "/ignored",
		Env:         func(string) string { return "/also-ignored" },
		Home:        "/home/u",
		Cwd:         t.TempDir(),
	})
	require.NoError(t, err)
	require.Equal(t, filepath.Dir(cfg), l.MnemosDir)
	require.Equal(t, cfg, l.Config)
	require.Equal(t, filepath.Join(filepath.Dir(cfg), "kb"), l.KB)
}

func TestResolveExplicitDirBeatsEnvAndDefault(t *testing.T) {
	dir := t.TempDir()

	l, err := workspace.Resolve(workspace.Options{
		ExplicitDir: dir,
		Env:         func(string) string { return "/env" },
		Home:        "/home/u",
	})
	require.NoError(t, err)
	require.Equal(t, dir, l.MnemosDir)
	require.Contains(t, l.Source, "--mnemos-dir")
}

func TestResolveEnvBeatsProjectAndDefault(t *testing.T) {
	envDir := t.TempDir()
	cwd := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(cwd, ".mnemos"), 0o750))

	l, err := workspace.Resolve(workspace.Options{
		Env:  func(k string) string { return map[string]string{"MNEMOS_DIR": envDir}[k] },
		Home: "/home/u",
		Cwd:  cwd,
	})
	require.NoError(t, err)
	require.Equal(t, envDir, l.MnemosDir)
	require.Contains(t, l.Source, "MNEMOS_DIR")
}

func TestResolveProjectDirWalkingUp(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".mnemos"), 0o750))
	deep := filepath.Join(root, "a", "b", "c")
	require.NoError(t, os.MkdirAll(deep, 0o750))

	l, err := workspace.Resolve(workspace.Options{Env: noEnv, Home: "/home/u", Cwd: deep})
	require.NoError(t, err)
	require.Equal(t, filepath.Join(root, ".mnemos"), l.MnemosDir)
	require.Contains(t, l.Source, "project")
}

func TestResolveProjectDiscoveryStopsAtGitRoot(t *testing.T) {
	// An ancestor has .mnemos, but a nearer .git marks the repo root: discovery
	// must stop there and fall back to the default rather than escaping the repo.
	ancestor := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(ancestor, ".mnemos"), 0o750))
	repo := filepath.Join(ancestor, "repo")
	require.NoError(t, os.MkdirAll(filepath.Join(repo, ".git"), 0o750))
	cwd := filepath.Join(repo, "pkg")
	require.NoError(t, os.MkdirAll(cwd, 0o750))

	l, err := workspace.Resolve(workspace.Options{Env: noEnv, Home: "/home/u", Cwd: cwd})
	require.NoError(t, err)
	require.Equal(t, filepath.Join("/home/u", ".mnemos"), l.MnemosDir)
	require.Contains(t, l.Source, "default")
}

func TestResolveDefaultsToHome(t *testing.T) {
	l, err := workspace.Resolve(workspace.Options{Env: noEnv, Home: "/home/u", Cwd: t.TempDir()})
	require.NoError(t, err)
	require.Equal(t, filepath.Join("/home/u", ".mnemos"), l.MnemosDir)
}

func TestResolveNoHomeErrors(t *testing.T) {
	_, err := workspace.Resolve(workspace.Options{Env: noEnv, Cwd: t.TempDir()})
	require.Error(t, err)
}
