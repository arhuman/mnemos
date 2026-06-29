# Contributing to mnemos

Thanks for your interest in improving mnemos! This guide covers local setup, the
quality bar, and how to get a change merged.

## Prerequisites

- Go 1.25+ (the `go.mod` floor; CI also runs on the latest stable Go).
- `make`. That's it for the default build: mnemos is pure-Go and cgo-free.

## Getting started

```bash
git clone https://github.com/arhuman/mnemos.git && cd mnemos
make build        # cgo-free binary -> bin/mnemos
make test         # go test -race ./...
```

Useful targets (`make help` lists them all):

| Target | What it does |
|--------|--------------|
| `make build` | Compile the default (lexical) cgo-free binary into `bin/` |
| `make build-embed` | Compile with semantic search (`-tags embed`, still cgo-free) |
| `make test` | Run all tests with the race detector |
| `make cover` | Tests + coverage report, fails under the 80% gate |
| `make lint` | Run golangci-lint |
| `make audit` | Full gate: vet, lint, staticcheck, govulncheck, race + coverage |

## Working on the semantic path

Semantic/hybrid retrieval lives behind the `embed` build tag so the heavy
ONNX/tokenizer dependencies never enter the default binary. If your change
touches `internal/embed`, `internal/search/hybrid*`, or the `--semantic` flags,
verify both builds compile:

```bash
make build          # default
make build-embed    # embed variant
go test -tags embed ./...
```

## Quality bar

Before opening a PR:

1. `make test` passes (CI runs `-race` on Go 1.25 **and** latest stable).
2. `make lint` is clean; `make audit` for non-trivial changes.
3. New behavior has tests. We keep total coverage at or above 80%.
4. If you change CLI flags, MCP tools, or config, update `README.md` (and
   `docs/` where relevant) in the same PR.

## Commit messages

Commits follow [Conventional Commits](https://www.conventionalcommits.org/)
(`feat:`, `fix:`, `docs:`, `build:`, `ci:`, `refactor:`, `test:`, `chore:`),
enforced by commitlint. Keep commits atomic and focused. Use a `!` or a
`BREAKING CHANGE:` footer for breaking changes.

## Architecture

See [docs/architecture.md](docs/architecture.md) for the subsystem map, design
principles, and evaluation methodology, and [docs/adr/](docs/adr) for the
Architecture Decision Records behind the major design choices.

## Reporting bugs and proposing features

Use the issue templates. For security issues, follow
[SECURITY.md](SECURITY.md): do not open a public issue.

By contributing, you agree that your contributions are licensed under the
project's [MIT License](LICENSE).
