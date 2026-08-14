package tui

import (
	"os"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"
)

// TestInitIssuesNoCommand proves the document is already loaded at construction.
func TestInitIssuesNoCommand(t *testing.T) {
	require.Nil(t, Model{}.Init())
}

// TestUpdateIgnoresUnknownMessages proves a message the editor does not handle
// leaves the model alone.
func TestUpdateIgnoresUnknownMessages(t *testing.T) {
	_, m := openTask(t)

	next, cmd := m.Update(struct{}{})
	require.Nil(t, cmd)
	require.Equal(t, m.uri, next.(Model).uri)
}

// TestWindowResizeLaysOutPanes proves a resize splits the width across the three
// panes and leaves the chrome room.
func TestWindowResizeLaysOutPanes(t *testing.T) {
	_, m := openTask(t)

	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	got, ok := next.(Model)
	require.True(t, ok)
	require.Equal(t, 38, got.paneWidth())
	require.Equal(t, 35, got.paneHeight())

	// A cramped terminal falls back on minimums rather than negative sizes.
	tiny := got.resize(10, 3)
	require.Equal(t, 20, tiny.paneWidth())
	require.Equal(t, 5, tiny.paneHeight())
}

// TestContentPaneScrolls proves the viewport keys move the body without marking
// it dirty.
func TestContentPaneScrolls(t *testing.T) {
	_, m := openTask(t)
	m.focus = paneContent
	m = m.resize(80, 10)
	m.viewport.SetContent("a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl\nm\nn\no\np\n")

	for _, k := range []string{keyDown, keyPageDown, keyUp, keyPageUp} {
		m = send(t, m, k)
	}
	require.False(t, m.isDirty())
	require.Equal(t, 0, m.viewport.YOffset)
}

// TestEditorCommandPrefersEnv proves $EDITOR wins, then $VISUAL, then the
// built-in fallback.
func TestEditorCommandPrefersEnv(t *testing.T) {
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")
	require.Equal(t, defaultEditor, editorCommand())

	t.Setenv("VISUAL", "emacs")
	require.Equal(t, "emacs", editorCommand())

	t.Setenv("EDITOR", "nano")
	require.Equal(t, "nano", editorCommand())
}

// TestWriteTempBodyStagesTheBody proves the $EDITOR hand-off file carries the
// body and reads back through the same seam the model uses.
func TestWriteTempBodyStagesTheBody(t *testing.T) {
	path, err := writeTempBody("## Goal\n\nBody.\n")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Remove(path) })
	require.FileExists(t, path)

	got, err := readFile(path)
	require.NoError(t, err)
	require.Equal(t, "## Goal\n\nBody.\n", got)

	_, err = readFile(path + ".missing")
	require.Error(t, err)
}

// TestOpenEditorIssuesAnExecCommand proves the shell-out is handed to bubbletea
// rather than run inline. The command itself is never executed here.
func TestOpenEditorIssuesAnExecCommand(t *testing.T) {
	t.Setenv("EDITOR", "true")
	_, m := openTask(t)
	m.focus = paneContent

	next, cmd := sendCmd(t, m, keyEdit)
	require.NotNil(t, cmd)
	require.False(t, next.isDirty(), "nothing is dirty until the editor returns")
}

// TestClamp covers the cursor bounds helper at both edges and on an empty list.
func TestClamp(t *testing.T) {
	require.Equal(t, 0, clamp(3, 0))
	require.Equal(t, 0, clamp(-1, 4))
	require.Equal(t, 3, clamp(9, 4))
	require.Equal(t, 2, clamp(2, 4))
}

// TestIsGateKey covers the confirm-again key set.
func TestIsGateKey(t *testing.T) {
	require.True(t, isGateKey(keyQuit))
	require.True(t, isGateKey(keyEnter))
	require.True(t, isGateKey(keyBack))
	require.False(t, isGateKey(keySave))
	require.False(t, isGateKey(keyFocusNext))
}
