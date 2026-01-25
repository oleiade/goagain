# k6 Load Tests

Performance and functional tests for the goagain API using k6 with the k6-testing assertions library.

## Prerequisites

Install k6: https://grafana.com/docs/k6/latest/set-up/install-k6/

```bash
# macOS
brew install k6

# Linux (Debian/Ubuntu)
sudo gpg -k
sudo gpg --no-default-keyring --keyring /usr/share/keyrings/k6-archive-keyring.gpg --keyserver hkp://keyserver.ubuntu.com:80 --recv-keys C5AD17C747E3415A3642D57D77C6C491D6AC1D69
echo "deb [signed-by=/usr/share/keyrings/k6-archive-keyring.gpg] https://dl.k6.io/deb stable main" | sudo tee /etc/apt/sources.list.d/k6.list
sudo apt-get update
sudo apt-get install k6
```

## Key Features

- **Fail-fast on server unavailable**: Tests use the k6-testing assertions library which immediately fails if the API server is not running (via the `setup()` function)
- **Proper assertions**: Uses `expect()` from k6-testing instead of just `check()`, ensuring tests fail on assertion errors
- **Comprehensive API coverage**: Tests all endpoints including cards, sets, keywords, CORS, and rate limiting

## Running Tests

### Start the API server

```bash
# Default configuration
go run ./cmd/api

# With custom settings for rate limit testing
RATE_LIMIT_RPS=50 go run ./cmd/api
```

### Run the main test suite

```bash
k6 run tests/k6/api.js
```

If the server is not running, you'll see an immediate failure:

```
Error: API server must be running and healthy
Expected: 200
Received: 0
```

### Run with custom API URL

```bash
k6 run -e API_URL=http://api.goagain.dev tests/k6/api.js
```

### Run trusted proxy tests

```bash
# Start server with trusted proxies configured
TRUSTED_PROXIES=127.0.0.1/32 RATE_LIMIT_RPS=10 go run ./cmd/api

# Run the test
k6 run tests/k6/trusted-proxies.js
```

## Test Files

### api.js

The main test suite with two scenarios:

| Scenario | Description |
|----------|-------------|
| `functional` | Single-VU tests for all API endpoints |
| `rate_limit` | Burst traffic (150 RPS) to verify rate limiting works |

**What it tests:**
- Health check endpoint
- Cards endpoint (list, search, get by ID, legality)
- Sets endpoint (list, get by ID)
- Keywords endpoint
- Abilities endpoint
- CORS headers (preflight and actual requests)
- Error handling (404 responses)
- Landing page (HTML vs JSON based on Accept header)
- Rate limiting (429 responses)

### trusted-proxies.js

Tests X-Forwarded-For and X-Real-IP header handling when the API is behind a reverse proxy.

**What it tests:**
- Requests with/without X-Forwarded-For header
- Requests with X-Real-IP header
- Rate limiting applies per-forwarded-IP (not per-connection)

## Understanding the Output

### Assertions (from k6-testing library)

Unlike k6 `check()`, assertions from the k6-testing library:
- **Fail immediately** when expectations are not met
- **Halt test execution** on failure
- Provide clear error messages showing expected vs received values

Example failure output:
```
Error: CORS Allow-Origin header must be present
Expected: defined
Received: undefined
```

### Rate Limiting

The rate limit test sends 150 requests/second against a default 100 RPS limit. You should see:
- Some requests succeed (200)
- Some requests hit the rate limit (429)
- 429 responses include `Retry-After` header

### CORS

The test verifies:
- Preflight (OPTIONS) requests return 204
- `Access-Control-Allow-Origin` header is present
- `Access-Control-Allow-Methods` header is present

## Example Output

```
          /\      |‾‾| /‾‾/   /‾‾/
     /\  /  \     |  |/  /   /  /
    /  \/    \    |     (   /   ‾‾\
   /          \   |  |\  \ |  (‾)  |
  / __________ \  |__| \__\ \_____/ .io

  execution: local
     script: tests/k6/api.js
     output: -

  scenarios: (100.00%) 2 scenarios, 21 max VUs, 10m30s max duration
           * functional: 1 iterations shared among 1 VUs (maxDuration: 10m0s)
           * rate_limit: 150.00 iterations/s for 5s (maxVUs: 20, startTime: 10s)

running (00m15.2s), 00/21 VUs, 752 complete and 0 interrupted iterations
functional ✓ [======================================] 1 VUs  00m00.8s/10m0s  1/1 shared iters
rate_limit ✓ [======================================] 20 VUs  5s  150 iters/s

     ✓ rate limit has retry-after header
     ✓ rate limit response is JSON
     ✓ non-rate-limited request succeeds

     checks.....................: 100.00% ✓ 1504     ✗ 0
     data_received..............: 2.1 MB  138 kB/s
     http_req_duration..........: avg=12.3ms min=1.2ms med=8.4ms max=89ms p(95)=32ms
```

## Troubleshooting

### "API server must be running"
Start the API server before running tests:
```bash
go run ./cmd/api
```

### No rate limit hits
- Check that `RATE_LIMIT_RPS` is set to a reasonable value (default is 100)
- The test sends 150 RPS, so with default settings you should see some 429s

### CORS test fails
- Ensure the API is returning CORS headers
- Check `CORS_ORIGINS` environment variable
