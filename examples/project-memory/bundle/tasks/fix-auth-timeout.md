---
type: Task
title: Fix auth token timeout
description: Tokens expired mid-request under clock skew; fixed with leeway.
status: done
priority: high
created: 2026-06-30
updated: 2026-07-02
started: 2026-07-01
completed: 2026-07-02
tags: [aurora, auth, bug]
timestamp: 2026-07-02T14:00:00Z
---

# Fix auth token timeout

## Goal

Requests no longer fail with `401` when the client clock drifts a few
seconds from the server.

## Outcome

Added 30 s validation leeway on `exp`/`nbf`. Root cause: strict equality on
expiry with skewed client clocks. Regression test covers ±29 s drift.
