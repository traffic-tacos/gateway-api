.PHONY: build test clean run docker-build docker-run help

# Variables
APP_NAME = gateway-api
VERSION ?= latest
DOCKER_IMAGE = $(APP_NAME):$(VERSION)
GO_VERSION = 1.22

# Build the application
build:
	@echo "Building $(APP_NAME)..."
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o bin/$(APP_NAME) cmd/gateway/main.go

# Run tests
test:
	@echo "Running tests..."
	go test -v -race -coverprofile=coverage.out ./...

# Test coverage
coverage: test
	@echo "Generating coverage report..."
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report saved to coverage.html"

# Run linting
lint:
	@echo "Running linter..."
	golangci-lint run

# Format code
fmt:
	@echo "Formatting code..."
	go fmt ./...

# Tidy dependencies
tidy:
	@echo "Tidying dependencies..."
	go mod tidy

# Run the application locally
run:
	@echo "Running $(APP_NAME) locally..."
	go run cmd/gateway/main.go

# Build Docker image
docker-build:
	@echo "Building Docker image: $(DOCKER_IMAGE)"
	docker build -t $(DOCKER_IMAGE) .

# Run Docker container
docker-run:
	@echo "Running Docker container: $(DOCKER_IMAGE)"
	docker run --rm -p 8000:8000 \
		-e JWT_JWKS_ENDPOINT="https://your-auth.com/.well-known/jwks.json" \
		-e JWT_ISSUER="https://your-auth.com" \
		-e JWT_AUDIENCE="gateway-api" \
		-e REDIS_ADDRESS="localhost:6379" \
		$(DOCKER_IMAGE)

# Benchmark
bench:
	@echo "Running benchmarks..."
	go test -bench=. -benchmem ./...

# Security scan
security:
	@echo "Running security scan..."
	gosec ./...

# Generate
generate:
	@echo "Running go generate..."
	go generate ./...

# Generate Swagger documentation
swagger:
	@echo "Generating Swagger documentation..."
	$(shell go env GOPATH)/bin/swag init -g cmd/gateway/main.go -o docs
	@echo "Swagger documentation generated in docs/ directory"

# Check dependencies for vulnerabilities
vuln-check:
	@echo "Checking for vulnerabilities..."
	govulncheck ./...

# Clean build artifacts
clean:
	@echo "Cleaning up..."
	rm -rf bin/
	rm -f coverage.out coverage.html
	docker image prune -f

# Production build with optimizations
build-prod:
	@echo "Building for production..."
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
		-ldflags="-w -s -X main.version=$(VERSION) -X main.buildTime=$(shell date -u +%Y%m%d.%H%M%S)" \
		-a -installsuffix cgo \
		-o bin/$(APP_NAME) cmd/gateway/main.go

# Multi-arch Docker build
docker-build-multi:
	@echo "Building multi-architecture Docker image..."
	docker buildx build --platform linux/amd64,linux/arm64 -t $(DOCKER_IMAGE) --push .

# Install development tools
install-tools:
	@echo "Installing development tools..."
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	go install github.com/securecodewarrior/gosec/v2/cmd/gosec@latest
	go install golang.org/x/vuln/cmd/govulncheck@latest
	go install github.com/swaggo/swag/cmd/swag@latest

# Full CI pipeline
ci: fmt lint test security vuln-check build

# Help
help:
	@echo "Available targets:"
	@echo "  build         - Build the application"
	@echo "  test          - Run tests"
	@echo "  coverage      - Generate test coverage report"
	@echo "  lint          - Run linter"
	@echo "  fmt           - Format code"
	@echo "  tidy          - Tidy dependencies"
	@echo "  run           - Run application locally"
	@echo "  docker-build  - Build Docker image"
	@echo "  docker-run    - Run Docker container"
	@echo "  swagger       - Generate Swagger documentation"
	@echo "  clean         - Clean build artifacts"
	@echo "  build-prod    - Production build"
	@echo "  ci            - Run CI pipeline"
	@echo "  help          - Show this help"