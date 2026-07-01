# Command reference

Every `mnemos` CLI command. Note the spelling split: `mnemos.search` (dot) is the
**MCP tool** Claude calls; `mnemos search` (space) is the **CLI command** you run.

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
| `mnemos models install <model>` | Download an embedding model (e.g. `all-MiniLM-L6-v2`) into `~/.mnemos/models` (for the embed build) |
| `mnemos reindex --embeddings` | Recompute and store embedding vectors for all chunks |
| `mnemos validate <bundle> [--json]` | Validate an OKF v0.1 bundle for conformance |
| `mnemos task list` | List indexed Task documents grouped by status |
| `mnemos forget <path>` | Remove a file from the OKF tree and de-index it (requires `allow_delete = true`) |
| `mnemos mv <src> <dst>` | Move a file or directory within the OKF tree and re-index it (requires `allow_delete = true`) |
| `mnemos okfy <file> [--collection --type --tags --out --force]` | Convert a `.txt`/`.md` file into an OKF document, then index it (source is kept intact) |

See also [configuration.md](configuration.md) for `.mnemos.toml`, and
[paths-and-indexing.md](paths-and-indexing.md) for how state is located and how URIs
are resolved.
