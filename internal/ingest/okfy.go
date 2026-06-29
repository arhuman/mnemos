package ingest

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/arhuman/mnemos/internal/chunk"
	"github.com/arhuman/mnemos/internal/okf"
	"github.com/arhuman/mnemos/internal/security"
)

// OkfyOptions configures Okfy: the shared core that turns a plain .txt or .md
// file into an OKF document and indexes it, leaving the source intact. It is
// called by both the `okfy` CLI command and the mnemos.okfy MCP tool so the two
// surfaces share one implementation.
type OkfyOptions struct {
	// TreeRoot is the writable OKF tree root used to confine Source and Out.
	TreeRoot string
	// Exclude is the [security].exclude glob set applied to Source and Out.
	Exclude []string
	// Source is the tree-root-relative (or absolute, within the tree) path of the
	// plain file to convert. It must resolve to a .txt or .md file.
	Source string
	// Out is the output path within the tree. When empty it defaults to Source
	// with a .md extension. It must resolve to a .md file and must not equal Source.
	Out string
	// Collection is the collection the OKF document is indexed under. Empty means
	// "default".
	Collection string
	// Type is the OKF note type recorded in the frontmatter (e.g. "document").
	Type string
	// Tags are the optional frontmatter tags.
	Tags []string
	// Force overwrites the output file when it already exists.
	Force bool
	// Chunking is the token budget applied to the ingest.
	Chunking chunk.Config
	// Scanner screens the source body for secrets before anything is written.
	Scanner security.SecretScanner
}

// OkfyResult reports the outcome of Okfy: the output document's tree-root-
// relative URI (an immediate citation), the source URI, the resolved collection,
// the document id, and the number of chunks indexed.
type OkfyResult struct {
	URI        string
	SourceURI  string
	Collection string
	DocumentID string
	Chunks     int
}

// Okfy validates Source within the tree, derives and validates the output path,
// secret-scans the source body, renders an OKF document, writes it atomically,
// indexes it, and keeps the source. The source file is never overwritten: an
// output equal to the source is rejected, and an existing output requires Force.
func Okfy(ctx context.Context, db *sql.DB, logger *slog.Logger, opts OkfyOptions) (OkfyResult, error) {
	absSrc, srcURI, err := security.ResolveWithin(opts.TreeRoot, opts.Source, opts.Exclude)
	if err != nil {
		return OkfyResult{}, fmt.Errorf("okfy: source: %w", err)
	}
	if ext := strings.ToLower(filepath.Ext(absSrc)); ext != ".txt" && ext != ".md" {
		return OkfyResult{}, fmt.Errorf("okfy: source %q must be a .txt or .md file", srcURI)
	}

	content, err := os.ReadFile(absSrc) //nolint:gosec // absSrc validated as a .txt/.md source above
	if err != nil {
		return OkfyResult{}, fmt.Errorf("okfy: read %q: %w", srcURI, err)
	}
	info, err := os.Stat(absSrc)
	if err != nil {
		return OkfyResult{}, fmt.Errorf("okfy: stat %q: %w", srcURI, err)
	}

	absOut, outURI, outExisted, err := resolveOkfyOut(opts, absSrc)
	if err != nil {
		return OkfyResult{}, err
	}

	// Secret-scan the source body before writing anything. Report only the rule
	// names; never echo the matched secret back to the caller.
	findings, err := opts.Scanner.Scan(string(content))
	if err != nil {
		return OkfyResult{}, fmt.Errorf("okfy: scan %q: %w", srcURI, err)
	}
	if len(findings) > 0 {
		return OkfyResult{}, fmt.Errorf("okfy: rejected: detected secrets (%s); nothing written", strings.Join(findingRules(findings), ", "))
	}

	body := string(content)
	if !hasMarkdownHeading(body) {
		title := strings.TrimSuffix(filepath.Base(absSrc), filepath.Ext(absSrc))
		body = "# " + title + "\n\n" + body
	}

	collection := opts.Collection
	if collection == "" {
		collection = "default"
	}
	noteType := opts.Type
	if noteType == "" {
		noteType = "document"
	}

	// timestamp records "last modified": the source mtime when creating the OKF
	// file, but the current write time when overwriting an existing output with
	// --force, so the stamp reflects the modification.
	now := time.Now()
	stamp := info.ModTime()
	if outExisted {
		stamp = now
	}

	_, okfDoc := RenderOKF(CaptureInput{
		Type:       noteType,
		Body:       body,
		Tags:       opts.Tags,
		Collection: collection,
		Timestamp:  stamp,
	})
	if _, err = WriteFileAtomic(absOut, okfDoc); err != nil {
		return OkfyResult{}, fmt.Errorf("okfy: write %q: %w", outURI, err)
	}

	docID, chunks, err := File(ctx, db, logger, absOut, outURI, collection, opts.Chunking)
	if err != nil {
		return OkfyResult{}, fmt.Errorf("okfy: index %q: %w", outURI, err)
	}

	if err := appendOkfyLog(absOut, outURI, outExisted, now); err != nil {
		return OkfyResult{}, err
	}

	return OkfyResult{
		URI:        outURI,
		SourceURI:  srcURI,
		Collection: collection,
		DocumentID: docID,
		Chunks:     chunks,
	}, nil
}

// resolveOkfyOut derives and validates the OKF output path for a source. The
// output defaults to the source with a .md extension, must resolve within the
// tree, must end in .md, and must not equal the source (the source is never
// overwritten). outExisted reports whether the destination already exists, which
// requires opts.Force.
func resolveOkfyOut(opts OkfyOptions, absSrc string) (absOut, outURI string, outExisted bool, err error) {
	outArg := opts.Out
	if outArg == "" {
		outArg = strings.TrimSuffix(opts.Source, filepath.Ext(opts.Source)) + ".md"
	}
	absOut, outURI, err = security.ResolveWithin(opts.TreeRoot, outArg, opts.Exclude)
	if err != nil {
		return "", "", false, fmt.Errorf("okfy: destination: %w", err)
	}
	if strings.ToLower(filepath.Ext(absOut)) != ".md" {
		return "", "", false, fmt.Errorf("okfy: output %q must have a .md extension", outURI)
	}
	if absOut == absSrc {
		return "", "", false, fmt.Errorf("okfy: output %q equals source; pass --out to write the OKF elsewhere (the source file is never overwritten)", outURI)
	}
	_, statErr := os.Stat(absOut)
	outExisted = statErr == nil
	if outExisted && !opts.Force {
		return "", "", false, fmt.Errorf("okfy: output %q already exists; pass --force to overwrite", outURI)
	}

	return absOut, outURI, outExisted, nil
}

// appendOkfyLog records the okfy write in the destination directory's log.md.
// Reserved OKF files get no log entry. The concept is already written and
// indexed, so a failure here is wrapped and surfaced without losing it.
func appendOkfyLog(absOut, outURI string, outExisted bool, now time.Time) error {
	base := filepath.Base(absOut)
	if okf.IsReservedOKFFile(base) {
		return nil
	}
	kind := okf.LogCreation
	if outExisted {
		kind = okf.LogUpdate
	}
	name := strings.TrimSuffix(base, filepath.Ext(base))
	if err := okf.AppendLog(filepath.Dir(absOut), kind, name, "./"+base, now); err != nil {
		return fmt.Errorf("okfy: log %q: %w", outURI, err)
	}

	return nil
}

// hasMarkdownHeading reports whether body already contains an ATX heading
// ("# ", "## ", ...), so Okfy knows whether to prepend a derived title.
func hasMarkdownHeading(body string) bool {
	for line := range strings.SplitSeq(body, "\n") {
		t := strings.TrimSpace(line)
		if !strings.HasPrefix(t, "#") {
			continue
		}
		rest := strings.TrimLeft(t, "#")
		if rest == "" || strings.HasPrefix(rest, " ") {
			return true
		}
	}

	return false
}

// findingRules returns the sorted, de-duplicated rule names from findings. It
// never includes the matched secret values.
func findingRules(findings []security.Finding) []string {
	seen := make(map[string]struct{}, len(findings))
	for _, f := range findings {
		seen[f.Rule] = struct{}{}
	}
	rules := make([]string, 0, len(seen))
	for r := range seen {
		rules = append(rules, r)
	}
	slices.Sort(rules)

	return rules
}
