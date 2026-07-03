# 6. KB health diagnostics: the `doctor` command

Date: 2026-07-03

## Status

Accepted

## Context

Mnemos has a mature **read side** (search, context, list, retrieval `eval`) but no
**write-side gardening**. Over time an OKF tree accumulates entropy: byte-identical or
near-duplicate documents, oversized files that dilute retrieval, orphaned inbound links
after moves ([ADR 0004](0004-move-failure-semantics.md) reports their count but does not
repair them), inconsistent tags (casing, plurals, single-use typos), and related notes that
were never linked. Nothing today surfaces this.

The broader ambition is a consolidation capability — provisionally `dream` — that would
*reorganize* the KB the way sleep consolidates memory: merge duplicates, split large files,
normalize tags, link related memories, prune stale ones. A multi-model evaluation
(`.claude/doc/dream-consolidation-command.md`) reached three conclusions that shape this ADR:

1. **Separate diagnosis from mutation.** A read-only analyzer is ~80% of the value at ~5% of
   the risk, has no blocker, and defines the problems a later `dream` would fix. It should
   ship first and stand alone.
2. **Deterministic detection belongs in Go; judgment belongs in the agent.** Finding
   duplicates, walking the link graph, and computing size histograms are deterministic. Which
   duplicate is canonical, where to split, and how to re-tag are judgment calls for the LLM
   agent (over MCP), not logic to hardcode in the binary.
3. **All mutation is blocked or gated.** Any operation that moves/splits/removes content
   changes a document's URI — which is its identity (`hash(collection + "\0" + uri)`), its
   on-disk locator, and its citation target. Inbound links are not rewritten on move
   (V0 limitation, [ADR 0002](0002-okf-tree-write-delete-move.md)), so mutation corrupts the
   link graph until a link-rewrite capability exists.

This ADR covers **only the read-only `doctor` command**. Mutation (`dream`), link-rewriting,
and stable document IDs are deliberately out of scope and recorded as future work.

## Decision

### 1. Add a read-only `doctor` command and a matching MCP tool

`mnemos doctor [path]` walks the OKF tree (confined via `security.ConfineDir`, like `ingest`)
and prints a **health report**. It performs **no writes** — no `allowCreate`, no mutation
primitives. A parallel MCP tool (`mnemos.doctor`, always-on since read-only) returns the same
findings as structured data so the agent can act on them with existing guarded-write
primitives (`move`/`remember`/`okfy`/`forget`).

Both are thin adapters over a new `memory.Service` method (e.g. `Diagnose(ctx, opts)
[]Finding`), consistent with the existing CLI/MCP-over-service pattern.

### 2. Deterministic detectors only, emitting structured findings

`doctor` runs a fixed set of deterministic checks. Each produces a machine-readable `Finding`
(category, severity, affected URIs, evidence, suggested action) — never a free-text opinion —
so agent follow-up is validated against real data:

| Detector | Basis | Notes |
|---|---|---|
| Exact duplicates | `content_hash` (xxhash, already stored) | Groups of byte-identical docs |
| Near-duplicates | vector similarity over existing embeddings + threshold | Candidate clusters only; ranking left to agent |
| Oversized documents | size / chunk-count histogram | Flags split candidates |
| Broken / orphan links | link-graph walk over stored `links` | Reuses the `dangling_links` accounting from ADR 0004 |
| Tag hygiene | frontmatter tag frequency | Case/plural variants, single-use (likely-typo) tags, co-occurrence |
| Structural gaps | frontmatter/body scan | Empty-body notes, missing frontmatter, missing `index.md` (OKF W4) |

Detectors read from the existing index and storage layer; they add no new persisted state.

### 3. `doctor` suggests actions but never applies them

Each finding may carry a suggested action ("canonicalize A→B", "split C at H2 boundaries",
"normalize tag `Project-A`→`project-a`"). Applying any suggestion is out of scope: it is the
job of a future `dream` (for gated/destructive ops) or of the agent (for judgment ops via
MCP). This keeps `doctor` safe to run anywhere, anytime, including in CI.

### 4. Frame findings around retrieval, not file hygiene

Where possible a finding states its retrieval impact ("these near-duplicate chunks rank for
the same queries") so its value is measurable with the existing `eval` command, rather than
being cosmetic tidiness.

## Consequences

### Positive

- Immediate, safe, standalone value: users can see KB entropy without any risk of mutation.
- No blocker: `doctor` is entirely read-only, so it does not wait on link-rewriting or
  stable IDs.
- Establishes the `Finding` vocabulary and detector infrastructure that a later `dream`
  consumes, so the risky feature is built on a proven, tested base.
- Same findings available to human (CLI) and agent (MCP) with no divergence in logic.

### Negative / risks

- `doctor` describes problems it cannot fix; users may expect a `--fix`. This is intentional
  and must be clearly communicated (fixing is future `dream` work).
- Near-duplicate detection is threshold-sensitive; a poor default produces noise. Mitigated
  by treating near-dup findings as *candidates* and keeping the threshold configurable.
- Detector cost on large trees (embedding comparisons). Mitigated by making expensive
  detectors opt-in via flags.

## Future work (explicitly out of scope here)

- **`dream` (consolidation/mutation):** offline, idle-time, **proposal-based** (dry-run
  default, machine-readable plan, `--apply` to commit, git working-tree required as
  undo/snapshot), **idempotent** (re-running an applied state yields no new proposals),
  **scoped** (`--path`/`--tag` to bound blast radius), with an immutable audit trail linking
  KB state → proposal → approval. Its first, *unblocked* operations should be **additive**:
  linking related-but-unlinked memories and generating Maps-of-Content index notes (no URI
  change). Destructive ops (dupe collapse, merge, split) are gated on the item below.
- **Link-rewrite-on-move** ([ADR 0002](0002-okf-tree-write-delete-move.md) V0 limitation):
  the hard prerequisite for any destructive consolidation. Its own ADR.
- **Stable, location-independent document IDs** (frontmatter UUID/CUID, links referencing the
  stable ID rather than the URI): the strategic end state that makes moves pointer updates.
  A breaking change deferred to its own ADR; the link-rewrite utility becomes part of that
  migration. Chosen over introducing an alias/redirect table now, which would add hidden
  mutable state at odds with the file-based, local-first design.

## Alternatives considered

- **One `consolidate` command that analyzes and mutates.** Rejected: bundles four operations
  of very different risk/maturity; couples a safe, shippable diagnostic to a blocked,
  dangerous mutator.
- **Embed an LLM in the Go binary to decide merges/splits.** Rejected: bloats and version-
  couples the binary; judgment belongs in the agent, which already has the MCP primitives.
- **Add an alias/redirect table now to unblock moves immediately.** Rejected for the first
  iteration: hidden mutable state fighting the file-based ethos and adding link-resolution
  latency; the stable-ID-in-frontmatter approach is the cleaner long-term fix.

## References

- Multi-model evaluation: `.claude/doc/dream-consolidation-command.md` (+ `-gemini.md`,
  `-qwen.md`).
- Related ADRs: [0002 Write/delete/move on the OKF tree](0002-okf-tree-write-delete-move.md),
  [0004 Move failure semantics](0004-move-failure-semantics.md).
- Anticipated implementation: `internal/memory` (new `Diagnose` service method),
  `internal/cli/doctor.go`, `internal/mcp/tools_doctor.go`; detectors over `internal/storage`
  and existing embeddings.
