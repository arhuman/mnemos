# Security Policy

## Supported versions

mnemos is pre-1.0. Security fixes land on the latest released minor version.
Released binaries are built with the latest stable Go toolchain so they carry
current runtime and standard-library security patches, independent of the
lower `go.mod` build floor.

| Version | Supported |
|---------|-----------|
| latest `0.x` | ✅ |
| older `0.x`  | ❌ |

## Reporting a vulnerability

Please report security issues **privately**, not via public issues.

- Preferred: open a private advisory at
  <https://github.com/arhuman/mnemos/security/advisories/new>.
- Alternatively, email the maintainer at `arhuman@gmail.com` with `[mnemos
  security]` in the subject.

Please include a description, reproduction steps, affected version
(`mnemos version -v`), and impact. We aim to acknowledge within a few days and
will coordinate a fix and disclosure timeline with you.

## Security posture

mnemos is designed to be safe by default:

- **No network, no telemetry.** The MCP server is stdio-only.
- **Read-only by default.** Writes require `allow_write = true`; destructive
  operations (forget, move) require a separate `allow_delete = true`.
- **Path confinement.** Every caller-supplied path is validated before any disk
  operation: `..` traversal, absolute paths outside the tree root, symlink
  escapes, access to `.mnemos/`, and `[security].exclude` globs are rejected.
- **Secret scanning.** Captured content is scanned for secrets before it is
  written or indexed, and `[security].exclude` patterns keep `.env`, keys, and
  secret directories out of the index.

If you find a way to bypass any of these guarantees, that is a vulnerability:
please report it.

## Known limitations

- **Write-path symlink TOCTOU.** Path confinement resolves symlinks on the
  deepest existing ancestor of a write target at validation time. A symlink
  created *after* that check but *before* the file is written could redirect the
  write outside the tree (a time-of-check/time-of-use race). mnemos assumes the
  tree root is owned by the single user running it; under that trust model the
  attacker would already control the directory. This is an inherent limitation
  of lock-free file storage and is not currently mitigated; do not point mnemos
  at a tree writable by untrusted users.
