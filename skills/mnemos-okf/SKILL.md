---
name: mnemos-okf
description: >-
  Local-first agent memory for the current project, served by the `mnemos` MCP
  server (and the `mnemos` binary) over the project's OKF tree. Use in six
  cases. (1) RECALL — the user references a decision, preference, fact, name,
  value, file, endpoint, or convention that is NOT in the current conversation
  and may have been stored before (phrasings like "as we decided", "the usual",
  "that config", "remember when", "what was X", or any detail assumed-known but
  absent): search memory with `mnemos.search`/`mnemos.context` BEFORE answering
  from assumption. DEFAULT: if the user asks a project-specific factual question
  (a value, name, path, decision, endpoint, convention) whose answer is not
  already in this conversation, search memory FIRST — even with no cue phrase and
  even if you think you already know. (2) CAPTURE — the user states something
  durable worth keeping (a decision, preference, project fact, gotcha,
  convention) or says "remember this / note that / save this": persist it with
  `mnemos.remember`. (3) OKF — the user mentions OKF, "knowledge bundle", okfy,
  validate/convert/structure knowledge for agents, or wants to list/browse the
  knowledge tree. (4) RESTORE — a session starts on a project with mnemos
  memory, or the user says "where were we", "resume", "continue", "what's the
  current task": rebuild the working set from memory before doing anything
  else. (5) TASK — the user requests work (implement, fix, add, refactor,
  migrate…) or manages tasks ("todo", "mark done", "what's pending", "current
  task"): track it as a Task document, not a loose note. (6) CONSOLIDATE — the
  user says "consolidate", "clean up notes/memory", "process the inbox", or
  "dedupe captures": turn raw captures into canonical knowledge. When unsure
  whether something was memorized, search first.
---

# mnemos memory + OKF

This skill makes the agent *use* the project's mnemos memory at the right
moments. The memory lives in the project's OKF tree and is served by the
`mnemos` MCP server; the `mnemos` binary is the same engine on the CLI.

**Source of truth.** All deterministic work — retrieval, ranking, citations,
parsing, indexing, writing, validation — is done by the `mnemos` MCP tools and
the `mnemos` binary. This skill only decides *when* to call them and how to
use the result. Never reimplement search, parsing, or OKF rules by hand; call
the tool. If a tool is missing from `/mcp`, the server isn't connected — say so
rather than guessing.

## Ground rules (all modes)

- **Inbox vs canonical.** `mnemos.remember` without `path` writes to the
  capture directory — that is an **inbox** of raw notes, not knowledge.
  Canonical documents (`tasks/`, `decisions/`, `status.md`, topic docs) are
  created or updated only deliberately: by TASK, by CONSOLIDATE, or by a
  CAPTURE whose correct location is certain.
- **Never overwrite silently.** Do not overwrite, rename, or delete an
  existing file except inside the CONSOLIDATE procedure or on explicit user
  request. Stateful documents are updated via **read-modify-write**:
  `mnemos.read` the current content, edit it in context, then
  `mnemos.remember` the full updated content back to the same path. Never
  write a partial update from memory of the file.
- **Lazy structure.** Never pre-create directories or empty template files;
  a directory exists when its first real document is written. Skeletons carry
  `TODO` markers — never silently invented content.
- **Report the pass.** Modes that scan or mutate the tree (RESTORE,
  CONSOLIDATE) end with a short standardized report — `Created / Updated /
  Already present / Warnings` — omitting empty sections.
- **Write gates.** `mnemos.remember`/`mnemos.okfy` require `[mcp] allow_write
  = true`; `mnemos.move`/`mnemos.forget` require `allow_delete = true`. Read
  defaults to safe. Don't assume access; check, then act or advise enabling it.

## 1. RECALL — search before answering

Before answering anything that depends on prior project knowledge not present in
the current conversation, **search first**:

- `mnemos.search(query, [collection], [limit])` → ranked, cited results.
- `mnemos.context(query, …)` → the same hits as ready-to-read context blocks
  (`uri:start-end` → content). Prefer this when you want to read the content.
- `mnemos.read(uri | chunk_id)` → a precise chunk or a whole document.

Rules:
- Trigger on "as we decided", "the usual", "that config/endpoint/value", "what
  was X", "remember when", or any detail the user assumes you know but that is
  absent from the conversation.
- **DEFAULT (no cue needed):** if the user asks a project-specific factual
  question (a value, name, path, decision, endpoint, convention) whose answer is
  not already in this conversation, search memory FIRST — even with no cue phrase
  and even if you believe you already know. Confident-but-unverified answers are
  the main failure mode; a quick search is cheap insurance.
- **Change requests recall too:** before renaming, moving, refactoring, or
  changing configuration, search for the existing convention or decision that
  covers it.
- **Bounded recall:** at most two searches before the first response — one
  broad, one refined from the first results. Then answer, or ask the user a
  clarifying question. No search spirals.
- **Cite** what you used (`uri`, line range) so the user can verify.
- If search returns nothing relevant, say the memory has nothing on it — do not
  fabricate a remembered answer.
- Don't over-search: skip it for things already in the conversation or for
  general knowledge that was never project-specific.

## 2. CAPTURE — inbox first, conservative

Captures are raw material, not finished knowledge. When the user states
something worth remembering or explicitly says "remember / note / save this":

- `mnemos.remember(type, text, [collection], [tags], [path])`.
- **Default to the inbox:** omit `path` unless the correct canonical location
  is beyond doubt (e.g. the user dictates a decision that clearly belongs in
  `decisions/`). When in doubt, capture without `path` and let CONSOLIDATE
  promote it later.

Capture **only if** at least one holds:
- the user explicitly asks to remember it;
- a decision was made (the choice, its rationale, alternatives rejected);
- a non-obvious durable project fact appeared (path, endpoint, invariant,
  constraint, gotcha) that would change future actions;
- a durable action item appeared — then use TASK mode, not a loose note.

Hard exclusions — do **not** capture:
- anything already written in the repo (docs, code, CLAUDE.md) — cite it
  via RECALL instead;
- raw logs, stack traces, command output, transient debugging chatter;
- secrets (the engine scans, but don't rely on it);
- "maybe" notes you cannot state as durable facts.

Prefer one clear fact per note; add `tags`/`type` so it ranks and filters well.
When in doubt, ask "is this worth recalling next week?" — if no, don't capture.

## 3. OKF — structure, convert, browse, validate

- `mnemos.list(path?, collection?, type?, indexed_only?, unindexed_only?)` —
  list/browse the OKF tree, annotated with index metadata and an `indexed` flag
  (shows both stored docs and not-yet-indexed files). Use it to answer "what do
  we have under X?" or to find un-indexed files.
- `mnemos.okfy(source, [out], …)` — convert an existing `.txt`/`.md` file into a
  conformant OKF document (frontmatter + body) and index it, keeping the source
  (requires `allow_write = true`).
- `mnemos.move(from, to)` / `mnemos.forget(path)` — relocate or remove tree
  files; both re-index. Destructive, so they require `[mcp] allow_delete = true`.
  `move` reports a `dangling_links` count when inbound links are left unrewritten.
- CLI equivalents for setup and bulk work: `mnemos ingest <path> --collection
  <c>`, `mnemos ls`, `mnemos validate <bundle>`, `mnemos watch <path>`,
  `mnemos status`. Use these in a terminal for indexing or conformance checks;
  use the MCP tools mid-conversation.

When **authoring** OKF content (frontmatter `type`/`title`/`description`/`tags`,
`index.md` structure, cross-links), the judgement is yours; the engine validates
conformance via `mnemos.okfy` / `mnemos validate`.

## 4. RESTORE — rebuild the working set

At the start of a session on a project with mnemos memory, or when the user
says "where were we", "resume", "continue", "what's the current task":

1. `mnemos.context("project status")` (and/or read `status.md` if the tree has
   one) — current goal, constraints.
2. Open tasks: `mnemos task list` in a terminal, or `mnemos.list(path:
   "tasks")` then read the state files (the MCP `type` filter matches file
   extensions, not OKF document types).
3. Recent decisions and the latest journal entry: `mnemos.search`, `--since`
   on the CLI.

Then present a short **working set** — current goal, constraints, open tasks,
recent decisions — every line cited. If memory has nothing, say so and start
fresh; never fabricate a state. Don't re-run RESTORE mid-session when the
working set is already in the conversation.

## 5. TASK — tasks are documents, not sentences

Tasks are OKF documents (`type: task`) under `tasks/`, grouped by
`mnemos task list`. Statuses: `backlog | todo | in_progress | done | cancelled`.

- **Auto-create silently.** When the user requests work (implement, fix, add,
  refactor, update, migrate, optimize, build…), create the task doc **before
  starting**, without announcing it — unless: the request is a question or
  analysis with no change expected; an equivalent task is already
  `in_progress`; or the user opted out ("no task").
- **State/history split.** `tasks/<slug>.md` holds only the current state and
  stays small; `tasks/<slug>-history.md` is an append-only event log. Update
  state via read-modify-write; append history rather than rewriting it.
- **Complete deliberately.** On `done`/`cancelled`: set `completed`, append the
  history event, and — in a jj/git repo — commit the finished work.
- A durable action item mentioned in passing ("we should…", "remind me…") is a
  `backlog`/`todo` task, not a capture note.

Read [references/task-schema.md](references/task-schema.md) **when creating
task files** — it holds the frontmatter contract, history format, templates,
and the status-report format.

## 6. CONSOLIDATE — turn captures into knowledge

An explicit maintenance mode: run it when the user says "consolidate", "clean
up notes", "process the inbox", or "dedupe captures" — never silently
mid-conversation. If RESTORE reveals a bloated inbox, report it and offer a
pass; don't self-trigger.

The shape of a pass (full procedure in
[references/consolidation.md](references/consolidation.md) — read it before
running one):

1. Scope: `mnemos.list` the capture directory.
2. Triage each capture into one of five buckets: already-known / new canonical
   knowledge / task / decision / transient noise.
3. Deduplicate into a single canonical document per topic, preserving the
   source trail; link related documents.
4. Contradictions are **never** resolved by overwrite: record both claims with
   citations in a `Conflicts` section and open a review task.
5. Journal the pass (one dated entry, written once), report
   `Created / Updated / Already present / Warnings`, and commit.

`mnemos.move`/`mnemos.forget` need `allow_delete = true`; without it, still
produce canonical documents but mark processed captures `status: pending-prune`
instead of removing them.

## Notes

- **OKF credit.** The Open Knowledge Format originates with the upstream
  [GoogleCloudPlatform/knowledge-catalog/okf](https://github.com/GoogleCloudPlatform/knowledge-catalog/tree/main/okf)
  project; this skill follows that format. See `references/spec-v01.md`.
- This skill is Claude Code-specific and optional. The `mnemos` MCP server works
  with any MCP client without it — the skill just encodes *when* to reach for the
  tools so memory is used without being asked.
- Read defaults to safe: `allow_write`/`allow_delete` are off until the project's
  `.mnemos.toml` opts in. Don't assume write access; check, then act or advise.
