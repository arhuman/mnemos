package tui

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/chunk"
	"github.com/arhuman/mnemos/internal/config"
	"github.com/arhuman/mnemos/internal/ingest"
	"github.com/arhuman/mnemos/internal/memory"
	"github.com/arhuman/mnemos/internal/okfschema"
	"github.com/arhuman/mnemos/internal/testutil"
)

const taskDoc = `---
type: task
title: Ship the thing
status: todo               # backlog | todo | in_progress | done | cancelled
priority: high
tags: [auth, bug]
---

## Goal

Ship it.
`

type fixture struct {
	svc      *memory.Service
	db       *sql.DB
	treeRoot string
}

func newFixture(t *testing.T) fixture {
	t.Helper()

	return newFixtureWith(t, nil)
}

// newFixtureWith builds a fixture whose config tweak runs after the defaults, so
// a test can open gates (allow_delete) the plain fixture leaves shut.
func newFixtureWith(t *testing.T, tweak func(*config.Config)) fixture {
	t.Helper()

	cfg, err := config.Load("", func(string) bool { return false })
	require.NoError(t, err)
	cfg.MCP.AllowWrite = true
	if tweak != nil {
		tweak(cfg)
	}

	db := testutil.NewDB(t)
	treeRoot := t.TempDir()
	testutil.Chdir(t, treeRoot)

	return fixture{
		svc:      memory.New(db, cfg, treeRoot, nil, testutil.DiscardLogger()),
		db:       db,
		treeRoot: treeRoot,
	}
}

func (f fixture) seed(t *testing.T, rel, content string) string {
	t.Helper()
	abs := testutil.WriteFile(t, f.treeRoot, rel, content)
	uri := filepath.ToSlash(rel)
	_, _, err := ingest.File(context.Background(), f.db, testutil.DiscardLogger(), abs, uri, "proj",
		chunk.Config{TargetTokens: 700, OverlapTokens: 80})
	require.NoError(t, err)

	return uri
}

// key builds the KeyMsg whose String() is s, covering the bindings the editor
// uses.
func key(s string) tea.KeyMsg {
	switch s {
	case keyEnter:
		return tea.KeyMsg{Type: tea.KeyEnter}
	case keyEscape:
		return tea.KeyMsg{Type: tea.KeyEsc}
	case keyFocusNext:
		return tea.KeyMsg{Type: tea.KeyTab}
	case keyLeft:
		return tea.KeyMsg{Type: tea.KeyLeft}
	case keyRight:
		return tea.KeyMsg{Type: tea.KeyRight}
	case keyUp:
		return tea.KeyMsg{Type: tea.KeyUp}
	case keyDown:
		return tea.KeyMsg{Type: tea.KeyDown}
	}

	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// send delivers a keypress and returns the resulting model.
func send(t *testing.T, m Model, k string) Model {
	t.Helper()
	next, _ := m.Update(key(k))
	got, ok := next.(Model)
	require.True(t, ok)

	return got
}

// sendCmd delivers a keypress and returns the model and the command it issued.
func sendCmd(t *testing.T, m Model, k string) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(key(k))
	got, ok := next.(Model)
	require.True(t, ok)

	return got, cmd
}

func openTask(t *testing.T) (fixture, Model) {
	t.Helper()
	f := newFixture(t)
	uri := f.seed(t, "tasks/ship.md", taskDoc)
	m, err := NewEditModel(context.Background(), f.svc, uri)
	require.NoError(t, err)

	return f, m
}

// TestNewEditModelLoadsDocument proves construction populates every pane from
// the file on disk.
func TestNewEditModelLoadsDocument(t *testing.T) {
	_, m := openTask(t)

	require.Equal(t, "tasks/ship.md", m.uri)
	require.True(t, m.src.Editable)
	require.Equal(t, "\n## Goal\n\nShip it.\n", m.body)
	require.False(t, m.isDirty())

	keys := make([]string, len(m.fields))
	for i, f := range m.fields {
		keys[i] = f.Key
	}
	require.Equal(t, []string{"type", "title", "status", "priority", "tags"}, keys)
	require.Equal(t, okfschema.KindEnum, m.fields[2].Schema.Kind, "status resolves through the task schema")
	require.Equal(t, okfschema.KindReadOnly, m.fields[0].Schema.Kind, "type is read-only")
}

// TestNewEditModelRejectsUnknownURI proves a bad uri fails at construction,
// before any program attaches to a terminal.
func TestNewEditModelRejectsUnknownURI(t *testing.T) {
	f := newFixture(t)

	_, err := NewEditModel(context.Background(), f.svc, "tasks/nope.md")
	require.Error(t, err)

	_, err = NewEditModel(context.Background(), nil, "tasks/nope.md")
	require.ErrorContains(t, err, "no store")
}

// TestFocusCycling proves tab walks the three panes and wraps.
func TestFocusCycling(t *testing.T) {
	_, m := openTask(t)
	require.Equal(t, paneNav, m.focus)

	m = send(t, m, keyFocusNext)
	require.Equal(t, paneMetadata, m.focus)
	m = send(t, m, keyFocusNext)
	require.Equal(t, paneContent, m.focus)
	m = send(t, m, keyFocusNext)
	require.Equal(t, paneNav, m.focus)
}

// TestMetadataEnumCycling proves arrows step an enum in both directions without
// touching disk, and that a non-enum field says so instead.
func TestMetadataEnumCycling(t *testing.T) {
	f, m := openTask(t)
	m.focus = paneMetadata
	m.fieldCursor = 2 // status

	m = send(t, m, keyRight)
	require.Equal(t, "in_progress", m.fields[2].Value)
	require.True(t, m.fields[2].changed)
	require.True(t, m.isDirty())

	m = send(t, m, keyLeft)
	require.Equal(t, "todo", m.fields[2].Value)

	require.Equal(t, taskDoc, readFileT(t, f.treeRoot, m.uri), "cycling never writes to disk")

	m.fieldCursor = 1 // title
	m = send(t, m, keyRight)
	require.Equal(t, "Ship the thing", m.fields[1].Value)
	require.Contains(t, m.statusMsg, "not an enum")
}

// TestMetadataTextEditCommitAndCancel proves the text input commits into the
// in-memory row on enter and discards on esc.
func TestMetadataTextEditCommitAndCancel(t *testing.T) {
	_, m := openTask(t)
	m.focus = paneMetadata
	m.fieldCursor = 1 // title

	m = send(t, m, keyEnter)
	require.True(t, m.editingField)
	m.textInput.SetValue("Ship something else")
	m = send(t, m, keyEnter)
	require.False(t, m.editingField)
	require.Equal(t, "Ship something else", m.fields[1].Value)
	require.True(t, m.fields[1].changed)

	m.fieldCursor = 4 // tags
	m = send(t, m, keyEnter)
	require.True(t, m.editingField)
	m.textInput.SetValue("auth, cookie, fix")
	m = send(t, m, keyEscape)
	require.False(t, m.editingField)
	require.Equal(t, []string{"auth", "bug"}, m.fields[4].Items, "esc discards the edit")
	require.False(t, m.fields[4].changed)
}

// TestMetadataTagsCommitSplitsItems proves a committed tag list is parsed back
// into items for the save.
func TestMetadataTagsCommitSplitsItems(t *testing.T) {
	_, m := openTask(t)
	m.focus = paneMetadata
	m.fieldCursor = 4 // tags

	m = send(t, m, keyEnter)
	m.textInput.SetValue("auth, cookie ,  fix ")
	m = send(t, m, keyEnter)

	require.Equal(t, []string{"auth", "cookie", "fix"}, m.fields[4].Items)
	edits := m.pendingEdits()
	require.Len(t, edits, 1)
	require.True(t, edits[0].List)
	require.Equal(t, []string{"auth", "cookie", "fix"}, edits[0].Items)
}

// TestMetadataReadOnlyFieldRefuses proves an index-owned field cannot be typed
// over.
func TestMetadataReadOnlyFieldRefuses(t *testing.T) {
	_, m := openTask(t)
	m.focus = paneMetadata
	m.fieldCursor = 0 // type

	m = send(t, m, keyEnter)
	require.False(t, m.editingField)
	require.Contains(t, m.statusMsg, "cannot be edited")
	require.False(t, m.isDirty())
}

// TestSaveWritesAndReindexes proves an explicit save patches the file in place
// and clears the dirty state.
func TestSaveWritesAndReindexes(t *testing.T) {
	f, m := openTask(t)
	m.focus = paneMetadata
	m.fieldCursor = 2

	m = send(t, m, keyRight)
	m = send(t, m, keySave)

	require.False(t, m.statusIsErr, m.statusMsg)
	require.Contains(t, m.statusMsg, "saved")
	require.False(t, m.isDirty())

	onDisk := readFileT(t, f.treeRoot, m.uri)
	// Only the value changes: the padding that trailed "todo" carries over as-is.
	require.Contains(t, onDisk, "status: in_progress               # backlog | todo | in_progress | done | cancelled")
	require.Contains(t, onDisk, "\n## Goal\n\nShip it.\n")
	require.Equal(t, "in_progress", m.fields[2].Value, "the reload picks the new value back up")
}

// TestSaveWithNothingPendingIsANoop proves a clean document is not rewritten.
func TestSaveWithNothingPendingIsANoop(t *testing.T) {
	_, m := openTask(t)

	m = send(t, m, keySave)
	require.Contains(t, m.statusMsg, "nothing to save")
	require.False(t, m.statusIsErr)
}

// TestNavJumpPushesAndPopsHistory proves following a row and going back walk the
// back-stack.
func TestNavJumpPushesAndPopsHistory(t *testing.T) {
	f, m := openTask(t)
	other := f.seed(t, "tasks/other.md", strings.Replace(taskDoc, "Ship the thing", "Other thing", 1))

	m.navRows = []navRow{{Label: "LINKS OUT", Head: true}, {URI: other, Label: "  Other thing"}}
	m.navCursor = 1

	m = send(t, m, keyEnter)
	require.Equal(t, other, m.uri)
	require.Equal(t, []string{"tasks/ship.md"}, m.history)

	m = send(t, m, keyBack)
	require.Equal(t, "tasks/ship.md", m.uri)
	require.Empty(t, m.history)

	m = send(t, m, keyBack)
	require.Contains(t, m.statusMsg, "no previous document")
}

// TestNavJumpSavesDirtyDocumentFirst proves navigating away is not a way to lose
// an unsaved edit.
func TestNavJumpSavesDirtyDocumentFirst(t *testing.T) {
	f, m := openTask(t)
	other := f.seed(t, "tasks/other.md", strings.Replace(taskDoc, "Ship the thing", "Other thing", 1))

	m.focus = paneMetadata
	m.fieldCursor = 2
	m = send(t, m, keyRight)
	require.True(t, m.isDirty())

	m.focus = paneNav
	m.navRows = []navRow{{URI: other, Label: "  Other thing"}}
	m.navCursor = 0
	m = send(t, m, keyEnter)

	require.Equal(t, other, m.uri)
	require.Contains(t, readFileT(t, f.treeRoot, "tasks/ship.md"), "status: in_progress")
}

// TestNavCursorSkipsUnfollowableRows proves headings and dangling links are
// stepped over rather than selected.
func TestNavCursorSkipsUnfollowableRows(t *testing.T) {
	_, m := openTask(t)
	m.navRows = []navRow{
		{Label: "LINKS OUT", Head: true},
		{Label: "  gone.md (dangling)"},
		{URI: "a.md", Label: "  a"},
		{Label: "BACKLINKS", Head: true},
		{URI: "b.md", Label: "  b"},
	}
	m.navCursor = firstSelectable(m.navRows)
	require.Equal(t, 2, m.navCursor)

	m = send(t, m, keyDown)
	require.Equal(t, 4, m.navCursor)
	m = send(t, m, keyDown)
	require.Equal(t, 4, m.navCursor, "the cursor stays put at the end")
	m = send(t, m, keyUp)
	require.Equal(t, 2, m.navCursor)
}

// TestSaveGateBlocksNavigationOnFailure proves a failed reindex leaves the
// document dirty and blocks the jump once, and that repeating the key pushes
// past a store that stays broken.
func TestSaveGateBlocksNavigationOnFailure(t *testing.T) {
	f, m := openTask(t)
	other := f.seed(t, "tasks/other.md", strings.Replace(taskDoc, "Ship the thing", "Other thing", 1))

	m.focus = paneMetadata
	m.fieldCursor = 2
	m = send(t, m, keyRight)

	require.NoError(t, f.db.Close())

	m.focus = paneNav
	m.navRows = []navRow{{URI: other, Label: "  Other thing"}}
	m.navCursor = 0

	m = send(t, m, keyEnter)
	require.Equal(t, "tasks/ship.md", m.uri, "navigation is blocked on the first attempt")
	require.True(t, m.statusIsErr)
	require.True(t, m.forcePending)
	require.True(t, m.isDirty(), "the pending edit is still pending")
	require.Contains(t, readFileT(t, f.treeRoot, "tasks/ship.md"), "status: in_progress",
		"the write landed even though the reindex did not")

	// The second press pushes past a store that is not coming back; the load then
	// fails on the same closed store, but the block itself is released.
	m = send(t, m, keyEnter)
	require.False(t, m.forcePending)
}

// TestQuitIsBlockedByAFailedSave proves quitting goes through the same gate as
// navigation.
func TestQuitIsBlockedByAFailedSave(t *testing.T) {
	f, m := openTask(t)
	m.focus = paneMetadata
	m.fieldCursor = 2
	m = send(t, m, keyRight)
	require.NoError(t, f.db.Close())

	m, cmd := sendCmd(t, m, keyQuit)
	require.Nil(t, cmd, "the first quit is held back")
	require.True(t, m.forcePending)

	_, cmd = sendCmd(t, m, keyQuit)
	require.NotNil(t, cmd, "the second quit goes through")
}

// TestQuitOnACleanDocumentQuits proves the gate is transparent when nothing is
// pending.
func TestQuitOnACleanDocumentQuits(t *testing.T) {
	_, m := openTask(t)

	_, cmd := sendCmd(t, m, keyQuit)
	require.NotNil(t, cmd)
}

// TestEditorRoundTrip drives the post-$EDITOR path through the injected
// read-back, covering a changed body, an unchanged one, and a failed editor.
func TestEditorRoundTrip(t *testing.T) {
	_, m := openTask(t)
	m.readEdited = func(string) (string, error) { return "\n## Goal\n\nShipped.\n", nil }

	m = m.afterEditor(editorFinishedMsg{path: filepath.Join(t.TempDir(), "gone.md")})
	require.True(t, m.bodyChanged)
	require.True(t, m.isDirty())
	require.Equal(t, "\n## Goal\n\nShipped.\n", m.body)

	same := m
	same.readEdited = func(string) (string, error) { return same.body, nil }
	same = same.afterEditor(editorFinishedMsg{path: "irrelevant"})
	require.Contains(t, same.statusMsg, "unchanged")

	failed := m.afterEditor(editorFinishedMsg{path: "irrelevant", err: context.Canceled})
	require.True(t, failed.statusIsErr)
}

// TestEditorRoundTripSaves proves a body edited through $EDITOR reaches disk
// with the frontmatter carried over untouched.
func TestEditorRoundTripSaves(t *testing.T) {
	f, m := openTask(t)
	m.readEdited = func(string) (string, error) { return "\n## Goal\n\nShipped.\n", nil }
	m = m.afterEditor(editorFinishedMsg{path: "irrelevant"})

	m = send(t, m, keySave)
	require.False(t, m.statusIsErr, m.statusMsg)

	onDisk := readFileT(t, f.treeRoot, m.uri)
	require.Contains(t, onDisk, "status: todo               # backlog")
	require.Contains(t, onDisk, "Shipped.")
	require.NotContains(t, onDisk, "Ship it.")
}

// TestReadOnlyDocumentRefusesFieldEdits proves a document whose frontmatter
// cannot be patched opens with body editing only.
func TestReadOnlyDocumentRefusesFieldEdits(t *testing.T) {
	f := newFixture(t)
	uri := f.seed(t, "docs/plain.md", "# Plain\n\nBody.\n")

	m, err := NewEditModel(context.Background(), f.svc, uri)
	require.NoError(t, err)
	require.False(t, m.src.Editable)
	require.Empty(t, m.fields)
	require.Contains(t, m.statusMsg, "not editable")

	m.focus = paneMetadata
	m = send(t, m, keyEnter)
	require.False(t, m.editingField)
}

// TestViewRendersEveryPane is a smoke test that the three panes and the status
// line render without panicking.
func TestViewRendersEveryPane(t *testing.T) {
	_, m := openTask(t)
	m = m.resize(120, 40)

	out := m.View()
	for _, want := range []string{"NAV", "METADATA", "CONTENT", "tasks/ship.md", "status"} {
		require.Contains(t, out, want)
	}
}

// TestBuildNavRowsSections proves the three sections render, with the hints for
// an empty direction and an un-embedded corpus.
func TestBuildNavRowsSections(t *testing.T) {
	rows := buildNavRows(memory.RelatedResult{
		Outbound: []memory.RelatedNeighbor{
			{URI: "a.md", Title: "A", Resolved: true},
			{URI: "gone.md", Resolved: false},
		},
	}, nil)

	labels := make([]string, 0, len(rows))
	for _, r := range rows {
		labels = append(labels, strings.TrimSpace(r.Label))
	}
	require.Contains(t, labels, "LINKS OUT")
	require.Contains(t, labels, "BACKLINKS")
	require.Contains(t, labels, "SIMILAR")
	require.Contains(t, labels, "(none)")
	require.Contains(t, labels, "gone.md (dangling)")
	require.Contains(t, labels, "(no embeddings; run mnemos reindex --embeddings)")

	require.Equal(t, "a.md", rows[firstSelectable(rows)].URI)
}

func readFileT(t *testing.T, treeRoot, rel string) string {
	t.Helper()
	b, err := readFile(filepath.Join(treeRoot, filepath.FromSlash(rel)))
	require.NoError(t, err)

	return b
}
