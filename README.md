# goagain

A REST API and MCP (Model Context Protocol) server for [Flesh and Blood](https://fabtcg.com) card game data.

## Features

- **REST API** - Query cards, sets, keywords, and abilities with filtering and pagination
- **MCP Server** - Integrate Flesh and Blood card data into AI assistants (Claude, etc.)
- **Format Legality** - Check card legality across Blitz, Classic Constructed, Commoner, Living Legend, Silver Age, and UPF
- **Full-Text Search** - Search card abilities and effects
- **Observability** - Prometheus metrics and structured JSON logging for production deployments
- **Docker Ready** - Multi-platform container images for easy deployment

## Hosted Service

Public instances are available at:

- **REST API**: https://api.goagain.dev
- **MCP Server**: https://mcp.goagain.dev

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
| `GET /metrics` | Prometheus metrics (when enabled) |

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
curl "https://api.goagain.dev/cards?class=Ninja&type=Attack"

# Find cards with "draw" in their text
curl "https://api.goagain.dev/cards?q=draw"

# Get a specific card
curl "https://api.goagain.dev/cards/WTR001"

# Check format legality
curl "https://api.goagain.dev/cards/WTR001/legality"

# List all sets
curl "https://api.goagain.dev/sets"
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

**Using the hosted service:**

```json
{
  "mcpServers": {
    "fab-cards": {
      "url": "https://mcp.goagain.dev/mcp"
    }
  }
}
```

**Using a local binary:**

```json
{
  "mcpServers": {
    "fab-cards": {
      "command": "/path/to/goagain-mcp"
    }
  }
}
```

**Using local HTTP mode:**

```json
{
  "mcpServers": {
    "fab-cards": {
      "url": "http://localhost:8081/mcp"
    }
  }
}
```

## Configuration

All configuration is via environment variables. See `.env.example` for a complete template.

### API Server

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT` | `8080` | API server port |
| `CORS_ORIGINS` | `*` | Comma-separated allowed origins |
| `RATE_LIMIT_RPS` | `100` | Rate limit (requests per second per IP) |
| `TRUSTED_PROXIES` | | Comma-separated CIDR blocks for proxy header trust |
| `API_BASE_URL` | `https://api.goagain.dev` | Base URL shown in landing page and docs |
| `MCP_BASE_URL` | `https://mcp.goagain.dev` | MCP URL shown in landing page |

### MCP Server

| Variable | Default | Description |
|----------|---------|-------------|
| `MCP_MODE` | `stdio` | MCP transport: `stdio` or `http` |
| `MCP_PORT` | `8081` | MCP HTTP server port |

### Observability

| Variable | Default | Description |
|----------|---------|-------------|
| `LOG_LEVEL` | `info` | Log level: `debug`, `info`, `warn`, `error` |
| `LOG_FORMAT` | `json` | Log format: `json` or `text` |
| `SERVICE_NAME` | `goagain-api` / `goagain-mcp` | Service name for metrics and logs |
| `METRICS_ENABLED` | `true` | Enable Prometheus metrics endpoint |
| `METRICS_PATH` | `/metrics` | Path for metrics endpoint |

## Observability

Both servers include built-in observability features for production deployments.

### Prometheus Metrics

When enabled, metrics are exposed at `/metrics` (configurable via `METRICS_PATH`).

**HTTP Metrics:**
- `http_requests_total{method,path,status_code}` - Total HTTP requests
- `http_request_duration_seconds{method,path,status_code}` - Request latency histogram
- `http_requests_in_flight` - Current in-flight requests
- `http_response_size_bytes{method,path,status_code}` - Response size histogram
- `http_rate_limit_rejections_total` - Rate limit rejections (API only)

**MCP Tool Metrics:**
- `mcp_tool_invocations_total{tool_name,status}` - Tool invocation count
- `mcp_tool_duration_seconds{tool_name,status}` - Tool execution latency
- `mcp_tool_result_count{tool_name}` - Results returned per invocation
- `mcp_tool_in_flight{tool_name}` - In-flight tool invocations

**Application Metrics:**
- `goagain_data_cards_total` - Total cards loaded
- `goagain_data_sets_total` - Total sets loaded
- `goagain_data_keywords_total` - Total keywords loaded
- `goagain_data_abilities_total` - Total abilities loaded

Plus standard Go runtime metrics (`go_*`, `process_*`).

### Structured Logging

Logs are output to stdout in structured JSON format (configurable to text):

```json
{
  "time": "2025-01-25T14:30:00.123Z",
  "level": "INFO",
  "msg": "HTTP request completed",
  "service": "goagain-api",
  "request_id": "01HQXYZ123ABC",
  "method": "GET",
  "path": "/cards",
  "status": 200,
  "duration_ms": 12.5,
  "client_ip": "192.168.1.100"
}
```

### Grafana Cloud Integration

For Grafana Cloud, configure [Grafana Alloy](https://grafana.com/docs/alloy/) to:
1. Scrape the `/metrics` endpoint for Prometheus metrics
2. Collect stdout logs for structured logging

## Development

### Prerequisites

- Go 1.25+
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
