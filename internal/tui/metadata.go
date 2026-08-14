package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/arhuman/mnemos/internal/memory"
	"github.com/arhuman/mnemos/internal/okfschema"
)

// fieldRow is one frontmatter key as the metadata pane holds it: the value the
// user has edited so far, and whether it still matches disk. Nothing here
// reaches the file until a save.
type fieldRow struct {
	Key     string
	Value   string
	Items   []string
	Schema  okfschema.FieldSchema
	changed bool
}

// buildFields turns a loaded document's frontmatter into editable rows.
func buildFields(src memory.EditSource) []fieldRow {
	rows := make([]fieldRow, 0, len(src.Fields))
	for _, f := range src.Fields {
		row := fieldRow{Key: f.Key, Value: f.Value, Schema: okfschema.FieldSchemaFor(src.Type, f.Key)}
		if f.IsList {
			row.Items = splitItems(f.Value)
		}
		rows = append(rows, row)
	}

	return rows
}

// splitItems parses the comma-separated rendering of a list field back into
// its items.
func splitItems(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	items := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			items = append(items, trimmed)
		}
	}

	return items
}

// pendingEdits returns the field patches a save must apply.
func (m Model) pendingEdits() []memory.FieldEdit {
	edits := make([]memory.FieldEdit, 0, len(m.fields))
	for _, f := range m.fields {
		if !f.changed {
			continue
		}
		edits = append(edits, memory.FieldEdit{
			Key:   f.Key,
			Value: f.Value,
			Items: f.Items,
			List:  f.Schema.Kind == okfschema.KindTags,
		})
	}

	return edits
}

// updateMetadata handles the metadata pane's keys.
func (m Model) updateMetadata(key string) (tea.Model, tea.Cmd) {
	if len(m.fields) == 0 {
		return m, nil
	}

	switch key {
	case keyUp, keyUpVim:
		m.fieldCursor = clamp(m.fieldCursor-1, len(m.fields))
	case keyDown, keyDownVim:
		m.fieldCursor = clamp(m.fieldCursor+1, len(m.fields))
	case keyLeft, keyLeftVim:
		return m.cycle(-1), nil
	case keyRight, keyRightVim:
		return m.cycle(1), nil
	case keyEnter:
		return m.beginFieldEdit()
	default:
	}

	return m, nil
}

// cycle steps an enum field to its neighbouring value, marking it changed
// without writing anything to disk.
func (m Model) cycle(delta int) Model {
	f := m.fields[m.fieldCursor]
	if !m.src.Editable {
		return m.fail("frontmatter is not editable in place")
	}
	if f.Schema.Kind != okfschema.KindEnum {
		return m.info("%s is not an enum; press enter to edit", f.Key)
	}

	next := f.Schema.Cycle(f.Value, delta)
	m.fields[m.fieldCursor].Value = next
	m.fields[m.fieldCursor].changed = true

	return m.info("%s = %s (unsaved)", f.Key, next)
}

// beginFieldEdit opens the text input over the selected field, or cycles it
// when the field is an enum.
func (m Model) beginFieldEdit() (tea.Model, tea.Cmd) {
	f := m.fields[m.fieldCursor]
	if !m.src.Editable {
		return m.fail("frontmatter is not editable in place"), nil
	}

	switch f.Schema.Kind {
	case okfschema.KindReadOnly:
		return m.info("%s is managed by the index and cannot be edited", f.Key), nil
	case okfschema.KindEnum:
		return m.cycle(1), nil
	case okfschema.KindTags:
		m.textInput.SetValue(strings.Join(f.Items, ", "))
	default:
		m.textInput.SetValue(f.Value)
	}

	m.textInput.CursorEnd()
	m.textInput.Focus()
	m.editingField = true
	m = m.info("editing %s; enter commits, esc cancels", f.Key)

	return m, nil
}

// updateFieldInput drives the open text input. Enter commits the edit into the
// in-memory row; esc discards it.
func (m Model) updateFieldInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case keyEscape:
		m.editingField = false
		m.textInput.Blur()
		m = m.info("cancelled")

		return m, nil
	case keyEnter:
		return m.commitFieldEdit(), nil
	default:
	}

	input, cmd := m.textInput.Update(msg)
	m.textInput = input

	return m, cmd
}

// commitFieldEdit copies the text input into the selected row.
func (m Model) commitFieldEdit() Model {
	value := m.textInput.Value()
	f := &m.fields[m.fieldCursor]

	if f.Schema.Kind == okfschema.KindTags {
		f.Items = splitItems(value)
		f.Value = strings.Join(f.Items, ", ")
	} else {
		f.Value = value
	}
	f.changed = true

	m.editingField = false
	m.textInput.Blur()
	m = m.info("%s = %s (unsaved)", f.Key, f.Value)

	return m
}

// clamp keeps a cursor inside [0, n).
func clamp(i, n int) int {
	switch {
	case n == 0:
		return 0
	case i < 0:
		return 0
	case i >= n:
		return n - 1
	}

	return i
}

// viewMetadata renders the metadata pane.
func (m Model) viewMetadata() string {
	lines := make([]string, 0, len(m.fields)+2)
	lines = append(lines, paneTitle("METADATA", m.focus == paneMetadata))

	if len(m.fields) == 0 {
		lines = append(lines, styleDim.Render("  (no editable frontmatter)"))

		return strings.Join(lines, "\n")
	}

	for i, f := range m.fields {
		lines = append(lines, m.viewField(i, f))
	}
	if m.editingField {
		lines = append(lines, m.textInput.View())
	}

	return strings.Join(lines, "\n")
}

// viewField renders one metadata row, marking the cursor and unsaved edits.
func (m Model) viewField(i int, f fieldRow) string {
	text := fmt.Sprintf("%-10s %s", f.Key+":", f.Value)
	if f.Schema.Kind == okfschema.KindReadOnly {
		text = styleDim.Render(text)
	}
	if f.changed {
		text = styleChanged.Render(text + " *")
	}
	if i == m.fieldCursor && m.focus == paneMetadata {
		return styleSelected.Render("›") + text
	}

	return " " + text
}
