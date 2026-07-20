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
  chunking for text, Markdown, and Go source, with token-aware splitting. A
  Markdown document's stored `documents.title` honors an explicit frontmatter
  `title:` (falling back to the first heading, then the first non-empty body
  line), so an OKF document is no longer mislabelled by its first `##` heading
  in `search`/`context`/`list`. Existing documents pick up the corrected title
  on their next re-ingest (unchanged files are content-hash-skipped).
- Lexical retrieval over SQLite FTS5 with bm25 ranking; frontmatter `tags`/`type`
  contribute fuzzy ranking signals. Citations report `uri#section` and line ranges.
  Search over-fetches a candidate pool before applying the heading-path boost, so
  a boosted chunk can be promoted into the top results instead of being truncated
  by the bm25 `LIMIT`.
- Result shaping at the MCP boundary to cut follow-up `read` calls and response
  size (plan §3.3): `search`/`context` return the real `documents.title` and
  `modified_at` instead of a heading-derived label; a heading/tag-only hit whose
  content snippet is empty falls back to its heading path rather than a blank
  line; `context` defaults to `group_by=document` (best chunk per document, with
  `group_by=chunk` for the flat listing) and trims the low-relevance tail at a
  relevance cliff; `context`/`read` accept `max_chars`/`max_tokens` budgets (with
  a `truncated` flag) and `read` accepts `section`/`lines` to scope a document to
  a subset of its chunks; `search` scores are rounded to one decimal.
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
- `reindex --content` re-parses and rewrites every already-indexed document from
  its on-disk file, bypassing the unchanged-file content-hash skip, so a parser or
  schema change (e.g. the frontmatter-title fix) propagates to the whole index
  without editing files. It walks the documents table, preserving each document's
  stored collection, and leaves rows whose file has vanished in place. Rewriting
  chunks cascades away their embeddings, so it advises a follow-up
  `reindex --embeddings`; `--content` and `--embeddings` may be combined (content
  runs first). Normal ingest/watch stay hash-skip-fast (the new `ingest.Options.Force`
  defaults off).
- Incremental file watcher with debounce/coalescing that reindexes changed files
  and removes deleted ones.
- Retrieval-quality evaluation (`mnemos eval`) over OKF bundles: auto-derived
  held-out query→source pairs reporting Hit@1 / Recall@12 / MRR@12 against a
  committed baseline.
- Collection visibility boundary: `[security].visibility.deny` lists collections
  hidden from every query surface (`search`, `context`, `list`, `task`), enforced
  server-side in the query layer — a denied collection never surfaces even when a
  caller names it explicitly. Empty by default (nothing hidden), so a single-store
  KB stays fully visible until configured. `search` results and `context` blocks
  now also carry their `collection` as a provenance label. See plan §5.2.
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
- `mnemos-okf` skill reworked from three modes to six: RECALL (bounded to two
  searches), CAPTURE (inbox-first with hard exclusions), OKF, plus RESTORE
  (session working set with citations), TASK (tasks as `type: task` documents
  with state/history split and silent auto-creation), and CONSOLIDATE
  (five-bucket triage, dedup into canonical docs, conflicts surfaced instead of
  overwritten, journaled passes). Frontmatter contracts and the full
  consolidation procedure live in `references/task-schema.md` and
  `references/consolidation.md`, read on demand.
- Claude Code hooks example at `skills/mnemos-okf/hooks/settings.example.json`:
  deterministic working-set injection via `SessionStart` (at startup, resume,
  and after compaction) and cue-gated recall via `UserPromptSubmit`; requires
  `jq`; silent no-op when no cue matches.
- `make install-hooks` target merges those hooks into `~/.claude/settings.json`
  idempotently (marker-keyed, keeps a `.bak`); `install-skill` now calls it so a
  single install both copies the skill and activates its hooks. `SKIP_HOOKS=1`
  opts out.
- `examples/project-memory/`: OKF-conformant project-memory bundle (status,
  constraints, decisions, tasks with state/history split, consolidation journal)
  demonstrating `mnemos add` and `mnemos task list`.
- README: memory loop and hook automation documented under "Connect Claude Code".

### Added
- Single `MNEMOS_DIR` workspace model (ADR 0005, Phase 2): one anchor from which
  every location derives — `kb/` (the knowledge base: tree root, URI namespace,
  write boundary), `kb/capture/`, `state/index.db`, `models/`, `mnemos.toml`.
  Resolved by precedence: `--config` > `--mnemos-dir` > `$MNEMOS_DIR` > project
  `./.mnemos` (discovered up to the git root) > the `~/.mnemos` default.
- `mnemos add <source> [--into <subpath>] [--mode copy|link]`: brings external
  content into the knowledge base and indexes it — the managed-store entry point.
  `copy` (default) snapshots; `link` symlinks a single file. (In-place directory
  linking is deferred to the external-source feature.)
- `mnemos init --global` initializes `~/.mnemos`; bare `init` creates a
  project-local `./.mnemos`.
- `mnemos migrate --from <old> [--to <dir>] [--move]`: relocates a pre-MNEMOS_DIR
  workspace into the `kb/` layout and reindexes (copy by default; the source is
  left intact unless `--move`).
- Root flag `--mnemos-dir`; `status` now reports the effective workspace (anchor,
  resolution source, kb root, index db).

### Changed
- MCP tool responses are serialized once on the wire instead of twice. Previously every
  response was emitted as both `structuredContent` and an identical mirrored `text` block;
  handlers now build the result explicitly and default to a single text block (the form
  the client already surfaces to the model), halving response bytes. A new
  `[mcp].result_mode` (`text` default, or `structured`/`both`) selects the shape; an
  unknown value is rejected at config load. Registering handlers with an untyped output
  also drops the advertised per-tool output schema (intended: it removes the hook the SDK
  used to auto-mirror, and trims the tool-surface token cost).
- Workspace resolution adds a `$CLAUDE_PROJECT_DIR` rung (precedence 4, after
  `$MNEMOS_DIR` and before the cwd walk-up): an unpinned `mnemos serve` in a Claude
  Code session resolves the session's project instead of leaking the global `~/.mnemos`.
  It fails closed: a signalled project with no `.mnemos` binds to its canonical
  uninitialized location so an absent database reports "run mnemos init" rather than
  silently serving the global KB. Bare invocations with no `$CLAUDE_PROJECT_DIR` still
  fall back to the global default. See ADR 0005 (amended). MCP `roots/list` resolution
  and a degraded no-workspace serve mode are deferred follow-ups.
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

### Fixed
- `mnemos task list` now titles each task from its frontmatter `title:` (falling back to
  the heading-derived title only when absent), instead of always showing the first
  heading — so a task file whose first heading is `## Goal` no longer lists as "Goal". It
  also shows each task's collection, and filters `type`/`status` in SQL via `json_extract`
  (guarded by `json_valid`) rather than scanning every document and JSON-decoding it in
  Go. `ListFilter` gains `DocType`/`Status`.
- `mnemos.remember` to an explicit path now preserves a body's existing frontmatter
  instead of prepending a second block. Read-modify-write of a stateful document (a
  task state file, `status.md`, a decision) is lossless: the author's
  `status`/`priority`/`title` are no longer shadowed by a generated block that carried
  only `type`/`tags`/`timestamp`/`collection`, so a remembered `type: task` document
  groups correctly in `mnemos task list`. Inbox captures (no path) still generate
  frontmatter as before. New `ingest.PrepareOKF` seam; `RenderOKF` unchanged.

### Removed
- Config keys `[storage].path` and `[capture].dir`, and the layered
  `~/.mnemos.toml` + `./.mnemos.toml` discovery — superseded by the single
  `MNEMOS_DIR`/`mnemos.toml` workspace.

[Unreleased]: https://github.com/arhuman/mnemos/commits/main
</content>
