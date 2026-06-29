package mcp

import (
	"context"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/arhuman/mnemos/internal/browse"
)

// listInput is the mnemos.list request. Every field is optional; with none set
// the tool returns every indexable file in the tree, annotated with index
// metadata. The SDK derives the JSON schema from these tags.
type listInput struct {
	Path          string `json:"path,omitempty"           jsonschema:"restrict to a file or directory path (matched at segment boundaries)"`
	Collection    string `json:"collection,omitempty"     jsonschema:"restrict to this collection (exact match)"`
	Type          string `json:"type,omitempty"           jsonschema:"restrict to a file extension, e.g. md"`
	All           bool   `json:"all,omitempty"            jsonschema:"include every file on disk, not just indexable ones"`
	IndexedOnly   bool   `json:"indexed_only,omitempty"   jsonschema:"only entries present in the index"`
	UnindexedOnly bool   `json:"unindexed_only,omitempty" jsonschema:"only entries not present in the index"`
	Limit         int    `json:"limit,omitempty"          jsonschema:"maximum number of entries (0 = unlimited)"`
}

// listOutput is the mnemos.list response: a flat array of entries sorted by uri.
// Agents can group it into a tree from the uri path segments.
type listOutput struct {
	Entries []browse.Entry `json:"entries"`
}

// registerList wires the read-only mnemos.list tool. It is always registered
// (alongside search/read/context) and needs no allow_* gate: listing the tree is
// not a write.
func (s *Server) registerList() {
	mcpsdk.AddTool(s.mcp, &mcpsdk.Tool{
		Name:        "mnemos.list",
		Description: "List and browse the OKF tree. Walks the tree on disk and annotates each file with the index metadata mnemos holds (title, type, tags, collection) plus an 'indexed' flag, so both stored documents and not-yet-indexed files are visible.",
	}, s.handleList)
}

func (s *Server) handleList(ctx context.Context, _ *mcpsdk.CallToolRequest, in listInput) (*mcpsdk.CallToolResult, listOutput, error) {
	if err := ctx.Err(); err != nil {
		return nil, listOutput{}, err
	}

	entries, err := s.svc.List(ctx, browse.Options{
		Collection:    in.Collection,
		PathPrefix:    in.Path,
		FileType:      in.Type,
		All:           in.All,
		IndexedOnly:   in.IndexedOnly,
		UnindexedOnly: in.UnindexedOnly,
		Limit:         in.Limit,
	})
	if err != nil {
		return nil, listOutput{}, err
	}

	return nil, listOutput{Entries: entries}, nil
}
