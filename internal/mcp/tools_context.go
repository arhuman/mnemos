package mcp

import (
	"context"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/arhuman/mnemos/internal/memory"
	"github.com/arhuman/mnemos/internal/search"
)

// contextInput is the mnemos.context request: the mnemos.search filters plus
// shaping controls (grouping and size budget) specific to the context tool.
type contextInput struct {
	Query      string `json:"query"                jsonschema:"the search query"`
	Collection string `json:"collection,omitempty" jsonschema:"restrict results to this collection"`
	Path       string `json:"path,omitempty"       jsonschema:"restrict to documents whose uri starts with this prefix"`
	Type       string `json:"type,omitempty"       jsonschema:"restrict to a file extension, e.g. md"`
	Since      string `json:"since,omitempty"      jsonschema:"restrict to documents modified at or after this RFC3339 timestamp"`
	Limit      int    `json:"limit,omitempty"      jsonschema:"maximum number of context blocks (defaults to the configured search limit)"`
	GroupBy    string `json:"group_by,omitempty"   jsonschema:"how to group blocks: 'document' (default) returns the best chunk per document with its title; 'chunk' returns every matching chunk"`
	MaxChars   int    `json:"max_chars,omitempty"  jsonschema:"cap each block's content to this many characters (0 = no limit)"`
	MaxTokens  int    `json:"max_tokens,omitempty" jsonschema:"cap each block's content to roughly this many tokens (~4 chars each; 0 = no limit)"`
}

// contextBlock is one retrieved passage ready to inject into an LLM prompt:
// Source cites it as "uri:start-end", Title/ModifiedAt label its document, and
// Content is the chunk text. Truncated is set when a size budget clipped it.
type contextBlock struct {
	Source     string `json:"source"`
	Title      string `json:"title,omitempty"`
	Collection string `json:"collection,omitempty"`
	ModifiedAt string `json:"modified_at,omitempty"`
	Content    string `json:"content"`
	Truncated  bool   `json:"truncated,omitempty"`
}

// contextOutput echoes the query and returns the top-k context blocks.
type contextOutput struct {
	Query   string         `json:"query"`
	Context []contextBlock `json:"context"`
}

// registerContext wires mnemos.context to the FTS engine plus a chunk-content
// fetch.
func (s *Server) registerContext() {
	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name:        "mnemos.context",
		Description: "Search and return the matching chunks' full content as LLM-ready context blocks.",
	}, s.handleContext)
}

func (s *Server) handleContext(ctx context.Context, _ *mcpsdk.CallToolRequest, in contextInput) (*mcpsdk.CallToolResult, any, error) {
	blocks, err := s.svc.ContextWithOptions(ctx, s.retriever, search.Query{
		Text:          in.Query,
		Collection:    in.Collection,
		PathPrefix:    in.Path,
		FileType:      in.Type,
		ModifiedSince: in.Since,
		Limit:         in.Limit,
	}, memory.ContextOptions{
		// Grouping and the relevance cliff are the tool's defaults; group_by=chunk
		// (or none) opts back into the flat per-chunk listing.
		GroupByDocument: in.GroupBy != "chunk" && in.GroupBy != "none",
		Cliff:           true,
		MaxChars:        in.MaxChars,
		MaxTokens:       in.MaxTokens,
	})
	if err != nil {
		return nil, nil, err
	}

	out := contextOutput{Query: in.Query, Context: make([]contextBlock, 0, len(blocks))}
	for _, b := range blocks {
		out.Context = append(out.Context, contextBlock{
			Source:     b.Source,
			Title:      b.Title,
			Collection: b.Collection,
			ModifiedAt: b.ModifiedAt,
			Content:    b.Content,
			Truncated:  b.Truncated,
		})
	}

	return s.result(out)
}
