package eval

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/arhuman/mnemos/internal/chunk"
	"github.com/arhuman/mnemos/internal/ingest"
	"github.com/arhuman/mnemos/internal/model"
	"github.com/arhuman/mnemos/internal/search"
	"github.com/arhuman/mnemos/internal/storage"
)

// defaultGraphK is the shallow retrieval depth used by the graph-answerability
// eval. It is deliberately small: the point is to measure whether the answer
// document is reachable via link neighbors when it is NOT in the top few lexical
// hits, so a deep K (which on a small bundle returns every document) would make
// the metric trivially 1.0.
const defaultGraphK = 3

// GraphCase is one curated graph-answerability example: a natural-language query
// whose best lexical match is a hub/overview document, and the expected answer
// document that is reachable from that hub by a link rather than by keywords.
type GraphCase struct {
	Query       string `json:"query"`
	ExpectedURI string `json:"expected_uri"`
	Note        string `json:"note,omitempty"`
}

// graphCasesFile is the on-disk shape of a bundle's cases.json.
type graphCasesFile struct {
	Description string      `json:"description,omitempty"`
	Cases       []GraphCase `json:"cases"`
}

// GraphOptions configures a graph-answerability run. Bundle is the OKF bundle
// (which must contain a cases.json). K is the lexical retrieval depth used both
// for the direct-hit measurement and as the seed pool whose neighbors are
// expanded; SeedDepth caps how many of the top-K hits are expanded. Include /
// Exclude / Chunking mirror the ingest configuration.
type GraphOptions struct {
	Bundle    string
	K         int
	SeedDepth int
	Include   []string
	Exclude   []string
	Chunking  chunk.Config
}

// GraphMetrics holds the graph-answerability scores over all cases. Rates are in
// [0,1]. DirectHitAtK is what plain lexical search achieves; NeighborInclusion
// is what read/context neighborhood injection would achieve (the answer is in
// the top-K OR in the 1-hop neighborhood of the top seeds); Lift is the value
// injection adds; RecoveredMisses is, of the cases plain search misses, the
// fraction injection recovers.
type GraphMetrics struct {
	N                 int     `json:"n"`
	K                 int     `json:"k"`
	SeedDepth         int     `json:"seed_depth"`
	DirectHitAtK      float64 `json:"direct_hit_at_k"`
	NeighborInclusion float64 `json:"neighbor_inclusion"`
	Lift              float64 `json:"lift"`
	RecoveredMisses   float64 `json:"recovered_misses"`
	// GraphHitAtK is the fraction of cases where the actual GraphRetriever (base +
	// link expansion) returns the expected document in its top-K. It is the real
	// Phase 2 measurement; NeighborInclusion is the raw-neighborhood proxy above.
	GraphHitAtK float64 `json:"graph_hit_at_k"`
}

// GraphCaseResult is the per-case outcome, surfaced so the report and tests can
// show exactly which cases injection recovered.
type GraphCaseResult struct {
	Query       string
	ExpectedURI string
	DirectHit   bool
	Included    bool
	GraphHit    bool
	Seeds       []string
}

// loadGraphCases reads and validates the bundle's cases.json.
func loadGraphCases(bundle string) ([]GraphCase, error) {
	path := filepath.Join(bundle, "cases.json")
	data, err := os.ReadFile(path) //nolint:gosec // path is <bundle>/cases.json, a caller-provided eval bundle
	if err != nil {
		return nil, fmt.Errorf("eval: read cases.json: %w", err)
	}
	var f graphCasesFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("eval: parse cases.json: %w", err)
	}
	for i, c := range f.Cases {
		if c.Query == "" || c.ExpectedURI == "" {
			return nil, fmt.Errorf("eval: cases.json case %d needs both query and expected_uri", i)
		}
	}

	return f.Cases, nil
}

// RunGraphInclusion ingests the bundle as-is (answers stay in place, links
// captured), runs each curated query through the lexical retriever, and measures
// whether the expected answer document is found directly (top-K) versus reachable
// through the 1-hop link neighborhood of the top seeds (what neighborhood
// injection surfaces). The user's real database is never touched. It returns the
// aggregate metrics and the per-case results.
func RunGraphInclusion(ctx context.Context, logger *slog.Logger, opts GraphOptions) (GraphMetrics, []GraphCaseResult, error) {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	k := opts.K
	if k <= 0 {
		k = defaultGraphK
	}
	seedDepth := opts.SeedDepth
	if seedDepth <= 0 {
		seedDepth = k
	}

	cases, err := loadGraphCases(opts.Bundle)
	if err != nil {
		return GraphMetrics{}, nil, err
	}
	if len(cases) == 0 {
		return GraphMetrics{K: k, SeedDepth: seedDepth}, nil, nil
	}

	db, cleanup, err := ingestBundle(ctx, logger, opts.Bundle, opts.Include, opts.Exclude, opts.Chunking)
	if err != nil {
		return GraphMetrics{}, nil, err
	}
	defer cleanup()

	retriever := search.NewEngine(db, logger)
	graphRetriever := search.NewGraphRetriever(retriever, db, seedDepth, 0, logger)
	results := make([]GraphCaseResult, 0, len(cases))
	for _, c := range cases {
		hits, err := retriever.Search(ctx, search.Query{Text: c.Query, Limit: k})
		if err != nil {
			return GraphMetrics{}, nil, fmt.Errorf("eval: graph search %q: %w", c.Query, err)
		}
		r := evalGraphCase(ctx, db, c, hits, seedDepth)

		graphHits, err := graphRetriever.Search(ctx, search.Query{Text: c.Query, Limit: k})
		if err != nil {
			return GraphMetrics{}, nil, fmt.Errorf("eval: graph-retriever search %q: %w", c.Query, err)
		}
		r.GraphHit = containsURI(graphHits, c.ExpectedURI)

		results = append(results, r)
	}

	return computeGraph(results, k, seedDepth), results, nil
}

// containsURI reports whether any result belongs to uri.
func containsURI(results []model.Result, uri string) bool {
	for _, r := range results {
		if r.URI == uri {
			return true
		}
	}

	return false
}

// evalGraphCase scores one case: whether the expected doc is a direct top-K hit,
// and whether it is included once the 1-hop neighborhood of the top seedDepth
// hits is added (the injection view).
func evalGraphCase(ctx context.Context, db *sql.DB, c GraphCase, hits []model.Result, seedDepth int) GraphCaseResult {
	topURIs := make(map[string]bool)
	var seeds []string
	for _, h := range hits {
		if !topURIs[h.URI] {
			if len(seeds) < seedDepth {
				seeds = append(seeds, h.URI)
			}
			topURIs[h.URI] = true
		}
	}

	directHit := topURIs[c.ExpectedURI]

	included := directHit
	if !included {
		neighbors := neighborhoodURIs(ctx, db, seeds)
		included = neighbors[c.ExpectedURI]
	}

	return GraphCaseResult{
		Query:       c.Query,
		ExpectedURI: c.ExpectedURI,
		DirectHit:   directHit,
		Included:    included,
		Seeds:       seeds,
	}
}

// neighborhoodURIs returns the set of document uris reachable in one hop
// (outbound or inbound) from any seed uri. This is exactly what read/context
// neighborhood injection would attach to the seed documents.
func neighborhoodURIs(ctx context.Context, db *sql.DB, seeds []string) map[string]bool {
	out := make(map[string]bool)
	for _, s := range seeds {
		outbound, err := storage.OutboundNeighbors(ctx, db, s)
		if err != nil {
			continue
		}
		for _, n := range outbound {
			out[n.URI] = true
		}
		inbound, err := storage.InboundNeighbors(ctx, db, s)
		if err != nil {
			continue
		}
		for _, n := range inbound {
			out[n.URI] = true
		}
	}

	return out
}

// computeGraph aggregates per-case results into the graph metrics.
func computeGraph(results []GraphCaseResult, k, seedDepth int) GraphMetrics {
	m := GraphMetrics{N: len(results), K: k, SeedDepth: seedDepth}
	if len(results) == 0 {
		return m
	}
	var directHits, included, graphHits, misses, recovered int
	for _, r := range results {
		if r.DirectHit {
			directHits++
		} else {
			misses++
			if r.Included {
				recovered++
			}
		}
		if r.Included {
			included++
		}
		if r.GraphHit {
			graphHits++
		}
	}
	n := float64(len(results))
	m.DirectHitAtK = float64(directHits) / n
	m.NeighborInclusion = float64(included) / n
	m.GraphHitAtK = float64(graphHits) / n
	m.Lift = m.NeighborInclusion - m.DirectHitAtK
	if misses > 0 {
		m.RecoveredMisses = float64(recovered) / float64(misses)
	}

	return m
}

// ingestBundle opens an ephemeral migrated database under a temp dir, ingests the
// whole bundle into it (no held-out stripping, so links are captured intact), and
// returns the db plus a cleanup that closes it and removes the temp dir.
func ingestBundle(ctx context.Context, logger *slog.Logger, bundle string, include, exclude []string, chunking chunk.Config) (*sql.DB, func(), error) {
	work, err := os.MkdirTemp("", "mnemos-grapheval-*")
	if err != nil {
		return nil, nil, fmt.Errorf("eval: temp dir: %w", err)
	}
	cleanupDir := func() { _ = os.RemoveAll(work) }

	db, err := storage.Open(ctx, filepath.Join(work, "grapheval.db"))
	if err != nil {
		cleanupDir()

		return nil, nil, fmt.Errorf("eval: open temp db: %w", err)
	}
	if err := storage.Migrate(db); err != nil {
		_ = db.Close()
		cleanupDir()

		return nil, nil, fmt.Errorf("eval: migrate temp db: %w", err)
	}

	pipeline := ingest.New(db, logger)
	if _, err := pipeline.Run(ctx, ingest.Options{
		Root:       bundle,
		Collection: evalCollection,
		Rules:      ingest.Rules{Include: include, Exclude: exclude},
		Chunking:   chunking,
	}); err != nil {
		_ = db.Close()
		cleanupDir()

		return nil, nil, fmt.Errorf("eval: ingest bundle: %w", err)
	}

	return db, func() { _ = db.Close(); cleanupDir() }, nil
}

// ReportGraph runs a graph-answerability evaluation and renders the metrics plus
// a per-case breakdown to out. It returns the computed metrics.
func ReportGraph(ctx context.Context, logger *slog.Logger, out io.Writer, opts GraphOptions) (GraphMetrics, error) {
	m, cases, err := RunGraphInclusion(ctx, logger, opts)
	if err != nil {
		return GraphMetrics{}, err
	}
	writeGraphReport(out, m, cases)

	return m, nil
}

// writeGraphReport prints the aggregate metrics and, beneath them, one line per
// case showing whether plain search hit it and whether injection included it.
func writeGraphReport(out io.Writer, m GraphMetrics, cases []GraphCaseResult) {
	_, _ = fmt.Fprintf(out, "graph-answerability  (N=%d, K=%d, seed_depth=%d)\n", m.N, m.K, m.SeedDepth)
	_, _ = fmt.Fprintf(out, "  direct hit@K        %.2f  (plain lexical search)\n", m.DirectHitAtK)
	_, _ = fmt.Fprintf(out, "  neighbor inclusion  %.2f  (search + 1-hop neighborhood, proxy)\n", m.NeighborInclusion)
	_, _ = fmt.Fprintf(out, "  graph hit@K         %.2f  (actual GraphRetriever, base + expansion)\n", m.GraphHitAtK)
	_, _ = fmt.Fprintf(out, "  lift                %+.2f  (neighbor inclusion over direct hit)\n", m.Lift)
	_, _ = fmt.Fprintf(out, "  recovered misses    %.2f  (of search misses, share injection recovers)\n", m.RecoveredMisses)
	if len(cases) == 0 {
		return
	}
	_, _ = fmt.Fprintln(out, "  cases:")
	for _, c := range cases {
		_, _ = fmt.Fprintf(out, "    [%s] %s -> %s\n", graphCaseLabel(c), c.Query, c.ExpectedURI)
	}
}

// graphCaseLabel classifies a case outcome for the per-case breakdown.
func graphCaseLabel(c GraphCaseResult) string {
	switch {
	case c.DirectHit:
		return "direct"
	case c.Included:
		return "recovered"
	default:
		return "missed"
	}
}
