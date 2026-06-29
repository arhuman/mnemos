package mcp

import (
	"context"
	"errors"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// forgetInput is the mnemos.forget request: a tree-root-relative path to the
// OKF file to remove from both disk and the index.
type forgetInput struct {
	Path string `json:"path" jsonschema:"the tree-root-relative path of the file to delete"`
}

// forgetOutput reports the removed file's uri and whether a file was actually
// deleted from disk (false when it was already absent — forget is idempotent).
type forgetOutput struct {
	URI     string `json:"uri"`
	Deleted bool   `json:"deleted"`
}

// registerForget wires mnemos.forget. It is called only when [mcp].allow_delete
// is true, so a disabled delete surface is never advertised in tools/list.
func (s *Server) registerForget() {
	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name:        "mnemos.forget",
		Description: "Delete a file from the OKF tree: it is removed from disk and de-indexed. Idempotent.",
	}, s.handleForget)
}

func (s *Server) handleForget(ctx context.Context, _ *mcpsdk.CallToolRequest, in forgetInput) (*mcpsdk.CallToolResult, forgetOutput, error) {
	if err := ctx.Err(); err != nil {
		return nil, forgetOutput{}, err
	}

	// Defensive re-check: the tool is only registered when allow_delete is true,
	// and the service re-checks too, but the handler refuses regardless so the
	// gate cannot be bypassed.
	if !s.cfg.MCP.AllowDelete {
		return nil, forgetOutput{}, errors.New("delete disabled: set [mcp].allow_delete=true")
	}

	res, err := s.svc.Forget(ctx, in.Path)
	if err != nil {
		return nil, forgetOutput{}, fmt.Errorf("mcp: %w", err)
	}

	return nil, forgetOutput{URI: res.URI, Deleted: res.Deleted}, nil
}
