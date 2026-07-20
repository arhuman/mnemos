// Package mcp exposes the mnemos retrieval surface over the Model Context
// Protocol. It builds an official-SDK server, registers the read-only search /
// read / context tools, and runs them over a transport (stdio in serve, an
// in-memory pair in tests). All tools reuse the Phase 1 search engine and
// storage; this package adds no retrieval logic of its own.
package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/arhuman/mnemos/internal/config"
	"github.com/arhuman/mnemos/internal/memory"
	"github.com/arhuman/mnemos/internal/search"
	"github.com/arhuman/mnemos/internal/security"
)

// version is reported to MCP clients in the server Implementation.
const version = "0.3.0"

// resultMode selects how a tool response is placed on the MCP wire. The zero
// value ("") behaves as resultText, so a Server built without an explicit mode
// defaults to text-only.
type resultMode string

const (
	// resultText emits the JSON payload once as a text content block and leaves
	// structuredContent nil. It is the default: it preserves exactly what the
	// client surfaces to the model today while dropping the redundant copy.
	resultText resultMode = "text"
	// resultStructured emits the payload once as structuredContent, no text.
	resultStructured resultMode = "structured"
	// resultBoth emits the payload as both text and structuredContent (the legacy
	// double-emit), for clients that require the structured object.
	resultBoth resultMode = "both"
)

// Server is the MCP adapter over the memory service. The same value backs both
// the stdio serve command and the in-memory test client. Every tool handler is
// thin: it shapes the request, calls a memory.Service method, and formats the
// result — the verb behavior, gating, and option construction live in the
// service, so this surface cannot drift from the CLI.
//
// retriever is the search seam Search/Context run through (held here, not in the
// service, because the CLI picks a retriever per command). cfg is retained only
// for the registration gates and the handler-level defensive re-checks on
// destructive tools.
type Server struct {
	mcp        *mcpsdk.Server
	svc        *memory.Service
	retriever  search.Retriever
	logger     *slog.Logger
	cfg        *config.Config
	resultMode resultMode
}

// NewServer builds the MCP server and registers the read-only tools. db is the
// open store; retriever the search seam (the lexical FTS engine, or a hybrid
// lexical+vector retriever when serve resolves one); cfg the loaded
// configuration (write/capture/indexing settings are read from it, and
// [search].default_limit is applied when a tool omits limit); treeRoot the OKF
// tree root paths are confined within; scanner the secret screen for captured
// text; and logger writes diagnostics to stderr (never stdout, the MCP
// transport). The write tools mnemos.remember/okfy are registered only when
// cfg.MCP.AllowWrite is true, and mnemos.forget/move only when
// cfg.MCP.AllowDelete is true (least capability: a disabled tool is never
// advertised in tools/list).
func NewServer(db *sql.DB, retriever search.Retriever, cfg *config.Config, treeRoot string, scanner security.SecretScanner, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	if cfg == nil {
		cfg = &config.Config{}
	}

	srv := &Server{
		mcp:        mcpsdk.NewServer(&mcpsdk.Implementation{Name: "mnemos", Version: version}, nil),
		svc:        memory.New(db, cfg, treeRoot, scanner, logger),
		retriever:  retriever,
		logger:     logger,
		cfg:        cfg,
		resultMode: resultMode(cfg.MCP.ResultMode),
	}

	srv.registerSearch()
	srv.registerRead()
	srv.registerContext()
	srv.registerList()
	if cfg.MCP.AllowWrite {
		srv.registerRemember()
		srv.registerOkfy()
	}
	if cfg.MCP.AllowDelete {
		srv.registerForget()
		srv.registerMove()
	}

	return srv
}

// Serve connects the server to the given transport and blocks until the session
// ends or ctx is cancelled. serve passes a stdio transport.
func (s *Server) Serve(ctx context.Context, transport mcpsdk.Transport) error {
	s.logger.Info("mcp server starting", "version", version)

	return s.mcp.Run(ctx, transport)
}

// Connect starts a single non-blocking session over transport and returns it.
// serve uses Serve (which blocks); Connect exists for the in-memory test harness
// that needs to drive the client side concurrently.
func (s *Server) Connect(ctx context.Context, transport mcpsdk.Transport) (*mcpsdk.ServerSession, error) {
	return s.mcp.Connect(ctx, transport, nil)
}

// result builds the CallToolResult for a successful tool call. It marshals v to
// JSON once and places it on the wire per the configured mode. All tools return
// through it as (result, nil, nil): because they are registered with Out=any and
// advertise no output schema, a nil Out makes the SDK emit this result untouched
// — no marshal, no text/structuredContent mirroring — so the wire carries the
// payload exactly once (in text mode) instead of the former double-emit.
func (s *Server) result(v any) (*mcpsdk.CallToolResult, any, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, nil, fmt.Errorf("mcp: marshal result: %w", err)
	}

	res := &mcpsdk.CallToolResult{}
	switch s.resultMode {
	case resultStructured:
		res.StructuredContent = json.RawMessage(data)
	case resultBoth:
		res.StructuredContent = json.RawMessage(data)
		res.Content = []mcpsdk.Content{&mcpsdk.TextContent{Text: string(data)}}
	default: // resultText (and the empty zero value)
		res.Content = []mcpsdk.Content{&mcpsdk.TextContent{Text: string(data)}}
	}

	return res, nil, nil
}
