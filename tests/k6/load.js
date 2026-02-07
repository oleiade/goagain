import http from 'k6/http';
import { check, sleep } from 'k6';
import { Trend } from 'k6/metrics';
import { expect } from 'https://jslib.k6.io/k6-testing/0.6.1/index.js';

/**
 * Load Tests for goagain API
 *
 * Purpose: Validate performance baselines and catch latency regressions
 * Run: On push to main only (after smoke tests pass)
 * Duration: ~45s
 *
 * Traffic distribution:
 *   10% health checks
 *   30% card listings with pagination
 *   30% card searches with various filters
 *   20% single card lookups
 *   10% other endpoints (sets, keywords, abilities)
 *
 * Usage:
 *   k6 run tests/k6/load.js
 *   API_URL=http://localhost:8080 k6 run tests/k6/load.js
 */

// Configuration
const BASE_URL = __ENV.API_URL || 'http://localhost:8080';

// Custom metrics for per-endpoint latency tracking
const healthLatency = new Trend('health_latency', true);
const cardsListLatency = new Trend('cards_list_latency', true);
const cardsSearchLatency = new Trend('cards_search_latency', true);
const cardGetLatency = new Trend('card_get_latency', true);
const setsLatency = new Trend('sets_latency', true);
const keywordsLatency = new Trend('keywords_latency', true);
const abilitiesLatency = new Trend('abilities_latency', true);

export const options = {
  scenarios: {
    mixed_load: {
      executor: 'ramping-vus',
      stages: [
        { duration: '10s', target: 10 }, // Ramp up
        { duration: '25s', target: 10 }, // Steady state
        { duration: '10s', target: 0 }, // Ramp down
      ],
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'], // Less than 1% errors
    http_req_duration: ['p(95)<500', 'p(99)<1000'], // Overall latency
    health_latency: ['p(95)<100'], // Health checks fast
    cards_list_latency: ['p(95)<300'], // Card listings
    cards_search_latency: ['p(95)<400'], // Card searches
    card_get_latency: ['p(95)<150'], // Single card lookups
    sets_latency: ['p(95)<200'], // Sets endpoints
    keywords_latency: ['p(95)<200'], // Keywords endpoints
    abilities_latency: ['p(95)<200'], // Abilities endpoints
  },
};

// Setup - verify server and cache some IDs for lookups
export function setup() {
  console.log(`Load testing API at: ${BASE_URL}`);

  const healthRes = http.get(`${BASE_URL}/health`, { timeout: '5s' });
  expect(healthRes.status, 'API server must be running').toBe(200);

  const healthData = healthRes.json();
  console.log(
    `API is healthy. Loaded ${healthData.stats.cards} cards, ${healthData.stats.sets} sets`
  );

  // Fetch some card IDs for single-card lookups
  let cardIds = [];
  const cardsRes = http.get(`${BASE_URL}/v1/cards?limit=50`);
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

  // Fetch keyword names
  let keywordNames = [];
  const kwRes = http.get(`${BASE_URL}/v1/keywords`);
  if (kwRes.status === 200) {
    const kwData = kwRes.json();
    keywordNames = kwData.map((k) => k.name);
  }

  return {
    baseUrl: BASE_URL,
    stats: healthData.stats,
    cardIds,
    setIds,
    keywordNames,
  };
}

// Helper to select random item from array
function randomItem(arr) {
  return arr[Math.floor(Math.random() * arr.length)];
}

// Traffic distribution weights
const endpoints = [
  { weight: 10, fn: healthCheck },
  { weight: 30, fn: cardsList },
  { weight: 30, fn: cardsSearch },
  { weight: 20, fn: cardGet },
  { weight: 5, fn: setsEndpoint },
  { weight: 3, fn: keywordsEndpoint },
  { weight: 2, fn: abilitiesEndpoint },
];

// Calculate cumulative weights for selection
const totalWeight = endpoints.reduce((sum, e) => sum + e.weight, 0);
const cumulativeWeights = [];
let cumulative = 0;
for (const e of endpoints) {
  cumulative += e.weight;
  cumulativeWeights.push(cumulative);
}

// Select endpoint based on weight distribution
function selectEndpoint() {
  const rand = Math.random() * totalWeight;
  for (let i = 0; i < cumulativeWeights.length; i++) {
    if (rand < cumulativeWeights[i]) {
      return endpoints[i].fn;
    }
  }
  return endpoints[0].fn;
}

// Endpoint functions
function healthCheck(data) {
  const res = http.get(`${data.baseUrl}/health`);
  healthLatency.add(res.timings.duration);
  check(res, { 'health: status 200': (r) => r.status === 200 });
}

function cardsList(data) {
  const limit = randomItem([10, 20, 50]);
  const offset = Math.floor(Math.random() * 100);
  const res = http.get(`${data.baseUrl}/v1/cards?limit=${limit}&offset=${offset}`);
  cardsListLatency.add(res.timings.duration);
  check(res, {
    'cards list: status 200': (r) => r.status === 200,
    'cards list: has data': (r) => {
      try {
        return r.json().data !== undefined;
      } catch {
        return false;
      }
    },
  });
}

function cardsSearch(data) {
  const searchTypes = [
    () => `name=${encodeURIComponent(randomItem(['Strike', 'Blade', 'Ninja', 'Warrior', 'Attack']))}`,
    () => `class=${encodeURIComponent(randomItem(['Ninja', 'Warrior', 'Wizard', 'Brute', 'Guardian']))}`,
    () => `pitch=${randomItem([1, 2, 3])}`,
    () =>
      `keyword=${encodeURIComponent(randomItem(data.keywordNames.length > 0 ? data.keywordNames : ['Go again']))}`,
    () => `type=${encodeURIComponent(randomItem(['Action', 'Attack', 'Equipment', 'Weapon']))}`,
  ];

  const searchParam = randomItem(searchTypes)();
  const res = http.get(`${data.baseUrl}/v1/cards?${searchParam}&limit=20`);
  cardsSearchLatency.add(res.timings.duration);
  check(res, {
    'cards search: status 200': (r) => r.status === 200,
  });
}

function cardGet(data) {
  if (data.cardIds.length === 0) return;
  const cardId = randomItem(data.cardIds);
  const res = http.get(`${data.baseUrl}/v1/cards/${cardId}`);
  cardGetLatency.add(res.timings.duration);
  check(res, {
    'card get: status 200': (r) => r.status === 200,
    'card get: has unique_id': (r) => {
      try {
        return r.json().unique_id !== undefined;
      } catch {
        return false;
      }
    },
  });
}

function setsEndpoint(data) {
  // 50% list, 50% get specific
  if (Math.random() < 0.5 || data.setIds.length === 0) {
    const res = http.get(`${data.baseUrl}/v1/sets`);
    setsLatency.add(res.timings.duration);
    check(res, { 'sets list: status 200': (r) => r.status === 200 });
  } else {
    const setId = randomItem(data.setIds);
    const res = http.get(`${data.baseUrl}/v1/sets/${setId}`);
    setsLatency.add(res.timings.duration);
    check(res, { 'set get: status 200': (r) => r.status === 200 });
  }
}

function keywordsEndpoint(data) {
  // 50% list, 50% get specific
  if (Math.random() < 0.5 || data.keywordNames.length === 0) {
    const res = http.get(`${data.baseUrl}/v1/keywords`);
    keywordsLatency.add(res.timings.duration);
    check(res, { 'keywords list: status 200': (r) => r.status === 200 });
  } else {
    const kwName = randomItem(data.keywordNames);
    const res = http.get(`${data.baseUrl}/v1/keywords/${encodeURIComponent(kwName)}`);
    keywordsLatency.add(res.timings.duration);
    check(res, { 'keyword get: status 200': (r) => r.status === 200 });
  }
}

function abilitiesEndpoint(data) {
  const res = http.get(`${data.baseUrl}/v1/abilities`);
  abilitiesLatency.add(res.timings.duration);
  check(res, { 'abilities: status 200': (r) => r.status === 200 });
}

// Main test function - called repeatedly by each VU
export default function (data) {
  const endpoint = selectEndpoint();
  endpoint(data);

  // Small sleep to simulate realistic user behavior
  sleep(0.1 + Math.random() * 0.2);
}

// Teardown
export function teardown(data) {
  console.log('\n=== Load Test Summary ===');
  console.log(`API URL: ${data.baseUrl}`);
  console.log(`Test data: ${data.cardIds.length} cards, ${data.setIds.length} sets`);
}
