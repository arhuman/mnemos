// Package okf implements an Open Knowledge Format (OKF) v0.1 conformance
// validator. It walks a bundle directory and reports structural violations as
// errors (which break conformance) and recommended-practice gaps as warnings.
// The logic is pure: it depends only on internal/model and internal/parse and
// never opens a store, so callers can validate a bundle without a database.
package okf

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/arhuman/mnemos/internal/model"
	"github.com/arhuman/mnemos/internal/parse"
)

// Severity classifies an Issue as a conformance error or a recommendation.
type Severity int

const (
	// SevError marks a conformance-breaking violation (E-rules).
	SevError Severity = iota
	// SevWarning marks a recommended-practice gap (W-rules).
	SevWarning
)

// Issue is a single validation finding. File is bundle-relative with
// forward-slash separators, or "" for a bundle-level issue.
type Issue struct {
	Code     string   `json:"code"`
	Severity Severity `json:"severity"`
	File     string   `json:"file"`
	Message  string   `json:"message"`
}

// Report is the outcome of validating a bundle: the issues found and their
// tallies. A bundle is OKF v0.1 conformant when it has zero errors.
type Report struct {
	Bundle   string  `json:"bundle"`
	Files    int     `json:"files"`
	Issues   []Issue `json:"issues"`
	Errors   int     `json:"errors"`
	Warnings int     `json:"warnings"`
}

// OK reports whether the bundle is conformant (no errors). Warnings do not
// break conformance.
func (r Report) OK() bool { return r.Errors == 0 }

// add appends issues to the report.
func (r *Report) add(issues []Issue) {
	r.Issues = append(r.Issues, issues...)
}

// tally recomputes the Errors/Warnings counts from the recorded issues.
func (r *Report) tally() {
	r.Errors, r.Warnings = 0, 0
	for _, iss := range r.Issues {
		switch iss.Severity {
		case SevError:
			r.Errors++
		case SevWarning:
			r.Warnings++
		default:
			// other severities are not counted
		}
	}
}

// Validate walks bundle and checks OKF v0.1 conformance. It classifies each
// markdown file as a reserved file (index.md, log.md) or a concept file and
// applies the rule set, returning a Report. It returns a non-nil error only on
// a filesystem I/O failure (WalkDir or ReadFile); non-conformance is reported
// through the Report, not as an error.
func Validate(ctx context.Context, bundle string) (Report, error) {
	rep := Report{Bundle: bundle}

	hasIndex := make(map[string]bool)
	conceptDirs := make(map[string]bool)

	walkErr := filepath.WalkDir(bundle, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !isMarkdown(p) {
			return nil
		}
		relPath, err := filepath.Rel(bundle, p)
		if err != nil {
			return err
		}
		rel := filepath.ToSlash(relPath)
		dir := filepath.ToSlash(filepath.Dir(rel))

		content, err := os.ReadFile(p) //nolint:gosec // p comes from walking the validated bundle dir
		if err != nil {
			return err
		}

		switch strings.ToLower(filepath.Base(rel)) {
		case "index.md":
			hasIndex[dir] = true
			rep.add(checkIndex(rel, content, dir == "."))
		case "log.md":
			rep.add(checkLog(rel, content))
		default:
			conceptDirs[dir] = true
			rep.Files++
			rep.add(checkConcept(ctx, bundle, rel, content))
		}

		return nil
	})
	if walkErr != nil {
		return Report{}, fmt.Errorf("validate %q: %w", bundle, walkErr)
	}

	for dir := range conceptDirs {
		if !hasIndex[dir] {
			rep.add([]Issue{{
				Code:     "W4",
				Severity: SevWarning,
				File:     dir,
				Message:  "directory with concept files has no index.md",
			}})
		}
	}

	rep.tally()

	return rep, nil
}

// checkConcept validates a single concept file (E1, E2, W1, W2, W3).
func checkConcept(ctx context.Context, bundle, rel string, content []byte) []Issue {
	if !parse.HasFrontmatter(content) {
		return []Issue{{Code: "E1", Severity: SevError, File: rel,
			Message: "concept file has no YAML frontmatter"}}
	}

	doc, err := parse.MarkdownParser{}.Parse(ctx, model.Source{URI: rel, Content: content})
	if err != nil {
		return []Issue{{Code: "E1", Severity: SevError, File: rel,
			Message: "unparseable frontmatter: " + err.Error()}}
	}

	var issues []Issue
	if strings.TrimSpace(doc.DocType) == "" {
		issues = append(issues, Issue{Code: "E2", Severity: SevError, File: rel,
			Message: "frontmatter has no non-empty `type` field"})
	}
	if asStr(doc.Frontmatter["title"]) == "" || asStr(doc.Frontmatter["description"]) == "" {
		issues = append(issues, Issue{Code: "W1", Severity: SevWarning, File: rel,
			Message: "concept missing recommended `title` or `description`"})
	}
	if strings.TrimSpace(doc.ModifiedAt) == "" {
		issues = append(issues, Issue{Code: "W3", Severity: SevWarning, File: rel,
			Message: "concept missing `timestamp`"})
	}
	for _, link := range doc.Links {
		if !linkExists(bundle, link) {
			issues = append(issues, Issue{Code: "W2", Severity: SevWarning, File: rel,
				Message: "broken cross-link: " + link})
		}
	}

	return issues
}

// checkIndex validates a reserved index.md (E3). The bundle-root index.md may
// carry frontmatter (e.g. okf_version) and is exempt.
func checkIndex(rel string, content []byte, isRoot bool) []Issue {
	if !parse.HasFrontmatter(content) || isRoot {
		return nil
	}

	return []Issue{{Code: "E3", Severity: SevError, File: rel,
		Message: "index.md must not have frontmatter (reserved file)"}}
}

// checkLog validates a reserved log.md (E3, W5).
func checkLog(rel string, content []byte) []Issue {
	if parse.HasFrontmatter(content) {
		return []Issue{{Code: "E3", Severity: SevError, File: rel,
			Message: "log.md must not have frontmatter (reserved file)"}}
	}
	if !datesISO8601Descending(content) {
		return []Issue{{Code: "W5", Severity: SevWarning, File: rel,
			Message: "log.md date headings are not ISO 8601 or not newest-first"}}
	}

	return nil
}

// linkExists reports whether a resolved bundle link points to a file that
// exists on disk. Same-file anchors (#frag) always exist. An OKF absolute link
// ("/a/b.md") is bundle-relative.
func linkExists(bundle, link string) bool {
	if i := strings.IndexByte(link, '#'); i >= 0 {
		if i == 0 {
			return true
		}
		link = link[:i]
	}
	link = strings.TrimPrefix(link, "/")
	if link == "" {
		return true
	}
	_, err := os.Stat(filepath.Join(bundle, filepath.FromSlash(link)))

	return err == nil
}

// datesISO8601Descending scans `## ` headings, parses a leading ISO 8601 date
// token from each, and reports whether the parsed dates are present and in
// non-increasing (newest-first) order. A log with no parseable date heading is
// treated as failing (false).
func datesISO8601Descending(content []byte) bool {
	var dates []time.Time
	for raw := range strings.SplitSeq(string(content), "\n") {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, "## ") {
			continue
		}
		token := strings.TrimSpace(strings.TrimPrefix(line, "##"))
		if token == "" {
			continue
		}
		token = strings.Fields(token)[0]
		t, err := time.Parse("2006-01-02", token)
		if err != nil {
			continue
		}
		dates = append(dates, t)
	}
	if len(dates) == 0 {
		return false
	}
	for i := 1; i < len(dates); i++ {
		if dates[i].After(dates[i-1]) {
			return false
		}
	}

	return true
}

// asStr renders a frontmatter scalar as a trimmed string ("" for nil).
func asStr(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(t)
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", t))
	}
}

// isMarkdown reports whether path has a markdown extension.
func isMarkdown(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown":
		return true
	default:
		return false
	}
}
