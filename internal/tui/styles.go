package tui

import "github.com/charmbracelet/lipgloss"

// The palette sticks to the terminal's own ANSI colours so the editor inherits
// whatever theme the user already runs.
var (
	styleTitle     = lipgloss.NewStyle().Bold(true)
	styleFocused   = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	styleDim       = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleSelected  = lipgloss.NewStyle().Foreground(lipgloss.Color("4")).Bold(true)
	styleChanged   = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	styleError     = lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true)
	styleSectionUp = lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Bold(true)
)

// paneTitle renders a pane heading, highlighted when the pane has focus.
func paneTitle(label string, focused bool) string {
	if focused {
		return styleFocused.Render("▸ " + label)
	}

	return styleTitle.Render("  " + label)
}
