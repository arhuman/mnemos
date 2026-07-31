package mcp

import (
	"context"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/arhuman/mnemos/internal/memory"
)

// readInput is the mnemos.read request. Exactly one of URI or ChunkID must be
// set: URI reconstructs a whole document from its stored chunks, ChunkID returns
// a single chunk with its citation. Section/Lines scope a document read to a
// subset of its chunks; MaxChars/MaxTokens cap the returned content.
type readInput struct {
	URI           string `json:"uri,omitempty"            jsonschema:"read a whole document by its uri"`
	ChunkID       string `json:"chunk_id,omitempty"       jsonschema:"read a single chunk by its id"`
	Section       string `json:"section,omitempty"        jsonschema:"for a uri read, keep only chunks whose heading path contains this text (case-insensitive)"`
	Lines         string `json:"lines,omitempty"          jsonschema:"for a uri read, keep only chunks overlapping this 1-based inclusive source-line span, e.g. 10-42"`
	MaxChars      int    `json:"max_chars,omitempty"      jsonschema:"cap the returned content to this many characters (0 = no limit)"`
	MaxTokens     int    `json:"max_tokens,omitempty"     jsonschema:"cap the returned content to roughly this many tokens (~4 chars each; 0 = no limit)"`
	FollowLinks   bool   `json:"follow_links,omitempty"   jsonschema:"also return the document's 1-hop link neighbors (outbound links and inbound backlinks)"`
	LinkLimit     int    `json:"link_limit,omitempty"     jsonschema:"with follow_links, max neighbors per direction (0 = server default)"`
	LinkDirection string `json:"link_direction,omitempty" jsonschema:"with follow_links, which edges to follow: outbound, inbound, or both (default both)"`
}

// citation locates a chunk for an agent: the owning document uri, the heading
// path, and the 1-based inclusive line range.
type citation struct {
	URI         string `json:"uri"`
	HeadingPath string `json:"heading_path"`
	StartLine   int    `json:"start_line"`
	EndLine     int    `json:"end_line"`
}

// readOutput is the mnemos.read response. Content is the chunk text or the
// reconstructed document body; the metadata fields locate it. Citation is set
// only for a chunk read. Truncated is set when a size budget clipped Content.
type readOutput struct {
	URI        string            `json:"uri"`
	Collection string            `json:"collection"`
	Title      string            `json:"title"`
	Content    string            `json:"content"`
	Truncated  bool              `json:"truncated,omitempty"`
	Citation   *citation         `json:"citation,omitempty"`
	Neighbors  []relatedNeighbor `json:"neighbors,omitempty"`
}

// registerRead wires mnemos.read to the memory service's read accessors.
func (s *Server) registerRead() {
	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name:        "mnemos.read",
		Description: "Read a precise document (by uri) or a single chunk (by chunk_id).",
	}, s.handleRead)
}

func (s *Server) handleRead(ctx context.Context, _ *mcpsdk.CallToolRequest, in readInput) (*mcpsdk.CallToolResult, any, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	res, err := s.svc.ReadOneOpts(ctx, in.URI, in.ChunkID, memory.ReadOptions{
		Section:       in.Section,
		Lines:         in.Lines,
		MaxChars:      in.MaxChars,
		MaxTokens:     in.MaxTokens,
		FollowLinks:   in.FollowLinks,
		LinkLimit:     in.LinkLimit,
		LinkDirection: memory.Direction(in.LinkDirection),
	})
	if err != nil {
		return nil, nil, err
	}

	return s.result(toReadOutput(res))
}

// toReadOutput maps a service ReadResult to the MCP response shape.
func toReadOutput(r memory.ReadResult) readOutput {
	out := readOutput{
		URI:        r.URI,
		Collection: r.Collection,
		Title:      r.Title,
		Content:    r.Content,
		Truncated:  r.Truncated,
	}
	if r.Citation != nil {
		out.Citation = &citation{
			URI:         r.Citation.URI,
			HeadingPath: r.Citation.HeadingPath,
			StartLine:   r.Citation.StartLine,
			EndLine:     r.Citation.EndLine,
		}
	}
	if len(r.Neighbors) > 0 {
		out.Neighbors = toRelatedNeighbors(r.Neighbors)
	}

	return out
}
