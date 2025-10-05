# Heartbeat 메커니즘: 자동 이탈 감지 및 정리

## 🎯 목적

**문제:**
- 사용자가 Join 후 브라우저를 닫거나 네트워크가 끊기면?
- Leave API를 호출하지 않고 떠난 "유령 사용자"가 대기열에 남음
- Position 계산에 포함되어 실제 사용자의 대기 시간 증가

**해결:**
- Heartbeat 기반 자동 감지 및 정리
- Status API 폴링 = Heartbeat 신호
- 5분간 Status 호출 없으면 자동으로 대기열에서 제거

---

## 🏗️ 아키텍처

### Redis 키 구조

```
heartbeat:{waiting_token}     # TTL: 5분, Value: "alive"
queue:waiting:{waiting_token}  # TTL: 30분
queue:event:{eventID}          # ZSET, TTL: 1시간
stream:event:{eventID}:user:{userID}  # Stream, TTL: 1시간
```

### 상태 전이도

```
┌─────────────────────────────────────────────────────────────┐
│                         JOIN                                │
│  1. Create queue:waiting:{token}                           │
│  2. Add to queue:event:{eventID} (ZSET)                    │
│  3. Add to stream:event:{eventID}:user:{userID}           │
│  4. Create heartbeat:{token} TTL=5min ✨ NEW              │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                       STATUS (Polling)                      │
│  Every 2 seconds (frontend)                                │
│                                                             │
│  1. Check heartbeat:{token} exists?                        │
│     ├─ YES → Renew TTL to 5min ✅                         │
│     └─ NO  → Cleanup & return "EXPIRED" ❌                │
│                                                             │
│  Cleanup steps:                                            │
│    - Remove from ZSET                                      │
│    - Remove from Stream                                    │
│    - Delete queue:waiting:{token}                          │
└─────────────────────────────────────────────────────────────┘
                              │
                    ┌─────────┼─────────┐
                    │                   │
                    ▼                   ▼
┌──────────────────────────┐  ┌──────────────────────────┐
│         ENTER            │  │         LEAVE            │
│  (Successful Entry)      │  │  (Explicit Exit)         │
│                          │  │                          │
│  1. Grant admission      │  │  1. Remove all keys      │
│  2. Delete heartbeat ✨  │  │  2. Delete heartbeat ✨  │
│  3. Remove from ZSET     │  │  3. Remove from ZSET     │
│  4. Return token         │  │  4. Remove from Stream   │
└──────────────────────────┘  └──────────────────────────┘
```

---

## 📋 API 동작 상세

### 1. Join API

**생성되는 키:**
```redis
SETEX heartbeat:{waiting_token} 300 "alive"  # 5분 TTL
```

**코드:**
```go
// internal/routes/queue.go
heartbeatKey := fmt.Sprintf("heartbeat:%s", waitingToken)
q.redisClient.Set(ctx, heartbeatKey, "alive", 5*time.Minute)
```

---

### 2. Status API (핵심!)

**Heartbeat 체크 및 갱신:**

```go
// 1. Heartbeat 존재 확인
heartbeatKey := fmt.Sprintf("heartbeat:%s", waitingToken)
exists, err := q.redisClient.Exists(ctx, heartbeatKey).Result()

if exists == 0 {
    // 2. Heartbeat 만료 → 자동 정리
    q.logger.Info("Heartbeat expired - cleaning up abandoned user")
    
    // 3. ZSET 제거
    eventQueueKey := fmt.Sprintf("queue:event:%s", queueData.EventID)
    q.redisClient.ZRem(ctx, eventQueueKey, waitingToken)
    
    // 4. Stream 제거
    streamKey := fmt.Sprintf("stream:event:{%s}:user:%s", queueData.EventID, queueData.UserID)
    // ... XDel logic
    
    // 5. Queue data 제거
    queueKey := fmt.Sprintf("queue:waiting:%s", waitingToken)
    q.redisClient.Del(ctx, queueKey)
    
    // 6. Return expired
    return notFoundError(c, "TOKEN_EXPIRED", "Waiting token expired due to inactivity")
} else {
    // 7. Heartbeat 갱신 (사용자 활동 중)
    q.redisClient.Expire(ctx, heartbeatKey, 5*time.Minute)
}
```

**API 응답:**
```json
// 정상 (Heartbeat 살아있음)
{
  "status": "waiting",
  "position": 7,
  "eta_sec": 14,
  "waiting_time": 15,
  "ready_for_entry": false
}

// 만료 (Heartbeat 없음)
{
  "error": {
    "code": "TOKEN_EXPIRED",
    "message": "Waiting token expired due to inactivity"
  }
}
```

---

### 3. Enter API

**성공 시 Heartbeat 삭제:**
```go
// User entered successfully, no longer waiting
heartbeatKey := fmt.Sprintf("heartbeat:%s", waitingToken)
q.redisClient.Del(ctx, heartbeatKey)
```

---

### 4. Leave API

**명시적 이탈 시 Heartbeat 삭제:**
```go
// User explicitly left
heartbeatKey := fmt.Sprintf("heartbeat:%s", waitingToken)
q.redisClient.Del(ctx, heartbeatKey)

// + ZSET, Stream, Queue data 모두 삭제
```

---

## 🧪 테스트 시나리오

### 시나리오 1: 정상 케이스 (Status 폴링 지속)

```bash
# 1. Join
curl -X POST /api/v1/queue/join -d '{"event_id":"test","user_id":"user1"}'
# → heartbeat:token-123 생성 (TTL 5분)

# 2. Status 폴링 (2초마다)
for i in {1..150}; do  # 5분 = 150번
  curl -X GET "/api/v1/queue/status?token=token-123"
  sleep 2
done
# → 매번 heartbeat TTL이 5분으로 갱신됨
# → 결과: 계속 대기열에 남아있음 ✅
```

---

### 시나리오 2: 비정상 케이스 (브라우저 닫음)

```bash
# 1. Join
curl -X POST /api/v1/queue/join -d '{"event_id":"test","user_id":"user2"}'
# → heartbeat:token-456 생성 (TTL 5분)

# 2. Status 1번만 호출
curl -X GET "/api/v1/queue/status?token=token-456"
# → 200 OK

# 3. 브라우저 닫음 (Status 폴링 중단)
# ...5분 대기...

# 4. 5분 후 다른 사용자가 Status 호출
curl -X GET "/api/v1/queue/status?token=other-token"
# → user2의 heartbeat 만료 감지 (Status 호출 시)
# → 자동으로 대기열에서 제거됨
# → Position 재계산 (user2 제외) ✅

# 5. user2가 다시 Status 호출 시도
curl -X GET "/api/v1/queue/status?token=token-456"
# → 404 TOKEN_EXPIRED ✅
```

---

### 시나리오 3: 명시적 Leave

```bash
# 1. Join
curl -X POST /api/v1/queue/join -d '{"event_id":"test","user_id":"user3"}'
# → heartbeat:token-789 생성

# 2. "나가기" 버튼 클릭
curl -X DELETE "/api/v1/queue/leave?token=token-789"
# → heartbeat:token-789 즉시 삭제
# → ZSET, Stream 즉시 삭제
# → Position 즉시 재계산 ✅
```

---

### 시나리오 4: Enter 후 상태

```bash
# 1. Join → Status 폴링 → Position 1
curl -X POST /api/v1/queue/enter -d '{"waiting_token":"token-123"}'
# → 200 OK (reservation_token 발급)
# → heartbeat:token-123 삭제
# → ZSET에서 제거

# 2. 다시 Status 호출 시도
curl -X GET "/api/v1/queue/status?token=token-123"
# → 404 TOKEN_EXPIRED (heartbeat 없음) ✅
```

---

## 📊 모니터링 지표

### Redis 키 수명 추적

```bash
# Active heartbeat 수
redis-cli --scan --pattern "heartbeat:*" | wc -l

# Expired된 키 확인 (Redis INFO)
redis-cli INFO keyspace
# keyspace_hits / keyspace_misses ratio
```

### 로그 모니터링

```json
// Heartbeat 갱신 성공 (정상)
{
  "level": "debug",
  "msg": "Heartbeat renewed",
  "waiting_token": "token-123",
  "ttl_remaining": "300s"
}

// Heartbeat 만료 감지 (자동 정리)
{
  "level": "info",
  "msg": "Heartbeat expired - cleaning up abandoned user",
  "waiting_token": "token-456",
  "event_id": "concert-a",
  "user_id": "user2"
}
```

---

## 🎯 설계 의도

### 왜 5분 TTL?

**장점:**
- ✅ 일시적 네트워크 끊김 허용 (2초 폴링 기준 150번 재시도 가능)
- ✅ 브라우저 일시정지/슬립 모드 대응
- ✅ 모바일 백그라운드 전환 대응

**단점:**
- ❌ 실제 이탈 후 최대 5분간 Position에 반영 지연
- ❌ 다음 Status 호출 시점까지 정리 지연

**대안:**
- 3분 TTL: 더 빠른 정리, but 네트워크 불안정 시 오탈
- 10분 TTL: 더 관대한 대기, but 유령 사용자 오래 남음

**결론: 5분이 균형적**

---

### 왜 Status API에서 정리?

**Why not Redis Keyspace Notification?**
```redis
CONFIG SET notify-keyspace-events Ex
SUBSCRIBE __keyevent@0__:expired
```

**문제점:**
- Redis Pub/Sub는 fire-and-forget (보장 없음)
- 고부하 시 이벤트 유실 가능
- 별도 백그라운드 워커 필요 (복잡도 증가)

**Why Status API?**
- ✅ 이미 2초마다 호출되는 API (추가 비용 없음)
- ✅ 정리 로직이 자연스럽게 분산됨 (각 Status 호출 시)
- ✅ 단순하고 확실함 (별도 프로세스 불필요)

---

## 🚀 배포 영향

### Before (v1.3.1)

```
유령 사용자 문제:
- Join 후 브라우저 닫으면 영구 남음
- Position 계산에 포함됨
- 메모리 누수 (1시간 후 ZSET TTL로 정리)
```

### After (v1.3.2)

```
자동 정리 시스템:
- 5분간 Status 호출 없으면 자동 제거
- Position 즉시 재계산
- 메모리 효율 극대화
```

---

## 📋 프론트엔드 가이드

### 1. 기존 코드 (이미 OK!)

```typescript
// Status 폴링 = Heartbeat 자동 전송
useQuery({
  queryKey: ['queue-status', waitingToken],
  queryFn: () => queueApi.getStatus(waitingToken),
  refetchInterval: 2000, // 2초마다
})
```

**결과:**
- ✅ Heartbeat TTL 자동 갱신
- ✅ 추가 구현 불필요!

---

### 2. 추가 권장 사항

**"나가기" 버튼 (선택):**
```typescript
const handleLeave = async () => {
  await queueApi.leave(waitingToken)
  localStorage.removeItem('waiting_token')
  navigate('/')
}

<button onClick={handleLeave}>대기열 나가기</button>
```

**beforeunload 핸들러 (선택):**
```typescript
useEffect(() => {
  const handleBeforeUnload = () => {
    // Best effort: 브라우저 닫기 전 Leave 시도
    navigator.sendBeacon(
      '/api/v1/queue/leave',
      JSON.stringify({ waiting_token: waitingToken })
    )
  }
  
  window.addEventListener('beforeunload', handleBeforeUnload)
  return () => window.removeEventListener('beforeunload', handleBeforeUnload)
}, [waitingToken])
```

---

## 🎉 결론

**Before:**
- 유령 사용자가 대기열에 계속 남음
- Position 부정확
- 메모리 낭비

**After:**
- 5분 비활성 시 자동 제거
- Position 정확도 향상
- 메모리 효율 개선

**핵심:**
- ✅ Heartbeat 기반 자동 감지
- ✅ Status API에서 자연스럽게 정리
- ✅ 프론트엔드 변경 불필요 (기존 폴링만으로 OK)

**v1.3.2 배포 완료!** 🚀
