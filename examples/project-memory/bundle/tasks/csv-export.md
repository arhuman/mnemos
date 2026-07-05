---
type: Task
title: Add CSV export
description: Export telemetry query results as CSV from GET /export.
status: todo
priority: medium
created: 2026-07-03
updated: 2026-07-03
tags: [aurora, export, v0.2]
timestamp: 2026-07-03T11:00:00Z
---

# Add CSV export

## Goal

`GET /export?format=csv` streams query results as RFC 4180 CSV.

## Next action

Decide the column set with the consumer team (blocked question in
[status.md](../status.md) does not apply here — start with the raw schema).

## Definition of done

- Streaming write, no full result buffering.
- Content-Disposition filename carries the query time range.
