package cli_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/cli"
)

// runCmd executes the root command with args, capturing stdout.
func runCmd(t *testing.T, args ...string) string {
	t.Helper()
	root := cli.NewRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	require.NoError(t, root.Execute())

	return out.String()
}

// chdir switches into dir for the duration of the test and restores the
// previous working directory on cleanup.
func chdir(t *testing.T, dir string) {
	t.Helper()
	prev, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(prev) })
}

// kbPath returns rel resolved inside the project-local knowledge base
// (./.mnemos/kb), where all addressable content lives under the Phase 2 model.
func kbPath(rel string) string {
	return filepath.Join(".mnemos", "kb", rel)
}

// seedKB writes content at rel inside the local kb, creating parent dirs. Use it
// with `ingest .` to index seeded content (the scan root resolves to the kb).
func seedKB(t *testing.T, rel, content string) {
	t.Helper()
	p := kbPath(rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o750))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
}

// TestInitThenStatus runs `init` then `status` in a fresh temp dir and asserts
// the workspace is created and status reports zero counts with FTS available.
func TestInitThenStatus(t *testing.T) {
	chdir(t, t.TempDir())

	initOut := runCmd(t, "init")
	require.Contains(t, initOut, "created config")
	require.Contains(t, initOut, "mnemos.toml")
	require.Contains(t, initOut, "initialized database")

	require.FileExists(t, ".mnemos/mnemos.toml")
	require.DirExists(t, ".mnemos/kb")
	require.DirExists(t, ".mnemos/kb/capture")
	require.FileExists(t, ".mnemos/state/index.db")

	statusOut := runCmd(t, "status")

	cases := []struct {
		label string
		want  string
	}{
		{"collections", "collections"},
		{"documents", "documents"},
		{"chunks", "chunks"},
		{"fts available", "fts available"},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			require.Contains(t, statusOut, tc.want)
		})
	}

	t.Run("fts reported available", func(t *testing.T) {
		require.Regexp(t, `fts available\s+true`, statusOut)
	})

	t.Run("zero counts", func(t *testing.T) {
		for line := range strings.SplitSeq(statusOut, "\n") {
			switch {
			case strings.HasPrefix(line, "collections"),
				strings.HasPrefix(line, "documents"),
				strings.HasPrefix(line, "chunks"):
				require.True(t, strings.HasSuffix(strings.TrimSpace(line), "0"),
					"expected zero count: %q", line)
			default:
				// other lines are ignored
			}
		}
	})
}

// TestInitDoesNotOverwriteConfig asserts a re-run keeps an existing config.
func TestInitDoesNotOverwriteConfig(t *testing.T) {
	chdir(t, t.TempDir())

	require.NoError(t, os.MkdirAll(".mnemos", 0o750))
	const seed = "[search]\ndefault_limit = 3\n"
	require.NoError(t, os.WriteFile(".mnemos/mnemos.toml", []byte(seed), 0o644))

	out := runCmd(t, "init")
	require.Contains(t, out, "kept existing config")

	got, err := os.ReadFile(".mnemos/mnemos.toml")
	require.NoError(t, err)
	require.Equal(t, seed, string(got))
}
