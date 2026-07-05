---
type: Decision
title: No ORM, plain SQL
description: Data access is hand-written SQL behind a narrow store interface.
tags: [decision, storage, sql]
timestamp: 2026-07-03T15:00:00Z
---

# No ORM, plain SQL

**Decision.** All data access is hand-written SQL behind one `Store`
interface; no ORM or query builder.

**Rationale.** The schema is five tables and the hot path is
performance-sensitive ([constraint](../constraints.md): 50 ms p99). An ORM
would hide the exact queries we need to keep tight.

**Rejected.** GORM and sqlc — both fine tools, both more machinery than
five tables justify.

**Sources.** Consolidated 2026-07-04 from two capture notes
(capture/2026-07-02-orm-discussion.md, capture/2026-07-03-sqlc-eval.md);
see the [journal entry](../journal/2026-07-04-consolidation.md).

**Status.** Accepted 2026-07-03. Supersedes nothing.
