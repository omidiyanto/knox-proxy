# ─── Build Stage ────────────────────────────────────────────────────────────
FROM golang:alpine AS builder

RUN apk --no-cache add ca-certificates git

WORKDIR /app

# Copy all source files
COPY . .

# Resolve dependencies and build
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o /knox-proxy .

# ─── Runtime Stage ──────────────────────────────────────────────────────────
FROM alpine:3.19

RUN apk --no-cache add ca-certificates

# Create non-root user for security
RUN addgroup -g 1001 knox && \
    adduser -u 1001 -G knox -s /bin/sh -D knox

WORKDIR /app

# Copy the binary from builder
COPY --from=builder /knox-proxy .

# Switch to non-root user
USER knox

# Expose the proxy port
EXPOSE 8443

# Health check
HEALTHCHECK --interval=30s --timeout=3s --retries=3 \
    CMD wget -qO- http://localhost:8443/healthz || exit 1

ENTRYPOINT ["./knox-proxy"]
