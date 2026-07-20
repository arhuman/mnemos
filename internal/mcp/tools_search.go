package mcp

import (
	"context"
	"math"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/arhuman/mnemos/internal/model"
	"github.com/arhuman/mnemos/internal/search"
)

// searchInput is the mnemos.search request. The SDK derives the JSON schema from
// these struct tags; jsonschema descriptions document each field for the LLM.
// The filter fields mirror the `search` CLI flags so both surfaces narrow
// results identically.
type searchInput struct {
	Query      string `json:"query"                jsonschema:"the search query"`
	Collection string `json:"collection,omitempty" jsonschema:"restrict results to this collection"`
	Path       string `json:"path,omitempty"       jsonschema:"restrict to documents whose uri starts with this prefix"`
	Type       string `json:"type,omitempty"       jsonschema:"restrict to a file extension, e.g. md"`
	Since      string `json:"since,omitempty"      jsonschema:"restrict to documents modified at or after this RFC3339 timestamp"`
	Limit      int    `json:"limit,omitempty"      jsonschema:"maximum number of results (defaults to the configured search limit)"`
}

// searchResult is one hit in the mnemos.search response. It mirrors model.Result
// (snake_case tags) and adds a Title the model lacks, derived from the document
// title or the last heading-path segment.
type searchResult struct {
	Title       string  `json:"title"`
	URI         string  `json:"uri"`
	Collection  string  `json:"collection"`
	HeadingPath string  `json:"heading_path"`
	StartLine   int     `json:"start_line"`
	EndLine     int     `json:"end_line"`
	Snippet     string  `json:"snippet"`
	Score       float64 `json:"score"`
}

// searchOutput is the mnemos.search response wrapping the ranked results.
type searchOutput struct {
	Results []searchResult `json:"results"`
}

// registerSearch wires mnemos.search to the FTS engine.
func (s *Server) registerSearch() {
	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name:        "mnemos.search",
		Description: "Search the mnemos index and return ranked, cited results.",
	}, s.handleSearch)
}

func (s *Server) handleSearch(ctx context.Context, _ *mcpsdk.CallToolRequest, in searchInput) (*mcpsdk.CallToolResult, any, error) {
	results, err := s.svc.Search(ctx, s.retriever, search.Query{
		Text:          in.Query,
		Collection:    in.Collection,
		PathPrefix:    in.Path,
		FileType:      in.Type,
		ModifiedSince: in.Since,
		Limit:         in.Limit,
	})
	if err != nil {
		return nil, nil, err
	}

	// Trim the low-relevance tail so the agent is not fed filler hits just to
	// reach the limit.
	results = search.RelevanceCliff(results)

	out := searchOutput{Results: make([]searchResult, 0, len(results))}
	for _, r := range results {
		out.Results = append(out.Results, toSearchResult(r))
	}

	return s.result(out)
}

// toSearchResult maps an internal Result to the MCP output shape, preferring the
// document's stored title and rounding the score to one decimal at the JSON
// boundary (bm25 + boost carries meaningless extra precision).
func toSearchResult(r model.Result) searchResult {
	return searchResult{
		Title:       resultTitle(r),
		URI:         r.URI,
		Collection:  r.Collection,
		HeadingPath: r.HeadingPath,
		StartLine:   r.StartLine,
		EndLine:     r.EndLine,
		Snippet:     r.Snippet,
		Score:       roundScore(r.Score),
	}
}

// resultTitle returns the document's stored title, falling back to the last
// heading-path segment, then the uri, so a hit always carries a human label.
func resultTitle(r model.Result) string {
	if r.Title != "" {
		return r.Title
	}
	if h := model.LastHeading(r.HeadingPath); h != "" {
		return h
	}

	return r.URI
}

// roundScore rounds a relevance score to one decimal place. Search ranks on
// bm25 plus a small heading boost; the trailing precision is noise that only
// inflates the response, so it is trimmed at the wire boundary.
func roundScore(score float64) float64 {
	return math.Round(score*10) / 10
}
