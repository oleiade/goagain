# goagain

A REST API and MCP (Model Context Protocol) server for [Flesh and Blood](https://fabtcg.com) card game data.

## Features

- **REST API** - Query cards, sets, keywords, and abilities with filtering and pagination
- **MCP Server** - Integrate Flesh and Blood card data into AI assistants (Claude, etc.)
- **Format Legality** - Check card legality across Blitz, Classic Constructed, Commoner, Living Legend, Silver Age, and UPF
- **Full-Text Search** - Search card abilities and effects
- **Observability** - OpenTelemetry traces, metrics, and logs for production deployments
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
| `SERVICE_NAME` | `goagain-api` / `goagain-mcp` | Service name for logs |
| `METRICS_ENABLED` | `true` | Enable OTel metrics collection |

### OpenTelemetry

| Variable | Default | Description |
|----------|---------|-------------|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | _(none)_ | OTLP endpoint (e.g., `localhost:4318`). If unset, telemetry goes to stdout |
| `OTEL_SERVICE_NAME` | `goagain-api` / `goagain-mcp` | Service name for traces, metrics, and logs |
| `OTEL_SERVICE_VERSION` | `0.1.0` | Service version reported in telemetry |
| `OTEL_ENVIRONMENT` | `development` | Deployment environment (e.g., `production`, `staging`) |

## Observability

Both servers include built-in OpenTelemetry observability for production deployments, providing distributed tracing, metrics, and structured logs.

### OpenTelemetry Integration

By default, all telemetry is output to stdout (useful for local development). To send telemetry to an OTLP-compatible collector (e.g., Grafana Alloy, Jaeger, or any OTel Collector), set the `OTEL_EXPORTER_OTLP_ENDPOINT` environment variable:

```bash
# Send telemetry to a local collector (applies to both API and MCP servers)
export OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4318

# Optional: customize service metadata
export OTEL_SERVICE_NAME=goagain-api  # or goagain-mcp for MCP server
export OTEL_SERVICE_VERSION=1.0.0
export OTEL_ENVIRONMENT=production
```

Both the API server (`cmd/api`) and MCP server (`cmd/mcp`) use identical OTel configuration and export traces, metrics, and logs to the same endpoint.

### Distributed Tracing

**HTTP Requests** (both API and MCP servers):

HTTP requests are automatically traced using `otelhttp`. Each request creates a span with:
- HTTP method, route, and status code
- Request/response sizes
- Timing information
- Trace context propagation (W3C TraceContext and Baggage)

**MCP Tool Invocations** (MCP server only):

Each MCP tool call creates a child span (`mcp.tool.<name>`) with:
- Tool name and execution duration
- Result count and error status
- Linked to parent HTTP span (in HTTP mode)

### Metrics

Metrics are collected using the OTel Metrics API and exported via OTLP.

**HTTP Metrics:**
- `http.server.request.total` - Total HTTP requests
- `http.server.request.duration` - Request latency histogram (seconds)
- `http.server.active_requests` - Current in-flight requests
- `http.server.response.size` - Response size histogram (bytes)
- `http.server.rate_limit.rejected` - Rate limit rejections (API only)

**MCP Tool Metrics:**
- `mcp.tool.invocations.total` - Tool invocation count
- `mcp.tool.duration` - Tool execution latency (seconds)
- `mcp.tool.result_count` - Results returned per invocation
- `mcp.tool.active` - In-flight tool invocations

**Application Metrics:**
- `goagain.data.cards` - Total cards loaded
- `goagain.data.sets` - Total sets loaded
- `goagain.data.keywords` - Total keywords loaded
- `goagain.data.abilities` - Total abilities loaded
- `goagain.data.index_entries` - Index entries by index name

### Structured Logging

Logs are output to stdout in structured JSON format and also sent to the OTel log pipeline:

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

For Grafana Cloud, configure [Grafana Alloy](https://grafana.com/docs/alloy/) to receive OTLP telemetry:

```alloy
otelcol.receiver.otlp "default" {
  grpc { endpoint = "0.0.0.0:4317" }
  http { endpoint = "0.0.0.0:4318" }

  output {
    traces  = [otelcol.exporter.otlp.grafana.input]
    metrics = [otelcol.exporter.otlp.grafana.input]
    logs    = [otelcol.exporter.otlp.grafana.input]
  }
}
```

Then point the application to Alloy:

```bash
export OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4318
```

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
