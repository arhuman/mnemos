---
type: Decision
title: SQLite as the storage engine
description: Telemetry storage uses embedded SQLite, not a server database.
tags: [decision, storage, sqlite]
timestamp: 2026-07-01T09:00:00Z
---

# SQLite as the storage engine

**Decision.** Aurora stores telemetry in embedded SQLite (WAL mode), one
database file per deployment.

**Rationale.** The deployment target is a single box with no operator; the
[single-binary constraint](../constraints.md) rules out a database server.
Write volume (~200 rows/s peak) is well within SQLite's envelope.

**Rejected.** Postgres (operational burden, violates the constraint);
flat append-only files (no queryability for the export endpoints).

**Status.** Accepted 2026-07-01. Supersedes nothing.
