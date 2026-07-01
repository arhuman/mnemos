---
type: Reference
title: "Worktree: multiple branches at once"
description: Check out several branches in parallel directories from one repository.
resource: https://git-scm.com/docs/git-worktree
tags: [worktree, parallel, branches]
timestamp: 2026-07-01T08:00:00Z
---

# Worktree: multiple branches at once

A worktree gives you a second working directory backed by the same
repository, so you can have main checked out in one folder and a feature
branch in another — no second clone, no constant branch switching. It is
ideal for running a long build on one branch while you keep coding on another.

Create a new worktree in a sibling directory for a branch:

```
git worktree add
```

List them with `git worktree list`, and remove one you are done with using
`git worktree remove`.

## Gotcha

A branch can be checked out in only **one** worktree at a time. Try to add a
second worktree for a branch already checked out elsewhere and git refuses,
which is what keeps the two trees from fighting over the same branch.
