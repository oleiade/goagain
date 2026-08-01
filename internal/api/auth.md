# auth.md

goagain requires **no authentication**. There are no API keys, no OAuth flows,
no registration, and no credentials to provision. Every endpoint is public and
anonymous.

This file exists so an agent can establish that fact directly, instead of
inferring it from the absence of a challenge.

## Services

| Service | Base URL | Authentication |
|---------|----------|----------------|
| REST API | https://api.goagain.dev | None |
| MCP server | https://mcp.goagain.dev | None |

Send no `Authorization` header. One sent anyway is ignored, not rejected.

No endpoint returns `401` or `403`. If you receive either, it came from
infrastructure in front of the application, not from goagain.

## Why there is no OAuth discovery metadata

`/.well-known/openid-configuration` and `/.well-known/oauth-authorization-server`
return `404`, deliberately. Serving them would advertise an authorization
server, token endpoint, and JWKS URI that do not exist, and send agents into a
flow that cannot complete. The absence of those documents is the accurate
signal: there is nothing to authenticate against.

The same reasoning applies to `/.well-known/oauth-protected-resource`. goagain
is not a protected resource.

## Rate limits

Requests are limited to **100 per second per IP address**, with a second global
tier protecting against IP rotation. This applies equally to every client;
there is no registration tier, and no way to request a higher allowance.

Exceeding the limit returns:

```
HTTP/1.1 429 Too Many Requests
Retry-After: 1
Content-Type: application/json; charset=utf-8

{"error": "rate limit exceeded"}
```

Honour `Retry-After`. The bucket refills continuously, so a one second pause is
genuinely enough.

## Identifying your agent

Send a descriptive `User-Agent` naming your tool and a contact URL. This is not
enforced and never gates access. It only means that if your traffic pattern
causes a problem, the first response can be an email rather than a block.

## Data provenance and reuse

Card data derives from the public
[the-fab-cube/flesh-and-blood-cards](https://github.com/the-fab-cube/flesh-and-blood-cards)
dataset. `robots.txt` permits all AI crawlers, including training, and
`Content-Signal` declares `ai-train=yes, search=yes, ai-input=yes`.

Flesh and Blood is a trademark of Legend Story Studios. goagain is an
unaffiliated community project.

## Discovery

- [/.well-known/api-catalog](https://api.goagain.dev/.well-known/api-catalog) - RFC 9727 API catalog
- [/.well-known/mcp/server-card.json](https://api.goagain.dev/.well-known/mcp/server-card.json) - MCP server card
- [/.well-known/agent-skills/index.json](https://api.goagain.dev/.well-known/agent-skills/index.json) - agent skills index
- [/openapi.yaml](https://api.goagain.dev/openapi.yaml) - OpenAPI 3.0 specification

## Contact

Report problems at
[github.com/oleiade/goagain/issues](https://github.com/oleiade/goagain/issues).
