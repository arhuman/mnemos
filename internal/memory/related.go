package memory

import (
	"context"
	"errors"
	"fmt"

	"github.com/arhuman/mnemos/internal/storage"
)

// Direction selects which link edges Related traverses.
type Direction string

const (
	// DirectionOutbound follows links the document contains.
	DirectionOutbound Direction = "outbound"
	// DirectionInbound follows backlinks that point at the document.
	DirectionInbound Direction = "inbound"
	// DirectionBoth follows both; it is also the zero-value default.
	DirectionBoth Direction = "both"
)

// defaultRelatedLimit caps neighbors per direction when the caller leaves limit
// unset, so a hub document does not return an unbounded list.
const defaultRelatedLimit = 50

// RelatedNeighbor is one 1-hop link-graph neighbor of a document. Resolved is
// false for a dangling outbound link (the target uri is not an ingested
// document), in which case Title and Collection are empty. Inbound neighbors are
// always resolved.
type RelatedNeighbor struct {
	URI        string `json:"uri"`
	Title      string `json:"title,omitempty"`
	Collection string `json:"collection,omitempty"`
	Direction  string `json:"direction"`
	Resolved   bool   `json:"resolved"`
}

// RelatedResult is the link-graph neighborhood of a document: the outbound links
// it contains and the inbound backlinks that point at it, one hop out. A
// direction not requested is nil.
type RelatedResult struct {
	URI      string            `json:"uri"`
	Outbound []RelatedNeighbor `json:"outbound"`
	Inbound  []RelatedNeighbor `json:"inbound"`
}

// Related returns the 1-hop link-graph neighbors of the document at uri, honoring
// the server-side visibility boundary ([security].visibility.deny): a neighbor in
// a hidden collection is dropped, and an unknown or hidden uri returns the same
// not-found error as a missing document (mirroring the read verbs). dir selects
// outbound, inbound, or both (the zero value "" is treated as both); limit caps
// each direction (<= 0 uses the default). Edges are document-granularity, so
// results locate documents; the agent reads or searches within one to get cited
// chunks.
func (s *Service) Related(ctx context.Context, uri string, dir Direction, limit int) (RelatedResult, error) {
	if uri == "" {
		return RelatedResult{}, errors.New("provide a uri")
	}

	// Existence + visibility gate, identical to ReadDocument, so Related cannot be
	// used as an oracle for hidden documents.
	doc, err := storage.GetDocumentByURI(ctx, s.db, uri)
	if err != nil {
		return RelatedResult{}, fmt.Errorf("memory: related document: %w", err)
	}
	if doc == nil || s.hiddenCollection(doc.Collection) {
		return RelatedResult{}, fmt.Errorf("unknown uri %q", uri)
	}

	if limit <= 0 {
		limit = defaultRelatedLimit
	}

	res := RelatedResult{URI: uri}
	if dir == DirectionOutbound || dir == DirectionBoth || dir == "" {
		ns, err := storage.OutboundNeighbors(ctx, s.db, uri)
		if err != nil {
			return RelatedResult{}, fmt.Errorf("memory: related outbound: %w", err)
		}
		res.Outbound = s.visibleNeighbors(ns, limit)
	}
	if dir == DirectionInbound || dir == DirectionBoth || dir == "" {
		ns, err := storage.InboundNeighbors(ctx, s.db, uri)
		if err != nil {
			return RelatedResult{}, fmt.Errorf("memory: related inbound: %w", err)
		}
		res.Inbound = s.visibleNeighbors(ns, limit)
	}

	return res, nil
}

// visibleNeighbors drops neighbors in hidden collections and caps the result to
// limit, preserving the storage-layer ordering. A dangling outbound neighbor has
// an empty collection, which is never hidden, so it is kept.
func (s *Service) visibleNeighbors(ns []storage.Neighbor, limit int) []RelatedNeighbor {
	out := make([]RelatedNeighbor, 0, len(ns))
	for _, n := range ns {
		if s.hiddenCollection(n.Collection) {
			continue
		}
		out = append(out, RelatedNeighbor{
			URI:        n.URI,
			Title:      n.Title,
			Collection: n.Collection,
			Direction:  n.Direction,
			Resolved:   n.Resolved,
		})
		if len(out) >= limit {
			break
		}
	}

	return out
}
