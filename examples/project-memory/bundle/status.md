---
type: Status
title: Project status — aurora
description: Current goal, focus, and open questions for the aurora API.
tags: [status, aurora]
timestamp: 2026-07-04T18:00:00Z
---

# Project status — aurora

**Goal (now):** ship v0.2 — ingestion hardening. Rate limiting is the last
blocker; see [rate-limit-ingest](tasks/rate-limit-ingest.md).

**Next up:** CSV export ([csv-export](tasks/csv-export.md)), then start the
retention-policy design.

**Recently settled:** auth timeout bug fixed
([fix-auth-timeout](tasks/fix-auth-timeout.md)); storage stays on SQLite
([decision 0001](decisions/0001-sqlite-storage.md)).

## Open questions

- Retention: hard delete or archive tier after 90 days?
- Do we need per-tenant rate limits in v0.2 or is a global cap enough?
