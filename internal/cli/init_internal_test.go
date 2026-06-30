package cli

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/workspace"
)

// TestInitTargetPrecedence pins the resolution order of `init`'s target
// MNEMOS_DIR: an explicit --mnemos-dir wins over --config, which wins over
// --global, which wins over the project-local default.
func TestInitTargetPrecedence(t *testing.T) {
	const home = "/home/u"
	const cwd = "/work/proj"

	t.Run("mnemos-dir wins over config and global", func(t *testing.T) {
		got, err := initTarget(flags{mnemosDir: "/explicit/dir", configPath: "/other/mnemos.toml"}, true, home, cwd)
		require.NoError(t, err)
		require.Equal(t, "/explicit/dir", got)
	})

	t.Run("config dir wins over global", func(t *testing.T) {
		got, err := initTarget(flags{configPath: "/cfg/mnemos.toml"}, true, home, cwd)
		require.NoError(t, err)
		require.Equal(t, "/cfg", got)
	})

	t.Run("global selects home/.mnemos", func(t *testing.T) {
		got, err := initTarget(flags{}, true, home, cwd)
		require.NoError(t, err)
		require.Equal(t, filepath.Join(home, workspace.DirName), got)
	})

	t.Run("default is cwd/.mnemos", func(t *testing.T) {
		got, err := initTarget(flags{}, false, home, cwd)
		require.NoError(t, err)
		require.Equal(t, filepath.Join(cwd, workspace.DirName), got)
	})
}

// TestInitTargetGlobalNeedsHome asserts --global fails with a clear error when
// no home directory is available.
func TestInitTargetGlobalNeedsHome(t *testing.T) {
	_, err := initTarget(flags{}, true, "", "/work/proj")
	require.Error(t, err)
	require.Contains(t, err.Error(), "home directory")
}

// TestInitTargetRelativeMnemosDirIsAbsolutized asserts a relative --mnemos-dir
// is returned as an absolute path (initTarget calls filepath.Abs).
func TestInitTargetRelativeMnemosDirIsAbsolutized(t *testing.T) {
	got, err := initTarget(flags{mnemosDir: "rel/dir"}, false, "/home/u", "/work/proj")
	require.NoError(t, err)
	require.True(t, filepath.IsAbs(got), "expected absolute path, got %q", got)
	require.Equal(t, "rel/dir", filepath.Base(filepath.Dir(got))+"/"+filepath.Base(got))
}
