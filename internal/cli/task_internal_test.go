package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGroupTasksOrdering(t *testing.T) {
	items := []taskItem{
		{URI: "b.md", Status: "done"},
		{URI: "a.md", Status: "done"},
		{URI: "c.md", Status: "in_progress"},
		{URI: "d.md", Status: "todo"},
		{URI: "e.md", Status: "blocked"},
		{URI: "f.md", Status: ""},
		{URI: "g.md", Status: "archived"},
	}

	groups := groupTasks(items)

	// Fixed order: in_progress, todo, done, then other statuses alphabetically
	// (archived, blocked), then the no-status bucket last.
	gotStatuses := make([]string, len(groups))
	for i, g := range groups {
		gotStatuses[i] = g.Status
	}
	require.Equal(t,
		[]string{"in_progress", "todo", "done", "archived", "blocked", noStatusLabel},
		gotStatuses,
	)

	// Within the done group, items are ordered by uri.
	var done taskGroup
	for _, g := range groups {
		if g.Status == "done" {
			done = g
		}
	}
	require.Equal(t, "a.md", done.Items[0].URI)
	require.Equal(t, "b.md", done.Items[1].URI)
}

func TestGroupTasksEmpty(t *testing.T) {
	require.Empty(t, groupTasks(nil))
}

func TestTaskMeta(t *testing.T) {
	dt, st := taskMeta(`{"type":"Task","status":"todo"}`)
	require.Equal(t, "Task", dt)
	require.Equal(t, "todo", st)

	dt, st = taskMeta(`{"type":"idea"}`)
	require.Equal(t, "idea", dt)
	require.Equal(t, "", st)

	dt, st = taskMeta("")
	require.Equal(t, "", dt)
	require.Equal(t, "", st)
}
