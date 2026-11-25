// Stress test: ramps concurrency well past the load-test level to find where
// latency and error rates start to degrade.
//
// Reuses the conformance checks from api.js. Thresholds are looser than the
// load test: the goal is to catch collapse (errors, runaway latency), not to
// enforce steady-state SLOs.
//
// Run with: k6 run tests/k6/stress.js
// The API should be started with a high RATE_LIMIT_RPS, since all VUs share
// a single source IP and would otherwise be throttled by the per-IP limiter.
export { default } from "./api.js";

export const options = {
  scenarios: {
    stress: {
      executor: "ramping-vus",
      startVUs: 0,
      stages: [
        { duration: "1m", target: 50 },
        { duration: "2m", target: 50 },
        { duration: "1m", target: 100 },
        { duration: "2m", target: 100 },
        { duration: "30s", target: 0 },
      ],
      gracefulRampDown: "15s",
      tags: { scenario: "stress" },
    },
  },
  thresholds: {
    http_req_failed: ["rate<0.05"],
    http_req_duration: ["p(95)<5000"],
    api_errors: ["rate<0.05"],
    checks: ["rate>0.95"],
  },
};
