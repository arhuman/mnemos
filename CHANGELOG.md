# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres
to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

The first release: an FTS5 MVP plus capture. The default binary is lexical,
pure-Go, and cgo-free; semantic/hybrid search is implemented and ships behind the
`embed` build tag.

### Added
- Single cgo-free Go binary bundling an MCP server, indexing pipeline, SQLite
  store, full-text search, an incremental file watcher, and an admin CLI.
- Indexing pipeline: directory scanning, content-hash change detection, and
  chunking for text, Markdown, and Go source, with token-aware splitting.
- Lexical retrieval over SQLite FTS5 with bm25 ranking; frontmatter `tags`/`type`
  contribute fuzzy ranking signals. Citations report `uri#section` and line ranges.
  Search over-fetches a candidate pool before applying the heading-path boost, so
  a boosted chunk can be promoted into the top results instead of being truncated
  by the bm25 `LIMIT`.
- Native OKF (Open Knowledge Format) support: frontmatter, cross-link edges
  (stored), and `index.md` structure handling.
- MCP tools: `search`, `read`, `context`, `remember`, `okfy`, `list`, `forget`,
  `move`. Write/delete tools are gated behind `allow_write` / `allow_delete`.
- Move (`mv`/`mnemos.move`) un-indexes old URIs before the on-disk rename, so a
  failure never leaves phantom URIs in the index; directory moves are best-effort
  and report an aggregated error, and the count of orphaned inbound links is
  surfaced to the caller. See ADR 0004.
- CLI: `init`, `ingest`, `search`, `ls`, `eval`, `watch`, `serve`, `status`,
  `version`, `models install`, `reindex`, `validate`, `task list`, `forget`,
  `mv`, `okfy`.
- Incremental file watcher with debounce/coalescing that reindexes changed files
  and removes deleted ones.
- Retrieval-quality evaluation (`mnemos eval`) over OKF bundles: auto-derived
  held-out query→source pairs reporting Hit@1 / Recall@12 / MRR@12 against a
  committed baseline.
- Security: stdio-only MCP server, read-only by default, path-confinement guard
  (rejects `..` traversal, symlink escapes, `.mnemos/` access, and configured
  exclusion globs), and secret-scanning of captured content before write/index.
- `list`/`mnemos.list` confine the `path` prefix to the tree root: a prefix that
  escapes the root (e.g. `../`) is refused and returns no entries, rather than
  walking the parent directory and returning metadata from outside the tree. The
  write/delete confinement guard always rejects paths matching `[security].exclude`,
  independent of the `[security].exclude_secrets` indexing toggle.
- Semantic + hybrid retrieval behind the `embed` build tag: local ONNX
  embeddings (cgo-free via gomlx/onnx-gomlx), `mnemos models install`,
  `mnemos reindex --embeddings`, and `--semantic` on `search`/`eval`. The cosine
  scan scores each candidate directly from the stored vector bytes (no per-chunk
  `[]float32` decode), skips the `chunks`/`documents` joins when no document
  filter is set, and runs its lexical and vector passes concurrently: roughly 40%
  faster and ~15 MB less garbage on a 10k-chunk unfiltered query, while staying an
  exact brute-force scan. `BenchmarkVectorSearch` / `BenchmarkHybridSearch` back
  the numbers. See ADR 0003.
- Benchmarks for the chunking (`BenchmarkDispatch`, `BenchmarkDispatchLarge`) and
  ingestion (`BenchmarkPipelineRun`, `BenchmarkIngestPath`) hot paths. New
  `make bench` (full run) and `make bench-smoke` (run each once) targets; CI runs
  the smoke pass with `-benchtime=1x` on every push so benchmarks cannot rot.
- `[indexing].max_file_bytes` config (default 4 MiB) caps the size of any single
  file read into memory during ingestion; oversize files are skipped with a
  warning instead of read whole, bounding memory under the parallel pipeline. Set
  to 0 to disable.
- Version metadata stamped via `-ldflags -X` (`mnemos version -v`).
- Optional Claude Code `mnemos-okf` skill bundled under `skills/`.

### Added
- Single `MNEMOS_DIR` workspace model (ADR 0005, Phase 2): one anchor from which
  every location derives — `kb/` (the knowledge base: tree root, URI namespace,
  write boundary), `kb/capture/`, `state/index.db`, `models/`, `mnemos.toml`.
  Resolved by precedence: `--config` > `--mnemos-dir` > `$MNEMOS_DIR` > project
  `./.mnemos` (discovered up to the git root) > the `~/.mnemos` default.
- `mnemos add <source> [--into <subpath>] [--mode copy|link]`: brings external
  content into the knowledge base (copy by default, or symlink) and indexes it —
  the managed-store entry point.
- `mnemos init --global` initializes `~/.mnemos`; bare `init` creates a
  project-local `./.mnemos`.
- `mnemos migrate --from <old> [--to <dir>] [--move]`: relocates a pre-MNEMOS_DIR
  workspace into the `kb/` layout and reindexes (copy by default; the source is
  left intact unless `--move`).
- Root flag `--mnemos-dir`; `status` now reports the effective workspace (anchor,
  resolution source, kb root, index db).

### Changed
- Configuration carries behaviour only (`[indexing]`, `[chunking]`, `[search]`,
  `[mcp]`, `[capture].defer_to_watcher`, `[security]`). All content and state
  locations are derived from `MNEMOS_DIR`, not configured.
- Ingestion now honors a document's own `collection:` frontmatter (the
  `--collection` flag is the fallback), so a re-index — including `mnemos migrate`
  — preserves each document's original collection.
- Document URIs are always relative to the kb root, regardless of the subtree
  passed to `add`/`ingest`/`watch`. A subtree ingest no longer mints short URIs
  that mismatch the on-disk path, so `ls`/`read`/`move` and citations share one
  namespace. (Glob matching stays anchored at the scan root.)
- Capture is the fixed `kb/capture` subdirectory; notes cite `capture/<file>`.
- `ingest <path>` confines its scan root to the kb, like `okfy`; an out-of-tree
  path is refused with guidance.

### Removed
- Config keys `[storage].path` and `[capture].dir`, and the layered
  `~/.mnemos.toml` + `./.mnemos.toml` discovery — superseded by the single
  `MNEMOS_DIR`/`mnemos.toml` workspace.

[Unreleased]: https://github.com/arhuman/mnemos/commits/main
</content>
