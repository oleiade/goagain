#!/bin/bash
set -euo pipefail

# Sync data from upstream submodule to internal/data/english/
# This script copies JSON files from the upstream repository to the embedded data directory.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

UPSTREAM_DIR="$PROJECT_ROOT/data/upstream/json/english"
TARGET_DIR="$PROJECT_ROOT/internal/data/english"

if [ ! -d "$UPSTREAM_DIR" ]; then
    echo "Error: Upstream data directory not found: $UPSTREAM_DIR"
    echo "Make sure the submodule is initialized: git submodule update --init"
    exit 1
fi

echo "Syncing data from $UPSTREAM_DIR to $TARGET_DIR..."

# Create target directory if it doesn't exist
mkdir -p "$TARGET_DIR"

# Copy all JSON files
cp -v "$UPSTREAM_DIR"/*.json "$TARGET_DIR/"

echo "Data sync complete!"
echo ""
echo "Files synced:"
ls -la "$TARGET_DIR"/*.json | wc -l
echo " JSON files"
