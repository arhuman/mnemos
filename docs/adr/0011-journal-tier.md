# 11. The session journal is a second tier, not more kb

Date: 2026-09-24

## Status

Proposed

## Context

mnemos holds what a project knows. It does not hold what happened in a session:
the request that prompted a change, the constraint the user stated in passing,
the approach that was tried and abandoned. That material is real project memory,
and today it exists only in the harness transcript, which no tool indexes and
nobody reads twice.

Capturing it would give mnemos the working-memory tier that systems like
mnemosyne provide through a BEAM-style hot/cold split. The appeal is concrete:
a session that ends mid-task could resume from what was actually said, and
`mnemos.remember` would stop being the only path for a fact to survive a context
clear.

The risk is specific to mnemos, and it is existential rather than cosmetic. The
product claim is that every result is a document a human would cite; the README
leads with it and `mnemos eval` measures it. Transcripts are the opposite of
citable: high volume, low density, and full of the model's own wrong guesses,
including guesses the conversation went on to correct. Indexed as ordinary kb
documents they would compete with ADRs in the same result set, and they would
win often, because they are numerous and they repeat the query's own vocabulary
back at it. An agent asking "why did we choose this architecture" would retrieve
the moment it speculated about the architecture, cite it faithfully, and be
wrong.

Systems whose memories are opaque rows do not face this, because nobody cites a
row. mnemos faces it precisely because its memories are files a human opens.

Two existing mechanisms look like they might already solve it, and neither does:

- **`[security].visibility.deny`** hides a collection from every query surface:
  search, context, read, list and task alike (`internal/memory/service.go`,
  `read.go`, `list.go`). Applied to a journal collection it would make the
  entries unreadable and unpromotable, which defeats the purpose of capturing
  them.
- **`kb/capture/`** is where `mnemos.remember` writes, and it is fully indexed
  and fully searchable. It is the inbox for durable facts an agent chose to
  record, not a log of everything that was said. Routing transcripts there would
  put the noise directly into the surface this ADR exists to protect.

## Decision

**Session exchanges are captured into a journal tier that is excluded from
search by default, and reaches the knowledge base only by promotion.**

Four parts:

### 1. The journal is OKF on disk, under the kb root

Entries are OKF markdown files with frontmatter, written under a dated
subdirectory of `kb/journal/`. They are not a new storage engine, not an opaque
table, and not a separate database.

This follows the same reasoning as ADR-0010's refusal to hide superseded
documents: the reason mnemos can cite is that memory is a file a human can open.
A journal entry that a human cannot read, diff, or delete with ordinary tools
would be the one part of the system that has to be trusted rather than inspected.
It also means promotion is a file operation, and that `jj`/`git` already provide
history, review and deletion for the journal exactly as they do for the kb.

### 2. Exclusion is a document class, never a path prefix

The journal is marked in the index as a distinct class, and the search path
excludes that class by default. Exclusion keys on the stored flag, not on the
uri starting with `journal/`.

A path check is the obvious implementation and the wrong one: it breaks silently
the moment someone renames the directory, moves a subtree with `mnemos mv`, or
adds a second journal root. The failure mode is the dangerous direction, since
a journal that stops being recognised starts appearing in cited results.

A caller may opt in explicitly (an `include_journal` parameter on the query,
surfaced as a flag on the CLI and a parameter on `mnemos.search`). The default
is off on every surface.

### 3. Excluded from search, never from read

`mnemos.read`, `mnemos.list` and the promotion path see journal entries
normally. Only ranked retrieval excludes them.

This is the distinction that rules out `visibility.deny`, which hides content
from every surface at once. The journal is not secret and not untrusted; it is
merely not citable. Making it unreadable would prevent the consolidation that is
the entire reason to keep it.

### 4. Promotion is the only path into the kb, and it is a write

Journal content becomes citable only when CONSOLIDATE promotes it into a real
kb document. Promotion writes a new document and stamps the source entry as
promoted, so a second pass does not re-promote the same material.

Nothing promotes automatically. An LLM summarising a transcript into a decision
is exactly the operation that invents a decision nobody made, and the resulting
document would be indistinguishable from one the user wrote. Promotion is
therefore a deliberate act under the existing `allow_write` gate, journaled like
every other consolidation pass, with conflicts surfaced rather than merged.

## Consequences

**Two tiers mean two places to look, and that cost is real.** A user who
remembers something was discussed must now know whether it was consolidated.
`mnemos.list` over the journal and the `include_journal` opt-in are the
mitigations; neither makes the split invisible.

**The journal grows without bound.** Rank decay (ADR-0010's sibling, this
plan's P1) does not apply, because excluded entries are not ranked at all.
Nothing in this decision deletes anything: pruning is an explicit,
human-initiated command, deferred to its own phase. A memory that deletes
itself on a timer is not compatible with the trust model ADR-0008 established,
and a journal directory that grows is a disk-space problem, which is the
cheaper of the two failures.

**Capture must be conservative at the source.** Because entries are excluded
rather than filtered by quality, a noisy journal costs disk and promotion
attention rather than citation quality. That makes volume the thing to control
at write time: one condensed entry per turn, not a verbatim transcript. The
capture mechanism is P7's decision, not this one, but this ADR is what makes
verbatim capture unnecessary.

**Secret scanning applies unchanged.** Journal entries pass the same
`internal/security` scan as `mnemos.remember`, because a transcript is a likelier
place for a pasted credential than a hand-written note. A scan failure drops the
entry rather than failing the session.

**An excluded document is still indexed.** The entries occupy the same tables
and cost the same ingest work as kb documents; only ranking skips them. This is
deliberate: promotion, listing and future journal-scoped search all need the
index, and a separate unindexed store would have to grow its own query path.

## Alternatives considered

**Index journal entries normally and demote them by score.** Rejected. It is
the mechanism ADR-0010 chose for supersession, and the reason it fits there and
not here is volume: one superseded ADR among twenty current ones ranks where its
demoted score puts it, but ten thousand journal entries at any finite weight will
occupy a top-N eventually, and the weight that prevents it is indistinguishable
from exclusion. Demotion manages a minority; exclusion is the honest name for
what a majority tier needs.

**Reuse `[security].visibility.deny` on a journal collection.** Rejected: it
hides from read and list as well as search, which makes consolidation impossible.
Its purpose is confidentiality, and a journal is not confidential.

**Store exchanges outside the kb in an opaque table.** Rejected: it would make
the journal the only part of mnemos a human cannot open, and it would need a
parallel query path for promotion and listing.

**Promote automatically with a local LLM.** Rejected: it manufactures decisions
nobody made, and the output is indistinguishable from a user-written document
once it lands in the kb.

## References

- ADR-0008 (memory trust model): the recall-time-posture principle, and the
  reason nothing here deletes memory on a timer.
- ADR-0010 (supersession semantics): demotion as the mechanism for a minority of
  stale documents, contrasted above with exclusion for a majority tier.
- `internal/memory/service.go`, `read.go`, `list.go`: the `visibility.deny`
  application points this decision deliberately does not reuse.
- Plan phases P4 (storage), P6 (search exclusion), P7 (capture), P8
  (promotion), P9 (pruning) implement this.
