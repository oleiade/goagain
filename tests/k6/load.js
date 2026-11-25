// Load test: sustained, moderate concurrency against the full API surface.
//
// Reuses the conformance checks from api.js so every endpoint is exercised
// with the same assertions as the smoke test, under realistic load.
//
// Run with: k6 run tests/k6/load.js
// The API should be started with a high RATE_LIMIT_RPS, since all VUs share
// a single source IP and would otherwise be throttled by the per-IP limiter.
export { default } from "./api.js";

export const options = {
  scenarios: {
    load: {
      executor: "ramping-vus",
      startVUs: 0,
      stages: [
        { duration: "30s", target: 10 },
        { duration: "1m", target: 10 },
        { duration: "15s", target: 0 },
      ],
      gracefulRampDown: "10s",
      tags: { scenario: "load" },
    },
  },
  thresholds: {
    http_req_failed: ["rate<0.01"],
    http_req_duration: ["p(95)<2000", "p(99)<5000"],
    api_errors: ["rate<0.01"],
    checks: ["rate>0.99"],
    card_search_duration: ["p(95)<2000"],
    card_get_duration: ["p(95)<1000"],
  },
};
