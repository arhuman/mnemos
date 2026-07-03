// Package doctor runs read-only health detectors over an indexed OKF tree and
// returns structured findings. It never mutates the store or the files: it is the
// diagnosis half of the consolidation story (see ADR 0006), and the Finding
// vocabulary it emits is what a human (the `doctor` CLI) or, later, an agent (an
// MCP tool) acts on. All detectors are deterministic and driven by the index, so
// a run is cheap and repeatable.
package doctor

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/arhuman/mnemos/internal/okf"
	"github.com/arhuman/mnemos/internal/storage"
)

// Finding is one health issue. Category groups issues of the same kind; Severity
// is "warn" (likely worth acting on) or "info" (advisory). URIs lists the
// documents the reader should look at — for issues about a broken reference this
// is the source documents that carry the link. The struct is JSON-tagged so the
// CLI's --json mode and a future MCP tool share one shape.
type Finding struct {
	Category   string   `json:"category"`
	Severity   string   `json:"severity"`
	Title      string   `json:"title"`
	Detail     string   `json:"detail,omitempty"`
	URIs       []string `json:"uris"`
	Suggestion string   `json:"suggestion,omitempty"`
}

// Finding categories and severities.
const (
	CategoryDuplicate  = "duplicate"
	CategoryBrokenLink = "broken-link"
	CategoryOversized  = "oversized"
	CategoryTagHygiene = "tag-hygiene"
	CategoryStructure  = "structure"

	SeverityWarn = "warn"
	SeverityInfo = "info"
)

// DefaultMaxBytes is the oversized-document threshold when Options.MaxBytes is
// unset (50 KiB): large enough that ordinary notes pass, small enough to flag
// files that likely should be split.
const DefaultMaxBytes int64 = 51200

// Options narrows and tunes a run. PathPrefix/Collection scope the checks the
// same way `ls`/`search` filters do (empty = whole tree). MaxBytes overrides the
// oversized threshold (<= 0 uses DefaultMaxBytes).
type Options struct {
	PathPrefix string
	Collection string
	MaxBytes   int64
}

// Run executes every detector against the store and returns the findings, grouped
// by category in a stable order (so repeated runs and --json output are diffable).
// It is read-only.
func Run(ctx context.Context, db *sql.DB, opts Options) ([]Finding, error) {
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = DefaultMaxBytes
	}

	digests, err := storage.ListDocumentDigests(ctx, db, storage.ListFilter{
		Collection: opts.Collection,
		PathPrefix: opts.PathPrefix,
	})
	if err != nil {
		return nil, err
	}
	broken, err := storage.ListBrokenLinks(ctx, db)
	if err != nil {
		return nil, err
	}
	chunkless, err := storage.ListDocsWithoutChunks(ctx, db)
	if err != nil {
		return nil, err
	}

	findings := make([]Finding, 0, len(digests))
	findings = append(findings, exactDuplicates(digests)...)
	findings = append(findings, brokenLinks(scopeBroken(broken, opts.PathPrefix))...)
	findings = append(findings, oversized(digests, opts.MaxBytes)...)
	findings = append(findings, tagHygiene(digests)...)
	findings = append(findings, structuralGaps(digests, scopePrefix(chunkless, opts.PathPrefix))...)

	return findings, nil
}

// exactDuplicates groups documents by content_hash; any group of two or more is a
// set of byte-identical files.
func exactDuplicates(digests []storage.DocumentDigest) []Finding {
	byHash := make(map[string][]string)
	for _, d := range digests {
		if d.ContentHash == "" {
			continue
		}
		byHash[d.ContentHash] = append(byHash[d.ContentHash], d.URI)
	}

	var out []Finding
	for _, h := range sortedKeys(byHash) {
		uris := byHash[h]
		if len(uris) < 2 {
			continue
		}
		slices.Sort(uris)
		out = append(out, Finding{
			Category:   CategoryDuplicate,
			Severity:   SeverityWarn,
			Title:      fmt.Sprintf("%d byte-identical documents", len(uris)),
			Detail:     "same content hash",
			URIs:       uris,
			Suggestion: "keep one canonical copy; forget or merge the others",
		})
	}

	return out
}

// brokenLinks reports each dangling link target together with the source
// documents that reference it (the files to edit).
func brokenLinks(broken []storage.BrokenLink) []Finding {
	bySrc := make(map[string][]string)
	for _, b := range broken {
		bySrc[b.DstURI] = append(bySrc[b.DstURI], b.SrcURI)
	}

	out := make([]Finding, 0, len(bySrc))
	for _, dst := range sortedKeys(bySrc) {
		srcs := uniqueSorted(bySrc[dst])
		out = append(out, Finding{
			Category:   CategoryBrokenLink,
			Severity:   SeverityWarn,
			Title:      fmt.Sprintf("link to missing %q", dst),
			Detail:     fmt.Sprintf("referenced by %d document(s)", len(srcs)),
			URIs:       srcs,
			Suggestion: "fix or remove the link, or restore the target",
		})
	}

	return out
}

// oversized flags documents larger than the threshold as split candidates.
func oversized(digests []storage.DocumentDigest, maxBytes int64) []Finding {
	var out []Finding
	for _, d := range digests { // already ordered by uri
		if d.SizeBytes > maxBytes {
			out = append(out, Finding{
				Category:   CategoryOversized,
				Severity:   SeverityInfo,
				Title:      fmt.Sprintf("%s is %d bytes (over %d)", d.URI, d.SizeBytes, maxBytes),
				URIs:       []string{d.URI},
				Suggestion: "consider splitting into smaller linked documents",
			})
		}
	}

	return out
}

// frontmatterTags is the slice of the frontmatter JSON the tag-hygiene check
// needs; everything else in the frontmatter is ignored.
type frontmatterTags struct {
	Tags []string `json:"tags"`
}

// tagHygiene finds tags that differ only by case/whitespace (variants that
// fragment retrieval) and tags used on a single document (often typos).
func tagHygiene(digests []storage.DocumentDigest) []Finding {
	rawURIs := make(map[string][]string)        // raw tag -> uris using it
	normRaw := make(map[string]map[string]bool) // normalized tag -> set of raw spellings
	for _, d := range digests {
		if !hasFrontmatter(d.FrontmatterJSON) {
			continue
		}
		var fm frontmatterTags
		if err := json.Unmarshal([]byte(d.FrontmatterJSON), &fm); err != nil {
			continue // malformed frontmatter is a separate concern; skip here
		}
		seen := make(map[string]bool)
		for _, t := range fm.Tags {
			t = strings.TrimSpace(t)
			if t == "" || seen[t] {
				continue
			}
			seen[t] = true
			rawURIs[t] = append(rawURIs[t], d.URI)
			n := normalizeTag(t)
			if normRaw[n] == nil {
				normRaw[n] = make(map[string]bool)
			}
			normRaw[n][t] = true
		}
	}

	var out []Finding
	// Variant groups: one normalized key spelled more than one way.
	for _, n := range sortedKeys(normRaw) {
		if len(normRaw[n]) < 2 {
			continue
		}
		raws := setKeys(normRaw[n])
		var uris []string
		for _, r := range raws {
			uris = append(uris, rawURIs[r]...)
		}
		out = append(out, Finding{
			Category:   CategoryTagHygiene,
			Severity:   SeverityWarn,
			Title:      "tag variants: " + strings.Join(raws, ", "),
			Detail:     "these tags normalize to the same key",
			URIs:       uniqueSorted(uris),
			Suggestion: "normalize to a single spelling",
		})
	}
	// Single-use tags that are not already part of a variant group.
	var singles []string
	for t, uris := range rawURIs {
		if len(uris) == 1 && len(normRaw[normalizeTag(t)]) == 1 {
			singles = append(singles, t)
		}
	}
	slices.Sort(singles)
	for _, t := range singles {
		out = append(out, Finding{
			Category:   CategoryTagHygiene,
			Severity:   SeverityInfo,
			Title:      fmt.Sprintf("tag %q used once", t),
			Detail:     "single-use tags are often typos",
			URIs:       rawURIs[t],
			Suggestion: "confirm the tag or fold it into an existing one",
		})
	}

	return out
}

// structuralGaps reports missing frontmatter, empty bodies, and directories that
// hold concept files but no index.md (OKF W4).
func structuralGaps(digests []storage.DocumentDigest, chunkless []string) []Finding {
	var out []Finding

	// (a) missing frontmatter on non-reserved markdown documents.
	for _, d := range digests {
		if !strings.HasSuffix(d.URI, ".md") || okf.IsReservedOKFFile(path.Base(d.URI)) {
			continue
		}
		if !hasFrontmatter(d.FrontmatterJSON) {
			out = append(out, Finding{
				Category:   CategoryStructure,
				Severity:   SeverityWarn,
				Title:      d.URI + " has no frontmatter",
				URIs:       []string{d.URI},
				Suggestion: "add a YAML frontmatter block with at least a type",
			})
		}
	}

	// (b) empty body: indexed documents with no chunks.
	for _, uri := range chunkless {
		out = append(out, Finding{
			Category:   CategoryStructure,
			Severity:   SeverityInfo,
			Title:      uri + " has no body",
			URIs:       []string{uri},
			Suggestion: "add content or remove the empty document",
		})
	}

	// (c) directories with documents but no index.md.
	uriSet := make(map[string]bool)
	dirs := make(map[string]bool)
	for _, d := range digests {
		uriSet[d.URI] = true
		dirs[dirOf(d.URI)] = true
	}
	for _, dir := range sortedBoolKeys(dirs) {
		if !uriSet[indexURI(dir)] {
			out = append(out, Finding{
				Category:   CategoryStructure,
				Severity:   SeverityInfo,
				Title:      fmt.Sprintf("directory %q has no index.md", dirLabel(dir)),
				URIs:       []string{indexURI(dir)},
				Suggestion: "add an index.md describing the directory (OKF W4)",
			})
		}
	}

	return out
}

// normalizeTag collapses case and surrounding whitespace so "Cherry-Pick" and
// "cherry-pick " map to the same key. Plural/stemming is intentionally not done
// (too many false merges).
func normalizeTag(t string) string {
	return strings.ToLower(strings.TrimSpace(t))
}

// hasFrontmatter reports whether the stored frontmatter JSON carries anything;
// an absent block is stored as "", "{}", or "null".
func hasFrontmatter(j string) bool {
	s := strings.TrimSpace(j)

	return s != "" && s != "{}" && s != "null"
}

// dirOf returns the slash-relative directory of a uri, or "" for a root-level
// file.
func dirOf(uri string) string {
	d := path.Dir(uri)
	if d == "." {
		return ""
	}

	return d
}

// indexURI is the index.md uri for a directory ("" = tree root).
func indexURI(dir string) string {
	if dir == "" {
		return "index.md"
	}

	return dir + "/index.md"
}

// dirLabel renders a directory for display ("." for the tree root).
func dirLabel(dir string) string {
	if dir == "" {
		return "."
	}

	return dir
}

// sortedKeys returns the map keys sorted, for deterministic iteration.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	return keys
}

// sortedBoolKeys is sortedKeys for a set.
func sortedBoolKeys(m map[string]bool) []string { return sortedKeys(m) }

// setKeys returns the sorted members of a string set.
func setKeys(m map[string]bool) []string { return sortedKeys(m) }

// uniqueSorted returns the input deduplicated and sorted.
func uniqueSorted(in []string) []string {
	set := make(map[string]bool)
	for _, s := range in {
		set[s] = true
	}

	return sortedKeys(set)
}

// scopePrefix keeps uris under prefix (empty prefix keeps all).
func scopePrefix(uris []string, prefix string) []string {
	if prefix == "" {
		return uris
	}
	var out []string
	for _, u := range uris {
		if strings.HasPrefix(u, prefix) {
			out = append(out, u)
		}
	}

	return out
}

// scopeBroken keeps broken-link edges whose source document is under prefix, so
// `doctor <path>` reports links carried by documents in that subtree.
func scopeBroken(broken []storage.BrokenLink, prefix string) []storage.BrokenLink {
	if prefix == "" {
		return broken
	}
	var out []storage.BrokenLink
	for _, b := range broken {
		if strings.HasPrefix(b.SrcURI, prefix) {
			out = append(out, b)
		}
	}

	return out
}
