package cli_test

import (
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
		seedKB(t, name, body)
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

func TestTaskListTitleFromFrontmatterAndCollection(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")
	// The heading ("Placeholder") differs from the frontmatter title so the two
	// sources are distinguishable in the output.
	seedKB(t, "task.md", "---\ntype: Task\nstatus: todo\ntitle: Real Title\n---\n# Placeholder\n\nbody\n")
	// A document with no frontmatter at all: the json_extract type filter must
	// exclude it without erroring on the empty frontmatter_json.
	seedKB(t, "plain.md", "just a plain note, no frontmatter\n")
	runCmd(t, "ingest", ".", "--collection", "demo")

	out := runCmd(t, "task", "list")

	require.Contains(t, out, "Real Title", "title comes from frontmatter")
	require.NotContains(t, out, "Placeholder", "heading is not used when frontmatter has a title")
	require.Contains(t, out, "demo", "collection is shown per task")
	require.NotContains(t, out, "plain.md", "non-task without frontmatter is excluded and does not error")
}

func TestTaskListStatusFilter(t *testing.T) {
	chdir(t, t.TempDir())
	seedTasks(t)

	out := runCmd(t, "task", "list", "--status", "todo")
	require.Contains(t, out, "t1.md")
	require.NotContains(t, out, "t2.md")
	require.NotContains(t, out, "t3.md")
}
