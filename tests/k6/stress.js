import http from 'k6/http';
import { check, sleep } from 'k6';
import { Counter, Trend } from 'k6/metrics';
import { expect } from 'https://jslib.k6.io/k6-testing/0.6.1/index.js';

/**
 * Stress Tests for goagain API
 *
 * Purpose: Test system limits, rate limiting behavior, and graceful degradation
 * Run: Nightly schedule and manual trigger only
 * Duration: ~2m
 *
 * Scenarios:
 *   1. stress: Ramping VUs from 50 to 100 to find breaking point
 *   2. rate_limit: Validate rate limiting works correctly at 150 RPS
 *
 * Usage:
 *   k6 run tests/k6/stress.js
 *   API_URL=http://localhost:8080 k6 run tests/k6/stress.js
 */

// Configuration
const BASE_URL = __ENV.API_URL || 'http://localhost:8080';

// Custom metrics
const rateLimitHits = new Counter('rate_limit_hits');
const stressLatency = new Trend('stress_latency', true);

export const options = {
  scenarios: {
    // Stress test - ramp up to find limits
    stress: {
      executor: 'ramping-vus',
      stages: [
        { duration: '15s', target: 50 }, // Ramp to 50 VUs
        { duration: '30s', target: 50 }, // Hold at 50
        { duration: '15s', target: 100 }, // Ramp to 100
        { duration: '30s', target: 100 }, // Hold at 100
        { duration: '15s', target: 0 }, // Ramp down
      ],
      exec: 'stressTest',
    },
    // Rate limit validation - constant high arrival rate
    rate_limit: {
      executor: 'constant-arrival-rate',
      rate: 150, // 150 RPS (above default 100 RPS limit)
      timeUnit: '1s',
      duration: '15s',
      preAllocatedVUs: 30,
      maxVUs: 50,
      exec: 'rateLimitTest',
      // Start after stress scenario completes (15+30+15+30+15 = 105s + 5s buffer)
      startTime: '110s',
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.10'], // Allow up to 10% failures under stress
    http_req_duration: ['p(95)<1000'], // Allow degradation under load
    rate_limit_hits: ['count>10'], // Verify rate limiting is working
  },
};

// Setup - verify server and cache data
export function setup() {
  console.log(`Stress testing API at: ${BASE_URL}`);

  const healthRes = http.get(`${BASE_URL}/health`, { timeout: '5s' });
  expect(healthRes.status, 'API server must be running').toBe(200);

  const healthData = healthRes.json();
  console.log(
    `API is healthy. Loaded ${healthData.stats.cards} cards, ${healthData.stats.sets} sets`
  );

  // Fetch some card IDs for lookups
  let cardIds = [];
  const cardsRes = http.get(`${BASE_URL}/v1/cards?limit=100`);
  if (cardsRes.status === 200) {
    const cardsData = cardsRes.json();
    cardIds = cardsData.data.map((c) => c.unique_id);
  }

  // Fetch set IDs
  let setIds = [];
  const setsRes = http.get(`${BASE_URL}/v1/sets`);
  if (setsRes.status === 200) {
    const setsData = setsRes.json();
    setIds = setsData.map((s) => s.id);
  }

  return {
    baseUrl: BASE_URL,
    stats: healthData.stats,
    cardIds,
    setIds,
  };
}

// Helper to select random item from array
function randomItem(arr) {
  return arr[Math.floor(Math.random() * arr.length)];
}

// Stress test function - mixed workload at high concurrency
export function stressTest(data) {
  const endpoints = [
    // Health check (fast, good for baseline)
    () => http.get(`${data.baseUrl}/health`),

    // Card listings with pagination
    () => {
      const limit = randomItem([10, 20, 50]);
      const offset = Math.floor(Math.random() * 200);
      return http.get(`${data.baseUrl}/v1/cards?limit=${limit}&offset=${offset}`);
    },

    // Card searches
    () => {
      const searchTerms = ['Strike', 'Blade', 'Ninja', 'Warrior', 'Attack', 'Defense'];
      return http.get(`${data.baseUrl}/v1/cards?name=${randomItem(searchTerms)}&limit=20`);
    },

    // Single card lookups
    () => {
      if (data.cardIds.length === 0) return http.get(`${data.baseUrl}/health`);
      return http.get(`${data.baseUrl}/v1/cards/${randomItem(data.cardIds)}`);
    },

    // Card legality
    () => {
      if (data.cardIds.length === 0) return http.get(`${data.baseUrl}/health`);
      return http.get(`${data.baseUrl}/v1/cards/${randomItem(data.cardIds)}/legality`);
    },

    // Sets list
    () => http.get(`${data.baseUrl}/v1/sets`),

    // Set details
    () => {
      if (data.setIds.length === 0) return http.get(`${data.baseUrl}/v1/sets`);
      return http.get(`${data.baseUrl}/v1/sets/${randomItem(data.setIds)}`);
    },

    // Keywords
    () => http.get(`${data.baseUrl}/v1/keywords`),

    // Abilities
    () => http.get(`${data.baseUrl}/v1/abilities`),
  ];

  const endpoint = randomItem(endpoints);
  const res = endpoint();

  stressLatency.add(res.timings.duration);

  // Track rate limits even in stress test
  if (res.status === 429) {
    rateLimitHits.add(1);
  }

  check(res, {
    'stress: response received': (r) => r.status !== 0,
    'stress: not server error': (r) => r.status < 500,
  });

  // Very short sleep to maximize throughput
  sleep(0.01 + Math.random() * 0.02);
}

// Rate limit test function - validates rate limiting behavior
export function rateLimitTest(data) {
  const res = http.get(`${data.baseUrl}/health`);

  if (res.status === 429) {
    rateLimitHits.add(1);

    // Verify rate limit response format
    check(res, {
      'rate limit: has Retry-After header': (r) => r.headers['Retry-After'] !== undefined,
      'rate limit: response is JSON': (r) => {
        try {
          const body = JSON.parse(r.body);
          return body.error === 'rate limit exceeded';
        } catch {
          return false;
        }
      },
    });
  } else {
    check(res, {
      'rate limit: non-limited request succeeds': (r) => r.status === 200,
    });
  }
}

// Default function - runs stress test
export default function (data) {
  stressTest(data);
}

// Teardown
export function teardown(data) {
  console.log('\n=== Stress Test Summary ===');
  console.log(`API URL: ${data.baseUrl}`);
  console.log(`Test data: ${data.cardIds.length} cards, ${data.setIds.length} sets`);
  console.log('Check rate_limit_hits metric to verify rate limiting worked.');
}
