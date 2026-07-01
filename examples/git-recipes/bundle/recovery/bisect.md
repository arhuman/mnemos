---
type: Playbook
title: "Bisect: find the breaking commit"
description: Binary-search the history to pinpoint the commit that introduced a bug.
resource: https://git-scm.com/docs/git-bisect
tags: [bisect, debugging, regression]
timestamp: 2026-07-01T08:00:00Z
---

# Bisect: find the breaking commit

When a bug appeared somewhere in the last hundred commits, bisect finds it in
a handful of steps by binary-searching between a known-good and a known-bad
commit. You test the midpoint, tell git the result, and it halves the range.

To start a session, run the command below, then mark one bad and one good
commit:

```
git bisect start
```

Mark each checkout with `git bisect good` or `git bisect bad`; git jumps to
the next midpoint. When it names the first bad commit, run `git bisect reset`
to return to where you were.

## Gotcha

Bisect is only as good as your test. If the check is flaky, a single wrong
`good`/`bad` answer sends bisect down the wrong half and blames an innocent
commit.
