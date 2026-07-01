---
type: Reference
title: Stash work in progress
description: Shelve uncommitted changes to get a clean tree, then restore them later.
resource: https://git-scm.com/docs/git-stash
tags: [stash, work-in-progress, switch]
timestamp: 2026-07-01T08:00:00Z
---

# Stash work in progress

A stash shelves your uncommitted changes and hands you a clean working tree,
so you can switch branches or pull without committing half-done work. The
shelved changes wait on a stack until you bring them back.

Push the current changes onto the stash stack:

```
git stash push
```

Restore them later with `git stash pop` (apply and drop) or `git stash apply`
(apply and keep). `git stash list` shows everything currently shelved.

## Gotcha

By default a stash ignores **untracked** files, so a brand-new file is left
behind in your tree and not shelved. Add `--include-untracked` when you want
those swept into the stash too.
