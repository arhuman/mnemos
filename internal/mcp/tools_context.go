package mcp

import (
	"context"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/arhuman/mnemos/internal/search"
)

// contextInput is the mnemos.context request: the same shape as mnemos.search.
type contextInput struct {
	Query      string `json:"query"                jsonschema:"the search query"`
	Collection string `json:"collection,omitempty" jsonschema:"restrict results to this collection"`
	Path       string `json:"path,omitempty"       jsonschema:"restrict to documents whose uri starts with this prefix"`
	Type       string `json:"type,omitempty"       jsonschema:"restrict to a file extension, e.g. md"`
	Since      string `json:"since,omitempty"      jsonschema:"restrict to documents modified at or after this RFC3339 timestamp"`
	Limit      int    `json:"limit,omitempty"      jsonschema:"maximum number of context blocks (defaults to the configured search limit)"`
}

// contextBlock is one retrieved passage ready to inject into an LLM prompt:
// Source cites it as "uri:start-end" and Content is the full chunk text.
type contextBlock struct {
	Source  string `json:"source"`
	Content string `json:"content"`
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

func (s *Server) handleContext(ctx context.Context, _ *mcpsdk.CallToolRequest, in contextInput) (*mcpsdk.CallToolResult, contextOutput, error) {
	blocks, err := s.svc.Context(ctx, s.retriever, search.Query{
		Text:          in.Query,
		Collection:    in.Collection,
		PathPrefix:    in.Path,
		FileType:      in.Type,
		ModifiedSince: in.Since,
		Limit:         in.Limit,
	})
	if err != nil {
		return nil, contextOutput{}, err
	}

	out := contextOutput{Query: in.Query, Context: make([]contextBlock, 0, len(blocks))}
	for _, b := range blocks {
		out.Context = append(out.Context, contextBlock{Source: b.Source, Content: b.Content})
	}

	return nil, out, nil
}
