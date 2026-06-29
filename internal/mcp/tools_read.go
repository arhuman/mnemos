package mcp

import (
	"context"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/arhuman/mnemos/internal/memory"
)

// readInput is the mnemos.read request. Exactly one of URI or ChunkID must be
// set: URI reconstructs a whole document from its stored chunks, ChunkID returns
// a single chunk with its citation.
type readInput struct {
	URI     string `json:"uri,omitempty"      jsonschema:"read a whole document by its uri"`
	ChunkID string `json:"chunk_id,omitempty" jsonschema:"read a single chunk by its id"`
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
// only for a chunk read.
type readOutput struct {
	URI        string    `json:"uri"`
	Collection string    `json:"collection"`
	Title      string    `json:"title"`
	Content    string    `json:"content"`
	Citation   *citation `json:"citation,omitempty"`
}

// registerRead wires mnemos.read to the memory service's read accessors.
func (s *Server) registerRead() {
	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name:        "mnemos.read",
		Description: "Read a precise document (by uri) or a single chunk (by chunk_id).",
	}, s.handleRead)
}

func (s *Server) handleRead(ctx context.Context, _ *mcpsdk.CallToolRequest, in readInput) (*mcpsdk.CallToolResult, readOutput, error) {
	if err := ctx.Err(); err != nil {
		return nil, readOutput{}, err
	}

	res, err := s.svc.ReadOne(ctx, in.URI, in.ChunkID)
	if err != nil {
		return nil, readOutput{}, err
	}

	return nil, toReadOutput(res), nil
}

// toReadOutput maps a service ReadResult to the MCP response shape.
func toReadOutput(r memory.ReadResult) readOutput {
	out := readOutput{
		URI:        r.URI,
		Collection: r.Collection,
		Title:      r.Title,
		Content:    r.Content,
	}
	if r.Citation != nil {
		out.Citation = &citation{
			URI:         r.Citation.URI,
			HeadingPath: r.Citation.HeadingPath,
			StartLine:   r.Citation.StartLine,
			EndLine:     r.Citation.EndLine,
		}
	}

	return out
}
