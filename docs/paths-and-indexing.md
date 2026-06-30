# Paths, indexing, and writes

mnemos operates out of one anchor directory, the **MNEMOS_DIR**. Everything it
reads and writes is a fixed subpath of it; nothing is configured by path.

## The workspace layout

```
<MNEMOS_DIR>/            # default ~/.mnemos ; or a project-local ./.mnemos
  mnemos.toml           # configuration (behaviour only — no location keys)
  kb/                   # the knowledge base: tree root, URI namespace, write boundary
    capture/            # auto-named notes from `remember` (URIs: capture/<file>)
    <your content>      # everything URI-addressable lives here
  state/
    index.db            # SQLite + FTS — derived state, outside the URI namespace
  models/               # embedding models
```

The **kb** is the single source of identity: a document's URI is its path relative
to `kb/`, and every write (capture, okfy, forget, move) is confined within it. The
database and models live in the MNEMOS_DIR but **outside** `kb/`, so they are never
indexed or addressable.

## How the MNEMOS_DIR is chosen

Resolution precedence, highest first:

1. `--config <file>` — an explicit `mnemos.toml`; its directory is the MNEMOS_DIR.
2. `--mnemos-dir <dir>` — an explicit anchor.
3. `$MNEMOS_DIR` — the environment variable.
4. **Project mode**: the nearest `./.mnemos` found by walking up from the current
   directory, bounded by the git root (an unrelated parent's `.mnemos` is never
   inherited).
5. **Global default**: `~/.mnemos`.

`status` always prints the resolved anchor and how it was chosen, so a command can
never silently act on the wrong workspace. Because the anchor is absolute (or
discovered, not cwd-relative), an MCP server started in an unknown directory still
finds the right data.

## Getting content in

mnemos is a managed store: addressable content lives under `kb/`, and you put it
there explicitly.

- **`mnemos init`** scaffolds a project-local `./.mnemos`; `mnemos init --global`
  scaffolds `~/.mnemos`.
- **`mnemos add <source> [--into <subpath>] [--mode copy|link]`** brings external
  content into the kb and indexes it. `--mode copy` (default) snapshots it into the
  kb; `--mode link` symlinks a **single file** (a directory cannot be linked in
  place yet — that is the planned external-source feature). `--into` places it at a
  chosen subpath; otherwise it lands at the source's base name.
- **`mnemos ingest <kb-subpath>`** re-indexes content already inside the kb. A path
  outside the kb is refused (it would mint URIs that `read`/`ls`/`move` cannot
  resolve).
- **`mnemos remember`** (MCP/CLI) writes a note under `kb/capture/`.
- **`mnemos okfy <kb-file>`** converts an in-kb `.txt`/`.md` into an OKF document.

A document's `collection:` frontmatter is authoritative; the `--collection` flag is
only the fallback for files that don't declare one. This keeps collections stable
across re-indexes.

## URIs and citations

`search` (and `read` / `move` / `forget`) cite documents by their **URI**. Citations
look like `security/scim.md#Provisioning` with line ranges. URIs are unique
store-wide, so two files that would resolve to the same relative path collide — the
later ingest wins. Keeping all content under one `kb/` and adding it at distinct
subpaths keeps URIs distinct.

## Migrating an older workspace

Pre-MNEMOS_DIR workspaces kept content at a config-derived tree root with the DB in
`.mnemos/`. `mnemos migrate --from <old-root-or-config> [--to <dir>] [--move]`
relocates that content under `<dir>/kb` (old capture under `kb/capture`) and
reindexes. It copies by default — the source is left intact until you pass `--move`.
Original collections survive the reindex wherever documents carry a `collection:`
frontmatter.

## Running it more than once

- **Idempotent:** unchanged files are skipped by content hash; changed files are
  re-indexed in place. Re-run freely.
- **One store:** every command shares the one `state/index.db`. `watch` (and
  `forget`/`move`) also remove deletions; a bare re-`ingest`/`add` only adds/updates.
