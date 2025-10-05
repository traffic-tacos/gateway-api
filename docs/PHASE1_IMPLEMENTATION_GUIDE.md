# 🚀 Phase 1 구현 가이드
## Per-User FIFO + Lua 원자성 + Composite Score

**우선순위**: P0 (긴급)  
**기간**: 2주  
**목표**: 순서 보장 + 원자성 확보

---

## 📋 구현 순서

### Day 1-2: Composite Score

#### 목표
동일 타임스탬프 충돌 해결 → FIFO 보장

#### 파일 생성
```bash
touch internal/queue/score.go
touch internal/queue/score_test.go
```

#### 구현 코드
```go
// internal/queue/score.go
package queue

import (
    "sync/atomic"
    "time"
)

var globalCounter uint32

// CompositeScore combines timestamp and counter for unique ordering
type CompositeScore struct {
    Timestamp int64  // Milliseconds since epoch
    Counter   uint32 // Atomic counter
}

// GenerateScore creates a new composite score
func GenerateScore() *CompositeScore {
    counter := atomic.AddUint32(&globalCounter, 1)
    
    return &CompositeScore{
        Timestamp: time.Now().UnixMilli(),
        Counter:   counter % (1 << 24), // 24-bit counter
    }
}

// ToFloat64 converts to Redis ZSet score
// Format: [40-bit timestamp][24-bit counter]
func (cs *CompositeScore) ToFloat64() float64 {
    // Shift timestamp left by 24 bits, OR with counter
    composite := (cs.Timestamp << 24) | int64(cs.Counter)
    return float64(composite)
}

// FromFloat64 parses a Redis score back to CompositeScore
func FromFloat64(score float64) *CompositeScore {
    composite := int64(score)
    
    return &CompositeScore{
        Timestamp: composite >> 24,
        Counter:   uint32(composite & 0xFFFFFF),
    }
}

// Compare returns -1, 0, or 1 if cs is less than, equal to, or greater than other
func (cs *CompositeScore) Compare(other *CompositeScore) int {
    if cs.Timestamp < other.Timestamp {
        return -1
    } else if cs.Timestamp > other.Timestamp {
        return 1
    }
    
    // Same timestamp, compare counters
    if cs.Counter < other.Counter {
        return -1
    } else if cs.Counter > other.Counter {
        return 1
    }
    
    return 0
}
```

#### 테스트 코드
```go
// internal/queue/score_test.go
package queue

import (
    "testing"
    "time"
)

func TestCompositeScore_ToFloat64(t *testing.T) {
    cs := &CompositeScore{
        Timestamp: 1728123456789, // Oct 2024
        Counter:   12345,
    }
    
    score := cs.ToFloat64()
    parsed := FromFloat64(score)
    
    if parsed.Timestamp != cs.Timestamp {
        t.Errorf("Timestamp mismatch: got %d, want %d", parsed.Timestamp, cs.Timestamp)
    }
    
    if parsed.Counter != cs.Counter {
        t.Errorf("Counter mismatch: got %d, want %d", parsed.Counter, cs.Counter)
    }
}

func TestGenerateScore_Uniqueness(t *testing.T) {
    const iterations = 10000
    scores := make(map[float64]bool)
    
    for i := 0; i < iterations; i++ {
        score := GenerateScore().ToFloat64()
        
        if scores[score] {
            t.Fatalf("Duplicate score detected: %f", score)
        }
        
        scores[score] = true
    }
    
    t.Logf("Generated %d unique scores", len(scores))
}

func TestCompositeScore_Ordering(t *testing.T) {
    cs1 := GenerateScore()
    time.Sleep(1 * time.Millisecond)
    cs2 := GenerateScore()
    
    if cs1.Compare(cs2) != -1 {
        t.Error("Expected cs1 < cs2")
    }
    
    if cs2.Compare(cs1) != 1 {
        t.Error("Expected cs2 > cs1")
    }
}

func BenchmarkGenerateScore(b *testing.B) {
    for i := 0; i < b.N; i++ {
        GenerateScore()
    }
}
```

#### 기존 코드 통합
```go
// internal/routes/queue.go
import "github.com/traffic-tacos/gateway-api/internal/queue"

// Before
score := float64(time.Now().UnixMilli())

// After
score := queue.GenerateScore().ToFloat64()
```

---

### Day 3-5: Lua Script 원자성

#### 목표
체크 + 큐잉 + TTL을 원자적으로 처리

#### 파일 생성
```bash
mkdir -p internal/queue/lua
touch internal/queue/lua/enqueue_atomic.lua
touch internal/queue/lua/hold_seat_atomic.lua
touch internal/queue/lua_executor.go
touch internal/queue/lua_executor_test.go
```

#### Lua Script: 원자적 큐잉
```lua
-- internal/queue/lua/enqueue_atomic.lua
-- KEYS[1]: dedupe key (e.g., "dedupe:abc123")
-- KEYS[2]: queue key (e.g., "queue:event:concert")
-- ARGV[1]: token
-- ARGV[2]: score (composite)
-- ARGV[3]: ttl (seconds)

-- 1. Check for duplicates
if redis.call('EXISTS', KEYS[1]) == 1 then
    return {err = 'DUPLICATE'}
end

-- 2. Add to queue
redis.call('ZADD', KEYS[2], ARGV[2], ARGV[1])

-- 3. Set dedupe key with TTL
redis.call('SETEX', KEYS[1], ARGV[3], '1')

-- 4. Get position
local rank = redis.call('ZRANK', KEYS[2], ARGV[1])

return {ok = rank}
```

#### Lua Script: 원자적 좌석 홀드
```lua
-- internal/queue/lua/hold_seat_atomic.lua
-- KEYS[1]: seat status key (e.g., "seat:status:concert")
-- KEYS[2]: hold key (e.g., "hold:seat:A-12")
-- KEYS[3]: inventory key (e.g., "inventory:concert")
-- ARGV[1]: seat ID
-- ARGV[2]: user ID
-- ARGV[3]: ttl (seconds)

-- 1. Check seat availability
local status = redis.call('HGET', KEYS[1], ARGV[1])
if status ~= 'AVAILABLE' and status ~= false then
    return {err = 'SEAT_UNAVAILABLE'}
end

-- 2. Decrement inventory
local remaining = redis.call('DECR', KEYS[3])
if remaining < 0 then
    redis.call('INCR', KEYS[3])
    return {err = 'SOLD_OUT'}
end

-- 3. Mark seat as HOLD
redis.call('HSET', KEYS[1], ARGV[1], 'HOLD')
redis.call('SETEX', KEYS[2], ARGV[3], ARGV[2])

return {ok = 'HOLD_SUCCESS', remaining = remaining}
```

#### Go Executor
```go
// internal/queue/lua_executor.go
package queue

import (
    "context"
    _ "embed"
    "fmt"
    
    "github.com/redis/go-redis/v9"
    "github.com/sirupsen/logrus"
)

//go:embed lua/enqueue_atomic.lua
var enqueueAtomicScript string

//go:embed lua/hold_seat_atomic.lua
var holdSeatAtomicScript string

type LuaExecutor struct {
    redis *redis.Client
    
    // Preloaded scripts
    enqueueScript *redis.Script
    holdScript    *redis.Script
    
    logger *logrus.Logger
}

func NewLuaExecutor(redis *redis.Client, logger *logrus.Logger) *LuaExecutor {
    return &LuaExecutor{
        redis:         redis,
        enqueueScript: redis.NewScript(enqueueAtomicScript),
        holdScript:    redis.NewScript(holdSeatAtomicScript),
        logger:        logger,
    }
}

// EnqueueAtomic performs atomic enqueue with dedupe check
func (le *LuaExecutor) EnqueueAtomic(
    ctx context.Context,
    dedupeKey string,
    queueKey string,
    token string,
    score float64,
    ttl int,
) (int64, error) {
    result, err := le.enqueueScript.Run(
        ctx,
        le.redis,
        []string{dedupeKey, queueKey},
        token, score, ttl,
    ).Result()
    
    if err != nil {
        return -1, fmt.Errorf("lua script failed: %w", err)
    }
    
    // Parse result
    resultMap, ok := result.(map[interface{}]interface{})
    if !ok {
        return -1, fmt.Errorf("unexpected result type")
    }
    
    // Check for errors
    if errMsg, exists := resultMap["err"]; exists {
        return -1, fmt.Errorf("%v", errMsg)
    }
    
    // Get position
    rank, ok := resultMap["ok"].(int64)
    if !ok {
        return -1, fmt.Errorf("invalid rank")
    }
    
    le.logger.WithFields(logrus.Fields{
        "token":    token,
        "position": rank + 1,
    }).Debug("Atomic enqueue successful")
    
    return rank, nil
}

// HoldSeatAtomic performs atomic seat reservation
func (le *LuaExecutor) HoldSeatAtomic(
    ctx context.Context,
    seatStatusKey string,
    holdKey string,
    inventoryKey string,
    seatID string,
    userID string,
    ttl int,
) (int64, error) {
    result, err := le.holdScript.Run(
        ctx,
        le.redis,
        []string{seatStatusKey, holdKey, inventoryKey},
        seatID, userID, ttl,
    ).Result()
    
    if err != nil {
        return -1, fmt.Errorf("lua script failed: %w", err)
    }
    
    resultMap, ok := result.(map[interface{}]interface{})
    if !ok {
        return -1, fmt.Errorf("unexpected result type")
    }
    
    if errMsg, exists := resultMap["err"]; exists {
        return -1, fmt.Errorf("%v", errMsg)
    }
    
    remaining, ok := resultMap["remaining"].(int64)
    if !ok {
        return -1, fmt.Errorf("invalid remaining count")
    }
    
    le.logger.WithFields(logrus.Fields{
        "seat_id":   seatID,
        "user_id":   userID,
        "remaining": remaining,
    }).Info("Seat hold successful")
    
    return remaining, nil
}
```

#### 기존 코드 통합
```go
// internal/routes/queue.go

// Before (3 separate calls)
exists := redis.Exists(ctx, dedupeKey)
redis.Set(ctx, dedupeKey, 1, 5*time.Minute)
redis.ZAdd(ctx, queueKey, score, token)

// After (1 atomic call)
luaExec := queue.NewLuaExecutor(redisClient, logger)
rank, err := luaExec.EnqueueAtomic(
    ctx,
    dedupeKey,
    queueKey,
    token,
    score,
    300, // 5 minutes
)

if err != nil {
    if strings.Contains(err.Error(), "DUPLICATE") {
        return fiber.NewError(409, "중복된 요청입니다")
    }
    return err
}

position := rank + 1
```

---

### Day 6-8: Redis Streams (Per-User FIFO)

#### 목표
같은 유저의 요청은 순서 보장

#### 파일 생성
```bash
touch internal/queue/streams.go
touch internal/queue/streams_test.go
```

#### 구현 코드
```go
// internal/queue/streams.go
package queue

import (
    "context"
    "fmt"
    "time"
    
    "github.com/redis/go-redis/v9"
    "github.com/sirupsen/logrus"
)

type StreamQueue struct {
    redis  *redis.Client
    logger *logrus.Logger
}

func NewStreamQueue(redis *redis.Client, logger *logrus.Logger) *StreamQueue {
    return &StreamQueue{
        redis:  redis,
        logger: logger,
    }
}

// EnqueueWithOrder adds to per-user stream
func (sq *StreamQueue) EnqueueWithOrder(
    ctx context.Context,
    eventID string,
    userID string,
    token string,
) (string, error) {
    // Use hash tag for same-event clustering
    streamKey := fmt.Sprintf("stream:event:{%s}:user:%s", eventID, userID)
    
    id, err := sq.redis.XAdd(ctx, &redis.XAddArgs{
        Stream: streamKey,
        Values: map[string]interface{}{
            "token":     token,
            "event_id":  eventID,
            "user_id":   userID,
            "timestamp": time.Now().Unix(),
        },
    }).Result()
    
    if err != nil {
        return "", fmt.Errorf("xadd failed: %w", err)
    }
    
    sq.logger.WithFields(logrus.Fields{
        "stream_id": id,
        "user_id":   userID,
    }).Debug("Added to stream")
    
    return id, nil
}

// DequeueByUser reads from user's stream
func (sq *StreamQueue) DequeueByUser(
    ctx context.Context,
    eventID string,
    userID string,
    count int64,
) ([]redis.XMessage, error) {
    streamKey := fmt.Sprintf("stream:event:{%s}:user:%s", eventID, userID)
    
    messages, err := sq.redis.XRange(ctx, streamKey, "-", "+").Result()
    if err != nil {
        return nil, fmt.Errorf("xrange failed: %w", err)
    }
    
    if int64(len(messages)) > count {
        messages = messages[:count]
    }
    
    return messages, nil
}

// GetUserPosition returns position in global queue
func (sq *StreamQueue) GetUserPosition(
    ctx context.Context,
    eventID string,
    userID string,
) (int, error) {
    // Aggregate all user streams
    pattern := fmt.Sprintf("stream:event:{%s}:user:*", eventID)
    
    keys, err := sq.redis.Keys(ctx, pattern).Result()
    if err != nil {
        return -1, err
    }
    
    totalAhead := 0
    userStreamKey := fmt.Sprintf("stream:event:{%s}:user:%s", eventID, userID)
    
    for _, key := range keys {
        if key == userStreamKey {
            break
        }
        
        length, _ := sq.redis.XLen(ctx, key).Result()
        totalAhead += int(length)
    }
    
    return totalAhead + 1, nil
}

// Cleanup removes processed entries
func (sq *StreamQueue) Cleanup(
    ctx context.Context,
    eventID string,
    userID string,
    messageID string,
) error {
    streamKey := fmt.Sprintf("stream:event:{%s}:user:%s", eventID, userID)
    
    return sq.redis.XDel(ctx, streamKey, messageID).Err()
}
```

---

### Day 9-10: Feature Flag 및 마이그레이션

#### 목표
안전한 점진적 마이그레이션

#### Feature Flag 구현
```go
// internal/config/config.go

type FeatureFlags struct {
    UseStreamsQueue  bool `env:"FEATURE_STREAMS_QUEUE" default:"false"`
    UseLuaAtomic     bool `env:"FEATURE_LUA_ATOMIC" default:"false"`
    UseCompositeScore bool `env:"FEATURE_COMPOSITE_SCORE" default:"false"`
}

func (c *Config) IsStreamsEnabled() bool {
    return c.FeatureFlags.UseStreamsQueue
}
```

#### 점진적 적용
```go
// internal/routes/queue.go

func (q *QueueHandler) Join(c *fiber.Ctx) error {
    req := new(JoinQueueRequest)
    c.BodyParser(req)
    
    // Feature flag check
    if q.config.IsStreamsEnabled() {
        return q.joinWithStreams(c, req)
    } else {
        return q.joinWithZSet(c, req)
    }
}

func (q *QueueHandler) joinWithStreams(c *fiber.Ctx, req *JoinQueueRequest) error {
    // New Streams-based logic
    streamQueue := queue.NewStreamQueue(q.redisClient, q.logger)
    
    streamID, err := streamQueue.EnqueueWithOrder(
        c.Context(),
        req.EventID,
        req.UserID,
        waitingToken,
    )
    
    // ...
}

func (q *QueueHandler) joinWithZSet(c *fiber.Ctx, req *JoinQueueRequest) error {
    // Legacy ZSet logic (keep for rollback)
    // ...
}
```

---

### Day 11-12: 통합 테스트

#### Integration Test
```go
// tests/integration/phase1_test.go
package integration

import (
    "context"
    "testing"
    "time"
    
    "github.com/stretchr/testify/assert"
)

func TestPerUserFIFO(t *testing.T) {
    ctx := context.Background()
    
    // 동일 유저의 3개 요청
    tokens := []string{}
    for i := 0; i < 3; i++ {
        resp := callJoinAPI(t, "user123", "concert-2025")
        tokens = append(tokens, resp.Token)
        time.Sleep(10 * time.Millisecond)
    }
    
    // 순서 검증
    for i, token := range tokens {
        position := getPosition(t, token)
        expectedPosition := i + 1
        
        assert.Equal(t, expectedPosition, position,
            "Token %d should be at position %d, got %d",
            i, expectedPosition, position)
    }
}

func TestLuaAtomicity(t *testing.T) {
    ctx := context.Background()
    
    // 동일 idempotency key로 100번 동시 요청
    idempotencyKey := "test-key-123"
    
    successCount := 0
    duplicateCount := 0
    
    for i := 0; i < 100; i++ {
        go func() {
            resp := callJoinAPIWithKey(t, idempotencyKey)
            if resp.StatusCode == 202 {
                successCount++
            } else if resp.StatusCode == 409 {
                duplicateCount++
            }
        }()
    }
    
    time.Sleep(2 * time.Second)
    
    // 정확히 1번만 성공해야 함
    assert.Equal(t, 1, successCount, "Should succeed exactly once")
    assert.Equal(t, 99, duplicateCount, "Should reject 99 duplicates")
}
```

---

## 📊 검증 기준

### Composite Score
- [ ] 10,000번 생성 시 충돌 0건
- [ ] 벤치마크: < 100ns per operation
- [ ] 순서 보장: cs1 < cs2 (시간순)

### Lua Script
- [ ] 원자성: Race condition 0건 (100 concurrent)
- [ ] 중복 방지: 동일 키 99% 거부
- [ ] 성능: P95 < 3ms

### Streams
- [ ] Per-User 순서: 100% 보장
- [ ] 전역 위치: ±5% 오차
- [ ] 처리량: > 10k enqueue/sec

---

## 🚀 배포 계획

### Alpha 배포 (Day 13)
```bash
# Feature Flag 비활성화 상태로 배포
FEATURE_COMPOSITE_SCORE=false \
FEATURE_LUA_ATOMIC=false \
FEATURE_STREAMS_QUEUE=false \
kubectl set image deployment/gateway-api ...
```

### Beta 배포 (Day 14)
```bash
# 10% 트래픽에만 적용
FEATURE_COMPOSITE_SCORE=true \
FEATURE_LUA_ATOMIC=true \
FEATURE_STREAMS_QUEUE=false \  # 아직 비활성화
kubectl set image deployment/gateway-api ...
```

### GA 배포 (Day 15+)
```bash
# 전체 활성화
FEATURE_COMPOSITE_SCORE=true \
FEATURE_LUA_ATOMIC=true \
FEATURE_STREAMS_QUEUE=true \
kubectl set image deployment/gateway-api ...
```

---

**작성일**: 2025-10-05  
**상태**: 📝 구현 준비 완료  
**다음 단계**: Composite Score 구현 시작
