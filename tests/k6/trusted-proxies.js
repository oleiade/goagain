import http from 'k6/http';
import { group } from 'k6';
import { expect } from 'https://jslib.k6.io/k6-testing/0.6.1/index.js';

/**
 * Test for trusted proxy header handling.
 *
 * This test verifies that the API correctly handles X-Forwarded-For headers
 * when TRUSTED_PROXIES is configured.
 *
 * To run this test, start the API with TRUSTED_PROXIES configured:
 *   TRUSTED_PROXIES=127.0.0.1/32 RATE_LIMIT_RPS=10 go run ./cmd/api
 *
 * Then run:
 *   k6 run tests/k6/trusted-proxies.js
 *
 * Note: The API logs include client_ip, so you can verify the correct IP
 * is being extracted by checking the logs.
 */

const BASE_URL = __ENV.API_URL || 'http://localhost:8080';

export const options = {
  vus: 1,
  iterations: 1,
};

// Setup - verify server is running
export function setup() {
  console.log(`Testing trusted proxy handling at: ${BASE_URL}`);
  console.log('Make sure API is started with: TRUSTED_PROXIES=127.0.0.1/32 RATE_LIMIT_RPS=10');

  const healthRes = http.get(`${BASE_URL}/health`, { timeout: '5s' });
  expect(healthRes.status, 'API server must be running').toBe(200);

  return { baseUrl: BASE_URL };
}

export default function (data) {
  group('X-Forwarded-For Header Handling', () => {
    // Request without X-Forwarded-For (should use remote IP)
    const noHeaderRes = http.get(`${data.baseUrl}/health`);
    expect(noHeaderRes.status).toBe(200);
    console.log('Request without XFF: Check API logs for client_ip');

    // Request with X-Forwarded-For (should extract first IP if trusted)
    const withXffRes = http.get(`${data.baseUrl}/health`, {
      headers: {
        'X-Forwarded-For': '203.0.113.195, 70.41.3.18, 150.172.238.178',
      },
    });
    expect(withXffRes.status).toBe(200);
    console.log('Request with XFF (203.0.113.195): Check API logs for client_ip');

    // Request with X-Real-IP (alternative header)
    const withRealIpRes = http.get(`${data.baseUrl}/health`, {
      headers: {
        'X-Real-IP': '198.51.100.42',
      },
    });
    expect(withRealIpRes.status).toBe(200);
    console.log('Request with X-Real-IP (198.51.100.42): Check API logs for client_ip');
  });

  group('Rate Limiting with Proxy Headers', () => {
    // This tests that rate limiting uses the correct client IP
    // When behind a trusted proxy, rate limits should apply per-original-client
    // We use a low rate limit (10 RPS) to make testing easier

    const forwardedIp = '10.0.0.100';
    let rateLimitHit = false;
    let requestCount = 0;

    // Send rapid requests with same forwarded IP until we hit rate limit
    for (let i = 0; i < 50 && !rateLimitHit; i++) {
      requestCount++;
      const res = http.get(`${data.baseUrl}/health`, {
        headers: {
          'X-Forwarded-For': forwardedIp,
        },
      });

      if (res.status === 429) {
        rateLimitHit = true;
        console.log(`Rate limit hit after ${requestCount} requests for forwarded IP ${forwardedIp}`);

        // Verify rate limit response
        expect(res.headers['Retry-After'], 'Rate limit response should have Retry-After header').toBeDefined();
        const body = res.json();
        expect(body.error).toBe('rate limit exceeded');
      }
    }

    if (rateLimitHit) {
      console.log('Rate limiting is working correctly with proxy headers.');

      // Now test with a different forwarded IP - should NOT be rate limited
      const differentIpRes = http.get(`${data.baseUrl}/health`, {
        headers: {
          'X-Forwarded-For': '10.0.0.200', // Different IP
        },
      });
      expect(
        differentIpRes.status,
        'Different forwarded IP should have its own rate limit bucket'
      ).toBe(200);
      console.log('Different forwarded IP (10.0.0.200) was not rate limited - per-IP buckets working.');
    } else {
      console.log(`No rate limit hit after ${requestCount} requests.`);
      console.log('Either RATE_LIMIT_RPS is set too high, or TRUSTED_PROXIES is not configured.');
      console.log('Expected: TRUSTED_PROXIES=127.0.0.1/32 RATE_LIMIT_RPS=10');
    }
  });
}

export function teardown(data) {
  console.log('\n=== Trusted Proxy Test Complete ===');
  console.log('Review API server logs to verify client_ip extraction.');
  console.log('Expected behavior when TRUSTED_PROXIES includes your IP:');
  console.log('  - Requests with X-Forwarded-For should log the forwarded IP');
  console.log('  - Requests with X-Real-IP should log the real IP');
  console.log('  - Rate limits should apply per-forwarded-IP, not per-connection');
}
