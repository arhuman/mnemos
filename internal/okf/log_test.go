package okf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/parse"
)

func TestAppendLogCreatesFileWithoutFrontmatter(t *testing.T) {
	dir := t.TempDir()
	when := time.Date(2026, 6, 27, 10, 0, 0, 0, time.UTC)

	require.NoError(t, AppendLog(dir, LogCreation, "alpha", "./alpha.md", when))

	content, err := os.ReadFile(filepath.Join(dir, "log.md"))
	require.NoError(t, err)
	body := string(content)

	require.True(t, strings.HasPrefix(body, "# Log\n"), "log starts with a title")
	require.False(t, strings.HasPrefix(body, "---"), "log must not have YAML frontmatter (E3)")
	require.Contains(t, body, "## 2026-06-27")
	require.Contains(t, body, "- **Creation** [alpha](./alpha.md)")

	// The validator's log checks must pass: no E3, no W5.
	require.False(t, parse.HasFrontmatter(content), "no frontmatter -> no E3")
	require.True(t, datesISO8601Descending(content), "headings ISO-8601 and newest-first -> no W5")
}

func TestAppendLogTopOfTodaySection(t *testing.T) {
	dir := t.TempDir()
	when := time.Date(2026, 6, 27, 10, 0, 0, 0, time.UTC)

	require.NoError(t, AppendLog(dir, LogCreation, "alpha", "./alpha.md", when))
	require.NoError(t, AppendLog(dir, LogUpdate, "beta", "./beta.md", when))

	content, err := os.ReadFile(filepath.Join(dir, "log.md"))
	require.NoError(t, err)
	body := string(content)

	// Only one date section for today.
	require.Equal(t, 1, strings.Count(body, "## 2026-06-27"))
	// Newest bullet (beta) is at the top of today's section, above alpha.
	betaIdx := strings.Index(body, "[beta]")
	alphaIdx := strings.Index(body, "[alpha]")
	require.Less(t, betaIdx, alphaIdx, "newest bullet first within the day")

	require.True(t, datesISO8601Descending(content))
	require.False(t, parse.HasFrontmatter(content))
}

func TestAppendLogNewerSectionGoesOnTop(t *testing.T) {
	dir := t.TempDir()
	older := time.Date(2026, 6, 26, 10, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 6, 27, 10, 0, 0, 0, time.UTC)

	require.NoError(t, AppendLog(dir, LogCreation, "old", "./old.md", older))
	require.NoError(t, AppendLog(dir, LogUpdate, "new", "./new.md", newer))

	content, err := os.ReadFile(filepath.Join(dir, "log.md"))
	require.NoError(t, err)
	body := string(content)

	newSecIdx := strings.Index(body, "## 2026-06-27")
	oldSecIdx := strings.Index(body, "## 2026-06-26")
	require.Positive(t, newSecIdx)
	require.Positive(t, oldSecIdx)
	require.Less(t, newSecIdx, oldSecIdx, "newest date section on top (newest-first)")

	// The validator's W5/E3 rules hold on the generated multi-day log.
	require.True(t, datesISO8601Descending(content), "newest-first preserved -> no W5")
	require.False(t, parse.HasFrontmatter(content), "no frontmatter -> no E3")
}

func TestIsReservedOKFFile(t *testing.T) {
	require.True(t, IsReservedOKFFile("log.md"))
	require.True(t, IsReservedOKFFile("INDEX.MD"))
	require.False(t, IsReservedOKFFile("concept.md"))
}
