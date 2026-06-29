// Package eval implements the OKF held-out retrieval evaluation: it derives
// example-query/expected-document pairs from a bundle, builds a held-out copy of
// the corpus with those queries stripped from their host chunks, ingests the
// copy into an ephemeral database, runs each query through the Retriever, and
// computes doc-level retrieval metrics against a versioned baseline.
package eval

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// md is the shared goldmark instance used to locate fenced code blocks. It
// mirrors the parser configuration used by the ingest pipeline (CommonMark
// defaults) so the AST view of a document matches what ingestion sees.
var md = goldmark.New()

// pair is one auto-derived ground-truth example: the example-query text lifted
// from a fenced code block, the uri of the host document that should be
// retrieved, and the exact block text so the held-out copy can strip it and the
// exact-chunk metric can locate the originating chunk.
type pair struct {
	// queryText is the fenced code block's content used as the search query.
	queryText string
	// expectedURI is the bundle-relative uri of the host file (the retrieval
	// target). It matches documents.uri after ingestion.
	expectedURI string
	// hostFile is the absolute path of the source markdown file.
	hostFile string
	// blockText is the verbatim block content (without the ``` fences) removed
	// from the held-out copy; used to map back to the originating chunk lines.
	blockText string
	// blockStartLine is the 1-based line of the first content line of the block
	// in the original file, used by the exact-chunk metric.
	blockStartLine int
}

// extractPairs walks the bundle directory and derives one pair per fenced code
// block found in each concept file. The deterministic V0 ground-truth rule:
//
//	a fenced code block (```…```) inside concept-X.md is an example query whose
//	expected retrieval target is document X.
//
// index.md files are skipped (OKF structure-only), as are empty/whitespace
// blocks. Only markdown files are considered. uris are computed relative to the
// bundle root with forward slashes, matching the ingest scanner.
func extractPairs(bundle string) ([]pair, error) {
	var pairs []pair
	walkErr := filepath.WalkDir(bundle, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !isMarkdown(p) || strings.EqualFold(filepath.Base(p), "index.md") {
			return nil
		}
		rel, err := filepath.Rel(bundle, p)
		if err != nil {
			return err
		}
		uri := filepath.ToSlash(rel)

		content, err := os.ReadFile(p) //nolint:gosec // p comes from walking the eval bundle dir
		if err != nil {
			return err
		}
		for _, b := range fencedBlocks(content) {
			if strings.TrimSpace(b.text) == "" {
				continue
			}
			pairs = append(pairs, pair{
				queryText:      strings.TrimSpace(b.text),
				expectedURI:    uri,
				hostFile:       p,
				blockText:      b.text,
				blockStartLine: b.startLine,
			})
		}

		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}

	return pairs, nil
}

// fencedCodeBlock is a located fenced code block: its raw inner text (the lines
// between the fences) and the 1-based line where that inner text starts.
type fencedCodeBlock struct {
	text      string
	startLine int
}

// fencedBlocks returns every fenced code block in content with its inner text
// and starting line. goldmark gives reliable block boundaries (so indented or
// commented backticks inside prose don't produce false positives); the line
// number is derived from the byte offset of the block's first content segment.
func fencedBlocks(content []byte) []fencedCodeBlock {
	root := md.Parser().Parse(text.NewReader(content))
	var blocks []fencedCodeBlock
	_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		fcb, ok := n.(*ast.FencedCodeBlock)
		if !ok {
			return ast.WalkContinue, nil
		}
		lines := fcb.Lines()
		if lines == nil || lines.Len() == 0 {
			return ast.WalkContinue, nil
		}
		var b strings.Builder
		for i := 0; i < lines.Len(); i++ {
			seg := lines.At(i)
			_, _ = b.Write(seg.Value(content))
		}
		first := lines.At(0)
		blocks = append(blocks, fencedCodeBlock{
			text:      b.String(),
			startLine: lineAtOffset(content, first.Start),
		})

		return ast.WalkContinue, nil
	})

	return blocks
}

// lineAtOffset returns the 1-based line number that byte offset falls on.
func lineAtOffset(content []byte, offset int) int {
	if offset > len(content) {
		offset = len(content)
	}
	line := 1
	for i := 0; i < offset; i++ {
		if content[i] == '\n' {
			line++
		}
	}

	return line
}

// buildHeldOut copies the bundle into dir and, in each host file, removes the
// exact example-query block text from the body so the answer is no longer
// sitting verbatim in its own chunk. Frontmatter, prose, and headings remain, so
// retrieval must rely on surrounding context. The copy is what gets ingested.
// dir must already exist. The returned path is dir itself (the held-out root).
func buildHeldOut(bundle, dir string, pairs []pair) (string, error) {
	// Group the block texts to strip per host file.
	stripByFile := make(map[string][]string)
	for _, pr := range pairs {
		stripByFile[pr.hostFile] = append(stripByFile[pr.hostFile], pr.blockText)
	}

	walkErr := filepath.WalkDir(bundle, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(bundle, p)
		if err != nil {
			return err
		}
		dst := filepath.Join(dir, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o750)
		}
		content, err := os.ReadFile(p) //nolint:gosec // p comes from walking the eval bundle dir
		if err != nil {
			return err
		}
		if blocks, ok := stripByFile[p]; ok {
			content = stripBlocks(content, blocks)
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
			return err
		}

		return os.WriteFile(dst, content, 0o600) //nolint:gosec // dst is filepath.Join(dir, rel) with rel from filepath.Rel within the walked bundle; no traversal
	})
	if walkErr != nil {
		return "", walkErr
	}

	return dir, nil
}

// stripBlocks removes each block's exact text from content. The block text is
// the fenced body (without fences); removing it leaves the surrounding fences as
// an empty code block, which is harmless and keeps line edits minimal and
// deterministic. Each block is removed once.
func stripBlocks(content []byte, blocks []string) []byte {
	s := string(content)
	for _, b := range blocks {
		if b == "" {
			continue
		}
		s = strings.Replace(s, b, "", 1)
	}

	return []byte(s)
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
