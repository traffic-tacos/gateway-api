# 🔄 Phase 1 수정된 계획

## ⚠️ Composite Score 포기

**이유**: Float64 정밀도 한계로 실용성 없음  
**대안**: Redis Streams로 직접 전환

---

## 📋 수정된 Phase 1 계획

### Day 1-5: Redis Streams 구현 (핵심) ⭐

#### 목표
Per-User FIFO 순서 보장 + 100% 정확도

#### 구현 파일
```
internal/queue/
├── streams.go         # Redis Streams 구현
├── streams_test.go    # 통합 테스트
└── migration.go       # ZSet → Streams 마이그레이션
```

#### 핵심 코드
```go
// internal/queue/streams.go
type StreamQueue struct {
    redis  *redis.Client
    logger *logrus.Logger
}

func (sq *StreamQueue) Enqueue(
    ctx context.Context,
    eventID string,
    userID string,
    token string,
) (string, int, error) {
    // Per-user stream with hash tag
    streamKey := fmt.Sprintf("stream:event:{%s}:user:%s", eventID, userID)
    
    // XAdd with auto-generated ID
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
        return "", -1, err
    }
    
    // Calculate global position
    position := sq.GetGlobalPosition(ctx, eventID, userID, id)
    
    return id, position, nil
}

func (sq *StreamQueue) GetGlobalPosition(
    ctx context.Context,
    eventID string,
    userID string,
    streamID string,
) int {
    // Aggregate all user streams for this event
    pattern := fmt.Sprintf("stream:event:{%s}:user:*", eventID)
    
    keys, _ := sq.redis.Keys(ctx, pattern).Result()
    
    totalAhead := 0
    userStreamKey := fmt.Sprintf("stream:event:{%s}:user:%s", eventID, userID)
    
    for _, key := range keys {
        if key == userStreamKey {
            // Count entries before our ID in user's stream
            entries, _ := sq.redis.XRange(ctx, key, "-", streamID).Result()
            totalAhead += len(entries) - 1
            break
        }
        
        // Count all entries in streams ahead of us
        length, _ := sq.redis.XLen(ctx, key).Result()
        totalAhead += int(length)
    }
    
    return totalAhead + 1
}
```

---

### Day 6-8: Lua Script 원자성

#### 기존 계획 유지
- 중복 체크 + Streams 추가 + TTL 설정을 원자적으로

```lua
-- enqueue_atomic_streams.lua
local dedupeKey = KEYS[1]
local streamKey = KEYS[2]
local token = ARGV[1]
local eventID = ARGV[2]
local userID = ARGV[3]
local ttl = ARGV[4]

-- 1. Dedupe check
if redis.call('EXISTS', dedupeKey) == 1 then
    return {err = 'DUPLICATE'}
end

-- 2. Add to stream
local streamID = redis.call('XADD', streamKey, '*', 
    'token', token,
    'event_id', eventID,
    'user_id', userID,
    'timestamp', redis.call('TIME')[1])

-- 3. Set dedupe TTL
redis.call('SETEX', dedupeKey, ttl, '1')

return {ok = streamID}
```

---

### Day 9-10: Feature Flag 및 마이그레이션

#### 변경 없음
```go
if config.IsStreamsEnabled() {
    return joinWithStreams(req)
} else {
    return joinWithZSet(req)  // Fallback
}
```

---

### Day 11-14: 통합 테스트 및 검증

#### 핵심 테스트
```go
func TestStreams_PerUserFIFO(t *testing.T) {
    // 동일 유저의 3개 요청
    for i := 0; i < 3; i++ {
        streamID, pos, _ := streamQueue.Enqueue(
            ctx, "event1", "user123", fmt.Sprintf("token%d", i),
        )
        
        assert.Equal(t, i+1, pos)
    }
    
    // 순서 검증
    entries, _ := redis.XRange(ctx, streamKey, "-", "+").Result()
    assert.Len(t, entries, 3)
    
    for i, entry := range entries {
        expectedToken := fmt.Sprintf("token%d", i)
        actualToken := entry.Values["token"].(string)
        assert.Equal(t, expectedToken, actualToken)
    }
}
```

---

## 🎯 성공 기준 (수정)

| 항목 | 기준 | 검증 방법 |
|---|---|---|
| Per-User 순서 | 100% | 통합 테스트 |
| 전역 Position | ±10% | 실시간 계산 |
| 처리량 | > 5k enqueue/sec | 부하 테스트 |
| Lua 원자성 | Race Condition 0건 | 동시성 테스트 |

---

## 📅 수정된 타임라인

```
Day 1-2:   Streams 구조 설계 및 기본 구현
Day 3-4:   Global Position 계산 최적화
Day 5:     Streams 통합 테스트
Day 6-7:   Lua Script 원자성
Day 8:     Lua Script 테스트
Day 9-10:  Feature Flag 및 마이그레이션
Day 11-12: E2E 통합 테스트
Day 13:    성능 튜닝
Day 14:    배포 준비
```

---

## ✅ 체크리스트

### Phase 1 (수정)
- [x] Composite Score 포기 결정
- [x] Streams 기반 설계 확정
- [ ] Streams 구현 시작
- [ ] Per-User FIFO 검증
- [ ] Lua Script 원자성
- [ ] Feature Flag 적용
- [ ] 통합 테스트
- [ ] 프로덕션 배포

---

**작성일**: 2025-10-05  
**상태**: 계획 수정 완료  
**다음 단계**: Streams 구현 시작
