---
type: Reference
title: Cherry-pick a commit
description: Copy a single commit onto your current branch without merging the rest.
resource: https://git-scm.com/docs/git-cherry-pick
tags: [cherry-pick, commit, backport]
timestamp: 2026-07-01T08:00:00Z
---

# Cherry-pick a commit

Cherry-pick copies one commit from anywhere in the history onto your current
branch. It is how you backport a single fix to a release branch without
dragging along everything else that landed on main.

If a cherry-pick stops on a conflict, resolve the files, stage them, and
then continue:

```
git cherry-pick --continue
```

You can abort instead with `--abort` to return to the state before the
cherry-pick began.

## Gotcha

A cherry-pick creates a **new** commit with a new hash, so the same change
now exists twice in history. When the branches later merge, git usually
reconciles this, but a noisy diff is the price of a cherry-pick over a
proper merge.
