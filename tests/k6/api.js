import http from "k6/http";
import { check, group, sleep } from "k6";
import { Rate, Trend } from "k6/metrics";

// Custom metrics
const errorRate = new Rate("api_errors");
const cardSearchDuration = new Trend("card_search_duration");
const cardGetDuration = new Trend("card_get_duration");

const BASE_URL = __ENV.API_BASE_URL || "http://localhost:8080";

export const options = {
  scenarios: {
    // Smoke test: quick conformance check (CI default)
    smoke: {
      executor: "shared-iterations",
      vus: 1,
      iterations: 1,
      tags: { scenario: "smoke" },
    },
    // Light load: sustained traffic for availability
    load: {
      executor: "ramping-vus",
      startVUs: 0,
      stages: [
        { duration: "10s", target: 5 },
        { duration: "30s", target: 5 },
        { duration: "10s", target: 0 },
      ],
      startTime: "5s",
      tags: { scenario: "load" },
    },
  },
  thresholds: {
    // CI gates: these fail the pipeline if breached
    http_req_failed: ["rate<0.01"],
    http_req_duration: ["p(95)<2000", "p(99)<5000"],
    api_errors: ["rate<0.01"],
    checks: ["rate>0.99"],
    card_search_duration: ["p(95)<2000"],
    card_get_duration: ["p(95)<1000"],
  },
};

// -- Helpers --

const jsonHeaders = { headers: { Accept: "application/json" } };

function hasFields(obj, fields) {
  if (typeof obj !== "object" || obj === null) return false;
  for (const f of fields) {
    if (!(f in obj)) return false;
  }
  return true;
}

// -- Test functions --

function testHealth() {
  group("Health", () => {
    const res = http.get(`${BASE_URL}/health`);
    const body = res.json();

    const ok = check(res, {
      "GET /health returns 200": (r) => r.status === 200,
      "health status is ok": () => body.status === "ok",
      "health has stats object": () => hasFields(body, ["status", "stats"]),
      "stats has expected counters": () =>
        hasFields(body.stats, ["cards", "sets", "keywords", "abilities"]),
      "stats cards > 0": () => body.stats.cards > 0,
      "stats sets > 0": () => body.stats.sets > 0,
      "stats keywords > 0": () => body.stats.keywords > 0,
    });
    errorRate.add(!ok);
  });
}

function testIndex() {
  group("Index", () => {
    const res = http.get(`${BASE_URL}/`, jsonHeaders);
    const body = res.json();

    const ok = check(res, {
      "GET / returns 200": (r) => r.status === 200,
      "index has name": () => typeof body.name === "string",
      "index has version": () => typeof body.version === "string",
      "index has endpoints map": () =>
        typeof body.endpoints === "object" && body.endpoints !== null,
      "index has stats": () => hasFields(body, ["stats"]),
      "index has Link header with api-catalog": (r) => {
        const links = r.headers["Link"];
        if (!links) return false;
        return links.includes('rel="api-catalog"');
      },
      "index has Link header with service-doc": (r) => {
        const links = r.headers["Link"];
        if (!links) return false;
        return links.includes('rel="service-doc"');
      },
      "index has Link header with service-desc": (r) => {
        const links = r.headers["Link"];
        if (!links) return false;
        return links.includes('rel="service-desc"');
      },
    });
    errorRate.add(!ok);
  });
}

function testOpenAPISpec() {
  group("OpenAPI Spec", () => {
    const res = http.get(`${BASE_URL}/openapi.yaml`);

    const ok = check(res, {
      "GET /openapi.yaml returns 200": (r) => r.status === 200,
      "openapi.yaml content-type is yaml": (r) =>
        r.headers["Content-Type"].includes("yaml") ||
        r.headers["Content-Type"].includes("text/plain"),
      "openapi.yaml body contains openapi version": (r) =>
        r.body.includes("openapi: 3.0"),
      "openapi.yaml body contains paths": (r) => r.body.includes("paths:"),
    });
    errorRate.add(!ok);

    // Also test /openapi alias
    const resAlias = http.get(`${BASE_URL}/openapi`);
    check(resAlias, {
      "GET /openapi alias returns 200": (r) => r.status === 200,
    });
  });
}

function testListCards() {
  group("Cards - List", () => {
    // Default listing
    const res = http.get(`${BASE_URL}/v1/cards`);
    const body = res.json();
    cardSearchDuration.add(res.timings.duration);

    const ok = check(res, {
      "GET /v1/cards returns 200": (r) => r.status === 200,
      "response has paginated shape": () =>
        hasFields(body, ["data", "total", "limit", "offset"]),
      "data is array": () => Array.isArray(body.data),
      "default limit is 50": () => body.limit === 50,
      "default offset is 0": () => body.offset === 0,
      "total > 0": () => body.total > 0,
      "data length <= limit": () => body.data.length <= body.limit,
    });
    errorRate.add(!ok);

    // Verify card schema on first item
    if (body.data && body.data.length > 0) {
      const card = body.data[0];
      check(card, {
        "card has unique_id": (c) => typeof c.unique_id === "string",
        "card has name": (c) => typeof c.name === "string",
        "card has types array": (c) => Array.isArray(c.types),
        "card has printings array": (c) => Array.isArray(c.printings),
      });
    }
  });
}

function testSearchCards() {
  group("Cards - Search", () => {
    // Search by name
    const byName = http.get(`${BASE_URL}/v1/cards?name=Enlightened+Strike`);
    cardSearchDuration.add(byName.timings.duration);
    const nameBody = byName.json();

    check(byName, {
      "search by name returns 200": (r) => r.status === 200,
      "search by name returns results": () => nameBody.total > 0,
      "search results match name filter": () =>
        nameBody.data.every((c) =>
          c.name.toLowerCase().includes("enlightened strike")
        ),
    });

    // Search by class
    const byClass = http.get(`${BASE_URL}/v1/cards?class=Ninja&limit=5`);
    cardSearchDuration.add(byClass.timings.duration);
    const classBody = byClass.json();

    check(byClass, {
      "search by class returns 200": (r) => r.status === 200,
      "search by class returns results": () => classBody.total > 0,
      "search by class respects limit": () => classBody.data.length <= 5,
    });

    // Search by pitch
    const byPitch = http.get(`${BASE_URL}/v1/cards?pitch=1&limit=3`);
    cardSearchDuration.add(byPitch.timings.duration);

    check(byPitch, {
      "search by pitch returns 200": (r) => r.status === 200,
      "search by pitch returns results": () => byPitch.json().total > 0,
    });

    // Search by keyword
    const byKeyword = http.get(
      `${BASE_URL}/v1/cards?keyword=Go+again&limit=3`
    );
    cardSearchDuration.add(byKeyword.timings.duration);

    check(byKeyword, {
      "search by keyword returns 200": (r) => r.status === 200,
      "search by keyword returns results": () => byKeyword.json().total > 0,
    });

    // Full-text search
    const byText = http.get(`${BASE_URL}/v1/cards?q=draw+a+card&limit=3`);
    cardSearchDuration.add(byText.timings.duration);

    check(byText, {
      "full-text search returns 200": (r) => r.status === 200,
      "full-text search returns results": () => byText.json().total > 0,
    });

    // Search by format legality
    const byLegality = http.get(
      `${BASE_URL}/v1/cards?legal_in=blitz&limit=3`
    );
    cardSearchDuration.add(byLegality.timings.duration);

    check(byLegality, {
      "search by legality returns 200": (r) => r.status === 200,
      "search by legality returns results": () => byLegality.json().total > 0,
    });

    // Combined filters
    const combined = http.get(
      `${BASE_URL}/v1/cards?class=Warrior&pitch=1&limit=3`
    );
    cardSearchDuration.add(combined.timings.duration);

    check(combined, {
      "combined filter returns 200": (r) => r.status === 200,
    });
  });
}

function testPagination() {
  group("Cards - Pagination", () => {
    // Custom limit
    const res = http.get(`${BASE_URL}/v1/cards?limit=5&offset=10`);
    const body = res.json();

    check(res, {
      "pagination returns 200": (r) => r.status === 200,
      "pagination respects limit": () => body.data.length <= 5,
      "pagination limit reflects param": () => body.limit === 5,
      "pagination offset reflects param": () => body.offset === 10,
    });

    // Max limit cap at 100
    const capped = http.get(`${BASE_URL}/v1/cards?limit=200`);
    const cappedBody = capped.json();

    check(capped, {
      "limit is capped at 100": () => cappedBody.limit === 100,
      "capped response length <= 100": () => cappedBody.data.length <= 100,
    });
  });
}

function testGetCard() {
  group("Cards - Get by ID", () => {
    // First, fetch a card ID from the list
    const listRes = http.get(`${BASE_URL}/v1/cards?limit=1`);
    const listBody = listRes.json();

    if (!listBody.data || listBody.data.length === 0) {
      console.error("No cards available for GetCard test");
      errorRate.add(true);
      return;
    }

    const cardId = listBody.data[0].unique_id;
    const cardName = listBody.data[0].name;

    // Get by unique_id
    const byId = http.get(`${BASE_URL}/v1/cards/${encodeURIComponent(cardId)}`);
    cardGetDuration.add(byId.timings.duration);
    const cardBody = byId.json();

    const ok = check(byId, {
      "GET /v1/cards/:id returns 200": (r) => r.status === 200,
      "card has unique_id": () => cardBody.unique_id === cardId,
      "card has name": () => typeof cardBody.name === "string",
      "card has types": () => Array.isArray(cardBody.types),
      "card has printings": () => Array.isArray(cardBody.printings),
      "card has functional_text": () =>
        typeof cardBody.functional_text === "string" ||
        cardBody.functional_text === "",
      "card has type_text": () => typeof cardBody.type_text === "string",
      "card has legality booleans": () =>
        typeof cardBody.blitz_legal === "boolean" &&
        typeof cardBody.cc_legal === "boolean",
    });
    errorRate.add(!ok);

    // Get by name
    const byName = http.get(
      `${BASE_URL}/v1/cards/${encodeURIComponent(cardName)}`
    );
    cardGetDuration.add(byName.timings.duration);

    check(byName, {
      "GET /v1/cards/:name returns 200": (r) => r.status === 200,
      "card by name matches": () => byName.json().name === cardName,
    });
  });
}

function testGetCardNotFound() {
  group("Cards - Not Found", () => {
    const res = http.get(`${BASE_URL}/v1/cards/nonexistent-card-id-12345`);
    const body = res.json();

    check(res, {
      "missing card returns 404": (r) => r.status === 404,
      "404 has error field": () =>
        hasFields(body, ["error"]) && typeof body.error === "string",
    });
  });
}

function testCardLegality() {
  group("Cards - Legality", () => {
    // Get a card ID first
    const listRes = http.get(`${BASE_URL}/v1/cards?limit=1`);
    const listBody = listRes.json();

    if (!listBody.data || listBody.data.length === 0) {
      errorRate.add(true);
      return;
    }

    const cardId = listBody.data[0].unique_id;

    const res = http.get(
      `${BASE_URL}/v1/cards/${encodeURIComponent(cardId)}/legality`
    );
    const body = res.json();

    const ok = check(res, {
      "GET legality returns 200": (r) => r.status === 200,
      "legality has card_id": () => body.card_id === cardId,
      "legality has card_name": () => typeof body.card_name === "string",
      "legality has legalities array": () => Array.isArray(body.legalities),
      "legalities covers all 6 formats": () => body.legalities.length === 6,
    });
    errorRate.add(!ok);

    // Validate legality entry schema
    if (body.legalities && body.legalities.length > 0) {
      const entry = body.legalities[0];
      check(entry, {
        "legality entry has format": (e) => typeof e.format === "string",
        "legality entry has legal boolean": (e) => typeof e.legal === "boolean",
        "legality entry has banned boolean": (e) =>
          typeof e.banned === "boolean",
        "legality entry has suspended boolean": (e) =>
          typeof e.suspended === "boolean",
      });
    }

    // Legality 404
    const notFound = http.get(
      `${BASE_URL}/v1/cards/nonexistent-id-xyz/legality`
    );
    check(notFound, {
      "legality for missing card returns 404": (r) => r.status === 404,
    });
  });
}

function testListSets() {
  group("Sets - List", () => {
    const res = http.get(`${BASE_URL}/v1/sets`);
    const body = res.json();

    const ok = check(res, {
      "GET /v1/sets returns 200": (r) => r.status === 200,
      "sets is array": () => Array.isArray(body),
      "sets has items": () => body.length > 0,
    });
    errorRate.add(!ok);

    if (body.length > 0) {
      const set = body[0];
      check(set, {
        "set has unique_id": (s) => typeof s.unique_id === "string",
        "set has id": (s) => typeof s.id === "string",
        "set has name": (s) => typeof s.name === "string",
        "set has printings": (s) => Array.isArray(s.printings),
      });
    }
  });
}

function testSearchSets() {
  group("Sets - Search", () => {
    // Search by name
    const byName = http.get(`${BASE_URL}/v1/sets?name=Welcome`);
    check(byName, {
      "search sets by name returns 200": (r) => r.status === 200,
      "search sets by name is array": () => Array.isArray(byName.json()),
    });

    // Search by q
    const byQ = http.get(`${BASE_URL}/v1/sets?q=WTR`);
    check(byQ, {
      "search sets by q returns 200": (r) => r.status === 200,
      "search sets by q is array": () => Array.isArray(byQ.json()),
    });
  });
}

function testGetSet() {
  group("Sets - Get by ID", () => {
    const res = http.get(`${BASE_URL}/v1/sets/WTR`);
    const body = res.json();

    const ok = check(res, {
      "GET /v1/sets/WTR returns 200": (r) => r.status === 200,
      "set has id field": () => body.id === "WTR",
      "set has name": () => typeof body.name === "string",
      "set has cards array": () => Array.isArray(body.cards),
      "set cards is non-empty": () => body.cards.length > 0,
    });
    errorRate.add(!ok);

    // Set not found
    const notFound = http.get(`${BASE_URL}/v1/sets/ZZZZZ`);
    check(notFound, {
      "missing set returns 404": (r) => r.status === 404,
    });
  });
}

function testListKeywords() {
  group("Keywords - List", () => {
    const res = http.get(`${BASE_URL}/v1/keywords`);
    const body = res.json();

    const ok = check(res, {
      "GET /v1/keywords returns 200": (r) => r.status === 200,
      "keywords is array": () => Array.isArray(body),
      "keywords has items": () => body.length > 0,
    });
    errorRate.add(!ok);

    if (body.length > 0) {
      const kw = body[0];
      check(kw, {
        "keyword has unique_id": (k) => typeof k.unique_id === "string",
        "keyword has name": (k) => typeof k.name === "string",
        "keyword has description": (k) => typeof k.description === "string",
      });
    }
  });
}

function testGetKeyword() {
  group("Keywords - Get by name", () => {
    const res = http.get(`${BASE_URL}/v1/keywords/Go%20again`);
    const body = res.json();

    const ok = check(res, {
      "GET /v1/keywords/Go again returns 200": (r) => r.status === 200,
      "keyword name matches": () =>
        body.name.toLowerCase() === "go again",
      "keyword has description": () => typeof body.description === "string",
      "keyword has description_plain": () =>
        typeof body.description_plain === "string",
    });
    errorRate.add(!ok);

    // Keyword not found
    const notFound = http.get(`${BASE_URL}/v1/keywords/NonexistentKeyword123`);
    check(notFound, {
      "missing keyword returns 404": (r) => r.status === 404,
    });
  });
}

function testListAbilities() {
  group("Abilities - List", () => {
    const res = http.get(`${BASE_URL}/v1/abilities`);
    const body = res.json();

    const ok = check(res, {
      "GET /v1/abilities returns 200": (r) => r.status === 200,
      "abilities is array": () => Array.isArray(body),
      "abilities has items": () => body.length > 0,
    });
    errorRate.add(!ok);

    if (body.length > 0) {
      check(body[0], {
        "ability has unique_id": (a) => typeof a.unique_id === "string",
        "ability has name": (a) => typeof a.name === "string",
      });
    }
  });
}

function testMCPServerCard() {
  group("MCP Server Card", () => {
    const res = http.get(`${BASE_URL}/.well-known/mcp/server-card.json`);
    const body = res.json();

    const ok = check(res, {
      "GET /.well-known/mcp/server-card.json returns 200": (r) =>
        r.status === 200,
      "mcp-card content-type is json": (r) =>
        r.headers["Content-Type"].includes("application/json"),
      "mcp-card has serverInfo.name": () =>
        body.serverInfo && body.serverInfo.name === "goagain-mcp",
      "mcp-card has serverInfo.version": () =>
        body.serverInfo && typeof body.serverInfo.version === "string",
      "mcp-card has serverInfo.description": () =>
        body.serverInfo && typeof body.serverInfo.description === "string",
      "mcp-card has transport.type": () =>
        body.transport && body.transport.type === "http",
      "mcp-card has transport.url": () =>
        body.transport && typeof body.transport.url === "string",
      "mcp-card has capabilities.tools": () =>
        body.capabilities && body.capabilities.tools === true,
    });
    errorRate.add(!ok);
  });
}

function testAPICatalog() {
  group("API Catalog", () => {
    const res = http.get(`${BASE_URL}/.well-known/api-catalog`);
    const body = res.json();

    const ok = check(res, {
      "GET /.well-known/api-catalog returns 200": (r) => r.status === 200,
      "api-catalog content-type is linkset+json": (r) =>
        r.headers["Content-Type"].includes("application/linkset+json"),
      "api-catalog has linkset array": () =>
        Array.isArray(body.linkset) && body.linkset.length > 0,
      "api-catalog entry has anchor": () =>
        typeof body.linkset[0].anchor === "string",
      "api-catalog entry has service-desc": () =>
        Array.isArray(body.linkset[0]["service-desc"]) &&
        body.linkset[0]["service-desc"][0].href.includes("/openapi.yaml"),
      "api-catalog entry has service-doc": () =>
        Array.isArray(body.linkset[0]["service-doc"]) &&
        body.linkset[0]["service-doc"][0].href.includes("/docs"),
      "api-catalog entry has status": () =>
        Array.isArray(body.linkset[0]["status"]) &&
        body.linkset[0]["status"][0].href.includes("/health"),
    });
    errorRate.add(!ok);
  });
}

function testSitemapXml() {
  group("Sitemap XML", () => {
    const res = http.get(`${BASE_URL}/sitemap.xml`);

    const ok = check(res, {
      "GET /sitemap.xml returns 200": (r) => r.status === 200,
      "sitemap content-type is xml": (r) =>
        r.headers["Content-Type"].includes("application/xml"),
      "sitemap has urlset root": (r) => r.body.includes("<urlset"),
      "sitemap has homepage loc": (r) =>
        r.body.includes("<loc>") && r.body.includes("</loc>"),
      "sitemap has /docs": (r) => r.body.includes("/docs</loc>"),
      "sitemap has /openapi.yaml": (r) =>
        r.body.includes("/openapi.yaml</loc>"),
      "sitemap has /v1/cards": (r) => r.body.includes("/v1/cards</loc>"),
      "sitemap has /v1/sets": (r) => r.body.includes("/v1/sets</loc>"),
      "sitemap has /v1/keywords": (r) =>
        r.body.includes("/v1/keywords</loc>"),
      "sitemap has /v1/abilities": (r) =>
        r.body.includes("/v1/abilities</loc>"),
    });
    errorRate.add(!ok);
  });
}

function testRobotsTxt() {
  group("Robots.txt", () => {
    const res = http.get(`${BASE_URL}/robots.txt`);

    const ok = check(res, {
      "GET /robots.txt returns 200": (r) => r.status === 200,
      "robots.txt content-type is text/plain": (r) =>
        r.headers["Content-Type"].includes("text/plain"),
      "robots.txt has wildcard user-agent": (r) =>
        r.body.includes("User-agent: *"),
      "robots.txt has GPTBot rules": (r) =>
        r.body.includes("User-agent: GPTBot"),
      "robots.txt has Claude-Web rules": (r) =>
        r.body.includes("User-agent: Claude-Web"),
      "robots.txt has Google-Extended rules": (r) =>
        r.body.includes("User-agent: Google-Extended"),
      "robots.txt has Sitemap directive": (r) =>
        r.body.includes("Sitemap:") && r.body.includes("/sitemap.xml"),
      "robots.txt has Content-Signal directive": (r) =>
        r.body.includes("Content-Signal:") &&
        r.body.includes("ai-train=no") &&
        r.body.includes("search=yes") &&
        r.body.includes("ai-input=yes"),
    });
    errorRate.add(!ok);
  });
}

function testContentType() {
  group("Content-Type headers", () => {
    const endpoints = [
      "/health",
      "/v1/cards?limit=1",
      "/v1/sets",
      "/v1/keywords",
      "/v1/abilities",
    ];

    for (const ep of endpoints) {
      const res = http.get(`${BASE_URL}${ep}`);
      check(res, {
        [`${ep} returns application/json`]: (r) =>
          r.headers["Content-Type"].includes("application/json"),
      });
    }
  });
}

// -- Main --

export default function () {
  testHealth();
  testIndex();
  testOpenAPISpec();
  testListCards();
  testSearchCards();
  testPagination();
  testGetCard();
  testGetCardNotFound();
  testCardLegality();
  testListSets();
  testSearchSets();
  testGetSet();
  testListKeywords();
  testGetKeyword();
  testListAbilities();
  testMCPServerCard();
  testAPICatalog();
  testSitemapXml();
  testRobotsTxt();
  testContentType();

  sleep(1);
}
