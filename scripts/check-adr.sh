#!/bin/sh
# check-adr.sh — check one ADR against this repo's house format.
# Usage: check-adr.sh docs/adr/NNNN-slug.md
#
# The repo's ADRs predate the 10x ADR Contract v1 and use a numbered title with
# a "Date:" line and prose sections, rather than YAML frontmatter. This checks
# that convention on a single file, so a new ADR can be gated without rewriting
# the nine that already exist.
set -eu

f="${1:?usage: check-adr.sh <adr-file>}"
fail=0
warn() { printf 'ADR: %s: %s\n' "$(basename "$f")" "$1" >&2; fail=1; }

[ -f "$f" ] || { printf 'ADR: no such file: %s\n' "$f" >&2; exit 1; }

basename "$f" | grep -qE '^[0-9]{4}-[a-z0-9][a-z0-9-]*\.md$' \
  || warn "filename is not NNNN-slug.md"

grep -qE '^# [0-9]+\. .+' "$f" || warn "no '# N. Title' heading"
grep -qE '^Date: [0-9]{4}-[0-9]{2}-[0-9]{2}$' "$f" || warn "no 'Date: YYYY-MM-DD' line"

for section in Status Context Decision Consequences; do
  grep -qE "^## ${section}\$" "$f" || warn "no '## ${section}' section"
done

grep -qE '^(Proposed|Accepted|Deprecated|Superseded)$' "$f" \
  || warn "no status value (Proposed|Accepted|Deprecated|Superseded) on its own line"

# An ADR still carrying a TODO is a stub, not a decision. This is what stops a
# placeholder from passing the gate the phase that writes it must clear.
grep -n 'TODO' "$f" >&2 && warn "still carries a TODO placeholder"

[ "$fail" -eq 0 ] && printf 'ADR: %s conformant (house format)\n' "$(basename "$f")"
exit "$fail"
