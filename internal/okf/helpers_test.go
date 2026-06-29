package okf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestLinkExists covers branches not exercised by the W2 validate fixture, which
// only tests a non-existent relative link (the stat-fails path).
func TestLinkExists(t *testing.T) {
	dir := t.TempDir()
	targetFile := filepath.Join(dir, "target.md")
	require.NoError(t, os.WriteFile(targetFile, []byte("# T\n"), 0o644))

	t.Run("same-file anchor returns true", func(t *testing.T) {
		require.True(t, linkExists(dir, "#section"))
	})
	t.Run("anchor on existing path returns true", func(t *testing.T) {
		require.True(t, linkExists(dir, "target.md#heading"))
	})
	t.Run("anchor on missing path returns false", func(t *testing.T) {
		require.False(t, linkExists(dir, "missing.md#heading"))
	})
	t.Run("slash-prefixed existing link returns true", func(t *testing.T) {
		require.True(t, linkExists(dir, "/target.md"))
	})
	t.Run("bare slash becomes empty link returns true", func(t *testing.T) {
		require.True(t, linkExists(dir, "/"))
	})
	t.Run("empty link returns true", func(t *testing.T) {
		require.True(t, linkExists(dir, ""))
	})
	t.Run("relative existing link returns true", func(t *testing.T) {
		require.True(t, linkExists(dir, "target.md"))
	})
	t.Run("relative missing link returns false", func(t *testing.T) {
		require.False(t, linkExists(dir, "nowhere.md"))
	})
}

// TestIsMarkdown covers the .markdown extension branch and the default (false)
// branch. TestValidate only reaches .md files via the testdata fixtures.
func TestIsMarkdown(t *testing.T) {
	t.Run("md extension", func(t *testing.T) {
		require.True(t, isMarkdown("file.md"))
	})
	t.Run("markdown extension", func(t *testing.T) {
		require.True(t, isMarkdown("notes.markdown"))
	})
	t.Run("markdown uppercase extension", func(t *testing.T) {
		require.True(t, isMarkdown("DOC.MARKDOWN"))
	})
	t.Run("txt is not markdown", func(t *testing.T) {
		require.False(t, isMarkdown("readme.txt"))
	})
	t.Run("no extension is not markdown", func(t *testing.T) {
		require.False(t, isMarkdown("Makefile"))
	})
}

// TestInsertLogEntryTitleNoSections covers the branch where content contains a
// title line but no existing "## " date sections. The new heading and bullet are
// inserted immediately after the title, making the date section appear first.
func TestInsertLogEntryTitleNoSections(t *testing.T) {
	content := "# Log\n"
	heading := "## 2026-06-28"
	bullet := "- **Creation** [foo](foo.md)"

	result := insertLogEntry(content, heading, bullet)

	require.Contains(t, result, heading)
	require.Contains(t, result, bullet)
	titleIdx := strings.Index(result, "# Log")
	headingIdx := strings.Index(result, heading)
	require.Less(t, titleIdx, headingIdx, "title must appear before the new heading")
}

// TestInsertLogEntryNoTitleNoSections covers the remaining sub-branch of Branch 3
// where content has neither a title nor any date sections (e.g. a completely empty
// log body). The heading and bullet are appended to whatever content exists.
func TestInsertLogEntryNoTitleNoSections(t *testing.T) {
	heading := "## 2026-06-28"
	bullet := "- **Creation** [bar](bar.md)"

	result := insertLogEntry("", heading, bullet)

	require.Contains(t, result, heading)
	require.Contains(t, result, bullet)
	// Result must end with a newline (guaranteed by insertLogEntry).
	require.True(t, strings.HasSuffix(result, "\n"), "result must end with newline")
}

// TestWriteFileAtomicCreatesNestedDir verifies that writeFileAtomic creates
// intermediate parent directories when they do not yet exist and writes content
// that can be read back verbatim.
func TestWriteFileAtomicCreatesNestedDir(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "sub", "nested", "file.txt")
	data := []byte("atomic content")

	require.NoError(t, writeFileAtomic(path, data))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, data, got)
}

// TestWriteFileAtomicOverwrites verifies that a second call to writeFileAtomic
// on the same path replaces the previous content atomically.
func TestWriteFileAtomicOverwrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "file.txt")

	require.NoError(t, writeFileAtomic(path, []byte("first")))
	require.NoError(t, writeFileAtomic(path, []byte("second")))

	got, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, []byte("second"), got)
}

// TestWriteFileAtomicErrorOnInvalidPath verifies that writeFileAtomic returns a
// non-nil error when the target path is inside a non-directory (e.g. /dev/null),
// exercising the MkdirAll error branch.
func TestWriteFileAtomicErrorOnInvalidPath(t *testing.T) {
	// /dev/null is a character device, not a directory; MkdirAll cannot create a
	// subdirectory inside it.
	err := writeFileAtomic("/dev/null/cannot/write/here.txt", []byte("x"))
	require.Error(t, err)
}
