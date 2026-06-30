package security

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// ResolveWithin validates a caller-supplied path against a write root and
// returns the cleaned absolute path and the slash-normalized, root-relative URI
// used as documents.uri. p may be relative to root or absolute; either way the
// resolved location must stay strictly under root.
//
// It is the single guard for every write/delete the agent can trigger
// (mnemos.remember custom path, forget, move). Root is the kb (the URI
// namespace); the index db and models live outside it by construction. It
// rejects, in order:
//   - traversal that escapes root (a "../" prefix after cleaning),
//   - a path that resolves to root itself (not a file),
//   - a path under a reserved ".mnemos/" directory (defense in depth against a
//     nested anchor; the db and models already sit outside the kb),
//   - any URI matching an exclude glob (the [security].exclude set),
//   - symlink escape: the deepest existing ancestor is resolved with
//     EvalSymlinks and re-checked for containment, so a symlink pointing
//     outside root cannot be used as a write target.
func ResolveWithin(root, p string, exclude []string) (abs string, uri string, err error) {
	if strings.TrimSpace(p) == "" {
		return "", "", errors.New("security: empty path")
	}

	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", "", fmt.Errorf("security: resolve root %q: %w", root, err)
	}
	rootAbs = filepath.Clean(rootAbs)

	candidate := p
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(rootAbs, candidate)
	}
	abs = filepath.Clean(candidate)

	rel, err := filepath.Rel(rootAbs, abs)
	if err != nil {
		return "", "", fmt.Errorf("security: relativize %q: %w", p, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", "", fmt.Errorf("security: path %q escapes the tree root", p)
	}
	if rel == "." {
		return "", "", fmt.Errorf("security: path %q resolves to the tree root, not a file", p)
	}

	uri = filepath.ToSlash(rel)

	if uri == ".mnemos" || strings.HasPrefix(uri, ".mnemos/") {
		return "", "", fmt.Errorf("security: path %q is inside the internal .mnemos directory", p)
	}
	for _, g := range exclude {
		if ok, matchErr := doublestar.Match(g, uri); matchErr == nil && ok {
			return "", "", fmt.Errorf("security: path %q matches excluded pattern %q", p, g)
		}
	}

	if err := checkSymlinkEscape(rootAbs, abs); err != nil {
		return "", "", err
	}

	return abs, uri, nil
}

// ConfineDir validates that directory path p resolves within root (or is root
// itself) and does not escape via traversal or a symlink. It guards a scan/ingest
// root rather than a single write target, so — unlike ResolveWithin — it permits
// the root itself and does not reject the internal .mnemos directory. It returns
// the cleaned absolute path on success.
func ConfineDir(root, p string) (abs string, err error) {
	if strings.TrimSpace(p) == "" {
		return "", errors.New("security: empty path")
	}

	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("security: resolve root %q: %w", root, err)
	}
	rootAbs = filepath.Clean(rootAbs)

	// A relative path is interpreted against the tree root (like ResolveWithin),
	// not the process cwd, so confinement is independent of where the command ran.
	candidate := p
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(rootAbs, candidate)
	}
	abs = filepath.Clean(candidate)

	rel, err := filepath.Rel(rootAbs, abs)
	if err != nil {
		return "", fmt.Errorf("security: relativize %q: %w", p, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("security: path %q is outside the tree root %q", p, rootAbs)
	}

	if err := checkSymlinkEscape(rootAbs, abs); err != nil {
		return "", err
	}

	return abs, nil
}

// checkSymlinkEscape resolves symlinks on root and on the deepest existing
// ancestor of abs, then verifies the resolved target still sits under the
// resolved root. The non-existing tail (a file being created) cannot contain a
// symlink, so resolving the deepest existing ancestor is sufficient.
func checkSymlinkEscape(rootAbs, abs string) error {
	rootReal, err := filepath.EvalSymlinks(rootAbs)
	if err != nil {
		return fmt.Errorf("security: resolve root symlinks: %w", err)
	}

	existing := deepestExisting(abs)
	resolved, err := filepath.EvalSymlinks(existing)
	if err != nil {
		return fmt.Errorf("security: resolve symlinks for %q: %w", abs, err)
	}

	rel, err := filepath.Rel(rootReal, resolved)
	if err != nil {
		return fmt.Errorf("security: relativize resolved path: %w", err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("security: path resolves via symlink outside the tree root")
	}

	return nil
}

// deepestExisting walks up from p until it finds a path that exists on disk,
// returning it. It always terminates at the filesystem root.
func deepestExisting(p string) string {
	cur := p
	for {
		if _, err := os.Lstat(cur); err == nil {
			return cur
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return cur
		}
		cur = parent
	}
}
