# Build stage
FROM golang:1.22-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git ca-certificates tzdata

# Set working directory
WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod download

# Copy source code
COPY . .

# Build the application
# CGO_ENABLED=0 for static linking
# GOOS=linux for Linux binary
# -a flag to force rebuild
# -installsuffix cgo to avoid cache conflicts
# -o for output filename
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -a \
    -installsuffix cgo \
    -o gateway-api \
    ./cmd/gateway

# Verify the binary
RUN ./gateway-api --help || true

# Runtime stage
FROM alpine:latest

# Install ca-certificates, timezone data, and curl for health checks
RUN apk --no-cache add ca-certificates tzdata curl && \
    addgroup -g 10001 -S appgroup && \
    adduser -u 10001 -S appuser -G appgroup

# Import certificates and timezone from builder stage
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo

# Copy the binary from builder stage
COPY --from=builder --chown=appuser:appgroup /app/gateway-api /gateway-api

# Switch to non-root user
USER appuser

# Expose port
EXPOSE 8080

# Health check using curl
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD curl -f http://localhost:8080/healthz || exit 1

# Set the binary as entrypoint
ENTRYPOINT ["/gateway-api"]
