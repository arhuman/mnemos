# 5. Single `MNEMOS_DIR` anchor for all state and content location

Date: 2026-06-30

## Status

Accepted (implementation phased — see Consequences). Phase 1 (tree-root anchoring
of capture/ingest) and Phase 2 (the `MNEMOS_DIR` model, `mnemos add`, and
`mnemos migrate`) have landed. Ingestion now also honors `collection:` frontmatter
so a re-index preserves collections. Remaining: making URIs fully kb-relative
(today they stay relative to the scan root within the kb), and the `index-only`
external-source mode (Phase 3).

Supersedes the location half of the layered-config model in
[0002](0002-okf-tree-write-delete-move.md) (the confinement validator and the
write/delete gates from 0002 are retained unchanged).

## Context

mnemos currently derives three location concepts from one another by relative
resolution: **config** (`--config`, or `~/.mnemos.toml` + `./.mnemos.toml`) →
**tree root** (the `--config` directory, or the cwd) → **storage/capture**
(`[storage].path`, `[capture].dir`, resolved against the tree root). This chain is
elegant for a single in-project KB but brittle elsewhere, and it has produced a
family of footguns documented in `KB_LOCATION.md` and evaluated against two
external models in `.claude/doc/mnemos-dir-unification.md`:

1. **cwd-MCP**: an MCP server's working directory is not guaranteed by the client,
   so a relative `[storage].path` followed the wrong directory. The current
   mitigation forces an **absolute `--config`**.
2. **Absolute `capture.dir` rejected**: `serve` refuses any absolute `[capture].dir`
   even when it resolves *inside* the tree root — a false positive that broke a real
   configuration (`~/MEMORY/mnemosAI.toml`).
3. **scan root ≠ tree root**: `ingest <path>` derives URIs relative to the scan-root
   argument, not the tree root, so ingesting a subdirectory yields short URIs that
   do not match the on-disk tree-root-relative path (`ls` reports them
   "not indexed"), and two trees sharing a relative path collide.
4. **Ingest is unguarded**: plain `ingest` has no tree-root awareness (unlike
   `okfy`, which confines via `internal/security/paths.go`), so external content can
   be indexed into dangling URIs that `read`/`ls`/`move` cannot resolve.

Compounding this, the path vocabulary has proliferated to ~10 surface terms for ~4
real concepts (`tree root`/`project root`/`workspace root`; `storage.path`/
`StorageDir`/`dbDir`; `capture.dir`/`captureDir`; `scan root`), and the home layout
is inconsistent: `~/.mnemos/models/` is a **directory**, while the home config is
the `~/.mnemos.toml` **dotfile**.

The user's actual deployment (`~/MEMORY`, a single personal cross-project KB) is the
topology these footguns hit hardest, and it is reached today only by abusing the
`--config` mechanism.

## Decision

Collapse the three location concepts into **one anchor, `MNEMOS_DIR`**, from which
everything else is **derived and non-configurable**. The path vocabulary reduces to
two terms: **`MNEMOS_DIR`** and **`kb`**.

### 1. Directory layout

```
$MNEMOS_DIR/            # default ~/.mnemos/ ; overridable by flag/env; or ./.mnemos (project)
  mnemos.toml          # the single config file (behaviour only — no location keys)
  kb/                  # THE tree root: everything URI-addressable lives here
    capture/           # mnemos.remember writes here by default (URI: capture/...)
    <content>          # URI = path relative to kb/
  state/
    index.db           # SQLite+FTS — derived state, reconstructible by `reindex`
  models/              # embedding models (already at ~/.mnemos/models today)
```

- **tree root = `$MNEMOS_DIR/kb`** (not configurable).
- **capture = `$MNEMOS_DIR/kb/capture`** — a reserved name *inside* the tree root,
  never a sibling (a sibling would put captured notes outside the URI namespace,
  invisible to `ls`/`read`/`move`).
- **All URIs resolve relative to `$MNEMOS_DIR/kb`.** There is one URI namespace.
- **DB and models live in `MNEMOS_DIR` but outside `kb/`**, so they are never in the
  URI namespace, never indexed, and not subject to `move`/`forget`.

### 2. Config loses all location keys

`[storage].path` and `[capture].dir` are **removed**. The "path vs dir" distinction
(a file vs a directory) disappears with the keys themselves. `mnemos.toml` keeps
only behaviour: `[indexing]`, `[chunking]`, `[search]`, `[mcp]`,
`[capture].defer_to_watcher`, `[security]`.

A single, **flag-only** escape hatch `--kb-root <path>` covers atypical layouts
(KB on a separate disk, migration). No location key returns to TOML — two TOML keys
"just in case" is explicitly rejected (see Alternatives).

### 3. Resolution precedence (project mode stays first-class)

1. `--mnemos-dir <path>` (explicit flag) — wins.
2. `$MNEMOS_DIR` (environment variable).
3. **Project mode**: a `./.mnemos/` discovered from the cwd — discovery is **cwd-only
   or stops at the git root**, never an unbounded walk-up.
4. **Global mode** (fallback): `~/.mnemos/`.

The global default is a **fallback**, not a replacement: the per-project KB remains
the primary topology when a project anchor is present. `mnemos status` always prints
the effective `mode`, `mnemos_dir`, `kb` root, and `index.db` path, so "it works but
on the wrong KB" cannot pass silently.

### 4. Ingestion becomes a managed store

Because every URI resolves from `kb/`, content must live under `kb/`. Ingestion
moves from "index files in place" to "put content into the store":

- **`mnemos add <src> [--into <kb-subpath>] [--mode copy|link] [--collection name]`**:
  `--mode copy` (default) copies `<src>` into `$MNEMOS_DIR/kb/<subpath>` then
  indexes; `--mode link` symlinks instead (zero-copy, for indexing a repo without
  duplicating it). URI is always relative to `kb/`. Idempotent by content hash.
  `--into` is **required** when `<src>` is a directory (or otherwise risks a URI
  collision); a future option may auto-namespace (e.g. `repos/<org>/<repo>/`).
- **`mnemos.remember` / `okfy` / `forget` / `move`**: unchanged in spirit; all act
  within `kb/`, already confined by `internal/security/paths.go`.
- **`mnemos watch`** watches `kb/` only. External sources synced manually.
- **`mnemos ingest`**: kept for one deprecation cycle as a **restricted** alias that
  accepts only paths *inside* `kb/` (out-of-tree paths error with guidance), then
  removed in favour of `add`.
- **`index-only` external sources** (register an absolute origin path in the DB,
  namespaced URIs, no copy) are deferred to a later phase — they add a real surface
  (list/remove/reindex source) and are not needed for the core decision.

This kills footguns 1–4 by construction: no out-of-tree ingest, no scan-root/tree-root
divergence, no cwd dependence, and the absolute-`capture.dir` key no longer exists.

## Consequences

### Positive

- One mental model and two terms (`MNEMOS_DIR`, `kb`) replace ~10 path terms.
- The cwd-MCP footgun is gone: `serve` anchors on `MNEMOS_DIR`, never the cwd.
- URI collisions and dangling URIs are eliminated by construction.
- Backups and reindexing are clean: `kb/` is the source of truth, `state/index.db`
  is reconstructible and excluded from the namespace.
- Layout is consistent: `~/.mnemos/` is one directory holding config, kb, state, and
  models — the `~/.mnemos.toml` dotfile inconsistency is retired.

### Negative / risks

- **Breaking change** to the config schema, the CLI (`ingest` → `add`), and the home
  layout. Requires a migration path and a compatibility window.
- **Philosophy shift**: "mnemos indexes my files in place" → "I put content into
  mnemos". Indexing a large repo's `docs/` without copying now requires
  `add --mode link` (symlink) rather than in-place ingest. Must be documented in the
  README headline.
- **Content duplication** in `--mode copy` for large corpora (mitigated by
  `--mode link` and content-hash dedup).
- **Discovery surprise**: a bare command in an unrelated directory now hits the
  global KB instead of erroring. Mitigated by the mandatory `mnemos status` banner.

### Implementation phasing

1. **Phase 1 (non-breaking)**: move location validation into `Config.Validate(treeRoot)`
   called by `app.Load`; accept an absolute `capture.dir`/`storage.path` that resolves
   *inside* the tree root (fixes footgun 2 immediately for the existing `~/MEMORY`
   config); confine `ingest` to the tree root like `okfy` (fixes footgun 4).
2. **Phase 2 (the model)**: introduce `MNEMOS_DIR`, derived `kb/`/`state/`/`models/`,
   drop `[storage].path`/`[capture].dir`, add `mnemos add` and `mnemos migrate`,
   update `docs/paths-and-indexing.md` and the README.
3. **Phase 3 (optional)**: `index-only` external sources if in-place repo indexing
   becomes a hard requirement.

### Migration

`mnemos migrate --from <old-config-or-root>` creates `$MNEMOS_DIR/{kb,state,models}`,
**moves** (not copies) the old tree-root content under `kb/` (capture into
`kb/capture/`), carries over non-location settings into `mnemos.toml`, and reindexes.
The legacy `~/.mnemos.toml` and `[storage].path`/`[capture].dir` keys are read with a
deprecation warning for one cycle, then ignored. For the `~/MEMORY` case: either
`MNEMOS_DIR=~/.mnemos` with content moved under `~/.mnemos/kb/`, or `--kb-root ~/MEMORY`
as the documented escape hatch.

## Alternatives considered

- **Patch only, keep the layered model.** Apply Phase 1 and stop. Fixes the acute
  footguns but leaves the vocabulary sprawl, the dir-vs-dotfile inconsistency, and the
  scan-root/tree-root divergence. Rejected as a stopping point; adopted as Phase 1.
- **Global-only (drop project mode).** Maximal simplification, but it changes the
  philosophy and destroys per-repo isolation with no ergonomic alternative (flagged by
  the GPT-5.2 evaluation). Rejected: project mode stays first-class, global is the
  fallback.
- **Keep `[storage].path` and `[capture].dir` as escape hatches.** Reintroduces the
  exact inconsistency and the `serve` validation footgun being removed. Rejected in
  favour of a single flag-only `--kb-root`.
- **DB inside `kb/`.** Would risk indexing/exposing the SQLite file and force
  `move`/`forget` to special-case internal state. Rejected: `state/index.db` lives
  outside the namespace.
- **Unbounded directory walk-up for project discovery.** Convenient but surprising
  (`~/Downloads/foo` would inherit `~`'s config). Rejected for cwd-only / stop-at-git-root.

## References

- Design analysis: `KB_LOCATION.md` (footgun catalogue, vocabulary table).
- Multi-model evaluation: `.claude/doc/mnemos-dir-unification.md` (+ `-gemini.md`,
  `-qwen.md`).
- Previous ADR: [0002: Writing to the full OKF tree](0002-okf-tree-write-delete-move.md)
  (confinement validator and write/delete gates, retained).
- Implementation touchpoints: `internal/config/config.go` (schema, `Resolve`),
  `internal/app/app.go` (`Load`, tree-root resolution), `internal/cli/ingest.go`
  (→ `add`), `internal/cli/serve.go` (drop the absolute-`capture.dir` rejection),
  `internal/security/paths.go` (confinement, reused by `add`).
