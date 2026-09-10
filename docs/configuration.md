# Configuration (`mnemos.toml`)

mnemos reads a single `mnemos.toml` from the active **MNEMOS_DIR**. The effective
configuration is the built-in defaults overlaid with that file: each key the file
sets wins, and every key it omits falls through to the default, so a partial file
(or no file at all) is fine.

MNEMOS_DIR is resolved in order: an explicit `--mnemos-dir`, `--config`, or
`$MNEMOS_DIR`; otherwise the nearest project `.mnemos/` found by walking up from the
working directory; otherwise the global `~/.mnemos`. The config file is always
`<MNEMOS_DIR>/mnemos.toml`. Passing `--config <path>` names that file directly, and
its parent directory becomes the MNEMOS_DIR (and tree root).

The config carries behaviour only — indexing, chunking, search, the MCP surface,
and security — never locations: every path (index db, kb, capture, models) is
derived from MNEMOS_DIR, not configured here.

```toml
[indexing]
include = ["**/*.md", "**/*.txt", "**/*.go", "**/*.sql"]
exclude = [".git/**", "node_modules/**", "vendor/**", "dist/**"]
max_file_bytes = 4194304    # skip any single file larger than this (0 disables the cap)

# Optional: declare legacy source charsets (see "Legacy source encodings" below).
# [[indexing.encoding]]
# match = ["**/*.pas"]
# charset = "windows-1250"

[chunking]
target_tokens = 700
overlap_tokens = 80

[search]
default_limit = 12
use_vectors = false         # true => hybrid lexical+vector search (needs the embed build + a model)
graph_expansion = false     # true => fill empty result slots with the top hits' 1-hop link neighbors
graph_seed_depth = 3        # how many top hits graph expansion pulls neighbors from
graph_decay = 0.5           # score scale applied to a seed's injected neighbors (0,1]

[mcp]
transport = "stdio"
allow_write = false         # gates mnemos.remember
allow_delete = false        # gates mnemos.forget and mnemos.move
result_mode = "text"        # wire shape: "text" | "structured" | "both" (legacy double-emit)

[capture]
defer_to_watcher = false    # true => remember is write-only, a running watcher ingests

[security]
exclude_secrets = true
exclude = ["**/.env", "**/*.pem", "**/*.key", "**/id_rsa", "**/secrets/**"]

[security.visibility]
# Collections hidden from every query surface (search, context, read, list, task),
# enforced server-side. Empty by default. A hidden collection never surfaces even
# when a caller names it. Example: deny = ["perso", "epfl"]
deny = []
```

## Legacy source encodings

Ingest is UTF-8-only by default. A file that is not valid UTF-8 is skipped with
`ingest skip non-UTF-8 file`, because nothing in its bytes says which legacy
charset it is and guessing would silently index mojibake. Files holding a NUL byte
are skipped separately as `ingest skip binary file`.

Legacy corpora (pre-Unicode Delphi, Windows-125x, ISO-8859-x) can declare their
charset so those files are decoded instead of skipped:

```toml
[[indexing.encoding]]
match = ["legacy/**"]       # narrower rules first: the first match wins
charset = "windows-1250"

[[indexing.encoding]]
match = ["**/*.dfm"]
charset = "cp1252"
```

- `match` uses the same glob syntax as `include`/`exclude`, on the path relative to
  the indexed root. It selects an **encoding only**: a file must still match
  `include` to be ingested at all.
- `charset` is any IANA name or alias (`windows-1250`, `cp1252`, `ISO-8859-2`,
  `latin2`, ...). An unknown charset fails when the config loads, not mid-ingest.
- Rules are ordered and the **first match wins**, so a per-directory rule listed
  above a per-extension one overrides it.
- A declared charset relaxes the UTF-8 requirement and **nothing else**: content
  holding a NUL byte is still skipped as binary, whatever the rule says. Binary
  Delphi `.dfm` form files (which start with `TPF0` and are NUL-dense) therefore
  stay rejected even under a `.dfm` rule.
- Content is decoded **before** it is hashed, so correcting a charset re-ingests the
  affected files rather than leaving the previous mis-decode behind the
  unchanged-hash skip.

Declaring the wrong charset produces valid-looking but wrong text, so prefer
converting files to UTF-8 upstream when you control the corpus. See
[ADR 0009](adr/0009-legacy-source-encodings.md) for the full rationale.

See also [commands.md](commands.md) for the CLI reference, and
[paths-and-indexing.md](paths-and-indexing.md) for how state is located, what gets
indexed, where writes land, and the idempotency/URI rules.
