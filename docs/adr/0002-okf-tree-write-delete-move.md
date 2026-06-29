# 2. Writing to the full OKF tree, deletion and move

Date: 2026-06-27

## Status

Accepted

## Context

The write-back capability introduced `mnemos.remember` to capture agent notes. Writing was then strictly confined to `capture_dir`, the only writable subdirectory. The full OKF tree (root = directory containing `.mnemos.toml`) remained out of the agent's reach.

Three concrete needs emerged:

1. **Free target path**: the agent must be able to place a note at a specific location in the tree (for example `adr/0003-rule-engine.md`) rather than accepting an auto-generated name under `capture_dir`.
2. **Deletion**: an incorrect or obsolete note must be removable from the index and from disk, without manual intervention.
3. **Move**: a file must be renamable or movable within the tree while keeping the index consistent.

These three operations go beyond the boundaries of `capture_dir` and require a reliable confinement mechanism to prevent an agent (or a malformed MCP call) from escaping the tree.

## Decision

### 1. Scope extended to the full OKF tree

The three capabilities operate on the **entire OKF tree** (root = directory containing `.mnemos.toml`), not just on `capture_dir`. This lets the agent manage its memory within the project's logical structure, not in a separate drawer.

### 2. Path-confinement validator (first guard of its kind in the project)

Every operation that accepts a caller-supplied path (MCP or CLI) goes through a validator (`internal/security/paths.go`) that:

- rejects `..` traversals and absolute paths outside the root;
- resolves symlinks on the deepest existing ancestor, then re-checks confinement;
- forbids access to the internal `.mnemos/` directory;
- rejects any path matching a `[security].exclude` glob.

This validator is the central piece that makes the scope extension safe. It is called by both the MCP tools and the CLI commands.

### 3. Two distinct configuration gates

- `[mcp] allow_write` (existing): gates write-back (`mnemos.remember`).
- `[mcp] allow_delete` (new, default `false`): gates destructive operations (`mnemos.forget`, `mnemos.move`). Distinct from `allow_write` on the principle of least privilege: an agent may be allowed to write without being allowed to delete or move.

When `allow_delete = false`: the corresponding MCP tools are not registered or advertised to the client, and the CLI commands refuse with an explicit message ("set [mcp].allow_delete=true to enable").

### 4. Operations

- **`mnemos.remember` / `mnemos remember`**: gains an optional `path` field (relative to the tree root, must end in `.md`). When provided, the file is written at that exact location and indexed; when absent, behavior is unchanged (auto name under `capture_dir`). The secret-scan always runs. Requires `allow_write = true`.
- **`mnemos.forget` / `mnemos forget <path>`**: removes the file from disk and un-indexes it (storage cascade). Idempotent: forgetting an absent file is not an error (output `deleted=false`). Requires `allow_delete = true`.
- **`mnemos.move` / `mnemos mv <src> <dst>`**: renames the file on disk and reindexes it under the new path. The document's collection is preserved. Since the document identifier is derived from `collection+uri`, a move is internally a delete-old-uri + ingest-new-uri (the id changes). Requires `allow_delete = true`.

### 5. Interaction with the watcher

If a watcher is monitoring the tree, a deletion or move via the tools is also detected by the watcher (delete-old/add-new): a redundant but idempotent effect. The tools update the index directly, so they also work without a watcher.

## Consequences

### Positive

- The agent can manage its memory (write, correct, reorganize) without manual intervention, within the project's logical structure.
- The path-confinement validator is explicit, reusable by any future operation on the tree.
- The `allow_write` / `allow_delete` separation respects the principle of least privilege and does not implicitly enable destructive operations on existing configurations.

### Negative / risks

- **Inbound links not rewritten (V0 limitation)**: during a move, markdown links pointing to the old path (`links.dst_doc` in the database) are not updated automatically. The number of orphaned inbound links is logged as a warning. Link rewriting is deferred to a future "graph" version.
- `allow_delete = false` by default means that enabling `allow_write` alone is not enough for `forget`/`move`; the operator must explicitly enable both if both are desired.

## Alternatives considered

- **Restrict forget/move to `capture_dir` only.** Avoids extending the scope, but makes structured memory management impossible (the agent cannot manage ADRs or notes in their natural location). Rejected.
- **Merge `allow_delete` into `allow_write`.** Simplifies the config, but implicitly grants the destructive right to any existing write-enabled configuration. Rejected: least surprise and least privilege take priority.
- **Delegate forget/move entirely to the watcher.** The watcher would detect disk changes. But this forces an active watcher, which goes against the "tools work without a watcher" design. Rejected.

## References

- Architecture: [docs/architecture.md](../architecture.md) (`cli`, `mcp`, and `security` packages).
- Previous ADR: [0001: Embeddings cgo-free](0001-embeddings-cgo-free.md).
- Implementation: `internal/security/paths.go` (confinement validator), `internal/mcp/tools_remember.go`, `internal/mcp/tools_forget.go`, `internal/mcp/tools_move.go`.
