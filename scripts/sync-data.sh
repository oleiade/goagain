#!/usr/bin/env bash
set -euo pipefail

# Sync data from the upstream submodule into internal/data/english/.
#
# Upstream (the-fab-cube/flesh-and-blood-cards) splits content across branches:
# `develop` is the up-to-date base, while set-specific branches (e.g.
# compendium-of-rathe, omens-of-the-third-age, and any future ones) hold content
# not yet folded into develop. This script merges every branch's JSON into a
# single unified data set so the binaries behave as if everything lived in
# develop.
#
# Branch selection is automatic: `develop` is the base, and every other remote
# branch (anything that is not main/develop/HEAD) is treated as a set branch.
# Each JSON file is a flat array of objects keyed by `unique_id`; files are
# merged by unioning on `unique_id` with set-branch-wins precedence.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

UPSTREAM_REPO="$PROJECT_ROOT/data/upstream"
TARGET_DIR="$PROJECT_ROOT/internal/data/english"

if [ ! -d "$UPSTREAM_REPO/.git" ] && [ ! -f "$UPSTREAM_REPO/.git" ]; then
    echo "Error: Upstream submodule not found at: $UPSTREAM_REPO"
    echo "Make sure the submodule is initialized: git submodule update --init data/upstream"
    exit 1
fi

if ! command -v jq >/dev/null 2>&1; then
    echo "Error: jq is required but not installed."
    echo "Install it with: brew install jq  (macOS)  /  apt-get install jq  (Debian/Ubuntu)"
    exit 1
fi

# Fetch every branch so we can merge across them without disturbing the
# submodule's own working checkout.
echo "Fetching upstream branches..."
git -C "$UPSTREAM_REPO" fetch --prune origin '+refs/heads/*:refs/remotes/origin/*'

# develop is the base; every other remote branch (except main/HEAD) is a set
# branch that overrides develop. Sorted for deterministic precedence.
SET_BRANCHES="$(git -C "$UPSTREAM_REPO" for-each-ref --format='%(refname:strip=3)' refs/remotes/origin \
    | grep -vxE 'HEAD|main|develop' | sort)"

# Layer order = merge precedence (last write wins): develop first, set branches
# after, so set branches override develop on shared unique_ids. If two set
# branches ever share a unique_id (sets are expected to be disjoint), the
# alphabetically-last branch wins.
LAYERS="develop $SET_BRANCHES"

echo "Merging branches (base first, later overrides):"
for b in $LAYERS; do
    echo "  - $b"
done

# Extract each branch's json/english tree into a temp workspace via git archive
# (never touches the submodule checkout).
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

for b in $LAYERS; do
    mkdir -p "$WORK/$b"
    git -C "$UPSTREAM_REPO" archive "origin/$b" json/english 2>/dev/null | tar -x -C "$WORK/$b" || true
done

# Union of all filenames across layers (a set branch may introduce a new file).
FILES="$(for b in $LAYERS; do ls "$WORK/$b/json/english" 2>/dev/null || true; done | sort -u)"

if [ -z "$FILES" ]; then
    echo "Error: no JSON files found in any branch under json/english."
    exit 1
fi

echo ""
echo "Syncing merged data to $TARGET_DIR..."
mkdir -p "$TARGET_DIR"

for f in $FILES; do
    inputs=""
    for b in $LAYERS; do
        [ -f "$WORK/$b/json/english/$f" ] && inputs="$inputs $WORK/$b/json/english/$f"
    done

    # Flatten every layer's array (file order preserved) -> keyed reduce with
    # last-write-wins -> back to an array. `// ($x|tostring)` is a safety net for
    # any object that happens to lack a unique_id.
    # shellcheck disable=SC2086
    jq -s '[ .[] | .[] ]
           | reduce .[] as $x ({}; .[($x.unique_id // ($x|tostring))] = $x)
           | [ .[] ]' $inputs > "$TARGET_DIR/$f"

    echo "  merged $f -> $(jq length "$TARGET_DIR/$f") entries"
done

echo ""
echo "Data sync complete!"
echo "$(printf '%s\n' "$FILES" | wc -l | tr -d ' ') JSON files synced."
