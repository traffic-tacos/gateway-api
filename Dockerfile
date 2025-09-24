# Multi-stage build for Go application
FROM golang:1.23-alpine AS builder

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

# Install swag for generating swagger docs
RUN go install github.com/swaggo/swag/cmd/swag@v1.16.3

# Generate swagger docs
RUN swag init -g cmd/gateway/main.go -o docs --parseDependency --parseInternal

# Verify docs were generated
RUN test -f docs/docs.go && test -f docs/swagger.json

# Build the application
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags='-w -s -extldflags "-static"' \
    -a -installsuffix cgo \
    -o gateway-api ./cmd/gateway

# Production stage
FROM scratch

# Copy timezone data and SSL certificates
COPY --from=builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Create non-root user files
COPY --from=builder /etc/passwd /etc/passwd

# Copy the binary
COPY --from=builder /app/gateway-api /gateway-api

# Create a non-root user
USER 10001:10001

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD ["/gateway-api", "--health-check"]

# Expose port
EXPOSE 8000

# Run the application
ENTRYPOINT ["/gateway-api"]