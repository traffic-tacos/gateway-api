# 🔧 Gateway API 리팩터링 플랜
## 30k RPS 프로덕션 준비를 위한 아키텍처 개선

**작성일**: 2025-10-05  
**버전**: v2.0.0 Roadmap  
**목적**: 피드백 기반 프로덕션 레디 시스템 구축

---

## 📊 현재 구현 상태 분석

### ✅ 구현 완료 (v1.2.0)

| 컴포넌트 | 상태 | 구현 방식 |
|---|---|---|
| Token Bucket | ✅ | Redis Lua Script |
| Sliding Window ETA | ✅ | 다중 시간대 가중 평균 |
| Redis 대기열 | ✅ | ZSet 기반 |
| 멱등성 키 | ✅ | Redis String (5분 TTL) |
| 기본 레이트 리미팅 | ✅ | IP 기반 |
| Health Check | ✅ | /healthz, /readyz |
| Metrics | ✅ | Prometheus |
| Tracing | ✅ | OpenTelemetry |

### ⚠️ 개선 필요 (피드백 기반)

| 항목 | 현재 | 문제점 | 우선순위 |
|---|---|---|---|
| **순서 보장** | 없음 | 전역 순서 미보장 | P0 |
| **Lua 원자성** | 부분적 | 체크+큐잉+TTL 분리 | P0 |
| **Consistent Hashing** | 없음 | Hot Key 문제 | P1 |
| **백프레셔** | 없음 | 큐 길이 무제한 | P1 |
| **Circuit Breaker** | 없음 | 다운스트림 장애 전파 | P1 |
| **Per-User FIFO** | 없음 | 같은 유저 순서 보장 안됨 | P0 |
| **페일오버 대응** | 약함 | Redis 장애 시 데이터 손실 | P2 |
| **봇 방지** | 없음 | 스팸 공격 취약 | P2 |
| **Hot Shard 탐지** | 없음 | 샤드별 모니터링 없음 | P2 |

---

## 🎯 리팩터링 목표

### Phase 1: 핵심 안정성 (P0) - 2주
**목표**: 순서 보장 + 원자성 + Per-User FIFO

### Phase 2: 확장성 (P1) - 4주
**목표**: Consistent Hashing + 백프레셔 + Circuit Breaker

### Phase 3: 운영 고도화 (P2) - 6주
**목표**: 페일오버 + 보안 + 관측성 강화

---

## 📋 Phase 1: 핵심 안정성 (P0)

### 1.1 Per-User FIFO 순서 보장

#### 현재 문제
```go
// 현재: 전역 ZSet, 순서 보장 안됨
queueKey := fmt.Sprintf("queue:event:%s", eventID)
redis.ZAdd(ctx, queueKey, score, token)
```

**문제점**:
- 동일 유저의 여러 요청이 순서 섞임
- 같은 좌석 중복 홀드 가능
- Redis Cluster 환경에서 샤드 분산

#### 개선 방안: Redis Streams + Hash Tag

**새 구조**:
```go
// Per-User 순서 보장
queueKey := fmt.Sprintf("queue:event:{%s}:user:%s", eventID, userID)
redis.XAdd(ctx, &redis.XAddArgs{
    Stream: queueKey,
    Values: map[string]interface{}{
        "token":     token,
        "event_id":  eventID,
        "timestamp": time.Now().Unix(),
    },
})
```

**장점**:
- `{eventID}` Hash Tag → 같은 이벤트는 같은 샤드
- Per-User Stream → 유저 내 순서 보장
- XREADGROUP → 워커 수평 확장 가능

#### 구현 파일
```
internal/queue/
├── streams.go         # 새로 생성
│   ├── StreamQueue struct
│   ├── EnqueueWithOrder(userID, token)
│   ├── DequeueByUser(userID, count)
│   └── GetUserPosition(userID)
└── migration.go       # ZSet → Streams 마이그레이션
```

#### 마이그레이션 전략
```go
// 단계적 마이그레이션
if featureFlag.IsEnabled("streams_queue") {
    return s.streamsQueue.Enqueue(ctx, userID, token)
} else {
    return s.zsetQueue.Enqueue(ctx, token)
}
```

---

### 1.2 Lua Script 원자성 강화

#### 현재 문제
```go
// 현재: 3번의 별도 Redis 호출
exists := redis.Exists(ctx, dedupeKey)
redis.Set(ctx, dedupeKey, 1, 5*time.Minute)
redis.ZAdd(ctx, queueKey, score, token)
```

**문제점**:
- Race Condition 가능
- 중복 체크와 큐잉 사이 간극
- 부분 실패 시 불일치

#### 개선 방안: 통합 Lua Script

**원자적 큐잉**:
```lua
-- enqueue_atomic.lua
local dedupeKey = KEYS[1]
local queueKey = KEYS[2]
local token = ARGV[1]
local score = ARGV[2]
local ttl = ARGV[3]

-- 1. 중복 체크
if redis.call('EXISTS', dedupeKey) == 1 then
    return {err = 'DUPLICATE'}
end

-- 2. 큐에 추가
redis.call('ZADD', queueKey, score, token)

-- 3. 중복 방지 키 설정
redis.call('SETEX', dedupeKey, ttl, '1')

-- 4. 현재 위치 반환
local rank = redis.call('ZRANK', queueKey, token)
return {ok = rank}
```

**좌석 홀드 원자성**:
```lua
-- hold_seat_atomic.lua
local seatKey = KEYS[1]
local holdKey = KEYS[2]
local inventoryKey = KEYS[3]
local seatID = ARGV[1]
local userID = ARGV[2]
local ttl = ARGV[3]

-- 1. 좌석 상태 확인
local status = redis.call('HGET', seatKey, seatID)
if status ~= 'AVAILABLE' then
    return {err = 'SEAT_UNAVAILABLE'}
end

-- 2. 재고 확인 및 감소
local remaining = redis.call('DECR', inventoryKey)
if remaining < 0 then
    redis.call('INCR', inventoryKey)
    return {err = 'SOLD_OUT'}
end

-- 3. 좌석 홀드
redis.call('HSET', seatKey, seatID, 'HOLD')
redis.call('SETEX', holdKey, ttl, userID)

-- 4. TTL 만료 시 자동 복구 (Keyspace Notification 활용)
return {ok = 'HOLD_SUCCESS'}
```

#### 구현 파일
```
internal/queue/
├── lua/
│   ├── enqueue_atomic.lua
│   ├── hold_seat_atomic.lua
│   ├── release_seat_atomic.lua
│   └── admission_atomic.lua
└── lua_executor.go    # Lua 스크립트 실행 래퍼
```

---

### 1.3 ZSet Score Collision 해결

#### 현재 문제
```go
// 현재: 같은 밀리초에 들어온 요청은 순서 보장 안됨
score := float64(time.Now().UnixMilli())
```

**문제점**:
- 동일 타임스탬프 충돌 시 순서 무작위
- 버스트 트래픽에서 자주 발생

#### 개선 방안: 복합 Score

```go
// 타임스탬프 + 카운터 복합 Score
type CompositeScore struct {
    Timestamp int64
    Counter   uint32
}

func (cs *CompositeScore) ToFloat64() float64 {
    // 상위 40비트: 밀리초 타임스탬프
    // 하위 24비트: 카운터 (16,777,216까지)
    return float64((cs.Timestamp << 24) | int64(cs.Counter))
}

// Redis Lua에서 원자적 카운터 증가
counter := redis.INCR("score:counter:" + eventID)
score := CompositeScore{
    Timestamp: time.Now().UnixMilli(),
    Counter:   counter % (1 << 24),
}.ToFloat64()
```

**구현**:
```
internal/queue/
└── score.go
    ├── CompositeScore struct
    ├── GenerateScore() float64
    └── ParseScore(float64) CompositeScore
```

---

## 📋 Phase 2: 확장성 (P1)

### 2.1 Consistent Hashing 샤딩

#### 현재 문제
```go
// 현재: 단일 큐, Hot Key 문제
queueKey := "queue:event:concert-2025"
```

**문제점**:
- 단일 키에 모든 트래픽 집중
- Redis Cluster 환경에서 병목
- 샤드 분산 불가

#### 개선 방안: Consistent Hashing

```go
// 샤드 수 (설정 가능)
const NumShards = 16

type ConsistentHashRouter struct {
    ring *consistent.Consistent
}

func NewRouter() *ConsistentHashRouter {
    ring := consistent.New()
    for i := 0; i < NumShards; i++ {
        ring.Add(fmt.Sprintf("shard-%d", i))
    }
    return &ConsistentHashRouter{ring: ring}
}

func (r *ConsistentHashRouter) GetShard(userID string) string {
    shard, _ := r.ring.Get(userID)
    return shard
}

// 사용
shard := router.GetShard(userID)
queueKey := fmt.Sprintf("queue:event:{%s}:shard:%s", eventID, shard)
```

**구현**:
```
internal/queue/
├── consistent_hash.go
│   ├── ConsistentHashRouter struct
│   ├── GetShard(userID) string
│   └── Rebalance() error
└── sharded_queue.go
    ├── ShardedQueue struct
    ├── EnqueueWithShard(userID, token)
    └── GetShardMetrics(shard) Metrics
```

#### 모니터링
```prometheus
# 샤드별 큐 길이
gateway_queue_length{shard="0",event_id="concert"}
gateway_queue_length{shard="1",event_id="concert"}

# 샤드별 처리량
gateway_admission_rate{shard="0"}

# Hot Shard 감지
gateway_hot_shard_detected{shard="3"} 1
```

---

### 2.2 백프레셔 (Backpressure)

#### 현재 문제
```go
// 현재: 큐 길이 무제한
redis.ZAdd(ctx, queueKey, score, token)
```

**문제점**:
- 메모리 무한 증가
- ETA 계산 부정확 (큐가 너무 길면)
- 사용자 대기 시간 무한정

#### 개선 방안: 동적 Admission Control

```go
type BackpressureController struct {
    redis       *redis.Client
    maxQueueLen int
    logger      *logrus.Logger
}

func (bc *BackpressureController) CanAdmit(ctx context.Context, eventID string) (bool, string) {
    // 1. 현재 큐 길이 확인
    totalLen := int64(0)
    for shard := 0; shard < NumShards; shard++ {
        queueKey := fmt.Sprintf("queue:event:{%s}:shard:%d", eventID, shard)
        len, _ := bc.redis.ZCard(ctx, queueKey).Result()
        totalLen += len
    }

    // 2. 임계치 초과 시 소프트 드랍
    if totalLen >= int64(bc.maxQueueLen) {
        return false, "QUEUE_FULL"
    }

    // 3. ETA 기반 동적 제한
    eta := bc.calculateGlobalETA(ctx, eventID)
    if eta > 600 { // 10분 이상 대기 시
        return false, "WAIT_TOO_LONG"
    }

    return true, ""
}
```

**구현**:
```
internal/queue/
└── backpressure.go
    ├── BackpressureController struct
    ├── CanAdmit(eventID) (bool, string)
    ├── GetQueueCapacity() int
    └── AdjustCapacity(newCap int)
```

#### HTTP 응답
```json
{
  "error": {
    "code": "QUEUE_FULL",
    "message": "대기열이 가득 찼습니다. 잠시 후 다시 시도해주세요.",
    "retry_after": 30
  }
}
```

---

### 2.3 Circuit Breaker

#### 현재 문제
```go
// 현재: 다운스트림 장애 시 무한 재시도
resp, err := http.Post(reservationAPI, body)
```

**문제점**:
- 백엔드 장애 시 Gateway도 함께 다운
- 불필요한 재시도로 리소스 낭비
- 복구 지연

#### 개선 방안: Hystrix 패턴

```go
import "github.com/sony/gobreaker"

type CircuitBreakerClient struct {
    cb     *gobreaker.CircuitBreaker
    client *http.Client
}

func NewCircuitBreakerClient(name string) *CircuitBreakerClient {
    settings := gobreaker.Settings{
        Name:        name,
        MaxRequests: 3,
        Interval:    10 * time.Second,
        Timeout:     60 * time.Second,
        ReadyToTrip: func(counts gobreaker.Counts) bool {
            failureRatio := float64(counts.TotalFailures) / float64(counts.Requests)
            return counts.Requests >= 10 && failureRatio >= 0.6
        },
        OnStateChange: func(name string, from gobreaker.State, to gobreaker.State) {
            log.Printf("Circuit Breaker '%s': %s -> %s", name, from, to)
        },
    }

    return &CircuitBreakerClient{
        cb:     gobreaker.NewCircuitBreaker(settings),
        client: &http.Client{Timeout: 5 * time.Second},
    }
}

func (cbc *CircuitBreakerClient) Post(url string, body []byte) (*http.Response, error) {
    result, err := cbc.cb.Execute(func() (interface{}, error) {
        return cbc.client.Post(url, "application/json", bytes.NewBuffer(body))
    })

    if err != nil {
        return nil, err
    }

    return result.(*http.Response), nil
}
```

**Fallback 응답**:
```go
func (h *ReservationHandler) CreateReservation(c *fiber.Ctx) error {
    resp, err := h.client.Post(reservationAPI, body)
    
    if err != nil {
        // Circuit Open 시 Fallback
        if errors.Is(err, gobreaker.ErrOpenState) {
            return c.Status(503).JSON(fiber.Map{
                "error": "SERVICE_UNAVAILABLE",
                "message": "서비스가 일시적으로 불안정합니다. 잠시 후 다시 시도해주세요.",
            })
        }
        return err
    }
    
    return c.JSON(resp)
}
```

**구현**:
```
internal/clients/
├── circuit_breaker.go
│   ├── CircuitBreakerClient struct
│   ├── Post(url, body) (Response, error)
│   └── Get(url) (Response, error)
└── reservation_breaker.go  # 기존 클라이언트 래핑
```

---

## 📋 Phase 3: 운영 고도화 (P2)

### 3.1 Redis 페일오버 대응

#### 현재 문제
```go
// 현재: 페일오버 시 데이터 손실 가능
redis.ZAdd(ctx, queueKey, score, token)
```

**문제점**:
- ElastiCache 페일오버 중 쓰기 실패
- 복제본 전환 시 최근 데이터 손실
- 순서 흐트러짐

#### 개선 방안: Dual Write + Reconciliation

```go
type ResilientQueue struct {
    primary   *redis.Client
    secondary *redis.Client // DynamoDB Streams
    logger    *logrus.Logger
}

func (rq *ResilientQueue) Enqueue(ctx context.Context, token string) error {
    // 1. Primary (Redis) 쓰기
    err := rq.primary.ZAdd(ctx, queueKey, score, token).Err()
    
    // 2. Secondary (DynamoDB) 백업 (비동기)
    go func() {
        rq.writeToBackup(token, score)
    }()
    
    // 3. Redis 실패 시 Secondary로 Fallback
    if err != nil {
        log.Warn("Redis write failed, using backup")
        return rq.writeToBackup(token, score)
    }
    
    return nil
}

// 페일오버 후 Reconciliation
func (rq *ResilientQueue) ReconcileAfterFailover(ctx context.Context) error {
    // DynamoDB에서 Redis로 복구
    items := rq.fetchFromBackup()
    for _, item := range items {
        rq.primary.ZAdd(ctx, queueKey, item.Score, item.Token)
    }
    return nil
}
```

**구현**:
```
internal/queue/
├── resilient_queue.go
│   ├── ResilientQueue struct
│   ├── Enqueue(token) error
│   ├── ReconcileAfterFailover() error
│   └── HealthCheck() (bool, error)
└── backup/
    ├── dynamodb_backup.go
    └── reconciler.go
```

---

### 3.2 봇 방지 (Anti-Bot)

#### 현재 문제
```go
// 현재: 봇 검증 없음
redis.ZAdd(ctx, queueKey, score, token)
```

**문제점**:
- 스크립트 공격 취약
- 대량 자동 예약 가능
- 정상 사용자 피해

#### 개선 방안: 다층 방어

**Layer 1: CAPTCHA**
```go
func (h *QueueHandler) Join(c *fiber.Ctx) error {
    req := new(JoinRequest)
    c.BodyParser(req)
    
    // reCAPTCHA v3 검증
    score := verifyCaptcha(req.CaptchaToken)
    if score < 0.5 {
        return c.Status(403).JSON(fiber.Map{
            "error": "CAPTCHA_FAILED",
        })
    }
    
    // 정상 처리
    return h.enqueue(req)
}
```

**Layer 2: Rate Limiting by IP**
```go
// IP별 제한 강화
ipLimiter := middleware.NewIPRateLimiter(5, time.Minute)
app.Use(ipLimiter.Limit())
```

**Layer 3: Behavioral Analysis**
```go
type BehaviorAnalyzer struct {
    redis *redis.Client
}

func (ba *BehaviorAnalyzer) IsBot(userID, ip string) (bool, float64) {
    // 1. 요청 패턴 분석
    pattern := ba.getRequestPattern(userID)
    
    // 2. 의심 점수 계산
    score := 0.0
    
    // 너무 빠른 요청 (< 1초)
    if pattern.AvgInterval < 1 {
        score += 0.3
    }
    
    // 동일 IP에서 다수 세션
    sessionsCount := ba.getSessionsByIP(ip)
    if sessionsCount > 10 {
        score += 0.4
    }
    
    // User-Agent 패턴
    if pattern.HasBotUserAgent {
        score += 0.3
    }
    
    return score > 0.6, score
}
```

**구현**:
```
internal/security/
├── captcha.go
│   └── VerifyCaptcha(token) float64
├── ip_limiter.go
│   └── IPRateLimiter struct
└── behavior.go
    ├── BehaviorAnalyzer struct
    ├── IsBot(userID, ip) (bool, float64)
    └── RecordActivity(userID, action)
```

---

### 3.3 Hot Shard 탐지 및 자동 재조정

#### 현재 문제
```go
// 현재: 샤드별 부하 모니터링 없음
```

**문제점**:
- 특정 샤드 과부하 감지 불가
- 수동 재조정 필요
- 성능 저하

#### 개선 방안: 자동 감지 및 재조정

```go
type HotShardDetector struct {
    metrics map[string]*ShardMetrics
    logger  *logrus.Logger
}

type ShardMetrics struct {
    QueueLength int64
    RequestRate float64
    AvgLatency  time.Duration
}

func (hsd *HotShardDetector) DetectHotShards(threshold float64) []string {
    hotShards := []string{}
    
    avgRate := hsd.calculateAverageRate()
    
    for shard, metrics := range hsd.metrics {
        if metrics.RequestRate > avgRate*threshold {
            hotShards = append(hotShards, shard)
            hsd.logger.WithFields(logrus.Fields{
                "shard":        shard,
                "rate":         metrics.RequestRate,
                "avg_rate":     avgRate,
                "queue_length": metrics.QueueLength,
            }).Warn("Hot shard detected")
        }
    }
    
    return hotShards
}

func (hsd *HotShardDetector) Rebalance(hotShard string) error {
    // 1. Hot Shard의 일부 유저를 다른 샤드로 이동
    users := hsd.getUsersInShard(hotShard)
    
    // 2. Cold Shard 찾기
    coldShard := hsd.findColdestShard()
    
    // 3. 이동 (새 요청부터 적용)
    for _, user := range users[:len(users)/2] {
        hsd.router.UpdateUserShard(user, coldShard)
    }
    
    return nil
}
```

**구현**:
```
internal/queue/
└── hot_shard.go
    ├── HotShardDetector struct
    ├── DetectHotShards(threshold) []string
    ├── Rebalance(shard) error
    └── StartMonitoring(interval time.Duration)
```

---

## 📊 새로운 메트릭 및 알림

### Prometheus 메트릭 추가

```go
// Phase 1 메트릭
gateway_queue_order_violations_total{event_id}
gateway_lua_script_duration_seconds{script}
gateway_dedupe_hits_total{result}

// Phase 2 메트릭
gateway_shard_queue_length{shard, event_id}
gateway_shard_request_rate{shard}
gateway_backpressure_rejections_total{reason}
gateway_circuit_breaker_state{service, state}

// Phase 3 메트릭
gateway_redis_failover_events_total{}
gateway_bot_detected_total{reason}
gateway_hot_shard_rebalance_total{}
```

### Grafana 알림 규칙

```yaml
# Hot Shard 알림
- alert: HotShardDetected
  expr: gateway_shard_request_rate > 2 * avg(gateway_shard_request_rate)
  for: 2m
  labels:
    severity: warning
  annotations:
    summary: "Hot shard detected on {{ $labels.shard }}"

# Circuit Breaker Open
- alert: CircuitBreakerOpen
  expr: gateway_circuit_breaker_state{state="open"} == 1
  for: 1m
  labels:
    severity: critical
  annotations:
    summary: "Circuit breaker open for {{ $labels.service }}"

# Queue Full
- alert: QueueFull
  expr: sum(gateway_shard_queue_length) > 100000
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "Queue length exceeds threshold"
```

---

## 🧪 테스트 전략

### Unit Tests

```go
// Phase 1
TestPerUserFIFO()
TestLuaScriptAtomicity()
TestCompositeScore()

// Phase 2
TestConsistentHashing()
TestBackpressureThreshold()
TestCircuitBreakerTrip()

// Phase 3
TestRedisFailoverRecovery()
TestBotDetection()
TestHotShardRebalance()
```

### Integration Tests

```bash
# 부하 테스트 (k6)
k6 run --vus 3000 --duration 10m \
    --rps 30000 \
    tests/load/queue_test.js

# 순서 보장 테스트
k6 run tests/integration/order_test.js

# 페일오버 테스트
./tests/chaos/redis_failover.sh
```

### Chaos Engineering

```bash
# Redis 장애 시뮬레이션
kubectl delete pod redis-master -n tickets-cache

# 네트워크 지연 주입
tc qdisc add dev eth0 root netem delay 500ms

# CPU 스로틀링
stress-ng --cpu 4 --timeout 60s
```

---

## 📅 타임라인 및 마일스톤

### Phase 1: 핵심 안정성 (2주)

**Week 1**:
- [ ] Redis Streams 구현
- [ ] Lua Script 통합
- [ ] CompositeScore 구현
- [ ] Unit Tests

**Week 2**:
- [ ] Per-User FIFO 통합
- [ ] 마이그레이션 스크립트
- [ ] Integration Tests
- [ ] v2.0.0-alpha 배포

### Phase 2: 확장성 (4주)

**Week 3-4**:
- [ ] Consistent Hashing 구현
- [ ] 샤드별 모니터링
- [ ] Backpressure 구현

**Week 5-6**:
- [ ] Circuit Breaker 통합
- [ ] Fallback 로직
- [ ] v2.0.0-beta 배포
- [ ] 부하 테스트 (30k RPS)

### Phase 3: 운영 고도화 (6주)

**Week 7-9**:
- [ ] Resilient Queue 구현
- [ ] DynamoDB 백업
- [ ] Reconciliation 로직

**Week 10-12**:
- [ ] 봇 방지 구현
- [ ] Hot Shard 탐지
- [ ] Grafana 대시보드
- [ ] v2.0.0-rc 배포

**Week 13**:
- [ ] 프로덕션 배포
- [ ] 모니터링
- [ ] v2.0.0 GA 🎉

---

## 💰 예상 비용 및 리소스

### 인프라 추가 비용

```
현재 (v1.2.0):
  Redis: $200/월 (cache.r6g.large)
  EKS: $600/월 (4 nodes)
  Total: $800/월

Phase 1 추가:
  Redis Streams: +0 (기존 노드)
  Total: $800/월

Phase 2 추가:
  Redis Cluster: +$300/월 (3 shards)
  Total: $1,100/월

Phase 3 추가:
  DynamoDB Backup: +$50/월
  Total: $1,150/월

최종 비용: $1,150/월 (44% 증가)
```

### 개발 리소스

```
Phase 1: 2명 × 2주 = 4 person-weeks
Phase 2: 2명 × 4주 = 8 person-weeks
Phase 3: 2명 × 6주 = 12 person-weeks

Total: 24 person-weeks (6 person-months)
```

---

## 🎯 성공 기준

### Phase 1 기준
- [ ] Per-User 순서 보장 100% (동일 유저 요청)
- [ ] Lua Script 원자성 검증 (Race Condition 0건)
- [ ] CompositeScore 충돌 < 0.001%

### Phase 2 기준
- [ ] Hot Shard 발생률 < 5%
- [ ] Backpressure 발동 시 메모리 안정화
- [ ] Circuit Breaker 오픈 시 복구 < 1분

### Phase 3 기준
- [ ] Redis 페일오버 시 데이터 손실 < 0.1%
- [ ] 봇 탐지율 > 90%
- [ ] Hot Shard 자동 재조정 < 30초

### 최종 목표 (v2.0.0)
- [ ] 30k RPS 10분 지속 처리
- [ ] P95 Latency < 20ms
- [ ] 에러율 < 0.5%
- [ ] 순서 보장 100% (Per-User)
- [ ] 페일오버 자동 복구

---

## 📝 체크리스트 (피드백 반영)

### A. 트래픽 제어 ✅
- [x] 전역 Token Bucket (v1.1.0)
- [x] Sliding Window Counter (v1.2.0)
- [ ] Consistent Hashing (Phase 2)

### B. 대기열/순서/중복 ⚠️
- [ ] Per-User FIFO (Phase 1) ⭐
- [ ] Redis Streams (Phase 1) ⭐
- [x] Idempotency Key (v1.0.0)
- [ ] Lua 원자 처리 (Phase 1) ⭐

### C. 좌석 재고/홀드 🔲
- [ ] 원자적 홀드 (Phase 1)
- [ ] TTL 자동 복구 (Phase 1)
- [ ] 오버셀 방지 검증 (Phase 1)

### D. 안정성/장애 대응 🔲
- [ ] Redis 페일오버 대응 (Phase 3)
- [ ] Circuit Breaker (Phase 2) ⭐
- [ ] 지수 백오프 재시도 (Phase 2)

### E. 관측성/SLO ✅
- [x] P50/P90/P99 메트릭 (v1.0.0)
- [x] 큐 길이 대시보드 (v1.1.0)
- [ ] 샤드별 모니터링 (Phase 2)
- [ ] Hot Shard 알림 (Phase 3)

### F. 보안/남용 방지 🔲
- [ ] CAPTCHA (Phase 3)
- [ ] 봇 탐지 (Phase 3)
- [ ] IP 기반 제한 강화 (Phase 2)

### G. 비용/용량 ✅
- [x] HPA 자동 확장 (v1.0.0)
- [ ] k6 부하 테스트 (Phase 2)
- [ ] 비용 최적화 분석 (Phase 3)

---

## 🚀 Quick Start (개발자용)

### Phase 1 개발 시작

```bash
# 1. Feature Branch 생성
git checkout -b feature/phase1-per-user-fifo

# 2. Redis Streams 의존성 추가
go get github.com/redis/go-redis/v9

# 3. 새 파일 생성
touch internal/queue/streams.go
touch internal/queue/lua_executor.go
touch internal/queue/score.go

# 4. 테스트 작성
touch internal/queue/streams_test.go

# 5. 개발 시작
code internal/queue/streams.go
```

### 로컬 테스트

```bash
# Redis Streams 테스트
docker run -d -p 6379:6379 redis:7-alpine

# 단위 테스트
go test ./internal/queue/... -v

# 통합 테스트
./tests/integration/run_phase1.sh
```

---

**작성일**: 2025-10-05  
**버전**: v2.0.0 Roadmap  
**상태**: 📝 리팩터링 플랜 확정  
**다음 단계**: Phase 1 개발 시작
