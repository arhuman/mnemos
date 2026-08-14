package tui

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/arhuman/mnemos/internal/browse"
)

// dirItem is one candidate directory in the picker. The path is both the label
// and the fuzzy-filter key; the picker shows no description.
type dirItem string

func (d dirItem) Title() string       { return string(d) }
func (dirItem) Description() string   { return "" }
func (d dirItem) FilterValue() string { return string(d) }

// beginMove opens the move prompt over the current uri. The move changes which
// document is open, so it goes through the same save gate as navigation.
func (m Model) beginMove() (tea.Model, tea.Cmd) {
	gated, ok := m.saveGate()
	if !ok {
		return gated, nil
	}

	gated.textInput.SetValue(gated.uri)
	gated.textInput.CursorEnd()
	gated.textInput.Focus()
	gated.movingDoc = true

	return gated.info("move to: tab picks a directory, enter commits, esc cancels"), nil
}

// updateMoveInput drives the move prompt. Enter commits the move, tab hands over
// to the directory picker, esc leaves the document where it is.
func (m Model) updateMoveInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case keyEscape:
		return m.closeMovePrompt().info("move cancelled"), nil
	case keyEnter:
		return m.commitMove(), nil
	case keyFocusNext:
		return m.beginDirPicker()
	default:
	}

	input, cmd := m.textInput.Update(msg)
	m.textInput = input

	return m, cmd
}

// commitMove renames the document and reopens it at its new uri. The old uri is
// not pushed onto the back-stack: it no longer exists, so going back to it would
// only fail.
func (m Model) commitMove() Model {
	to := strings.TrimSpace(m.textInput.Value())
	closed := m.closeMovePrompt()

	if to == "" || to == closed.uri {
		return closed.info("move cancelled; the path is unchanged")
	}

	res, err := closed.svc.Move(closed.ctx, closed.uri, to)
	if err != nil {
		return closed.fail("move failed: %v", err)
	}

	next, lerr := closed.load(res.To)
	if lerr != nil {
		return m.fail("moved to %s, but reopening it failed: %v", res.To, lerr)
	}

	moved := next.info("moved to %s, %d file(s) reindexed", res.To, len(res.Result.Entries))
	if res.Result.DanglingLinks > 0 {
		moved.statusMsg += fmt.Sprintf("; %d inbound link(s) still point at the old path", res.Result.DanglingLinks)
	}

	return moved
}

// beginDirPicker lists the directories that already hold documents and offers
// them as move targets. With none to offer it stays in the free-text prompt
// rather than opening an empty list.
func (m Model) beginDirPicker() (tea.Model, tea.Cmd) {
	entries, err := m.svc.List(m.ctx, browse.Options{})
	if err != nil {
		return m.fail("cannot list directories: %v", err), nil
	}

	dirs := directoriesOf(entries)
	if len(dirs) == 0 {
		return m.info("no directories to pick from; type a path"), nil
	}

	items := make([]list.Item, 0, len(dirs))
	for _, d := range dirs {
		items = append(items, dirItem(d))
	}

	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = false

	picker := list.New(items, delegate, m.width, m.paneHeight())
	picker.Title = "MOVE TO"
	picker.SetShowStatusBar(false)
	picker.SetShowHelp(false)

	opened := m
	opened.dirList = picker
	opened.pickingDir = true

	return opened.info("pick a directory: / filters, enter selects, esc goes back"), nil
}

// closeMovePrompt takes the move prompt down, leaving the document untouched.
func (m Model) closeMovePrompt() Model {
	m.movingDoc = false
	m.textInput.Blur()

	return m
}

// updateDirPicker drives the directory picker. While its filter is being typed
// the list keeps enter and esc for itself, so the picker's own bindings only
// apply outside that state.
func (m Model) updateDirPicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.dirList.FilterState() != list.Filtering {
		switch msg.String() {
		case keyEscape:
			m.pickingDir = false

			return m.info("move to: enter commits, esc cancels"), nil
		case keyEnter:
			return m.applyPickedDir(), nil
		default:
		}
	}

	picker, cmd := m.dirList.Update(msg)
	m.dirList = picker

	return m, cmd
}

// applyPickedDir relocates the filename currently in the prompt under the picked
// directory, leaving the user back in the prompt to hand-edit it.
func (m Model) applyPickedDir() Model {
	picked := m
	picked.pickingDir = false

	dir, ok := picked.dirList.SelectedItem().(dirItem)
	if !ok {
		return picked.info("nothing selected; move to: enter commits, esc cancels")
	}

	picked.textInput.SetValue(path.Join(string(dir), path.Base(picked.textInput.Value())))
	picked.textInput.CursorEnd()

	return picked.info("move to: enter commits, esc cancels")
}

// directoriesOf returns the sorted, distinct directories holding the listed
// entries. The tree root is left out: a document lands there by typing a bare
// filename, so it needs no entry of its own.
func directoriesOf(entries []browse.Entry) []string {
	seen := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		if dir := path.Dir(e.URI); dir != "." {
			seen[dir] = struct{}{}
		}
	}

	dirs := make([]string, 0, len(seen))
	for d := range seen {
		dirs = append(dirs, d)
	}
	slices.Sort(dirs)

	return dirs
}
