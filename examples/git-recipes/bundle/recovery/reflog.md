---
type: Reference
title: "Reflog: recover lost commits"
description: Find and restore commits orphaned by a reset, rebase, or deleted branch.
resource: https://git-scm.com/docs/git-reflog
tags: [reflog, recovery, reset, orphan]
timestamp: 2026-07-01T08:00:00Z
---

# Reflog: recover lost commits

The reflog records where `HEAD` and each branch tip has pointed, even after a
hard reset, a botched rebase, or a deleted branch. As long as the commit is
not yet garbage-collected, the reflog is how you find its hash again.

List recent positions, then `git checkout` or `git branch` the hash you want
back. To expire stale reflog entries yourself rather than wait for git to
expire them during garbage collection:

```
git reflog expire
```

## Gotcha

The reflog is **local and per-clone**: it never travels with a push or a
fetch. A fresh clone has an empty reflog, so recover the commit before you
re-clone, not after.
