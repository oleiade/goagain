# goagain Dashboard Overhaul: Implementation Plan

Status: ready to implement.
Date: 2026-07-26.
Scope: rebuild the two Grafana Cloud dashboards for goagain (API and MCP), fix the
instrumentation gaps they depend on, and add the supporting synthetic check,
annotations, and alerts.

This document is self-contained. An implementer does not need any prior
conversation context. Every query in this plan was verified against live data on
2026-07-26 unless marked otherwise. Do not invent metric names, label names, or
label values: use exactly what is written here, and verify with the commands in
each phase before shipping.

---

## 1. Context and ground truth

### 1.1 The system

goagain is a Go REST API and MCP server for Flesh and Blood card game data.
Two binaries, both instrumented with OpenTelemetry (OTLP export through a local
Alloy agent on host `sakai` into Grafana Cloud):

- `goagain-api`: REST API, public at `https://api.goagain.dev`
- `goagain-mcp`: MCP server (streamable HTTP), route `/mcp`

Relevant code:

- `internal/observability/metrics.go`: all custom metric instruments
- `internal/observability/logging.go`: `LoggingMiddleware` (structured request logs)
- `internal/observability/otel.go`: `SetupOTelSDK` (tracer, meter, logger providers)
- `cmd/api/main.go`: wires `otelhttp.NewHandler(router, "goagain-api", ...)`
- `cmd/mcp/main.go`: wires `otelhttp.NewHandler(handler, "goagain-mcp", ...)` and
  `mcp.NewStreamableHTTPServer` (package `github.com/mark3labs/mcp-go/server`)
- `internal/mcp/tools.go`: `NewServer`, tool registration, tool metric recording

### 1.2 Grafana access

All Grafana interaction goes through the `gcx` CLI with the `theocrevon` context:

```
gcx --context theocrevon <command>
```

Instance: `https://theocrevon.grafana.net`.

Datasources (verified):

| Purpose    | Name                        | UID                  | Type       |
|------------|-----------------------------|----------------------|------------|
| Metrics    | grafanacloud-theocrevon-prom| `grafanacloud-prom`  | prometheus |
| Logs       | grafanacloud-theocrevon-logs| `grafanacloud-logs`  | loki       |
| Traces     | grafanacloud-theocrevon-traces| `grafanacloud-traces`| tempo    |

Dashboards to rebuild:

| Dashboard | UID | Title today |
|-----------|-----|-------------|
| API | `th8phsg` | GoAgain API - Health & Performance |
| MCP | `th2qhfr` | GoAgain MCP - Health & Performance |

Both live in folder `fdvgy3wngzbb4a` (named "Théo").

### 1.3 gcx mechanics (important quirks)

- `gcx` sometimes prints a hint line (`hint: use --json list ...`) before JSON
  output. When capturing JSON to a file, strip it:
  `gcx ... -o json | sed '/^hint:/d' > file.json`.
- Dashboards use the Kubernetes-style API, schema `dashboard.grafana.app/v2`.
  A dashboard manifest has `apiVersion`, `kind`, `metadata` (including
  `metadata.resourceVersion`), and `spec`. The v2 spec structure:
  - `spec.elements`: a map of `panel-N` keys to panel definitions. Each panel has
    `kind: "Panel"` and `spec` with `title`, `description`, `vizConfig`
    (visualization type, field config, options), and `data.spec.queries`.
  - `spec.layout`: `kind: "GridLayout"` with `spec.items`, each item referencing
    an element by name with `x`, `y`, `width`, `height` on a 24-column grid.
  - `spec.variables`: list of variable definitions.
  - `spec.annotations`: list of annotation query definitions.
  - `spec.timeSettings`: default time range, refresh.
- Update workflow (optimistic concurrency; re-fetch on conflict):

```
gcx --context theocrevon dashboards get th8phsg -o json | sed '/^hint:/d' > api.json
# edit api.json (keep metadata.resourceVersion intact, replace spec)
gcx --context theocrevon dashboards update th8phsg -f api.json
```

- Rollback: `gcx dashboards versions --help` (version history exists). Also keep
  the pre-change JSON files committed in the repo (see 1.6).
- Visual check after each update:
  `gcx --context theocrevon dashboards snapshot th8phsg` renders a PNG.
  Inspect it (Read tool on the PNG path) to confirm panels render and show data.
- Query verification commands:

```
gcx --context theocrevon metrics query --datasource grafanacloud-prom '<promql>'
gcx --context theocrevon logs query --datasource grafanacloud-logs '<logql>' --limit 5
```

When verifying a PromQL expression that uses `$__range`, `$__rate_interval`,
`$__interval`, or `$__auto`, substitute a concrete duration (`7d`, `5m`, `1h`)
for the variable. The dashboard keeps the variable form.

### 1.4 Verified telemetry inventory

Prometheus metrics (label set verified):

- `http_server_request_total{job, http_route, http_request_method, http_response_status_code, ...}`
  Both jobs `goagain-api` and `goagain-mcp`. `http_route` is bounded by an
  allowlist since v0.9.2; unknown paths collapse into the literal route
  `"/other"`. Health checks appear as `http_route="/health"` and dominate volume
  (about 2,900/day per service). Historical data before the allowlist fix
  contains high-cardinality scanner routes and a large burst of 500s; do not be
  surprised by that in ranges older than late July 2026.
- `http_server_request_duration_seconds_bucket/_sum/_count{job, http_route, ...}`
- `http_server_response_body_size_bytes_bucket/_sum/_count{job, ...}`
- `http_server_request_body_size_bytes_bucket/_sum/_count{job, ...}`
- `http_server_active_requests{job}`
- `goagain_data_cards`, `goagain_data_sets`, `goagain_data_abilities`,
  `goagain_data_keywords`, `goagain_data_index_entries` (gauges, label `job`,
  NO `_total` suffix, NO `service` label)
- `mcp_tool_invocations_total{job="goagain-mcp", tool_name, tool_status}`
  `tool_status` values: `success`, `error`. `tool_name` values seen:
  `get_card`, `search_cards`, `search_card_text`. SPARSE: series only exist
  after a tool has been invoked; there can be long gaps with no samples.
- `mcp_tool_duration_seconds_bucket/_sum/_count{job="goagain-mcp", tool_name, tool_status}` (sparse)
- `mcp_tool_result_count_bucket/_sum/_count{job="goagain-mcp", tool_name}` (sparse)
- `mcp_tool_active{job="goagain-mcp"}` (up-down counter; note the name, there is
  NO metric called `mcp_tool_in_flight`)
- `mcp_sessions_total`, `mcp_sessions_active`: instruments exist in code but are
  NEVER recorded today. Phase 1 fixes this. Until Phase 1 is deployed these
  series do not exist in Prometheus.
- `target_info{job, instance, service_version, ...}`: OTel resource info metric,
  always 1 while the exporter is alive. `service_version` currently `0.9.2`.
- Synthetic monitoring (Grafana Cloud Synthetic Monitoring app):
  `probe_success{job="goagain API", instance="https://api.goagain.dev", probe}`
  with probes `Montreal`, `Frankfurt`, `Singapore`, every 5 minutes. Also
  `probe_all_duration_seconds_*`, `probe_ssl_earliest_cert_expiry`,
  `sm_check_info`. There is NO check for the MCP endpoint yet (Phase 4).

Loki streams: `{service_name="goagain-api"}` and `{service_name="goagain-mcp"}`,
stream labels include `detected_level` (lowercase: `info`, `warn`, `error`) and
`level` (uppercase). Log lines are nested JSON; request logs have body
`"HTTP request completed"` and attributes reachable via LogQL `json` hints:

```
attributes.duration_ms, attributes.method, attributes.path, attributes.status,
attributes.client_ip, attributes.response_size, attributes.request_id
```

Plus top-level `traceid`. The Loki datasource already has derived fields that
link `traceid` to Tempo; log panels get trace links for free.

Tempo: traces exist for both services (`resource.service.name`). Currently
polluted by 0ms `HEAD /health` traces; Phase 1 removes those at the source.

### 1.5 Existing alerts (do not duplicate)

- Synthetic Monitoring app alerts: probe failures (5m/15m), HTTP latency avg,
  TLS expiry.
- Custom rule "goagain telemetry stopped reporting" in folder "Théo":
  `absent(target_info{job="goagain-api"}) or absent(target_info{job="goagain-mcp"})`.

Phase 4 adds 5xx-ratio and p95-latency rules; nothing else.

### 1.6 Version control and repo conventions

- Use `jj`, not raw git. Before ANY file edit: run `jj status`. If `@` is
  non-empty or described with unrelated work, run `jj new` first.
- Conventional commits via `jj describe -m "..."`. One logical change per commit.
- After every code change run, from the repo root:

```
go build -v ./...
go test -race ./...
go vet ./...
gofmt -l .            # must print nothing
golangci-lint run
gosec ./...
govulncheck ./...
```

- Dashboards as code: this plan creates `dashboards/` in the repo. Every phase
  that touches a dashboard commits the final manifest JSON there
  (`dashboards/api.json`, `dashboards/mcp.json`), so the repo always holds the
  deployed state and rollback is a `gcx dashboards update -f` away.

---

## 2. Design conventions for both dashboards

These rules apply to every panel built in Phases 2 and 3.

1. **Range-correct.** The owner checks these dashboards rarely, over multi-day
   windows. Every headline stat and every table must aggregate over the
   selected range (`$__range` with instant queries), never show only the
   current instant. Timeseries panels use `$__rate_interval` (Prometheus) or
   `$__auto` (Loki).
2. **Exclude health checks from every user-facing number.** Add
   `http_route!="/health"` to every `http_server_*` query unless the panel is
   explicitly about health checks.
3. **5xx is reliability, 4xx is client behavior.** Never sum them in one
   "errors" series.
4. **Sparse-metric hygiene (MCP tool metrics).** Wrap range aggregations that
   can be empty in `or vector(0)` for stats; for timeseries use bar style with
   `increase(...[$__interval])` so missing samples read as zero activity, not
   as broken lines.
5. **Units.** Latency: `s`. Percent stats: `percent` (0-100 values). Counts:
   `short`. Rates: `reqps`. Bytes: `bytes`. Days: `d`.
6. **Thresholds and color.** Stats carry thresholds so the answer is a color:
   - Availability / uptime / success rate: red < 99, yellow < 99.9, green >= 99.9
   - p95 latency: green < 0.1s, yellow 0.1-0.5s, red >= 0.5s
   - SSL days remaining: red < 14, yellow < 30, green >= 30
   - Status-class series color overrides: 2xx green, 3xx blue, 4xx orange,
     5xx red.
7. **Panel descriptions state the question.** Each panel description begins
   with the concrete question it answers, e.g. "Which routes were slowest over
   the selected period?". Titles stay short.
8. **Rows.** Panels are grouped in this order: At a glance, Reliability,
   Performance (API) / Tool usage (MCP), Usage / Sessions and transport, Data
   freshness, Drill-down. Drill-down is collapsed by default if the schema
   supports collapsed rows; if the v2 GridLayout in use has no row construct,
   approximate with a text/separator panel and place the group last.
9. **Defaults.** Both dashboards: default time range `now-7d` to `now`,
   auto-refresh OFF (empty string), timezone `browser`.
10. **Cross-links.** Each dashboard defines a dashboard link to the other one
    (spec-level links list): API -> `th2qhfr`, MCP -> `th8phsg`.
11. **No panel may ship unverified.** Before adding a query to a manifest, run
    it through `gcx metrics query` / `gcx logs query` with concrete durations
    substituted. A query returning empty is acceptable only when the emptiness
    is structural (e.g. zero 5xx in the window); note that in the panel
    description ("shows 0 when no errors occurred").

---

## 3. Phase 1: instrumentation fixes (code)

All changes in this phase are in the goagain repo. Ship as one or more
conventional commits (`fix(observability): ...` / `feat(observability): ...`).
The dashboard phases depend on 3.1; 3.2 and 3.3 are independent but should land
with it.

### 3.1 Record MCP session metrics

Problem: `internal/observability/metrics.go` defines `mcp.sessions.total` and
`mcp.sessions.active` and exposes `RecordSessionStart()` / `RecordSessionEnd()`
(around lines 374-385), but no code calls them, so the metrics never exist.

Fix: wire session lifecycle hooks from `mark3labs/mcp-go`.

- Location: `cmd/mcp/main.go` (server construction) and/or
  `internal/mcp/tools.go` `NewServer` (where `server.NewMCPServer` is called).
- Check the pinned mcp-go version in `go.mod` first, then check the actual API
  in the module cache. The expected mechanism: build a `server.Hooks` value,
  register `AddOnRegisterSession(func(ctx, session))` calling
  `metrics.RecordSessionStart()` and `AddOnUnregisterSession` calling
  `metrics.RecordSessionEnd()`, and pass it with the `server.WithHooks(hooks)`
  option to `NewMCPServer`. If the pinned version names these hooks
  differently, adapt to what the version actually exposes; do not upgrade the
  dependency just for this.
- `NewServer` in `internal/mcp/tools.go` already receives
  `*observability.Metrics`; thread it to wherever the hooks are constructed.
- Guard for nil metrics (stdio mode may construct the server without metrics;
  mirror the existing nil-handling pattern used for tool metrics in tools.go).
- Test: add a unit test that instantiates the server, simulates a session
  register/unregister through the hooks, and asserts the counters moved. Follow
  the style of existing tests in `internal/mcp/tools_test.go` and
  `internal/observability/metrics_test.go`.

Verification after deploy (not just after merge):

```
gcx --context theocrevon metrics query --datasource grafanacloud-prom 'mcp_sessions_active{job="goagain-mcp"}'
```

Non-empty result required before building the session panels of Phase 3.
If the service has not been redeployed yet, build those panels anyway (queries
are fixed by this plan) and note in the rollout checklist that they stay empty
until the next release.

### 3.2 Export Go runtime metrics

Problem: neither service exports Go runtime metrics (two current panels
apologize for this in their descriptions).

Fix: in `internal/observability/otel.go`, inside `SetupOTelSDK` after the meter
provider is registered as global, start the OTel runtime instrumentation:

- Add dependency `go.opentelemetry.io/contrib/instrumentation/runtime` (pick
  the version line matching the existing `go.opentelemetry.io/contrib/...`
  modules in `go.mod`).
- Call `runtime.Start(runtime.WithMinimumReadMemStatsInterval(15 * time.Second))`
  and handle the returned error like the surrounding code handles setup errors.

Do NOT hardcode panel queries for runtime metrics in this plan: the exported
names depend on the contrib version. After the first deploy, discover names:

```
gcx --context theocrevon metrics query --datasource grafanacloud-prom 'count by(__name__) ({job="goagain-api", __name__=~"go_.*|process_runtime_go.*|runtime_.*"})'
```

Then (optional, Phase 5) add a small "Runtime" row using whatever heap-bytes,
goroutine-count, and GC metrics actually exist.

### 3.3 Stop tracing and logging health checks

Problem: `HEAD /health` produces most trace and log volume (0ms traces in
Tempo, "HTTP request completed" spam in Loki) with zero information.

Fix (keep /health in METRICS; it is bounded and the dashboards exclude it):

1. Traces: in `cmd/api/main.go` and `cmd/mcp/main.go`, add an option to the
   existing `otelhttp.NewHandler(...)` calls:
   `otelhttp.WithFilter(func(r *http.Request) bool { return r.URL.Path != "/health" })`.
2. Logs: in `internal/observability/logging.go` `LoggingMiddleware`, skip the
   completion log when the request path is `/health` (early passthrough or a
   conditional around the log call; match existing code style). Keep logging
   non-200 responses on `/health` (a failing health check IS information):
   only skip when the final status is < 400.

Tests: extend the LoggingMiddleware tests (if present in the package) with a
case asserting no log line for a 200 `/health` request and a log line for a
500 one.

### 3.4 Phase 1 gate

Run the full check battery from section 1.6. All green before commit. Commit
message suggestion:

```
feat(observability): record MCP sessions, runtime metrics, drop health-check noise
```

(Split into three commits if the diffs are large; one logical change each.)

---

## 4. Phase 2: rebuild the API dashboard (uid `th8phsg`)

### 4.1 Procedure

1. Fetch and archive current state:

```
mkdir -p dashboards
gcx --context theocrevon dashboards get th8phsg -o json | sed '/^hint:/d' > dashboards/api.json
cp dashboards/api.json dashboards/api.pre-overhaul.json
```

Commit `dashboards/api.pre-overhaul.json` first (rollback artifact).

2. Rewrite `spec.elements`, `spec.layout`, `spec.timeSettings`, `spec.links`
   in `dashboards/api.json` per the tables below. Keep `metadata` (especially
   `resourceVersion`) untouched. Reuse the existing panel JSON as structural
   templates: copy an existing stat panel element, then change title,
   description, queries, unit, thresholds. This avoids inventing v2 schema
   fields. Existing panels of each viz kind (stat, timeseries, table, logs)
   exist in the current manifests; there is no existing heatmap panel, so model
   the heatmap panel on a timeseries element with `vizConfig.group` set to
   `heatmap` and the field/options shape from Grafana heatmap defaults (verify
   by pushing and rendering a snapshot; iterate until it renders).
3. `spec.timeSettings`: `from: "now-7d"`, `to: "now"`, `autoRefresh: ""`.
4. Push, render, inspect:

```
gcx --context theocrevon dashboards update th8phsg -f dashboards/api.json
gcx --context theocrevon dashboards snapshot th8phsg
```

5. Iterate until the snapshot shows every panel rendering with data (or
   documented-empty). Re-fetch before each retry if update reports a
   resourceVersion conflict.
6. Commit the final `dashboards/api.json`:
   `feat(observability): rebuild API dashboard around range-correct SLIs`.

### 4.2 Dashboard-level config

- Title: `goagain API`.
- Links: dashboard link titled `MCP dashboard` to uid `th2qhfr`.
- Annotations (deploy markers): Prometheus annotation query named `Deploys`,
  datasource `grafanacloud-prom`, expression:

```
count by(service_version) (target_info{job="goagain-api"}) unless count by(service_version) (target_info{job="goagain-api"} offset 10m)
```

  Text/title template: `Deploy {{service_version}}`. This emits a point when a
  new `service_version` value first appears. Verify by querying the expression
  over a range that contains the 0.9.2 rollout.

### 4.3 Panels

Grid is 24 columns wide. `y` values below assume each row starts where the
previous ended; keep the relative arrangement.

Row A: "At a glance" (y=0, six stats, width 4 each, height 5)

| # | Title | Type | Query (datasource grafanacloud-prom unless noted) | Unit / thresholds |
|---|-------|------|---------------------------------------------------|-------------------|
| A1 | Uptime (external) | stat | `avg(avg_over_time(probe_success{job="goagain API", instance="https://api.goagain.dev"}[$__range])) * 100` | percent; red<99, yellow<99.9, green>=99.9. Description: "Was the API reachable from outside? 3-region synthetic probe, 5 min interval." |
| A2 | Availability (5xx) | stat | `(1 - (sum(increase(http_server_request_total{job="goagain-api", http_response_status_code=~"5..", http_route!="/health"}[$__range])) or vector(0)) / sum(increase(http_server_request_total{job="goagain-api", http_route!="/health"}[$__range]))) * 100` | percent; same thresholds. "What share of real requests succeeded (non-5xx)?" |
| A3 | Requests | stat | `sum(increase(http_server_request_total{job="goagain-api", http_route!="/health"}[$__range]))` | short. "How many real (non-health) requests in the selected period?" |
| A4 | p95 latency | stat | `histogram_quantile(0.95, sum by(le) (increase(http_server_request_duration_seconds_bucket{job="goagain-api", http_route!="/health"}[$__range])))` | s; green<0.1, yellow<0.5, red>=0.5. "How slow was the slowest 5% of requests over the period?" |
| A5 | Telemetry | stat | `count(target_info{job="goagain-api"}) or vector(0)` | value mappings: 0 -> "STALE" red, >=1 -> "OK" green. "Is the service still shipping telemetry? If STALE, every other panel is lying." |
| A6 | SSL expiry | stat | `min((probe_ssl_earliest_cert_expiry{instance="https://api.goagain.dev"} - time()) / 86400)` | unit `d`; red<14, yellow<30, green>=30. "Days until the api.goagain.dev certificate expires." |

Row B: "Reliability" (height 8)

| # | Title | Type | Query | Notes |
|---|-------|------|-------|-------|
| B1 (w12) | Requests by status class | timeseries, stacked bars | `sum by(class) (label_replace(rate(http_server_request_total{job="goagain-api", http_route!="/health"}[$__rate_interval]), "class", "${1}xx", "http_response_status_code", "([0-9]).."))` legend `{{class}}` | Color overrides per convention 6. "What did responses look like over time, by class?" |
| B2 (w12) | 5xx by route | timeseries | `sum by(http_route) (rate(http_server_request_total{job="goagain-api", http_response_status_code=~"5.."}[$__rate_interval]))` | noValue: 0. "Which routes produced server errors? Empty means zero 5xx." |
| B3 (w8) | External probe success by region | timeseries | `min by(probe) (probe_success{job="goagain API", instance="https://api.goagain.dev"})` legend `{{probe}}` | min so a single failed probe run dips visibly. |
| B4 (w8) | External probe latency by region | timeseries | `sum by(probe) (rate(probe_all_duration_seconds_sum{job="goagain API"}[$__rate_interval])) / sum by(probe) (rate(probe_all_duration_seconds_count{job="goagain API"}[$__rate_interval]))` | unit s. |
| B5 (w8) | Scanner noise (404s on unknown paths) | timeseries | `sum(rate(http_server_request_total{job="goagain-api", http_route="/other", http_response_status_code=~"4.."}[$__rate_interval]))` | "Background internet scanning hitting unknown paths. Expected to be nonzero; a spike is only interesting if 5xx spikes with it." |

Row C: "Performance" (height 8)

| # | Title | Type | Query | Notes |
|---|-------|------|-------|-------|
| C1 (w12) | Latency percentiles | timeseries | three queries, legend p50/p95/p99: `histogram_quantile(0.50, sum by(le) (rate(http_server_request_duration_seconds_bucket{job="goagain-api", http_route!="/health"}[$__rate_interval])))` and same for 0.95, 0.99 | unit s. |
| C2 (w12) | Latency heatmap | heatmap | `sum by(le) (increase(http_server_request_duration_seconds_bucket{job="goagain-api", http_route!="/health"}[$__interval]))` with heatmap format | "Where does latency cluster? Bands moving up = degradation." If the heatmap panel proves hard to express in the v2 schema, fall back to the same query as a stacked timeseries and note it. |
| C3 (w24, h7) | Slowest routes (p95 over range) | table (instant) | `topk(10, histogram_quantile(0.95, sum by(http_route, le) (increase(http_server_request_duration_seconds_bucket{job="goagain-api", http_route!="/health"}[$__range]))))` | unit s. "Which routes were slowest over the selected period?" |

Row D: "Usage" (height 8)

| # | Title | Type | Query | Notes |
|---|-------|------|-------|-------|
| D1 (w12) | Traffic by route | timeseries, stacked | `sum by(http_route) (rate(http_server_request_total{job="goagain-api", http_route!="/health"}[$__rate_interval]))` | |
| D2 (w12, table, instant) | Top routes over range | table | `topk(10, sum by(http_route) (increase(http_server_request_total{job="goagain-api", http_route!="/health"}[$__range])))` | unit short. |
| D3 (w8) | Requests by method | timeseries | `sum by(http_request_method) (rate(http_server_request_total{job="goagain-api", http_route!="/health"}[$__rate_interval]))` | |
| D4 (w8) | Response size | timeseries | p95 and p50 of `http_server_response_body_size_bytes_bucket{job="goagain-api"}` via `histogram_quantile(X, sum by(le) (rate(...[$__rate_interval])))` | unit bytes. |
| D5 (w8, table, instant, datasource grafanacloud-logs) | Top client IPs | table | `topk(10, sum by(attributes_client_ip) (count_over_time({service_name="goagain-api"} | json attributes_client_ip="attributes.client_ip", attributes_path="attributes.path" | attributes_path != "/health" [$__range])))` | "Who used the API most in the period?" Note: after Phase 1 ships, health-check lines vanish from logs anyway; the filter stays harmless. |

Row E: "Data freshness" (height 4, five stats, width 4-5 each)

Stats, all datasource grafanacloud-prom, unit short, no thresholds:

- Cards: `goagain_data_cards{job="goagain-api"}`
- Sets: `goagain_data_sets{job="goagain-api"}`
- Abilities: `goagain_data_abilities{job="goagain-api"}`
- Keywords: `goagain_data_keywords{job="goagain-api"}`
- Index entries: `goagain_data_index_entries{job="goagain-api"}`

Shared description: "Embedded data set size. Bump expected after a data sync
release; a drop is a regression."

Row F: "Drill-down" (last; collapsed if the schema allows)

| # | Title | Type | Query (grafanacloud-logs) |
|---|-------|------|---------------------------|
| F1 (w24, h8) | Errors and warnings | logs | `{service_name="goagain-api"} | detected_level=~"warn|error"` |
| F2 (w24, h8) | Slow requests (>50ms) | logs | `{service_name="goagain-api"} | json attributes_duration_ms="attributes.duration_ms" | attributes_duration_ms > 50` |

Log panels: enable "Show time", sort descending. Trace links come from the
datasource's derived fields automatically.

### 4.4 Panels to delete (do not carry over)

- "HTTP Errors (Logs)", "Request Duration (Logs)", "Response Size Distribution"
  (log-derived duplicates; the log versions computed averages of per-stream
  aggregates, which is statistically invalid).
- "Active Requests" stat (near-constant zero; no decision value).
- "Log Volume by Severity" (drill-down row covers it).
- Old "Game Data Entities" timeseries (replaced by Row E stats).
- Old instant-rate stat panels and instant tables (replaced by range-correct
  versions above).

---

## 5. Phase 3: rebuild the MCP dashboard (uid `th2qhfr`)

### 5.1 Procedure

Same as 4.1 with `th2qhfr` and `dashboards/mcp.json` /
`dashboards/mcp.pre-overhaul.json`. Commit message:
`feat(observability): rebuild MCP dashboard around per-tool usage`.

### 5.2 Dashboard-level config

- Title: `goagain MCP`.
- Link to API dashboard uid `th8phsg`.
- Deploy annotation: same expression as 4.2 with `job="goagain-mcp"`.
- Variable `level`: custom, values `error,warn,info,debug`, multi-select,
  include-all, default `error+warn` if expressible (otherwise default all).
  Used only by panel F1.

### 5.3 Panels

Row A: "At a glance" (six stats, width 4, height 5)

| # | Title | Query | Unit / thresholds |
|---|-------|-------|-------------------|
| A1 | Tool calls | `sum(increase(mcp_tool_invocations_total{job="goagain-mcp"}[$__range])) or vector(0)` | short. "How many MCP tool invocations in the period?" |
| A2 | Tool success rate | `(sum(increase(mcp_tool_invocations_total{job="goagain-mcp", tool_status="success"}[$__range])) or vector(0)) / (sum(increase(mcp_tool_invocations_total{job="goagain-mcp"}[$__range])) or vector(1)) * 100` | percent; red<99, yellow<99.9, green>=99.9. Reads 0 when there were zero calls; description must say "0% with 0 calls means no traffic, not failure." |
| A3 | p95 tool latency | `histogram_quantile(0.95, sum by(le) (increase(mcp_tool_duration_seconds_bucket{job="goagain-mcp"}[$__range])))` | s; green<0.1, yellow<0.5, red>=0.5. Empty when no calls; set noValue text "no calls". |
| A4 | Availability (HTTP 5xx) | `(1 - (sum(increase(http_server_request_total{job="goagain-mcp", http_response_status_code=~"5..", http_route!="/health"}[$__range])) or vector(0)) / sum(increase(http_server_request_total{job="goagain-mcp", http_route!="/health"}[$__range]))) * 100` | percent; standard thresholds. |
| A5 | Active sessions | `sum(mcp_sessions_active{job="goagain-mcp"}) or vector(0)` | short. Depends on Phase 1 deploy; until then shows 0. Description states this. |
| A6 | Telemetry | `count(target_info{job="goagain-mcp"}) or vector(0)` | mappings: 0 -> STALE red, >=1 -> OK green. |

Row B: "Tool usage" (the heart of this dashboard)

| # | Title | Type | Query | Notes |
|---|-------|------|-------|-------|
| B1 (w24, h8) | Per-tool summary | table, instant, 4 queries joined | A: `sum by(tool_name) (increase(mcp_tool_invocations_total{job="goagain-mcp"}[$__range]))`; B: `sum by(tool_name) (increase(mcp_tool_invocations_total{job="goagain-mcp", tool_status="error"}[$__range]))`; C: `histogram_quantile(0.95, sum by(tool_name, le) (increase(mcp_tool_duration_seconds_bucket{job="goagain-mcp"}[$__range])))`; D: `histogram_quantile(0.95, sum by(tool_name, le) (increase(mcp_tool_result_count_bucket{job="goagain-mcp"}[$__range])))` | Transformations: joinByField on `tool_name` (outer join), then organize: rename A->Calls, B->Errors, C->p95 duration (s), D->p95 results. "Which tools were used, how often, did they fail, how slow?" |
| B2 (w12, h8) | Calls by tool | timeseries, stacked bars | `sum by(tool_name) (increase(mcp_tool_invocations_total{job="goagain-mcp"}[$__interval]))` legend `{{tool_name}}` | Bars so sparse data reads as discrete activity. |
| B3 (w6, h8) | Tool errors | timeseries, bars | `sum by(tool_name) (increase(mcp_tool_invocations_total{job="goagain-mcp", tool_status="error"}[$__interval]))` | noValue 0. |
| B4 (w6, h8) | Tool latency percentiles | timeseries | p50 and p95: `histogram_quantile(0.95, sum by(le) (increase(mcp_tool_duration_seconds_bucket{job="goagain-mcp"}[$__interval])))` | unit s. |

Row C: "Sessions and transport"

| # | Title | Type | Query | Notes |
|---|-------|------|-------|-------|
| C1 (w12, h8) | Sessions | timeseries | `sum(mcp_sessions_active{job="goagain-mcp"})` legend "Active"; `sum(increase(mcp_sessions_total{job="goagain-mcp"}[$__interval]))` legend "New per interval", bars | Empty until Phase 1 deploys; keep. |
| C2 (w12, h8) | /mcp requests by status | timeseries, stacked | `sum by(http_response_status_code) (rate(http_server_request_total{job="goagain-mcp", http_route="/mcp"}[$__rate_interval]))` | "Transport-level view of MCP traffic (POST /mcp is client activity)." |
| C3 (w12, h6) | Scanner noise | timeseries | `sum by(http_response_status_code) (rate(http_server_request_total{job="goagain-mcp", http_route="/other"}[$__rate_interval]))` | |
| C4 (w12, h6) | Tools in flight | timeseries | `sum(mcp_tool_active{job="goagain-mcp"}) or vector(0)` | NOTE metric name: `mcp_tool_active`, not `mcp_tool_in_flight`. |

Row D: "Data freshness": same five stats as API Row E but with
`{job="goagain-mcp"}`.

Row E: "Drill-down"

| # | Title | Type | Query |
|---|-------|------|-------|
| E1 (w24, h10) | Logs | logs | `{service_name="goagain-mcp"} | detected_level=~"$level"` (uses the `level` variable; with include-all this becomes a match-anything regex, which is fine) |

### 5.4 Panels to delete

- "Active sessions" / "Session rate" / "Session activity over time" originals
  (queried `{service="goagain-mcp"}`: wrong label, and the metrics did not
  exist; replaced by A5/C1 with `job` label).
- "Tools in flight" original (queried nonexistent `mcp_tool_in_flight`).
- "Total cards/sets/abilities/keywords" stats (queried nonexistent
  `goagain_data_*_total{service=...}`; replaced by Row D).
- "Recent errors" / "Recent warnings" / "All logs" trio (replaced by E1).
- "Tool result counts" standalone panel (folded into B1 table).
- "Log volume by level" (used the uppercase `level` stream label
  inconsistently; drill-down covers the need).

---

## 6. Phase 4: synthetics, alerts

### 6.1 Synthetic check for the MCP endpoint

BLOCKED ON USER INPUT: the public URL of the MCP endpoint is not recorded
anywhere in the repo or the existing checks. Ask the owner for it (likely a
subdomain of goagain.dev). Then create an HTTP check mirroring the existing
API check (job "goagain API", 3 probes, 300s frequency):

```
gcx --context theocrevon synthetic-monitoring checks list
gcx --context theocrevon synthetic-monitoring checks create --help
```

Create job name `goagain MCP`, target the health endpoint of the MCP host
(plain GET, expect 200), probes Montreal + Frankfurt + Singapore, frequency
300s. After data flows, add to the MCP dashboard Row A an
"Uptime (external)" stat and Row C probe panels, mirroring API A1/B3/B4 with
`job="goagain MCP"`.

### 6.2 Alert rules

Create two rules in the existing folder "Théo" (folder uid `fdvgy3wngzbb4a`),
via `gcx alert rules create --help` for the exact manifest shape (or the
Grafana UI as fallback; if creating rules proves brittle through the CLI,
output the two rule definitions as JSON files under `dashboards/alerts/` and
stop; the owner can import them).

Rule 1: `goagain 5xx ratio high`

```
(
  sum by(job) (increase(http_server_request_total{job=~"goagain-api|goagain-mcp", http_response_status_code=~"5..", http_route!="/health"}[15m]))
  /
  sum by(job) (increase(http_server_request_total{job=~"goagain-api|goagain-mcp", http_route!="/health"}[15m]))
) > 0.02
```

Pending period 15m. Annotation: "More than 2% of real requests returned 5xx
over 15m for {{ $labels.job }}."

Rule 2: `goagain p95 latency high`

```
histogram_quantile(0.95, sum by(job, le) (rate(http_server_request_duration_seconds_bucket{job=~"goagain-api|goagain-mcp", http_route!="/health"}[15m]))) > 0.5
```

Pending period 15m. Annotation: "p95 latency above 500ms over 15m for
{{ $labels.job }}."

Note on no-traffic behavior: with zero requests both rules return no data
(division by zero yields no series; empty rate yields no series). Set the
rules' no-data handling to OK, and rely on the existing telemetry-stopped
alert plus synthetics for liveness.

---

## 7. Phase 5 (optional): runtime row

Only after Phase 1 has been deployed and runtime metric names are confirmed
per 3.2: add a "Runtime" row (heap bytes, goroutines, GC pause p95) to both
dashboards using the discovered names. Skip entirely if the discovery query
returns nothing.

---

## 8. Acceptance checklist

Per phase, all must hold before declaring done:

Phase 1:
- [ ] Full check battery green (build, test -race, vet, gofmt, golangci-lint, gosec, govulncheck).
- [ ] Session hook test exists and passes.
- [ ] No log line emitted for 200 /health in middleware test.

Phase 2 / 3 (each dashboard):
- [ ] `dashboards/<name>.pre-overhaul.json` committed before any update.
- [ ] Every PromQL/LogQL expression verified via gcx with concrete durations,
      results consistent with the panel's intent.
- [ ] `gcx dashboards update` succeeded; `gcx dashboards snapshot` rendered;
      snapshot inspected; no panel shows an error state.
- [ ] Stats change meaningfully when switching between 6h and 7d ranges
      (verify by substituting `6h` vs `7d` into the `$__range` queries with
      gcx and confirming the numbers differ as expected).
- [ ] Final manifest committed to `dashboards/`.
- [ ] Dead panels from sections 4.4 / 5.4 absent from the new manifest.

Phase 4:
- [ ] MCP check created (or explicitly blocked on the URL and reported).
- [ ] Both alert rules exist and evaluate (state listed via
      `gcx alert rules list`), or rule JSON committed under `dashboards/alerts/`
      with a note.

Reporting: when finishing a phase, state plainly what was done, what was
verified, and anything skipped or blocked. Never claim a query works without
having run it.
