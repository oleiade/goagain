import http from "k6/http";
import { check, group, sleep } from "k6";
import { Rate, Trend } from "k6/metrics";

// Custom metrics
const errorRate = new Rate("mcp_errors");
const toolCallDuration = new Trend("mcp_tool_call_duration");

const BASE_URL = __ENV.MCP_BASE_URL || "http://localhost:8081";

export const options = {
  scenarios: {
    // Smoke test: quick conformance check (CI default)
    smoke: {
      executor: "shared-iterations",
      vus: 1,
      iterations: 1,
      tags: { scenario: "smoke" },
    },
    // Light load: sustained MCP traffic
    load: {
      executor: "ramping-vus",
      startVUs: 0,
      stages: [
        { duration: "10s", target: 3 },
        { duration: "30s", target: 3 },
        { duration: "10s", target: 0 },
      ],
      startTime: "5s",
      tags: { scenario: "load" },
    },
  },
  thresholds: {
    http_req_failed: ["rate<0.01"],
    http_req_duration: ["p(95)<3000", "p(99)<5000"],
    mcp_errors: ["rate<0.01"],
    checks: ["rate>0.99"],
    mcp_tool_call_duration: ["p(95)<3000"],
  },
};

// -- Helpers --

const jsonRPCHeaders = {
  "Content-Type": "application/json",
  Accept: "application/json, text/event-stream",
};

let requestId = 0;

function nextId() {
  return ++requestId;
}

function mcpRequest(method, params) {
  return JSON.stringify({
    jsonrpc: "2.0",
    id: nextId(),
    method: method,
    params: params || {},
  });
}

function mcpNotification(method, params) {
  return JSON.stringify({
    jsonrpc: "2.0",
    method: method,
    params: params || {},
  });
}

// Parse a JSON-RPC response from the MCP server.
// The StreamableHTTP transport may return:
//   - A direct JSON response (for non-streaming)
//   - An SSE stream with event: message lines
// We handle both cases.
function parseResponse(res) {
  const ct = res.headers["Content-Type"] || "";

  if (ct.includes("text/event-stream")) {
    // Parse SSE: look for "data: " lines containing JSON-RPC responses
    const lines = res.body.split("\n");
    for (const line of lines) {
      if (line.startsWith("data: ")) {
        try {
          return JSON.parse(line.substring(6));
        } catch {
          // skip malformed lines
        }
      }
    }
    return null;
  }

  // Direct JSON response
  try {
    return res.json();
  } catch {
    return null;
  }
}

// -- Test functions --

function testMCPHealth() {
  group("MCP Health", () => {
    const res = http.get(`${BASE_URL}/health`);
    const body = res.json();

    const ok = check(res, {
      "GET /health returns 200": (r) => r.status === 200,
      "health status is ok": () => body.status === "ok",
    });
    errorRate.add(!ok);
  });
}

function testMCPInitialize() {
  group("MCP Initialize", () => {
    const body = mcpRequest("initialize", {
      protocolVersion: "2025-03-26",
      capabilities: {},
      clientInfo: {
        name: "k6-test-client",
        version: "1.0.0",
      },
    });

    const res = http.post(`${BASE_URL}/mcp`, body, {
      headers: jsonRPCHeaders,
    });
    toolCallDuration.add(res.timings.duration);

    const rpc = parseResponse(res);

    const ok = check(res, {
      "initialize returns 2xx": (r) => r.status >= 200 && r.status < 300,
      "initialize returns valid JSON-RPC": () => rpc !== null,
      "initialize has result": () => rpc && rpc.result !== undefined,
      "result has serverInfo": () =>
        rpc && rpc.result && rpc.result.serverInfo !== undefined,
      "result has capabilities": () =>
        rpc && rpc.result && rpc.result.capabilities !== undefined,
    });
    errorRate.add(!ok);

    // Send initialized notification
    if (ok) {
      const sessionHeader = res.headers["Mcp-Session-Id"];
      const notifHeaders = { ...jsonRPCHeaders };
      if (sessionHeader) {
        notifHeaders["Mcp-Session-Id"] = sessionHeader;
      }

      const notif = mcpNotification("notifications/initialized");
      const notifRes = http.post(`${BASE_URL}/mcp`, notif, {
        headers: notifHeaders,
      });

      check(notifRes, {
        "initialized notification accepted": (r) =>
          r.status >= 200 && r.status < 300,
      });
    }
  });
}

// Establishes a session and returns the session ID header for subsequent calls.
function initSession() {
  const body = mcpRequest("initialize", {
    protocolVersion: "2025-03-26",
    capabilities: {},
    clientInfo: { name: "k6-test-client", version: "1.0.0" },
  });

  const res = http.post(`${BASE_URL}/mcp`, body, { headers: jsonRPCHeaders });
  const sessionId = res.headers["Mcp-Session-Id"];

  // Send initialized notification
  const notifHeaders = { ...jsonRPCHeaders };
  if (sessionId) {
    notifHeaders["Mcp-Session-Id"] = sessionId;
  }
  http.post(`${BASE_URL}/mcp`, mcpNotification("notifications/initialized"), {
    headers: notifHeaders,
  });

  return sessionId;
}

function callTool(sessionId, toolName, args) {
  const headers = { ...jsonRPCHeaders };
  if (sessionId) {
    headers["Mcp-Session-Id"] = sessionId;
  }

  const body = mcpRequest("tools/call", {
    name: toolName,
    arguments: args || {},
  });

  const res = http.post(`${BASE_URL}/mcp`, body, { headers });
  toolCallDuration.add(res.timings.duration);

  return { response: res, rpc: parseResponse(res) };
}

function testToolsList() {
  group("MCP Tools List", () => {
    const sessionId = initSession();
    const headers = { ...jsonRPCHeaders };
    if (sessionId) {
      headers["Mcp-Session-Id"] = sessionId;
    }

    const body = mcpRequest("tools/list", {});
    const res = http.post(`${BASE_URL}/mcp`, body, { headers });
    const rpc = parseResponse(res);

    const ok = check(res, {
      "tools/list returns 2xx": (r) => r.status >= 200 && r.status < 300,
      "tools/list has result": () => rpc && rpc.result !== undefined,
      "tools/list has tools array": () =>
        rpc && rpc.result && Array.isArray(rpc.result.tools),
    });
    errorRate.add(!ok);

    if (rpc && rpc.result && rpc.result.tools) {
      const toolNames = rpc.result.tools.map((t) => t.name);
      const expectedTools = [
        "search_cards",
        "get_card",
        "list_sets",
        "search_sets",
        "get_set",
        "search_card_text",
        "get_format_legality",
        "list_keywords",
        "get_keyword",
      ];

      for (const expected of expectedTools) {
        check(toolNames, {
          [`tools/list includes ${expected}`]: (names) =>
            names.includes(expected),
        });
      }
    }
  });
}

function testSearchCards() {
  group("MCP search_cards", () => {
    const sessionId = initSession();

    // Search by class
    const { response, rpc } = callTool(sessionId, "search_cards", {
      class: "Ninja",
      limit: 5,
    });

    const ok = check(response, {
      "search_cards returns 2xx": (r) => r.status >= 200 && r.status < 300,
      "search_cards has result": () => rpc && rpc.result !== undefined,
      "search_cards result has content": () =>
        rpc && rpc.result && Array.isArray(rpc.result.content),
    });
    errorRate.add(!ok);

    if (rpc && rpc.result && rpc.result.content) {
      const text = rpc.result.content
        .filter((c) => c.type === "text")
        .map((c) => c.text)
        .join("");

      check(text, {
        "search_cards content is non-empty": (t) => t.length > 0,
      });
    }
  });
}

function testGetCard() {
  group("MCP get_card", () => {
    const sessionId = initSession();

    // Fetch a known card (using name)
    const { response, rpc } = callTool(sessionId, "get_card", {
      id: "Enlightened Strike",
    });

    const ok = check(response, {
      "get_card returns 2xx": (r) => r.status >= 200 && r.status < 300,
      "get_card has result": () => rpc && rpc.result !== undefined,
      "get_card result has content": () =>
        rpc && rpc.result && Array.isArray(rpc.result.content),
    });
    errorRate.add(!ok);
  });
}

function testListSets() {
  group("MCP list_sets", () => {
    const sessionId = initSession();
    const { response, rpc } = callTool(sessionId, "list_sets", {});

    const ok = check(response, {
      "list_sets returns 2xx": (r) => r.status >= 200 && r.status < 300,
      "list_sets has result": () => rpc && rpc.result !== undefined,
    });
    errorRate.add(!ok);
  });
}

function testGetSet() {
  group("MCP get_set", () => {
    const sessionId = initSession();
    const { response, rpc } = callTool(sessionId, "get_set", {
      id: "WTR",
      include_cards: false,
    });

    const ok = check(response, {
      "get_set returns 2xx": (r) => r.status >= 200 && r.status < 300,
      "get_set has result": () => rpc && rpc.result !== undefined,
    });
    errorRate.add(!ok);
  });
}

function testSearchCardText() {
  group("MCP search_card_text", () => {
    const sessionId = initSession();
    const { response, rpc } = callTool(sessionId, "search_card_text", {
      query: "draw a card",
      limit: 5,
    });

    const ok = check(response, {
      "search_card_text returns 2xx": (r) =>
        r.status >= 200 && r.status < 300,
      "search_card_text has result": () => rpc && rpc.result !== undefined,
    });
    errorRate.add(!ok);
  });
}

function testGetFormatLegality() {
  group("MCP get_format_legality", () => {
    const sessionId = initSession();
    const { response, rpc } = callTool(sessionId, "get_format_legality", {
      id: "Enlightened Strike",
    });

    const ok = check(response, {
      "get_format_legality returns 2xx": (r) =>
        r.status >= 200 && r.status < 300,
      "get_format_legality has result": () =>
        rpc && rpc.result !== undefined,
    });
    errorRate.add(!ok);
  });
}

function testListKeywords() {
  group("MCP list_keywords", () => {
    const sessionId = initSession();
    const { response, rpc } = callTool(sessionId, "list_keywords", {});

    const ok = check(response, {
      "list_keywords returns 2xx": (r) => r.status >= 200 && r.status < 300,
      "list_keywords has result": () => rpc && rpc.result !== undefined,
    });
    errorRate.add(!ok);
  });
}

function testGetKeyword() {
  group("MCP get_keyword", () => {
    const sessionId = initSession();
    const { response, rpc } = callTool(sessionId, "get_keyword", {
      name: "Go again",
    });

    const ok = check(response, {
      "get_keyword returns 2xx": (r) => r.status >= 200 && r.status < 300,
      "get_keyword has result": () => rpc && rpc.result !== undefined,
    });
    errorRate.add(!ok);
  });
}

function testSearchSets() {
  group("MCP search_sets", () => {
    const sessionId = initSession();
    const { response, rpc } = callTool(sessionId, "search_sets", {
      q: "Welcome",
    });

    const ok = check(response, {
      "search_sets returns 2xx": (r) => r.status >= 200 && r.status < 300,
      "search_sets has result": () => rpc && rpc.result !== undefined,
    });
    errorRate.add(!ok);
  });
}

// -- Main --

export default function () {
  testMCPHealth();
  testMCPInitialize();
  testToolsList();
  testSearchCards();
  testGetCard();
  testListSets();
  testGetSet();
  testSearchCardText();
  testGetFormatLegality();
  testListKeywords();
  testGetKeyword();
  testSearchSets();

  sleep(1);
}
