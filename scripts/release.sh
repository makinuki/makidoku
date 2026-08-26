#!/usr/bin/env bash
set -euo pipefail

# Usage: scripts/release.sh <patch|minor|major|x.y.z> [--dry-run]
#  - must be on master, clean tree, has ## [Unreleased] in CHANGELOG.md
#  - bumps internal/version/version.go and CHANGELOG.md atomically
#  - commits chore(release): X.Y.Z and tags vX.Y.Z (does not push)

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
VERSION_FILE="$ROOT/internal/version/version.go"
CHANGELOG="$ROOT/CHANGELOG.md"

arg="${1:-}"
dry_run=""

if [[ "$arg" == "" ]]; then
  echo "usage: $0 <patch|minor|major|x.y.z> [--dry-run]" >&2
  exit 1
fi
if [[ "${2:-}" == "--dry-run" ]]; then
  dry_run="1"
fi

# Guards: master branch, clean tree
branch="$(git rev-parse --abbrev-ref HEAD)"
if [[ "$branch" != "master" ]]; then
  echo "releases are cut from master (current: $branch)" >&2
  exit 1
fi
if [[ -n "$(git status --porcelain)" ]]; then
  echo "working tree is not clean" >&2
  git status --short >&2
  exit 1
fi

if [[ ! -f "$VERSION_FILE" ]]; then
  echo "missing $VERSION_FILE" >&2
  exit 1
fi
if [[ ! -f "$CHANGELOG" ]]; then
  echo "missing $CHANGELOG" >&2
  exit 1
fi

current="$(sed -n 's/.*Version = "\([0-9]\+\.[0-9]\+\.[0-9]\+\)".*/\1/p' "$VERSION_FILE" | head -n1)"
if [[ ! "$current" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "cannot parse current version from $VERSION_FILE" >&2
  exit 1
fi

# Validate semver (no leading zeros)
valid_semver() {
  local v="$1"
  if [[ ! "$v" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
    return 1
  fi
  return 0
}

if ! valid_semver "$current"; then
  echo "current version is not valid semver: $current" >&2
  exit 1
fi

if ! grep -q "^## \[Unreleased\]" "$CHANGELOG"; then
  echo "CHANGELOG.md has no ## [Unreleased] section" >&2
  exit 1
fi

# Compute next version
IFS='.' read -r cur_major cur_minor cur_patch <<< "$current"
next=""

case "$arg" in
  patch)
    next="$cur_major.$cur_minor.$((cur_patch + 1))"
    ;;
  minor)
    next="$cur_major.$((cur_minor + 1)).0"
    ;;
  major)
    next="$((cur_major + 1)).0.0"
    ;;
  *)
    if valid_semver "$arg"; then
      next="$arg"
    else
      echo "arg must be patch|minor|major or x.y.z (got: $arg)" >&2
      exit 1
    fi
    ;;
esac

# Must be greater than current
if [[ "$(printf '%s\n%s\n' "$current" "$next" | sort -V | tail -n1)" != "$next" || "$current" == "$next" ]]; then
  echo "next version $next must be greater than current $current" >&2
  exit 1
fi

# Must not already have tag
if git tag -l | grep -qx "v$next"; then
  echo "tag v$next already exists" >&2
  exit 1
fi

# Gates: typecheck and tests
echo "checking go vet..."
go vet ./...
echo "checking go test..."
go test ./internal/version ./internal/config -count=1 >/dev/null

if [[ -n "$dry_run" ]]; then
  echo "dry-run: would bump $current -> $next"
  echo "  $VERSION_FILE: Version = \"$next\""
  echo "  $CHANGELOG: ## [Unreleased] -> ## [$next] - $(date +%Y-%m-%d)"
  echo "  git commit -m \"chore(release): $next\" && git tag -a v$next -m \"v$next\""
  exit 0
fi

# Bump version.go
# Use a portable sed: replace first occurrence of Version = "x.y.z"
tmp_ver="$(mktemp)"
# shellcheck disable=SC2016
sed "0,/Version = \".*\"/s//Version = \"$next\"/" "$VERSION_FILE" > "$tmp_ver" && mv "$tmp_ver" "$VERSION_FILE"

# Bump CHANGELOG.md
today="$(date +%Y-%m-%d)"
tmp_cl="$(mktemp)"
# Replace first ## [Unreleased] with two lines: ## [Unreleased] + blank + ## [next] - date
awk -v newver="$next" -v today="$today" '
  BEGIN { replaced=0 }
  /^## \[Unreleased\]$/ && !replaced { print; print ""; print "## [" newver "] - " today; replaced=1; next }
  /^## \[Unreleased\]$/ && replaced { print }
  !/^## \[Unreleased\]$/ { print }
' "$CHANGELOG" > "$tmp_cl" && mv "$tmp_cl" "$CHANGELOG"

git add "$VERSION_FILE" "$CHANGELOG"
git commit -m "chore(release): $next"
git tag -a "v$next" -m "v$next"

echo "released $next"
echo "  git push --follow-tags origin master"
