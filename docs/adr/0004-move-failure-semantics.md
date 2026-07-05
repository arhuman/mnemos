# 4. Move failure semantics (file and directory)

Date: 2026-06-28

## Status

Accepted — **amended 2026-07-05**: the ordering in Decision #1 was reversed from *un-index-before-rename* to *rename-before-un-index*. The implementation (`internal/ingest/move.go`) does rename-first; this ADR now matches it. The original decision and its rationale are preserved under [Amendment](#amendment-2026-07-05-rename-before-un-index).

## Context

[ADR 0002](0002-okf-tree-write-delete-move.md) introduced moving files within the OKF tree, since extended to **directories** (moving an entire subtree with re-indexing of each document under the new prefix). All the logic is centralized in `internal/ingest/move.go` (`MovePath`), called by the `mv` CLI command and the `mnemos.move` MCP tool.

A move combines a filesystem operation (rename) and several SQLite transactions (delete-old + ingest-new per document). These two worlds are not atomic together. A multi-model review noted two weaknesses:

1. For a directory, if re-indexing failed mid-loop, the old version aborted everything and left the remaining documents pointing (in the index) to a path that no longer existed: **phantom URIs**.
2. `moveOneFile` renamed *before* un-indexing, whereas `moveDir` un-indexed *before* the rename: two inconsistent orderings for the same operation.

## Decision

### 1. Rename before un-indexing (both paths)

> Amended 2026-07-05. The original decision (un-index before renaming) is preserved under [Amendment](#amendment-2026-07-05-rename-before-un-index).

`moveOneFile` and `moveDir` `os.Rename` **first**, then un-index the old URIs and re-index at the new path. Rationale: the most likely failure of the whole operation is the rename itself (cross-device, permissions, destination exists). Doing it first means that failure leaves the index **completely untouched** — a failed move is a no-op, never a silent drop of the document from search. Only once the file is safely at its new path is the index mutated.

Trade-off: between the rename and the un-index there is a brief window where the index still holds the old URI, now pointing at a path the file has left — a **transient phantom URI**. It is closed as soon as the un-index + re-index run; if a step fails in between, the worst state is a file on disk not yet re-indexed (or an old URI not yet removed): **benign and replayable** — a watcher or a re-run reconciles it. This window is judged preferable to the original ordering's failure mode, where a rename failure after un-indexing dropped an on-disk file out of the index entirely (invisible to search until re-ingested). Rename is the frequent failure; the SQLite un-index/re-index steps rarely fail once reached.

Both paths (`moveOneFile`, `moveDir`) use this same ordering, so the two surfaces cannot drift.

### 2. "Best-effort" re-indexing for directories

In `moveDir`, the re-indexing loop no longer aborts on the first failure: a file that fails is logged and recorded, then the loop continues. Thus a single corrupted file cannot leave all the following ones un-indexed. The failed files remain on disk (already moved) and un-indexed: a benign, replayable state. Failures are reported as an **aggregated error** afterwards, listing the affected URIs, while still keeping the actually-moved entries in the result.

### 3. Report the number of orphaned inbound links

Rewriting inbound markdown links remains deferred (V0 limitation, see ADR 0002). In the meantime, the number of inbound links left orphaned is now **reported to the caller**: `dangling_links` field in the `mnemos.move` output, and an explicit warning in the `mv` CLI output. The user therefore knows immediately that a manual search/repair may be necessary.

## Consequences

### Positive

- A failed `rename` (the most likely failure) is a clean no-op: the index is untouched and the document is never silently dropped from search.
- Any phantom URI is transient (bounded by the un-index step) and self-healing via the watcher or a re-run, rather than a persistent corrupted state.
- Both paths (file, directory) have the same ordering semantics, easier to reason about.
- An isolated failure on one file does not block the move of the rest of a directory.
- The user is informed of orphaned inbound links instead of discovering them later.

### Negative / risks

- Between the rename and the un-index there is a transient phantom-URI window (old URI in the index, file already moved). It is replayable and self-healing, but worth knowing.
- The actual rewriting of inbound links remains undone (deferred to a future "graph" version).

## Alternatives considered

- **Un-index before renaming (original 0004 decision, superseded 2026-07-05).** Eliminates any phantom-URI window, but exposes the frequent rename-failure case: a rename that fails after the un-index leaves an on-disk file dropped from the index (invisible to search until re-ingested). Superseded because a silent search drop is worse than a transient, self-healing phantom window. See [Amendment](#amendment-2026-07-05-rename-before-un-index).
- **Abort on the first re-indexing failure (previous behavior).** Simple, but a single error can leave all the rest of a directory un-indexed. Rejected in favor of best-effort + aggregated error.
- **Rewrite inbound links now.** Desirable but it is a real feature (searching the sources, markdown rewriting, re-ingestion); deferred. Reporting the count is the intermediate step retained.

## Amendment (2026-07-05): rename before un-index

The original Decision #1 was **un-index before renaming**, to guarantee the index never held a phantom URI. The implementation was subsequently changed to **rename before un-indexing**, and this ADR asserted the opposite of the code until this amendment. The reversal is deliberate and is now the accepted decision (see Decision #1 above); the trade-off was re-weighed:

- **Original priority:** never expose a phantom URI, even briefly. Cost: a rename failure *after* un-indexing drops an on-disk file out of the index — it silently vanishes from search until re-ingested.
- **Amended priority:** never silently drop a document from search on the most likely failure (the rename). Cost: a transient phantom-URI window between the rename and the un-index — bounded, replayable, and reconciled by the watcher or a re-run.

Rename-first wins because the rename is the frequent, externally-caused failure (cross-device, permissions, destination exists), whereas the SQLite un-index/re-index steps rarely fail once reached. A benign, self-healing window is preferable to a silent search drop.

**Original decision text (for the record):**

> **Un-index before renaming (both paths).** `moveOneFile` and `moveDir` un-index all the old URIs **before** the `os.Rename`. Re-indexing (which reads the file at the new location) necessarily comes after. Consequence: if a later step fails, the index never contains a phantom URI pointing to the old path. The worst possible state is a file present on disk but not yet indexed: a **benign and replayable** state (a watcher, or a simple re-ingestion, catches it up). Accepted trade-off: if the `rename` itself fails after the un-index, the file remains at the old location but un-indexed. This is judged preferable to phantom URIs, and a `rename` failure (a single system call, parent already created) is far rarer than a re-indexing failure.

The reversal shows the last clause of that original trade-off — "a `rename` failure … is far rarer than a re-indexing failure" — is what flipped: a rename failure is in fact the *dominant* failure mode, and its consequence under un-index-first (a silent search drop) is worse than the phantom window it was avoiding.

## References

- Previous ADR: [0002: Write/delete/move on the OKF tree](0002-okf-tree-write-delete-move.md).
- Implementation: `internal/ingest/move.go` (`MovePath`, `moveOneFile`, `moveDir`), `internal/cli/mv.go`, `internal/mcp/tools_move.go`.
- Review: `.claude/doc/repo-evaluation-10x.md` (recommendations #2 and #3); deepening pass `.claude/doc/review_mnemos_20260705_2/mnemos_deepening.md` (Finding 1, which surfaced the ADR/code divergence).
