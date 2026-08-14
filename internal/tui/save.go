package tui

import (
	"errors"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/arhuman/mnemos/internal/memory"
)

// save writes the pending edits and reindexes the document. The write always
// happens first (inside the memory verb), so a failure here costs a stale index,
// never the user's work.
func (m Model) save() Model {
	if !m.isDirty() {
		return m.info("nothing to save")
	}

	in := memory.EditFrontmatterInput{URI: m.uri, Fields: m.pendingEdits()}
	if m.bodyChanged {
		body := m.body
		in.Body = &body
	}

	res, err := m.svc.EditFrontmatter(m.ctx, in)
	if err != nil {
		if errors.Is(err, memory.ErrReindexAfterWrite) {
			return m.fail("saved to disk but the index is stale: %v", err)
		}

		return m.fail("save failed, nothing written: %v", err)
	}

	// Reload so the panes show the bytes that actually landed, and the dirty
	// flags clear against them.
	reloaded, lerr := m.load(m.uri)
	if lerr != nil {
		return m.fail("saved, but reloading failed: %v", lerr)
	}

	saved := reloaded.info("saved, %d chunks", res.Chunks)
	if m.hadEmbeddings {
		saved.statusMsg += "; embeddings stale, run mnemos reindex --embeddings"
	}

	return saved
}

// saveGate saves before leaving the current document. ok is false when the save
// failed and the user has not yet confirmed leaving anyway: the file is already
// safe on disk, so the block exists to stop the document quietly falling out of
// search, not to prevent data loss. Repeating the same key pushes past it.
func (m Model) saveGate() (Model, bool) {
	if !m.isDirty() {
		return m, true
	}
	if m.forcePending {
		m.forcePending = false

		return m, true
	}

	saved := m.save()
	if !saved.statusIsErr {
		return saved, true
	}

	saved.forcePending = true
	saved.statusMsg += "; press the same key again to continue anyway"

	return saved, false
}

// requestQuit leaves the editor, saving a dirty document through the same gate
// navigation uses.
func (m Model) requestQuit() (tea.Model, tea.Cmd) {
	gated, ok := m.saveGate()
	if !ok {
		return gated, nil
	}

	return gated, tea.Quit
}
