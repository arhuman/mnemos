// Package tui is the terminal editor behind `mnemos edit`: three panes over one
// OKF document — its link-graph neighborhood, its frontmatter fields, and its
// body — with the index kept in sync on save.
//
// The panes are one flat Model rather than nested tea.Models. They are tightly
// coupled (unsaved metadata gates navigation, the $EDITOR shell-out suspends the
// whole program), so keeping the save/dirty/history logic in one Update is
// simpler than routing messages between a parent and three children.
package tui

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/arhuman/mnemos/internal/memory"
	"github.com/arhuman/mnemos/internal/search"
)

// pane identifies which of the three panes has focus.
type pane int

const (
	paneNav pane = iota
	paneMetadata
	paneContent
	paneCount
)

// navLimit caps each navigation section so a hub document does not fill the
// pane with one kind of neighbor.
const navLimit = 20

// defaultWidth and defaultHeight lay the panes out until the first
// WindowSizeMsg arrives.
const (
	defaultWidth  = 100
	defaultHeight = 30
)

// Model is the editor state for one document at a time.
type Model struct {
	ctx context.Context //nolint:containedctx // a TUI session's cancellation scope is the model's lifetime
	svc *memory.Service

	uri     string
	src     memory.EditSource
	body    string
	related memory.RelatedResult
	similar []search.SimilarResult

	navRows   []navRow
	navCursor int

	fields       []fieldRow
	fieldCursor  int
	editingField bool
	textInput    textinput.Model

	movingDoc  bool
	pickingDir bool
	dirList    list.Model

	viewport viewport.Model
	history  []string

	focus         pane
	bodyChanged   bool
	hadEmbeddings bool
	forcePending  bool
	statusMsg     string
	statusIsErr   bool
	width         int
	height        int

	// readEdited reads back the temp file $EDITOR was pointed at. It is a field
	// so the post-editor path is testable without spawning a subprocess.
	readEdited func(path string) (string, error)
}

// NewEditModel loads uri and builds the editor over it. It fails when the
// document cannot be opened, so `mnemos edit` reports a bad uri on the terminal
// it was invoked from rather than inside an alternate screen.
func NewEditModel(ctx context.Context, svc *memory.Service, uri string) (Model, error) {
	if svc == nil {
		return Model{}, fmt.Errorf("tui: edit %q: no store", uri)
	}

	input := textinput.New()
	input.Prompt = "> "

	m := Model{
		ctx:        ctx,
		svc:        svc,
		textInput:  input,
		viewport:   viewport.New(defaultWidth, defaultHeight),
		width:      defaultWidth,
		height:     defaultHeight,
		readEdited: readFile,
	}

	m, err := m.load(uri)
	if err != nil {
		return Model{}, err
	}

	return m, nil
}

// Init implements tea.Model. The document is already loaded, so there is no
// startup command.
func (Model) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m.resize(msg.Width, msg.Height), nil
	case editorFinishedMsg:
		return m.afterEditor(msg), nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

// handleKey routes a keypress: an open prompt swallows everything while it is
// up (innermost first), then the global bindings, then the focused pane's own.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.pickingDir {
		return m.updateDirPicker(msg)
	}
	if m.movingDoc {
		return m.updateMoveInput(msg)
	}
	if m.editingField {
		return m.updateFieldInput(msg)
	}

	key := msg.String()
	if !isGateKey(key) {
		m.forcePending = false
	}

	switch key {
	case keyQuit, keyQuitCtrl:
		return m.requestQuit()
	case keySave:
		return m.save(), nil
	case keyMove:
		return m.beginMove()
	case keyFocusNext:
		m.focus = (m.focus + 1) % paneCount

		return m, nil
	case keyBack, keyBackspace:
		return m.goBack()
	}

	switch m.focus {
	case paneNav:
		return m.updateNav(key)
	case paneMetadata:
		return m.updateMetadata(key)
	case paneContent:
		return m.updateContent(key)
	}

	return m, nil
}

// View implements tea.Model.
func (m Model) View() string {
	header := styleTitle.Render(m.uri)
	if m.isDirty() {
		header += styleChanged.Render("  [unsaved]")
	}

	status := styleDim.Render(m.statusMsg)
	if m.statusIsErr {
		status = styleError.Render(m.statusMsg)
	}

	if m.pickingDir {
		return strings.Join([]string{header, m.dirList.View(), status}, "\n")
	}

	panes := lipgloss.JoinHorizontal(lipgloss.Top,
		lipgloss.NewStyle().Width(m.paneWidth()).Render(m.viewNav()),
		lipgloss.NewStyle().Width(m.paneWidth()).Render(m.viewMetadata()),
		lipgloss.NewStyle().Width(m.paneWidth()).Render(m.viewContent()),
	)

	lines := []string{header, panes}
	// The move prompt is not pane-scoped, so it renders under the panes rather
	// than inside one of them.
	if m.movingDoc {
		lines = append(lines, m.textInput.View())
	}

	return strings.Join(append(lines, status, styleDim.Render(helpLine)), "\n")
}

// load reads uri from disk, refreshes every pane from it, and clears the dirty
// state. History is carried over, so callers layer their own push/pop on top.
func (m Model) load(uri string) (Model, error) {
	src, err := m.svc.OpenForEdit(m.ctx, uri)
	if err != nil {
		return m, err
	}

	// The graph and the similarity scan are best-effort: an unindexed or
	// un-embedded document still opens, with those sections simply empty.
	related, _ := m.svc.Related(m.ctx, uri, memory.DirectionBoth, navLimit)
	similar, _ := m.svc.Similar(m.ctx, uri, navLimit)

	m.uri = src.URI
	m.src = src
	m.body = src.Body
	m.related = related
	m.similar = similar
	m.hadEmbeddings = len(similar) > 0
	m.fields = buildFields(src)
	m.navRows = buildNavRows(related, similar)
	m.navCursor = firstSelectable(m.navRows)
	m.fieldCursor = 0
	m.bodyChanged = false
	m.editingField = false
	m.forcePending = false
	m.viewport.SetContent(m.body)
	m.viewport.GotoTop()

	if !src.Editable {
		m.statusMsg = "frontmatter is not editable in place; body edits only"
		m.statusIsErr = false
	}

	return m, nil
}

// isDirty reports whether the in-memory document differs from disk.
func (m Model) isDirty() bool {
	if m.bodyChanged {
		return true
	}
	for _, f := range m.fields {
		if f.changed {
			return true
		}
	}

	return false
}

// resize re-lays the panes for a new terminal size.
func (m Model) resize(width, height int) Model {
	m.width, m.height = width, height
	m.viewport.Width = m.paneWidth()
	m.viewport.Height = m.paneHeight()

	return m
}

// paneWidth is the column budget for one of the three panes.
func (m Model) paneWidth() int {
	w := m.width/int(paneCount) - 2
	if w < 20 {
		return 20
	}

	return w
}

// paneHeight is the row budget for a pane's body, leaving the header, status
// and help lines room.
func (m Model) paneHeight() int {
	h := m.height - 5
	if h < 5 {
		return 5
	}

	return h
}

// info sets a non-error status line.
func (m Model) info(format string, args ...any) Model {
	m.statusMsg = fmt.Sprintf(format, args...)
	m.statusIsErr = false

	return m
}

// fail sets an error status line.
func (m Model) fail(format string, args ...any) Model {
	m.statusMsg = fmt.Sprintf(format, args...)
	m.statusIsErr = true

	return m
}

// readFile is the production read-back of the file $EDITOR wrote.
func readFile(path string) (string, error) {
	b, err := os.ReadFile(path) //nolint:gosec // the path is a temp file this package created
	if err != nil {
		return "", fmt.Errorf("tui: read edited body: %w", err)
	}

	return string(b), nil
}

var _ tea.Model = Model{}
