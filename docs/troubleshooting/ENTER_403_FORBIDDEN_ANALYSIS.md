# Enter API 403 Forbidden 에러 분석

## 📋 개요

`POST /api/v1/queue/enter` API는 대기열에서 본 시스템으로 입장하기 위한 엔드포인트입니다.
403 Forbidden 에러는 **사용자가 입장 조건을 충족하지 못했을 때** 발생합니다.

---

## 🔍 403 에러 발생 원인 (3가지)

### 1️⃣ **Position > 100** (Top 100이 아님)

```go
// internal/routes/queue.go:570-577
if position > 100 {
    q.logger.WithFields(logrus.Fields{
        "waiting_token": waitingToken,
        "position":      position,
    }).Debug("Not eligible: not in top 100 positions")
    return false
}
```

**조건:**
- 현재 대기열에서 Position이 101 이상일 때

**해결 방법:**
- `/queue/status`로 현재 Position 확인
- Position이 100 이하로 떨어질 때까지 대기
- 앞 사람들이 Enter하거나 Heartbeat 만료되면 Position 상승

**테스트 재현:**
```bash
# 150명 사용자 추가 후 Position 151로 Enter 시도
curl -X POST https://api.traffictacos.store/api/v1/queue/enter \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: $(uuidgen)" \
  -d '{"waiting_token": "..."}'

# Response:
{
  "error": {
    "code": "NOT_READY",
    "message": "Your turn has not arrived yet",
    "trace_id": "..."
  }
}
```

---

### 2️⃣ **Minimum Wait Time 미충족**

```go
// internal/routes/queue.go:579-602
waitTime := time.Since(queueData.JoinedAt)
var minWaitTime time.Duration

if position <= 10 {
    minWaitTime = 0 * time.Second // Top 10: 즉시 입장 가능! 🎉
} else if position <= 50 {
    minWaitTime = 2 * time.Second
} else {
    minWaitTime = 5 * time.Second
}

if waitTime < minWaitTime {
    return false // 403 Forbidden
}
```

**조건:**
| Position | 최소 대기 시간 | 설명 |
|---------|-------------|------|
| 1-10    | **0초**      | VIP 대우, 즉시 입장 가능 |
| 11-50   | **2초**      | 짧은 대기 |
| 51-100  | **5초**      | 중간 대기 |

**해결 방법:**
- Position에 따른 최소 대기 시간을 충족할 때까지 대기
- `/queue/status`의 `waiting_time` 필드로 경과 시간 확인

**테스트 재현:**
```bash
# Position 20인 사용자가 Join 직후 (0.5초) Enter 시도
curl -X POST https://api.traffictacos.store/api/v1/queue/enter \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: $(uuidgen)" \
  -d '{"waiting_token": "..."}'

# Response (2초 미만이면):
{
  "error": {
    "code": "NOT_READY",
    "message": "Your turn has not arrived yet",
    "trace_id": "..."
  }
}
```

**로그 예시:**
```json
{
  "level": "debug",
  "msg": "Not eligible: minimum wait time not met",
  "waiting_token": "16422802-...",
  "position": 20,
  "wait_time": 0.5,
  "min_wait_time": 2.0
}
```

---

### 3️⃣ **Token Bucket Rate Limiting**

```go
// internal/routes/queue.go:604-614
// 🔴 Top 10 users bypass token bucket (VIP treatment)
if position <= 10 {
    q.logger.Info("Eligibility check completed - VIP bypass")
    return true
}

// Token Bucket check for position 11-100
if !q.tryAcquireToken(ctx, queueData.EventID) {
    q.logger.Debug("Not eligible: rate limit exceeded")
    return false // 403 Forbidden
}
```

**조건:**
- Position 11-100인 사용자는 Token Bucket 체크
- **Admission Control**에서 초당 허용 인원 제한
- Token이 부족하면 403

**Token Bucket 설정:**
- **Capacity**: 100 tokens
- **Refill Rate**: 10 tokens/sec
- **Purpose**: 백엔드 서비스 보호 (1,000 RPS 제한)

**해결 방법:**
- 재시도 (backoff 후)
- Token이 refill 될 때까지 대기 (100ms ~ 1초)

**테스트 재현:**
```bash
# 100명이 동시에 Enter 시도 (Token Bucket 고갈)
for i in {1..100}; do
  curl -X POST https://api.traffictacos.store/api/v1/queue/enter \
    -H "Content-Type: application/json" \
    -H "Idempotency-Key: $(uuidgen)" \
    -d "{\"waiting_token\": \"token-$i\"}" &
done
wait

# 101번째 요청:
{
  "error": {
    "code": "NOT_READY",
    "message": "Your turn has not arrived yet",
    "trace_id": "..."
  }
}
```

**로그 예시:**
```json
{
  "level": "debug",
  "msg": "Not eligible: rate limit exceeded",
  "waiting_token": "16422802-...",
  "position": 50,
  "event_id": "evt_2025_1001"
}
```

---

## 📊 Enter Eligibility 판단 플로우

```
┌──────────────────────────────────────────────┐
│  POST /api/v1/queue/enter                    │
│  {"waiting_token": "..."}                    │
└──────────────────┬───────────────────────────┘
                   │
                   ▼
         ┌─────────────────┐
         │ Get Queue Data  │
         │ (Redis Lookup)  │
         └────────┬─────────┘
                  │
                  ▼
         ┌─────────────────────────┐
         │ isEligibleForEntry()    │
         └────────┬────────────────┘
                  │
    ┌─────────────┼─────────────┐
    │             │             │
    ▼             ▼             ▼
┌────────┐  ┌───────────┐  ┌──────────────┐
│Position│  │ Wait Time │  │ Token Bucket │
│ ≤ 100? │  │ Sufficient?│  │  Available?  │
└───┬────┘  └─────┬─────┘  └──────┬───────┘
    │ No          │ No            │ No
    │             │               │
    └─────────────┴───────────────┘
                  │
                  ▼
         ┌──────────────────┐
         │  403 Forbidden   │
         │  "NOT_READY"     │
         └──────────────────┘
                  
    All Yes ✅
         │
         ▼
┌─────────────────────────┐
│ Generate Reservation    │
│ Token (30s TTL)         │
└────────┬────────────────┘
         │
         ▼
┌─────────────────────────┐
│ 200 OK                  │
│ {"reservation_token":...}│
└─────────────────────────┘
```

---

## 🛠️ 디버깅 방법

### 1. Status API로 현재 상태 확인

```bash
curl -X GET "https://api.traffictacos.store/api/v1/queue/status?token=YOUR_WAITING_TOKEN" \
  -H "Origin: https://www.traffictacos.store"
```

**Response:**
```json
{
  "status": "waiting",
  "position": 45,              // ← Position 확인
  "eta_sec": 12,
  "waiting_time": 1,           // ← 대기 시간 (초) 확인
  "ready_for_entry": false     // ← true여야 Enter 가능
}
```

### 2. ready_for_entry 필드 활용

`ready_for_entry: true`일 때만 Enter 시도
- Position ≤ 100
- Minimum Wait Time 충족
- (Token Bucket은 Status API에서 체크 안 함)

### 3. 프로덕션 로그 확인

```bash
# CloudWatch Logs Insights Query
fields @timestamp, @message
| filter @message like /Not eligible/
| sort @timestamp desc
| limit 100
```

**로그 필터링:**
```
"Not eligible: not in top 100 positions"
"Not eligible: minimum wait time not met"
"Not eligible: rate limit exceeded"
```

---

## 💡 Frontend 권장 로직

```javascript
// 1. Join Queue
const joinResponse = await fetch('/api/v1/queue/join', {
  method: 'POST',
  headers: {
    'Content-Type': 'application/json',
    'Idempotency-Key': generateUUID(),
  },
  body: JSON.stringify({
    event_id: 'evt_2025_1001',
    user_id: currentUser.id,
  }),
});

const { waiting_token } = await joinResponse.json();

// 2. Poll Status every 3 seconds
const pollStatus = async () => {
  const statusResponse = await fetch(
    `/api/v1/queue/status?token=${waiting_token}`
  );
  const status = await statusResponse.json();

  console.log(`Position: ${status.position}, Ready: ${status.ready_for_entry}`);

  // 3. Only attempt Enter when ready_for_entry is true
  if (status.ready_for_entry && status.position <= 100) {
    tryEnter();
  } else {
    setTimeout(pollStatus, 3000); // Poll again after 3s
  }
};

// 4. Enter with retry logic
const tryEnter = async (retryCount = 0) => {
  const enterResponse = await fetch('/api/v1/queue/enter', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      'Idempotency-Key': generateUUID(),
    },
    body: JSON.stringify({
      waiting_token: waiting_token,
    }),
  });

  if (enterResponse.status === 403) {
    // Token Bucket 일 가능성 → 재시도
    if (retryCount < 3) {
      await sleep(1000); // 1초 대기
      return tryEnter(retryCount + 1);
    } else {
      alert('입장에 실패했습니다. 잠시 후 다시 시도해주세요.');
    }
  } else if (enterResponse.ok) {
    const { reservation_token } = await enterResponse.json();
    console.log('Reservation Token:', reservation_token);
    // Redirect to reservation page
  }
};
```

---

## 📈 403 에러 통계 (CloudWatch Metrics)

**메트릭:**
```
http_server_requests_total{method="POST", route="/api/v1/queue/enter", status_code="403"}
```

**정상 범위:**
- **403 비율**: < 5% (대부분 사용자는 ready_for_entry 체크 후 요청)
- **Top 10 Bypass**: ~10% (VIP 대우, 즉시 입장)

**이상 징후:**
- **403 > 20%**: Token Bucket 용량 부족 또는 Position 계산 오류
- **403 > 50%**: Frontend 로직 오류 (ready_for_entry 무시)

---

## 🎯 결론

**Enter API 403 에러는 정상적인 시스템 동작입니다:**
1. **Position > 100**: 아직 순번이 안 됨
2. **Wait Time 부족**: 최소 대기 시간 미충족
3. **Rate Limiting**: Admission Control 보호 메커니즘

**해결 방법:**
- `/queue/status`로 `ready_for_entry: true` 확인 후 Enter 시도
- 403 시 1초 후 재시도 (Token Bucket refill)
- Position이 100 이하로 내려올 때까지 polling

**Frontend는 403을 에러로 취급하지 말고, "잠시만 기다려주세요" 메시지 표시 권장**

---

*Last Updated: 2025-10-08*
*Related: internal/routes/queue.go:556-614 (isEligibleForEntry)*

