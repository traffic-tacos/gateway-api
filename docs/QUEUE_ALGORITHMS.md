# 대기열 시스템 핵심 알고리즘 설계

## 🎯 핵심 과제

30k RPS 트래픽을 안전하게 제어하면서 사용자에게 정확한 대기 시간을 제공하는 것

### 두 가지 핵심 문제

1. **ETA 계산**: 사용자에게 얼마나 기다려야 하는지 정확히 알려주기
2. **Admission Control**: 백엔드가 감당할 수 있는 속도로만 입장시키기

---

## 📊 1. ETA (Estimated Time to Admission) 계산 알고리즘

### ❌ 현재 구현 (너무 단순)

```go
// 문제: 고정 속도 가정
eta := position * 2  // 1명당 2초 고정
```

**문제점:**
- 트래픽 변화 반영 안됨
- 실제 처리 속도 무시
- 백엔드 부하 고려 안됨
- 사용자 신뢰도 하락

---

### ✅ 방법 1: Exponential Moving Average (EMA) - **추천**

실시간 처리 속도를 추적하여 적응형 ETA 계산

```go
package queue

import (
    "context"
    "fmt"
    "time"
    "github.com/redis/go-redis/v9"
)

type AdmissionMetrics struct {
    redisClient *redis.Client
    eventID     string
}

// EMA 기반 처리 속도 계산
func (m *AdmissionMetrics) GetAdmissionRate(ctx context.Context) (float64, error) {
    key := fmt.Sprintf("metrics:admission:%s", m.eventID)
    
    // Redis Time Series 또는 Sorted Set 사용
    // 최근 1분간 입장 이력 조회
    now := time.Now().Unix()
    oneMinuteAgo := now - 60
    
    count, err := m.redisClient.ZCount(ctx, key, 
        fmt.Sprintf("%d", oneMinuteAgo),
        fmt.Sprintf("%d", now)).Result()
    
    if err != nil {
        return 0, err
    }
    
    // 초당 입장 인원 = 1분간 입장 수 / 60초
    rate := float64(count) / 60.0
    
    return rate, nil
}

// ETA 계산 (Exponential Moving Average)
func (q *QueueHandler) calculateSmartETA(ctx context.Context, eventID string, position int) int {
    metrics := &AdmissionMetrics{
        redisClient: q.redisClient,
        eventID:     eventID,
    }
    
    // 실시간 처리 속도 조회
    admissionRate, err := metrics.GetAdmissionRate(ctx)
    if err != nil || admissionRate == 0 {
        // Fallback: 기본값 사용
        return position * 2
    }
    
    // ETA = 내 순서 / 초당 처리 인원
    eta := float64(position) / admissionRate
    
    // 신뢰도 향상을 위해 10% 여유 추가
    eta = eta * 1.1
    
    // 최소 1초, 최대 600초 제한
    if eta < 1 {
        return 1
    } else if eta > 600 {
        return 600
    }
    
    return int(eta)
}

// 입장 이벤트 기록 (Admission Worker에서 호출)
func (m *AdmissionMetrics) RecordAdmission(ctx context.Context, userID string) error {
    key := fmt.Sprintf("metrics:admission:%s", m.eventID)
    
    // Sorted Set에 현재 시간을 Score로 저장
    now := time.Now().Unix()
    err := m.redisClient.ZAdd(ctx, key, redis.Z{
        Score:  float64(now),
        Member: userID,
    }).Err()
    
    if err != nil {
        return err
    }
    
    // 1시간 이상 된 데이터 정리 (메모리 절약)
    oneHourAgo := now - 3600
    m.redisClient.ZRemRangeByScore(ctx, key, "-inf", fmt.Sprintf("%d", oneHourAgo))
    
    return nil
}
```

**Redis 구조:**
```redis
# 입장 이력 저장 (ZSet)
Key: metrics:admission:evt_2025_1001
Score: Unix timestamp
Member: user_id

# 예시:
ZADD metrics:admission:evt_2025_1001 1704103210 "user123"
ZADD metrics:admission:evt_2025_1001 1704103215 "user456"

# 최근 1분간 입장 수 조회
ZCOUNT metrics:admission:evt_2025_1001 1704103150 1704103210
>>> 42  # 1분간 42명 입장 → 초당 0.7명
```

**장점:**
- ✅ 실시간 트래픽 반영
- ✅ 구현 간단
- ✅ Redis만으로 가능
- ✅ 메모리 효율적 (1시간 데이터만 유지)

---

### ✅ 방법 2: Sliding Window Counter - **더 정확함**

여러 시간대의 처리 속도를 가중 평균

```go
type SlidingWindowMetrics struct {
    redisClient *redis.Client
    eventID     string
}

func (m *SlidingWindowMetrics) GetWeightedAdmissionRate(ctx context.Context) float64 {
    now := time.Now()
    
    // 여러 시간 윈도우 조회 (1분, 5분, 15분)
    rate1min := m.getAdmissionRateForWindow(ctx, now, 60)
    rate5min := m.getAdmissionRateForWindow(ctx, now, 300)
    rate15min := m.getAdmissionRateForWindow(ctx, now, 900)
    
    // 가중 평균: 최근일수록 높은 가중치
    // 50% 1분, 30% 5분, 20% 15분
    weightedRate := rate1min*0.5 + rate5min*0.3 + rate15min*0.2
    
    return weightedRate
}

func (m *SlidingWindowMetrics) getAdmissionRateForWindow(ctx context.Context, now time.Time, windowSeconds int64) float64 {
    key := fmt.Sprintf("metrics:admission:%s", m.eventID)
    
    start := now.Unix() - windowSeconds
    end := now.Unix()
    
    count, err := m.redisClient.ZCount(ctx, key,
        fmt.Sprintf("%d", start),
        fmt.Sprintf("%d", end)).Result()
    
    if err != nil || count == 0 {
        return 0
    }
    
    return float64(count) / float64(windowSeconds)
}
```

**장점:**
- ✅ 더 정확한 예측
- ✅ 급격한 변화에 덜 민감
- ✅ 트렌드 반영

**단점:**
- ❌ 계산 복잡도 증가
- ❌ Redis 조회 3배 증가

---

### ✅ 방법 3: Machine Learning 기반 (고급) - **선택적**

과거 데이터로 패턴 학습

```python
# Python 예시 (별도 서비스)
import numpy as np
from sklearn.ensemble import GradientBoostingRegressor

class ETAPredictor:
    def __init__(self):
        self.model = GradientBoostingRegressor()
        
    def train(self, historical_data):
        """
        Features:
        - position: 대기 순번
        - hour_of_day: 시간대 (0-23)
        - day_of_week: 요일 (0-6)
        - current_admission_rate: 현재 처리 속도
        - queue_length: 총 대기 인원
        
        Target:
        - actual_wait_time: 실제 대기 시간
        """
        X = historical_data[['position', 'hour_of_day', 'day_of_week', 
                            'current_admission_rate', 'queue_length']]
        y = historical_data['actual_wait_time']
        
        self.model.fit(X, y)
    
    def predict_eta(self, position, current_time, admission_rate, queue_length):
        features = np.array([[
            position,
            current_time.hour,
            current_time.weekday(),
            admission_rate,
            queue_length
        ]])
        
        eta = self.model.predict(features)[0]
        return int(eta)
```

**장점:**
- ✅ 가장 정확
- ✅ 복잡한 패턴 학습 가능
- ✅ 시간대/요일 패턴 반영

**단점:**
- ❌ 구현 복잡도 매우 높음
- ❌ 별도 ML 서비스 필요
- ❌ 학습 데이터 필요

---

## 🚪 2. Admission Control (입장 제어) 알고리즘

### ❌ 현재 구현 (문제 있음)

```go
// 문제: 10초만 기다리면 누구나 입장 가능
func (q *QueueHandler) isEligibleForEntry(ctx context.Context, queueData *QueueData) bool {
    waitTime := time.Since(queueData.JoinedAt)
    return waitTime > 10*time.Second
}
```

**문제점:**
- 백엔드 부하 무시
- 동시에 수천명 입장 가능
- Admission Rate 제어 불가
- 30k RPS를 제어할 수 없음

---

### ✅ 방법 1: Token Bucket Algorithm - **추천**

일정한 속도로 토큰을 생성하고, 토큰이 있을 때만 입장 허가

```go
package queue

import (
    "context"
    "fmt"
    "time"
    "github.com/redis/go-redis/v9"
)

type TokenBucketAdmission struct {
    redisClient *redis.Client
    eventID     string
    capacity    int     // 버킷 최대 용량
    refillRate  float64 // 초당 토큰 생성 속도
}

func NewTokenBucketAdmission(redis *redis.Client, eventID string) *TokenBucketAdmission {
    return &TokenBucketAdmission{
        redisClient: redis,
        eventID:     eventID,
        capacity:    100,   // 최대 100명 버스트
        refillRate:  10.0,  // 초당 10명 입장 (안정 상태)
    }
}

// Token Bucket 알고리즘 (Redis Lua Script)
var tokenBucketLuaScript = `
local key = KEYS[1]
local capacity = tonumber(ARGV[1])
local refill_rate = tonumber(ARGV[2])
local requested = tonumber(ARGV[3])
local now = tonumber(ARGV[4])

-- 현재 버킷 상태 조회
local bucket = redis.call('HMGET', key, 'tokens', 'last_refill')
local tokens = tonumber(bucket[1]) or capacity
local last_refill = tonumber(bucket[2]) or now

-- 경과 시간 계산
local elapsed = now - last_refill

-- 토큰 리필 (경과 시간 * 리필 속도)
local new_tokens = tokens + (elapsed * refill_rate)
if new_tokens > capacity then
    new_tokens = capacity
end

-- 토큰이 충분한지 확인
if new_tokens >= requested then
    new_tokens = new_tokens - requested
    redis.call('HMSET', key, 'tokens', new_tokens, 'last_refill', now)
    redis.call('EXPIRE', key, 3600)  -- 1시간 TTL
    return 1  -- 입장 허가
else
    redis.call('HMSET', key, 'tokens', new_tokens, 'last_refill', now)
    redis.call('EXPIRE', key, 3600)
    return 0  -- 입장 거부
end
`

func (t *TokenBucketAdmission) TryAdmit(ctx context.Context, userID string) (bool, error) {
    key := fmt.Sprintf("admission:bucket:%s", t.eventID)
    
    // Lua 스크립트로 원자적 실행
    result, err := t.redisClient.Eval(ctx, tokenBucketLuaScript, 
        []string{key},
        t.capacity,
        t.refillRate,
        1,  // 1개 토큰 요청
        time.Now().Unix()).Result()
    
    if err != nil {
        return false, err
    }
    
    // 1이면 허가, 0이면 거부
    return result.(int64) == 1, nil
}

// QueueHandler에 통합
func (q *QueueHandler) isEligibleForEntry(ctx context.Context, queueData *QueueData, waitingToken string) bool {
    // 1. 최소 대기 시간 체크 (5초)
    waitTime := time.Since(queueData.JoinedAt)
    if waitTime < 5*time.Second {
        return false
    }
    
    // 2. 큐 위치 체크 (상위 100명만)
    eventQueueKey := fmt.Sprintf("queue:event:%s", queueData.EventID)
    rank, err := q.redisClient.ZRank(ctx, eventQueueKey, waitingToken).Result()
    if err != nil || int(rank) >= 100 {
        return false
    }
    
    // 3. Token Bucket 체크 (핵심!)
    bucket := NewTokenBucketAdmission(q.redisClient, queueData.EventID)
    admitted, err := bucket.TryAdmit(ctx, queueData.UserID)
    
    return admitted
}
```

**Redis 구조:**
```redis
# Token Bucket 상태
Key: admission:bucket:evt_2025_1001
Type: Hash
Fields:
  tokens: 87.5          # 현재 남은 토큰
  last_refill: 1704103210  # 마지막 리필 시간

# 토큰 소비 예시
HMGET admission:bucket:evt_2025_1001 tokens last_refill
>>> ["87.5", "1704103210"]

# 5초 후 (50개 토큰 리필)
# tokens = 87.5 + (5 * 10.0) = 137.5 → capacity(100)로 제한
```

**동작 원리:**
```
초기: [●●●●●●●●●●] 100개 토큰
      ↓ 10명 입장
10초 후: [●●●●●●●●●○] 90개 토큰
      ↓ 1초 경과 (10개 리필)
11초 후: [●●●●●●●●●●] 100개 토큰
```

**장점:**
- ✅ **버스트 트래픽 허용**: 갑자기 많은 사람이 들어와도 버킷 용량만큼 처리
- ✅ **안정 상태 제어**: 평균적으로 초당 10명만 입장
- ✅ **원자적 실행**: Lua 스크립트로 race condition 방지
- ✅ **백엔드 보호**: 과부하 방지

---

### ✅ 방법 2: Leaky Bucket Algorithm - **더 엄격함**

일정한 속도로만 출력 (버스트 불가)

```go
type LeakyBucketAdmission struct {
    redisClient *redis.Client
    eventID     string
    rate        float64  // 초당 처리 속도
}

var leakyBucketLuaScript = `
local key = KEYS[1]
local rate = tonumber(ARGV[1])  -- 초당 처리 속도
local now = tonumber(ARGV[2])

-- 마지막 입장 시간 조회
local last_admission = tonumber(redis.call('GET', key)) or 0

-- 다음 입장 가능 시간 계산
local next_admission = last_admission + (1.0 / rate)

if now >= next_admission then
    -- 입장 허가
    redis.call('SET', key, now)
    redis.call('EXPIRE', key, 3600)
    return 1
else
    -- 입장 거부
    return 0
end
`

func (l *LeakyBucketAdmission) TryAdmit(ctx context.Context) (bool, error) {
    key := fmt.Sprintf("admission:leaky:%s", l.eventID)
    
    result, err := l.redisClient.Eval(ctx, leakyBucketLuaScript,
        []string{key},
        l.rate,
        time.Now().Unix()).Result()
    
    if err != nil {
        return false, err
    }
    
    return result.(int64) == 1, nil
}
```

**동작 원리:**
```
초당 10명 제한:
0.0초: ✅ 입장
0.1초: ✅ 입장
0.2초: ✅ 입장
...
1.0초: ✅ 입장 (10번째)
1.05초: ❌ 거부 (너무 빠름)
1.1초: ✅ 입장 (11번째)
```

**장점:**
- ✅ **정확한 속도 제어**: 버스트 불가
- ✅ **백엔드 안정성 최대화**: 예측 가능한 부하
- ✅ **구현 간단**: Token Bucket보다 단순

**단점:**
- ❌ **사용자 경험 저하**: 버스트 처리 불가
- ❌ **유연성 낮음**: 갑작스러운 트래픽 대응 어려움

---

### ✅ 방법 3: Adaptive Rate Limiting - **최고급**

백엔드 상태를 실시간으로 모니터링하여 동적 조절

```go
type AdaptiveAdmission struct {
    redisClient     *redis.Client
    eventID         string
    minRate         float64  // 최소 입장 속도
    maxRate         float64  // 최대 입장 속도
    targetP95Latency float64 // 목표 P95 지연시간 (ms)
}

func (a *AdaptiveAdmission) GetBackendHealth(ctx context.Context) float64 {
    // Prometheus/CloudWatch에서 실시간 메트릭 조회
    // - P95 지연시간
    // - 에러율
    // - CPU/Memory 사용률
    
    p95Latency := a.queryPrometheusP95(ctx)
    errorRate := a.queryPrometheusErrorRate(ctx)
    
    // 백엔드가 건강하면 1.0, 문제 있으면 0.0
    if p95Latency > a.targetP95Latency*2 || errorRate > 0.05 {
        return 0.0  // 매우 나쁨
    } else if p95Latency > a.targetP95Latency {
        return 0.5  // 나쁨
    } else {
        return 1.0  // 좋음
    }
}

func (a *AdaptiveAdmission) GetCurrentRate(ctx context.Context) float64 {
    health := a.GetBackendHealth(ctx)
    
    // 건강도에 따라 입장 속도 조절
    currentRate := a.minRate + (a.maxRate-a.minRate)*health
    
    // Redis에 현재 속도 저장 (1분 TTL)
    key := fmt.Sprintf("admission:adaptive:%s:rate", a.eventID)
    a.redisClient.Set(ctx, key, currentRate, 60*time.Second)
    
    return currentRate
}

// Token Bucket과 결합
func (a *AdaptiveAdmission) TryAdmit(ctx context.Context) (bool, error) {
    // 현재 속도 조회
    currentRate := a.GetCurrentRate(ctx)
    
    // Token Bucket에 동적 속도 적용
    bucket := &TokenBucketAdmission{
        redisClient: a.redisClient,
        eventID:     a.eventID,
        capacity:    100,
        refillRate:  currentRate,  // 동적!
    }
    
    return bucket.TryAdmit(ctx, "")
}
```

**동작 시나리오:**

```
[정상 상태]
Backend P95: 50ms (목표: 100ms)
→ Health: 1.0
→ Rate: 50명/초 (최대)
→ 빠른 입장

[부하 증가]
Backend P95: 120ms (목표 초과)
→ Health: 0.5
→ Rate: 25명/초 (중간)
→ 입장 속도 감소

[장애 상태]
Backend P95: 500ms, Error: 10%
→ Health: 0.0
→ Rate: 10명/초 (최소)
→ 백엔드 보호 모드
```

**장점:**
- ✅ **최적의 처리량**: 백엔드 상태에 따라 자동 조절
- ✅ **장애 방지**: 과부하 자동 감지 및 보호
- ✅ **사용자 경험 최적화**: 백엔드가 건강하면 빠르게 입장

**단점:**
- ❌ **구현 복잡도 높음**: Prometheus/CloudWatch 통합 필요
- ❌ **지연 시간**: 메트릭 조회 오버헤드

---

## 🏆 최종 추천: 하이브리드 접근법

### 단계별 구현 전략

#### Phase 1: 기본 (즉시 적용 가능) ⭐⭐⭐

```go
func (q *QueueHandler) isEligibleForEntry(ctx context.Context, queueData *QueueData, waitingToken string) bool {
    // 1. 최소 대기 시간 (5초)
    if time.Since(queueData.JoinedAt) < 5*time.Second {
        return false
    }
    
    // 2. 큐 위치 (상위 100명)
    eventQueueKey := fmt.Sprintf("queue:event:%s", queueData.EventID)
    rank, _ := q.redisClient.ZRank(ctx, eventQueueKey, waitingToken).Result()
    if int(rank) >= 100 {
        return false
    }
    
    // 3. Token Bucket (초당 10명, 버스트 100명)
    bucket := NewTokenBucketAdmission(q.redisClient, queueData.EventID)
    admitted, _ := bucket.TryAdmit(ctx, queueData.UserID)
    
    return admitted
}

func (q *QueueHandler) calculateSmartETA(ctx context.Context, eventID string, position int) int {
    // Exponential Moving Average 기반
    metrics := &AdmissionMetrics{
        redisClient: q.redisClient,
        eventID:     eventID,
    }
    
    rate, _ := metrics.GetAdmissionRate(ctx)
    if rate == 0 {
        rate = 5.0  // 기본값: 초당 5명
    }
    
    eta := float64(position) / rate * 1.1  // 10% 여유
    
    if eta < 1 {
        return 1
    } else if eta > 600 {
        return 600
    }
    
    return int(eta)
}
```

**예상 성능:**
- ETA 정확도: 80-90%
- 백엔드 보호: ✅
- 구현 난이도: 낮음

---

#### Phase 2: 중급 (1-2주 개발) ⭐⭐⭐⭐

```go
// Sliding Window + Token Bucket
func (q *QueueHandler) calculateAdvancedETA(ctx context.Context, eventID string, position int) int {
    metrics := &SlidingWindowMetrics{...}
    
    // 1분, 5분, 15분 가중 평균
    rate := metrics.GetWeightedAdmissionRate(ctx)
    
    // 시간대별 가중치 적용
    hourWeight := q.getHourlyWeight(time.Now().Hour())
    adjustedRate := rate * hourWeight
    
    eta := float64(position) / adjustedRate * 1.1
    
    return int(eta)
}

func (q *QueueHandler) getHourlyWeight(hour int) float64 {
    // 트래픽 패턴 반영
    weights := map[int]float64{
        0: 0.3,  // 새벽: 낮은 트래픽
        9: 1.0,  // 오전: 정상
        12: 1.5, // 점심: 높은 트래픽
        18: 2.0, // 저녁: 피크 트래픽
        22: 0.8, // 밤: 낮아짐
    }
    
    if w, ok := weights[hour]; ok {
        return w
    }
    return 1.0
}
```

**예상 성능:**
- ETA 정확도: 90-95%
- 시간대 패턴 반영: ✅
- 구현 난이도: 중간

---

#### Phase 3: 고급 (1개월 개발) ⭐⭐⭐⭐⭐

```go
// Adaptive Rate + ML 예측
func (q *QueueHandler) TryAdmitWithAdaptiveRate(ctx context.Context, queueData *QueueData) bool {
    adaptive := &AdaptiveAdmission{
        redisClient:      q.redisClient,
        eventID:          queueData.EventID,
        minRate:          5.0,   // 최소: 초당 5명
        maxRate:          50.0,  // 최대: 초당 50명
        targetP95Latency: 100.0, // 목표: 100ms
    }
    
    return adaptive.TryAdmit(ctx)
}

// ML 기반 ETA (별도 Python 서비스 호출)
func (q *QueueHandler) calculateMLETA(ctx context.Context, eventID string, position int) int {
    // gRPC로 ML 서비스 호출
    resp, err := q.mlClient.PredictETA(ctx, &ml.ETARequest{
        EventID:   eventID,
        Position:  int32(position),
        Timestamp: time.Now().Unix(),
    })
    
    if err != nil {
        // Fallback to EMA
        return q.calculateSmartETA(ctx, eventID, position)
    }
    
    return int(resp.Eta)
}
```

**예상 성능:**
- ETA 정확도: 95-99%
- 백엔드 자동 보호: ✅
- 구현 난이도: 높음

---

## 📊 알고리즘 비교표

| 알고리즘 | ETA 정확도 | 백엔드 보호 | 구현 난이도 | 추천도 |
|---|---|---|---|---|
| **고정 속도** | 50% | ❌ | ⭐ | ❌ |
| **EMA** | 85% | ❌ | ⭐⭐ | ✅ |
| **Sliding Window** | 90% | ❌ | ⭐⭐⭐ | ✅ |
| **ML 예측** | 95% | ❌ | ⭐⭐⭐⭐⭐ | ⚠️ |
| **Token Bucket** | - | ✅ | ⭐⭐ | ✅✅ |
| **Leaky Bucket** | - | ✅✅ | ⭐⭐ | ⚠️ |
| **Adaptive Rate** | - | ✅✅✅ | ⭐⭐⭐⭐ | ✅✅ |

---

## 🎯 최종 결론

### 즉시 구현 (1-2일)

**ETA 계산**: Exponential Moving Average
**Admission**: Token Bucket (초당 10명, 버스트 100명)

### 장기 목표 (1-2개월)

**ETA 계산**: Sliding Window + 시간대 가중치
**Admission**: Adaptive Rate Limiting (백엔드 헬스 기반)

### 구현 우선순위

1. ✅ **Token Bucket** (백엔드 보호 최우선)
2. ✅ **EMA 기반 ETA** (사용자 경험 개선)
3. ⏳ **Sliding Window** (정확도 향상)
4. ⏳ **Adaptive Rate** (자동 최적화)
5. 🔮 **ML 예측** (선택적)

---

**작성일**: 2025-10-05  
**작성자**: Traffic Tacos Team
