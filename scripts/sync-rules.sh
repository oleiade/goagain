#!/usr/bin/env bash
set -euo pipefail

# Sync the Legend Story Studios Flesh and Blood Comprehensive Rules into
# internal/data/rules/comprehensive-rules.txt.
#
# LSS publishes the Comprehensive Rules in two formats at the same URL
# prefix: a plain text export and a PDF. The text export is preferred as the
# source of truth because it already carries no page numbers, headers, or
# footers (those are PDF pagination artifacts); the PDF is downloaded only to
# read the version and date printed on its cover page, since the text export
# does not include either. Redistribution of this unmodified text is covered
# by the LSS Third Party Applications policy (Rules Enforcement Applications
# / APIs) at https://fabtcg.com/resources/terms-use-licensed-assets/; see
# internal/data/rules/README.md for the full basis.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(dirname "$SCRIPT_DIR")"

TARGET_DIR="$PROJECT_ROOT/internal/data/rules"
TARGET_FILE="$TARGET_DIR/comprehensive-rules.txt"

TXT_URL="https://rules.fabtcg.com/txt/latest/en-fab-cr.txt"
PDF_URL="https://rules.fabtcg.com/pdf/en-fab-cr.pdf"

if ! command -v curl >/dev/null 2>&1; then
    echo "Error: curl is required but not installed."
    exit 1
fi

if ! command -v pdftotext >/dev/null 2>&1; then
    echo "Error: pdftotext (from poppler) is required but not installed."
    echo "Install it with: brew install poppler  (macOS)  /  apt-get install poppler-utils  (Debian/Ubuntu)"
    exit 1
fi

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

echo "Downloading Comprehensive Rules text from $TXT_URL..."
curl -sL -A "Mozilla/5.0" -f "$TXT_URL" -o "$WORK/en-fab-cr.txt"

echo "Downloading Comprehensive Rules PDF from $PDF_URL (cover page only, for version/date)..."
curl -sL -A "Mozilla/5.0" -f "$PDF_URL" -o "$WORK/en-fab-cr.pdf"

echo ""
echo "Cover page (version and date):"
pdftotext -layout -f 1 -l 1 "$WORK/en-fab-cr.pdf" - | sed -n '1,5p'
echo ""

# Clean: strip trailing whitespace from every line. The text export has no
# page numbers, headers, footers, or ligature artifacts to remove (those are
# PDF-extraction artifacts; this source is not PDF-derived), so this is the
# only normalization required. Do not reorder or renumber anything: the
# Glossary and Acknowledgments sections appear out of sequence relative to
# the PDF's chapter order, and the Glossary uses a non-standard `N.-1.M`
# numbering; both are preserved as-is to keep the redistributed text
# unmodified.
perl -pe 's/[ \t]+$//' "$WORK/en-fab-cr.txt" > "$WORK/en-fab-cr.clean.txt"
printf '\n' >> "$WORK/en-fab-cr.clean.txt"

mkdir -p "$TARGET_DIR"
cp "$WORK/en-fab-cr.clean.txt" "$TARGET_FILE"

RULE_COUNT="$(grep -cE '^[0-9]+\.[0-9]+\.[0-9]+[a-z]?[[:space:]]' "$TARGET_FILE" || true)"

echo "Synced $(wc -l < "$TARGET_FILE" | tr -d ' ') lines to $TARGET_FILE"
echo "$RULE_COUNT lines match the Chapter.Section.Rule[letter] pattern."
echo ""
echo "Update internal/data/rules/README.md with the version and date shown above"
echo "if they changed."
