package cli_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/cli"
)

// runHook executes the root command with args, feeding stdin, and captures
// stdout. Hook subcommands read the Claude Code event JSON from stdin, so the
// plain runCmd helper (no stdin) cannot exercise recall.
func runHook(t *testing.T, stdin string, args ...string) string {
	t.Helper()
	root := cli.NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetIn(strings.NewReader(stdin))
	root.SetArgs(args)
	require.NoError(t, root.Execute())

	return out.String()
}

// seedWorkingSet indexes two open tasks (in_progress, todo), one done task, and
// one decision document in a fresh local workspace under the current temp dir.
func seedWorkingSet(t *testing.T) {
	t.Helper()
	runCmd(t, "init")
	seedKB(t, "tasks/live.md", "---\ntype: Task\nstatus: in_progress\ntitle: Wire the gateway\n---\n# Wire the gateway\n\nbody\n")
	seedKB(t, "tasks/next.md", "---\ntype: Task\nstatus: todo\ntitle: Add CSV export\n---\n# Add CSV export\n\nbody\n")
	seedKB(t, "tasks/old.md", "---\ntype: Task\nstatus: done\ntitle: Ancient history\n---\n# Ancient history\n\nbody\n")
	seedKB(t, "decisions/0001-anchor.md", "---\ntype: decision\ntitle: Single MNEMOS_DIR anchor\n---\n# Single MNEMOS_DIR anchor\n\nbody\n")
	runCmd(t, "ingest", ".", "--collection", "demo")
}

func TestHookSessionStartInjectsScopedWorkingSet(t *testing.T) {
	chdir(t, t.TempDir())
	t.Setenv("CLAUDE_PROJECT_DIR", "") // force cwd resolution to the local workspace
	seedWorkingSet(t)

	out := runHook(t, "", "hook", "session-start")

	require.Contains(t, out, "## mnemos working set")
	require.Contains(t, out, "### Open tasks")
	require.Contains(t, out, "Wire the gateway")
	require.Contains(t, out, "Add CSV export")
	require.Contains(t, out, "### Recent decisions")
	require.Contains(t, out, "Single MNEMOS_DIR anchor")
	// done tasks are never in the working set.
	require.NotContains(t, out, "Ancient history")
	// project scope: no per-collection labelling banner.
	require.NotContains(t, out, "labelled by collection")
}

func TestHookSessionStartSilentWithoutWorkspace(t *testing.T) {
	// A signalled project with no .mnemos resolves fail-closed to an uninitialized
	// location; the absent database must make the hook stay silent, never dump the
	// global store.
	proj := t.TempDir()
	t.Setenv("CLAUDE_PROJECT_DIR", proj)
	chdir(t, t.TempDir())

	out := runHook(t, "", "hook", "session-start")

	require.Empty(t, out)
}

func TestHookRecallInjectsHits(t *testing.T) {
	chdir(t, t.TempDir())
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	runCmd(t, "init")
	seedKB(t, "decisions/0002-staging.md",
		"---\ntype: decision\ntitle: Staging on the VPS\n---\n# Staging on the VPS\n\nWe choose staging on the vps because it is cheap.\n")
	runCmd(t, "ingest", ".", "--collection", "demo")

	out := runHook(t, `{"prompt":"why did we choose staging on the vps?","hook_event_name":"UserPromptSubmit"}`, "hook", "recall")

	require.Contains(t, out, "## mnemos recall")
	require.Contains(t, out, "decisions/0002-staging.md")
}

func TestHookRecallSilentOnNoHits(t *testing.T) {
	chdir(t, t.TempDir())
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	runCmd(t, "init")
	seedKB(t, "decisions/0002-staging.md", "---\ntype: decision\ntitle: Staging\n---\n# Staging\n\nWe choose staging on the vps.\n")
	runCmd(t, "ingest", ".", "--collection", "demo")

	out := runHook(t, `{"prompt":"what about banana pancakes for breakfast?"}`, "hook", "recall")

	require.Empty(t, out)
}

func TestHookRecallSilentOnNonQuestion(t *testing.T) {
	chdir(t, t.TempDir())
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	runCmd(t, "init")
	seedKB(t, "decisions/0002-staging.md", "---\ntype: decision\ntitle: Staging\n---\n# Staging\n\nstaging vps notes\n")
	runCmd(t, "ingest", ".", "--collection", "demo")

	// No cue phrase and not question-shaped: recall must not fire even though the
	// terms would match.
	out := runHook(t, `{"prompt":"update the staging vps config"}`, "hook", "recall")

	require.Empty(t, out)
}

func TestHookRecallSilentWithoutWorkspace(t *testing.T) {
	proj := t.TempDir()
	t.Setenv("CLAUDE_PROJECT_DIR", proj)
	chdir(t, t.TempDir())

	out := runHook(t, `{"prompt":"why did we choose staging on the vps?"}`, "hook", "recall")

	require.Empty(t, out)
}
