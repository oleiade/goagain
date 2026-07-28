---
name: sync-data
description: >-
  Refresh goagain's embedded data: card data from the upstream submodule and the
  LSS Comprehensive Rules. Use for "sync data", "refresh the cards", "update the
  rules", "pull the new set", "the data is stale", or before a release that should
  ship new cards. Runs scripts/sync-data.sh and scripts/sync-rules.sh, then verifies
  the refreshed data still parses and the tests pinned to specific cards and rules
  still pass.
allowed-tools: Read, Edit, Bash
---

# Data sync skill

Two independent sources, both compiled into the binaries via `go:embed`. Sync
either one alone; they share nothing.

## Cards: `internal/data/english/*.json`

1. `./scripts/sync-data.sh` (needs `jq`). Fetches every branch of the
   `data/upstream` submodule and merges them by `unique_id`: `develop` is the
   base, set branches override it.
2. `go test -race ./...`

A new set arrives as its own upstream branch and is picked up automatically.
No script edit is needed for new sets.

## Rules: `internal/data/rules/comprehensive-rules.txt`

1. `./scripts/sync-rules.sh` (needs `curl` and `pdftotext` from poppler). It
   downloads the official text export and prints the PDF cover page.
2. If the version or date on that cover page changed, update
   `internal/data/rules/README.md` to match. The script will not do it for you.
3. `go test -race ./...`

## Gotchas

- **A CR renumber breaks pinned tests.** `internal/data/rules_test.go` asserts
  rule `8.3.5` exists and mentions "Gain 1 action point";
  `internal/mcp/rules_test.go` asserts `1.0.2` and `1.0.2a`. If LSS renumbers,
  repoint each assertion at a currently valid rule that tests the same property.
  Do not delete the assertion.
- **`NewStore` hard-fails under 300 parsed rules** (`minParsedRules`,
  `internal/data/rules.go`). If a sync mangles the text, both servers refuse to
  start. That guard is working as intended: inspect the `.txt` and fix the
  parse, never lower the threshold to get past it.
- **Commit with `jj`, not `git`.** `sync-data.sh` runs `git` only inside the
  submodule (`fetch` plus `archive`) and never checks anything out, so the only
  working-copy changes are under `internal/data/english/`. Keep a data refresh
  in its own change, separate from code.
- **A refresh must reach `main` before tagging.** `release.yml` does not run
  either script; it builds whatever is embedded at that commit. See the
  `release` skill.
- **Keep the rules README honest.** That text is redistributed under the LSS
  third-party terms cited there. When the document version changes, the README's
  version, date, and retrieval date change with it.
