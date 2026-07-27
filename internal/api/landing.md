# goagain - Flesh and Blood Card API & MCP Server

Free API for Flesh and Blood TCG card data. No API key required. REST endpoints and MCP server for Claude, ChatGPT, and custom apps.

## Features

- **REST API** - Simple JSON endpoints for card data, sets, keywords, and abilities
- **MCP Server** - Model Context Protocol server for AI assistants (Claude, ChatGPT)
- **Always Current** - Card data synced from the official Flesh and Blood card database

## API Endpoints

| Method | Path | Description |
|--------|------|-------------|
| GET | `/` | Landing page (HTML) or API info (JSON with `Accept: application/json`) |
| GET | `/health` | Health check with stats |
| GET | `/docs` | Interactive API documentation |
| GET | `/openapi.yaml` | OpenAPI 3.0 specification |
| GET | `/v1/cards` | List/search cards (params: name, type, class, set, pitch, keyword, q, legal_in, limit, offset) |
| GET | `/v1/cards/{id}` | Get card by unique_id or name |
| GET | `/v1/cards/{id}/legality` | Get card legality across all formats |
| GET | `/v1/sets` | List/search sets (params: name, id, q) |
| GET | `/v1/sets/{id}` | Get set details with cards |
| GET | `/v1/keywords` | List all keywords |
| GET | `/v1/keywords/{name}` | Get keyword description |
| GET | `/v1/abilities` | List all abilities |

## MCP Tools

The MCP server exposes the following tools for AI assistants:

- `search_cards` - Search cards by name, type, class, set, pitch, or keyword
- `get_card` - Get full card details by unique_id or name
- `list_sets` - List all card sets
- `search_sets` - Search sets by name, id, or query
- `get_set` - Get set details with optional card list
- `search_card_text` - Full-text search in card ability text
- `get_format_legality` - Check card legality across all formats
- `get_banned_list` - List cards banned, suspended, restricted, or Living Legend in a format
- `list_keywords` - List all game keywords
- `get_keyword` - Get keyword description by name

## Quick Start

### REST API

```bash
# Search for cards
curl https://api.goagain.dev/v1/cards?name=Enlightened+Strike

# Get a specific card
curl https://api.goagain.dev/v1/cards/enlightened-strike

# List all sets
curl https://api.goagain.dev/v1/sets
```

### MCP Server

Add to your Claude Desktop configuration:

```json
{
  "mcpServers": {
    "goagain": {
      "url": "https://mcp.goagain.dev/"
    }
  }
}
```

## Agent Discovery

- [API Catalog](/.well-known/api-catalog) - RFC 9727 API discovery
- [MCP Server Card](/.well-known/mcp/server-card.json) - MCP server metadata
- [Agent Skills Index](/.well-known/agent-skills/index.json) - Skills discovery
- [OpenAPI Spec](/openapi.yaml) - OpenAPI 3.0 specification
- [Sitemap](/sitemap.xml) - XML sitemap
- [Robots.txt](/robots.txt) - Crawler rules and content signals

## Links

- [Documentation](https://api.goagain.dev/docs)
- [GitHub](https://github.com/oleiade/goagain)
- [Flesh and Blood](https://fabtcg.com)
