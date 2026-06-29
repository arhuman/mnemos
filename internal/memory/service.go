// Package memory is the verb layer of mnemos: one place that owns the behavior
// behind every memory operation (search, read, context, list, remember, okfy,
// forget, move). The CLI commands and the MCP tools are thin adapters over it —
// they parse arguments or JSON, call a Service method, and format the result —
// so the two surfaces cannot drift in semantics, gating, or option construction.
//
// A Service is built from the same primitives an *app.App exposes (the open
// database, the loaded config, the OKF tree root, a secret scanner, and a
// logger). It deliberately does not build a search retriever: the CLI selects a
// retriever per command (lexical or hybrid via --semantic) while the MCP server
// builds one for its lifetime, so the retriever is passed in to Search/Context.
package memory

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	"github.com/arhuman/mnemos/internal/config"
	"github.com/arhuman/mnemos/internal/model"
	"github.com/arhuman/mnemos/internal/search"
	"github.com/arhuman/mnemos/internal/security"
	"github.com/arhuman/mnemos/internal/storage"
)

// Service owns the memory verbs over an open store, the loaded configuration,
// the OKF tree root, and a secret scanner. The same value backs both surfaces.
type Service struct {
	db           *sql.DB
	cfg          *config.Config
	treeRoot     string
	scanner      security.SecretScanner
	logger       *slog.Logger
	defaultLimit int
}

// New builds a Service. cfg supplies the write/capture/indexing settings and the
// gate flags read by the write verbs; treeRoot confines every caller-supplied
// path; scanner screens captured/converted text for secrets (a nil scanner
// defaults to a regex scanner so the write verbs are always guarded); logger
// writes diagnostics. The configured [search].default_limit is applied by
// Search/Context when a caller leaves limit unset.
func New(db *sql.DB, cfg *config.Config, treeRoot string, scanner security.SecretScanner, logger *slog.Logger) *Service {
	if cfg == nil {
		cfg = &config.Config{}
	}
	if scanner == nil {
		scanner = security.NewRegexScanner()
	}
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	defaultLimit := cfg.Search.DefaultLimit
	if defaultLimit <= 0 {
		defaultLimit = 1
	}

	return &Service{
		db:           db,
		cfg:          cfg,
		treeRoot:     treeRoot,
		scanner:      scanner,
		logger:       logger,
		defaultLimit: defaultLimit,
	}
}

// resolveLimit returns the caller-supplied limit, or the configured default when
// limit is unset (zero or negative).
func (s *Service) resolveLimit(limit int) int {
	if limit <= 0 {
		return s.defaultLimit
	}

	return limit
}

// Search runs q through r, applying the configured default limit when q.Limit is
// unset. r is the retriever the surface chose (lexical FTS, or hybrid
// lexical+vector); the service does not pick one.
func (s *Service) Search(ctx context.Context, r search.Retriever, q search.Query) ([]model.Result, error) {
	q.Limit = s.resolveLimit(q.Limit)
	results, err := r.Search(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("memory: search: %w", err)
	}

	return results, nil
}

// ContextBlock is one retrieved passage ready to inject into an LLM prompt:
// Source cites it as "uri:start-end" and Content is the full chunk text.
type ContextBlock struct {
	Source  string
	Content string
}

// Context runs q through r and batch-loads each hit's full chunk content,
// returning LLM-ready blocks in rank order. The retriever only returns a
// highlighted snippet, so context fetches the whole chunk in one query (avoiding
// the per-result N+1). A chunk that vanished between search and fetch falls back
// to its snippet so the block still carries something.
func (s *Service) Context(ctx context.Context, r search.Retriever, q search.Query) ([]ContextBlock, error) {
	results, err := s.Search(ctx, r, q)
	if err != nil {
		return nil, err
	}

	ids := make([]string, len(results))
	for i, res := range results {
		ids[i] = res.ID
	}
	contents, err := storage.GetChunkContentsByIDs(ctx, s.db, ids)
	if err != nil {
		return nil, fmt.Errorf("memory: context chunks: %w", err)
	}

	blocks := make([]ContextBlock, 0, len(results))
	for _, res := range results {
		content, ok := contents[res.ID]
		if !ok {
			content = res.Snippet
		}
		blocks = append(blocks, ContextBlock{
			Source:  fmt.Sprintf("%s:%d-%d", res.URI, res.StartLine, res.EndLine),
			Content: content,
		})
	}

	return blocks, nil
}
