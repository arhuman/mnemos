package mcp

import (
	"context"
	"errors"
	"fmt"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// moveInput is the mnemos.move request: tree-root-relative source and
// destination paths for a file or directory in the OKF tree.
type moveInput struct {
	From string `json:"from" jsonschema:"the tree-root-relative source path (file or directory)"`
	To   string `json:"to"   jsonschema:"the tree-root-relative destination path"`
}

// moveOutput reports the old and new uris and the re-indexed document id. The
// document id changes because it is derived from collection + uri. For a
// directory move, Files is the number of documents re-indexed and DocumentID is
// empty (each file gets its own new id).
type moveOutput struct {
	From          string `json:"from"`
	To            string `json:"to"`
	DocumentID    string `json:"document_id,omitempty"`
	IsDir         bool   `json:"is_dir"`
	Files         int    `json:"files"`
	DanglingLinks int    `json:"dangling_links"`
}

// registerMove wires mnemos.move. It is called only when [mcp].allow_delete is
// true (a move deletes the old index entry), so a disabled surface is never
// advertised in tools/list.
func (s *Server) registerMove() {
	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name:        "mnemos.move",
		Description: "Move a file or directory within the OKF tree: it is renamed on disk and re-indexed under the new path, preserving each document's collection. A directory moves its whole subtree and re-indexes every document under it.",
	}, s.handleMove)
}

func (s *Server) handleMove(ctx context.Context, _ *mcpsdk.CallToolRequest, in moveInput) (*mcpsdk.CallToolResult, moveOutput, error) {
	if err := ctx.Err(); err != nil {
		return nil, moveOutput{}, err
	}

	// Defensive re-check: the tool is only registered when allow_delete is true,
	// and the service re-checks too, but the handler refuses regardless so the
	// gate cannot be bypassed.
	if !s.cfg.MCP.AllowDelete {
		return nil, moveOutput{}, errors.New("delete disabled: set [mcp].allow_delete=true")
	}

	res, err := s.svc.Move(ctx, in.From, in.To)
	if err != nil {
		return nil, moveOutput{}, fmt.Errorf("mcp: %w", err)
	}

	mv := res.Result
	out := moveOutput{From: res.From, To: res.To, IsDir: mv.IsDir, Files: len(mv.Entries), DanglingLinks: mv.DanglingLinks}
	if !mv.IsDir && len(mv.Entries) > 0 {
		out.DocumentID = mv.Entries[0].DocumentID
	}

	return nil, out, nil
}
