# goagain

A REST API and MCP (Model Context Protocol) server for [Flesh and Blood](https://fabtcg.com) card game data.

## Features

- **REST API** - Query cards, sets, keywords, and abilities with filtering and pagination
- **MCP Server** - Integrate Flesh and Blood card data into AI assistants (Claude, etc.)
- **Format Legality** - Check card legality across Blitz, Classic Constructed, Commoner, Living Legend, Silver Age, and UPF
- **Full-Text Search** - Search card abilities and effects
- **Docker Ready** - Multi-platform container images for easy deployment

## Quick Start

### Run with Go

```bash
# Clone the repository
git clone --recursive https://github.com/oleiade/goagain.git
cd goagain

# Run the API server (default port 8080)
go run ./cmd/api

# Or run the MCP server (default port 8081)
go run ./cmd/mcp
```

### Run with Docker

```bash
# API server
docker run -p 8080:8080 ghcr.io/oleiade/goagain-api

# MCP server (HTTP mode)
docker run -p 8081:8081 -e MCP_MODE=http ghcr.io/oleiade/goagain-mcp
```

## REST API

### Endpoints

| Endpoint | Description |
|----------|-------------|
| `GET /` | Landing page (HTML) or API info (JSON with `Accept: application/json`) |
| `GET /health` | Health check with data statistics |
| `GET /docs` | Interactive Swagger UI documentation |
| `GET /openapi.yaml` | OpenAPI 3.0 specification |
| `GET /cards` | List/search cards |
| `GET /cards/{id}` | Get card by unique ID or name |
| `GET /cards/{id}/legality` | Get card legality across all formats |
| `GET /sets` | List/search sets |
| `GET /sets/{id}` | Get set details with cards |
| `GET /keywords` | List all keywords |
| `GET /keywords/{name}` | Get keyword description |
| `GET /abilities` | List all abilities |

### Card Search Parameters

| Parameter | Description |
|-----------|-------------|
| `name` | Filter by card name (partial match) |
| `type` | Filter by card type (e.g., `Action`, `Attack`, `Equipment`) |
| `class` | Filter by class (e.g., `Warrior`, `Ninja`, `Wizard`) |
| `set` | Filter by set code (e.g., `WTR`, `ARC`, `MON`) |
| `pitch` | Filter by pitch value (`1`, `2`, or `3`) |
| `keyword` | Filter by keyword (e.g., `Go again`, `Dominate`) |
| `q` | Full-text search in card abilities |
| `legal_in` | Filter by format legality (`blitz`, `cc`, `commoner`, `ll`, `silver_age`, `upf`) |
| `limit` | Results per page (default 50, max 100) |
| `offset` | Pagination offset |

### Examples

```bash
# Search for Ninja attack actions
curl "http://localhost:8080/cards?class=Ninja&type=Attack"

# Find cards with "draw" in their text
curl "http://localhost:8080/cards?q=draw"

# Get a specific card
curl "http://localhost:8080/cards/WTR001"

# Check format legality
curl "http://localhost:8080/cards/WTR001/legality"

# List all sets
curl "http://localhost:8080/sets"
```

## MCP Server

The MCP server allows AI assistants to query Flesh and Blood card data. It supports both stdio (for local integrations) and HTTP transports.

### Tools

| Tool | Description |
|------|-------------|
| `search_cards` | Search cards by name, type, class, set, pitch, or keyword |
| `get_card` | Get full details of a card by ID or name |
| `list_sets` | List all card sets |
| `search_sets` | Search sets by name or code |
| `get_set` | Get set details with optional card list |
| `search_card_text` | Full-text search in card abilities |
| `get_format_legality` | Check card legality across all formats |
| `list_keywords` | List all game keywords |
| `get_keyword` | Get keyword description |

### Claude Desktop Integration

Add to your Claude Desktop configuration (`claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "fab-cards": {
      "command": "/path/to/goagain-mcp"
    }
  }
}
```

Or using HTTP mode:

```json
{
  "mcpServers": {
    "fab-cards": {
      "url": "http://localhost:8081"
    }
  }
}
```

## Configuration

Configuration is via environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | API server port |
| `CORS_ORIGINS` | `*` | Comma-separated allowed origins |
| `RATE_LIMIT_RPS` | `100` | Rate limit (requests per second per IP) |
| `TRUSTED_PROXIES` | | Comma-separated CIDR blocks for proxy header trust |
| `MCP_MODE` | `stdio` | MCP transport: `stdio` or `http` |
| `MCP_PORT` | `8081` | MCP HTTP server port |

## Development

### Prerequisites

- Go 1.23+
- [golangci-lint](https://golangci-lint.run/) (for linting)
- [k6](https://k6.io/) (for load testing, optional)

### Commands

```bash
# Build
go build -v ./...

# Test
go test -race -v ./...

# Lint
golangci-lint run

# Run load tests (requires running API server)
k6 run tests/k6/api.js
```

### Updating Card Data

Card data is sourced from an upstream submodule. To update:

```bash
./scripts/sync-data.sh
```

## Contributing

Contributions are welcome! Please:

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Make your changes
4. Run tests and linting (`go test -race -v ./... && golangci-lint run`)
5. Commit your changes using [conventional commits](https://www.conventionalcommits.org/)
6. Push to your branch (`git push origin feature/amazing-feature`)
7. Open a Pull Request

## Data Attribution

All card data is sourced from the [flesh-and-blood-cards](https://github.com/the-fab-cube/flesh-and-blood-cards) project maintained by [The Fab Cube](https://github.com/the-fab-cube). This community-driven project provides comprehensive, machine-readable data for Flesh and Blood cards.

We are deeply grateful to the maintainers and contributors of flesh-and-blood-cards for their work in making this data available to the community.

## Legal Disclaimer

**Flesh and Blood** is a trademark of Legend Story Studios (LSS). All card names, artwork, and game mechanics are the intellectual property of Legend Story Studios.

This project is not produced, endorsed, supported, or affiliated with Legend Story Studios. This is an unofficial, fan-made project created for educational and community purposes.

The card data provided by this API is derived from publicly available information and is intended to help players, developers, and content creators build tools and applications for the Flesh and Blood community.

All trademarks and copyrights belong to their respective owners. Use of Flesh and Blood card data should comply with Legend Story Studios' [Community Guidelines](https://fabtcg.com/resources/community-guidelines/) and [Intellectual Property Policy](https://fabtcg.com/resources/ip-policy/).

## License

This project is open source. See [LICENSE](LICENSE) for details.
