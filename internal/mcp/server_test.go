package mcp_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/arhuman/mnemos/internal/chunk"
	"github.com/arhuman/mnemos/internal/config"
	"github.com/arhuman/mnemos/internal/ingest"
	"github.com/arhuman/mnemos/internal/mcp"
	"github.com/arhuman/mnemos/internal/search"
	"github.com/arhuman/mnemos/internal/security"
	"github.com/arhuman/mnemos/internal/storage"
)

// bigText builds a many-line plain-text body that forces the windowed chunker to
// emit several overlapping chunks (small target/overlap in the ingest config).
func bigText(lines int) string {
	var b strings.Builder
	for i := 1; i <= lines; i++ {
		_, _ = fmt.Fprintf(&b, "line %d alpha beta gamma delta\n", i)
	}

	return b.String()
}

// ingestCorpus writes a tiny corpus, ingests it with a small chunk budget (to
// guarantee overlapping windows for the big file), and returns the open DB.
func ingestCorpus(t *testing.T) *sql.DB {
	t.Helper()

	src := t.TempDir()
	mustWrite(t, src, "guide.md", "# SCIM Guide\n\nProvisioning users with Entra and SCIM is straightforward.\n")
	mustWrite(t, src, "notes.txt", bigText(60))

	db, err := storage.Open(context.Background(), filepath.Join(t.TempDir(), "mnemos.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, storage.Migrate(db))

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	sum, err := ingest.New(db, logger).Run(context.Background(), ingest.Options{
		Root:       src,
		Collection: "epfl",
		Rules:      ingest.Rules{Include: []string{"**/*.md", "**/*.txt"}},
		Chunking:   chunk.Config{TargetTokens: 20, OverlapTokens: 5},
	})
	require.NoError(t, err)
	require.Equal(t, 2, sum.FilesIngested)
	require.Positive(t, sum.ChunksWritten)

	return db
}

func mustWrite(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(t, os.WriteFile(p, []byte(content), 0o644))
}

// srvCfg bundles the in-memory test server's inputs: the loaded config (which
// NewServer reads write/capture/indexing settings off) and the tree root that
// caller-supplied paths are confined within. It replaces the former
// mcp.WriteConfig, removed when the server began taking *config.Config directly.
type srvCfg struct {
	cfg      *config.Config
	treeRoot string
}

// connect wires an in-memory client to a read-only server backed by db and
// returns the initialized client session.
func connect(t *testing.T, db *sql.DB) *mcpsdk.ClientSession {
	t.Helper()

	return connectWith(t, db, srvCfg{})
}

// connectWith wires an in-memory client to a server backed by db with the given
// configuration, and returns the initialized client session.
func connectWith(t *testing.T, db *sql.DB, sc srvCfg) *mcpsdk.ClientSession {
	t.Helper()
	ctx := context.Background()

	cfg := sc.cfg
	if cfg == nil {
		cfg = &config.Config{}
	}
	if cfg.Search.DefaultLimit == 0 {
		cfg.Search.DefaultLimit = 12
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	engine := search.NewEngine(db, logger)
	srv := mcp.NewServer(db, engine, cfg, sc.treeRoot, security.NewRegexScanner(), logger)

	serverT, clientT := mcpsdk.NewInMemoryTransports()
	// Servers must connect before clients.
	ss, err := srv.Connect(ctx, serverT)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ss.Close() })

	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "v0"}, nil)
	cs, err := client.Connect(ctx, clientT, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cs.Close() })

	return cs
}

// callTool invokes name with args and unmarshals the structured result into out.
func callTool(t *testing.T, cs *mcpsdk.ClientSession, name string, args any, out any) *mcpsdk.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{Name: name, Arguments: args})
	require.NoError(t, err)
	if out != nil && !res.IsError {
		b, err := json.Marshal(res.StructuredContent)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(b, out))
	}

	return res
}

type searchHitJSON struct {
	Title       string  `json:"title"`
	URI         string  `json:"uri"`
	HeadingPath string  `json:"heading_path"`
	StartLine   int     `json:"start_line"`
	EndLine     int     `json:"end_line"`
	Snippet     string  `json:"snippet"`
	Score       float64 `json:"score"`
}

type citationJSON struct {
	URI         string `json:"uri"`
	HeadingPath string `json:"heading_path"`
	StartLine   int    `json:"start_line"`
	EndLine     int    `json:"end_line"`
}

type contextBlockJSON struct {
	Source  string `json:"source"`
	Content string `json:"content"`
}

type searchRefJSON struct {
	URI       string `json:"uri"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
}

func TestSearchTool(t *testing.T) {
	cs := connect(t, ingestCorpus(t))

	var out struct {
		Results []searchHitJSON `json:"results"`
	}
	res := callTool(t, cs, "mnemos.search", map[string]any{"query": "SCIM provisioning Entra"}, &out)
	require.False(t, res.IsError)
	require.NotEmpty(t, out.Results)
	require.Equal(t, "guide.md", out.Results[0].URI)
	require.Equal(t, "SCIM Guide", out.Results[0].Title)
	require.NotZero(t, out.Results[0].EndLine)
}

// TestSearchToolHonorsFilters proves the path/type filters added for CLI/MCP
// parity actually narrow results. The corpus has one .md (guide.md) and one .txt
// (notes.txt); each query below matches only the .txt, so a markdown-only or
// guide-only filter must exclude it.
func TestSearchToolHonorsFilters(t *testing.T) {
	cs := connect(t, ingestCorpus(t))

	type resp struct {
		Results []searchHitJSON `json:"results"`
	}

	// Baseline: the query matches the .txt document.
	var base resp
	res := callTool(t, cs, "mnemos.search", map[string]any{"query": "line alpha beta gamma"}, &base)
	require.False(t, res.IsError)
	require.NotEmpty(t, base.Results)
	for _, r := range base.Results {
		require.Equal(t, "notes.txt", r.URI)
	}

	// type=md excludes the only (.txt) matches.
	var byType resp
	res = callTool(t, cs, "mnemos.search", map[string]any{"query": "line alpha beta gamma", "type": "md"}, &byType)
	require.False(t, res.IsError)
	require.Empty(t, byType.Results, "type=md must exclude the .txt matches")

	// path=guide.md excludes notes.txt too.
	var byPath resp
	res = callTool(t, cs, "mnemos.search", map[string]any{"query": "line alpha beta gamma", "path": "guide.md"}, &byPath)
	require.False(t, res.IsError)
	require.Empty(t, byPath.Results, "path=guide.md must exclude notes.txt matches")
}

// TestContextToolHonorsFilters confirms the same filters thread through the
// context tool, which shares runSearch with search.
func TestContextToolHonorsFilters(t *testing.T) {
	cs := connect(t, ingestCorpus(t))

	var out struct {
		Context []contextBlockJSON `json:"context"`
	}
	res := callTool(t, cs, "mnemos.context", map[string]any{"query": "line alpha beta gamma", "type": "md"}, &out)
	require.False(t, res.IsError)
	require.Empty(t, out.Context, "type=md must exclude the .txt matches from context")
}

func TestReadChunkTool(t *testing.T) {
	db := ingestCorpus(t)
	cs := connect(t, db)

	// Grab a real chunk id from the markdown doc.
	chunks, err := storage.GetChunksByDocURI(context.Background(), db, "guide.md")
	require.NoError(t, err)
	require.NotEmpty(t, chunks)
	want := chunks[0]

	var out struct {
		URI        string        `json:"uri"`
		Collection string        `json:"collection"`
		Title      string        `json:"title"`
		Content    string        `json:"content"`
		Citation   *citationJSON `json:"citation"`
	}
	res := callTool(t, cs, "mnemos.read", map[string]any{"chunk_id": want.ID}, &out)
	require.False(t, res.IsError)
	require.Equal(t, want.Content, out.Content)
	require.Equal(t, "guide.md", out.URI)
	require.Equal(t, "epfl", out.Collection)
	require.NotNil(t, out.Citation)
	require.Equal(t, "guide.md", out.Citation.URI)
	require.Equal(t, want.StartLine, out.Citation.StartLine)
	require.Equal(t, want.EndLine, out.Citation.EndLine)
}

func TestReadDocumentDedupsOverlap(t *testing.T) {
	db := ingestCorpus(t)
	cs := connect(t, db)

	// The text file is chunked into overlapping windows; reading by uri must
	// reconstruct each source line exactly once.
	chunks, err := storage.GetChunksByDocURI(context.Background(), db, "notes.txt")
	require.NoError(t, err)
	require.Greater(t, len(chunks), 1, "expected overlapping windows")

	var out struct {
		Content string `json:"content"`
	}
	res := callTool(t, cs, "mnemos.read", map[string]any{"uri": "notes.txt"}, &out)
	require.False(t, res.IsError)

	// Every original line present exactly once, in order, no duplicates.
	for i := 1; i <= 60; i++ {
		line := fmt.Sprintf("line %d alpha beta gamma delta", i)
		require.Equal(t, 1, strings.Count(out.Content, line+"\n")+boolToInt(strings.HasSuffix(out.Content, line)),
			"line %d should appear exactly once", i)
	}
	// Reconstructed line count matches the source (no duplicated overlap lines).
	require.Equal(t, 60, len(nonEmptyLines(out.Content)))
}

func boolToInt(b bool) int {
	if b {
		return 1
	}

	return 0
}

func nonEmptyLines(s string) []string {
	var out []string
	for l := range strings.SplitSeq(s, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}

	return out
}

func TestContextTool(t *testing.T) {
	db := ingestCorpus(t)
	cs := connect(t, db)

	var out struct {
		Query   string             `json:"query"`
		Context []contextBlockJSON `json:"context"`
	}
	res := callTool(t, cs, "mnemos.context", map[string]any{"query": "SCIM provisioning Entra"}, &out)
	require.False(t, res.IsError)
	require.Equal(t, "SCIM provisioning Entra", out.Query)
	require.NotEmpty(t, out.Context)

	top := out.Context[0]
	require.Regexp(t, `^guide\.md:\d+-\d+$`, top.Source)
	// Context returns full chunk content, not a snippet.
	require.Contains(t, top.Content, "Provisioning")

	// Cross-check: the content equals the stored chunk, not a highlighted snippet.
	chunks, err := storage.GetChunksByDocURI(context.Background(), db, "guide.md")
	require.NoError(t, err)
	require.Equal(t, chunks[0].Content, top.Content)
}

func TestContextBatchPreservesSearchOrder(t *testing.T) {
	db := ingestCorpus(t)
	cs := connect(t, db)

	// A query broad enough to return several ranked results across both files.
	const query = "line alpha beta gamma"

	var searchResp struct {
		Results []searchRefJSON `json:"results"`
	}
	sres := callTool(t, cs, "mnemos.search", map[string]any{"query": query}, &searchResp)
	require.False(t, sres.IsError)
	require.Greater(t, len(searchResp.Results), 1, "need multiple results to test ordering")

	var out struct {
		Context []contextBlockJSON `json:"context"`
	}
	cres := callTool(t, cs, "mnemos.context", map[string]any{"query": query}, &out)
	require.False(t, cres.IsError)

	// One block per search result, in the same rank order, each with content.
	require.Len(t, out.Context, len(searchResp.Results))
	for i, r := range searchResp.Results {
		want := fmt.Sprintf("%s:%d-%d", r.URI, r.StartLine, r.EndLine)
		require.Equal(t, want, out.Context[i].Source, "block %d source/order mismatch", i)
		require.NotEmpty(t, out.Context[i].Content, "block %d must carry content", i)
	}
}

func TestReadBadInputIsToolError(t *testing.T) {
	cs := connect(t, ingestCorpus(t))

	cases := []struct {
		name string
		args map[string]any
	}{
		{"neither", make(map[string]any)},
		{"both", map[string]any{"uri": "guide.md", "chunk_id": "x"}},
		{"unknown chunk", map[string]any{"chunk_id": "does-not-exist"}},
		{"unknown uri", map[string]any{"uri": "missing.md"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := callTool(t, cs, "mnemos.read", tc.args, nil)
			require.True(t, res.IsError, "expected a tool error, not a crash")
		})
	}
}
