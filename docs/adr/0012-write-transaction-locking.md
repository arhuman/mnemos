# 12. Every explicit transaction takes its write lock at BEGIN

Date: 2026-09-26

## Status

Proposed

## Context

Enabling a connection pool surfaced a defect that a single connection had kept unreachable.
`TestWatcherIndexesDirectoryMovedIn` began failing deterministically: a file nested inside a
directory moved into the watched tree was never indexed, and the watcher logged

```
watch reindex failed uri=sub/deep/two.md
  error="ingest: upsert: storage: evict superseded document: database is locked (517)"
```

SQLite error 517 is `SQLITE_BUSY_SNAPSHOT`: a transaction that already holds a read snapshot
asked to upgrade to a write lock while another transaction held one. SQLite fails that
upgrade **immediately** and deliberately. There is no wait that could succeed, because both
transactions would have to be rolled back to make progress, so `busy_timeout` does not apply
and no retry interval rescues it. This is the one `SQLITE_BUSY` variant a timeout cannot absorb.

Three facts turned a latent design issue into a live bug:

1. **The write path reads before it writes.** ADR-0010's supersession work and the fix for the
   id-mismatch bug (#39) both landed in `UpsertDocument`, which now issues
   `DELETE FROM documents WHERE uri = ? AND id <> ?` before its `INSERT`. `write()` in
   `internal/ingest/process.go` wraps that in `BeginTx(ctx, nil)`, which is a **deferred**
   transaction: the read takes a snapshot, and the write lock is requested later.
2. **Two writers can now overlap.** The watcher debounces per path, so a moved-in directory
   fires one callback per file concurrently. Before the pool, `SetMaxOpenConns(1)` serialized
   them at the connection level, which is why this never reproduced.
3. **`busy_timeout` was never the protection.** Verified directly: `busy_timeout=5000` and
   `journal_mode=wal` are correctly applied on every pooled connection, because the PRAGMAs
   travel in the DSN. The setting is right; it simply does not cover lock upgrade.

The failure was also invisible. `reindexPath` logs a warning and returns rather than failing
the watch loop, which is correct for a live event handler, and the test wires the watcher's
logger to `io.Discard`. So the only symptom was a 10-second deadline expiring on an assertion
about an unrelated-looking file.

Reduced to its essence with no watcher, fsnotify or debouncer involved: two goroutines each
running read-then-write in a deferred transaction. One receives 517. Remove the read, and both
commit.

## Decision

### 1. Transactions acquire the write lock at BEGIN

The connection DSN carries `_txlock=immediate`, so `database/sql`'s `BeginTx` issues
`BEGIN IMMEDIATE` rather than `BEGIN DEFERRED`. A second writer then blocks on `busy_timeout`
(a wait that can succeed) instead of racing to upgrade a snapshot it already holds (a wait
that cannot).

This is a property of the connection, not of each call site, so a future read-then-write
transaction inherits it without its author having to know this failure mode exists.

### 2. Explicit transactions are for writing

The cost of `BEGIN IMMEDIATE` is that a transaction opened purely to read also takes the write
lock, which would serialize readers and defeat the pool.

That cost is currently zero, and the reason is recorded here so it stays that way: all four
`BeginTx` sites write. `internal/ingest/process.go` writes a document, `internal/ingest/watcher.go`
deletes a batch of vanished documents, and both sites in `internal/search/hybrid.go` belong to
the embedding reindex path. Read paths use `QueryContext`/`QueryRowContext` on the pool
directly, outside any transaction, and are unaffected.

So: **do not open an explicit transaction to read.** A read that needs a consistent view across
several statements is the case this rule does not cover, and it needs its own decision rather
than a deferred transaction quietly reintroducing the upgrade conflict.

### 3. Writes stay funnelled where they already are

`_txlock=immediate` converts a hard failure into a wait, which is a correctness fix, not a
throughput one. It does not license concurrent writers. The ingest pipeline keeps funnelling
writes through a single goroutine, because serialized writers never contend at all, and WAL
admits one writer regardless.

## Consequences

### Positive

- The lock-upgrade failure is structurally unreachable rather than avoided by luck of timing.
- The pool can be enabled: reads proceed concurrently, which is what it was for.
- A write that contends now waits up to `busy_timeout` and succeeds, instead of dropping a
  document and leaving a warning in a log nobody reads.
- The invariant behind the fix is written down, so the next transaction author does not have
  to rediscover error 517.

### Negative

- Nothing enforces decision 2. A read-only `BeginTx` added later would serialize readers
  against writers, and the symptom would be latency under load, not a test failure. A lint
  rule or a wrapper that makes the intent explicit would close this; neither exists yet, and
  this ADR is the interim guardrail.
- Every writer now takes the write lock slightly earlier, so a long write transaction blocks
  other writers for marginally longer. Irrelevant while writes are funnelled through one
  goroutine; worth revisiting if that changes.
- `_txlock` is a `modernc.org/sqlite` DSN parameter. Switching drivers means re-establishing
  this behaviour rather than inheriting it.

## Alternatives considered

**Retry on `SQLITE_BUSY`.** The usual answer, and wrong for 517 specifically: the snapshot the
transaction holds is the thing blocking it, so retrying the upgrade without rolling back
cannot succeed. A correct retry would have to restart the whole transaction, including the
read, which means every write call site grows a retry loop and a rollback path. `BEGIN IMMEDIATE`
achieves the same ordering in one DSN parameter.

**Raise `watchDeadline`.** Makes the test pass and fixes nothing. The document genuinely fails
to index; a longer deadline only hides that behind a slower green.

**Keep `SetMaxOpenConns(1)`.** The status quo, and it does work: serialized connections cannot
contend. It also forecloses concurrent reads permanently, so a slow vector scan keeps blocking
every other query behind one connection. The defect is worth fixing properly rather than
keeping the constraint that masked it.

**Serialize writes with a mutex in `storage`.** Duplicates in application code what SQLite's
lock manager already does, and silently degrades if a second process opens the same database.

## References

- `internal/storage/sqlite.go` (DSN construction, pool sizing)
- `internal/storage/documents.go` (`UpsertDocument`, the read-then-write pattern)
- `internal/ingest/process.go` (`write`), `internal/ingest/watcher.go` (vanished-batch delete)
- ADR-0010 (supersession semantics, which introduced part of the read-before-write)
- SQLite: lock upgrade and `SQLITE_BUSY_SNAPSHOT`, <https://www.sqlite.org/rescode.html#busy_snapshot>
