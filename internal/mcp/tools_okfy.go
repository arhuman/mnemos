package mcp

import (
	"context"
	"errors"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/arhuman/mnemos/internal/memory"
)

// okfyInput is the mnemos.okfy request: convert an existing plain .txt or .md
// file in the tree into an OKF document at out and index it, leaving the source
// intact. source and out are tree-root-relative paths confined to the tree.
type okfyInput struct {
	Source     string   `json:"source"               jsonschema:"the tree-root-relative path of the .txt or .md file to convert"`
	Out        string   `json:"out,omitempty"        jsonschema:"the tree-root-relative output path (must end in .md); defaults to the source path with a .md extension; must not equal the source"`
	Collection string   `json:"collection,omitempty" jsonschema:"the collection to index the OKF document under (defaults to default)"`
	Type       string   `json:"type,omitempty"       jsonschema:"the OKF note type recorded in the frontmatter (defaults to document)"`
	Tags       []string `json:"tags,omitempty"       jsonschema:"optional tags recorded in the OKF frontmatter"`
	Force      bool     `json:"force,omitempty"      jsonschema:"overwrite the output file if it already exists"`
}

// okfyOutput is the mnemos.okfy response: the OKF document's tree-root-relative
// uri (an immediate citation), the document id, and the number of chunks indexed.
type okfyOutput struct {
	URI        string `json:"uri"`
	DocumentID string `json:"document_id"`
	Chunks     int    `json:"chunks"`
}

// registerOkfy wires mnemos.okfy. It is called only when [mcp].allow_write is
// true (it creates and indexes a file and never deletes), so a disabled write
// surface is never advertised in tools/list (least capability).
func (s *Server) registerOkfy() {
	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name:        "mnemos.okfy",
		Description: "Convert an existing plain .txt or .md file in the tree into an OKF document and index it, leaving the source intact, returning an immediate citation.",
	}, s.handleOkfy)
}

func (s *Server) handleOkfy(ctx context.Context, _ *mcpsdk.CallToolRequest, in okfyInput) (*mcpsdk.CallToolResult, okfyOutput, error) {
	if err := ctx.Err(); err != nil {
		return nil, okfyOutput{}, err
	}

	// Defensive re-check: the tool is only registered when allow_write is true,
	// but the handler refuses regardless so the gate cannot be bypassed. (Okfy is
	// un-gated in the service — the local CLI runs it freely — so the MCP write
	// gate is enforced here, at the remote boundary.)
	if !s.cfg.MCP.AllowWrite {
		return nil, okfyOutput{}, errors.New("write disabled: set [mcp].allow_write=true")
	}

	res, err := s.svc.Okfy(ctx, memory.OkfyInput{
		Source:     in.Source,
		Out:        in.Out,
		Collection: in.Collection,
		Type:       in.Type,
		Tags:       in.Tags,
		Force:      in.Force,
	})
	if err != nil {
		return nil, okfyOutput{}, err
	}

	s.logger.Info("okfy converted file", "source", res.SourceURI, "uri", res.URI, "chunks", res.Chunks)

	return nil, okfyOutput{URI: res.URI, DocumentID: res.DocumentID, Chunks: res.Chunks}, nil
}
