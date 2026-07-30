package mcp

import (
	"context"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/arhuman/mnemos/internal/memory"
)

// relatedInput is the mnemos.related request: the document uri to walk from, the
// edge direction to follow, and a per-direction cap.
type relatedInput struct {
	URI       string `json:"uri"                 jsonschema:"the document uri to find link-graph neighbors of"`
	Direction string `json:"direction,omitempty" jsonschema:"which edges to follow: outbound, inbound, or both (default both)"`
	Limit     int    `json:"limit,omitempty"     jsonschema:"maximum neighbors per direction (0 = server default)"`
}

// relatedNeighbor is one 1-hop neighbor on the wire. Resolved is false for a
// dangling outbound link (the target uri is not indexed), where title/collection
// are empty.
type relatedNeighbor struct {
	URI        string `json:"uri"`
	Title      string `json:"title,omitempty"`
	Collection string `json:"collection,omitempty"`
	Direction  string `json:"direction"`
	Resolved   bool   `json:"resolved"`
}

// relatedOutput is the mnemos.related response: the queried uri and its outbound
// links and inbound backlinks. Both slices are always present (possibly empty).
type relatedOutput struct {
	URI      string            `json:"uri"`
	Outbound []relatedNeighbor `json:"outbound"`
	Inbound  []relatedNeighbor `json:"inbound"`
}

// registerRelated wires mnemos.related to the memory service. It is read-only and
// needs no allow_* gate.
func (s *Server) registerRelated() {
	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name:        "mnemos.related",
		Description: "List the link-graph neighbors of a document: outbound links it contains and inbound backlinks that point at it (1 hop, document-level).",
	}, s.handleRelated)
}

func (s *Server) handleRelated(ctx context.Context, _ *mcpsdk.CallToolRequest, in relatedInput) (*mcpsdk.CallToolResult, any, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}

	res, err := s.svc.Related(ctx, in.URI, memory.Direction(in.Direction), in.Limit)
	if err != nil {
		return nil, nil, err
	}

	return s.result(toRelatedOutput(res))
}

// toRelatedOutput maps a service RelatedResult to the MCP response shape,
// normalizing nil slices to empty so the JSON always carries the two arrays.
func toRelatedOutput(r memory.RelatedResult) relatedOutput {
	return relatedOutput{
		URI:      r.URI,
		Outbound: toRelatedNeighbors(r.Outbound),
		Inbound:  toRelatedNeighbors(r.Inbound),
	}
}

func toRelatedNeighbors(ns []memory.RelatedNeighbor) []relatedNeighbor {
	out := make([]relatedNeighbor, 0, len(ns))
	for _, n := range ns {
		out = append(out, relatedNeighbor{
			URI:        n.URI,
			Title:      n.Title,
			Collection: n.Collection,
			Direction:  n.Direction,
			Resolved:   n.Resolved,
		})
	}

	return out
}
