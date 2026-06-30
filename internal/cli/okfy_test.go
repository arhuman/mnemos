package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOkfyTxtProducesOKFAndIndexes(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")

	seedKB(t, "note.txt", "Plain text body without a heading.\n")

	out := runCmd(t, "okfy", "note.txt")
	require.Contains(t, out, "okfied note.txt -> note.md")
	require.Contains(t, out, "collection default")

	// Source kept intact.
	require.FileExists(t, kbPath("note.txt"))

	// OKF output has frontmatter and a derived heading.
	okf, err := os.ReadFile(kbPath("note.md"))
	require.NoError(t, err)
	body := string(okf)
	require.True(t, strings.HasPrefix(body, "---\n"), "expected frontmatter block")
	require.Contains(t, body, "type: document")
	require.Contains(t, body, "collection: default")
	require.Contains(t, body, "# note")

	// Indexed with chunks and discoverable.
	require.Regexp(t, `\d+ chunks`, out)
	status := runCmd(t, "status")
	require.Regexp(t, `documents\s+1`, status)

	search := runCmd(t, "search", "Plain text body")
	require.Contains(t, search, "note.md")
}

func TestOkfyHonorsOutFlag(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")

	seedKB(t, "source.txt", "Body content.\n")

	out := runCmd(t, "okfy", "source.txt", "--out", "docs/result.md", "--collection", "tech", "--type", "idea", "--tags", "a, b")
	require.Contains(t, out, "okfied source.txt -> docs/result.md")
	require.Contains(t, out, "collection tech")

	okf, err := os.ReadFile(kbPath(filepath.Join("docs", "result.md")))
	require.NoError(t, err)
	body := string(okf)
	require.Contains(t, body, "type: idea")
	require.Contains(t, body, "collection: tech")
	require.Contains(t, body, "- a")
	require.Contains(t, body, "- b")
}

func TestOkfyRejectsTraversalOut(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")

	seedKB(t, "source.txt", "Body.\n")

	_, err := runCmdErr(t, "okfy", "source.txt", "--out", "../escape.md")
	require.Error(t, err)
	require.Contains(t, err.Error(), "okfy: destination:")
}

func TestOkfyMdSourceWithoutOutErrors(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")

	seedKB(t, "doc.md", "# Doc\n\nBody.\n")

	_, err := runCmdErr(t, "okfy", "doc.md")
	require.Error(t, err)
	require.Contains(t, err.Error(), "equals source")

	// With --out it works and keeps the source.
	out := runCmd(t, "okfy", "doc.md", "--out", "doc.okf.md")
	require.Contains(t, out, "okfied doc.md -> doc.okf.md")
	require.FileExists(t, kbPath("doc.md"))
	require.FileExists(t, kbPath("doc.okf.md"))
}

func TestOkfyRejectsNonTextExtension(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")

	seedKB(t, "data.json", "{}\n")

	_, err := runCmdErr(t, "okfy", "data.json")
	require.Error(t, err)
	require.Contains(t, err.Error(), "must be a .txt or .md file")
}

func TestOkfyRejectsSecretInSource(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")

	seedKB(t, "leak.txt", "deploy key AKIAQYLPMN5HXYZ12345 do not commit\n")

	_, err := runCmdErr(t, "okfy", "leak.txt")
	require.Error(t, err)
	require.Contains(t, err.Error(), "detected secrets")

	// No OKF output written when a secret is detected.
	require.NoFileExists(t, kbPath("leak.md"))
}

func TestOkfyRejectsExistingOutput(t *testing.T) {
	chdir(t, t.TempDir())
	runCmd(t, "init")

	seedKB(t, "source.txt", "Body.\n")
	seedKB(t, "taken.md", "existing\n")

	_, err := runCmdErr(t, "okfy", "source.txt", "--out", "taken.md")
	require.Error(t, err)
	require.Contains(t, err.Error(), "already exists")

	// --force overwrites.
	out := runCmd(t, "okfy", "source.txt", "--out", "taken.md", "--force")
	require.Contains(t, out, "okfied source.txt -> taken.md")
}
