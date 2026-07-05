# Consolidation — full procedure

Read this file before running a CONSOLIDATE pass. The goal is entropy
reduction: fewer, better documents — not summarizing for the sake of it.
A pass must never lose information silently: every merge keeps a source
trail, every removal is journaled, contradictions are surfaced, not settled.

## 0. Scope

- `mnemos.list(path: <capture_dir>)` — the inbox of raw captures. Add
  `unindexed_only` to catch files the watcher missed.
- If the inbox is empty, report `Already present: inbox clean` and stop.
- Announce the plan first: "N captures to process". Process oldest first.

## 1. Triage — five buckets

For each capture: `mnemos.read` it, extract the key entities (project,
component, decision, task, constraint), then `mnemos.search` those entities
across the tree (excluding the capture dir). Route to exactly one bucket:

| Bucket | Test | Action |
|---|---|---|
| Already known | an existing doc fully covers it | prune the capture (§4) |
| Enrichment / new knowledge | adds to or creates a topic | merge or promote (§2) |
| Task | it is really an action item | create/update a task doc (see task-schema.md) |
| Decision | a choice with rationale | create/update `decisions/<slug>.md` |
| Transient noise | no durable value | prune, with one-line justification in the journal |

## 2. Merge and promote

- **One canonical document per topic.** Prefer enriching an existing
  decision/topic doc over creating a new one; create only when nothing covers
  the topic.
- Merging: `mnemos.read` the canonical doc, integrate the capture's content
  in place (not appended as a raw blob), then `mnemos.remember` the full
  updated doc. **Preserve the source trail**: note the original capture path
  and date in the doc (e.g. a `Sources` line).
- Promoting a wholly new note: `mnemos.move(capture, canonical_path)` keeps
  the file and its identity; then read-modify-write to clean up frontmatter
  (`type`, `title`, `tags`).
- **Link the network:** after writing, `mnemos.search` for adjacent docs and
  add a short `Related` section (markdown links) both ways when it helps
  future retrieval. Isolated notes don't get recalled.

## 3. Duplicates and contradictions

- **Duplicates:** `mnemos.search(topic, limit: 8)`. If several hits state the
  same fact, fold them into the canonical doc's single statement, list the
  folded URIs under `Sources`, and prune the redundant copies.
- **Contradictions — never resolve by overwrite.** The agent is not the
  arbiter of truth:
  1. in the canonical doc, add a `## Conflicts` section: claim A (citation),
     claim B (citation), `Resolution: unresolved` plus what evidence would
     settle it;
  2. open a review task (`status: todo`) referencing both URIs;
  3. keep the conflicting capture (move it under the capture dir's
     `archive/`, don't forget it).
  When the user later rules, apply the resolution via read-modify-write and
  mark the losing claim superseded — visibly, with the date.

## 4. Pruning

- Requires `allow_delete = true`. Prefer `mnemos.move` into
  `<capture_dir>/archive/` over `mnemos.forget`; reserve `forget` for pure
  noise. Watch the `dangling_links` count `move` returns — rewrite inbound
  links it reports.
- Without `allow_delete`: leave the capture in place and read-modify-write
  its frontmatter to `status: pending-prune`, so the next authorized pass
  can finish the job.
- Never prune a capture before its canonical version exists and is indexed.

## 5. Health check (optional, on request or when the pass is small)

- Orphans: docs with no inbound links (`mnemos.list` + link edges) —
  candidates for linking or archiving.
- Stale state: `in_progress` tasks untouched for weeks, `status.md` older
  than recent decisions. Open review tasks; don't auto-fix.

## 6. Close the pass

1. **Journal** — one dated entry, written once, never edited:
   `journal/<YYYY-MM-DD>-consolidation.md` (`type: document`,
   `tags: [journal, consolidation]`) listing captures processed, docs
   created/updated, prunes with reasons, conflicts opened.
2. **Report** to the user: `Created / Updated / Already present / Warnings`
   (omit empty sections; `Warnings` carries conflicts and dangling links).
3. **Commit**: in a jj repo, `jj new -m "chore: consolidate memory <date>"`
   — the VCS diff is the audit trail of the whole pass.
