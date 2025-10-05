# ElastiCache Redis 기반 대기열 시스템 워크플로우

## 🎯 시스템 개요

Traffic Tacos 플랫폼의 대기열 시스템은 **AWS ElastiCache Redis**를 사용하여 30k RPS 트래픽을 효과적으로 제어합니다.

**핵심 특징:**
- ✅ Redis Sorted Set (ZSet) 기반 순위 관리
- ✅ O(log N) 복잡도로 실시간 위치 조회
- ✅ TLS 암호화 및 AUTH 토큰 보안
- ✅ 30분 TTL로 자동 정리

---

## 🏗️ Redis 데이터 구조

### 1. 개별 대기 정보 (String - JSON)

```redis
Key: queue:waiting:{waiting_token}
Type: String (JSON)
TTL: 30분 (1800초)

Example:
queue:waiting:73bf3da3-04bb-47e2-9455-03bf5455ab81
```

**Value 구조:**
```json
{
  "event_id": "evt_2025_1001",
  "user_id": "user123",
  "joined_at": "2024-01-01T12:00:00Z",
  "position": 4,
  "status": "waiting"
}
```

### 2. 이벤트별 대기열 (Sorted Set)

```redis
Key: queue:event:{event_id}
Type: ZSet
Score: Unix Timestamp (참여 시간)
Member: waiting_token

Example:
queue:event:evt_2025_1001
```

**ZSet 구조:**
```
Score (Unix Time)    Member (Waiting Token)
─────────────────────────────────────────────────────
1704103200           73bf3da3-04bb-47e2-9455-03bf5455ab81
1704103205           a1b2c3d4-5678-9012-3456-789012345678
1704103210           e5f6g7h8-9012-3456-7890-123456789012
```

**Score가 작을수록 먼저 참여 → 앞 순서**

### 3. 입장 허가 토큰 (String - JSON)

```redis
Key: queue:reservation:{reservation_token}
Type: String (JSON)
TTL: 30초

Example:
queue:reservation:abc123-def456-ghi789
```

**Value 구조:**
```json
{
  "event_id": "evt_2025_1001",
  "user_id": "user123",
  "waiting_token": "73bf3da3-04bb-...",
  "granted_at": "2024-01-01T12:05:00Z"
}
```

---

## 🔄 전체 워크플로우

### 시각적 다이어그램

```
┌─────────────────────────────────────────────────────────────────┐
│                 사용자 (Browser/Mobile)                         │
│                 https://traffictacos.store                       │
└────────────┬────────────────────────────────────────────────────┘
             │
             │ 1. POST /api/v1/queue/join
             │    {"event_id": "evt_2025_1001"}
             ▼
┌─────────────────────────────────────────────────────────────────┐
│                    Gateway API (Go/Fiber)                       │
│                 Port 8000 (K8s Service)                          │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │ QueueHandler.Join()                                       │  │
│  │  ┌────────────────────────────────────────────────────┐  │  │
│  │  │ 1. UUID 생성: waiting_token = uuid.New()         │  │  │
│  │  │ 2. Redis ZCARD: 현재 대기 인원 확인              │  │  │
│  │  │    → position = count + 1                         │  │  │
│  │  │ 3. Redis ZADD: 이벤트 큐에 추가                  │  │  │
│  │  │    → queue:event:{event_id}                       │  │  │
│  │  │    → Score: time.Now().Unix()                     │  │  │
│  │  │ 4. Redis SET: 대기 정보 저장 (TTL: 30분)        │  │  │
│  │  │    → queue:waiting:{waiting_token}                │  │  │
│  │  └────────────────────────────────────────────────────┘  │  │
│  └──────────────────────────────────────────────────────────┘  │
└────────────┬────────────────────────────────────────────────────┘
             │
             │ 202 Accepted
             │ {
             │   "waiting_token": "73bf3da3-...",
             │   "position_hint": 4,
             │   "status": "waiting"
             │ }
             ▼
┌─────────────────────────────────────────────────────────────────┐
│              AWS ElastiCache Redis Cluster                      │
│        master.traffic-tacos-redis.w6eqga.apn2.cache...         │
│                    (Multi-AZ, TLS Enabled)                      │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │ ZSet: queue:event:evt_2025_1001                          │  │
│  │                                                          │  │
│  │   Score        Member                                   │  │
│  │   ─────────────────────────────────────────────────     │  │
│  │   1704103200 → "token-aaa..."                          │  │
│  │   1704103205 → "token-bbb..."                          │  │
│  │   1704103210 → "73bf3da3-04bb-..." ← 현재 유저        │  │
│  │                                                          │  │
│  └──────────────────────────────────────────────────────────┘  │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │ String: queue:waiting:73bf3da3-04bb-...                  │  │
│  │                                                          │  │
│  │   {"event_id":"evt_2025_1001",                          │  │
│  │    "position":3,                                        │  │
│  │    "status":"waiting",                                  │  │
│  │    "joined_at":"2024-01-01T12:00:00Z"}                 │  │
│  │                                                          │  │
│  │   TTL: 1795초 (30분 - 5초)                             │  │
│  └──────────────────────────────────────────────────────────┘  │
└────────────┬────────────────────────────────────────────────────┘
             │
             │ 2. GET /api/v1/queue/status?token=...
             │    (폴링: 3-5초마다 반복)
             ▼
┌─────────────────────────────────────────────────────────────────┐
│                       Gateway API                               │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │ QueueHandler.Status()                                     │  │
│  │  ┌────────────────────────────────────────────────────┐  │  │
│  │  │ 1. Redis GET: 대기 정보 조회                      │  │  │
│  │  │    → queue:waiting:{waiting_token}                 │  │  │
│  │  │ 2. Redis ZRANK: 현재 순위 계산 ← 🔧 버그 수정!   │  │  │
│  │  │    → ZRANK queue:event:{event_id} {waiting_token} │  │  │
│  │  │    → rank = 2 (0-based index)                      │  │  │
│  │  │ 3. ETA 계산: position * 2초                        │  │  │
│  │  │    → eta = 3 * 2 = 6초                             │  │  │
│  │  │ 4. waiting_time: 경과 시간 계산                   │  │  │
│  │  │    → time.Since(joined_at).Seconds()               │  │  │
│  │  └────────────────────────────────────────────────────┘  │  │
│  └──────────────────────────────────────────────────────────┘  │
└────────────┬────────────────────────────────────────────────────┘
             │
             │ 200 OK
             │ {
             │   "status": "waiting",
             │   "position": 3,      ← 실시간 업데이트!
             │   "eta_sec": 6,       ← position * 2초
             │   "waiting_time": 34  ← 경과 시간
             │ }
             ▼
┌─────────────────────────────────────────────────────────────────┐
│                    사용자 화면 업데이트                         │
│                                                                 │
│   ┌─────────────────────────────────────────────────────────┐ │
│   │  대기열 현황                                            │ │
│   │  ─────────────────────────────────────────────────────  │ │
│   │  현재 순서: 3번째                                       │ │
│   │  예상 대기 시간: 6초                                    │ │
│   │  대기 중: 34초                                          │ │
│   │                                                         │ │
│   │  [●●●○○○○○○○] 30% 완료                               │ │
│   └─────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────┘
             │
             │ (10초 이상 대기 후)
             │ 3. POST /api/v1/queue/enter
             │    {"waiting_token": "73bf3da3-..."}
             ▼
┌─────────────────────────────────────────────────────────────────┐
│                       Gateway API                               │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │ QueueHandler.Enter()                                      │  │
│  │  ┌────────────────────────────────────────────────────┐  │  │
│  │  │ 1. Redis GET: 대기 정보 검증                      │  │  │
│  │  │ 2. isEligibleForEntry(): 입장 가능 여부 확인     │  │  │
│  │  │    → waitTime > 10초 체크                         │  │  │
│  │  │ 3. UUID 생성: reservation_token                   │  │  │
│  │  │ 4. Redis SET: 입장 토큰 저장 (TTL: 30초)        │  │  │
│  │  │    → queue:reservation:{reservation_token}        │  │  │
│  │  │ 5. Redis SET: 상태 업데이트 (waiting → ready)   │  │  │
│  │  └────────────────────────────────────────────────────┘  │  │
│  └──────────────────────────────────────────────────────────┘  │
└────────────┬────────────────────────────────────────────────────┘
             │
             │ 200 OK
             │ {
             │   "admission": "granted",
             │   "reservation_token": "abc123-...",
             │   "ttl_sec": 30
             │ }
             ▼
┌─────────────────────────────────────────────────────────────────┐
│                    예약 가능 상태                               │
│                                                                 │
│   ⚠️  30초 내에 POST /api/v1/reservations 호출 필요!          │
│                                                                 │
│   다음 단계: Reservation API로 실제 예약 생성                  │
└─────────────────────────────────────────────────────────────────┘
```

---

## 📊 Redis 명령어 상세 흐름

### 단계 1: Join (대기열 참여)

```go
// Go 코드
waitingToken := uuid.New().String()
eventQueueKey := fmt.Sprintf("queue:event:%s", eventID)

// 1. 현재 대기 인원 확인
count, _ := redisClient.ZCard(ctx, eventQueueKey).Result()
position := int(count) + 1

// 2. 대기열에 추가
redisClient.ZAdd(ctx, eventQueueKey, redis.Z{
    Score:  float64(time.Now().Unix()),
    Member: waitingToken,
})

// 3. 대기 정보 저장
queueData := QueueData{...}
redisClient.Set(ctx, 
    fmt.Sprintf("queue:waiting:%s", waitingToken),
    json.Marshal(queueData),
    30*time.Minute)
```

**Redis 명령어:**
```redis
# 1. 현재 대기 인원 확인
ZCARD queue:event:evt_2025_1001
>>> 2

# 2. 대기열에 추가 (Score = Unix timestamp)
ZADD queue:event:evt_2025_1001 1704103210 "73bf3da3-04bb-47e2-9455-03bf5455ab81"
>>> 1  # 추가된 멤버 수

# 3. 대기 정보 저장 (TTL: 30분 = 1800초)
SETEX queue:waiting:73bf3da3-04bb-47e2-9455-03bf5455ab81 1800 '{"event_id":"evt_2025_1001","user_id":"user123","joined_at":"2024-01-01T12:00:00Z","position":3,"status":"waiting"}'
>>> OK
```

### 단계 2: Status (상태 조회) - 🔧 수정된 로직

```go
// Go 코드 (수정 후)
queueData, _ := getQueueData(ctx, waitingToken)

// 🔧 수정: waiting_token을 직접 전달
rank, err := redisClient.ZRank(ctx, eventQueueKey, waitingToken).Result()
position := int(rank) + 1
eta := position * 2
waitingTime := int(time.Since(queueData.JoinedAt).Seconds())
```

**Redis 명령어:**
```redis
# 1. 대기 정보 조회
GET queue:waiting:73bf3da3-04bb-47e2-9455-03bf5455ab81
>>> '{"event_id":"evt_2025_1001","position":3,...}'

# 2. 실시간 순위 조회 (0-based index)
ZRANK queue:event:evt_2025_1001 "73bf3da3-04bb-47e2-9455-03bf5455ab81"
>>> 2  # 0부터 시작하므로 3번째

# 3. 응답 계산
# position = rank + 1 = 2 + 1 = 3
# eta = position * 2 = 3 * 2 = 6초
```

**Before (버그):**
```redis
# ❌ 잘못된 멤버로 조회 (항상 실패)
ZRANK queue:event:evt_2025_1001 "queue:waiting:evt_2025_1001"
>>> (nil)  # 존재하지 않는 멤버

# 결과: 항상 기본값(초기 position) 반환
```

**After (수정):**
```redis
# ✅ 실제 waiting_token으로 조회
ZRANK queue:event:evt_2025_1001 "73bf3da3-04bb-47e2-9455-03bf5455ab81"
>>> 2  # 실제 순위 반환!
```

### 단계 3: Enter (입장 허가)

```go
// Go 코드
reservationToken := uuid.New().String()

// 입장 가능 여부 확인
if time.Since(queueData.JoinedAt) < 10*time.Second {
    return error("NOT_READY")
}

// 입장 토큰 생성
redisClient.Set(ctx,
    fmt.Sprintf("queue:reservation:%s", reservationToken),
    json.Marshal(reservationData),
    30*time.Second)

// 상태 업데이트
queueData.Status = "ready"
redisClient.Set(ctx,
    fmt.Sprintf("queue:waiting:%s", waitingToken),
    json.Marshal(queueData),
    30*time.Minute)
```

**Redis 명령어:**
```redis
# 1. 입장 토큰 생성 (TTL: 30초)
SETEX queue:reservation:abc123-def456-ghi789 30 '{"event_id":"evt_2025_1001","user_id":"user123","waiting_token":"73bf3da3-...","granted_at":"2024-01-01T12:05:00Z"}'
>>> OK

# 2. 상태 업데이트 (waiting → ready)
SETEX queue:waiting:73bf3da3-04bb-47e2-9455-03bf5455ab81 1800 '{"status":"ready",...}'
>>> OK
```

### 단계 4: Leave (대기열 이탈)

```go
// Go 코드
redisClient.Del(ctx, fmt.Sprintf("queue:waiting:%s", waitingToken))
redisClient.ZRem(ctx, eventQueueKey, waitingToken)
```

**Redis 명령어:**
```redis
# 1. 대기 정보 삭제
DEL queue:waiting:73bf3da3-04bb-47e2-9455-03bf5455ab81
>>> 1  # 삭제된 키 수

# 2. 이벤트 큐에서 제거
ZREM queue:event:evt_2025_1001 "73bf3da3-04bb-47e2-9455-03bf5455ab81"
>>> 1  # 제거된 멤버 수
```

---

## 🔍 실시간 모니터링

### Redis CLI로 큐 상태 확인

```bash
# ElastiCache Redis 접속 (TLS 활성화)
redis-cli -h master.traffic-tacos-redis.w6eqga.apn2.cache.amazonaws.com \
  -p 6379 \
  --tls \
  --cacert /path/to/ca-cert.pem \
  -a YOUR_AUTH_TOKEN

# 또는 Bastion 호스트를 통한 접속
ssh -L 6379:master.traffic-tacos-redis.w6eqga.apn2.cache.amazonaws.com:6379 bastion-host
redis-cli -p 6379
```

### 유용한 Redis 명령어

```redis
# 1. 이벤트별 대기 인원 확인
ZCARD queue:event:evt_2025_1001
>>> 47  # 현재 47명 대기 중

# 2. 대기열 전체 조회 (시간순, 최근 10명)
ZRANGE queue:event:evt_2025_1001 0 9 WITHSCORES
>>> 1) "73bf3da3-04bb-..."
>>> 2) "1704103210"
>>> 3) "a1b2c3d4-5678-..."
>>> 4) "1704103215"
>>> ...

# 3. 특정 토큰의 순위 확인 (0-based)
ZRANK queue:event:evt_2025_1001 "73bf3da3-04bb-47e2-9455-03bf5455ab81"
>>> 2  # 3번째 순서

# 4. 특정 토큰의 Score 확인 (참여 시간)
ZSCORE queue:event:evt_2025_1001 "73bf3da3-04bb-47e2-9455-03bf5455ab81"
>>> "1704103210"

# 5. 대기 정보 조회
GET queue:waiting:73bf3da3-04bb-47e2-9455-03bf5455ab81
>>> '{"event_id":"evt_2025_1001","position":3,...}'

# 6. TTL 확인
TTL queue:waiting:73bf3da3-04bb-47e2-9455-03bf5455ab81
>>> 1795  # 남은 시간 (초)

# 7. 입장 토큰 확인
GET queue:reservation:abc123-def456-ghi789
>>> '{"event_id":"evt_2025_1001",...}'

TTL queue:reservation:abc123-def456-ghi789
>>> 25  # 30초 중 25초 남음
```

### Kubernetes에서 Redis 모니터링

```bash
# Gateway API Pod에서 Redis 연결 확인
kubectl exec -it gateway-api-779c8f886b-w97bz -n tacos-app -- \
  /bin/sh -c 'echo "PING" | nc -w 1 master.traffic-tacos-redis.w6eqga.apn2.cache.amazonaws.com 6379'

# Gateway API 로그에서 Redis 연결 확인
kubectl logs -f deployment/gateway-api -n tacos-app | grep -i redis
```

---

## 📈 성능 특성

### ElastiCache Redis ZSet의 장점

1. **O(log N) 순위 조회**
   - ZRANK 명령어는 매우 빠름 (밀리초 단위)
   - 수만 명의 대기열도 즉시 처리

2. **자동 정렬**
   - Score(Unix timestamp) 기반 자동 정렬
   - 먼저 참여한 사람이 자동으로 앞 순서

3. **원자적 연산**
   - ZADD, ZREM, ZRANK 모두 원자적 (Atomic)
   - 동시 접속에도 안전

4. **확장성**
   - Multi-AZ 복제로 고가용성
   - 읽기 복제본으로 읽기 성능 확장 가능

### 예상 성능 지표

| 지표 | 값 | 설명 |
|---|---|---|
| **ZADD 지연시간** | < 1ms | 대기열 추가 |
| **ZRANK 지연시간** | < 1ms | 순위 조회 |
| **GET 지연시간** | < 1ms | 대기 정보 조회 |
| **최대 대기 인원** | ~100만 | ZSet 멤버 제한 |
| **동시 요청 처리** | 10k+ RPS | ElastiCache 성능 |

### 실제 측정 (예시)

```bash
# Redis 성능 벤치마크
redis-benchmark -h master.traffic-tacos-redis.w6eqga.apn2.cache.amazonaws.com \
  -p 6379 --tls \
  -t zadd,zrank,get \
  -n 100000 \
  -c 50

# 결과 예시:
# ZADD: 94339.62 requests per second
# ZRANK: 97656.25 requests per second
# GET: 102040.82 requests per second
```

---

## 🎯 현재 구현의 한계와 개선 방안

### 1. 입장 조건 (isEligibleForEntry)

**현재 구현 (단순화):**
```go
func (q *QueueHandler) isEligibleForEntry(ctx context.Context, queueData *QueueData) bool {
    // 단순히 10초 이상 대기만 체크
    waitTime := time.Since(queueData.JoinedAt)
    return waitTime > 10*time.Second
}
```

**문제점:**
- ❌ 실제 순위를 무시함
- ❌ 10초만 기다리면 누구나 입장 가능
- ❌ Admission Rate 제어 불가

**개선 방안 (프로덕션):**
```go
func (q *QueueHandler) isEligibleForEntry(ctx context.Context, queueData *QueueData, waitingToken string) bool {
    eventQueueKey := fmt.Sprintf("queue:event:%s", queueData.EventID)
    
    // 실제 큐 위치 확인
    rank, err := q.redisClient.ZRank(ctx, eventQueueKey, waitingToken).Result()
    if err != nil {
        return false
    }
    
    // 상위 100명만 입장 허용 (Admission Rate Control)
    if int(rank) >= 100 {
        return false
    }
    
    // 최소 5초는 대기해야 함
    waitTime := time.Since(queueData.JoinedAt)
    if waitTime < 5*time.Second {
        return false
    }
    
    // 초당 입장 인원 제한 (Leaky Bucket)
    admissionKey := fmt.Sprintf("admission:rate:%s", queueData.EventID)
    currentCount, _ := q.redisClient.Incr(ctx, admissionKey).Result()
    if currentCount == 1 {
        q.redisClient.Expire(ctx, admissionKey, 1*time.Second)
    }
    
    // 초당 최대 50명만 입장
    return currentCount <= 50
}
```

### 2. ETA 계산

**현재 구현 (단순화):**
```go
eta := position * 2  // 2초 per person
```

**문제점:**
- ❌ 너무 단순함
- ❌ 실제 처리 속도 반영 안됨
- ❌ 트래픽 변화에 적응 못함

**개선 방안 (프로덕션):**
```go
func (q *QueueHandler) calculateSmartETA(ctx context.Context, eventID string, position int) int {
    // 과거 5분간 처리 속도 측정
    statsKey := fmt.Sprintf("queue:stats:%s", eventID)
    
    // Redis Time Series 또는 별도 메트릭 스토어 사용
    recentAdmissions, _ := q.getRecentAdmissionRate(ctx, statsKey)
    
    if recentAdmissions == 0 {
        return position * 2  // Fallback
    }
    
    // 초당 처리 인원 기반 ETA 계산
    eta := float64(position) / recentAdmissions
    
    // 최소 1초, 최대 600초 제한
    if eta < 1 {
        eta = 1
    } else if eta > 600 {
        eta = 600
    }
    
    return int(eta)
}
```

### 3. Admission Worker 구현 (현재 없음)

**필요한 이유:**
- 대기열을 자동으로 진행시키기
- 백엔드 부하에 따라 입장 속도 조절
- 만료된 토큰 정리

**구현 예시 (Go Worker):**
```go
func (w *AdmissionWorker) Run(ctx context.Context) {
    ticker := time.NewTicker(1 * time.Second)
    defer ticker.Stop()
    
    for {
        select {
        case <-ticker.C:
            w.processAdmissions(ctx)
        case <-ctx.Done():
            return
        }
    }
}

func (w *AdmissionWorker) processAdmissions(ctx context.Context) {
    // 이벤트 목록 조회
    events := w.getActiveEvents(ctx)
    
    for _, eventID := range events {
        eventQueueKey := fmt.Sprintf("queue:event:%s", eventID)
        
        // 상위 50명의 상태를 "ready"로 변경
        tokens, _ := w.redisClient.ZRange(ctx, eventQueueKey, 0, 49).Result()
        
        for _, token := range tokens {
            queueKey := fmt.Sprintf("queue:waiting:%s", token)
            // 상태 업데이트 로직
            w.updateQueueStatus(ctx, queueKey, "ready")
        }
    }
}
```

---

## 🧪 테스트 시나리오

### 시나리오 1: 정상 흐름

```bash
# 1. 대기열 참여
curl -X POST https://api.traffictacos.store/api/v1/queue/join \
  -H "Content-Type: application/json" \
  -d '{"event_id": "evt_2025_1001"}'

# 응답: {"waiting_token": "73bf3da3-...", "position_hint": 4, "status": "waiting"}

# 2. 상태 조회 (3-5초마다 폴링)
curl "https://api.traffictacos.store/api/v1/queue/status?token=73bf3da3-..."

# 응답 1: {"status":"waiting","position":4,"eta_sec":8,"waiting_time":2}
# 응답 2: {"status":"waiting","position":3,"eta_sec":6,"waiting_time":7}
# 응답 3: {"status":"waiting","position":2,"eta_sec":4,"waiting_time":12}

# 3. 입장 요청 (10초 대기 후)
curl -X POST https://api.traffictacos.store/api/v1/queue/enter \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer dev-super-key-local-testing" \
  -H "Idempotency-Key: $(uuidgen | tr '[:upper:]' '[:lower:]')" \
  -d '{"waiting_token": "73bf3da3-..."}'

# 응답: {"admission":"granted","reservation_token":"abc123-...","ttl_sec":30}

# 4. 예약 생성 (30초 내)
curl -X POST https://api.traffictacos.store/api/v1/reservations \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer abc123-..." \
  -d '{"event_id": "evt_2025_1001", "seat_ids": ["A-1"]}'
```

### 시나리오 2: 너무 빨리 입장 시도

```bash
# 1. 대기열 참여
curl -X POST https://api.traffictacos.store/api/v1/queue/join \
  -d '{"event_id": "evt_2025_1001"}'

# 2. 즉시 입장 시도 (5초 대기)
sleep 5
curl -X POST https://api.traffictacos.store/api/v1/queue/enter \
  -H "Authorization: Bearer dev-super-key-local-testing" \
  -d '{"waiting_token": "73bf3da3-..."}'

# 응답: 403 Forbidden
# {"error": {"code": "NOT_READY", "message": "Your turn has not arrived yet"}}
```

### 시나리오 3: 대기열 이탈

```bash
# 1. 대기열 참여
curl -X POST https://api.traffictacos.store/api/v1/queue/join \
  -d '{"event_id": "evt_2025_1001"}'

# 2. 대기열 이탈
curl -X DELETE "https://api.traffictacos.store/api/v1/queue/leave?token=73bf3da3-..."

# 응답: {"status": "left"}

# 3. 상태 조회 시도
curl "https://api.traffictacos.store/api/v1/queue/status?token=73bf3da3-..."

# 응답: 404 Not Found
# {"error": {"code": "TOKEN_NOT_FOUND", "message": "Waiting token not found or expired"}}
```

---

## 🔒 보안 고려사항

### 1. TLS 암호화

```go
// ElastiCache in-transit encryption
if cfg.TLSEnabled {
    options.TLSConfig = &tls.Config{
        ServerName: extractHostname(cfg.Address),
    }
}
```

### 2. AUTH 토큰

```bash
# AWS Secrets Manager에서 자동 로드
export REDIS_PASSWORD_FROM_SECRETS=true
export AWS_SECRET_NAME="traffic-tacos/redis/auth-token"
```

### 3. 네트워크 격리

- ElastiCache는 VPC 내부에만 접근 가능
- Gateway API Pod만 보안 그룹으로 접근 허용
- 외부 직접 접근 차단

### 4. TTL 기반 자동 정리

- 대기 정보: 30분 후 자동 삭제
- 입장 토큰: 30초 후 자동 삭제
- 메모리 누수 방지

---

## 📝 요약

### ✅ 구현 완료

- [x] Redis ZSet 기반 대기열 구조
- [x] TLS 암호화 ElastiCache 연결
- [x] Join, Status, Enter, Leave API
- [x] TTL 기반 자동 정리
- [x] 실시간 position 업데이트 (버그 수정)

### 🔧 개선 필요

- [ ] Admission Worker 구현
- [ ] 스마트 ETA 계산 (과거 데이터 기반)
- [ ] 실제 큐 위치 기반 입장 제어
- [ ] Admission Rate Limiting (Leaky Bucket)
- [ ] Redis Cluster Mode 지원
- [ ] 메트릭 및 대시보드

### 🚀 다음 단계

1. 수정된 코드 배포
2. 실제 동작 검증
3. 성능 모니터링
4. 추가 개선 사항 구현

---

**문서 작성일**: 2025-10-05  
**버전**: 1.0  
**작성자**: Traffic Tacos Team
