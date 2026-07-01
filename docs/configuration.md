# Configuration (`.mnemos.toml`)

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

See also [commands.md](commands.md) for the CLI reference, and
[paths-and-indexing.md](paths-and-indexing.md) for how state is located, what gets
indexed, where writes land, and the idempotency/URI rules.
