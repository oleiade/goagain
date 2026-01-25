# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

goagain is a Go REST API and MCP (Model Context Protocol) server providing access to Flesh and Blood card game data. It produces two binaries: an API server (port 8080) and an MCP server (port 8081).

## Commands

```bash
# Build
go build -v ./...

# Test (with race detector)
go test -race -v ./...

# Lint
golangci-lint run

# Run API server
go run ./cmd/api

# Run MCP server
go run ./cmd/mcp

# Run k6 load tests (requires running API server)
k6 run tests/k6/api.js
k6 run tests/k6/trusted-proxies.js

# Sync card data from upstream submodule
./scripts/sync-data.sh
```

## Architecture

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
- Card data is embedded via `go:embed` from `internal/data/english/` (sourced from `data/upstream/` git submodule)
- Store is loaded at startup and passed via dependency injection to handlers
- Rate limiting uses per-IP token buckets; honors X-Forwarded-For only from TRUSTED_PROXIES

## Configuration

Environment variables (see `.env.example`):
- `PORT` - API server port (default: 8080)
- `CORS_ORIGINS` - Comma-separated origins (default: `*`)
- `RATE_LIMIT_RPS` - Requests per second limit (default: 100)
- `TRUSTED_PROXIES` - CIDR blocks for proxy header trust
- `MCP_MODE` - `stdio` or `http` (default: stdio)
- `MCP_PORT` - MCP HTTP port (default: 8081)
