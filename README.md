# Gateway API

A high-performance BFF (Backend for Frontend) service for the Traffic Tacos ticket reservation system, designed to handle 30k RPS traffic with robust authentication, rate limiting, and observability.

## Recent Updates

- ✅ **gRPC Integration**: Successfully migrated to proto-contracts Go module for backend communication
- ✅ **Hybrid Architecture**: REST API frontend with gRPC backend communication for optimal performance
- ✅ **Proto Contracts**: Integrated Traffic Tacos proto-contracts for type-safe inter-service communication
- ✅ **Client Refactoring**: Replaced HTTP clients with gRPC clients for reservation and payment services
- ✅ **Local Development Setup**: Complete Redis installation and configuration guide
- ✅ **AWS ElastiCache Integration**: Production-ready Redis with Secrets Manager auth

## Features

- **High Performance**: Built with Go and Fiber framework for handling 30k RPS
- **JWT Authentication**: JWKS-based token validation with Redis caching
- **Rate Limiting**: Token bucket algorithm with Redis backend
- **Idempotency**: Request deduplication with conflict detection
- **Observability**: Prometheus metrics, OpenTelemetry tracing, structured logging
- **Queue Management**: Virtual queuing system for traffic control
- **Backend Integration**: gRPC-based communication with reservation, inventory, and payment services
- **Proto Contracts**: Type-safe service communication using Traffic Tacos proto-contracts module
- **AWS Integration**: ElastiCache Redis with Secrets Manager for production deployment

## Architecture

### Hybrid REST + gRPC Architecture

```
Load Balancer
    ↓ HTTP/REST
┌─────────────────┐
│   Gateway API   │ (REST API Frontend)
│  (Go + Fiber)   │ Port 8000
└─────────┬───────┘
          ↓ gRPC Communication
┌─────────────────┬─────────────────┬─────────────────┐
│ reservation-api │ inventory-api   │ payment-sim-api │
│ (Kotlin:9090)   │ (Go:9091)      │ (Go:9092)      │
└─────────────────┴─────────────────┴─────────────────┘
         ↕ gRPC
    (Inter-service communication)
```

### Communication Patterns

- **External Interface**: HTTP REST API for web/mobile clients
- **Internal Communication**: High-performance gRPC using proto-contracts
- **Service Discovery**: Direct address configuration with health checks
- **Type Safety**: Shared proto definitions across all services

## Quick Start

### Prerequisites

- Go 1.23+
- Redis (local or AWS ElastiCache)
- Access to JWT JWKS endpoint
- gRPC backend services (reservation-api, inventory-api, payment-sim-api)
- Traffic Tacos proto-contracts module
- AWS CLI configured (for ElastiCache access)

### Local Development Setup

1. **Clone the repository**
   ```bash
   git clone https://github.com/traffic-tacos/gateway-api.git
   cd gateway-api
   ```

2. **Install Redis locally**
   ```bash
   # macOS
   brew install redis
   brew services start redis

   # Or use Docker
   docker run -d --name redis -p 6379:6379 redis:7-alpine
   ```

3. **Install dependencies**
   ```bash
   # Download Go modules including proto-contracts
   go mod download
   ```

4. **Setup environment configuration**
   ```bash
   # For local development with local Redis
   cp .env .env.local

   # For AWS ElastiCache development
   cp .env.aws .env.local
   # Edit AWS profile and ElastiCache endpoints
   ```

5. **Configure backend services**
   ```bash
   # Set gRPC backend addresses for local development
   export BACKEND_RESERVATION_API_GRPC_ADDRESS="localhost:9090"
   export BACKEND_PAYMENT_API_GRPC_ADDRESS="localhost:9092"
   export BACKEND_RESERVATION_API_TLS_ENABLED="false"
   export BACKEND_PAYMENT_API_TLS_ENABLED="false"
   ```

6. **Run the application**
   ```bash
   # Load environment and start
   source .env.local
   export JWT_JWKS_ENDPOINT="https://www.googleapis.com/oauth2/v3/certs"
   export JWT_ISSUER="https://accounts.google.com"
   export JWT_AUDIENCE="gateway-api-local"

   go run cmd/gateway/main.go
   ```

7. **Access Swagger documentation**
   ```
   http://localhost:8000/swagger/index.html
   ```

8. **Additional Resources**
   - [Local Development Guide](README_LOCAL.md) - Detailed Redis setup options
   - [Proto Contracts Documentation](.cursor/rules/project-proto-contracts.mdc) - gRPC service definitions and usage
   - [AWS Configuration](.env.aws) - ElastiCache and Secrets Manager setup

### Alternative Setup (Manual)

1. **Install dependencies**
   ```bash
   go mod download
   ```

2. **Set environment variables**
   ```bash
   export JWT_JWKS_ENDPOINT="https://www.googleapis.com/oauth2/v3/certs"
   export JWT_ISSUER="https://accounts.google.com"
   export JWT_AUDIENCE="gateway-api-local"
   export REDIS_ADDRESS="localhost:6379"
   export SERVER_PORT="8000"
   ```

3. **Run the application**
   ```bash
   go run cmd/gateway/main.go
   ```

## Configuration

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `SERVER_PORT` | Server port | `8000` |
| `SERVER_ENVIRONMENT` | Environment (development/production) | `development` |
| `REDIS_ADDRESS` | Redis server address | `localhost:6379` |
| `JWT_JWKS_ENDPOINT` | JWKS endpoint URL | **Required** |
| `JWT_ISSUER` | JWT issuer | **Required** |
| `JWT_AUDIENCE` | JWT audience | **Required** |
| `BACKEND_RESERVATION_API_GRPC_ADDRESS` | Reservation API gRPC address | `reservation-api:9090` |
| `BACKEND_PAYMENT_API_GRPC_ADDRESS` | Payment API gRPC address | `payment-sim-api:9092` |
| `BACKEND_RESERVATION_API_TLS_ENABLED` | Enable TLS for reservation API | `false` |
| `BACKEND_PAYMENT_API_TLS_ENABLED` | Enable TLS for payment API | `false` |
| `RATE_LIMIT_RPS` | Rate limit per second | `50` |
| `RATE_LIMIT_BURST` | Rate limit burst | `100` |
| `OBSERVABILITY_OTLP_ENDPOINT` | OTLP endpoint | `http://localhost:4318` |
| `LOG_LEVEL` | Log level (debug/info/warn/error) | `info` |

### AWS Integration

For production deployment with AWS resources:

```bash
# AWS credentials (use IAM roles in production)
export AWS_PROFILE=tacos
export AWS_REGION=ap-northeast-2

# ElastiCache Redis
export REDIS_ADDRESS="master.traffic-tacos-redis.w6eqga.apn2.cache.amazonaws.com:6379"
export REDIS_TLS_ENABLED=true

# Secrets Manager integration for Redis AUTH token
export AWS_SECRET_NAME="traffic-tacos/redis/auth-token"
export REDIS_PASSWORD_FROM_SECRETS=true

# Other AWS configurations
export AWS_SDK_LOAD_CONFIG=true
```

#### ElastiCache Setup

1. **Configure AWS credentials**:
   ```bash
   aws configure --profile tacos
   # Or use IAM roles in production
   ```

2. **Update .env file**:
   ```bash
   # Copy and edit environment file for AWS
   cp .env.aws .env.local

   # Or manually update Redis address to your ElastiCache endpoint
   REDIS_ADDRESS=master.traffic-tacos-redis.w6eqga.apn2.cache.amazonaws.com:6379
   AWS_PROFILE=tacos
   AWS_REGION=ap-northeast-2
   REDIS_TLS_ENABLED=true
   REDIS_PASSWORD_FROM_SECRETS=true
   ```

3. **Security Group Configuration**:
   - Allow inbound traffic on port 6379 from your application
   - For development: Allow your local IP
   - For production: Allow EKS cluster security group

4. **ElastiCache Features**:
   - **AUTH token**: Set `REDIS_PASSWORD` for authentication
   - **In-transit encryption**: Set `REDIS_TLS_ENABLED=true`
   - **At-rest encryption**: Enable in ElastiCache configuration

#### AWS Observability (분산 추적 & 모니터링)

**OTEL (OpenTelemetry)**은 마이크로서비스 간 요청 추적과 성능 모니터링을 위한 도구입니다.

**AWS 관측성 서비스 옵션:**

1. **AWS X-Ray** (추천):
   ```bash
   # .env 설정
   OBSERVABILITY_OTLP_ENDPOINT=          # 비워두면 X-Ray 사용
   OBSERVABILITY_TRACING_ENABLED=true
   OBSERVABILITY_SAMPLE_RATE=0.1         # 10% 샘플링
   ```

2. **AWS OTEL Collector (ECS/EKS)**:
   ```bash
   # ECS/EKS 환경에서 OTEL Collector 사용
   OBSERVABILITY_OTLP_ENDPOINT=http://aws-otel-collector:4318
   ```

3. **Amazon CloudWatch**:
   - X-Ray와 통합되어 트레이스 데이터를 CloudWatch에서 확인 가능
   - 자동으로 서비스 맵과 성능 메트릭 생성

**X-Ray에서 확인할 수 있는 정보:**
- 🔍 **서비스 맵**: Gateway → Reservation → Inventory → Payment 흐름
- ⏱️ **응답 시간**: 각 서비스별 지연시간 분석
- 🚨 **에러 추적**: 실패한 요청의 전체 경로 추적
- 📊 **성능 분석**: 병목 구간 식별

## API Endpoints

### Health & Metrics

- `GET /healthz` - Health check
- `GET /readyz` - Readiness check
- `GET /version` - Version information
- `GET /metrics` - Prometheus metrics
- `GET /swagger/*` - Swagger API documentation

### Queue Management (Public)

- `POST /api/v1/queue/join` - Join waiting queue
- `GET /api/v1/queue/status` - Check queue status
- `POST /api/v1/queue/enter` - Request queue entrance
- `DELETE /api/v1/queue/leave` - Leave queue

### Reservations (Authenticated)

- `POST /api/v1/reservations` - Create reservation
- `GET /api/v1/reservations/{id}` - Get reservation
- `POST /api/v1/reservations/{id}/confirm` - Confirm reservation
- `POST /api/v1/reservations/{id}/cancel` - Cancel reservation

### Payments (Authenticated)

- `POST /api/v1/payment/intent` - Create payment intent
- `GET /api/v1/payment/{id}/status` - Get payment status
- `POST /api/v1/payment/process` - Process payment (testing)

## Authentication

### JWT Requirements

All protected endpoints require a valid JWT token:

```http
Authorization: Bearer <jwt-token>
```

### Token Validation

- **JWKS-based verification**: Dynamic key fetching and caching
- **Standard claims validation**: `iss`, `aud`, `exp`, `nbf`
- **Redis caching**: JWK sets cached for 10 minutes

### Exempt Endpoints

The following endpoints don't require authentication:
- Health checks (`/healthz`, `/readyz`, `/version`)
- Metrics (`/metrics`)
- Queue join and status (`/api/v1/queue/join`, `/api/v1/queue/status`)

## Rate Limiting

### Token Bucket Algorithm

- **Per-user limits**: 50 RPS, burst of 100
- **IP-based fallback**: When user ID unavailable
- **Redis-backed**: Atomic operations with Lua scripts

### Headers

Rate limit information is returned in response headers:

```http
X-RateLimit-Limit: 50
X-RateLimit-Remaining: 42
X-RateLimit-Reset: 1642678800
Retry-After: 1
```

## Idempotency

### Request Deduplication

For state-changing operations (`POST`, `PUT`, `PATCH`, `DELETE`):

```http
Idempotency-Key: 550e8400-e29b-41d4-a716-446655440000
```

### Features

- **UUID v4 validation**: Ensures proper key format
- **Conflict detection**: Rejects different requests with same key
- **Response caching**: Successful responses cached for 5 minutes
- **Automatic cleanup**: Expired keys auto-removed

## Observability

### Metrics

Prometheus metrics available at `/metrics`:

- `http_server_requests_total` - HTTP request count
- `http_server_requests_duration_seconds` - Request latency
- `backend_call_duration_seconds` - Backend API latency
- `ratelimit_dropped_total` - Rate limit drops
- `idempotency_hits_total` - Idempotency cache hits
- `queue_operations_total` - Queue operations

### Tracing

OpenTelemetry tracing with:

- **Distributed tracing**: Across all service calls
- **Automatic instrumentation**: HTTP requests and responses
- **Custom spans**: Business logic tracing
- **Context propagation**: W3C trace context headers

### Logging

Structured JSON logging with:

```json
{
  "ts": "2024-01-01T12:00:00Z",
  "level": "info",
  "msg": "request_completed",
  "http": {
    "method": "POST",
    "route": "/api/v1/reservations",
    "status": 201
  },
  "latency_ms": 45.2,
  "trace_id": "abc123...",
  "user_id": "user456..."
}
```

## Development

### Local Development

#### Recommended Setup

1. **Install Redis locally**
   ```bash
   # macOS with Homebrew
   brew install redis
   brew services start redis

   # Verify Redis is running
   redis-cli ping  # Should return PONG
   ```

2. **Setup environment**
   ```bash
   # For local development
   cp .env .env.local
   # Edit .env.local with your preferred configuration
   ```

3. **Run the service**
   ```bash
   # Load environment and start
   source .env.local
   export JWT_JWKS_ENDPOINT="https://www.googleapis.com/oauth2/v3/certs"
   export JWT_ISSUER="https://accounts.google.com"
   export JWT_AUDIENCE="gateway-api-local"

   go run cmd/gateway/main.go
   ```

4. **Access services**
   ```bash
   # API Documentation
   open http://localhost:8000/swagger/index.html

   # Health Check
   curl http://localhost:8000/healthz

   # Metrics
   curl http://localhost:8000/metrics
   ```

#### Alternative: Docker Redis
```bash
# If you prefer Docker for Redis
docker run -d --name redis -p 6379:6379 redis:7-alpine

# Verify Redis is accessible
docker exec redis redis-cli ping
```

#### Development Tools
```bash
# Generate/Update Swagger docs
go install github.com/swaggo/swag/cmd/swag@latest
swag init -g cmd/gateway/main.go -o docs

# Build the application
go build -o gateway-api cmd/gateway/main.go

# Run tests
go test ./internal/... -v

# Check Redis connection
redis-cli -h localhost -p 6379 ping
```

### Testing

```bash
# Unit tests
go test ./internal/...

# Integration tests with testcontainers
go test ./tests/integration/...

# Load testing
# Configure your load testing tool to hit localhost:8000
```

### Building

```bash
# Local build
go build -o gateway-api cmd/gateway/main.go

# Generate Swagger documentation
make swagger

# Docker build
docker build -t gateway-api:latest .

# Multi-arch build
docker buildx build --platform linux/amd64,linux/arm64 -t gateway-api:latest .
```

### API Documentation

The service provides interactive Swagger documentation:

1. **Start the service**:
   ```bash
   go run cmd/gateway/main.go
   ```

2. **Access Swagger UI**:
   ```
   http://localhost:8000/swagger/index.html
   ```

3. **Generate/Update docs**:
   ```bash
   make swagger
   ```

The Swagger documentation includes:
- **Interactive API explorer** - Test endpoints directly from the browser
- **Request/response schemas** - Complete data models
- **Authentication examples** - JWT Bearer token usage
- **Error responses** - Comprehensive error codes and messages

## Deployment

### Docker

```bash
# Pull and run
docker run -d \
  --name gateway-api \
  -p 8000:8000 \
  -e JWT_JWKS_ENDPOINT="https://your-auth.com/.well-known/jwks.json" \
  -e JWT_ISSUER="https://your-auth.com" \
  -e JWT_AUDIENCE="gateway-api" \
  -e REDIS_ADDRESS="redis:6379" \
  gateway-api:latest
```

### Kubernetes

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: gateway-api
spec:
  replicas: 3
  selector:
    matchLabels:
      app: gateway-api
  template:
    metadata:
      labels:
        app: gateway-api
    spec:
      containers:
      - name: gateway-api
        image: gateway-api:latest
        ports:
        - containerPort: 8000
        env:
        - name: SERVER_PORT
          value: "8000"
        - name: REDIS_ADDRESS
          value: "redis-cluster:6379"
        livenessProbe:
          httpGet:
            path: /healthz
            port: 8000
          initialDelaySeconds: 30
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /readyz
            port: 8000
          initialDelaySeconds: 5
          periodSeconds: 5
        resources:
          requests:
            cpu: 200m
            memory: 256Mi
          limits:
            cpu: 1000m
            memory: 512Mi
```

## Performance

### Targets

- **Throughput**: 30k RPS sustained
- **Latency**: P95 < 50ms (excluding backend calls)
- **Error Rate**: < 0.5%
- **Memory**: < 512MB per instance

### Optimization

- **Connection pooling**: Optimized HTTP client settings
- **Redis pipelining**: Batch operations where possible
- **Graceful degradation**: Continue serving on Redis failures
- **Circuit breakers**: Prevent cascade failures

## Security

### Best Practices

- **Minimal attack surface**: Only necessary endpoints exposed
- **Input validation**: All request data validated
- **Security headers**: HSTS, CSP, X-Frame-Options
- **Error handling**: No sensitive data in error responses
- **Rate limiting**: DDoS protection

### Production Considerations

- **TLS termination**: At load balancer level
- **Secret management**: Use AWS Secrets Manager
- **Network policies**: Restrict inter-service communication
- **Audit logging**: All authentication events logged

## Troubleshooting

### Common Issues

1. **JWT validation failures**
   - Check JWKS endpoint accessibility
   - Verify issuer and audience configuration
   - Check Redis connectivity for key caching

2. **Rate limiting issues**
   - Verify Redis connection
   - Check rate limit configuration
   - Monitor rate limit metrics

3. **Backend connectivity**
   - Verify backend service URLs
   - Check network connectivity
   - Monitor backend call metrics

### Monitoring

- **Health endpoints**: Regular health check monitoring
- **Metrics alerting**: Set up alerts on error rates and latency
- **Log monitoring**: Monitor for error patterns
- **Distributed tracing**: Use for request flow debugging

## Contributing

1. Fork the repository
2. Create a feature branch
3. Add tests for new functionality
4. Ensure all tests pass
5. Submit a pull request

## License

This project is part of the Traffic Tacos microservices platform.