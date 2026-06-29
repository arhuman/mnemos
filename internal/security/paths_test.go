package security_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/security"
)

func TestResolveWithinNominal(t *testing.T) {
	root := t.TempDir()

	abs, uri, err := security.ResolveWithin(root, "perso/note-x.md", nil)
	require.NoError(t, err)
	require.Equal(t, "perso/note-x.md", uri)
	require.Equal(t, filepath.Join(root, "perso", "note-x.md"), abs)
}

func TestResolveWithinAbsoluteInsideRoot(t *testing.T) {
	root := t.TempDir()
	inside := filepath.Join(root, "tech", "note.md")

	abs, uri, err := security.ResolveWithin(root, inside, nil)
	require.NoError(t, err)
	require.Equal(t, "tech/note.md", uri)
	require.Equal(t, inside, abs)
}

func TestResolveWithinRejectsTraversal(t *testing.T) {
	root := t.TempDir()

	_, _, err := security.ResolveWithin(root, "../escape.md", nil)
	require.Error(t, err)
}

func TestResolveWithinRejectsAbsoluteOutsideRoot(t *testing.T) {
	root := t.TempDir()

	_, _, err := security.ResolveWithin(root, "/etc/passwd", nil)
	require.Error(t, err)
}

func TestResolveWithinRejectsRootItself(t *testing.T) {
	root := t.TempDir()

	_, _, err := security.ResolveWithin(root, ".", nil)
	require.Error(t, err)
}

func TestResolveWithinRejectsMnemosDir(t *testing.T) {
	root := t.TempDir()

	_, _, err := security.ResolveWithin(root, ".mnemos/capture/x.md", nil)
	require.Error(t, err)
}

func TestResolveWithinRejectsExcludedPattern(t *testing.T) {
	root := t.TempDir()

	_, _, err := security.ResolveWithin(root, "secrets/key.md", []string{"**/secrets/**"})
	require.Error(t, err)

	_, _, err = security.ResolveWithin(root, "config/.env", []string{"**/.env"})
	require.Error(t, err)
}

func TestResolveWithinRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()

	// A symlinked subdirectory inside root that points outside root: a write
	// target under it would escape the tree.
	link := filepath.Join(root, "evil")
	require.NoError(t, os.Symlink(outside, link))

	_, _, err := security.ResolveWithin(root, "evil/note.md", nil)
	require.Error(t, err)
}

func TestResolveWithinRejectsEmpty(t *testing.T) {
	root := t.TempDir()

	_, _, err := security.ResolveWithin(root, "  ", nil)
	require.Error(t, err)
}
