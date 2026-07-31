package search

import (
	"cmp"
	"context"
	"database/sql"
	"log/slog"
	"math"
	"slices"

	"github.com/arhuman/mnemos/internal/model"
	"github.com/arhuman/mnemos/internal/storage"
)

// defaultGraphSeedDepth and defaultGraphDecay are the fallbacks when the config
// leaves the graph-expansion knobs unset.
const (
	defaultGraphSeedDepth = 3
	defaultGraphDecay     = 0.5
)

// GraphRetriever wraps a base Retriever and, when the base under-fills the
// requested limit, appends the 1-hop link neighbors of the top seed documents as
// extra results. It is deliberately conservative: it never reorders or evicts a
// base result, so ranking metrics (Hit@1, MRR) over base-covered queries cannot
// regress; it only fills empty slots, so recall can rise when the answer document
// is a link neighbor that shares no keywords with the query. Injected neighbors
// carry a decayed seed score and the neighbor document's first chunk as citation.
type GraphRetriever struct {
	base      Retriever
	db        *sql.DB
	seedDepth int
	decay     float64
	logger    *slog.Logger
}

// NewGraphRetriever wraps base with link-neighbor expansion over db. seedDepth is
// how many top hits are expanded (<= 0 uses the default); decay in (0,1] scales a
// seed's score for its neighbors (<= 0 uses the default).
func NewGraphRetriever(base Retriever, db *sql.DB, seedDepth int, decay float64, logger *slog.Logger) *GraphRetriever {
	if seedDepth <= 0 {
		seedDepth = defaultGraphSeedDepth
	}
	if decay <= 0 {
		decay = defaultGraphDecay
	}
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}

	return &GraphRetriever{base: base, db: db, seedDepth: seedDepth, decay: decay, logger: logger}
}

// Search runs the base retriever, then fills any remaining slots up to the limit
// with link neighbors of the top seed documents. Base results are returned
// unchanged and in place; neighbors are only appended.
func (g *GraphRetriever) Search(ctx context.Context, q Query) ([]model.Result, error) {
	base, err := g.base.Search(ctx, q)
	if err != nil {
		return nil, err
	}
	limit := normalizeLimit(q.Limit)
	room := limit - len(base)
	if room <= 0 {
		// Base already fills the limit: appending nothing keeps expansion strictly
		// non-regressive at the requested depth.
		return base, nil
	}

	seen := make(map[string]bool, len(base))
	for _, r := range base {
		seen[r.URI] = true
	}
	denied := make(map[string]bool, len(q.ExcludeCollections))
	for _, c := range q.ExcludeCollections {
		denied[c] = true
	}

	extra := make([]model.Result, 0, room)
	for rank, seed := range distinctSeeds(base, g.seedDepth) {
		weight := seed.score * math.Pow(g.decay, float64(rank+1))
		for _, nuri := range g.resolvedNeighborURIs(ctx, seed.uri) {
			if seen[nuri] {
				continue
			}
			seen[nuri] = true
			res, ok := g.neighborResult(ctx, nuri, weight, denied)
			if ok {
				extra = append(extra, res)
			}
		}
	}

	// Order neighbors by their decayed weight (higher first), stably, then take
	// only enough to fill the empty slots.
	slices.SortStableFunc(extra, func(a, b model.Result) int { return cmp.Compare(b.Score, a.Score) })
	if len(extra) > room {
		extra = extra[:room]
	}

	return append(base, extra...), nil
}

// seed is a base result reduced to what expansion needs: the document uri and its
// score.
type seed struct {
	uri   string
	score float64
}

// distinctSeeds returns the first n distinct-document base results (best-first),
// preserving base order. Base results are already sorted best-first, so the first
// occurrence of each uri is its best chunk.
func distinctSeeds(base []model.Result, n int) []seed {
	seen := make(map[string]bool, n)
	out := make([]seed, 0, n)
	for _, r := range base {
		if seen[r.URI] {
			continue
		}
		seen[r.URI] = true
		out = append(out, seed{uri: r.URI, score: r.Score})
		if len(out) >= n {
			break
		}
	}

	return out
}

// resolvedNeighborURIs returns the distinct uris of ingested (resolvable) 1-hop
// neighbors of srcURI, both outbound and inbound. Dangling outbound targets are
// dropped: they have no chunk to cite. Errors are logged and treated as "no
// neighbors" so expansion never fails the base query.
func (g *GraphRetriever) resolvedNeighborURIs(ctx context.Context, srcURI string) []string {
	var uris []string
	seen := make(map[string]bool)

	add := func(ns []storage.Neighbor) {
		for _, n := range ns {
			if !n.Resolved || seen[n.URI] {
				continue
			}
			seen[n.URI] = true
			uris = append(uris, n.URI)
		}
	}

	outbound, err := storage.OutboundNeighbors(ctx, g.db, srcURI)
	if err != nil {
		g.logger.Debug("graph expansion: outbound neighbors failed", "uri", srcURI, "err", err)
	} else {
		add(outbound)
	}
	inbound, err := storage.InboundNeighbors(ctx, g.db, srcURI)
	if err != nil {
		g.logger.Debug("graph expansion: inbound neighbors failed", "uri", srcURI, "err", err)
	} else {
		add(inbound)
	}

	return uris
}

// neighborResult builds a result for a neighbor document at uri with the given
// score, or (zero, false) when the document is hidden, missing, or has no chunk
// to cite. The citation is the document's first chunk, since the neighbor did not
// match the query lexically and has no query-specific best chunk.
func (g *GraphRetriever) neighborResult(ctx context.Context, uri string, score float64, denied map[string]bool) (model.Result, bool) {
	doc, err := storage.GetDocumentByURI(ctx, g.db, uri)
	if err != nil || doc == nil || denied[doc.Collection] {
		return model.Result{}, false
	}
	chunk, err := storage.FirstChunkByDocURI(ctx, g.db, uri)
	if err != nil || chunk == nil {
		return model.Result{}, false
	}

	return model.Result{
		ID:          chunk.ID,
		DocumentID:  chunk.DocumentID,
		URI:         doc.URI,
		Collection:  doc.Collection,
		Title:       doc.Title,
		ModifiedAt:  doc.ModifiedAt,
		HeadingPath: chunk.HeadingPath,
		StartLine:   chunk.StartLine,
		EndLine:     chunk.EndLine,
		Snippet:     snippet(chunk.Content),
		Score:       score,
	}, true
}
