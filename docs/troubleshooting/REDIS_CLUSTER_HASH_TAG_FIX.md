# Redis Cluster Hash Tag 문제 해결 가이드

## 📋 목차
- [문제 개요](#문제-개요)
- [증상 및 에러](#증상-및-에러)
- [기존 코드 분석](#기존-코드-분석)
- [근본 원인](#근본-원인)
- [해결 방법](#해결-방법)
- [기술적 배경](#기술적-배경)
- [추가 고려사항](#추가-고려사항)

---

## 문제 개요

**발생 시점**: 2025-10-07  
**영향 범위**: `POST /api/v1/queue/join` API 전체 실패  
**환경**: ElastiCache Redis Cluster Mode Enabled (3 shards, 6 nodes)

### 타임라인
1. **ElastiCache Standalone → Cluster Mode 마이그레이션**
   - Configuration Endpoint: `clustercfg.traffic-tacos-redis.w6eqga.apn2.cache.amazonaws.com:6379`
   - Read Replica 활성화 (`REDIS_ROUTE_BY_LATENCY=true`)
2. **코드 마이그레이션**: `redis.Client` → `redis.UniversalClient`
3. **배포 후**: queue/join API가 100% 실패

---

## 증상 및 에러

### 클라이언트 에러 응답
```bash
$ curl -X POST https://api.traffictacos.store/api/v1/queue/join \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: $(uuidgen)" \
  -d '{"event_id": "evt_2025_1001", "user_id": "anonymous"}'

{
  "error": {
    "code": "QUEUE_ERROR",
    "message": "Failed to join queue",
    "trace_id": ""
  }
}
```

### 서버 로그 (예상)
```go
// internal/routes/queue.go:127
q.logger.WithError(err).WithFields(logrus.Fields{
    "event_id": req.EventID,
    "user_id":  req.UserID,
}).Error("Failed to enqueue atomically")
```

### Lua Script 실행 실패
```go
// internal/queue/lua_executor.go:74
le.logger.WithError(err).WithFields(logrus.Fields{
    "dedupe_key": dedupeKey,
    "stream_key": streamKey,
}).Error("Lua script execution failed")
// Error: "CROSSSLOT Keys in request don't hash to the same slot"
```

---

## 기존 코드 분석

### ❌ 문제가 있던 코드 (Commit: a5bb2c1)

**파일**: `internal/routes/queue.go`

```go
// Line 109-111
// Atomic enqueue with deduplication using Lua Script
dedupeKey := fmt.Sprintf("dedupe:%s", idempotencyKey)
streamKey := fmt.Sprintf("stream:event:{%s}:user:%s", req.EventID, req.UserID)

result, err := q.luaExecutor.EnqueueAtomic(
    ctx,
    dedupeKey,   // ❌ "dedupe:user123:evt_2025_1001:1728336000"
    streamKey,   // ❌ "stream:event:{evt_2025_1001}:user:anonymous"
    waitingToken,
    req.EventID,
    req.UserID,
    300,
)
```

**문제점**:
1. `dedupeKey`에 **hash tag가 없음** → Redis가 전체 키를 해싱
2. `streamKey`에만 **hash tag `{evt_2025_1001}`** 존재
3. 두 키가 **서로 다른 hash slot**에 할당됨

### Hash Slot 계산 예시

```
Redis Cluster Slot Range: 0-16383 (총 16,384개)

dedupeKey = "dedupe:user123:evt_2025_1001:1728336000"
→ CRC16("dedupe:user123:evt_2025_1001:1728336000") % 16384 = 5234 (예시)

streamKey = "stream:event:{evt_2025_1001}:user:anonymous"
→ CRC16("evt_2025_1001") % 16384 = 12891 (예시)  // {} 안의 부분만 해싱

Slot 5234 → Shard 1 (Primary: 0001-001, Replica: 0001-002)
Slot 12891 → Shard 3 (Primary: 0003-001, Replica: 0003-002)
```

### Lua Script가 사용하는 키들

**파일**: `internal/queue/lua/enqueue_atomic_streams.lua`

```lua
-- KEYS[1] = dedupeKey
-- KEYS[2] = streamKey

-- Dedupe 확인
local deduped = redis.call("EXISTS", KEYS[1])
if deduped == 1 then
    return {0, "DUPLICATE"}
end

-- Stream 추가
local stream_id = redis.call("XADD", KEYS[2], "*", 
    "token", token, 
    "event_id", event_id, 
    "user_id", user_id
)

-- Dedupe 키 저장
redis.call("SETEX", KEYS[1], ttl, stream_id)
```

**Redis Cluster의 제약사항**:
- Lua script는 **atomic 실행**을 보장
- 하지만 **모든 키가 같은 hash slot**에 있어야 함
- 다른 slot이면 `CROSSSLOT` 에러 발생

---

## 근본 원인

### Redis Cluster의 Hash Slot 메커니즘

1. **Standalone Mode**:
   ```
   모든 데이터 → 단일 Redis 인스턴스
   → Lua script에서 모든 키 접근 가능
   ```

2. **Cluster Mode**:
   ```
   데이터 분산 → 16,384개 hash slot → 3개 shard
   Shard 1: slot 0-5461
   Shard 2: slot 5462-10922
   Shard 3: slot 10923-16383
   
   → Lua script는 단일 shard에서만 실행 가능!
   ```

3. **Hash Tag `{}`의 역할**:
   ```
   일반 키: "my:key:123"
   → 전체 문자열로 slot 계산
   
   Hash Tag 사용: "my:{tag}:123"
   → {tag} 부분만으로 slot 계산
   → 같은 tag를 가진 키들이 같은 slot에 배치됨
   ```

### 왜 Standalone에서는 문제가 없었나?

```
Standalone Mode:
- 모든 키 → 같은 Redis 인스턴스
- Lua script 제약 없음

Cluster Mode:
- 키가 여러 shard에 분산
- Lua script는 CROSSSLOT 에러 발생
```

---

## 해결 방법

### ✅ 수정된 코드 (Commit: 084fbfa)

**파일**: `internal/routes/queue.go`

```go
// Line 109-112
// Atomic enqueue with deduplication using Lua Script
// 🔴 Use hash tag {eventID} to ensure both keys are in the same Redis Cluster slot
dedupeKey := fmt.Sprintf("dedupe:{%s}:%s", req.EventID, idempotencyKey)
streamKey := fmt.Sprintf("stream:event:{%s}:user:%s", req.EventID, req.UserID)

result, err := q.luaExecutor.EnqueueAtomic(
    ctx,
    dedupeKey,   // ✅ "dedupe:{evt_2025_1001}:user123:..."
    streamKey,   // ✅ "stream:event:{evt_2025_1001}:user:anonymous"
    waitingToken,
    req.EventID,
    req.UserID,
    300,
)
```

### Hash Slot 계산 결과

```
dedupeKey = "dedupe:{evt_2025_1001}:user123:evt_2025_1001:1728336000"
→ CRC16("evt_2025_1001") % 16384 = 12891

streamKey = "stream:event:{evt_2025_1001}:user:anonymous"
→ CRC16("evt_2025_1001") % 16384 = 12891

✅ 같은 slot 12891 → Shard 3
✅ Lua script 실행 가능!
```

### 변경 사항 요약

| 항목 | Before | After |
|-----|--------|-------|
| **dedupeKey** | `dedupe:user123:evt_2025_1001:...` | `dedupe:{evt_2025_1001}:user123:...` |
| **Hash 대상** | 전체 문자열 | `{evt_2025_1001}` 부분만 |
| **Slot 할당** | Random (예: 5234) | 12891 (streamKey와 동일) |
| **Lua Script** | ❌ CROSSSLOT 에러 | ✅ 정상 실행 |

---

## 기술적 배경

### Redis Cluster의 데이터 분산

```
┌─────────────────────────────────────────────────┐
│         Redis Cluster (16,384 slots)            │
├─────────────────────────────────────────────────┤
│ Shard 1 (slots 0-5461)                          │
│   ├─ Primary:  traffic-tacos-redis-0001-001     │
│   └─ Replica:  traffic-tacos-redis-0001-002     │
├─────────────────────────────────────────────────┤
│ Shard 2 (slots 5462-10922)                      │
│   ├─ Primary:  traffic-tacos-redis-0002-001     │
│   └─ Replica:  traffic-tacos-redis-0002-002     │
├─────────────────────────────────────────────────┤
│ Shard 3 (slots 10923-16383)                     │
│   ├─ Primary:  traffic-tacos-redis-0003-001     │
│   └─ Replica:  traffic-tacos-redis-0003-002     │
└─────────────────────────────────────────────────┘
```

### Hash Slot 계산 알고리즘

```go
func GetSlot(key string) int {
    // 1. Hash Tag 추출
    start := strings.Index(key, "{")
    end := strings.Index(key, "}")
    
    var hashKey string
    if start != -1 && end != -1 && end > start+1 {
        // Hash Tag가 있으면 {} 안의 문자열만 사용
        hashKey = key[start+1:end]
    } else {
        // Hash Tag가 없으면 전체 키 사용
        hashKey = key
    }
    
    // 2. CRC16 계산 후 16384로 나눈 나머지
    return int(crc16(hashKey)) % 16384
}
```

### Hash Tag 사용 예시

```go
// ❌ Bad: 각 키마다 다른 slot
"user:123:profile"        // → slot 5234
"user:123:settings"       // → slot 8901
"user:123:notifications"  // → slot 12456

// ✅ Good: 모든 키가 같은 slot
"user:{123}:profile"        // → slot 7890
"user:{123}:settings"       // → slot 7890
"user:{123}:notifications"  // → slot 7890

// ✅ Good: eventID 기준 그룹핑
"dedupe:{evt_2025_1001}:user123"           // → slot 12891
"stream:event:{evt_2025_1001}:user:john"  // → slot 12891
"queue:event:{evt_2025_1001}"             // → slot 12891
```

### Lua Script와 Cluster Mode

**Lua Script의 장점**:
```lua
-- Atomic 연산 보장
-- Race condition 방지
-- Network round-trip 최소화
```

**Cluster Mode에서의 제약**:
```
1. 모든 KEYS[]는 같은 slot에 있어야 함
2. KEYS[]로 명시하지 않은 키 접근 금지
3. 다른 slot 키에 접근하면 CROSSSLOT 에러
```

**올바른 사용법**:
```lua
-- ✅ Good
local KEYS[1] = "data:{tag}:key1"
local KEYS[2] = "data:{tag}:key2"
redis.call("SET", KEYS[1], "value1")
redis.call("GET", KEYS[2])

-- ❌ Bad
local KEYS[1] = "data:key1"
local KEYS[2] = "other:key2"  -- 다른 slot!
redis.call("SET", KEYS[1], "value1")
redis.call("GET", KEYS[2])  -- CROSSSLOT 에러!
```

---

## 추가 고려사항

### 1. 다른 Queue 관련 키들

현재 코드베이스에서 hash tag가 **필요한** 키들:

```go
// ✅ 이미 hash tag 사용 중
streamKey := fmt.Sprintf("stream:event:{%s}:user:%s", eventID, userID)

// ⚠️ 단일 키 작업이므로 hash tag 선택적
queueKey := fmt.Sprintf("queue:waiting:%s", waitingToken)
heartbeatKey := fmt.Sprintf("heartbeat:%s", waitingToken)
eventQueueKey := fmt.Sprintf("queue:event:%s", eventID)

// 💡 미래의 Lua script를 위한 권장 패턴
eventQueueKey := fmt.Sprintf("queue:event:{%s}", eventID)
reservationKey := fmt.Sprintf("queue:reservation:{%s}:%s", eventID, token)
```

### 2. inventory-api의 Lua Script

**파일**: `internal/queue/lua_executor.go`

```go
// HoldSeatAtomic - 3개 키 사용
[]string{seatStatusKey, holdKey, inventoryKey}

// 💡 권장: 모두 {eventID} hash tag 사용
seatStatusKey = "seat:{evt_2025_1001}:A-12:status"
holdKey       = "hold:{evt_2025_1001}:A-12"
inventoryKey  = "inventory:{evt_2025_1001}"
```

### 3. Performance 고려사항

**Hash Tag의 Trade-off**:

```
✅ 장점:
- Lua script 사용 가능
- Atomic 연산 보장
- Transaction 지원

⚠️ 단점:
- 같은 tag의 키들이 한 shard에 집중
- Hot shard 발생 가능
- 이벤트별로 shard가 다르므로 부하 분산됨

💡 권장 설계:
{eventID} 사용 → 이벤트별 shard 분산
{userID} 사용 → 사용자별 shard 분산 (X - 부하 불균형)
```

### 4. 모니터링 포인트

```bash
# 1. Shard별 키 분포 확인
redis-cli --cluster call clustercfg.traffic-tacos-redis...:6379 \
  DBSIZE

# 2. Slot migration 상태 확인
redis-cli --cluster check clustercfg.traffic-tacos-redis...:6379

# 3. Lua script 에러 모니터링
kubectl logs -n tacos-app -l app=gateway-api | \
  grep -i "crossslot\|lua.*failed"

# 4. 이벤트별 키 분포 확인
redis-cli --cluster call clustercfg.traffic-tacos-redis...:6379 \
  KEYS "*{evt_2025_1001}*" | wc -l
```

### 5. 테스트 체크리스트

```bash
# ✅ Standalone Mode에서 테스트
export REDIS_CLUSTER_MODE=false
./run_local.sh

# ✅ Cluster Mode에서 테스트
export REDIS_CLUSTER_MODE=true
export REDIS_ADDRESS=clustercfg.traffic-tacos-redis...:6379

# ✅ Lua script 동작 확인
curl -X POST http://localhost:8000/api/v1/queue/join \
  -H "Content-Type: application/json" \
  -d '{"event_id": "evt_test", "user_id": "test"}'

# ✅ 여러 이벤트 동시 처리
for i in {1..10}; do
  curl -X POST https://api.traffictacos.store/api/v1/queue/join \
    -H "Content-Type: application/json" \
    -H "Idempotency-Key: $(uuidgen)" \
    -d "{\"event_id\": \"evt_$i\", \"user_id\": \"user_$i\"}" &
done
wait
```

### 6. 추가 참고자료

**Redis 공식 문서**:
- [Redis Cluster Specification](https://redis.io/docs/reference/cluster-spec/)
- [Hash Tags](https://redis.io/docs/reference/cluster-spec/#hash-tags)
- [Lua Scripting](https://redis.io/docs/manual/programmability/eval-intro/)

**AWS ElastiCache 문서**:
- [Cluster Mode Best Practices](https://docs.aws.amazon.com/AmazonElastiCache/latest/red-ug/BestPractices.html)
- [Lua Scripting in Cluster Mode](https://docs.aws.amazon.com/AmazonElastiCache/latest/red-ug/cluster-mode-scripting.html)

---

## 결론

### 핵심 교훈

1. **Redis Cluster Mode는 Standalone과 다른 제약사항이 있음**
   - Lua script의 모든 키는 같은 hash slot에 있어야 함
   - Hash tag `{}`를 사용하여 키를 같은 slot으로 그룹핑

2. **코드 마이그레이션 시 검토 필요**
   - Standalone → Cluster 마이그레이션 시 모든 Lua script 검토
   - 단일 키 작업은 영향 없음
   - 다중 키 작업(Transaction, Lua script)은 hash tag 필수

3. **설계 패턴**
   - 이벤트 기반 서비스: `{eventID}` 사용 권장
   - 사용자 기반 서비스: `{userID}` 사용 (Hot shard 주의)
   - 글로벌 데이터: Hash tag 불필요

### 체크리스트

배포 전 확인 사항:
- [ ] 모든 Lua script의 KEYS[] 확인
- [ ] Hash tag 패턴 일관성 유지
- [ ] Standalone/Cluster 모드 모두 테스트
- [ ] 로그에서 CROSSSLOT 에러 모니터링
- [ ] Shard별 부하 분산 확인

---

**작성일**: 2025-10-07  
**작성자**: Gateway API Team  
**관련 Commit**: 
- `a5bb2c1` - feat: Add ElastiCache Cluster Mode with Read Replica support
- `084fbfa` - fix: Add Redis Cluster hash tag to dedupeKey for Lua script compatibility
