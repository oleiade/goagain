# Project Overview

goagain is a Go REST API and MCP (Model Context Protocol) server providing access to Flesh and Blood card game data. It produces two binaries: an API server (port 8080) and an MCP server (port 8081).
It is built on top of [the-fab-cube/flesh-and-blood-cards](https://github.com/the-fab-cube/flesh-and-blood-cards) card data set.

# 1. Build

`go build -v ./...` # Build the project

# 2. RUN
`go run ./cmd/api`  # Run the API server
`go run ./cmd/mcp`  # Run the MCP server

# 3. Run tests

go test -race -v ./... # Test (always with race detector)
k6 run tests/k6/api.js  # api load performance tests
k6 run tests/k6/trusted-proxies.js # functional tests
 
# 4. Format and lint after every changes

gofmt -w
golangci-lint run

# 5. Verify correctness and safety after every changes

go vet ./...
gosec ./...
govulncheck ./...

# 6. Sync card data from upstream submodule

./scripts/sync-data.sh
# Fetches every upstream branch and merges them into a single unified data set:
# `develop` is the base, and every other branch (any branch but main/develop) is
# a set branch. Files are unioned by `unique_id` with set-branch-wins precedence,
# then written to internal/data/english/. Requires jq.


# Architecture

```
cmd/
  api/          # REST API server entry point
  mcp/          # MCP server entry point
internal/
  api/          # REST handlers, routing, middleware (CORS, rate limiting, logging)
  mcp/          # MCP tools and server implementation
  data/         # Data loading, indexing, and searching; english/ contains embedded JSON
  domain/       # Core types: Card, Set, Keyword, Ability, Legality
```

**Key patterns:**
- Card data is embedded via `go:embed` from `internal/data/english/` (merged from all branches of the `data/upstream/` git submodule by `scripts/sync-data.sh`)
- Store is loaded at startup and passed via dependency injection to handlers
- Rate limiting uses per-IP token buckets; honors X-Forwarded-For only from TRUSTED_PROXIES

# Version control (jujutsu)

This repo is managed with **jujutsu (`jj`)**, with git as an export backend. Use `jj`, not raw `git`, for commits and history.

- The working copy **is** a commit (`@`). Every file edit is auto-snapshotted into `@` — there is no staging step.
- **Before starting new work, run `jj status`.** If `@` already contains someone else's in-progress changes (or a described commit), run `jj new` to start a fresh empty change first. Otherwise your edits get snapshotted into that existing commit and conflated with unrelated work. This bit us once: edits meant to be their own commit were folded into an unrelated `chore(security)` commit and had to be split back out.
- `git status` can report **clean** while the working copy has uncommitted changes (git sees only the last jj-exported commit, often as a detached HEAD). Trust `jj status`, never `git status`, to know the real state.
- Use conventional-commit messages via `jj describe -m "..."`. Keep each commit to one logical change; `jj split` a commit that ended up mixing concerns.
- Everything is reversible: `jj undo` reverts the last operation, `jj op log` shows the full history of operations to recover from. `jj evolog -r <rev>` shows a single commit's prior snapshots — useful for finding a clean boundary when splitting.

# Configuration

Environment variables (see `.env.example`):
- `PORT` - API server port (default: 8080)
- `CORS_ORIGINS` - Comma-separated origins (default: `*`)
- `RATE_LIMIT_RPS` - Requests per second limit (default: 100)
- `TRUSTED_PROXIES` - CIDR blocks for proxy header trust
- `MCP_MODE` - `stdio` or `http` (default: stdio)
- `MCP_PORT` - MCP HTTP port (default: 8081)
