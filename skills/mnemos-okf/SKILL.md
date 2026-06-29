---
name: mnemos-okf
description: >-
  Local-first agent memory for the current project, served by the `mnemos` MCP
  server (and the `mnemos` binary) over the project's OKF tree. Use in three
  cases. (1) RECALL — the user references a decision, preference, fact, name,
  value, file, endpoint, or convention that is NOT in the current conversation
  and may have been stored before (phrasings like "as we decided", "the usual",
  "that config", "remember when", "what was X", or any detail assumed-known but
  absent): search memory with `mnemos.search`/`mnemos.context` BEFORE answering
  from assumption. DEFAULT: if the user asks a project-specific factual question
  (a value, name, path, decision, endpoint, convention) whose answer is not
  already in this conversation, search memory FIRST — even with no cue phrase and
  even if you think you already know. (2) CAPTURE — the user states something
  durable worth keeping
  (a decision, preference, project fact, gotcha, convention) or says "remember
  this / note that / save this": persist it with `mnemos.remember`. (3) OKF —
  the user mentions OKF, "knowledge bundle", okfy, validate/convert/structure
  knowledge for agents, or wants to list/browse the knowledge tree. When unsure
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
- **Cite** what you used (`uri`, line range) so the user can verify.
- If search returns nothing relevant, say the memory has nothing on it — do not
  fabricate a remembered answer.
- Don't over-search: skip it for things already in the conversation or for
  general knowledge that was never project-specific.

## 2. CAPTURE — persist durable facts (conservative, gated)

When the user states something worth remembering — a decision, preference,
project fact, gotcha, or convention — or explicitly says "remember / note /
save this":

- `mnemos.remember(type, text, [collection], [tags], [path])`. Pass an explicit
  `path` (e.g. `adr/0003-rule-engine.md`) to place it in the OKF tree; omit it
  to auto-name under `capture_dir`.

Guardrails:
- **Be conservative.** Capture durable, reusable facts — not transient chatter,
  not things already written elsewhere in the repo, not secrets. When in doubt,
  ask "is this worth recalling next week?"; if no, don't capture.
- `mnemos.remember` requires `[mcp] allow_write = true`. If the tool isn't
  available, tell the user to enable write-back rather than failing silently.
- Captured content is secret-scanned by the engine before it is written.
- Prefer one clear fact per note; add `tags`/`type` so it ranks and filters well.

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

## Notes

- **OKF credit.** The Open Knowledge Format originates with the upstream
  [GoogleCloudPlatform/knowledge-catalog/okf](https://github.com/GoogleCloudPlatform/knowledge-catalog/tree/main/okf)
  project; this skill follows that format. See `references/spec-v01.md`.
- This skill is Claude Code-specific and optional. The `mnemos` MCP server works
  with any MCP client without it — the skill just encodes *when* to reach for the
  tools so memory is used without being asked.
- Read defaults to safe: `allow_write`/`allow_delete` are off until the project's
  `.mnemos.toml` opts in. Don't assume write access; check, then act or advise.
