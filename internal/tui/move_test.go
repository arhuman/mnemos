package tui

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/browse"
	"github.com/arhuman/mnemos/internal/config"
)

// openMovable opens the task document in a fixture whose allow_delete gate is
// open, since a move deletes the old index entries.
func openMovable(t *testing.T) (fixture, Model) {
	t.Helper()
	f := newFixtureWith(t, func(c *config.Config) { c.MCP.AllowDelete = true })
	uri := f.seed(t, "tasks/ship.md", taskDoc)
	m, err := NewEditModel(context.Background(), f.svc, uri)
	require.NoError(t, err)

	return f, m
}

// TestBeginMovePrefillsCurrentURI proves the prompt opens over the document's
// own uri, so a rename is an edit of what is already there.
func TestBeginMovePrefillsCurrentURI(t *testing.T) {
	_, m := openMovable(t)

	m = send(t, m, keyMove)
	require.True(t, m.movingDoc)
	require.False(t, m.pickingDir)
	require.Equal(t, "tasks/ship.md", m.textInput.Value())
	require.Contains(t, m.statusMsg, "move to")
}

// TestBeginMoveWorksFromEveryPane proves the binding is global rather than
// pane-scoped.
func TestBeginMoveWorksFromEveryPane(t *testing.T) {
	_, base := openMovable(t)

	for _, focus := range []pane{paneNav, paneMetadata, paneContent} {
		m := base
		m.focus = focus
		m = send(t, m, keyMove)
		require.True(t, m.movingDoc)
	}
}

// TestBeginMoveIsBlockedByAFailedSave proves the move goes through the same gate
// as navigation: a document that could not be saved is not moved out from under
// the failed save.
func TestBeginMoveIsBlockedByAFailedSave(t *testing.T) {
	f, m := openMovable(t)
	m.focus = paneMetadata
	m.fieldCursor = 2
	m = send(t, m, keyRight)
	require.NoError(t, f.db.Close())

	m = send(t, m, keyMove)
	require.False(t, m.movingDoc, "the prompt does not open on the first attempt")
	require.True(t, m.statusIsErr)
	require.True(t, m.forcePending)

	m = send(t, m, keyMove)
	require.True(t, m.movingDoc, "the same key again pushes past the gate")
	require.False(t, m.forcePending)
}

// TestMoveInputEscCancels proves esc leaves the document exactly where it was.
func TestMoveInputEscCancels(t *testing.T) {
	f, m := openMovable(t)

	m = send(t, m, keyMove)
	m.textInput.SetValue("archive/ship.md")
	m = send(t, m, keyEscape)

	require.False(t, m.movingDoc)
	require.Equal(t, "tasks/ship.md", m.uri)
	require.Contains(t, m.statusMsg, "cancelled")
	require.False(t, m.statusIsErr)
	require.Equal(t, taskDoc, readFileT(t, f.treeRoot, "tasks/ship.md"))
}

// TestCommitMoveRelocatesAndReopens proves a committed move renames the file,
// reopens the editor at the new uri, and does not leave the vanished old uri on
// the back-stack.
func TestCommitMoveRelocatesAndReopens(t *testing.T) {
	f, m := openMovable(t)

	m = send(t, m, keyMove)
	m.textInput.SetValue("archive/shipped.md")
	m = send(t, m, keyEnter)

	require.False(t, m.statusIsErr, m.statusMsg)
	require.False(t, m.movingDoc)
	require.Equal(t, "archive/shipped.md", m.uri)
	require.Empty(t, m.history, "the old uri is gone, so it is not pushed")
	require.Contains(t, m.statusMsg, "moved to archive/shipped.md")
	require.NotContains(t, m.statusMsg, "inbound link")
	require.Equal(t, taskDoc, readFileT(t, f.treeRoot, "archive/shipped.md"))
	require.Equal(t, "Ship the thing", m.fields[1].Value, "the reopened document is the moved one")
}

// TestCommitMoveReportsDanglingLinks proves inbound links left pointing at the
// old path are surfaced, since the move does not rewrite them.
func TestCommitMoveReportsDanglingLinks(t *testing.T) {
	f, m := openMovable(t)
	f.seed(t, "hub.md", "# Hub\n\nSee [ship](tasks/ship.md).\n")

	m = send(t, m, keyMove)
	m.textInput.SetValue("archive/shipped.md")
	m = send(t, m, keyEnter)

	require.False(t, m.statusIsErr, m.statusMsg)
	require.Contains(t, m.statusMsg, "1 inbound link(s) still point at the old path")
}

// TestCommitMoveUnchangedPathIsANoop proves committing the pre-filled value
// leaves the document alone.
func TestCommitMoveUnchangedPathIsANoop(t *testing.T) {
	_, m := openMovable(t)

	m = send(t, m, keyMove)
	m = send(t, m, keyEnter)

	require.False(t, m.movingDoc)
	require.Equal(t, "tasks/ship.md", m.uri)
	require.Contains(t, m.statusMsg, "unchanged")
}

// TestCommitMoveFailureKeepsTheDocument proves a refused move (the delete gate
// is shut) reports the reason and changes nothing.
func TestCommitMoveFailureKeepsTheDocument(t *testing.T) {
	f, m := openTask(t)

	m = send(t, m, keyMove)
	m.textInput.SetValue("archive/shipped.md")
	m = send(t, m, keyEnter)

	require.True(t, m.statusIsErr)
	require.Contains(t, m.statusMsg, "move failed")
	require.Contains(t, m.statusMsg, "allow_delete")
	require.False(t, m.movingDoc)
	require.Equal(t, "tasks/ship.md", m.uri)
	require.Equal(t, taskDoc, readFileT(t, f.treeRoot, "tasks/ship.md"))
}

// TestDirPickerOffersDistinctDirectories proves tab opens the picker over the
// deduplicated directories that hold documents.
func TestDirPickerOffersDistinctDirectories(t *testing.T) {
	f, m := openMovable(t)
	f.seed(t, "archive/old.md", taskDoc)
	f.seed(t, "notes/a.md", "# A\n")
	f.seed(t, "notes/b.md", "# B\n")
	f.seed(t, "loose.md", "# Loose\n")

	m = send(t, m, keyMove)
	m = send(t, m, keyFocusNext)

	require.True(t, m.pickingDir)
	require.True(t, m.movingDoc, "the prompt stays open behind the picker")

	got := make([]string, 0, len(m.dirList.Items()))
	for _, it := range m.dirList.Items() {
		got = append(got, it.FilterValue())
	}
	require.Equal(t, []string{"archive", "notes", "tasks"}, got)
}

// TestDirPickerEscReturnsToThePrompt proves cancelling the picker leaves the
// typed path untouched.
func TestDirPickerEscReturnsToThePrompt(t *testing.T) {
	_, m := openMovable(t)

	m = send(t, m, keyMove)
	m.textInput.SetValue("tasks/renamed.md")
	m = send(t, m, keyFocusNext)
	require.True(t, m.pickingDir)

	m = send(t, m, keyEscape)
	require.False(t, m.pickingDir)
	require.True(t, m.movingDoc)
	require.Equal(t, "tasks/renamed.md", m.textInput.Value())
}

// TestDirPickerEnterRelocatesTheFilename proves selecting a directory rewrites
// the path while keeping the filename the user was already editing.
func TestDirPickerEnterRelocatesTheFilename(t *testing.T) {
	f, m := openMovable(t)
	f.seed(t, "archive/old.md", taskDoc)

	m = send(t, m, keyMove)
	m.textInput.SetValue("tasks/renamed.md")
	m = send(t, m, keyFocusNext)
	m = send(t, m, keyEnter)

	require.False(t, m.pickingDir)
	require.True(t, m.movingDoc, "the user lands back in the prompt")
	require.Equal(t, "archive/renamed.md", m.textInput.Value())

	m = send(t, m, keyEnter)
	require.False(t, m.statusIsErr, m.statusMsg)
	require.Equal(t, "archive/renamed.md", m.uri)
}

// TestDirPickerWithNoDirectoriesStaysInThePrompt proves a flat tree does not
// open an empty picker.
func TestDirPickerWithNoDirectoriesStaysInThePrompt(t *testing.T) {
	f := newFixtureWith(t, func(c *config.Config) { c.MCP.AllowDelete = true })
	uri := f.seed(t, "ship.md", taskDoc)
	m, err := NewEditModel(context.Background(), f.svc, uri)
	require.NoError(t, err)

	m = send(t, m, keyMove)
	m = send(t, m, keyFocusNext)

	require.False(t, m.pickingDir)
	require.True(t, m.movingDoc)
	require.Contains(t, m.statusMsg, "no directories")
}

// TestDirPickerRendersInsteadOfThePanes is a smoke test that the picker view
// composes without panicking.
func TestDirPickerRendersInsteadOfThePanes(t *testing.T) {
	f, m := openMovable(t)
	f.seed(t, "archive/old.md", taskDoc)
	m = m.resize(120, 40)

	m = send(t, m, keyMove)
	require.Contains(t, m.View(), "tasks/ship.md", "the prompt renders under the panes")

	m = send(t, m, keyFocusNext)
	out := m.View()
	require.Contains(t, out, "MOVE TO")
	require.Contains(t, out, "archive")
	require.NotContains(t, out, "METADATA")
}

// TestDirectoriesOfSkipsTheRoot proves the candidate set is deduplicated, sorted,
// and free of the tree root.
func TestDirectoriesOfSkipsTheRoot(t *testing.T) {
	dirs := directoriesOf([]browse.Entry{
		{URI: "notes/b.md"},
		{URI: "loose.md"},
		{URI: "notes/a.md"},
		{URI: "adr/sub/0001.md"},
	})

	require.Equal(t, []string{"adr/sub", "notes"}, dirs)
}
