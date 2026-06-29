# Paths, indexing, and writes

mnemos operates inside one project directory. Three path ideas are worth keeping
straight: where state lives, what gets indexed, and where writes go.

## Citations are relative to the scan root

`search` (and the `mnemos.read` / `move` / `forget` tools) cite documents by their
**URI = the path relative to the scan root you passed to `ingest`**, not your
current working directory. So after `mnemos ingest docs`, a hit prints as
`security/scim.md#Provisioning` (relative to `docs/`), not `docs/security/scim.md`.

## Workspace root: where state lives

`mnemos init` writes `./.mnemos.toml` and `./.mnemos/` in the current directory.
Every command auto-discovers config from `~/.mnemos.toml` then `./.mnemos.toml`
(project wins) and resolves a relative `[storage].path` (default
`.mnemos/mnemos.db`) **against the tree root**: the current directory in
auto-discovery mode, or the `--config` file's directory when you pass one. So a
bare command works from the project root, and an absolute
`--config /abs/path/.mnemos.toml` works from any working directory without needing
an absolute `[storage].path`.

Read commands (`search`, `serve`, `status`, `ls`, …) fail with an actionable error
rather than silently creating an empty database when the store is missing; run
`mnemos init` or `mnemos ingest` first. A single database file holds all
collections; there is no global/shared store.

## What gets indexed

`mnemos ingest <path>` (and `mnemos watch <path>`) scans `<path>` and indexes files
matching `[indexing].include` (default `**/*.md`, `**/*.txt`, `**/*.go`, `**/*.sql`),
minus `[indexing].exclude` (`.git`, `node_modules`, `vendor`, `dist`), minus the
`[security].exclude` globs (`**/.env`, `**/*.pem`, …) while `exclude_secrets` is on.
Binary / non-UTF-8 files are skipped. A document's identity is its
**URI = the path relative to the scan root you passed to `ingest`**:

- `mnemos ingest docs` stores `security/scim.md` (relative to `docs/`),
- `mnemos ingest .` stores `docs/security/scim.md` (relative to the project root).

Citations and the `mnemos.read` / `move` / `forget` tools all use that URI.

## Where writes go (CLI + MCP)

Write/delete operations are confined to the **tree root**: your working directory
by default (auto-discovery mode); with `--config`, the config file's directory.
Every caller-supplied path is validated before any disk operation (`..`, symlink
escape, absolute-outside-root, `.mnemos/`, and `[security].exclude` are all
rejected). Within that root:

- `remember` writes under `[capture].dir` (default `.mnemos/capture`) when you
  don't pass an explicit `path`; with a `path` it writes there. Requires `allow_write`.
- `okfy` writes the converted `.md` at `out`. Requires `allow_write`.
- `forget` / `move` act on existing tree files. Require `allow_delete`.

## Running it more than once

- **Same path + collection is idempotent:** unchanged files are skipped by content
  hash; changed files are re-indexed in place. Re-run freely.
- **One store, many runs:** every `ingest` / `watch` / `serve` shares the one
  `[storage].path` database. `watch` (and `forget`/`move`) also remove deletions; a
  bare re-`ingest` only adds/updates.
- **Different paths: mind the URI.** URIs are **unique store-wide**. If two scan
  roots contain a file with the *same relative path* (e.g. each has `index.md`),
  they resolve to the same URI and the **later ingest overwrites the earlier one**.
  To index several trees cleanly, ingest them from one common root
  (`mnemos ingest .`) so their URIs stay distinct, rather than ingesting each
  subdirectory separately. Collections are labels you filter on, not isolated path
  namespaces.
</content>
