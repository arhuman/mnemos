# Task documents — contract

Read this file when creating or updating task files. Tasks live in the OKF
tree under `tasks/` and are surfaced by `mnemos task list`, which groups
`type: task` documents by their frontmatter `status`.

## State file: `tasks/<slug>.md`

Holds the **current state only** — keep it small; everything historical goes
to the history file. Update via read-modify-write (`mnemos.read`, edit in
context, `mnemos.remember` the full content back to the same path).

```markdown
---
type: task
title: Fix session cookie not cleared on logout
status: in_progress        # backlog | todo | in_progress | done | cancelled
priority: high             # low | medium | high | critical
created: 2026-07-05
updated: 2026-07-05
started: 2026-07-05        # set when status → in_progress
completed:                 # set when status → done | cancelled
tags: [auth, bug]
refs:                      # citations: decisions, code, related docs
  - decisions/0003-session-handling.md
---

## Goal
TODO — one paragraph: what done looks like.

## Next action
TODO — the single next concrete step.

## Definition of done
- TODO
```

Slug: kebab-case from the title (`fix-logout-cookie.md`). Before creating,
check `mnemos.list(type: "task")` for an equivalent open task — update it
rather than duplicating.

### Inference rules (silent auto-creation)

- `title`: concise and action-based, from the user's request ("Fix login bug",
  "Add CSV export").
- `priority`: `critical` if the user says urgent/blocker; `high` for
  fix/bug; `medium` otherwise.
- `status`: `in_progress` when work starts immediately; `todo`/`backlog` for
  action items mentioned in passing.

## History file: `tasks/<slug>-history.md`

Append-only event log — one line per event, never rewrite existing lines.
Keeping history out of the state file keeps reads cheap.

```markdown
---
type: document
title: History — fix-logout-cookie
tags: [task-history]
---

2026-07-05T14:02 | created | status=in_progress priority=high
2026-07-05T16:40 | note | root cause: cookie path mismatch
2026-07-05T17:05 | status_change | in_progress → done
```

Events: `created`, `status_change`, `priority_change`, `title_change`, `note`.

## Completing a task

On `done` or `cancelled`:
1. read-modify-write the state file: `status`, `completed`, `updated`;
2. append the `status_change` event to the history file;
3. in a jj repo, commit the finished work: `jj new -m "<task title>"`
   (in plain git, a conventional commit). One task, one commit.

## Status report format

When asked "what's pending?", "current task", or during RESTORE:

```markdown
## Project status — <project>

**In progress** (N)
- <title> [priority] — next: <next action>   (tasks/<slug>.md)

**Todo / backlog** (N)
- <title> [priority]

**Completed this week** (N)
- <title>
```

Every line cites its task file. Omit empty sections. Statuses come from
frontmatter — trust `mnemos task list` / `mnemos.list(type: "task")`, don't
rescan bodies.
