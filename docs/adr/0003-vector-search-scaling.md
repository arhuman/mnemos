# 3. Vector search: linear scan now, ANN index deferred

Date: 2026-06-28

## Status

Accepted

## Context

[ADR 0001](0001-embeddings-cgo-free.md) introduced local ONNX embeddings (cgo-free, behind the `embed` build tag) and hybrid lexical + vector retrieval fused by RRF. Vector search is implemented as a **brute-force linear scan**: `VectorRetriever.Search` (`internal/search/hybrid.go`) iterates over all embeddings of the model, computes the cosine of each, then sorts in Go to keep the top-K.

A multi-model review (Claude, Gemini, local gemma) unanimously identified this scan as **the main scaling risk**: beyond a few thousand chunks, reading and decoding all vectors on every query becomes expensive. The top recommendation was to add an ANN (Approximate Nearest Neighbor, HNSW-type) index or a candidate pre-filter.

We must decide what to do **now**, without betraying the project's invariants:

- single **cgo-free** binary by default (a performant ANN index often implies cgo);
- "useful without embeddings": FTS5 lexical search remains the default path;
- current target scale = personal / project memory (hundreds to a few thousand documents).

## Decision

### 1. Keep the brute-force linear scan for the current scale

The exact scan remains the strategy. At the target scale it is **correct, simple and dependency-free**, and guarantees exact recall (no approximation). A SQL `LIMIT` would be **incorrect** here: SQLite cannot sort by cosine, so a `LIMIT` would truncate on arbitrary rows and silently break recall. There is therefore deliberately no `LIMIT` in `vectorScanSQL`.

### 2. Applied constant-factor optimizations (exact recall preserved)

Several optimizations make the scan cheaper without changing the strategy or approximating recall:

- **Content fetched only for the top-K.** The scan selects only the chunk id and the vector (small, fixed size); the chunk's `content` (large text payload) is fetched **only for the K winners**, in `hydrate` (`internal/search/hybrid.go`). This removes the dominant avoidable I/O (reading the text of all chunks).
- **Score computed directly from the stored bytes.** Each candidate's cosine is computed straight off the little-endian vector blob (`dotBlob`), instead of decoding a `[]float32` per row. This removes one heap allocation per chunk: the scan's dominant garbage source.
- **No joins on the unfiltered path.** When the query carries no document filter, the scan reads `embeddings` alone (the chunk id and vector both live there); the `chunks` / `documents` joins are emitted only when a filter needs them (`vectorScanSQL`).
- **Lexical and vector retrievers run concurrently.** `HybridRetriever` overlaps the two passes, hiding the query-embedding inference and the scan's Go-side scoring behind the lexical query. The two SQL statements still serialize on the single SQLite connection (`SetMaxOpenConns(1)`, kept because modernc applies PRAGMAs per-connection), so the win is the CPU work either side of them; lifting that would require a second read-only connection with PRAGMAs set via DSN, deliberately out of scope here.

Measured on a 10k-chunk corpus (dim 384), these together cut an unfiltered semantic query by roughly 40% wall-clock and ~15 MB of allocations versus the naive decode-then-dot scan, widening the headroom before the threshold in §3 is reached. The vector scan itself remains unavoidable without an index.

### 3. ANN index deferred, with an explicit threshold and plan

Adding an ANN index is **deferred** until a real need appears, defined by an **indicative threshold of about 10,000 chunks** (or a semantic-search p95 latency > ~100 ms measured by a benchmark). Plan when the threshold is crossed:

1. **cgo-free preference**: evaluate a pure-Go ANN library (e.g. pure-Go HNSW) to preserve the default binary's invariant.
2. **cgo fallback behind a tag**: if no pure-Go option is satisfactory, expose the performant index only behind a build tag (like `embed`), never in the default build.
3. **Persistence**: the index must rebuild from the `embeddings` table (source of truth), via `mnemos reindex`, without a new fragile schema.

### 4. Measure before optimizing

A search benchmark (`internal/search`, `BenchmarkVectorSearch` / `BenchmarkHybridSearch`) makes the decision data-driven: it scans realistic dim-384 dense vectors across corpus sizes (filtered and unfiltered) and reports allocations, so an index is introduced only when the benchmark justifies it, not by anticipation. It also quantifies the §2 optimizations above.

## Consequences

### Positive

- No dependency or cgo added as long as personal scale is respected; the binary stays simple.
- Exact recall guaranteed (no approximation) at the current scale.
- The applied constant-factor optimizations (top-K hydration, byte-level scoring, conditional joins, concurrent retrievers) already cut the scan's cost, without approximation, and push the threshold further out.
- The threshold and plan are written down: the debt is explicit, not implicit.

### Negative / risks

- Known performance ceiling: on large corpora, semantic search will degrade linearly until the ANN index is implemented.
- The threshold (~10,000 chunks) is indicative and will need to be confirmed by the benchmark on the target hardware.

## Alternatives considered

- **Add HNSW (cgo) now.** Would solve the scale but would break the default cgo-free invariant and add a heavy dependency for a need not yet reached. Rejected (premature).
- **Add a SQL `LIMIT` to the scan.** Suggested in review, but **incorrect**: without cosine sorting on the database side, it truncates arbitrarily and silently degrades recall. Rejected.
- **Candidate pre-filter without an index.** No cheap exact pre-filter exists for cosine without an index structure; illusory gain. Rejected.

## References

- Previous ADR: [0001: Embeddings cgo-free](0001-embeddings-cgo-free.md).
- Implementation: `internal/search/hybrid.go` (`VectorRetriever.Search`, `dotBlob`, `vectorScanSQL`, `hydrate`, `HybridRetriever.Search`); benchmark in `internal/search/bench_test.go`.
- Review: `.claude/doc/repo-evaluation-10x.md` (multi-model evaluation, recommendation #1).
