// Package mcp exposes the mnemos retrieval surface over the Model Context
// Protocol. It builds an official-SDK server, registers the read-only search /
// read / context tools, and runs them over a transport (stdio in serve, an
// in-memory pair in tests). All tools reuse the Phase 1 search engine and
// storage; this package adds no retrieval logic of its own.
package mcp

import (
	"context"
	"database/sql"
	"log/slog"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/arhuman/mnemos/internal/config"
	"github.com/arhuman/mnemos/internal/memory"
	"github.com/arhuman/mnemos/internal/search"
	"github.com/arhuman/mnemos/internal/security"
)

// version is reported to MCP clients in the server Implementation.
const version = "0.3.0"

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
	mcp       *mcpsdk.Server
	svc       *memory.Service
	retriever search.Retriever
	logger    *slog.Logger
	cfg       *config.Config
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
		mcp:       mcpsdk.NewServer(&mcpsdk.Implementation{Name: "mnemos", Version: version}, nil),
		svc:       memory.New(db, cfg, treeRoot, scanner, logger),
		retriever: retriever,
		logger:    logger,
		cfg:       cfg,
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
