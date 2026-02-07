import http from 'k6/http';
import { group } from 'k6';
import { expect } from 'https://jslib.k6.io/k6-testing/0.6.1/index.js';

/**
 * Smoke/Functional Tests for goagain API
 *
 * Purpose: Validate API contracts, functional correctness, and catch regressions
 * Run: On every PR and push to main
 * Duration: ~30s
 *
 * Usage:
 *   k6 run tests/k6/api.js
 *   API_URL=http://localhost:8080 k6 run tests/k6/api.js
 */

// Configuration
const BASE_URL = __ENV.API_URL || 'http://localhost:8080';

export const options = {
  scenarios: {
    functional: {
      executor: 'shared-iterations',
      vus: 1,
      iterations: 1,
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.001'], // Near-zero errors allowed
    http_req_duration: ['p(95)<500'], // All requests under 500ms
    checks: ['rate==1.0'], // All checks must pass
  },
};

// Setup function - runs once before all tests
// Verifies the API server is running and accessible
export function setup() {
  console.log(`Testing API at: ${BASE_URL}`);

  const healthRes = http.get(`${BASE_URL}/health`, { timeout: '5s' });

  // Use assertion - this will fail the entire test if server is not running
  expect(healthRes.status, 'API server must be running and healthy').toBe(200);

  const healthData = healthRes.json();
  expect(healthData.status, 'Health check must return ok status').toBe('ok');
  expect(healthData.stats, 'Health check must return stats').toBeDefined();
  expect(healthData.stats.cards, 'Cards must be loaded').toBeGreaterThan(0);

  console.log(
    `API is healthy. Loaded ${healthData.stats.cards} cards, ${healthData.stats.sets} sets`
  );

  return { baseUrl: BASE_URL, stats: healthData.stats };
}

// Functional tests for API endpoints
export function functionalTests(data) {
  const BASE = data.baseUrl;

  group('Health Check', () => {
    const res = http.get(`${BASE}/health`);
    expect(res.status).toBe(200);

    const body = res.json();
    expect(body.status).toBe('ok');
    expect(body.stats).toBeDefined();
    expect(body.stats.cards).toBeGreaterThan(0);
  });

  group('Cards Endpoint', () => {
    // List cards with limit
    const listRes = http.get(`${BASE}/v1/cards?limit=10`);
    expect(listRes.status).toBe(200);

    const listBody = listRes.json();
    expect(listBody.data).toBeDefined();
    expect(listBody.data.length).toBeLessThanOrEqual(10);
    expect(listBody.total).toBeGreaterThan(0);

    // Search cards by name
    const searchRes = http.get(`${BASE}/v1/cards?name=Strike&limit=5`);
    expect(searchRes.status).toBe(200);

    const searchBody = searchRes.json();
    expect(searchBody.data).toBeDefined();
    expect(searchBody.data.length).toBeGreaterThan(0);

    // Filter by class
    const classRes = http.get(`${BASE}/v1/cards?class=Ninja&limit=5`);
    expect(classRes.status).toBe(200);

    const classBody = classRes.json();
    expect(classBody.data).toBeDefined();

    // Filter by pitch
    const pitchRes = http.get(`${BASE}/v1/cards?pitch=3&limit=5`);
    expect(pitchRes.status).toBe(200);

    const pitchBody = pitchRes.json();
    expect(pitchBody.data).toBeDefined();

    // Filter by keyword
    const keywordRes = http.get(`${BASE}/v1/cards?keyword=Go%20again&limit=5`);
    expect(keywordRes.status).toBe(200);

    const keywordBody = keywordRes.json();
    expect(keywordBody.data).toBeDefined();

    // Get specific card (using first result from search)
    if (searchBody.data && searchBody.data.length > 0) {
      const cardId = searchBody.data[0].unique_id;
      const cardRes = http.get(`${BASE}/v1/cards/${cardId}`);
      expect(cardRes.status).toBe(200);

      const card = cardRes.json();
      expect(card.unique_id).toBe(cardId);
      expect(card.name).toBeDefined();
    }

    // Get card legality
    if (searchBody.data && searchBody.data.length > 0) {
      const cardId = searchBody.data[0].unique_id;
      const legalityRes = http.get(`${BASE}/v1/cards/${cardId}/legality`);
      expect(legalityRes.status).toBe(200);

      const legality = legalityRes.json();
      expect(legality).toBeDefined();
      // Should be an array of format legalities
      expect(Array.isArray(legality)).toBe(true);
    }
  });

  group('Sets Endpoint', () => {
    const res = http.get(`${BASE}/v1/sets`);
    expect(res.status).toBe(200);

    const body = res.json();
    expect(Array.isArray(body)).toBe(true);
    expect(body.length).toBeGreaterThan(0);

    // Get specific set with cards
    if (body.length > 0) {
      const setRes = http.get(`${BASE}/v1/sets/${body[0].id}`);
      expect(setRes.status).toBe(200);

      const setBody = setRes.json();
      expect(setBody.id).toBe(body[0].id);
      expect(setBody.cards).toBeDefined();
      expect(Array.isArray(setBody.cards)).toBe(true);
    }
  });

  group('Keywords Endpoint', () => {
    const res = http.get(`${BASE}/v1/keywords`);
    expect(res.status).toBe(200);

    const body = res.json();
    expect(Array.isArray(body)).toBe(true);
    expect(body.length).toBeGreaterThan(0);

    // Get specific keyword
    if (body.length > 0) {
      const kwRes = http.get(`${BASE}/v1/keywords/${encodeURIComponent(body[0].name)}`);
      expect(kwRes.status).toBe(200);

      const kw = kwRes.json();
      expect(kw.name).toBeDefined();
    }
  });

  group('Abilities Endpoint', () => {
    const res = http.get(`${BASE}/v1/abilities`);
    expect(res.status).toBe(200);

    const body = res.json();
    expect(Array.isArray(body)).toBe(true);
  });

  group('CORS Headers', () => {
    // Test preflight request
    const preflightRes = http.options(`${BASE}/v1/cards`, null, {
      headers: {
        Origin: 'https://example.com',
        'Access-Control-Request-Method': 'GET',
      },
    });

    // Preflight should return 204 No Content
    expect(preflightRes.status).toBe(204);

    // Check CORS headers
    const allowOrigin = preflightRes.headers['Access-Control-Allow-Origin'];
    const allowMethods = preflightRes.headers['Access-Control-Allow-Methods'];

    expect(allowOrigin, 'CORS Allow-Origin header must be present').toBeDefined();
    expect(allowMethods, 'CORS Allow-Methods header must be present').toBeDefined();

    // Test actual request with Origin header
    const res = http.get(`${BASE}/v1/cards?limit=1`, {
      headers: {
        Origin: 'https://example.com',
      },
    });
    expect(res.status).toBe(200);
    expect(res.headers['Access-Control-Allow-Origin']).toBeDefined();
  });

  group('Error Handling', () => {
    // 404 for non-existent card
    const notFoundRes = http.get(`${BASE}/v1/cards/non-existent-card-id-12345`);
    expect(notFoundRes.status).toBe(404);

    const errorBody = notFoundRes.json();
    expect(errorBody.error).toBeDefined();
  });

  group('Landing Page', () => {
    // Default request should return HTML landing page
    const htmlRes = http.get(`${BASE}/`);
    expect(htmlRes.status).toBe(200);
    expect(htmlRes.headers['Content-Type']).toContain('text/html');

    // Request with Accept: application/json should return JSON
    const jsonRes = http.get(`${BASE}/`, {
      headers: {
        Accept: 'application/json',
      },
    });
    expect(jsonRes.status).toBe(200);
    expect(jsonRes.headers['Content-Type']).toContain('application/json');

    const jsonBody = jsonRes.json();
    expect(jsonBody.name).toBeDefined();
    expect(jsonBody.endpoints).toBeDefined();
  });
}

// Default function - runs the functional tests
export default function (data) {
  functionalTests(data);
}

// Teardown - runs once after all tests
export function teardown(data) {
  console.log('\n=== Smoke Test Summary ===');
  console.log(`API URL: ${data.baseUrl}`);
  console.log(`Cards in database: ${data.stats.cards}`);
  console.log(`Sets in database: ${data.stats.sets}`);
}
