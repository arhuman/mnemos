# mnemos

[![CI](https://github.com/arhuman/mnemos/actions/workflows/ci.yml/badge.svg)](https://github.com/arhuman/mnemos/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/arhuman/mnemos)](https://goreportcard.com/report/github.com/arhuman/mnemos)
[![Latest release](https://img.shields.io/github/v/release/arhuman/mnemos?sort=semver)](https://github.com/arhuman/mnemos/releases)
[![Go version](https://img.shields.io/github/go-mod/go-version/arhuman/mnemos)](go.mod)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

### Give your AI agent a memory it can cite.

**mnemos** is a local-first memory for AI agents, shipped as a single Go binary.
It indexes your project's notes, docs, ADRs, source, and OKF knowledge base, then
exposes them over [MCP](https://modelcontextprotocol.io) so Claude Code (or any MCP
client) can **search, read, and cite your own knowledge** instead of guessing, with no
external services (no Ollama, Qdrant, Chroma, Python, Node, or mandatory Docker).

<p align="center">
  <img src="docs/demo.gif" alt="mnemos CLI: init, ingest, and a search that returns a cited result" width="760">
</p>

## Why mnemos

- **Truly local-first**: runs entirely on your machine. No network, no telemetry, no data leaves your project.
- **Zero dependencies**: one self-contained, cgo-free Go binary. No Python, Docker, Qdrant, or Ollama.
- **Any MCP client**: built for Claude Code, works with anything that speaks MCP.
- **Cited answers**: every result links back to the exact `file#section` and line range, so claims are verifiable.
- **Fast search by default**: SQLite FTS5 / bm25 out of the box; optional local semantic + hybrid search behind a build tag.
- **Read-write memory**: the agent can capture durable notes (`remember`); you can manage the tree (`forget`, `move`, `list`).
- **Safe by default**: read-only unless you opt in; writes are path-confined and content is secret-scanned.

## How it works

```text
your files  →  mnemos index (SQLite/FTS5)  →  Claude Code memory  →  cited answers
```

Once wired in, Claude can answer from your project instead of hand-waving, and
point back to the source:

- *"Why did we choose this architecture?"*
- *"Where is the ADR about the rule engine?"*
- *"Summarize what we know about SCIM provisioning."*
- *"What changed in this project's memory recently?"*

Under the hood, the binary embeds an MCP server, an indexing pipeline, a SQLite
store, full-text search, an incremental file watcher, and an admin CLI. See
[docs/architecture.md](docs/architecture.md).

## Install

```bash
git clone https://github.com/arhuman/mnemos.git && cd mnemos
make install        # builds a cgo-free binary into $GOBIN
# or: make build    # -> ./bin/mnemos
```

Requires Go 1.25+. The default build is **pure Go / cgo-free** (`make build` sets `CGO_ENABLED=0`).

## Quick start

```bash
cd ~/work/myproject
mnemos init                                 # creates ./.mnemos.toml + ./.mnemos/
mnemos ingest docs --collection myproject   # index a directory
mnemos search "rule engine"                 # query from the CLI
```

`search` prints citations:

```text
1. security/scim.md#Provisioning
   lines 42-88
   score 12.7
```

Citations use the path **relative to the scan root** you ingested (`docs` here),
not your working directory.

> ⚠️ **Mind the URI footgun.** A document's identity is its path *relative to where
> you run `ingest`*. Ingesting two directories that each contain (say) `index.md`
> resolves both to the same URI: the **second ingest silently overwrites the
> first**. To index several trees cleanly, ingest from one common root
> (`mnemos ingest .`). Details in [docs/paths-and-indexing.md](docs/paths-and-indexing.md).

## Connect Claude Code

Register the MCP server with an **absolute `--config`** path:

```bash
claude mcp add mnemos -- mnemos serve --config /abs/path/to/project/.mnemos.toml
```

Or commit it with the repo via `.mcp.json`:

```json
{ "mcpServers": { "mnemos": { "command": "mnemos", "args": ["serve", "--config", "/abs/path/to/project/.mnemos.toml"] } } }
```

Verify with `claude mcp list` (should show `mnemos ✓ connected`) and `/mcp` inside
a session. Claude then calls the tools automatically; see [Capabilities](#capabilities).

<details>
<summary>Why the <code>--config</code> path must be absolute</summary>

Claude Code does not guarantee the working directory it spawns the server in, so
anchoring to the config file is what makes retrieval reliable. `mnemos serve`
resolves a relative `[storage].path` against the config file's directory, so an
absolute `--config` is all you need: the database, capture directory, and tree
root all anchor next to that file regardless of where Claude Code launches the
server. A bare `mnemos serve` only finds your data when the server's working
directory happens to be the project root, which Claude Code does not promise; when
the database can't be found, `serve` fails with a clear error instead of silently
returning empty results.
</details>

<p align="center">
  <img src="docs/demo-mcp.gif" alt="Registering mnemos as an MCP server; Claude Code reports it connected" width="760">
  <br><em>One command wires it in: Claude Code reports <code>mnemos ✓ connected</code>.</em>
</p>

<p align="center">
  <img src="docs/demo-session.gif" alt="Claude Code answering a question from mnemos memory, with a file#section citation" width="760">
  <br><em>Claude answers from memory and lands on the <code>file#section</code> source.</em>
</p>

### Make Claude use memory automatically (optional skill)

MCP tools are *passive*: they're available, but Claude still has to decide to call
`mnemos.search` before answering or `mnemos.remember` when you say something worth
keeping, and models often don't. The bundled **`mnemos-okf` skill** closes that
gap by encoding *when* to reach for memory: recall before answering from
assumption, capture durable facts, and drive the OKF tools.

It's optional and Claude Code-specific: the server works with any MCP client
without it. Install it user-wide (all projects):

```bash
make install-skill        # copies skills/mnemos-okf -> ~/.claude/skills/
```

Or place it manually, e.g. project-level for this repo only:

```bash
mkdir -p .claude/skills && cp -r skills/mnemos-okf .claude/skills/mnemos-okf
```

Capture is deliberately conservative (durable facts only, secret-scanned) and stays
gated behind `allow_write`/`allow_delete`: the skill never grants access the config
hasn't opted into. See [`skills/mnemos-okf/SKILL.md`](skills/mnemos-okf/SKILL.md).

## Capabilities

Claude reaches your memory through MCP tools (and you through the matching CLI
commands). Note the spelling: `mnemos.search` is the **MCP tool** Claude calls;
`mnemos search` is the **CLI command** you run.

### Query (read-only, no gate)

- **`mnemos.search`**: ranked, filtered retrieval with citations.
- **`mnemos.read`**: read a precise chunk (by `chunk_id`) or a whole document (by `uri`).
- **`mnemos.context`**: top-k results as LLM-ready context blocks (`uri:start-end` → content).
- **`mnemos.list`**: walk the OKF tree on disk and annotate each file with index metadata (title, type, tags, collection) plus an `indexed` flag, so both stored and not-yet-indexed files are visible. Filter by `path`, `collection`, `type`, or indexed state.

### Write (requires `allow_write = true`)

- **`mnemos.remember`**: write a note into memory. Pass an optional `path` (e.g. `"adr/0003-rule-engine.md"`) to place it at an explicit location in the OKF tree instead of auto-naming under `[capture].dir`. Content is **secret-scanned** before it is written and indexed.
- **`mnemos.okfy`**: convert an existing `.txt`/`.md` file in the tree into an OKF document (frontmatter + body) at `out` (defaults to the source path with a `.md` extension) and index it, leaving the source intact. The source body is **secret-scanned** first.

### Manage (requires `allow_delete = true`)

- **`mnemos.forget`**: remove a file from the OKF tree and de-index it; idempotent.
- **`mnemos.move`**: move a file **or directory** within the tree and re-index it under the new path. A directory moves its whole subtree, preserving each document's collection. Inbound markdown links to the old paths are not rewritten in V0 (logged as a warning).

## Keep memory fresh

Run a watcher to reindex on change (incremental; removes deleted files):

```bash
mnemos watch . --collection myproject
```

Enable write-back in `.mnemos.toml` so Claude can capture and manage notes:

```toml
[mcp]
allow_write = true     # gates mnemos.remember and mnemos.okfy
allow_delete = true    # gates mnemos.forget and mnemos.move
```

If a watcher is running over the tree, `forget`/`move` operations are also seen by
the watcher (redundant but idempotent); the tools update the index directly and
work without a watcher. Set `[capture] defer_to_watcher = true` when a watcher
covers `capture_dir` to avoid double indexation of remembered notes.

## Semantic search (optional)

The default binary is **lexical only** (FTS5 / bm25) and stays small and cgo-free.
Local semantic + hybrid retrieval is fully implemented but compiled behind the
`embed` build tag, so the ONNX/tokenizer dependencies never enter the default
binary. To enable it:

```bash
make build-embed            # or: make install-embed  (still cgo-free, CGO_ENABLED=0)
mnemos models install       # downloads an embedding model into ~/.mnemos/models
mnemos reindex --embeddings # compute vectors for already-indexed chunks
mnemos search "why did we choose this architecture" --semantic
```

`--semantic` fuses bm25 with vector similarity, so natural-language queries that the
lexical index misses still resolve. Without the embed build (or an installed model)
the flag is rejected with a clear message; plain `mnemos search` always works.

## OKF support

mnemos natively understands [OKF](https://github.com/GoogleCloudPlatform/knowledge-catalog/tree/main/okf)
(Open Knowledge Format) bundles, and any Markdown vault with YAML frontmatter and
cross-links, with no special mode:

- frontmatter `tags`/`type` become fuzzy ranking signals in FTS,
- markdown links are captured as edges (stored, not yet traversed),
- `index.md` files are treated as structure only (kept out of FTS and the link graph).

OKF bundles double as the corpus for `mnemos eval`, which auto-derives held-out
query→source pairs and reports Hit@1 / Recall@12 / MRR@12 against a committed
baseline. See [docs/architecture.md](docs/architecture.md#retrieval-evaluation).

## Reference

<details>
<summary><strong>All commands</strong></summary>

| Command | Purpose |
|---|---|
| `mnemos init` | Create config, state dir, and database |
| `mnemos ingest <path> --collection <c>` | Index a file or directory |
| `mnemos search <query> [--collection --path --type --since --limit --semantic --json]` | Search the index (`--semantic` fuses lexical + vector; needs the embed build) |
| `mnemos ls [path] [--collection --type --tree --depth --all --indexed --unindexed --limit --json]` | List/browse the OKF tree, annotated with index metadata |
| `mnemos eval <okf-bundle> [--baseline <f> --save --semantic --limit N]` | Retrieval-quality eval on an OKF bundle (`--semantic` evaluates the hybrid retriever) |
| `mnemos watch <path> --collection <c>` | Watch and incrementally reindex |
| `mnemos serve` | Run the MCP server (stdio) |
| `mnemos status` | Show storage path, counts, and FTS availability |
| `mnemos version [-v]` | Print the version; `-v` adds commit, build date, and Go toolchain |
| `mnemos models install` | Download an embedding model into `~/.mnemos/models` (for the embed build) |
| `mnemos reindex --embeddings` | Recompute and store embedding vectors for all chunks |
| `mnemos validate <bundle> [--json]` | Validate an OKF v0.1 bundle for conformance |
| `mnemos task list` | List indexed Task documents grouped by status |
| `mnemos forget <path>` | Remove a file from the OKF tree and de-index it (requires `allow_delete = true`) |
| `mnemos mv <src> <dst>` | Move a file or directory within the OKF tree and re-index it (requires `allow_delete = true`) |
| `mnemos okfy <file> [--collection --type --tags --out --force]` | Convert a `.txt`/`.md` file into an OKF document, then index it (source is kept intact) |

</details>

<details>
<summary><strong>Configuration (<code>.mnemos.toml</code>)</strong></summary>

Configuration is layered, lowest precedence first:

1. **Built-in defaults** (the values shown below).
2. **`~/.mnemos.toml`**: user-global overrides, applied to every project.
3. **`./.mnemos.toml`**: the project file in the working directory; its keys win over the home file.

Each layer overrides only the keys it sets; everything else falls through to the
layer below, so a partial file is fine. Passing `--config <path>` **replaces** this
auto-discovery entirely: only that file is layered on top of the defaults (the home
and project files are ignored), and its directory becomes the tree root.

```toml
[storage]
path = ".mnemos/mnemos.db"

[indexing]
include = ["**/*.md", "**/*.txt", "**/*.go", "**/*.sql"]
exclude = [".git/**", "node_modules/**", "vendor/**", "dist/**"]
max_file_bytes = 4194304    # skip any single file larger than this (0 disables)

[chunking]
target_tokens = 700
overlap_tokens = 80

[search]
default_limit = 12

[mcp]
transport = "stdio"
allow_write = false        # gates mnemos.remember
allow_delete = false       # gates mnemos.forget and mnemos.move

[capture]
dir = ".mnemos/capture"    # default path for auto-named notes
defer_to_watcher = false   # true => remember is write-only, watcher ingests

[security]
exclude_secrets = true
exclude = ["**/.env", "**/*.pem", "**/*.key", "**/id_rsa", "**/secrets/**"]
```

</details>

**Paths, URIs, and writes**, covering how state is located, what gets indexed, where writes
land, and the idempotency/URI rules: see
[docs/paths-and-indexing.md](docs/paths-and-indexing.md).

## Security

- No network, no telemetry; the MCP server is stdio-only.
- Read-only by default. Write-back is opt-in (`allow_write = true`). Destructive operations (forget, move) require a separate opt-in (`allow_delete = true`).
- All caller-supplied paths are validated by a confinement guard before any disk operation: `..` traversal, absolute paths outside the tree root, symlink escapes, access to `.mnemos/`, and `[security].exclude` globs are all rejected.
- Captured content is secret-scanned before it is written or indexed.
- Path/secret exclusion patterns keep `.env`, keys, and secret dirs out of the index.

See [SECURITY.md](SECURITY.md) for the full policy.

## Development

```bash
make build      # cgo-free binary -> bin/
make test       # go test -race ./...
make audit      # golangci-lint (incl. govet + staticcheck) + govulncheck + race tests
make tools      # install pinned dev tools (golangci-lint, govulncheck)
make help       # list all targets
```

Architecture, design principles, and the retrieval-evaluation methodology live in
[docs/architecture.md](docs/architecture.md); design decisions are recorded as
[ADRs](docs/adr/). Contributions welcome; see [CONTRIBUTING.md](CONTRIBUTING.md).

## License

mnemos is licensed under the [MIT License](LICENSE): this covers mnemos's own code
and content. The repository also vendors third-party material that is **not**
covered by the MIT License and remains under its own terms; see
[THIRD-PARTY-NOTICES.md](THIRD-PARTY-NOTICES.md) (notably the example OKF bundle in
`examples/onpage-seo/bundle/`).
</content>
