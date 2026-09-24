# 10. Supersession semantics (demote, never hide)

Date: 2026-09-24

## Status

Proposed

## Context

ADR-0008 named this gap while solving a different one: the index has "no way to retire a
contradicted one," so a decision that has been replaced is indistinguishable, at recall time,
from the decision that replaced it. It introduced `review: deprecated` as a human review
posture, but a deprecated document says only "a human stopped vouching for this." It does not
say what replaced it, and nothing in the index links the two.

That link is the missing piece. A knowledge base whose whole product claim is a citation has a
specific failure mode: an agent asks "why did we choose this architecture", retrieves the ADR
that argued for X, cites it faithfully, and is wrong, because the project moved to Y eighteen
months ago. The citation is real, the line range is correct, and the answer is stale. No amount
of retrieval quality fixes this, because the retrieval is working exactly as designed.

A `superseded_by` frontmatter key naming the replacement uri is the obvious representation, and
it is already the convention the repo follows by hand: `docs/adr/0007-link-rewrite-on-move.md`
carries an `## Amendment` section rather than a machine-readable pointer, and CLAUDE.md
instructs "update superseded ADRs' status rather than deleting them". Naming the replacement
settles nothing on its own, though. The decision is what retrieval does when it finds one.

Three candidate behaviours, in increasing destructiveness:

- **Demote.** The superseded document still matches, still returns, still cites, but ranks below
  its replacement and carries the replacement's uri in the result.
- **Hide.** The superseded document is filtered out of results unless a caller explicitly opts
  in, the way `.mnemos/` internals are excluded today.
- **Delete.** Supersession removes the old document, on the theory that the replacement contains
  everything worth keeping.

The tension is real in both directions. Demotion means a stale document can still surface and
still be miscited, which is the exact failure the feature exists to prevent. Hiding prevents
that but makes the history unreachable through the tool that is supposed to hold it.

## Decision

**A superseded document is demoted in ranking. It is never hidden and never deleted.**

Three parts:

### 1. `superseded_by` holds a uri, and the result carries it

A document may carry `superseded_by: <uri>` in its frontmatter, naming the document that
replaces it. Retrieval resolves that uri and attaches it to the result, so a caller that cites a
superseded document always receives, in the same payload, the pointer to what replaced it. An
agent is then able to follow it without a second query; a human sees it in the citation.

A `superseded_by` naming a document that does not exist resolves to `false` and is reported as
unresolved, mirroring how `mnemos.related` already reports dangling outbound links
(`internal/search/graph.go`). A dangling pointer degrades to "this is superseded, target
unknown", never to an error and never to silently dropping the marker.

### 2. Demotion is a ranking multiplier, not a filter

The superseded document's score is multiplied by a constant factor below 1, applied on the same
path as the recency factor introduced in this plan's P1. This makes supersession composable with
recency instead of competing with it: a superseded document is discounted whether it is old or
new, and an undated one is still discounted, because supersession is a fact about the document's
standing rather than about its age.

A multiplier rather than a filter, for the same reason ADR-0008 put trust at recall time rather
than write time: friction belongs where it changes ranking, not where it removes information.

### 3. One hop, and cycles are reported rather than followed

Resolution follows `superseded_by` exactly one hop. A chain (A superseded by B, B superseded by
C) reports B from A, not C.

Following a chain transitively would be strictly more useful in the common case and strictly
more dangerous in the uncommon one: a cycle, which a hand-edited frontmatter key makes
reachable, would hang retrieval or require cycle detection on the hot path for a case that is
always a data error. One hop is decidable in constant time, and a caller that wants the end of
the chain can follow it explicitly, seeing each step. A self-reference (`A superseded_by A`) is
reported as unresolved rather than obeyed.

## Consequences

**A stale document can still be retrieved and still be miscited.** This is the accepted cost.
Demotion lowers the odds; it does not eliminate them. The mitigation is that the replacement
pointer travels with the result, so an agent that reads the payload it was given has what it
needs to correct itself. An agent that ignores the pointer will cite the stale document, and no
ranking choice prevents that.

**History stays citable, which is the point.** "Why did we choose Y over X" is answerable only
while the argument for X is still retrievable. Hiding superseded ADRs would make the knowledge
base unable to explain its own decisions, and deleting them would contradict ADR-0008's trust
model, which keeps contradicted memories in place and adjusts their recall posture instead.

**`superseded_by` is a claim, not a verified fact.** Nothing checks that the replacement
actually covers the same ground. It carries the same trust posture as any other frontmatter
under ADR-0008: an agent can write one, and being agent-written is visible via `source:`.

**Demotion interacts with the graph retriever.** `GraphRetriever` injects 1-hop link neighbors
of top seeds when the base retriever under-fills the limit. A superseded document reachable as a
neighbor is injected with its seed's decayed score; the demotion factor applies to it there too,
so supersession cannot be laundered by arriving through the graph path.

**Two writes are needed to supersede, and only one is enforced.** Marking A `superseded_by: B`
does not make B aware of A. A backlink is derivable at query time from the existing link index
and is deliberately not stored, to avoid two fields that can disagree.

## Alternatives considered

**Hide superseded documents behind an opt-in flag.** Rejected: it defends against miscitation by
making history unreachable by default, and the default is what agents get. The repo's own
convention (CLAUDE.md: "update superseded ADRs' status rather than deleting them") already
encodes the opposite preference.

**Reuse `review: deprecated` from ADR-0008 instead of a new field.** Rejected: they answer
different questions. `deprecated` is a human review posture meaning "no longer vouched for";
`superseded_by` is a structural pointer meaning "replaced by specifically this." A document can
be deprecated with no replacement, and replaced while never having been reviewed. Folding them
would repeat the category error ADR-0008 explicitly rejected when it refused to collapse
`enforcement` into `confidence`.

**Store a `supersedes` backlink on the replacement.** Rejected as redundant: it is derivable
from the forward key, and two independently-writable fields encoding one relationship drift
apart the moment a human edits only one of them.

**Follow the chain transitively to its end.** Rejected: unbounded work on the hot path and a
hang on a cycle, for a convenience a caller can implement explicitly when it wants it.

## References

- ADR-0008 (memory trust model): the recall-time-posture principle this follows, and the
  `review: deprecated` field this deliberately does not reuse.
- `internal/search/graph.go`: dangling-link reporting, the precedent for unresolved targets.
- Plan phase P2 implements this; P1 provides the scoring path the demotion multiplier joins.
