# 4. Move failure semantics (file and directory)

Date: 2026-06-28

## Status

Accepted

## Context

[ADR 0002](0002-okf-tree-write-delete-move.md) introduced moving files within the OKF tree, since extended to **directories** (moving an entire subtree with re-indexing of each document under the new prefix). All the logic is centralized in `internal/ingest/move.go` (`MovePath`), called by the `mv` CLI command and the `mnemos.move` MCP tool.

A move combines a filesystem operation (rename) and several SQLite transactions (delete-old + ingest-new per document). These two worlds are not atomic together. A multi-model review noted two weaknesses:

1. For a directory, if re-indexing failed mid-loop, the old version aborted everything and left the remaining documents pointing (in the index) to a path that no longer existed: **phantom URIs**.
2. `moveOneFile` renamed *before* un-indexing, whereas `moveDir` un-indexed *before* the rename: two inconsistent orderings for the same operation.

## Decision

### 1. Un-index before renaming (both paths)

`moveOneFile` and `moveDir` un-index all the old URIs **before** the `os.Rename`. Re-indexing (which reads the file at the new location) necessarily comes after. Consequence: if a later step fails, the index never contains a phantom URI pointing to the old path. The worst possible state is a file present on disk but not yet indexed: a **benign and replayable** state (a watcher, or a simple re-ingestion, catches it up).

Accepted trade-off: if the `rename` itself fails after the un-index, the file remains at the old location but un-indexed. This is judged preferable to phantom URIs, and a `rename` failure (a single system call, parent already created) is far rarer than a re-indexing failure.

### 2. "Best-effort" re-indexing for directories

In `moveDir`, the re-indexing loop no longer aborts on the first failure: a file that fails is logged and recorded, then the loop continues. Thus a single corrupted file cannot leave all the following ones un-indexed. The failed files remain on disk (already moved) and un-indexed: a benign, replayable state. Failures are reported as an **aggregated error** afterwards, listing the affected URIs, while still keeping the actually-moved entries in the result.

### 3. Report the number of orphaned inbound links

Rewriting inbound markdown links remains deferred (V0 limitation, see ADR 0002). In the meantime, the number of inbound links left orphaned is now **reported to the caller**: `dangling_links` field in the `mnemos.move` output, and an explicit warning in the `mv` CLI output. The user therefore knows immediately that a manual search/repair may be necessary.

## Consequences

### Positive

- No more phantom URIs in the index after a move, regardless of the failure point.
- Both paths (file, directory) have the same ordering semantics, easier to reason about.
- An isolated failure on one file does not block the move of the rest of a directory.
- The user is informed of orphaned inbound links instead of discovering them later.

### Negative / risks

- If the `rename` fails after the un-index, existing files end up temporarily un-indexed (replayable, but worth knowing).
- The actual rewriting of inbound links remains undone (deferred to a future "graph" version).

## Alternatives considered

- **Rename before un-indexing.** Protects the (rare) rename-failure case, but exposes the (frequent) re-indexing-failure case that leaves phantom URIs. Rejected: the benign failure mode (un-indexed file) is preferable to the corrupted failure mode (phantom URI).
- **Abort on the first re-indexing failure (previous behavior).** Simple, but a single error can leave all the rest of a directory un-indexed. Rejected in favor of best-effort + aggregated error.
- **Rewrite inbound links now.** Desirable but it is a real feature (searching the sources, markdown rewriting, re-ingestion); deferred. Reporting the count is the intermediate step retained.

## References

- Previous ADR: [0002: Write/delete/move on the OKF tree](0002-okf-tree-write-delete-move.md).
- Implementation: `internal/ingest/move.go` (`MovePath`, `moveOneFile`, `moveDir`), `internal/cli/mv.go`, `internal/mcp/tools_move.go`.
- Review: `.claude/doc/repo-evaluation-10x.md` (recommendations #2 and #3).
