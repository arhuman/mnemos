package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/arhuman/mnemos/internal/memory"
	"github.com/arhuman/mnemos/internal/search"
)

// navRow is one line of the navigation pane. A row with an empty URI is a
// section heading or a hint: it renders but cannot be selected.
type navRow struct {
	URI   string
	Label string
	Head  bool
}

// selectable reports whether Enter can follow this row.
func (r navRow) selectable() bool { return r.URI != "" }

// buildNavRows lays out the three navigation sections: outbound links, inbound
// backlinks, and semantically similar documents.
func buildNavRows(related memory.RelatedResult, similar []search.SimilarResult) []navRow {
	rows := make([]navRow, 0, len(related.Outbound)+len(related.Inbound)+len(similar)+3)

	rows = append(rows, navRow{Label: "LINKS OUT", Head: true})
	rows = appendNeighbors(rows, related.Outbound)

	rows = append(rows, navRow{Label: "BACKLINKS", Head: true})
	rows = appendNeighbors(rows, related.Inbound)

	rows = append(rows, navRow{Label: "SIMILAR", Head: true})
	if len(similar) == 0 {
		return append(rows, navRow{Label: "  (no embeddings; run mnemos reindex --embeddings)"})
	}
	for _, s := range similar {
		rows = append(rows, navRow{URI: s.URI, Label: fmt.Sprintf("  %.2f %s", s.Score, label(s.URI, s.Title))})
	}

	return rows
}

// appendNeighbors renders one direction's neighbors, marking a dangling link
// (an outbound target that is not an indexed document) as unfollowable.
func appendNeighbors(rows []navRow, neighbors []memory.RelatedNeighbor) []navRow {
	if len(neighbors) == 0 {
		return append(rows, navRow{Label: "  (none)"})
	}
	for _, n := range neighbors {
		if !n.Resolved {
			rows = append(rows, navRow{Label: "  " + n.URI + " (dangling)"})

			continue
		}
		rows = append(rows, navRow{URI: n.URI, Label: "  " + label(n.URI, n.Title)})
	}

	return rows
}

// label prefers a document's title, falling back on its uri.
func label(uri, title string) string {
	if strings.TrimSpace(title) == "" {
		return uri
	}

	return title
}

// firstSelectable returns the index of the first followable row, or 0.
func firstSelectable(rows []navRow) int {
	for i, r := range rows {
		if r.selectable() {
			return i
		}
	}

	return 0
}

// updateNav handles the navigation pane's keys.
func (m Model) updateNav(key string) (tea.Model, tea.Cmd) {
	switch key {
	case keyUp, keyUpVim:
		m.navCursor = step(m.navRows, m.navCursor, -1)
	case keyDown, keyDownVim:
		m.navCursor = step(m.navRows, m.navCursor, 1)
	case keyEnter:
		return m.jump()
	default:
	}

	return m, nil
}

// step moves the cursor to the next selectable row in direction delta, staying
// put when there is none.
func step(rows []navRow, cursor, delta int) int {
	for i := cursor + delta; i >= 0 && i < len(rows); i += delta {
		if rows[i].selectable() {
			return i
		}
	}

	return cursor
}

// jump follows the selected row, saving a dirty document first and pushing the
// current uri onto the back-stack.
func (m Model) jump() (tea.Model, tea.Cmd) {
	if m.navCursor >= len(m.navRows) || !m.navRows[m.navCursor].selectable() {
		return m.info("nothing to follow here"), nil
	}

	gated, ok := m.saveGate()
	if !ok {
		return gated, nil
	}

	from := gated.uri
	next, err := gated.load(m.navRows[m.navCursor].URI)
	if err != nil {
		return gated.fail("%v", err), nil
	}
	next.history = append(next.history, from)

	return next, nil
}

// goBack pops the back-stack, saving a dirty document first.
func (m Model) goBack() (tea.Model, tea.Cmd) {
	if len(m.history) == 0 {
		return m.info("no previous document"), nil
	}

	gated, ok := m.saveGate()
	if !ok {
		return gated, nil
	}

	back := gated.history[len(gated.history)-1]
	next, err := gated.load(back)
	if err != nil {
		return gated.fail("%v", err), nil
	}
	next.history = gated.history[:len(gated.history)-1]

	return next, nil
}

// viewNav renders the navigation pane.
func (m Model) viewNav() string {
	lines := make([]string, 0, len(m.navRows)+1)
	lines = append(lines, paneTitle("NAV", m.focus == paneNav))

	for i, r := range m.navRows {
		switch {
		case r.Head:
			lines = append(lines, styleSectionUp.Render(r.Label))
		case !r.selectable():
			lines = append(lines, styleDim.Render(r.Label))
		case i == m.navCursor && m.focus == paneNav:
			lines = append(lines, styleSelected.Render("›"+r.Label))
		default:
			lines = append(lines, " "+r.Label)
		}
	}

	return strings.Join(lines, "\n")
}
