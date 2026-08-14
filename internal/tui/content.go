package tui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// defaultEditor is used when $EDITOR (or $VISUAL) is unset.
const defaultEditor = "vi"

// editorFinishedMsg carries the outcome of the $EDITOR shell-out back into
// Update once bubbletea has restored the terminal.
type editorFinishedMsg struct {
	path string
	err  error
}

// updateContent handles the content pane's keys: scrolling, and handing the body
// to $EDITOR.
func (m Model) updateContent(key string) (tea.Model, tea.Cmd) {
	switch key {
	case keyEdit:
		return m.openEditor()
	case keyUp, keyUpVim:
		m.viewport.ScrollUp(1)
	case keyDown, keyDownVim:
		m.viewport.ScrollDown(1)
	case keyPageUp:
		m.viewport.PageUp()
	case keyPageDown:
		m.viewport.PageDown()
	default:
	}

	return m, nil
}

// openEditor writes the current body to a temp file and suspends the program
// while $EDITOR runs over it.
func (m Model) openEditor() (tea.Model, tea.Cmd) {
	path, err := writeTempBody(m.body)
	if err != nil {
		return m.fail("%v", err), nil
	}

	cmd := exec.CommandContext(m.ctx, editorCommand(), path) //nolint:gosec // the command is the user's own $EDITOR

	return m, tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editorFinishedMsg{path: path, err: err}
	})
}

// afterEditor reads the edited body back and marks it dirty when it changed.
func (m Model) afterEditor(msg editorFinishedMsg) Model {
	defer func() { _ = os.Remove(msg.path) }()

	if msg.err != nil {
		return m.fail("editor exited with an error: %v", msg.err)
	}

	edited, err := m.readEdited(msg.path)
	if err != nil {
		return m.fail("%v", err)
	}
	if edited == m.body {
		return m.info("body unchanged")
	}

	m.body = edited
	m.bodyChanged = true
	m.viewport.SetContent(edited)
	m = m.info("body edited (unsaved)")

	return m
}

// writeTempBody stages the body in a temp file for $EDITOR. The .md suffix is
// what gives the editor its syntax highlighting.
func writeTempBody(body string) (string, error) {
	f, err := os.CreateTemp("", "mnemos-edit-*.md")
	if err != nil {
		return "", fmt.Errorf("tui: create temp body: %w", err)
	}
	path := f.Name()

	if _, err = f.WriteString(body); err != nil {
		_ = f.Close()
		_ = os.Remove(path)

		return "", fmt.Errorf("tui: write temp body: %w", err)
	}
	if err = f.Close(); err != nil {
		_ = os.Remove(path)

		return "", fmt.Errorf("tui: close temp body: %w", err)
	}

	return path, nil
}

// editorCommand resolves the editor to shell out to.
func editorCommand() string {
	for _, env := range []string{"EDITOR", "VISUAL"} {
		if v := strings.TrimSpace(os.Getenv(env)); v != "" {
			return v
		}
	}

	return defaultEditor
}

// viewContent renders the content pane.
func (m Model) viewContent() string {
	title := paneTitle("CONTENT", m.focus == paneContent)
	if m.bodyChanged {
		title += styleChanged.Render("  *")
	}

	return title + "\n" + m.viewport.View()
}
