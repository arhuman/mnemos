---
okf_version: "0.1"
---

# Aurora — project memory

The living memory of a small fictional project (aurora, a telemetry REST
API), kept as an OKF bundle. This is the layout the `mnemos-okf` skill
maintains for an agent: durable state in canonical documents, tasks as
first-class `Task` documents, and a journal of consolidation passes —
instead of a pile of raw notes.

Ingest it and the state becomes searchable, citable, and groupable:

```
mnemos ingest examples/project-memory/bundle --collection aurora
mnemos task list
```

## Folder map

- [status.md](status.md) — where the project stands right now
- [constraints.md](constraints.md) — durable constraints agents must respect
- [/decisions/](decisions/index.md) — one file per decision, with rationale
- [/tasks/](tasks/index.md) — current state per task; history in sibling files
- [/journal/](journal/index.md) — dated, append-only log of memory passes

See [log.md](log.md) for the change history.
