# Traffic Tacos gRPC 통신 개발 플래닝

## 📋 현재 상황 분석

### ✅ 완료된 작업
1. **프로젝트 아키텍처 분석**
   - HTTP/REST 기반 Gateway API 구조 파악
   - Queue, Reservation, Payment 서비스 통신 패턴 분석
   - 기존 HTTP 클라이언트 구조 이해

2. **Proto Contracts 준비**
   - GitHub 레포: `github.com/traffic-tacos/proto-contracts`
   - 퍼블릭 레포로 변경 완료
   - Proto 파일들 확인 (reservation, payment, common, gateway)

3. **환경 설정**
   - Redis 로컬 설치 및 구동 ✅
   - AWS ElastiCache 연결 설정 ✅
   - buf 도구 설치 ✅

### ⚠️ 현재 이슈
- Proto contracts Go 모듈에서 생성된 Go 파일들이 아직 푸시되지 않음
- go.mod에서 proto contracts 패키지를 찾을 수 없음

## 🚀 gRPC 통신 개발 전략

### Phase 1: Proto Contracts 안정화
```bash
# Proto contracts 레포에서 작업 필요
cd proto-contracts/
make generate  # Go 파일 생성
git add gen/go/
git commit -m "Add generated Go files"
git tag v0.1.0
git push origin main --tags
```

### Phase 2: Gateway API에서 gRPC 클라이언트 구현

#### 2.1 디렉토리 구조
```
internal/
├── grpc/
│   ├── clients/
│   │   ├── reservation.go    # ReservationService 클라이언트
│   │   ├── payment.go        # PaymentService 클라이언트
│   │   └── manager.go        # 연결 관리
│   ├── server/
│   │   ├── queue.go          # QueueService 구현
│   │   ├── gateway.go        # GatewayService 구현
│   │   └── server.go         # gRPC 서버 설정
│   └── interceptors/
│       ├── auth.go           # JWT 인증
│       ├── metrics.go        # Prometheus 메트릭
│       └── logging.go        # 로깅
```

#### 2.2 환경 변수 추가
```bash
# .env.local에 추가
GRPC_ENABLED=true
GRPC_PORT=9090
GRPC_USE_TLS=false

# Backend 서비스 gRPC 주소
BACKEND_RESERVATION_GRPC_ADDRESS=localhost:9090
BACKEND_PAYMENT_GRPC_ADDRESS=localhost:9091
```

### Phase 3: Dual Protocol 지원 (HTTP + gRPC)

#### 3.1 서비스 인터페이스 추상화
```go
// internal/services/interfaces.go
type ReservationService interface {
    CreateReservation(ctx context.Context, req *CreateReservationRequest) (*ReservationResponse, error)
    GetReservation(ctx context.Context, id string) (*ReservationResponse, error)
    ConfirmReservation(ctx context.Context, id string, req *ConfirmReservationRequest) (*ConfirmReservationResponse, error)
    CancelReservation(ctx context.Context, id string) error
}

type PaymentService interface {
    CreatePaymentIntent(ctx context.Context, req *CreatePaymentIntentRequest) (*PaymentIntentResponse, error)
    GetPaymentStatus(ctx context.Context, id string) (*PaymentStatusResponse, error)
    ProcessPayment(ctx context.Context, req *ProcessPaymentRequest) (*ProcessPaymentResponse, error)
}
```

#### 3.2 클라이언트 팩토리 패턴
```go
// internal/clients/factory.go
type ClientConfig struct {
    UseGRPC bool
    HTTPConfig *HTTPClientConfig
    GRPCConfig *GRPCClientConfig
}

func NewReservationClient(cfg *ClientConfig) ReservationService {
    if cfg.UseGRPC {
        return NewReservationGRPCClient(cfg.GRPCConfig)
    }
    return NewReservationHTTPClient(cfg.HTTPConfig)
}
```

### Phase 4: gRPC 서버 구현

#### 4.1 Queue Service (신규)
```go
// internal/grpc/server/queue.go
type queueServer struct {
    gatewaypb.UnimplementedQueueServiceServer
    redisClient *redis.Client
    logger      *logrus.Logger
}

func (s *queueServer) JoinQueue(ctx context.Context, req *gatewaypb.JoinQueueRequest) (*gatewaypb.JoinQueueResponse, error) {
    // 기존 HTTP 핸들러 로직을 gRPC로 이식
}
```

#### 4.2 Gateway Service (신규)
```go
// internal/grpc/server/gateway.go
type gatewayServer struct {
    gatewaypb.UnimplementedGatewayServiceServer
    reservationClient ReservationService
    paymentClient     PaymentService
}
```

### Phase 5: 미들웨어 및 인터셉터

#### 5.1 gRPC 인터셉터
```go
// internal/grpc/interceptors/auth.go
func JWTAuthInterceptor(jwtConfig *config.JWTConfig) grpc.UnaryServerInterceptor {
    return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
        // JWT 토큰 검증 로직
    }
}

// internal/grpc/interceptors/metrics.go
func MetricsInterceptor() grpc.UnaryServerInterceptor {
    return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
        // Prometheus 메트릭 수집
    }
}
```

#### 5.2 메인 서버 통합
```go
// cmd/gateway/main.go
func main() {
    // HTTP 서버 (기존)
    go func() {
        app := fiber.New()
        routes.Setup(app, cfg, logger, middlewareManager)
        app.Listen(":" + cfg.Server.Port)
    }()

    // gRPC 서버 (신규)
    if cfg.GRPC.Enabled {
        go func() {
            grpcServer := grpc.NewServer(
                grpc.UnaryInterceptor(grpc_middleware.ChainUnaryServer(
                    interceptors.JWTAuthInterceptor(cfg.JWT),
                    interceptors.MetricsInterceptor(),
                    interceptors.LoggingInterceptor(logger),
                )),
            )
            // 서비스 등록
            gatewaypb.RegisterQueueServiceServer(grpcServer, queueService)
            gatewaypb.RegisterGatewayServiceServer(grpcServer, gatewayService)

            lis, _ := net.Listen("tcp", ":"+cfg.GRPC.Port)
            grpcServer.Serve(lis)
        }()
    }
}
```

## 🔄 마이그레이션 전략

### 단계별 전환 계획

#### 1단계: 백엔드 통신 gRPC 전환
- Reservation API ↔ Gateway API (gRPC)
- Payment API ↔ Gateway API (gRPC)
- HTTP API는 그대로 유지

#### 2단계: 클라이언트 통신 gRPC 추가
- React Frontend → Gateway API (HTTP + WebSocket 유지)
- Mobile Apps → Gateway API (gRPC 지원 추가)

#### 3단계: 성능 최적화
- 연결 풀링 최적화
- 스트리밍 API 도입 (대기열 상태 실시간 업데이트)
- Circuit breaker 패턴 적용

#### 4단계: 점진적 트래픽 전환
- Feature flag로 gRPC/HTTP 비율 조정
- A/B 테스트로 성능 비교
- 모니터링 기반 롤백 준비

## 📊 기대 효과

### 성능 향상
- **지연시간**: 20-30% 감소 (HTTP/2 기반)
- **처리량**: 30-50% 증가 (Binary 프로토콜)
- **연결 효율성**: Multiplexing으로 연결 수 감소

### 개발 생산성
- **타입 안전성**: Proto buffer 스키마 검증
- **API 일관성**: 중앙화된 contract 관리
- **도구 지원**: buf, Evans 등 gRPC 도구 활용

### 운영 효율성
- **스키마 진화**: Backward/Forward compatibility
- **관찰성**: gRPC 메트릭, 트레이싱 표준화
- **오류 처리**: Structured error codes

## 🛠️ 다음 단계

### 즉시 작업 필요
1. ✅ Proto contracts 레포에서 Go 파일 생성 및 푸시
2. ⏳ Gateway API에서 gRPC 클라이언트 구현
3. ⏳ 간단한 health check gRPC 서비스 구현

### 단기 목표 (1-2주)
1. Reservation/Payment gRPC 클라이언트 완성
2. Queue gRPC 서버 구현
3. HTTP/gRPC dual protocol 지원

### 중기 목표 (1개월)
1. 모든 서비스 간 gRPC 통신 완료
2. 성능 테스트 및 최적화
3. 프로덕션 배포 준비

## 🔧 개발 명령어

```bash
# 개발 환경 실행
./run_local.sh

# gRPC 서버 포함 실행 (향후)
GRPC_ENABLED=true ./run_local.sh

# Proto 변경 시 재생성 (proto-contracts 레포에서)
make generate

# gRPC 서비스 테스트 (Evans CLI)
evans --host localhost --port 9090 --package gateway.v1 --service QueueService
```

이 계획을 단계별로 실행하여 30k RPS 목표를 달성할 수 있는 고성능 gRPC 통신 아키텍처를 구축할 예정입니다.