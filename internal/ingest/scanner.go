package ingest

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/bmatcuk/doublestar/v4"
)

// scanned is a file selected for ingestion: its absolute path and the
// scan-root-relative URI stored as documents.uri.
type scanned struct {
	absPath string
	uri     string
}

// scanRules holds the glob sets that decide inclusion. A file is included iff it
// matches at least one Include glob and none of Exclude or SecurityExclude.
// Matching is performed on the slash-normalized path relative to the scan root.
type scanRules struct {
	include         []string
	exclude         []string
	securityExclude []string
}

// scan walks root and returns the files selected by rules, in deterministic
// (lexical) order. root may be a single file, in which case it is evaluated
// against the rules using its base name as the relative path.
//
// Glob matching is performed on the path relative to root (so exclude anchors
// like ".git/**" catch the scan root), while the stored URI is the path relative
// to uriBase. uriBase is the knowledge-base root, so a subtree ingest still mints
// kb-relative URIs; passing root as uriBase reproduces scan-root-relative URIs.
func scan(root, uriBase string, rules scanRules) ([]scanned, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("scan: stat %q: %w", root, err)
	}
	if uriBase == "" {
		// Default to the scan root, reproducing scan-root-relative URIs. For a
		// single-file root the base is its parent dir, so the URI is the base name
		// (Rel(file, file) would otherwise be ".").
		if info.IsDir() {
			uriBase = root
		} else {
			uriBase = filepath.Dir(root)
		}
	}
	baseAbs, err := filepath.Abs(uriBase)
	if err != nil {
		return nil, fmt.Errorf("scan: abs uri base %q: %w", uriBase, err)
	}

	if !info.IsDir() {
		return scanFile(root, baseAbs, rules)
	}

	return scanDir(root, baseAbs, rules)
}

// uriRelTo returns the slash-normalized path of abs relative to baseAbs.
func uriRelTo(baseAbs, abs string) (string, error) {
	rel, err := filepath.Rel(baseAbs, abs)
	if err != nil {
		return "", fmt.Errorf("scan: uri rel %q: %w", abs, err)
	}

	return filepath.ToSlash(rel), nil
}

// scanFile evaluates a single-file root against the rules (matched by base name)
// and, if selected, returns it with a uriBase-relative URI.
func scanFile(root, baseAbs string, rules scanRules) ([]scanned, error) {
	if !rules.match(filepath.Base(root)) {
		return nil, nil
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("scan: abs %q: %w", root, err)
	}
	uri, err := uriRelTo(baseAbs, abs)
	if err != nil {
		return nil, err
	}

	return []scanned{{absPath: abs, uri: uri}}, nil
}

// scanDir walks a directory root, selecting files by rules (matched on the
// root-relative path) and storing each with a uriBase-relative URI.
func scanDir(root, baseAbs string, rules scanRules) ([]scanned, error) {
	var out []scanned
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		matchRel, err := filepath.Rel(root, path)
		if err != nil {
			return fmt.Errorf("scan: rel %q: %w", path, err)
		}
		if !rules.match(filepath.ToSlash(matchRel)) {
			return nil
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("scan: abs %q: %w", path, err)
		}
		uri, err := uriRelTo(baseAbs, abs)
		if err != nil {
			return err
		}
		out = append(out, scanned{absPath: abs, uri: uri})

		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("scan: walk %q: %w", root, walkErr)
	}

	return out, nil
}

// match reports whether the slash-normalized relative path is selected: it must
// match an include glob and no exclude (indexing or security) glob.
func (r scanRules) match(rel string) bool {
	if !anyMatch(r.include, rel) {
		return false
	}
	if anyMatch(r.exclude, rel) {
		return false
	}
	if anyMatch(r.securityExclude, rel) {
		return false
	}

	return true
}

// Match reports whether the file at root-relative path relPath is selected by
// the include/exclude glob sets, using the exact same predicate the directory
// scanner applies. relPath is matched after slash-normalization, so callers may
// pass an OS-native relative path. The watcher uses this to decide, per live
// event, whether a file should be ingested — sharing one predicate guarantees
// the watcher and the batch pipeline never disagree about what is indexable.
func Match(relPath string, include, exclude, securityExclude []string) bool {
	rel := filepath.ToSlash(relPath)

	return scanRules{include: include, exclude: exclude, securityExclude: securityExclude}.match(rel)
}

// anyMatch reports whether rel matches any of the doublestar globs. An invalid
// glob pattern is treated as non-matching rather than fatal.
func anyMatch(globs []string, rel string) bool {
	for _, g := range globs {
		if ok, err := doublestar.Match(g, rel); err == nil && ok {
			return true
		}
	}

	return false
}
