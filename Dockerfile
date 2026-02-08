# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build both binaries
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /goagain-api ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /goagain-mcp ./cmd/mcp

# Runtime stage for API
FROM alpine:3.19 AS api

LABEL org.opencontainers.image.title="goagain-api" \
      org.opencontainers.image.description="REST API for Flesh and Blood card data" \
      org.opencontainers.image.source="https://github.com/oleiade/goagain"

# Environment variable documentation
LABEL io.goagain.env.PORT="API server port (default: 8080)" \
      io.goagain.env.CORS_ORIGINS="Comma-separated allowed CORS origins (default: *)" \
      io.goagain.env.RATE_LIMIT_RPS="Rate limit requests per second per IP (default: 100)" \
      io.goagain.env.TRUSTED_PROXIES="Comma-separated CIDR blocks for proxy header trust" \
      io.goagain.env.LOG_LEVEL="Log level: debug, info, warn, error (default: info)" \
      io.goagain.env.LOG_FORMAT="Log format: json or text (default: json)"

# OpenTelemetry configuration
LABEL io.goagain.otel.OTEL_EXPORTER_OTLP_ENDPOINT="OTLP endpoint (e.g., localhost:4318). If unset, telemetry goes to stdout" \
      io.goagain.otel.OTEL_SERVICE_NAME="Service name for traces/metrics/logs (default: goagain-api)" \
      io.goagain.otel.OTEL_SERVICE_VERSION="Service version (default: 0.1.0)" \
      io.goagain.otel.OTEL_ENVIRONMENT="Deployment environment (default: development)"

RUN apk --no-cache add ca-certificates wget && \
    addgroup -g 1000 appgroup && \
    adduser -u 1000 -G appgroup -D appuser

WORKDIR /app
COPY --from=builder /goagain-api /app/goagain-api

USER appuser

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -q --spider http://localhost:8080/health || exit 1

ENTRYPOINT ["/app/goagain-api"]

# Runtime stage for MCP
FROM alpine:3.19 AS mcp

LABEL org.opencontainers.image.title="goagain-mcp" \
      org.opencontainers.image.description="MCP server for Flesh and Blood card data" \
      org.opencontainers.image.source="https://github.com/oleiade/goagain"

# Environment variable documentation
LABEL io.goagain.env.MCP_MODE="Transport mode: stdio or http (default: stdio)" \
      io.goagain.env.MCP_PORT="MCP HTTP server port (default: 8081)" \
      io.goagain.env.LOG_LEVEL="Log level: debug, info, warn, error (default: info)" \
      io.goagain.env.LOG_FORMAT="Log format: json or text (default: json)"

# OpenTelemetry configuration
LABEL io.goagain.otel.OTEL_EXPORTER_OTLP_ENDPOINT="OTLP endpoint (e.g., localhost:4318). If unset, telemetry goes to stdout" \
      io.goagain.otel.OTEL_SERVICE_NAME="Service name for traces/metrics/logs (default: goagain-mcp)" \
      io.goagain.otel.OTEL_SERVICE_VERSION="Service version (default: 0.1.0)" \
      io.goagain.otel.OTEL_ENVIRONMENT="Deployment environment (default: development)"

RUN apk --no-cache add ca-certificates wget && \
    addgroup -g 1000 appgroup && \
    adduser -u 1000 -G appgroup -D appuser

WORKDIR /app
COPY --from=builder /goagain-mcp /app/goagain-mcp

USER appuser

EXPOSE 8081

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -q --spider http://localhost:8081/health || exit 1

ENTRYPOINT ["/app/goagain-mcp"]
CMD ["-mode=http"]
