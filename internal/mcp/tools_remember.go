package mcp

import (
	"context"
	"errors"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/arhuman/mnemos/internal/memory"
)

// rememberInput is the mnemos.remember request. type is a free-form OKF type
// (e.g. idea, document, Task) decided explicitly by the agent; text is the note
// body; collection defaults to "default"; tags are optional frontmatter signals.
type rememberInput struct {
	Type       string   `json:"type"                 jsonschema:"the OKF note type, free-form (e.g. idea, document, Task); must be non-empty"`
	Text       string   `json:"text"                 jsonschema:"the note content to remember (non-empty)"`
	Collection string   `json:"collection,omitempty" jsonschema:"the collection to store the note in (defaults to default)"`
	Tags       []string `json:"tags,omitempty"       jsonschema:"optional tags recorded in the note frontmatter"`
	// Path, when set, is a tree-root-relative target for the note (must end in
	// .md). It is validated to stay within the tree root. When empty the note is
	// auto-named under the capture directory (the original behavior).
	Path string `json:"path,omitempty" jsonschema:"optional tree-root-relative target path for the note (must end in .md); when omitted the note is auto-named under the capture directory"`
}

// rememberOutput is the mnemos.remember response: the stored note's
// project-relative uri (an immediate citation), the document id, the number of
// chunks indexed, and the recorded type. Deferred is true when the note was
// written but not ingested (defer_to_watcher mode): the watcher owns ingestion,
// so document_id is empty and chunks is 0 until the watcher picks the file up.
type rememberOutput struct {
	URI        string `json:"uri"`
	DocumentID string `json:"document_id"`
	Chunks     int    `json:"chunks"`
	Type       string `json:"type"`
	Deferred   bool   `json:"deferred"`
}

// registerRemember wires mnemos.remember. It is called only when
// [mcp].allow_write is true, so a disabled write surface is never advertised in
// tools/list (least capability).
func (s *Server) registerRemember() {
	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name:        "mnemos.remember",
		Description: "Capture a note (any OKF type, e.g. idea/document/Task) into memory: it is written as an OKF markdown file and indexed, returning an immediate citation.",
	}, s.handleRemember)
}

func (s *Server) handleRemember(ctx context.Context, _ *mcpsdk.CallToolRequest, in rememberInput) (*mcpsdk.CallToolResult, rememberOutput, error) {
	// Defensive re-check: the tool is only registered when allow_write is true,
	// and the service re-checks too, but the handler refuses regardless so the
	// gate cannot be bypassed.
	if !s.cfg.MCP.AllowWrite {
		return nil, rememberOutput{}, errors.New("write disabled: set [mcp].allow_write=true")
	}

	res, err := s.svc.Remember(ctx, memory.RememberInput{
		Type:       in.Type,
		Text:       in.Text,
		Collection: in.Collection,
		Tags:       in.Tags,
		Path:       in.Path,
	})
	if err != nil {
		return nil, rememberOutput{}, err
	}

	return nil, rememberOutput{
		URI:        res.URI,
		DocumentID: res.DocumentID,
		Chunks:     res.Chunks,
		Type:       res.Type,
		Deferred:   res.Deferred,
	}, nil
}
