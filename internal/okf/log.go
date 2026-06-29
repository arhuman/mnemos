package okf

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LogKind is the leading bold word of a log.md bullet: the lifecycle event a
// concept entry records.
type LogKind string

const (
	// LogCreation marks a concept written for the first time.
	LogCreation LogKind = "Creation"
	// LogUpdate marks an existing concept overwritten in place.
	LogUpdate LogKind = "Update"
	// LogDeprecation marks a concept retired.
	LogDeprecation LogKind = "Deprecation"
)

// logFileName is the reserved per-directory changelog OKF expects.
const logFileName = "log.md"

// AppendLog records a concept change in dirAbs/log.md, the reserved
// per-directory changelog. It maintains a newest-first list of ISO-8601
// (YYYY-MM-DD) date sections, each holding "- **<Kind>** [name](link)" bullets.
//
// The file is created with a "# Log" title and NO YAML frontmatter (E3 forbids
// frontmatter in a reserved file). A new bullet is inserted at the top of
// today's section; when today's section is absent it is created immediately
// after the title, above any older sections, so the date headings stay
// newest-first (W5). Existing content is never reordered. The write is atomic.
func AppendLog(dirAbs string, kind LogKind, conceptName, link string, when time.Time) error {
	logPath := filepath.Join(dirAbs, logFileName)
	heading := "## " + when.Format("2006-01-02")
	bullet := fmt.Sprintf("- **%s** [%s](%s)", kind, conceptName, link)

	raw, err := os.ReadFile(logPath) //nolint:gosec // logPath derived from the validated bundle root
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("okf: read log %q: %w", logPath, err)
	}

	var content string
	if errors.Is(err, fs.ErrNotExist) || len(bytes.TrimSpace(raw)) == 0 {
		content = "# Log\n\n" + heading + "\n" + bullet + "\n"
	} else {
		content = insertLogEntry(string(raw), heading, bullet)
	}

	if err := writeFileAtomic(logPath, []byte(content)); err != nil {
		return fmt.Errorf("okf: write log %q: %w", logPath, err)
	}

	return nil
}

// insertLogEntry inserts bullet into content under heading, keeping the log
// newest-first. When heading is already the topmost `## ` section the bullet is
// placed at the top of that section; otherwise a fresh heading+bullet section is
// inserted immediately before the first existing section (just under the title).
func insertLogEntry(content, heading, bullet string) string {
	lines := strings.Split(content, "\n")

	titleIdx, firstSecIdx := -1, -1
	for i, ln := range lines {
		t := strings.TrimSpace(ln)
		if titleIdx == -1 && strings.HasPrefix(t, "# ") {
			titleIdx = i
		}
		if strings.HasPrefix(t, "## ") {
			firstSecIdx = i

			break
		}
	}

	// Today's section is already topmost: insert the bullet at its top.
	if firstSecIdx != -1 && strings.TrimSpace(lines[firstSecIdx]) == heading {
		out := make([]string, 0, len(lines)+1)
		out = append(out, lines[:firstSecIdx+1]...)
		out = append(out, bullet)
		out = append(out, lines[firstSecIdx+1:]...)

		return strings.Join(out, "\n")
	}

	// A new section is needed. Place it before the first existing section so it
	// stays newest-first; when there is no section yet, append after the title.
	if firstSecIdx != -1 {
		block := []string{heading, bullet, ""}
		out := make([]string, 0, len(lines)+len(block))
		out = append(out, lines[:firstSecIdx]...)
		out = append(out, block...)
		out = append(out, lines[firstSecIdx:]...)

		return strings.Join(out, "\n")
	}

	at := len(lines)
	if titleIdx != -1 {
		at = titleIdx + 1
	}
	block := []string{"", heading, bullet}
	out := make([]string, 0, len(lines)+len(block))
	out = append(out, lines[:at]...)
	out = append(out, block...)
	out = append(out, lines[at:]...)
	joined := strings.Join(out, "\n")
	if !strings.HasSuffix(joined, "\n") {
		joined += "\n"
	}

	return joined
}

// writeFileAtomic writes content to absPath via a same-directory temp file and a
// rename, so a reader never sees a half-written log. It mirrors the ingest
// writer but lives here to keep the okf package free of an ingest import.
func writeFileAtomic(absPath string, content []byte) error {
	dir := filepath.Dir(absPath)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("mkdir %q: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(absPath)+".tmp-*")
	if err != nil {
		return fmt.Errorf("temp file in %q: %w", dir, err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)

		return fmt.Errorf("write %q: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)

		return fmt.Errorf("close %q: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, absPath); err != nil {
		_ = os.Remove(tmpPath)

		return fmt.Errorf("rename %q -> %q: %w", tmpPath, absPath, err)
	}

	return nil
}

// IsReservedOKFFile reports whether base is an OKF reserved file (log.md or
// index.md), which must never get its own log entry.
func IsReservedOKFFile(base string) bool {
	switch strings.ToLower(base) {
	case "log.md", "index.md":
		return true
	default:
		return false
	}
}
