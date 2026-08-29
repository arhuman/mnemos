#!/usr/bin/env bash
# release.sh: cut a release with minimal ceremony. Language-agnostic; the only
# project hooks are the quality gate and, where a version manifest exists, the
# file it lives in, so it drops into any standard repo.
#
# Derives the next semantic version from the Conventional Commits made since the
# last v* tag (feat -> minor, fix/other -> patch, ! or BREAKING CHANGE -> major,
# capped to minor while still on 0.x), lets the caller confirm or override it
# (pass the version as $1 for non-interactive use), runs the gate (`make ci`
# when a Makefile exists; customize this line otherwise, e.g. `uv run pytest &&
# uv run ruff check`), stamps CHANGELOG.md, the version pins in $pin_files
# (adoption snippets that name the released tag) and, for a Python package, the
# pyproject.toml `version` field (Go has no such manifest: its version comes
# from the tag via ldflags at build time), then commits, tags and pushes.
# Pushing the tag is what triggers .github/workflows/release.yml.
# An executable scripts/release-preflight.sh, when present, runs with the
# preconditions and can veto the release with project-specific checks.
# Everything fallible is checked before anything is mutated: the remote is
# fetched and must not have diverged, and leftovers of an interrupted run (a
# stale changelog stamp, an untagged release commit at the head) are refused,
# so a re-run can never double-stamp or stack a second release commit.
#
# jj-colocated repos are handled natively: the release branch resolves from the
# jj bookmark (git HEAD is permanently detached there) and the release commit
# is made and pushed with jj, then tagged with git on the shared .git store.
#
# Invoked by the Makefile `release` target (`make release`) when one exists,
# otherwise directly: `scripts/release.sh [vX.Y.Z]`.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"
changelog="CHANGELOG.md"
# Files whose adoption snippets pin the released version (`vX.Y.Z` and
# `VERSION=X.Y.Z` spellings). Stamped from the previous tag to the new one in
# the release commit; missing files are skipped. Keep the changelog out: its
# past version headings are history, not pins.
pin_files=(README.md .pre-commit-hooks.yaml action.yml docs/ci.md)

die() { echo "release: $*" >&2; exit 1; }

# jj colocation leaves git HEAD permanently detached, so resolve the release
# branch from the jj bookmark rather than the checked-out ref.
if [ -d .jj ] && command -v jj >/dev/null 2>&1; then
  vcs=jj
else
  vcs=git
fi

# --- preconditions -----------------------------------------------------------
[ -z "$(git status --porcelain)" ] || die "working tree not clean; commit or stash first"

# Learn about remote movement now, not at the final push: by then the changelog
# is stamped and the release commit described, and re-running after that
# failure is what double-stamps and stacks a second release commit.
has_remote=false
if git remote get-url origin >/dev/null 2>&1; then
  has_remote=true
  if [ "$vcs" = jj ]; then
    jj git fetch || die "cannot fetch origin; releasing needs the remote reachable"
  else
    git fetch origin || die "cannot fetch origin; releasing needs the remote reachable"
  fi
fi

if [ "$vcs" = git ]; then
  branch=$(git rev-parse --abbrev-ref HEAD)
  [ "$branch" = main ] || [ "$branch" = master ] || die "not on main/master (on $branch)"
  if $has_remote && git rev-parse -q --verify "origin/$branch" >/dev/null; then
    git merge-base --is-ancestor "origin/$branch" "$branch" \
      || die "origin/$branch has moved; rebase onto it and re-run"
  fi
else
  # Read the list once into a variable rather than piping into `grep -q`:
  # grep exits on its first match, and jj killed by the resulting broken pipe
  # returns 3, which pipefail turns into the status of the whole pipeline. The
  # bookmark would then look missing whenever it is not the last line printed.
  # A clean git status still allows a described-but-empty @, whose message the
  # release describe below would silently overwrite.
  wc_desc=$(jj log -r @ --no-graph -T 'description.first_line()' 2>/dev/null || true)
  [ -z "$wc_desc" ] || die "working copy already describes '$wc_desc'; run jj new first"
  bookmarks=$(jj bookmark list 2>/dev/null || true)
  grep -qE '^(main|master) \(conflicted\)' <<<"$bookmarks" \
    && die "the release bookmark is conflicted; reconcile it (jj bookmark set) and re-run"
  if grep -qE '^main:' <<<"$bookmarks"; then branch=main
  elif grep -qE '^master:' <<<"$bookmarks"; then branch=master
  else die "no main/master bookmark in this jj repo"; fi
  if $has_remote; then
    remote_head=$(jj log -r "${branch}@origin" --no-graph -T 'commit_id' 2>/dev/null || true)
    if [ -n "$remote_head" ]; then
      behind=$(jj log -r "${branch}@origin & ::${branch}" --no-graph -T 'commit_id' 2>/dev/null || true)
      [ -n "$behind" ] || die "${branch}@origin has moved; rebase onto it and re-run"
    fi
  fi
fi

# A half-finished earlier run leaves its release commit at the head, untagged;
# a blind re-run would stack a second one on top. A tagged one is just history.
if [ "$vcs" = git ]; then
  last_subject=$(git log -1 --format=%s)
else
  last_subject=$(jj log -r @- --no-graph -T 'description.first_line()' 2>/dev/null || true)
fi
case "$last_subject" in
  "chore(release): v"*)
    stale=${last_subject#chore(release): }
    git rev-parse -q --verify "refs/tags/$stale" >/dev/null \
      || die "head is '$last_subject' but tag $stale does not exist: an earlier run half-finished; drop the commit (jj abandon @- / git reset --hard HEAD^) or finish it by hand"
    ;;
esac

[ -f "$changelog" ] || die "$changelog not found"
# Project-specific preconditions live in an optional hook, keeping this script
# generic; typical veto: a sibling repo or remote fixture CI depends on is
# dirty or unpushed, which a green local gate cannot see.
if [ -x scripts/release-preflight.sh ]; then
  ./scripts/release-preflight.sh || die "preflight failed"
fi

# --- last tag + bump detection ----------------------------------------------
last=$(git tag --list 'v*' --sort=-v:refname | head -n1)
if [ -z "$last" ]; then
  last="v0.0.0"; range="HEAD"
else
  range="${last}..HEAD"
fi
IFS=. read -r major minor patch <<<"${last#v}"

subjects=$(git log "$range" --no-merges --format='%s')
[ -n "$subjects" ] || die "no commits since $last; nothing to release"
bodies=$(git log "$range" --no-merges --format='%B')

bump=patch
if printf '%s\n' "$subjects" | grep -qE '^[a-z]+(\([^)]+\))?!:' \
   || printf '%s\n' "$bodies" | grep -qE '^BREAKING CHANGE'; then
  bump=major
elif printf '%s\n' "$subjects" | grep -qE '^feat(\([^)]+\))?:'; then
  bump=minor
fi
# SemVer 0.x: a breaking change bumps minor, not major, until the first 1.0.0.
[ "$major" -eq 0 ] && [ "$bump" = major ] && bump=minor

case "$bump" in
  major) major=$((major + 1)); minor=0; patch=0 ;;
  minor) minor=$((minor + 1)); patch=0 ;;
  patch) patch=$((patch + 1)) ;;
esac
suggested="v${major}.${minor}.${patch}"

# --- confirm / override ------------------------------------------------------
n_all=$(git rev-list --count --no-merges "$range")
n_feat=$(printf '%s\n' "$subjects" | grep -cE '^feat' || true)
n_fix=$(printf '%s\n' "$subjects" | grep -cE '^fix' || true)
echo "Last tag       : $last"
echo "Commits since  : $n_all ($n_feat feat, $n_fix fix)  ->  bump = $bump"
# Version may be passed as $1 (non-interactive); otherwise prompt on the tty.
chosen="${1:-}"
if [ -z "$chosen" ]; then
  printf 'Version [%s]: ' "$suggested"
  read -r chosen </dev/tty || true
fi
version=${chosen:-$suggested}
[[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || die "invalid version '$version' (want vMAJOR.MINOR.PATCH)"
git rev-parse "$version" >/dev/null 2>&1 && die "tag $version already exists"
# The tag-exists check above cannot catch an interrupted run: the tag is the
# very last thing created, so a stale stamp means the run died before it.
grep -qF "## [${version#v}]" "$changelog" \
  && die "$changelog already carries ${version#v}: an earlier run half-finished; remove that heading and re-run"

# --- gate --------------------------------------------------------------------
if [ -f Makefile ]; then
  echo "Running make ci ..."
  make ci
else
  echo "release: no Makefile found; customize this gate for your ecosystem" >&2
  die "add this repo's test+lint command here (e.g. 'uv run pytest && uv run ruff check .'), then remove this line"
fi
[ -z "$(git status --porcelain)" ] || die "gate left changes (fmt/tidy?); commit them and re-run"

# --- stamp pyproject.toml version (Python only; Go has no version manifest) --
pyproject="pyproject.toml"
if [ -f "$pyproject" ]; then
  tmp=$(mktemp)
  awk -v ver="${version#v}" '
    !stamped && /^version = "/ { print "version = \"" ver "\""; stamped = 1; next }
    { print }
  ' "$pyproject" > "$tmp" && mv "$tmp" "$pyproject"
  grep -qF "version = \"${version#v}\"" "$pyproject" || die "failed to stamp $pyproject (no 'version = \"...\"' line under [project]?)"
  git add "$pyproject"
fi

# --- stamp version pins ------------------------------------------------------
# Rewrites the previous release's pins to the new version in $pin_files, both
# the `vX.Y.Z` and the bare `VERSION=X.Y.Z` spellings. Exact-match on the last
# tag, so surrounding prose and other version-like strings are untouched.
if [ "$last" != "v0.0.0" ]; then
  last_re=${last//./\\.}
  for f in "${pin_files[@]}"; do
    [ -f "$f" ] || continue
    tmp=$(mktemp)
    sed -e "s/${last_re}/${version}/g" \
        -e "s/VERSION=${last_re#v}/VERSION=${version#v}/g" "$f" > "$tmp" && mv "$tmp" "$f"
    git add "$f"
  done
fi

# --- stamp CHANGELOG ---------------------------------------------------------
# Promote the [Unreleased] section: keep an empty [Unreleased] on top and open
# a new dated version heading beneath it, over the accumulated changes.
today=$(date +%F)
tmp=$(mktemp)
awk -v ver="${version#v}" -v date="$today" '
  !stamped && /^## \[Unreleased\]/ {
    print "## [Unreleased]"; print "";
    print "## [" ver "] - " date;
    stamped = 1; next
  }
  { print }
' "$changelog" > "$tmp" && mv "$tmp" "$changelog"
grep -qF "## [${version#v}] - $today" "$changelog" || die "failed to stamp $changelog (no '## [Unreleased]' heading?)"

# --- commit, tag, push -------------------------------------------------------
if [ "$vcs" = git ]; then
  git add "$changelog"
  git commit -m "chore(release): $version"
  git tag -a "$version" -m "$version"
  git push origin "$branch"
  git push origin "$version"
else
  # jj: every stamped file lives in the working-copy commit (jj snapshots the
  # tree on invocation, so the `git add`s above are inert here). Describe it,
  # advance the bookmark, push it, then tag that commit and push the tag.
  jj describe -m "chore(release): $version"
  jj new
  jj bookmark set "$branch" -r @-
  jj git push --bookmark "$branch"
  hash=$(jj log --no-graph -r @- -T 'commit_id')
  git tag -a "$version" -m "$version" "$hash"
  git push origin "$version"
fi

echo "Pushed $version. .github/workflows/release.yml is now building."
