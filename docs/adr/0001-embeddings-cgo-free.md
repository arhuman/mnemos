# 1. Semantic search via embeddings, while staying cgo-free

Date: 2026-06-27

## Status

Accepted

## Context

mnemos's current retrieval is purely **lexical** (SQLite FTS5 / bm25). The goal
is **semantic** search via local embeddings, then **hybrid search**
(vectors + bm25), in order to answer natural-language queries that lexical search
misses ("why did we choose this architecture").

Two hard promises of the project constrain any solution:

1. **Single binary, cgo-free**: `make build`/`make install` force
   `CGO_ENABLED=0`; the SQLite driver is `modernc.org/sqlite` (100% Go),
   chosen precisely for this.
2. **Local-first, zero external services**: no Ollama, Qdrant, Chroma,
   Python, Node, nor mandatory Docker.

The reflex "embeddings = ONNX Runtime" conflicts head-on with (1):
ONNX Runtime is C++, and its reference Go binding
(`github.com/yalue/onnxruntime_go`) requires **cgo + a shared library
`libonnxruntime`** loaded at runtime. Choosing this path would mean abandoning
the single static binary.

The real question is therefore not "ONNX or not" but
**"can we run embedding inference in pure Go, cgo-free, with acceptable quality
and latency?"**.

## Decision

### Inference: pure Go via gomlx (SimpleGo backend) + onnx-gomlx

- Target model: **all-MiniLM-L6-v2** (384 dimensions, BERT/WordPiece tokenizer,
  masked mean-pooling + L2 normalization).
- Import the `.onnx` model via `github.com/gomlx/onnx-gomlx`, execute via
  `github.com/gomlx/gomlx` with its **SimpleGo (pure Go) backend**, **not** the
  XLA/PJRT backend (which would reintroduce cgo).
- Tokenizer: `github.com/sugarme/tokenizer` (pure Go, reads HF `tokenizer.json`).
- Mean-pooling (with attention mask) and L2 are done **in Go** after the
  forward pass, to minimize the surface of ONNX ops that SimpleGo must cover.

This choice is validated by a POC (worktree `agent-a79532c4aa18eb47c`,
`poc/embed/`):

- `CGO_ENABLED=0 go test ./poc/embed/...` **passes**; the test binary is **static**
  (no libc/dylib link); SimpleGo executes the full BERT graph **with no missing
  op**.
- Quality: cos("a dog is running", "a puppy runs") = **0.82** vs
  cos(…, "the stock market crashed") = **0.035**; L2 norm ≈ 1.0; dim 384.
- Latency (CPU, Apple Silicon): ~**137 ms**/request; batch throughput ~**12
  embeds/s**.

### Confinement behind a build tag

The embedder and its dependencies live behind a build tag (`//go:build embed`).

- **Default** build: FTS5 only, dependencies unchanged, lean binary.
- **`-tags embed`** build: semantic search enabled.

The tag is **not** used to isolate cgo (the chosen path has none) but to
**contain the weight of the dependencies**: gomlx pulls in ~45 indirect modules in
`go.sum` (gonum/plot, charmbracelet, go-xla…) that are **not compiled** into
our package, but pollute `go mod tidy` and the `govulncheck` surface.
The default `Embedder` remains a lexical `noop`.

### Storage and search

- Migration `0002`: table `embeddings(chunk_id, model, dim, vector BLOB)`,
  little-endian float32 vectors **L2-normalized** (→ cosine = dot product).
- `VectorRetriever`: linear scan (sufficient up to a few tens of thousands of
  chunks, cf. plan).
- `HybridRetriever`: implements the existing `Retriever` interface
  (`internal/search/engine.go`) and **fuses bm25 + vectors via Reciprocal
  Rank Fusion (RRF)**, `score = Σ 1/(k + rank_i)` (k ≈ 60). RRF avoids the
  fragile normalization between bm25 scores (open scale) and cosine `[-1,1]`.
- Model distribution: `~/.mnemos/models/all-MiniLM-L6-v2/` via
  `mnemos models install` (Option A of the plan, no `//go:embed` initially).

### Quality gate

`mnemos eval` must show a **positive delta** in Hit@1 / Recall@12 / MRR@12 vs
the FTS lexical baseline for embeddings to be retained.

## Consequences

### Positive

- Promise #1 (single binary, cgo-free, zero external services) is **preserved**
  even with semantics.
- The `Embedder` is an interface: if SimpleGo throughput becomes a blocker, we
  plug in an ONNX Runtime backend (cgo) without touching the callers.
- The `Retriever` seam already exists → the hybrid inserts without breaking MCP or CLI.

### Negative / risks

- **Low indexing throughput** (~12 embeds/s): ~80 s for 1000 chunks.
  Considered mitigations: embedders in parallel goroutines, int8 model
  (23 MB), or accepting a slow initial reindex (incremental afterwards via the watcher).
- **`go.mod` noise**: +45 indirect modules (not linked). Contained by the build tag.
- **SimpleGo recompiles the graph** when the batch/seq shape changes → bucketize
  sequence lengths in production.
- gomlx/onnx-gomlx are **young**; risk of API churn (already observed between
  versions) and of op coverage for other future models.

## Alternatives considered

- **Path B: ONNX Runtime via `yalue/onnxruntime_go` (cgo).** Faster
  (CPU ORT, several ×) and more mature, but reintroduces cgo + an embedded
  native lib → breaks the single static binary. **Rejected** as the default
  path; kept as a fallback if throughput becomes critical at scale.
- **`sqlite-vec` (V2 of the plan) for vector search.** Loadable C extension,
  **incompatible** with the pure-Go driver `modernc.org/sqlite`.
  **Rejected.** To scale while staying cgo-free, aim instead for a pure-Go ANN
  index (HNSW) built in memory from the BLOBs.
- **Sidecar (Ollama / llama.cpp / Python).** Contradicts "zero external services".
  **Rejected.**
- **Hand-rolled pure Go (custom BERT forward).** Too much effort, slow, fragile.
  **Rejected** in favor of gomlx.

## References

- Architecture: [docs/architecture.md](../architecture.md) (`embed`, `search`, and `storage` packages).
- POC: worktree `.claude/worktrees/agent-a79532c4aa18eb47c`, `poc/embed/`.
- Retrieval seam: `internal/search/engine.go` (`Retriever` interface).
