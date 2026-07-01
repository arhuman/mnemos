---
type: Reference
title: Interactive rebase
description: Reorder, squash, reword, or drop your own commits before sharing them.
resource: https://git-scm.com/docs/git-rebase
tags: [rebase, interactive, history, squash]
timestamp: 2026-07-01T08:00:00Z
---

# Interactive rebase

An interactive rebase lets you rewrite a run of your own commits: squash
fixups into their parent, reword messages, reorder, or drop a commit
entirely. It is the everyday tool for cleaning up a branch before you open
a pull request.

Start an interactive rebase over the commits since you branched:

```
git rebase --interactive
```

Git opens an editor with one line per commit. Change `pick` to `squash` to
fold a commit into the one above it, `reword` to edit a message, or delete a
line to drop that commit. Save and close to apply.

## Gotcha

Only rebase commits you have **not** pushed. Rewriting shared history forces
everyone else to recover from the change. If the work is already public,
[cherry-pick](/rewriting/cherry-pick.md) a fix forward instead.
