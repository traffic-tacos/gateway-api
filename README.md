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

### 전제 조건

- Go 1.22 이상
- Redis (로컬 또는 Docker)

### 실행

```bash
# 의존성 설치
go mod download

# 애플리케이션 실행
go run cmd/gateway/main.go
```

### API 엔드포인트

| 엔드포인트 | 메서드 | 설명 |
|------------|--------|------|
| `/healthz` | GET | 헬스 체크 |
| `/readyz` | GET | readiness 체크 |
| `/version` | GET | 버전 정보 |
| `/metrics` | GET | Prometheus 메트릭 |
| `/api/v1/queue/join` | POST | 대기열 참여 |
| `/api/v1/reservations` | POST | 예약 생성 |
| `/api/v1/payment/intent` | POST | 결제 인텐트 생성 |

## 환경 변수

| 변수 | 설명 | 기본값 |
|------|------|--------|
| `PORT` | 서버 포트 | `8080` |
| `JWT_ISSUER` | JWT 발급자 | - |
| `REDIS_ADDR` | Redis 주소 | `redis:6379` |
| `RATE_LIMIT_RPS` | 초당 요청 제한 | `50` |
| `LOG_LEVEL` | 로그 레벨 | `info` |
