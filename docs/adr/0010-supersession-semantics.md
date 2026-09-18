# 10. Supersession semantics (demote, never hide)

Date: 2026-09-24

## Status

Proposed

## Context

TODO: written by plan phase P3.

ADR-0008 identified the gap this decision closes: the index has "no way to retire a
contradicted one," so a superseded decision is indistinguishable at recall time from the
one that replaced it. A `superseded_by` frontmatter key names the replacement, but naming
it settles nothing on its own; what retrieval does with it is the decision.

Three candidate behaviours, in increasing destructiveness: demote the superseded document
in ranking while still returning it, hide it from results unless explicitly requested, or
delete it on supersession.

## Decision

TODO: written by plan phase P3.

## Consequences

TODO: written by plan phase P3.
