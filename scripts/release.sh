#!/usr/bin/env bash
# kpot release driver — semver bump + tag + push automation.
#
# Usage:
#   scripts/release.sh {patch|minor|major} [-y|--yes]
#
# Run this from the repo root (or via `make release-patch` etc.).
#
# Refuses unless:
#   - the working tree is clean
#   - HEAD is on `main`
#   - local main is fully synced with origin/main (no ahead, no behind)
#   - all CI gates pass locally (vet + gofmt + test)
#
# The annotated tag message is auto-generated from
# `git log <prev>..HEAD` and includes the install snippet so the
# Releases page reads cleanly.

set -euo pipefail

# --- Args ---

bump_type=${1:-}
case "$bump_type" in
  patch|minor|major) ;;
  *)
    echo "usage: $0 {patch|minor|major} [-y|--yes]" >&2
    exit 64
    ;;
esac

auto_yes=0
case "${2:-}" in
  -y|--yes) auto_yes=1 ;;
  "") ;;
  *)
    echo "unknown second arg: $2 (expected -y or --yes)" >&2
    exit 64
    ;;
esac

# --- Pretty output ---

if [ -t 1 ]; then
  RED='\033[31m'; GRN='\033[32m'; CYN='\033[36m'; RST='\033[0m'
else
  RED=''; GRN=''; CYN=''; RST=''
fi
err()  { printf "${RED}error:${RST} %s\n" "$*" >&2; exit 1; }
info() { printf "${CYN}→${RST} %s\n" "$*"; }
ok()   { printf "${GRN}✓${RST} %s\n" "$*"; }

# --- Pre-flight checks ---

if [ ! -d .git ] && [ ! -f .git ]; then
  err "not in a git repository (run from kpot project root)"
fi

if ! git diff --quiet || ! git diff --cached --quiet; then
  err "working tree has uncommitted changes (commit or stash first)"
fi

branch=$(git rev-parse --abbrev-ref HEAD)
if [ "$branch" != "main" ]; then
  err "not on main (currently on $branch). Run 'git checkout main' first."
fi

info "Fetching origin/main..."
git fetch --quiet origin main

ahead=$(git rev-list --count origin/main..HEAD)
behind=$(git rev-list --count HEAD..origin/main)

if [ "$behind" -gt 0 ]; then
  err "local main is $behind commit(s) behind origin/main. Run 'git pull --ff-only' first."
fi
if [ "$ahead" -gt 0 ]; then
  err "local main is $ahead commit(s) ahead of origin/main. Push via a PR before tagging."
fi

# --- Compute next version ---

# Pick the latest *clean* semver tag (skip pre-releases like v0.6.0-rc1).
current=$(git tag --list 'v[0-9]*.[0-9]*.[0-9]*' \
            | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' \
            | sort -V \
            | tail -1)
if [ -z "$current" ]; then
  current="v0.0.0"
fi

current_num=${current#v}
IFS='.' read -r major minor patch <<<"$current_num"

case "$bump_type" in
  patch) patch=$((patch + 1)) ;;
  minor) minor=$((minor + 1)); patch=0 ;;
  major) major=$((major + 1)); minor=0; patch=0 ;;
esac
next="v${major}.${minor}.${patch}"

# Sanity: the computed tag must not already exist.
if git rev-parse "$next" >/dev/null 2>&1; then
  err "tag $next already exists; refusing to clobber"
fi

# --- Run quality gates (CI-equivalent) ---

info "Running quality gates (vet, gofmt, test)..."
if ! make --no-print-directory check; then
  err "quality gates failed — fix before tagging"
fi
ok "quality gates passed"

# --- Generate tag message ---

range="${current}..HEAD"
if [ "$current" = "v0.0.0" ]; then
  range="HEAD"
fi

# `--no-merges` keeps the changelog short — squash-merged PRs show up
# as their squash commit; explicit Merge-pull-request commits are
# excluded so we don't double up.
changelog=$(git log "$range" --no-merges --pretty=format:'- %s' || true)
if [ -z "$changelog" ]; then
  changelog="- (no commit changes since $current — re-tag of same code)"
fi

tagmsg=$(cat <<EOF
$next

Release $next ($bump_type bump from $current).

Changes since $current:

$changelog

Install:
  go install github.com/Shin-R2un/kpot/cmd/kpot@$next
or grab a prebuilt binary from
  https://github.com/Shin-R2un/kpot/releases/tag/$next
EOF
)

# --- Confirm ---

cat <<EOF

  Current: $current
  Next:    $next  ($bump_type)
  Branch:  $branch  (synced with origin)

  Tag message preview:
  ─────────────────────────────────────────────────────────
$(printf '%s\n' "$tagmsg" | sed 's/^/  /')
  ─────────────────────────────────────────────────────────

EOF

if [ "$auto_yes" -eq 0 ]; then
  read -r -p "Proceed and push tag $next? [y/N] " ans
  case "$ans" in
    y|Y|yes|YES) ;;
    *) err "cancelled" ;;
  esac
fi

# --- Tag and push ---

info "Creating annotated tag $next..."
git tag -a "$next" -m "$tagmsg"

info "Pushing tag $next to origin..."
git push origin "$next"

ok "Tagged and pushed. GitHub Actions release workflow is now running:"
echo "    https://github.com/Shin-R2un/kpot/actions/workflows/release.yml"
echo ""
echo "Once it completes, the release page will show all 5 binaries:"
echo "    https://github.com/Shin-R2un/kpot/releases/tag/$next"
