---
type: Constraint
title: Durable constraints — aurora
description: Constraints any agent working on aurora must respect.
tags: [constraints, aurora]
timestamp: 2026-07-04T18:00:00Z
---

# Durable constraints — aurora

- **Single binary, no external services.** Deploys are `scp` + restart;
  nothing may require a queue, cache server, or managed database.
- **SQLite is the storage engine** — see
  [decision 0001](decisions/0001-sqlite-storage.md). Don't propose Postgres
  again without new evidence.
- **No ORM** — plain SQL behind a narrow store interface; see
  [decision 0002](decisions/0002-no-orm.md).
- **Ingestion must stay under 50 ms p99** on the reference box; any change
  touching the hot path needs a before/after benchmark.
