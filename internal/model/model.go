// Package model holds the core value types shared across the ingestion
// pipeline: the source descriptor, the parsed-document representation, and the
// persistence aggregates (Document, Chunk, Link). Keeping them in one upstream
// package lets parse/chunk/storage depend on a common vocabulary without
// importing one another sideways.
package model

import "strings"

// LastHeading returns the final segment of an "A > B > C" heading path. An empty
// path yields "". It is the shared helper used to derive a display label (CLI
// citations, MCP result titles) from a chunk's heading path.
func LastHeading(headingPath string) string {
	if headingPath == "" {
		return ""
	}
	if i := strings.LastIndex(headingPath, ">"); i >= 0 {
		return strings.TrimSpace(headingPath[i+1:])
	}

	return strings.TrimSpace(headingPath)
}

// Source describes a single file handed to a Parser. AbsPath is the absolute
// filesystem path used to read bytes; URI is the scan-root-relative path stored
// as documents.uri (the stable, portable identifier). Collection groups
// documents and participates in the deterministic document ID.
type Source struct {
	AbsPath     string
	URI         string
	Collection  string
	Content     []byte
	ContentHash string
	// ModTime is the file mtime in RFC3339, used as modified_at unless the
	// frontmatter supplies a timestamp fallback.
	ModTime string
}

// Section is a contiguous region of a parsed document with a heading path and a
// 1-based inclusive line range. Markdown parsing emits one Section per heading
// scope; the text/code parsers emit a single whole-document Section.
type Section struct {
	HeadingPath string
	StartLine   int
	EndLine     int
	Content     string
}

// ParsedDoc is the normalized output of a Parser: title, raw frontmatter, the
// ordered sections that chunkers consume, and the outbound link URIs. Kind lets
// chunkers dispatch without re-sniffing the extension.
type ParsedDoc struct {
	Title       string
	Frontmatter map[string]any
	// FrontmatterJSON is the raw frontmatter re-encoded as JSON for
	// documents.frontmatter_json. Empty when there is no frontmatter.
	FrontmatterJSON string
	DocType         string
	Tags            []string
	// ModifiedAt overrides Source.ModTime when the frontmatter carries a
	// timestamp. Empty means "use the file mtime".
	ModifiedAt string
	Sections   []Section
	Links      []string
	Kind       Kind
	// StructureOnly marks documents (e.g. OKF index.md) that should yield a
	// documents row but no chunks, FTS, or links.
	StructureOnly bool
	// Metadata is stashed per-document context (e.g. the frontmatter
	// `resource` key) propagated into chunk metadata_json.
	Metadata map[string]any
}

// Kind identifies the parser/chunker family for a document.
type Kind int

const (
	// KindText is the plain-text fallback family.
	KindText Kind = iota
	// KindMarkdown is the goldmark-backed markdown family.
	KindMarkdown
	// KindGoCode is the go/parser-backed Go source family.
	KindGoCode
)

// Document is a persisted documents row.
type Document struct {
	ID              string
	URI             string
	Collection      string
	ContentHash     string
	Title           string
	MimeType        string
	SizeBytes       int64
	ModifiedAt      string
	IndexedAt       string
	FrontmatterJSON string
}

// Chunk is a persisted chunks row. StartLine/EndLine are 1-based inclusive.
type Chunk struct {
	ID           string
	DocumentID   string
	Ordinal      int
	HeadingPath  string
	Content      string
	Tags         string
	DocType      string
	TokenCount   int
	StartLine    int
	EndLine      int
	MetadataJSON string
}

// Link is a persisted links edge. DstDoc is a plain URI string; the target need
// not be ingested, so there is no foreign key on it.
type Link struct {
	SrcDoc string
	DstDoc string
}

// Result is one ranked retrieval hit: the chunk that matched, its owning
// document's uri/collection, the heading path and 1-based line range for
// citation, a highlighted snippet, and the final relevance score (higher is
// better). It is the value a Retriever returns and the CLI/MCP layers render.
type Result struct {
	ID          string  `json:"id"`
	DocumentID  string  `json:"document_id"`
	URI         string  `json:"uri"`
	Collection  string  `json:"collection"`
	HeadingPath string  `json:"heading_path"`
	StartLine   int     `json:"start_line"`
	EndLine     int     `json:"end_line"`
	Snippet     string  `json:"snippet"`
	Score       float64 `json:"score"`
}
