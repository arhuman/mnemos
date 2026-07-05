---
type: Task
title: Rate-limit the ingest endpoint
description: Add a global token-bucket rate limit to POST /ingest.
status: in_progress
priority: high
created: 2026-07-01
updated: 2026-07-04
started: 2026-07-02
tags: [aurora, ingest, v0.2]
timestamp: 2026-07-04T17:30:00Z
refs:
  - ../constraints.md
---

# Rate-limit the ingest endpoint

## Goal

`POST /ingest` refuses excess load with `429` instead of degrading; the
50 ms p99 [constraint](../constraints.md) holds under the reference load
test.

## Next action

Wire the token bucket into the ingest middleware and re-run the benchmark.

## Definition of done

- Global token bucket, capacity and refill rate configurable.
- `429` responses carry `Retry-After`.
- Before/after benchmark attached to the history.
