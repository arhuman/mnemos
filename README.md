# mnemos

[![CI](https://github.com/arhuman/mnemos/actions/workflows/ci.yml/badge.svg)](https://github.com/arhuman/mnemos/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/arhuman/mnemos)](https://goreportcard.com/report/github.com/arhuman/mnemos)
[![Latest release](https://img.shields.io/github/v/release/arhuman/mnemos?sort=semver)](https://github.com/arhuman/mnemos/releases)
[![Go version](https://img.shields.io/github/go-mod/go-version/arhuman/mnemos)](go.mod)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

### Give your AI agent a memory it can cite.

> **Local memory for AI agents. Source citations included.**

Claude Code is powerful, but it forgets your project context. It re-derives decisions
you already made and guesses instead of reading what you wrote.

**mnemos** gives it a local, cited memory of:

- your ADRs and design docs
- your notes and runbooks
- your source code
- your OKF knowledge base

No vector database. No Ollama. No Python or Node service. Just one cgo-free Go binary
that indexes your files — any plain-Markdown folder works as-is — and serves them over
[MCP](https://modelcontextprotocol.io), so Claude can **search, read, and cite your own
knowledge** instead of guessing, with every answer landing on the exact `file#section`
and line range.

<p align="center">
  <img src="docs/demo.gif" alt="mnemos CLI: init, ingest, and a search that returns a cited result" width="760">
</p>

## Try it in 60 seconds

```bash
# 1. install — one cgo-free binary into $GOBIN (requires Go 1.25+)
git clone https://github.com/arhuman/mnemos.git && cd mnemos
make install

# 2. index a project
cd ~/work/myproject
mnemos init                                 # creates ./.mnemos.toml + ./.mnemos/
mnemos ingest docs --collection myproject   # index a directory
mnemos search "why did we choose this architecture"
```

> Prefer `make build` (→ `./bin/mnemos`) to keep it off your `$PATH`. The default build
> is pure Go / cgo-free (`CGO_ENABLED=0`).

`search` prints citations you can open:

```text
1. security/scim.md#Provisioning
   lines 42-88
   score 12.7
```

Then wire it into Claude Code (note the **absolute** `--config` path):

```bash
claude mcp add mnemos -- mnemos serve --config /abs/path/to/myproject/.mnemos.toml
```

Now Claude answers from your project instead of guessing, and shows its source:

> **You:** How do I recover lost commits?
>
> **Claude:** Per `recovery/reflog.md`, the reflog records where `HEAD` and each branch
> tip has pointed — even after a hard reset — so you can check out the lost commit's
> hash. *(recovery/reflog.md — "Reflog: recover lost commits")*

That's the whole loop — **index → ask → cited answer.** Everything below is depth:
[why it's built this way](#why-mnemos), [how it works](#how-it-works), and the full
[capabilities](#capabilities).

<details>
<summary>⚠️ One gotcha: a document's identity is its path <em>relative to where you ran <code>ingest</code></em></summary>

A document's URI is its path relative to the scan root you ingested (`docs` above), not
your working directory. Ingesting two directories that each contain (say) `index.md`
resolves both to the same URI: the **second ingest silently overwrites the first**. To
index several trees cleanly, ingest from one common root (`mnemos ingest .`). Details in
[docs/paths-and-indexing.md](docs/paths-and-indexing.md).
</details>

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

## Does it actually retrieve?

**A cited hit, out of the box** (default lexical build, on a shipped example bundle):

```text
$ mnemos ingest examples/git-recipes/bundle --collection git
$ mnemos search "recover lost commits" --limit 1
1. recovery/reflog.md#Gotcha
   lines 24-28
   score 7.8
```

Every result is a real `file#section` and line range you can open — that is the whole
point.

**Retrieval quality, measured.** `mnemos eval` auto-derives held-out query→source pairs
from an OKF bundle (it strips each example block from its own document, then checks
whether retrieval still finds the right document) and reports doc-level metrics. On the
shipped `examples/git-recipes` bundle (6 recipes, keyword-style queries):

| Retriever | Hit@1 | Recall@12 | MRR@12 |
|---|---:|---:|---:|
| Lexical, default (FTS5 / bm25) | 0.83 | 0.83 | 0.83 |
| Semantic + hybrid (`--semantic`) | **1.00** | **1.00** | **1.00** |

All three are **fractions in `[0,1]`** (×100 for a percentage); higher is better. The
`@K` is the retrieval depth — top‑1 for Hit, top‑12 for the rest:

- **Hit@1** — share of queries whose **#1** result is the correct document (`0.83` = 5/6).
- **Recall@12** — share where the correct document appears **anywhere in the top 12**.
- **MRR@12** — mean reciprocal rank: average of `1/(rank of the first correct doc)` over the top 12 (`1.0` = always ranked first).

The default lexical build already nails keyword retrieval. The optional embed build (see
[Semantic search](#semantic-search-optional)) earns its keep on harder,
natural-language-over-structured-data queries: on the `examples/onpage-seo` bundle, whose
held-out answers are JSON-LD / sitemap-XML blocks that share *no* keywords with their
prose, lexical scores `0.00` while `--semantic` recovers Hit@1 `0.57` / Recall@12 `0.86`.
Reproduce (N is 6 and 7 respectively — smoke signals, not benchmarks):

```bash
mnemos eval examples/git-recipes/bundle              # lexical → 0.83
make build-embed && mnemos models install all-MiniLM-L6-v2
mnemos eval examples/git-recipes/bundle --semantic   # hybrid  → 1.00
mnemos eval examples/onpage-seo/bundle --semantic    # the hard case → 0.57
```

## Connect Claude Code

The 60-second path above used `claude mcp add` with an **absolute `--config`** path:

```bash
claude mcp add mnemos -- mnemos serve --config /abs/path/to/project/.mnemos.toml
```

To share it with the repo, commit it via `.mcp.json` instead:

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
  <img src="docs/demo-session.gif" alt="Claude Code answering a keyword-free question via semantic search, citing recovery/reflog.md" width="760">
  <br><em><strong>Semantic search:</strong> the question says <em>"disappeared"</em> — a word that appears nowhere in the notes — yet Claude finds and cites <code>recovery/reflog.md</code>.</em>
</p>

> This clip uses the **optional semantic build** (`make install-embed` + `mnemos models install all-MiniLM-L6-v2` + `use_vectors = true`; see [Semantic search](#semantic-search-optional)). The **default `make install`** binary is lexical-only, so it won't answer a keyword-free question like this — search by keyword instead (e.g. `mnemos search "recover lost commits"`, which hits the same doc).

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
make build-embed                       # or: make install-embed  (still cgo-free, CGO_ENABLED=0)
mnemos models install all-MiniLM-L6-v2 # downloads the embedding model into ~/.mnemos/models
mnemos reindex --embeddings            # compute vectors for already-indexed chunks
mnemos search "why did we choose this architecture" --semantic
```

`--semantic` fuses bm25 with vector similarity, so natural-language queries that the
lexical index misses still resolve. Without the embed build (or an installed model)
the flag is rejected with a clear message; plain `mnemos search` always works.

How it works under the hood (model, pure-Go ONNX inference, RRF fusion):
[docs/architecture.md](docs/architecture.md#semantic-search-the-embed-build).

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

- **[docs/commands.md](docs/commands.md)** — every CLI command and its flags.
- **[docs/configuration.md](docs/configuration.md)** — the layered `.mnemos.toml`, with all defaults.
- **[docs/paths-and-indexing.md](docs/paths-and-indexing.md)** — how state is located, what gets indexed, where writes land, and the idempotency/URI rules.
- **[docs/architecture.md](docs/architecture.md)** — design principles and the retrieval-evaluation methodology.

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
