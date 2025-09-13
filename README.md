# Gateway API

고성능 티켓 예매 시스템을 위한 API 게이트웨이 애플리케이션입니다.

## 개요

이 프로젝트는 대규모 티켓 예매 시스템의 핵심 컴포넌트인 API 게이트웨이의 Go 애플리케이션 코드입니다.

주요 기능:
- JWT 인증 및 권한 부여
- 요청 라우팅 및 로드 밸런싱
- 레이트 리미팅 및 DDoS 방어
- Idempotency-Key 기반 중복 요청 방지
- OpenTelemetry 분산 추적
- Prometheus 메트릭 수집
- 구조화된 JSON 로깅

## 기술 스택

- **언어**: Go 1.22+
- **프레임워크**: Fiber v2
- **JWT**: lestrrat-go/jwx
- **캐싱**: go-redis/v8
- **메트릭**: Prometheus client
- **트레이싱**: OpenTelemetry
- **로깅**: Zap (구조화)

## 프로젝트 구조

```
gateway-api/
├── cmd/gateway/           # 메인 애플리케이션
├── internal/
│   ├── config/           # 설정 관리
│   ├── logging/          # 구조화 로깅
│   ├── metrics/          # Prometheus 메트릭
│   ├── middleware/       # 미들웨어 (JWT, RateLimit, 등)
│   ├── clients/          # 백엔드 API 클라이언트
│   ├── routes/           # API 핸들러
│   └── handlers/         # 비즈니스 로직
├── pkg/errors/           # 에러 처리
└── go.mod, go.sum        # Go 모듈
```

## 빠른 시작

### 로컬 개발 (Go)

#### 전제 조건
- Go 1.22 이상
- Redis (로컬 또는 Docker)

#### 실행
```bash
# 의존성 설치
go mod download

# Redis 실행 (별도 터미널)
redis-server

# 애플리케이션 실행
go run cmd/gateway/main.go
```

### Docker 컨테이너

#### Docker 이미지 빌드
```bash
# 이미지 빌드
docker build -t traffic-tacos/gateway-api .

# 컨테이너 실행
docker run -p 8080:8080 \
  -e REDIS_ADDR=host.docker.internal:6379 \
  -e JWT_SKIP_VERIFY=true \
  traffic-tacos/gateway-api
```

### Docker Compose (전체 스택)

#### 로컬 개발 환경 실행
```bash
# 전체 스택 실행 (Redis + 목 API 서버들)
docker-compose up -d

# 로그 확인
docker-compose logs -f gateway-api

# 중지
docker-compose down
```

#### 서비스 구성
- **gateway-api**: 메인 API 게이트웨이 (포트 8080)
- **redis**: 캐싱 및 레이트 리미팅 (포트 6379)
- **reservation-api**: 목 예약 API (포트 8081)
- **payment-sim-api**: 목 결제 API (포트 8082)
- **otel-collector**: OpenTelemetry 수집기 (포트 4317)

### API 테스트

```bash
# 헬스 체크
curl http://localhost:8080/healthz

# 대기열 참여 (익명 허용)
curl -X POST http://localhost:8080/api/v1/queue/join \
  -H "Content-Type: application/json" \
  -d '{"event_id": "event-001", "user_id": "user-123"}'

# 예약 생성 (JWT 필요)
curl -X POST http://localhost:8080/api/v1/reservations/create \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer <JWT_TOKEN>" \
  -H "Idempotency-Key: $(uuidgen)" \
  -d '{
    "event_id": "event-001",
    "seat_ids": ["A1", "A2"],
    "quantity": 2,
    "total_price": 100.0
  }'
```

### API 엔드포인트

| 엔드포인트 | 메서드 | 설명 | 인증 |
|------------|--------|------|------|
| `/healthz` | GET | 헬스 체크 | ❌ |
| `/readyz` | GET | readiness 체크 | ❌ |
| `/version` | GET | 버전 정보 | ❌ |
| `/metrics` | GET | Prometheus 메트릭 | ❌ |
| `/api/v1/queue/join` | POST | 대기열 참여 | ❌ (익명 허용) |
| `/api/v1/queue/status` | GET | 대기열 상태 조회 | ❌ |
| `/api/v1/reservations/create` | POST | 예약 생성 | ✅ (JWT + 멱등성) |
| `/api/v1/reservations/{id}` | GET | 예약 조회 | ✅ (JWT) |
| `/api/v1/reservations/{id}/cancel` | POST | 예약 취소 | ✅ (JWT + 멱등성) |
| `/api/v1/reservations/{id}/confirm` | POST | 예약 확정 | ✅ (JWT + 멱등성) |
| `/api/v1/payment/intent` | POST | 결제 인텐트 생성 | ✅ (JWT + 멱등성) |
| `/api/v1/payment/intent/{id}` | GET | 결제 인텐트 조회 | ✅ (JWT) |

## 환경 변수

### 필수 설정
| 변수 | 설명 | 기본값 | 필수 |
|------|------|--------|------|
| `REDIS_ADDR` | Redis 서버 주소 | `redis:6379` | ✅ |
| `JWT_ISSUER` | JWT 토큰 발급자 | - | ✅ (프로덕션) |
| `JWT_AUDIENCE` | JWT 대상자 | - | ✅ (프로덕션) |
| `JWT_JWKS_URL` | JWK Set URL | - | ✅ (프로덕션) |

### 선택 설정
| 변수 | 설명 | 기본값 |
|------|------|--------|
| `PORT` | 서버 포트 | `8080` |
| `RATE_LIMIT_RPS` | 초당 요청 제한 | `50` |
| `RATE_LIMIT_BURST` | 버스트 허용량 | `100` |
| `IDEMPOTENCY_TTL_SECONDS` | 멱등성 키 TTL | `300` |
| `LOG_LEVEL` | 로그 레벨 (debug/info/warn/error) | `info` |
| `JWT_SKIP_VERIFY` | JWT 검증 스킵 (개발용) | `false` |
| `TRACE_SAMPLING` | 트레이스 샘플링 비율 | `0.1` |

### 백엔드 서비스
| 변수 | 설명 | 기본값 |
|------|------|--------|
| `UPSTREAM_RESERVATION_BASE` | 예약 API 베이스 URL | `http://reservation-api.tickets-api.svc.cluster.local:8080` |
| `UPSTREAM_PAYMENT_BASE` | 결제 API 베이스 URL | `http://payment-sim-api.tickets-api.svc.cluster.local:8080` |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OpenTelemetry 수집기 | `http://otel-collector:4317` |
