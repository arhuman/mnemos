package cli_test

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// seedTasks creates a small tree of Task documents (and one non-task) and
// indexes them. It assumes the test has already chdir'd into a temp dir.
func seedTasks(t *testing.T) {
	t.Helper()
	runCmd(t, "init")
	write := func(name, body string) {
		require.NoError(t, os.WriteFile(name, []byte(body), 0o644))
	}
	write("t1.md", "---\ntype: Task\nstatus: todo\ntitle: First\n---\n# First\n\nbody\n")
	write("t2.md", "---\ntype: Task\nstatus: in_progress\ntitle: Second\n---\n# Second\n\nbody\n")
	write("t3.md", "---\ntype: Task\ntitle: Third\n---\n# Third\n\nbody\n")
	write("note.md", "---\ntype: idea\n---\n# Note\n\nbody\n")
	runCmd(t, "ingest", ".", "--collection", "demo")
}

func TestTaskListGroupsByStatus(t *testing.T) {
	chdir(t, t.TempDir())
	seedTasks(t)

	out := runCmd(t, "task", "list")

	require.Contains(t, out, "in_progress")
	require.Contains(t, out, "todo")
	require.Contains(t, out, "(no status)")
	require.Contains(t, out, "t1.md")
	require.Contains(t, out, "t2.md")
	require.Contains(t, out, "t3.md")
	// Non-task documents are excluded.
	require.NotContains(t, out, "note.md")

	// in_progress is listed before todo (fixed order).
	require.Less(t, strings.Index(out, "in_progress"), strings.Index(out, "todo"),
		"in_progress group precedes todo group")
}

func TestTaskListStatusFilter(t *testing.T) {
	chdir(t, t.TempDir())
	seedTasks(t)

	out := runCmd(t, "task", "list", "--status", "todo")
	require.Contains(t, out, "t1.md")
	require.NotContains(t, out, "t2.md")
	require.NotContains(t, out, "t3.md")
}
