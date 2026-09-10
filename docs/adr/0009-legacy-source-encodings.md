# 9. Legacy source encodings at ingest

Date: 2026-09-10

## Status

Accepted

## Context

`internal/ingest/process.go` skips any file its `isBinary` check rejects, before hashing and parsing:

```go
func isBinary(content []byte) bool {
	return bytes.IndexByte(content, 0) >= 0 || !utf8.Valid(content)
}
```

The check conflates two distinct properties. The NUL clause is a genuine binary marker. The `!utf8.Valid` clause was added to catch NUL-free binaries (PDF streams, images matched by an over-broad include glob), and it does, but it also rejects every legacy single-byte text encoding: Windows-125x and ISO-8859-x place text characters at bytes >= `0x80` that are not valid UTF-8 sequences.

[Issue #35](https://github.com/arhuman/mnemos/issues/35) reports the consequence against a real corpus of pre-Unicode Delphi sources. A Windows-1250 `.pas` file, containing no NUL bytes and decoding to entirely valid Romanian text, is skipped with `ingest skip binary file`: the same log line and the same verdict as a NUL-bearing binary form file. Re-encoding the identical text to UTF-8 makes it ingest. Encoding alone decides the verdict, and the log gives the operator no way to tell the two rejection causes apart.

Corpora that can be normalized upstream have a workaround (convert to UTF-8 before ingest, which the reporter uses). The gap is real for corpora that cannot be re-encoded at the source: a vendored legacy tree, a read-only checkout, or one where the byte encoding is load-bearing for another toolchain.

Three constraints frame the decision:

- **The NUL rejection must survive.** Reducing `isBinary` to a NUL-only check would admit NUL-free binary blobs, which the current check correctly rejects. The reporter explicitly rules this out, and so do we.
- **Mis-decoding is a data-quality risk.** cp1250, cp1252 and ISO-8859-2 are byte-compatible over ASCII and mutually ambiguous above it. A wrong guess produces plausible-looking mojibake that is indexed, embedded, and returned by search as if correct. The current skip is at least fail-safe.
- **Legacy encodings are a property of a corpus, not of a file.** Whoever operates the ingest knows their tree is Delphi-era Windows-1250; nothing in the bytes reliably says so.

## Decision

### 1. Declare charsets explicitly in config, per glob

A new array-of-tables under `[indexing]` maps globs to charsets. Files matching a rule are decoded from that charset to UTF-8 before hashing, parsing and chunking:

```toml
[[indexing.encoding]]
match = ["**/*.pas"]
charset = "windows-1250"

[[indexing.encoding]]
match = ["**/*.dfm"]
charset = "windows-1252"
```

```go
// EncodingRule declares the charset of files matching Match. Decoding runs
// after the NUL-byte binary check and before hashing, so the content stored,
// hashed and chunked is always UTF-8.
type EncodingRule struct {
	Match   []string `koanf:"match"`
	Charset string   `koanf:"charset"`
}
```

Rules are ordered and **first match wins**, so a per-directory rule placed above a per-extension one overrides it. The globs go through the existing `doublestar` matcher on the root-relative path, the same predicate `include`/`exclude` use (`internal/ingest/scanner.go`).

`match` is deliberately not named `include`: these globs select an encoding, they do not widen discovery. A `.pas` file must still appear in `indexing.include` to be ingested at all.

Charset names resolve through `golang.org/x/text/encoding/ianaindex`, which accepts IANA names and their registered aliases (`windows-1250`, `ISO-8859-2`, `latin2`). An unresolvable charset is a **config load error**, not a per-file ingest failure: the operator learns at startup, not after a partial index.

One deviation from a pure passthrough, found during implementation: the IANA registry has **no `cpNNNN` alias**, so `cp1252` (the spelling operators most often reach for, and the one this ADR and the config docs advertise) is rejected by every `ianaindex` index. `internal/encoding.canonicalName` maps `cp1250`-`cp1258` to their `windows-NNNN` equivalents before lookup; every other name passes through untouched. The alternative was to document only the `windows-` spelling, rejected as a needless papercut.

### 2. The NUL check runs first, unconditionally

Binary detection splits into its two constituent parts, in order:

1. A NUL byte anywhere is binary. Always. No charset declaration overrides it.
2. Otherwise, if a rule matches, decode from the declared charset; if the decode fails, skip the file.
3. Otherwise, the file must be valid UTF-8, exactly as today.

A declared charset therefore relaxes the UTF-8 requirement and nothing else. Binary Delphi form files, which begin with `TPF0` and are dense with NULs, stay rejected under a `.dfm` rule; that rule only ever applies to the text-form variant.

### 3. Hash the decoded UTF-8, not the raw bytes

`hashContent` is applied to the post-decode content. Changing a rule's charset therefore changes the hash, and affected files re-ingest on the next pass instead of being hash-skipped with the previous mis-decode still in the index. This makes a charset correction self-healing.

### 4. Distinguish the skip reasons in the log

`ingest skip binary file` splits into a NUL-marker skip and a `ingest skip non-UTF-8 file` skip naming the offending offset, so an operator can tell a genuine binary from a file that needs an encoding rule. This is worth doing on its own merits, independently of the rest.

## Consequences

### Positive

- Legacy corpora that cannot be normalized upstream become ingestible, without weakening binary rejection.
- Decoding is explicit and auditable: the charset is in config, reviewable, and diffable. No heuristic decides what a file means.
- A wrong charset is recoverable: fix the rule, the hash changes, the files re-ingest.
- The two rejection causes stop being indistinguishable in the log.
- Default behavior is unchanged: with no `[[indexing.encoding]]` rules, ingest is UTF-8-only exactly as before.

### Negative / risks

- A *plausible but wrong* declared charset still yields mojibake, now with the operator's explicit blessing rather than a fail-safe skip. Decision #3 bounds the damage to "recoverable", not "impossible".
- `golang.org/x/text` is promoted from indirect to direct in `go.mod`. It is already in the module graph at v0.41.0 with `encoding/ianaindex` present, so this adds no module to the tree.
- Config grows a section. Kept inside `[indexing]` rather than at top level, since it is a discovery/read concern.
- Content stored in the index no longer round-trips to the on-disk bytes for decoded files. Documents hold the UTF-8 text, not the original encoding. Acceptable, since search and embedding operate on text, but it means the index is not a byte-faithful copy of a legacy tree.

## Alternatives considered

- **Attempted legacy decode as a last resort when NUL is absent** (issue #35, option 3). Requires no config, which is its whole appeal. Rejected: with no declaration there is nothing to distinguish cp1250 from cp1252 from ISO-8859-2, all of which "succeed" on the same bytes and disagree about what they mean. It would silently index mojibake and be indistinguishable, downstream, from a correct decode. The failure is invisible, which is worse than the current visible skip.
- **A single global `--encoding` / config option** (issue #35, option 2). Simpler, and adequate for a homogeneous corpus. Rejected as the primary mechanism because a mixed tree (UTF-8 markdown alongside legacy `.pas`) would need it applied per-glob anyway; the global form is the degenerate case of a rule matching `**/*`, so nothing is lost by starting with the general shape.
- **A flat map, `[indexing.encoding]` with `"**/*.pas" = "windows-1250"`.** Terser. Rejected because TOML maps carry no defined ordering, so overlapping globs would resolve unpredictably, exactly when a per-directory override is what the operator needs.
- **Reducing `isBinary` to a NUL-only check.** Admits NUL-free binary blobs the current check correctly rejects. Explicitly ruled out by the reporter and by us.
- **Declining, and documenting UTF-8-only prominently** (issue #35, option 4). A legitimate outcome: the reporter frames the whole request as conditional on legacy corpora being in scope, and has a working upstream-normalization workaround. Rejected because the cost here is bounded (one config section, one existing dependency promoted) and the alternative asks every legacy-corpus user to maintain a parallel converted tree. Decision #4 (the log split) is retained from this option regardless, since it is the part that helps even under a UTF-8-only policy.

## References

- Issue: [#35: Windows-1250 source files are skipped as non-UTF-8 during ingest](https://github.com/arhuman/mnemos/issues/35).
- Implementation: `internal/encoding/` (`Lookup`, `Decoder.Decode`, `canonicalName`), `internal/ingest/encoding.go` (`EncodingRule`, `encodingRules.decoderFor`), `internal/ingest/process.go` (`Pipeline.textContent`, `hasNUL`, `firstInvalidUTF8`), `internal/ingest/pipeline.go` (`WithEncodings`), `internal/ingest/watcher.go` (`WatchConfig.Encoding`), `internal/config/config.go` (`EncodingRule`, `validate`, `defaultTOML`), `internal/cli/encoding.go` (config-to-ingest adapter).
- Tests: `internal/encoding/encoding_test.go`, `internal/config/encoding_test.go`, `internal/ingest/encoding_test.go` (the latter reproduces the issue's four-file scenario and asserts the decoded text, not just the file count).
- Documentation: `docs/configuration.md` ("Legacy source encodings"), `docs/paths-and-indexing.md` ("Running it more than once").
