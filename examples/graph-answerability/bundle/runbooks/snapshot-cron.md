---
title: Snapshot Cron
type: runbook
tags: [snapshot, retention, cron]
---

# Snapshot Cron

Snapshots run from a cron entry on the primary node:

- `0 */6 * * *` full snapshot every six hours to object storage.
- `*/30 * * * *` incremental delta every thirty minutes.

Retention: hourly deltas kept for two days, six-hourly snapshots for thirty days.
Verify a restore from the newest snapshot at the start of each month.
