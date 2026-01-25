# Build stage
FROM golang:1.23-alpine AS builder

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
