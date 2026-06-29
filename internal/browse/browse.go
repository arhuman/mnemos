// Package browse lists the OKF tree by walking it on disk and annotating each
// file with the index metadata mnemos holds for it. The disk walk is the
// source of truth for what exists (so not-yet-indexed files are visible); the
// index supplies title/type/tags/collection and the "indexed" flag. It adds no
// retrieval logic of its own — it reuses the ingest scanner predicate and the
// storage document lister.
package browse

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"github.com/arhuman/mnemos/internal/ingest"
	"github.com/arhuman/mnemos/internal/storage"
)

// Entry is one file in the OKF tree with its index annotation. Collection,
// Title, Type, Tags, and ModifiedAt are populated only when the file is indexed.
type Entry struct {
	URI        string   `json:"uri"`
	Indexed    bool     `json:"indexed"`
	Collection string   `json:"collection,omitempty"`
	Title      string   `json:"title,omitempty"`
	Type       string   `json:"type,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	ModifiedAt string   `json:"modified_at,omitempty"`
	SizeBytes  int64    `json:"size_bytes"`
}

// Options narrows a List call. Empty string fields are ignored. All includes
// every file on disk (not just indexable ones); IndexedOnly / UnindexedOnly
// restrict to files that are / are not in the index.
type Options struct {
	Collection    string
	PathPrefix    string
	FileType      string
	All           bool
	IndexedOnly   bool
	UnindexedOnly bool
	Limit         int
}

// List walks treeRoot and returns entries sorted by uri, annotated from the
// index. include/exclude/secExclude are the indexing globs (the same sets the
// ingest scanner uses); a file is listed when Options.All is set or it matches
// those globs. The internal .mnemos directory is always skipped.
func List(ctx context.Context, db *sql.DB, treeRoot string, include, exclude, secExclude []string, o Options) ([]Entry, error) {
	// Push the metadata filters into SQL so the annotation map holds only the
	// rows we may actually need, rather than materializing the whole documents
	// table. Limit is deliberately not pushed down: the final entries come from
	// the disk walk (which also surfaces un-indexed files), so the cap is applied
	// after the walk. The prefix here is the coarse LIKE form; keep() applies the
	// precise, segment-aware prefix to every walked entry.
	docs, err := storage.ListDocuments(ctx, db, storage.ListFilter{
		Collection: o.Collection,
		PathPrefix: o.PathPrefix,
		FileType:   o.FileType,
	})
	if err != nil {
		return nil, fmt.Errorf("browse: list documents: %w", err)
	}
	byURI := make(map[string]storage.DocumentRow, len(docs))
	for _, d := range docs {
		byURI[d.URI] = d
	}

	walkRoot, ok := walkRootFor(treeRoot, o.PathPrefix)
	if !ok {
		// The prefix escapes the tree root (e.g. "../"). Such a prefix can never
		// name an in-tree file, so refuse it and return nothing rather than walking
		// outside the confined tree and leaking file metadata from beyond the root.
		return []Entry{}, nil
	}

	var entries []Entry
	walkErr := filepath.WalkDir(walkRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return dirAction(treeRoot, walkRoot, path, o.All, exclude, secExclude)
		}
		e, keep, ferr := entryForFile(treeRoot, path, byURI, include, exclude, secExclude, o)
		if ferr != nil {
			return ferr
		}
		if keep {
			entries = append(entries, e)
		}

		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("browse: walk %q: %w", walkRoot, walkErr)
	}

	slices.SortFunc(entries, func(a, b Entry) int { return strings.Compare(a.URI, b.URI) })
	if o.Limit > 0 && len(entries) > o.Limit {
		entries = entries[:o.Limit]
	}

	return entries, nil
}

// walkRootFor narrows the walk to PathPrefix when that prefix names an in-tree
// directory. It returns ok=false when the prefix escapes the tree root so List
// can refuse it. A non-directory in-tree prefix (a file path or a missing name)
// leaves the walk at treeRoot; keep() applies the precise, segment-aware match
// per entry.
func walkRootFor(treeRoot, prefix string) (root string, ok bool) {
	if prefix == "" {
		return treeRoot, true
	}
	cand, within, dir := resolvePrefix(treeRoot, prefix)
	if !within {
		return "", false
	}
	if dir {
		return cand, true
	}

	return treeRoot, true
}

// dirAction decides whether the walk descends into directory path. It prunes the
// internal .mnemos dir always, and (unless all is set) any directory an exclude
// glob covers wholesale (e.g. ".git/**", "node_modules/**"), so the walk never
// descends huge irrelevant subtrees.
func dirAction(treeRoot, walkRoot, path string, all bool, exclude, secExclude []string) error {
	if path == walkRoot {
		return nil
	}
	rel := filepath.ToSlash(mustRel(treeRoot, path))
	if rel == ".mnemos" {
		return fs.SkipDir
	}
	if !all && dirPruned(rel, exclude, secExclude) {
		return fs.SkipDir
	}

	return nil
}

// entryForFile builds the annotated Entry for a walked file, returning keep=false
// when the file is filtered out: outside the tree root (defense in depth — with a
// confined walkRoot and no symlink-following this cannot fire, but it keeps the
// containment invariant local to the leak-prone code), the internal .mnemos dir,
// not matched by the indexing globs (unless o.All), or rejected by keep().
func entryForFile(treeRoot, path string, byURI map[string]storage.DocumentRow, include, exclude, secExclude []string, o Options) (Entry, bool, error) {
	rel, err := filepath.Rel(treeRoot, path)
	if err != nil {
		return Entry{}, false, fmt.Errorf("browse: rel %q: %w", path, err)
	}
	uri := filepath.ToSlash(rel)
	if uri == ".." || strings.HasPrefix(uri, "../") {
		return Entry{}, false, nil
	}
	if uri == ".mnemos" || strings.HasPrefix(uri, ".mnemos/") {
		return Entry{}, false, nil
	}
	if !o.All && !ingest.Match(uri, include, exclude, secExclude) {
		return Entry{}, false, nil
	}

	e := Entry{URI: uri}
	if row, ok := byURI[uri]; ok {
		e.Indexed = true
		e.Collection = row.Collection
		e.Title = row.Title
		e.ModifiedAt = row.ModifiedAt
		e.SizeBytes = row.SizeBytes
		e.Type, e.Tags = metaFromFrontmatter(row.FrontmatterJSON)
	}
	if !keep(e, o) {
		return Entry{}, false, nil
	}

	return e, true, nil
}

// keep applies the post-walk filters (path prefix, file type, collection, and
// indexed/un-indexed selectors) to a single entry.
func keep(e Entry, o Options) bool {
	if o.PathPrefix != "" && !pathMatches(e.URI, o.PathPrefix) {
		return false
	}
	if o.FileType != "" && !strings.HasSuffix(e.URI, "."+o.FileType) {
		return false
	}
	if o.Collection != "" && e.Collection != o.Collection {
		return false
	}
	if o.IndexedOnly && !e.Indexed {
		return false
	}
	if o.UnindexedOnly && e.Indexed {
		return false
	}

	return true
}

// dirPruned reports whether a directory (slash-relative path rel) is covered
// wholesale by an exclude glob of the form "<base>/**" — e.g. ".git/**",
// "node_modules/**", "**/secrets/**". Such a directory can contain no indexable
// file, so the walk can skip descending into it entirely. Only "/**"-suffixed
// patterns prune a directory; file-shaped patterns (e.g. "**/*.tmp", "**/.env")
// do not, since they exclude individual files rather than whole trees.
func dirPruned(rel string, globSets ...[]string) bool {
	for _, set := range globSets {
		for _, g := range set {
			base, ok := strings.CutSuffix(g, "/**")
			if !ok {
				continue
			}
			if m, err := doublestar.Match(base, rel); err == nil && m {
				return true
			}
		}
	}

	return false
}

// mustRel returns the slash-agnostic relative path of target under base. The
// walk only ever passes paths under treeRoot, so filepath.Rel cannot fail; on
// the impossible error it returns target unchanged rather than panicking.
func mustRel(base, target string) string {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return target
	}

	return rel
}

// resolvePrefix interprets a slash-relative PathPrefix against treeRoot. It
// returns the cleaned candidate path, whether that candidate stays within
// treeRoot, and whether it is an existing directory. A prefix that escapes the
// root (e.g. "../" or an absolute path pointing elsewhere) is reported with
// within=false so List can refuse it instead of walking outside the tree.
func resolvePrefix(treeRoot, prefix string) (cand string, within, dir bool) {
	cand = filepath.Join(treeRoot, filepath.FromSlash(prefix))
	rel, err := filepath.Rel(treeRoot, cand)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return cand, false, false
	}

	return cand, true, isDir(cand)
}

// pathMatches reports whether uri is the path prefix itself or sits beneath it
// at a segment boundary. This keeps the prefix path-aware: "adr" matches
// "adr/0001.md" but not "adr-archive/x.md". A trailing slash on the prefix is
// ignored so "adr" and "adr/" behave identically.
func pathMatches(uri, prefix string) bool {
	prefix = strings.TrimSuffix(prefix, "/")

	return uri == prefix || strings.HasPrefix(uri, prefix+"/")
}

// TreeNode is a node in the directory tree built from a flat entry list.
// Directory nodes have Entry == nil; file nodes carry their Entry.
type TreeNode struct {
	Name     string
	IsDir    bool
	Entry    *Entry
	Children []*TreeNode
}

// BuildTree groups entries into a directory tree derived from their uri path
// segments. Children are ordered directories-first then by name. The returned
// root node represents the tree root (its Name is empty).
func BuildTree(entries []Entry) *TreeNode {
	root := &TreeNode{IsDir: true}
	for i := range entries {
		e := entries[i]
		segs := strings.Split(e.URI, "/")
		node := root
		for j, seg := range segs {
			leaf := j == len(segs)-1
			child := findChild(node, seg)
			if child == nil {
				child = &TreeNode{Name: seg, IsDir: !leaf}
				node.Children = append(node.Children, child)
			}
			if leaf {
				child.IsDir = false
				ep := e
				child.Entry = &ep
			}
			node = child
		}
	}
	sortTree(root)

	return root
}

func findChild(n *TreeNode, name string) *TreeNode {
	for _, c := range n.Children {
		if c.Name == name {
			return c
		}
	}

	return nil
}

func sortTree(n *TreeNode) {
	slices.SortFunc(n.Children, func(a, b *TreeNode) int {
		if a.IsDir != b.IsDir {
			if a.IsDir {
				return -1 // directories first
			}

			return 1
		}

		return strings.Compare(a.Name, b.Name)
	})
	for _, c := range n.Children {
		sortTree(c)
	}
}

// metaFromFrontmatter extracts the type and tags from a document's stored
// frontmatter JSON (the raw YAML re-encoded as JSON). Missing or malformed
// frontmatter yields empty values rather than an error.
func metaFromFrontmatter(js string) (docType string, tags []string) {
	if js == "" {
		return "", nil
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(js), &m); err != nil {
		return "", nil
	}
	if t, ok := m["type"].(string); ok {
		docType = t
	}
	switch v := m["tags"].(type) {
	case []any:
		for _, e := range v {
			if s, ok := e.(string); ok {
				tags = append(tags, s)
			}
		}
	case string:
		if v != "" {
			tags = []string{v}
		}
	default:
		// no tags present
	}

	return docType, tags
}

func isDir(p string) bool {
	info, err := os.Stat(p)

	return err == nil && info.IsDir()
}
