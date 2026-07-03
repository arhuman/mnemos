# 7. Link rewriting on move (inbound reference integrity)

Date: 2026-07-03

## Status

Proposed

## Context

Moving or renaming a document changes its URI, which is its identity
(`hash(collection + "\0" + uri)`), its on-disk locator, and its citation target. Inbound
markdown links from *other* documents are **not** updated, so every move silently breaks the
references pointing at the moved document. This was accepted as a V0 limitation in
[ADR 0002](0002-okf-tree-write-delete-move.md) and partially instrumented in
[ADR 0004](0004-move-failure-semantics.md), which counts the orphaned links
(`MoveResult.DanglingLinks`, surfaced as `dangling_links` on `mnemos.move` and as a `mv`
warning) but still leaves the repair manual.

This gap is the single hard prerequisite for any **destructive consolidation** (the future
`dream` work in [ADR 0006](0006-kb-health-diagnostics-doctor.md)): dedupe-collapse, merge,
and split all move content, and without link rewriting each one corrupts the link graph.

How links are stored today (verified in code):

- `links(src_doc TEXT, dst_doc TEXT, FK src_doc → documents.id ON DELETE CASCADE)`
  (`internal/storage/migrations/0001_init.sql:73`).
- `src_doc` is a document **id**; `dst_doc` is a **plain URI string with no foreign key**
  (`internal/storage/links.go:12`). There is `idx_links_src_doc` but **no index on
  `dst_doc`**.
- `CountInboundLinks(dstURI)` = `SELECT COUNT(*) FROM links WHERE dst_doc = ?`
  (`internal/storage/documents.go:49`). Links are (re)written per document during the ingest
  pipeline via `ReplaceLinks` (`internal/ingest/pipeline.go`, `internal/storage/links.go:14`).

The decisive constraint: **the index is derived from the markdown files.** Updating only the
`links` table would be a lie — the next re-ingest of the source file re-derives the edge from
the file body and overwrites it. A durable rewrite must therefore edit the **source markdown
files** that contain the reference, then re-index them (which repopulates `links` correctly).
That is why ADR 0004 called this "a real feature (searching the sources, markdown rewriting,
re-ingestion)."

## Decision

### 1. On move, rewrite inbound references in the source files, then re-index them

When `MovePath` relocates `oldURI → newURI` it will, after the existing rename + re-index of
the moved document:

1. Find every source document with an inbound edge to `oldURI` (query `links` where
   `dst_doc = oldURI`, resolve `src_doc` ids to their file paths).
2. In each such source **markdown file**, rewrite the link token(s) that resolve to `oldURI`
   so they resolve to `newURI`, preserving the author's link style (markdown `[text](...)`
   vs. wiki `[[...]]`).
3. Re-index each rewritten source so its `links` rows match the file on disk.

For a directory move the old→new mapping for the whole subtree is computed **up front**
(Phase 0, as ADR 0004 already does for the dangling count), then applied to every inbound
source in one pass, so a link from one moved document to another resolves to the correct new
URI.

### 2. Canonicalize links to tree-root-relative URIs

To keep rewriting tractable, the canonical stored/rewritten link form is the
**tree-root-relative URI** — exactly what `dst_doc` already holds. With this normalization:

- Moving a **target** requires only replacing `oldURI → newURI` in the inbound sources.
- Moving a **source** does **not** change its outbound links (they name absolute-in-tree
  targets, not paths relative to the source's own location).

This deliberately sidesteps the combinatorial mess of recomputing relative `../` paths on
both sides of every move. Trade-off: it may rewrite an author's relative link into the
canonical form. This is accepted, and it doubles as a stepping stone toward stable IDs (see
Future work).

### 3. Failure semantics mirror ADR 0004: best-effort, benign, replayable

Editing N other source files widens the blast radius, so it inherits the ADR 0004 posture:

- The moved document is relocated and re-indexed first (unchanged).
- Inbound-source rewriting is **best-effort**: a source that fails to rewrite or re-index is
  logged and recorded, and the loop continues — one bad source cannot strand the rest.
- The worst case for a failed source is a single **stale link** (today's default outcome, now
  the rare exception) — benign and re-runnable; a re-ingest or a `doctor` broken-link sweep
  ([ADR 0006](0006-kb-health-diagnostics-doctor.md)) catches it up. No source file is left in
  a half-written state (write is atomic per file: temp + rename).
- `MoveResult` gains `RewrittenLinks` and `FailedRewrites` counts; `DanglingLinks` is retained
  for links that could not be rewritten. Both `mnemos.move` output and the `mv` warning report
  the new tallies.

### 4. Add an index on `links.dst_doc`

Inbound lookups (`WHERE dst_doc = ?`) currently full-scan. A migration adds
`idx_links_dst_doc` so both the existing count and the new rewrite lookup are indexed.

### 5. Scope: inbound references only

Because links are canonicalized to tree-root URIs (decision 2), outbound links from the moved
document already point at unchanged absolute targets and need no rewriting. Only inbound
references (`dst_doc = oldURI`) are rewritten. `forget`/delete is out of scope for this ADR;
its inbound links become genuinely dangling and are left to `doctor` to report.

## Consequences

### Positive

- Moves and renames preserve link integrity — the precondition for destructive consolidation
  (`dream`) is met.
- Both surfaces benefit automatically: `mv` and `mnemos.move` route through `MovePath`.
- Canonical tree-root link form is simpler to reason about and eases the later stable-ID
  migration.
- Indexed `dst_doc` speeds both the existing count and the new rewrite.

### Negative / risks

- A move is no longer a near-constant-time operation: cost scales with the number of inbound
  sources (each is edited and re-indexed). Acceptable — moves are infrequent and interactive.
- Rewriting may normalize an author's hand-written relative links into canonical URIs,
  changing file contents beyond the moved document. This must be documented as expected
  behavior.
- fs edits + DB updates remain non-atomic across multiple files; the design accepts benign,
  replayable partial states rather than a heavyweight transaction, consistent with ADR 0004.
- Link-token matching must handle markdown and wiki styles, anchored (`#section`) targets, and
  URL-encoded spaces (`%20`); a missed pattern silently leaves a stale link (caught by
  `doctor`). Parser reuse (the same extraction that populates `links`) mitigates drift.

## Future work

- **Stable, location-independent document IDs** (frontmatter UUID/CUID; links reference the
  id, not the URI). This is the strategic end state from ADR 0006: once links name a stable
  id, a move is a pure pointer update and **no source rewriting is needed**. This ADR is the
  tactical solution that delivers integrity now; its rewrite machinery becomes the migration
  path for existing URI-style links when stable IDs land. Its own ADR.
- **Link rewriting on `forget`/delete** — when a document is removed (including the losing
  side of a dedupe-collapse), its inbound links cannot be redirected to a new target and
  instead need a distinct policy (redirect-to-nothing, tombstone reporting, or block-if-
  referenced). Deliberately out of scope here; deferred to its own future ADR. Until then,
  delete leaves genuinely dangling links, reported by `doctor`.

## Alternatives considered

- **Rewrite only the `links` table, not the source files.** Rejected: the index is derived
  from the markdown, so the next re-ingest re-derives the old edge and reverts the fix. Only
  editing the source is durable.
- **Keep author-relative links and recompute `../` paths on every move.** Rejected:
  combinatorial and error-prone (both source-move and target-move change relative paths);
  canonical tree-root URIs make rewriting a string substitution.
- **Introduce an alias/redirect table so old URIs keep resolving.** Rejected for the same
  reason as in ADR 0006: hidden mutable state at odds with the file-based, local-first design,
  plus added link-resolution latency and redirect chains. Stable IDs are the cleaner endgame.
- **Abort the move if it would break inbound links.** Rejected: makes reorganization hostile
  and contradicts ADR 0004's best-effort, non-blocking philosophy.

## References

- Related ADRs: [0002 Write/delete/move on the OKF tree](0002-okf-tree-write-delete-move.md),
  [0004 Move failure semantics](0004-move-failure-semantics.md),
  [0006 KB health diagnostics: the `doctor` command](0006-kb-health-diagnostics-doctor.md).
- Implementation: `internal/ingest/move.go` (`MovePath`, `moveOneFile`, `moveDir`),
  `internal/storage/links.go` (`ReplaceLinks`), `internal/storage/documents.go`
  (`CountInboundLinks`, new inbound-source listing), `internal/storage/migrations`
  (new `idx_links_dst_doc`), `internal/cli/mv.go`, `internal/mcp/tools_move.go`.
