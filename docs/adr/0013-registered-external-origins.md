# 13. Registered external origins are indexed in place, read-only

Date: 2026-09-28

## Status

Proposed

## Context

ADR-0005 made `kb/` both the URI namespace and the write boundary: every URI resolves from the
kb root, so content must live under it. It deferred `index-only` external sources to a Phase 3
it marked optional, gated on "if in-place repo indexing becomes a hard requirement"
(ADR-0005:206). Issue #41 argues the gate is met, and issue #39 before it supplied the
deployment: two canonical, physically independent trees (a shared specification knowledge base
and an application source tree, on different volumes with independent ownership and tooling)
that must live in one store with cross-tree retrieval, and that cannot be relocated or
duplicated for index-layout reasons. That deployment is still on v0.2.0 for this reason alone.

The shipped escape hatch does not scale. `mnemos add --mode link` symlinks a **single file**,
zero-copy, and that genuinely works: the scan stats through the link, the original stays the
source of truth, and cross-collection retrieval works because everything is in one store. A
directory is refused outright (`internal/cli/add.go:69`). A several-hundred-file specification
tree therefore means several hundred explicit `add` calls, and a file added to the tree later
is never discovered. A manual link inventory is another synchronization layer, which is what
the reporter is trying to eliminate.

The reporter's requirements, given in #41 after the single-file behaviour was clarified, are
narrower than the original framing: register one absolute directory, give it a stable URI
prefix, optionally label it with a collection, index it **read-only** in place, reindex on
demand (extending `watch` can wait), keep the confinement guard unchanged outside registered
roots, and do not keep serving a citation into a file that has disappeared. Write-back into the
origins is explicitly not wanted: the trees stay canonical under their own tooling.

Four properties of the current code shape any design here.

1. **The confinement guard rejects out-of-kb paths by resolving symlinks.**
   `checkSymlinkEscape` (`internal/security/paths.go:119-140`) resolves the deepest existing
   ancestor with `EvalSymlinks` and refuses anything landing outside the resolved root. It is
   load-bearing, not incidental: ADR-0005:169-170 credits it with killing four footguns by
   construction (no out-of-tree ingest, no scan-root/tree-root divergence, no cwd dependence,
   no absolute `capture.dir`). Any design must preserve those rather than punch a hole in it.

2. **Retrieval never touches the filesystem.** `search`, `context`, `read`, `related` and
   `list` are served entirely from SQLite: `ReadDocumentOpts` reconstructs a document from its
   chunks (`internal/memory/read.go:128-163`) and never opens the file. Only the edit family
   (`internal/memory/edit.go:98`, `:178`) and the ingest pipeline read bytes from disk. So a
   read-only origin needs **no read-side path resolution at all**, which is a much smaller
   surface than it first appears.

3. **There is no `documents.path` column.** The absolute path is always recomputed as
   `Join(kbRoot, FromSlash(uri))`, at three places that matter here:
   `internal/ingest/reindex_content.go:56`, `internal/ingest/watcher.go:222`, and (via
   `ResolveWithin`) the edit and write verbs. `documents.uri` is `UNIQUE` **store-wide**, not
   per collection, so two trees sharing a relative path collide. Namespacing is mandatory.

4. **`WalkDir` never follows symlinks** (`internal/ingest/scanner.go:95`,
   `internal/ingest/watcher.go:250`). A linked directory inside `kb/` is invisible to both the
   scan and the watcher. An external tree has to be walked as its own scan root, which the
   pipeline already supports since `Options.Root` and `Options.URIBase` are separate fields.

Separately, issue #42 records that a document whose file was deleted stays indexed and citable:
`ingest` is additive, `reindex --content` declines to reconcile by design
(`internal/ingest/reindex_content.go:29-30`), and `doctor` has no filesystem-aware detector
(`internal/doctor/doctor.go:74-88`). That is a bug in shipped behaviour, independent of this
feature, and it is fixed first. This ADR assumes its reconcile pass exists.

## Decision

### 1. An origin is a registration in the database, not a config key

A new `origins` table holds the prefix (unique), the resolved absolute path, an optional
collection label, and `registered_at` / `last_indexed_at`. ADR-0005:165-167 already chose the
database over configuration, and the config package documents itself as carrying "behaviour
only, never locations" (`internal/config/config.go:1-6`), with every path derived from
`MNEMOS_DIR` by the workspace package. A registered origin is a location, so it does not belong
in `mnemos.toml`.

The path is stored **resolved** (symlinks evaluated at registration). Registration is the one
moment where the operator states intent explicitly, so it is the right place to freeze what the
path means. A later swap of an intermediate symlink then cannot silently redirect an origin
somewhere else.

The cost is that `state/index.db` stops being purely reconstructible from `kb/`, which ADR-0005
listed as a positive consequence. `origin list --json` is the mitigation: a registration set can
be exported and replayed, so a lost database costs a reindex, not a lost configuration.

### 2. URIs are `<prefix>/<relative-path>`, and the prefix is reserved

The prefix is chosen by the operator at registration, not derived. ADR-0005:158 floated
auto-namespacing (`repos/<org>/<repo>/`); an explicit prefix is more predictable, and it never
retroactively renames existing URIs when the derivation rule changes.

Registration refuses a prefix that collides with an existing top-level entry under `kb/`, and
the write path then refuses to create anything under a registered prefix. So the namespace is
partitioned by construction, and store-wide URI uniqueness keeps working with no schema change
on `documents`. Two origins with identical relative paths no longer collide, which is the
capability #39 asked for.

### 3. Read-only is an explicit refusal in front of the guard, not a flag

The first draft of this ADR claimed the existing confinement guard already refused an origin
URI, because it resolves outside the kb. **That is wrong, and the implementation proved it.**
An origin URI is a plain relative path (`spec/note.md`); joined to the kb root it lands *inside*
the kb, so `ResolveWithin` accepts it and merely yields a path to a file that does not exist.
`forget` treats a missing file as an index-only deletion and evicts the document. Relying on
the guard alone would have made an origin's index silently deletable through a normal verb.

So the refusal is explicit and runs **before** the guard: one seam, `resolveWritable`, checks
the URI against the registered prefixes and refuses with an error naming the origin, then
delegates to `ResolveWithin` unchanged. Every write verb (`remember`, `forget`, `move`, and the
edit family) goes through it.

Read-only stays a property of the registration rather than a permission flag: there is no
`allow_origin_write` to get wrong. But it is enforced by code written for it, not inherited for
free, and a future write verb must route through the same seam. That is the cost of the
correction, and it is recorded here so the next author does not re-derive the wrong version.

Because retrieval is served from the index (context, fact 2), the read side needs no new path
resolution and no new guard. That half of the original claim held. The asymmetry is permanent
and intentional: `search`, `context`, `read` and `related` work against origins; every verb that
would write does not.

`list` is the exception on the read side, and it is left alone deliberately. It walks the kb on
disk rather than reading the index, precisely so a not-yet-indexed file is visible, so an origin
that lives elsewhere is absent from it by construction. Teaching it to walk registered roots
would mean either a second walk with different semantics or giving up the disk-is-truth property
that makes it useful. `mnemos origin list` covers what an operator actually needs here (which
trees are registered, when each was last indexed, whether its root is readable), and `search`
covers finding a document.

### 4. Indexing walks the origin as its own scan root

`origin reindex` runs the pipeline with `Root` set to the origin's absolute path and the URI
prefix supplied explicitly, rather than symlinking anything into `kb/`. This sidesteps fact 4
entirely: there is no symlinked directory to walk. It needs one new pipeline option, because
`uriRelTo` would otherwise emit `../`-shaped URIs for an out-of-kb root
(`internal/ingest/scanner.go:64-71`).

The option is threaded through `pipelineOptions` (`internal/cli/encoding.go:19`). The watcher
currently re-derives that same option set inline instead of calling it, and that duplication is
closed first so origins cannot drift between the two.

Content-hash short-circuiting already makes a reindex of an unchanged tree cheap, so "reindex on
demand" is a usable workflow for hundreds of files rather than a full re-parse each time.

### 5. A missing origin root is reported, never mass-evicted

Reindex reconciles: a file gone from the origin is evicted from the index, scoped to that
prefix. That is the eviction-on-reindex the reporter asked for, inherited from the #42 fix.

But if the origin **root** is missing or unreadable, the reindex fails with an error and evicts
nothing. An unmounted volume and an emptied tree are indistinguishable from inside the walk, and
the safe reading is that the operator would rather see an error than lose a namespace. The
condition surfaces in `origin list` and as a `doctor` finding, so it is diagnosable without
running a reindex.

`doctor` gains an `origin` category: a missing origin root, an indexed document with no backing
file, and a stale `last_indexed_at`.

### 6. `watch` stays kb-only and says so

Watching an origin is out of scope, and `watch` refuses one with explicit guidance rather than
appearing to work. The watcher is single-root per process and registers directories through a
walk that does not descend symlinks, so multi-root watching is its own change with its own
decision.

## Consequences

### Positive

- The deployment in #39 and #41 can move off v0.2.0: both trees index in place, in one store,
  with cross-tree retrieval and stable namespaces.
- The confinement guard is untouched. All four footguns ADR-0005 killed stay dead, and an
  unregistered symlink escape is rejected exactly as before.
- Read-only is structural. It cannot be misconfigured, and it costs no new code on the read path
  because retrieval never resolves a path.
- Deleted origin files stop being citable, and a broken origin is visible in `doctor` and
  `origin list` rather than inferred from missing search results.
- `add --mode link` keeps working for the single-file case it already serves; nothing is
  removed.

### Negative / risks

- **The database is no longer reconstructible from `kb/` alone.** Losing it loses the
  registration set. `origin list --json` mitigates this but does not eliminate it, and an
  operator who never exports is exposed.
- **A registered origin is a standing grant.** Its subtree is indexable for as long as the
  registration exists, and the index is served to an LLM. The secret scanner covers content
  (`[security].exclude_secrets` applies to the ingest path), but registering a home directory or
  a repository full of credentials is a foot-gun that only the operator can avoid. Registration
  should be as deliberate as the wording of its command makes it.
- **Origin content drifts silently** between reindexes. `watch` is kb-only, so a specification
  updated this morning is served from this morning's index until someone reindexes. The stale
  `last_indexed_at` finding in `doctor` is the only counter-pressure in this version.
- **A path-resolution seam is introduced but barely used.** Only the ingest and reconcile paths
  need it; the edit path deliberately does not get it (that is what makes origins read-only). A
  later author adding a disk-reading feature must decide consciously whether it resolves origins,
  and this ADR is the note telling them so.
- **One more namespace concept.** ADR-0005 traded ~10 path terms for two (`MNEMOS_DIR`, `kb`).
  This adds a third (a registered prefix), and the README headline has to carry it.

## Alternatives considered

**Link a whole directory into `kb/`.** The obvious extension of `--mode link`, and it cannot
work: `WalkDir` does not descend a symlinked directory, so the tree is invisible to both scan
and watcher, and the confinement guard would reject reads through the link. Walking the origin
as its own root avoids both problems instead of fighting them.

**Two separate workspaces, one per tree.** Available today, no code needed, originals stay put.
It removes cross-tree retrieval, because each store has its own catalogue and its own relevance
scale. The reporter states cross-tree retrieval is a first-class workflow they already use
(specification to implementation to gap analysis, and the reverse), so this is a capability loss,
not an inconvenience.

**`--mode copy` under distinct `--into` prefixes.** Also available today, and it gives
cross-tree retrieval. The cost is a real second copy of every tree that drifts from the
original, which is precisely the constraint the reporter cannot accept: the trees are canonical
under other ownership.

**Relax the confinement guard globally, or make the kb root a set of roots.** Simpler to write
and it discards the property that makes the guard worth having. A registered prefix is a
narrow, enumerable, operator-declared exception; a multi-root guard is a different security
posture adopted by accident.

**Read-write origins.** Rejected for this version, on the reporter's own answer: they do not
want `move`, `okfy` or `remember` reaching into the origins. It would also require the read
path to resolve paths, an origin-aware write guard, and a policy for moving a document across
the kb/origin boundary. Read-only is the smaller risk surface, and nothing here forecloses
revisiting it.

**Auto-derived prefixes** (`repos/<org>/<repo>/`). Fewer decisions at registration, at the cost
of URIs that change when the derivation rule does, and of a rule that has to be right for
layouts nobody anticipated. Explicit prefixes are one more argument and no surprises.

## References

- Issue #41 (this feature, and the deployment requirements), issue #39 (the URI-namespace
  motivation), issue #42 (vanished-file eviction, fixed first)
- ADR-0005 lines 12, 150-167, 186-190, 206-207 (the deferral, and the phase gate)
- `internal/security/paths.go:30` (`ResolveWithin`), `:81` (`ConfineDir`), `:119`
  (`checkSymlinkEscape`)
- `internal/ingest/scanner.go:36` (`scan`, separate `Root`/`URIBase`), `:64` (`uriRelTo`)
- `internal/ingest/reindex_content.go:29-30` (no deletion reconciliation),
  `internal/ingest/watcher.go:213` (`removeVanished`, the pass to generalize)
- `internal/memory/read.go:128` (retrieval served from the index, not the disk)
- `internal/doctor/doctor.go:70` (`Run`, index-driven detectors)
- `internal/storage/migrations/0001_init.sql:3-19` (`documents`, no `path` column, `uri` unique
  store-wide)
