# Architecture

mnemos is a single cgo-free Go binary. Everything below is compiled into one
executable; there are no external services. This document maps the subsystems so
contributors can find their way around.

## Design principles

1. **Local-first.** All data stays on the machine; no external services.
2. **Single binary.** Deployment is trivial: one cgo-free executable.
3. **SQLite-first.** A single portable `.db` file: backupable, inspectable,
   versionable if needed.
4. **Search before RAG.** The engine must first be excellent at retrieval; RAG
   is one use case layered on top, not the foundation.
5. **Mandatory citations.** Every result points to a verifiable source
   (file + line range).
6. **Optional vectors.** The default build is useful without embeddings;
   semantic/hybrid retrieval is added on top.
7. **Agent-friendly.** Results are structured to be consumed by an LLM, not just
   displayed to a human.
8. **Read-first, then write-back.** Read-only is the starting point; capture
   (`mnemos.remember`) is contained: opt-in, confined to the OKF tree, and
   secret-scanned.

## Data flow

```text
            ┌─────────────┐
files ────► │   ingest    │  scan → hash (change detection) → parse → chunk
            └──────┬──────┘
                   │ documents + chunks (+ links, embeddings)
                   ▼
            ┌─────────────┐
            │   storage   │  SQLite: documents, chunks, FTS5, links, embeddings
            └──────┬──────┘
                   │
        ┌──────────┴───────────┐
        ▼                      ▼
 ┌─────────────┐        ┌─────────────┐
 │   search    │        │  watcher    │  fsnotify → debounce → re-ingest
 │ bm25/hybrid │        └─────────────┘
 └──────┬──────┘
        │ cited results
   ┌────┴────┐
   ▼         ▼
┌──────┐  ┌──────┐
│ cli  │  │ mcp  │   serve over stdio to Claude Code / any MCP client
└──────┘  └──────┘
```

## Packages (`internal/`)

| Package | Responsibility |
|---------|----------------|
| `app` | Config loading (TOML via koanf), logger, database wiring; the `App` handle subcommands build. |
| `cli` | Cobra command tree (`init`, `ingest`, `search`, `serve`, …). Thin layer over the engines. |
| `mcp` | MCP server and tool handlers (`search`, `read`, `context`, `remember`, `okfy`, `list`, `forget`, `move`). |
| `ingest` | Indexing pipeline: scanner, content hasher, debounced file watcher, capture, OKF-aware processing. |
| `parse` | Frontmatter extraction and format-specific parsing (Markdown, Go, plain text). |
| `chunk` | Token-aware splitting for text, Markdown, and code; golden-tested. |
| `storage` | SQLite persistence (modernc.org/sqlite, pure Go), goose migrations, FTS5, documents/chunks/links/embeddings access. |
| `search` | Retrieval: bm25 lexical engine, hybrid (vector + bm25) fusion, reranking, query parsing. |
| `embed` | Embedding interface. Default build is a no-op; the `embed` build tag swaps in local ONNX inference (gomlx), with pooling and normalization. |
| `okf` | OKF bundle validation and the auto-maintained `log.md`. |
| `browse` | Directory walking with exclusion patterns (backs `ls` / `mnemos.list`). |
| `security` | Path-confinement guard and secret scanning. |
| `eval` | Retrieval-quality evaluation over OKF bundles (held-out pairs, Hit@1 / Recall@12 / MRR@12, baselines). |
| `model` | Shared data types (`Result`, `Chunk`, …). |
| `version` | Build metadata stamped via `-ldflags -X`. |
| `testutil` | Test helpers. |

## Key design decisions

- **cgo-free, single binary.** SQLite is `modernc.org/sqlite` (pure Go) and
  builds force `CGO_ENABLED=0`. Even local embeddings stay cgo-free via gomlx.
  See [ADR 0001](adr/0001-embeddings-cgo-free.md).
- **Build-tag-gated semantics.** Heavy ONNX/tokenizer dependencies are behind the
  `embed` tag (`internal/embed/onnx.go` vs `noop.go`), keeping the default binary
  small and dependency-light.
- **Search before RAG.** Lexical FTS5/bm25 is the default and the foundation;
  semantic/hybrid retrieval is layered on top, not a replacement.
- **Local-first and safe by default.** stdio-only MCP, read-only unless opted in,
  path confinement, and secret scanning. See [SECURITY.md](../SECURITY.md).

For the rationale behind specific decisions, see the [ADRs](adr/).

## Retrieval evaluation

`mnemos eval <bundle>` measures retrieval quality over an OKF bundle. OKF
bundles play a dual role: they are both the native ingestion format and the
corpus for evaluating retrieval. The methodology (`internal/eval`):

- **Ground truth, auto-derived.** Each OKF *example query* embedded in a
  concept's body has that concept's document as its expected retrieval target, with
  no manual annotation. The tradeoff: these queries are often lexical (sometimes
  SQL), so the eval measures lexical retrieval as a regression guardrail, not as
  a proxy for an agent's natural-language questions.
- **Held-out (anti auto-match).** Because the query appears verbatim in its
  target chunk, indexing it as-is would make Hit@1 ≈ 1 and the eval useless. The
  eval builds an ephemeral copy of the corpus with each example query *removed*
  from its host chunk, so retrieval must rely on the surrounding prose, headings,
  and frontmatter.
- **Granularity and metrics.** Doc-level Hit (any chunk of the expected document
  counts: the agent reads the section, not an isolated chunk), with the stricter
  exact-chunk rate tracked separately. Metrics: **Hit@1**, **Recall@12**
  (= `default_limit`), and **MRR@12**.
- **Baseline.** Eval is CLI-only (no CI gate). It reads a versioned
  `baseline.json` and prints the deltas; the FTS lexical baseline is the number
  semantic embeddings must beat. Without a gate, watching for drift over time is
  manual.

## Performance profile

Benchmarks live with the hot-path packages (`internal/chunk`, `internal/ingest`,
`internal/search`). A CPU + allocation profile of the search benchmarks
(`go test ./internal/search -bench=. -benchmem -cpuprofile -memprofile`, 12-core
darwin) gives the picture below. Re-run it after any retrieval change.

| Benchmark | chunks | ns/op | B/op | allocs/op |
|---|---|---|---|---|
| `LexicalSearch` | — | 2.75M | 22.7K | 668 |
| `VectorSearch` | 2,000 | 3.33M | 6.45M | 14,246 |
| `VectorSearch` | 10,000 | 16.5M | 32.7M | 70,252 |
| `HybridSearch` | 2,000 | 6.98M | 6.60M | 17,453 |
| `HybridSearch` | 10,000 | 30.0M | 32.8M | 73,459 |

Findings:

- **The lexical default path is cheap and flat** — ~2.75 ms and 22 KB per query,
  with the work pushed into SQLite FTS5. The default cgo-free binary pays none of
  the cost below.
- **Vector/hybrid cost is linear in the chunk count** and allocation-bound. The
  alloc profile attributes ~90% of bytes to `sqlite.columnBlob` + `bytes.Clone`
  inside `VectorRetriever.Search` (cum 95.7%), and the CPU profile is ~67% syscall
  — i.e. every query reads and copies *every* stored vector blob out of SQLite
  (a brute-force scan), ~3.2 KB and ~7 allocs per chunk.
- **Implication.** Semantic retrieval scales O(N·dim) per query because there is no
  vector index; this is the known limitation tracked in
  [ADR 0003](adr/0003-vector-search-scaling.md). The first optimization lever, if
  semantic search becomes hot on large corpora, is to stop re-reading all blobs per
  query — cache decoded vectors in memory or add an ANN index — not to micro-tune
  the scan. The lexical path needs no work.
