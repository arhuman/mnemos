# 8. Memory trust model (provenance, review, confidence, enforcement)

Date: 2026-08-03

## Status

Proposed

## Context

mnemos commits an agent-authored memory as ground truth the instant an agent asserts it.
`mnemos.remember` writes the OKF file to disk and indexes it immediately; the only gate is the
boolean `allow_write` capability (`internal/memory/write.go:62-158`). There is no notion of who
wrote a memory, how much it should be trusted, or whether it has gone stale. The only metadata a
document carries is a raw `timestamp`, mapped to `modified_at` at parse time
(`internal/parse/frontmatter.go:61-66`); frontmatter extraction captures no author and no trust
signal.

The consequence is recall pollution. Over a long-lived workspace the index fills with
agent guesses, superseded decisions, and one-off observations that are indistinguishable, at
recall time, from facts the user stated directly. The recall path already surfaces a caveat to
consumers that a memory "reflects what was true when written," but nothing stands behind that
caveat: there is no field to read, no way to down-weight an unverified claim, and no way to
retire a contradicted one. Graph-assisted retrieval compounds this: `GraphRetriever` injects
1-hop neighbors of the top seeds when the base retriever under-fills the limit
(`internal/search/graph.go:29-102`), so a low-trust neighbor can be smuggled into an otherwise
conservative result set.

Two constraints shape the design:

- `status` is already taken. OKF `Task` documents use a `status` field
  (`in_progress|todo|done`) purely for grouping in `task list` (`internal/cli/task.go:24`,
  extracted at `:103-117`). A second, global meaning for `status` would collide with Task data
  and its grouping logic.
- A dormant retirement hook exists. `LogKind` already defines a `Deprecation` value that the
  write path never emits (`internal/okf/log.go:14-25`); a trust model can finally use it.

A multi-model design evaluation examined adding a single lifecycle or confidence field. Its
central finding: any single-field scheme (an editorial `draft -> published` lifecycle, or an
epistemic `idea -> tested -> proven -> validated -> enforced` ladder) fuses axes that are
actually independent, becomes impossible to keep honest, and rots. Trust for an agent-memory
store is not one dimension. It is four.

## Decision

### 1. Four orthogonal frontmatter fields, none named `status`

Trust is modeled as four independent, optional fields. Each has a small closed value set and a
single clear owner, so none can be silently corrupted by overloading another.

```yaml
source: user | agent:<name>                      # provenance / attribution
review: unreviewed | trusted | deprecated        # human review posture
confidence: hypothesis | established | canonical  # epistemic strength (self-reported)
enforcement: advisory | enforced                  # normativity (rules only)
```

Rationale for keeping them separate:

- **`enforcement` is not a confidence level.** A convention can be `enforced` (a CI gate) while
  being an arbitrary choice with no truth value, and a claim can be `canonical` while being
  purely advisory. Folding enforcement into a confidence ladder is a category error that would
  teach the recall consumer that "enforced implies true."
- **Review posture is not confidence.** A hand-typed user note is `trusted` the moment it is
  written yet may still be a `hypothesis`; an agent can assert something `established` that is
  nonetheless `unreviewed`.
- **Three values per axis, at most.** Metadata that a human or an agent must keep honest rots
  when it is too granular: nobody reliably distinguishes `tested` from `proven` from
  `validated`. Each axis is capped at three states with crisp transitions.

### 2. Trust is a recall-time posture, never a write-time block

The write path is not gated on review. `mnemos.remember` keeps writing and indexing
immediately (`internal/memory/write.go`), but stamps `source: agent:<name>` and
`review: unreviewed`. Writes originating from the user via CLI default to `source: user` and
`review: trusted`. Promotion from `unreviewed` to `trusted` is an explicit later step
(decision 5), not a precondition for the memory to exist.

This deliberately avoids a human-approval queue in front of writes. Agents externalize memory
orders of magnitude faster than a human can review it; a write-blocking gate produces an
unbounded backlog that is abandoned, and a hidden memory is simply re-learned on the next turn.
Friction belongs at the point of use, where it changes ranking, not at the point of capture,
where it only creates a queue.

### 3. Type-scoped fields, Task untouched

No field is mandatory on every document; each applies only where it is meaningful.

- `confidence` and `enforcement`: decision, convention, feedback, and fact memories.
- `review`: any agent-written document, with its strongest effect on high-stakes types
  (identity, credentials, decisions).
- `source`: all memories (cheapest and universally useful).
- `user` identity facts: inherently trusted because user-stated; `confidence` is noise and is
  omitted.
- `reference` pointers: provenance only; they are not truth claims, so `confidence` and
  `enforcement` are omitted.
- `Task`: left entirely alone. It keeps its existing `status` field; the trust model adds
  nothing to it and reuses none of its names.

### 4. Recall integration

The value is realized only when recall reads these fields.

- Default recall down-weights `unreviewed` (a score multiplier, not a hard cut), excludes
  `deprecated`, and surfaces `source`, `review`, and `confidence` as inline badges so the
  consuming model can hedge its answer.
- **High-stakes types hard-filter `unreviewed` by default.** A recall serving an identity or
  credential question must not answer from an unreviewed agent assertion; an explicit
  `--include-unreviewed` (CLI) / equivalent MCP flag overrides.
- **Graph expansion respects the trust threshold.** `GraphRetriever`
  (`internal/search/graph.go:29-102`) holds injected neighbors to at least the same trust bar as
  base hits, so the under-fill expansion cannot reintroduce low-trust nodes that the base query
  correctly excluded.

### 5. Automate deprecation only, never promotion

Curation is asymmetric on purpose. Removing trust is conservative and safe to automate; adding
trust is not.

- **Deprecation is automated.** A `source: agent:*`, `review: unreviewed` memory left untouched
  and unlinked for a configured age is retired: `review` becomes `deprecated`, a `Deprecation`
  entry is appended to `log.md` (the dormant `LogKind`, `internal/okf/log.go:14-25`), and the
  document drops out of the default recall scope. It is never auto-deleted.
- **Promotion is never automatic.** `unreviewed -> trusted`, and any `confidence` upgrade,
  require an explicit human action or independent corroboration. In particular, an agent
  re-asserting its own earlier claim does not promote it: that path launders a hallucination
  into a trusted fact.
- A `mnemos review` CLI (`list`, `approve`, `reject`) is the load-bearing surface. Without it
  the `unreviewed` set is write-once rot and recall silently hides good memories behind stale
  flags. It ships with the field, or the field does not ship.

### 6. Staleness is derived, not stored

There is no stored freshness enum. Staleness is computed from `timestamp` and the age at recall
time, feeding the deprecation trigger (decision 5) and an optional recall age penalty. A stored
"fresh/stale" flag would itself go stale; the timestamp is the single source of truth.

### 7. Phasing

The model lands incrementally, each phase independently useful:

1. `source` attribution, recorded on write and in `log.md`. No recall change; immediately aids
   debugging "why does this memory exist."
2. `review: unreviewed|trusted` on writes, recall down-weighting, and the `mnemos review` CLI.
3. `confidence` (three levels), type-scoping, and inline recall badges.
4. `enforcement` and the automated deprecation job, only once phases 1 to 3 prove the metadata
   is actually maintained.

## Consequences

### Positive

- Recall can finally distinguish a user-stated fact from an unreviewed agent guess, and act on
  the difference. The long-standing "reflects what was true when written" caveat gains a
  mechanism.
- Capture stays fast and unblocked: no approval queue in front of agent writes, so the
  high-bandwidth memory loop is preserved.
- Four small closed axes are each maintainable and each independently owned; overloading one
  cannot corrupt another.
- High-stakes recall (identity, credentials) refuses to answer from unreviewed memory by
  default, closing a concrete safety gap.
- The dormant `Deprecation` LogKind is put to use, and retirement is automatic and reversible
  (deprecated, not deleted).
- Zero impact on existing Task documents: no shared field names, no migration of Task data.

### Negative / risks

- Value depends entirely on the `mnemos review` UX and the deprecation job existing. Ship the
  fields without them and the `unreviewed` set rots while recall quietly suppresses good
  memories: strictly worse than no field. This is why they are bundled into the same phases.
- More frontmatter to keep honest. The type-scoping and three-value caps mitigate this, but a
  careless agent can still stamp `confidence: canonical` on a guess; only human review or
  corroboration counteracts it, by design.
- Recall gains conditional branches (trust multipliers, type-dependent hard filters, graph
  threshold). This is added complexity in the hot path, justified by the pollution it prevents.
- Self-reported `confidence` is a claim, not a measurement. It is deliberately coarse and
  advisory; a derived numeric trust score is out of scope here (see Alternatives).

## Alternatives considered

- **A single editorial `status` lifecycle (`draft -> in_review -> published -> deprecated ->
  archived`).** Rejected: it answers a publish-readiness question mnemos does not have, collides
  with the Task `status` field, and still fuses review posture with retirement into one
  overloaded axis.
- **A single epistemic ladder (`idea -> experiment -> tested -> proven -> validated ->
  enforced`).** Rejected: it fuses four orthogonal axes into one, `enforced` is normativity and
  not a confidence level, and six levels are unmaintainable. Its useful core survives, split
  into `confidence` (three levels) and `enforcement`.
- **A continuous `confidence: 0.0-1.0` float.** Rejected for v1: a hand-set float is fake
  precision no human or agent can calibrate. A numeric score is defensible only if machine
  derived (corroboration count, recall hits); that is deferred to a later, separate signal, not
  a self-reported field.
- **Renaming Task's `status` to `task_state` to free `status` for global use.** Rejected: a
  breaking change to existing OKF Task data to reclaim a name we do not need. A new,
  differently named field leaves Tasks untouched.
- **A human-approval gate in front of writes.** Rejected: it produces an unbounded review
  backlog that gets abandoned, and hiding unreviewed memory just makes agents re-learn the same
  facts. Trust is enforced at recall, not at capture.
- **Auto-promoting confidence on agent re-assertion.** Rejected: it launders a repeated
  hallucination into a trusted fact. Only deprecation is automated; promotion needs a human or
  independent corroboration.

## References

- Design evaluation: an internal multi-model synthesis of the orthogonal-axes model, the
  `enforced`-is-not-confidence finding, and the recall-time-not-write-time posture.
- Related ADR: [0006 KB health diagnostics: the `doctor` command](0006-kb-health-diagnostics-doctor.md)
  (the future `dream` consolidation and staleness sweeps that the deprecation trigger feeds).
- Implementation surfaces: `internal/memory/write.go` (`Remember`, write path and defaulting),
  `internal/parse/frontmatter.go` (new field extraction), `internal/search/graph.go`
  (trust-aware graph expansion), `internal/okf/log.go` (`Deprecation` LogKind), a new
  `mnemos review` CLI, and the `mnemos.remember` MCP handler.
