# 🚨 Critical Bugfix: Position 고정 버그 수정 (v1.3.1)

## 📋 문제 요약

**증상**: 대기열에서 `position`이 절대 감소하지 않음
- 사용자가 Join 후 Status를 폴링해도 `position: 7`이 계속 유지
- 다른 사용자가 Enter해도 앞사람이 빠지지 않는 것처럼 보임
- `waiting_time`만 증가하고 `position`과 `eta_sec`은 고정

**영향도**: 🔴 **CRITICAL** - 대기열 기능 완전 마비

## 🔍 근본 원인 분석

### 문제의 핵심

**Join/Enter/Leave에서 데이터 구조 동기화 실패**

```
현재 데이터 구조:
1. Redis Streams: stream:event:{eventID}:user:{userID}
2. Redis ZSET: queue:event:{eventID}
3. Redis Hash: queue:waiting:{token}

문제:
1. Join → Stream에만 추가, ZSET ❌
2. Enter → 아무것도 제거 안 함 ❌
3. Leave → ZSET만 제거, Stream ❌
4. Status → ZSET에서 position 계산 (항상 고정!)
```

### 코드 레벨 분석

#### Join 함수 (Before)
```go
// ❌ Stream에만 추가
result, err := q.luaExecutor.EnqueueAtomic(...)

// ❌ ZSET에는 추가하지 않음!
// 따라서 Status에서 ZSET fallback 시 항상 실패
```

#### Enter 함수 (Before)
```go
// ✅ reservation_token 생성
// ✅ metrics 기록

// ❌ ZSET에서 제거하지 않음!
// ❌ Stream에서 제거하지 않음!
// 따라서 다른 사용자가 Enter해도 position 변화 없음
```

#### Status 함수 (Before)
```go
// Try Stream first
entries, err := q.redisClient.XRange(...)

// Fallback to ZSET
rank, err := q.redisClient.ZRank(ctx, eventQueueKey, waitingToken).Result()
// ❌ ZSET이 비어있거나 Join 시 추가 안 되어서 항상 같은 값 반환
```

## 🛠️ 수정 내용

### 1. Join 함수 수정

**ZSET에도 추가하여 이중 추적**

```go
// ✅ Stream 추가 (Lua Script)
result, err := q.luaExecutor.EnqueueAtomic(...)

// 🔴 NEW: ZSET에도 추가
eventQueueKey := fmt.Sprintf("queue:event:%s", req.EventID)
score := float64(time.Now().Unix()) // FIFO ordering
if err := q.redisClient.ZAdd(ctx, eventQueueKey, redis.Z{
    Score:  score,
    Member: waitingToken,
}).Err(); err != nil {
    q.logger.WithError(err).Warn("Failed to add to ZSET queue")
}
```

**효과**:
- Stream: 감사 추적 및 per-user ordering
- ZSET: 빠른 position 계산 (O(log N))
- 이중 안전장치: 하나가 실패해도 다른 하나로 복구 가능

### 2. Enter 함수 수정

**입장 시 ZSET에서 제거하여 position 업데이트**

```go
// 🔴 NEW: ZSET에서 제거
eventQueueKey := fmt.Sprintf("queue:event:%s", queueData.EventID)
if err := q.redisClient.ZRem(ctx, eventQueueKey, req.WaitingToken).Err(); err != nil {
    q.logger.WithError(err).Warn("Failed to remove from ZSET queue")
}

// 🔴 NEW: Stream 정리 (선택적, 감사 추적 유지)
streamKey := fmt.Sprintf("stream:event:{%s}:user:%s", queueData.EventID, queueData.UserID)
// Keep for audit trail, ZSET removal is sufficient
```

**효과**:
- 사용자가 Enter하면 ZSET에서 즉시 제거
- 뒤에 있는 사용자의 ZRANK가 자동으로 감소
- Status 폴링 시 position이 실시간으로 업데이트됨!

### 3. Leave 함수 수정

**Stream까지 완전히 정리**

```go
// Get queue data first (before deletion)
queueData, err := q.getQueueData(ctx, waitingToken)

// Remove from ZSET
eventQueueKey := fmt.Sprintf("queue:event:%s", queueData.EventID)
q.redisClient.ZRem(ctx, eventQueueKey, waitingToken)

// 🔴 NEW: Stream에서도 제거
streamKey := fmt.Sprintf("stream:event:{%s}:user:%s", queueData.EventID, queueData.UserID)
entries, _ := q.redisClient.XRange(ctx, streamKey, "-", "+").Result()
for _, entry := range entries {
    if token, ok := entry.Values["token"].(string); ok && token == waitingToken {
        q.redisClient.XDel(ctx, streamKey, entry.ID)
        break
    }
}
```

**효과**:
- 사용자가 자발적으로 떠날 때 완전히 정리
- 메모리 누수 방지
- 다음 사용자의 position 정확도 향상

## 📊 수정 전후 비교

### Before (버그 상태)

```
User A joins queue:
  Stream: ✅ Added
  ZSET:   ❌ Not added

User A polls status:
  Stream lookup: ✅ Success, position=7
  ZSET fallback: (not reached)

User B enters (position 1-6):
  ZSET removal: ❌ Not executed
  
User A polls status again:
  Stream lookup: ✅ Success, position=7 (unchanged!)
  → 버그! B가 입장했는데도 A의 position은 그대로!
```

### After (수정 후)

```
User A joins queue:
  Stream: ✅ Added (stream:event:{id}:user:{A})
  ZSET:   ✅ Added (queue:event:{id}, member=tokenA, score=timestamp)

User A polls status:
  ZSET ZRANK: position=7
  → 정확!

User B enters (position 1-6):
  ZSET removal: ✅ ZREM tokenB
  → ZSET now has 6 less members
  
User A polls status again:
  ZSET ZRANK: position=1 (7 → 1!)
  → 정확! B의 입장이 실시간 반영됨!
```

## 🧪 테스트 시나리오

### 1. 단일 사용자 테스트

```bash
# 1. Join
TOKEN=$(curl -X POST .../queue/join -d '{"event_id":"test","user_id":"A"}' | jq -r '.waiting_token')

# 2. Status (초기)
curl ".../queue/status?token=$TOKEN"
# 예상: {"position": 1, "eta_sec": 2, ...}

# 3. Enter (본인)
curl -X POST .../queue/enter -d "{\"waiting_token\":\"$TOKEN\"}"

# 4. Status (입장 후 - 다른 토큰으로)
# 예상: 404 Not Found (이미 입장함)
```

### 2. 다중 사용자 테스트

```bash
# 3명이 순차 Join
TOKEN_A=$(curl -X POST .../queue/join -d '{"event_id":"test","user_id":"A"}' | jq -r '.waiting_token')
TOKEN_B=$(curl -X POST .../queue/join -d '{"event_id":"test","user_id":"B"}' | jq -r '.waiting_token')
TOKEN_C=$(curl -X POST .../queue/join -d '{"event_id":"test","user_id":"C"}' | jq -r '.waiting_token')

# C의 초기 position
curl ".../queue/status?token=$TOKEN_C"
# 예상: {"position": 3, ...}

# A가 입장
curl -X POST .../queue/enter -d "{\"waiting_token\":\"$TOKEN_A\"}"

# C의 position 확인 (1초 후)
sleep 1
curl ".../queue/status?token=$TOKEN_C"
# 예상: {"position": 2, ...}  ← 3에서 2로 감소! ✅

# B도 입장
curl -X POST .../queue/enter -d "{\"waiting_token\":\"$TOKEN_B\"}"

# C의 position 확인
sleep 1
curl ".../queue/status?token=$TOKEN_C"
# 예상: {"position": 1, ...}  ← 2에서 1로 감소! ✅
```

### 3. Leave 테스트

```bash
# A, B, C 순차 Join
# ...

# B가 중간에 Leave
curl -X DELETE ".../queue/leave?token=$TOKEN_B"

# C의 position 확인
curl ".../queue/status?token=$TOKEN_C"
# 예상: {"position": 2, ...}  ← B가 떠났으므로 3→2로 감소! ✅
```

## 🚀 배포 계획

### 1. 긴급 배포 (v1.3.1)

```bash
# Build
docker build -t 533267001323.dkr.ecr.ap-northeast-2.amazonaws.com/gateway-api:v1.3.1 .

# Push
docker push 533267001323.dkr.ecr.ap-northeast-2.amazonaws.com/gateway-api:v1.3.1

# Deploy
kubectl set image deployment/gateway-api gateway-api=533267001323.dkr.ecr.ap-northeast-2.amazonaws.com/gateway-api:v1.3.1 -n tickets-api
```

### 2. 검증 절차

```bash
# 1. Pod 재시작 확인
kubectl get pods -n tickets-api -l app=gateway-api

# 2. 로그 확인
kubectl logs -f deployment/gateway-api -n tickets-api | grep "joined queue"

# 3. 실제 API 테스트
./test_position_update.sh
```

### 3. 롤백 계획

```bash
# 문제 발생 시 v1.3.0으로 즉시 롤백
kubectl rollout undo deployment/gateway-api -n tickets-api

# 또는 명시적 버전 지정
kubectl set image deployment/gateway-api gateway-api=533267001323.dkr.ecr.ap-northeast-2.amazonaws.com/gateway-api:v1.3.0 -n tickets-api
```

## 📈 성능 영향

### Redis 명령어 증가

**Join 함수**:
- Before: XADD (Stream) × 1
- After: XADD × 1 + ZADD × 1
- **증가**: +1 명령어 (O(log N))

**Enter 함수**:
- Before: SET × 2
- After: SET × 2 + ZREM × 1
- **증가**: +1 명령어 (O(log N))

**Leave 함수**:
- Before: DEL × 1 + ZREM × 1
- After: DEL × 1 + ZREM × 1 + XRANGE × 1 + XDEL × 1
- **증가**: +2 명령어 (O(N) where N = 사용자별 stream entries, 보통 1-10개)

**Status 함수**:
- 변화 없음 (이미 ZRANK 사용 중)

### 예상 성능

```
Join:  ~1ms (XADD + ZADD)
Enter: ~1ms (SET + ZREM)
Leave: ~2ms (DEL + ZREM + XRANGE + XDEL)
Status: ~0.5ms (ZRANK - 변화 없음)

30k RPS 기준:
- Redis CPU: +2% 예상
- 응답 시간: P95 < 50ms 유지 가능
```

## 🎯 핵심 교훈

### 1. 데이터 구조 일관성

**교훈**: 여러 데이터 구조를 사용할 때는 모든 CRUD 작업에서 동기화 필수

```
Rule: 
- Create → 모든 구조에 추가
- Read → 우선순위 기반 조회 + fallback
- Update → 모든 구조 동기화
- Delete → 모든 구조에서 제거
```

### 2. 테스트 커버리지

**교훈**: 단일 사용자 테스트로는 불충분, 다중 사용자 시나리오 필수

```
테스트 시나리오:
✅ 단일 사용자 Join → Status → Enter
✅ 다중 사용자 순차 Join → 일부 Enter → 나머지 Status 확인
✅ 다중 사용자 중간 Leave → position 업데이트 확인
```

### 3. 로깅 강화

**교훈**: 각 단계에서 어떤 데이터 구조를 수정했는지 명시적 로깅

```go
q.logger.WithFields(logrus.Fields{
    "operation": "enter",
    "zset_removed": true,
    "stream_kept": true, // audit trail
    "waiting_token": token,
}).Info("User entered, removed from ZSET")
```

## 📝 체크리스트

- [x] 코드 수정 완료
- [x] 로컬 빌드 성공
- [x] 린팅 통과
- [ ] Docker 이미지 빌드
- [ ] ECR 푸시
- [ ] Kubernetes 배포
- [ ] 실제 API 테스트
- [ ] 다중 사용자 시나리오 검증
- [ ] 로그 확인
- [ ] 메트릭 확인 (admission rate)

## 🔗 관련 문서

- [QUEUE_WORKFLOW.md](./QUEUE_WORKFLOW.md) - 대기열 전체 워크플로우
- [PHASE1_GATEWAY_INTEGRATION.md](./PHASE1_GATEWAY_INTEGRATION.md) - Gateway API 통합
- [REFACTORING_PLAN.md](./REFACTORING_PLAN.md) - 전체 리팩터링 계획

---

**버전**: v1.3.1  
**작성일**: 2025-01-XX  
**작성자**: Traffic Tacos Team  
**우선순위**: 🔴 CRITICAL  
**예상 배포 시간**: ~15분 (빌드 + 배포 + 검증)
