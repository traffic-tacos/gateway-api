# Status API 502/504 에러 완전 해결 가이드

## 📊 **문제 상황**

### 발생 시점
- 2025-10-08 15:00-15:30 (30분간 지속)
- k6 부하 테스트 중 Status API 대량 호출

### 에러 통계
```
총 호출: 150,000회
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
✅ 성공: ~53,000회 (35.5%)
🔴 502: 89,000회 (59.3%) ← 압도적!
🔴 504: 7,300회 (4.9%)
🔴 500: 373회 (0.2%)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
총 에러율: 64.5% 🚨
평균 응답 시간: 9-15초
```

### 사용자 영향
- Status API 응답 없음 → 대기열 위치 확인 불가
- 502 Bad Gateway → Pod가 NotReady 상태
- 504 Gateway Timeout → 응답 지연
- 서비스 사실상 불가능

---

## 🔍 **근본 원인 분석**

### 1️⃣ Position Index 키 불일치 → KEYS() 호출 폭증

#### 문제 코드
```go
// Join API (internal/routes/queue.go:181)
positionIndexKey := fmt.Sprintf("position_index:{%s}", req.EventID)

// CalculateApproximatePosition (internal/queue/streams_optimized.go:114)
positionKey := fmt.Sprintf("queue:event:{%s}:position", eventID)  // ❌ 다른 키!
```

#### 발생 과정
```
1. Status API 호출
   ↓
2. calculatePositionAndETA()
   ↓
3. CalculateApproximatePosition() 
   → ZRANK position_index:{evt}     ❌ 실패 (키 없음)
   ↓
4. Fallback to Stream calculation
   ↓
5. GetGlobalPosition()
   → redis.KEYS("stream:event:{evt}:user:*")  🔴 O(N) 블로킹!
   ↓
6. Redis CPU 100%, 응답 15초 지연
   ↓
7. 사용자는 504 Timeout 🔴
```

#### Redis KEYS() 명령의 문제점
```go
// streams.go:108
keys, err := sq.redis.Keys(ctx, pattern).Result()

문제점:
1. O(N) 복잡도 - 모든 키를 스캔
2. Redis 완전 블로킹 - 다른 명령도 멈춤
3. Cluster Mode에서 더 느림 (모든 샤드 스캔)
4. 150,000 호출 × KEYS = Redis CPU 100%
```

---

### 2️⃣ Redis Cluster Mode Hash Tag 누락

#### 문제 코드
```go
// Hash tag 없음 (다른 샤드로 분산!)
eventQueueKey := fmt.Sprintf("queue:event:%s", req.EventID)

// Hash tag 있음 (같은 샤드)
positionIndexKey := fmt.Sprintf("position_index:{%s}", req.EventID)
streamKey := fmt.Sprintf("stream:event:{%s}:user:%s", eventID, userID)
```

#### Hash Slot 분산 문제
```
Redis Cluster Mode (3 샤드):

키 이름                           Hash Slot    샤드
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
queue:event:evt_123              67890        Shard 2  ❌
position_index:{evt_123}         12345        Shard 1  ✅
stream:event:{evt_123}:user:*    12345        Shard 1  ✅

결과:
  - Pipeline 사용 시 CROSSSLOT 에러 발생!
  - 트랜잭션 원자성 보장 불가
  - 네트워크 왕복 증가 (샤드 간 통신)
```

#### Hash Tag 작동 원리
```
Redis Cluster는 {} 안의 문자열만으로 slot 계산:

queue:event:{evt_123}
            ^^^^^^^^^ 이 부분만 해싱!
            
모든 키가 같은 슬롯으로:
  - queue:event:{evt_123}          → slot 12345
  - position_index:{evt_123}       → slot 12345
  - stream:event:{evt_123}:user:*  → slot 12345
  
→ 같은 샤드에 배치! ✅
```

---

### 3️⃣ Redis Health Check Timeout 부족

#### 문제 설정
```yaml
# deployment.yaml (기존)
readinessProbe:
  timeoutSeconds: 3          # ❌ 너무 짧음!
  periodSeconds: 5
  failureThreshold: 6

# redis.go (기존)
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)  // ❌
```

#### 발생 과정
```
1. Redis CPU 100% (KEYS 호출로 인해)
   ↓
2. Health Check 18.8초 소요
   ↓
3. Readiness Probe Timeout (3초 초과)
   ↓
4. Pod가 NotReady로 마킹
   ↓
5. ALB가 NotReady Pod로 트래픽 전송
   ↓
6. 사용자는 502 Bad Gateway 🔴
```

#### 로그 증거
```json
{
  "error": "context deadline exceeded",
  "level": "error",
  "msg": "Redis health check failed",
  "ts": "2025-10-08T15:22:16.416Z"
}
{
  "duration_ms": 18814,
  "path": "/readyz",
  "status_code": 503,
  "user_agent": "kube-probe/1.33+"
}
```

---

## ✅ **해결 방법**

### Solution 1: Position Index 키 통일 (Commit: b472fb0)

#### 변경 내용
```go
// Before (streams_optimized.go:114)
positionKey := fmt.Sprintf("queue:event:{%s}:position", eventID)

// After
positionKey := fmt.Sprintf("position_index:{%s}", eventID)
```

#### 효과
```
KEYS() 호출 완전 제거!

Before:
  CalculateApproximatePosition → 실패
  → Fallback to GetGlobalPosition
  → redis.KEYS() 🔴
  → 15초 지연

After:
  CalculateApproximatePosition → 성공 ✅
  → redis.ZRANK() (O(log N))
  → 10-50ms 응답!
```

---

### Solution 2: Hash Tag 추가 (Commit: 7bb809f)

#### 변경 내용
```go
// Before (queue.go:168, 259, 378, 443, 532, 558)
eventQueueKey := fmt.Sprintf("queue:event:%s", req.EventID)

// After
eventQueueKey := fmt.Sprintf("queue:event:{%s}", req.EventID)
```

#### 효과
```
모든 이벤트 관련 키가 같은 샤드에!

Before:
  queue:event:evt_123          → Shard 2  ❌
  position_index:{evt_123}     → Shard 1
  stream:event:{evt_123}:*     → Shard 1
  → Pipeline CROSSSLOT 에러!

After:
  queue:event:{evt_123}        → Shard 1  ✅
  position_index:{evt_123}     → Shard 1  ✅
  stream:event:{evt_123}:*     → Shard 1  ✅
  → Pipeline 안전! 원자성 보장!
```

---

### Solution 3: Timeout 증가 (Commit: 88bdb20)

#### 변경 내용

**1. Redis Health Check (redis.go:309)**
```go
// Before
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)

// After
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
```

**2. Readiness Probe (deployment.yaml:215-223)**
```yaml
# Before
readinessProbe:
  initialDelaySeconds: 10
  periodSeconds: 5
  timeoutSeconds: 3
  failureThreshold: 6

# After
readinessProbe:
  initialDelaySeconds: 20    # ⬆️ Redis 연결 대기
  periodSeconds: 10          # ⬆️ 체크 간격 증가
  timeoutSeconds: 10         # ⬆️ 타임아웃 증가
  failureThreshold: 3        # ⬇️ 빠른 감지
```

#### 효과
```
Pod 안정화!

Before:
  Health Check: 18.8s
  Timeout: 3s
  → Pod NotReady → 502 에러

After:
  Health Check: <10s
  Timeout: 10s
  → Pod Ready 유지 → 정상 응답 ✅
```

---

## 📊 **성능 개선 결과**

### Before (문제 발생 시)
```
Status API 성능:
  - 응답 시간: 9,000-15,000ms
  - 에러율: 64.5%
  - Redis CPU: 99.8%
  - Redis Commands: 150,000 KEYS/min
  - Pod 상태: NotReady (70개 중 40개)
  
사용자 경험:
  - 502: 59.3% (Pod NotReady)
  - 504: 4.9% (Timeout)
  - 대기열 위치 확인 불가
```

### After (수정 후 예상)
```
Status API 성능:
  - 응답 시간: 10-50ms (300배 개선!)
  - 에러율: <2%
  - Redis CPU: 10-20%
  - Redis Commands: 150,000 ZRANK/min (O(log N))
  - Pod 상태: Ready (모든 Pod)
  
사용자 경험:
  - 502: <0.5% (Pod 안정)
  - 504: <0.1% (빠른 응답)
  - 실시간 위치 확인 가능 ✅
```

---

## 🔧 **검증 방법**

### 1. Redis 명령 모니터링
```bash
# Redis에서 KEYS 명령이 사라졌는지 확인
redis-cli MONITOR | grep KEYS
# 결과: 아무것도 나오지 않아야 함 ✅

# ZRANK 명령 사용 확인
redis-cli MONITOR | grep ZRANK
# 결과: Status API 호출 시마다 ZRANK 보여야 함
```

### 2. CloudWatch Metrics 확인
```
메트릭 이름: EngineCPUUtilization
기대값: 10-20% (기존 99.8%에서 개선)

메트릭 이름: CommandsProcessed
KEYS 명령: 0 (완전 제거)
ZRANK 명령: 증가 (정상)
```

### 3. Pod 상태 확인
```bash
# NotReady Pod가 없어야 함
kubectl get pods -n tacos-app -l app=gateway-api | grep -v "1/1"
# 결과: 아무것도 나오지 않아야 함

# Readiness probe 실패 이벤트 확인
kubectl get events -n tacos-app --field-selector reason=Unhealthy
# 결과: readiness probe 관련 이벤트 없어야 함
```

### 4. Status API 응답 시간 테스트
```bash
# 직접 테스트
for i in {1..10}; do
  time curl -s "https://api.traffictacos.store/api/v1/queue/status?token=test" \
    -H "Origin: https://www.traffictacos.store"
done

# 기대값: 각 요청이 100ms 이하
```

### 5. Hash Tag 검증
```bash
# Redis Cluster에서 키들이 같은 슬롯에 있는지 확인
redis-cli CLUSTER KEYSLOT "queue:event:{evt_123}"
redis-cli CLUSTER KEYSLOT "position_index:{evt_123}"
redis-cli CLUSTER KEYSLOT "stream:event:{evt_123}:user:john"

# 결과: 모두 같은 slot 번호 (예: 12345) ✅
```

---

## 📝 **교훈 (Lessons Learned)**

### 1. Redis KEYS는 절대 프로덕션에서 사용 금지
```
KEYS 명령의 문제:
  - O(N) 복잡도
  - Redis 완전 블로킹
  - Cluster Mode에서 더 느림
  
대안:
  - SCAN: 커서 기반 non-blocking
  - ZRANK: O(log N), 매우 빠름
  - Secondary Index: 미리 구축된 ZSET/Hash
```

### 2. Redis Cluster Mode는 Hash Tag 필수
```
Hash Tag 규칙:
  - {key} 안의 문자열만 해싱
  - 같은 hash tag → 같은 샤드
  - Pipeline/Transaction 사용 시 필수!
  
체크리스트:
  ✅ 관련 키들이 모두 같은 hash tag 사용?
  ✅ Pipeline 사용 전 CLUSTER KEYSLOT 확인?
  ✅ CROSSSLOT 에러 발생 가능성 검토?
```

### 3. Fallback은 항상 검증 필수
```
문제의 Fallback:
  - Fast path 실패 시 Stream calculation
  - Stream은 KEYS() 사용
  - KEYS()는 프로덕션 금지 명령!
  
교훈:
  ✅ Fallback도 성능 테스트 필수
  ✅ O(N) 명령은 절대 사용 금지
  ✅ Fast path 성공률 모니터링
```

### 4. Timeout은 여유있게 설정
```
Health Check Timeout 공식:
  readinessProbe.timeoutSeconds 
    ≥ RedisHealthCheck timeout
    ≥ 실제 소요 시간 × 2
  
예:
  실제 소요: 5s
  → Health Check: 10s
  → Readiness Probe: 10s
  
여유가 있어야 일시적 지연에도 안정적!
```

### 5. 키 이름 불일치는 치명적
```
문제:
  - Join API: position_index:{evt}
  - Status API: queue:event:{evt}:position
  → 완전히 다른 키!
  
교훈:
  ✅ 키 이름 상수화 (constants.go)
  ✅ 코드 리뷰 시 키 이름 검증
  ✅ 통합 테스트에서 실제 Redis 데이터 확인
```

---

## 🚀 **재발 방지 대책**

### 1. Code Review 체크리스트
```markdown
- [ ] Redis KEYS/SMEMBERS 명령 사용 여부 (절대 금지!)
- [ ] Cluster Mode에서 Hash Tag 일관성
- [ ] Timeout 설정이 충분한지 (실제 × 2)
- [ ] Fallback 로직의 성능 검증
- [ ] 키 이름 일관성 (Join/Status/Enter 모두 동일)
```

### 2. 모니터링 알람 추가
```yaml
# CloudWatch Alarm: KEYS 명령 감지
Metric: Redis KEYS Command Count
Threshold: > 0
Action: SNS 알림 + PagerDuty

# CloudWatch Alarm: Status API 지연
Metric: API Latency P95
Threshold: > 500ms
Action: SNS 알림

# CloudWatch Alarm: Pod NotReady
Metric: NotReady Pod Count
Threshold: > 5
Action: SNS 알림
```

### 3. 통합 테스트 강화
```go
// tests/integration/status_api_test.go
func TestStatusAPIPerformance(t *testing.T) {
    // 1. Redis Cluster Mode 시뮬레이션
    // 2. 대량 Status 호출 (10,000 req/s)
    // 3. P95 latency < 100ms 검증
    // 4. Redis CPU < 30% 검증
    // 5. Pod Ready 상태 유지 검증
}

func TestHashTagConsistency(t *testing.T) {
    // 모든 이벤트 관련 키의 slot 동일성 검증
}
```

### 4. 자동화된 성능 회귀 테스트
```yaml
# .github/workflows/performance-test.yml
name: Status API Performance Test
on: [pull_request]
jobs:
  perf-test:
    - name: Run k6 load test
      run: k6 run --vus 1000 --duration 1m status_api_test.js
    - name: Check latency
      run: |
        if [ $P95_LATENCY -gt 200 ]; then
          echo "Performance regression detected!"
          exit 1
        fi
```

---

## 📚 **관련 문서**

- [Redis CPU Optimization](./REDIS_CPU_OPTIMIZATION.md)
- [Redis Cluster Hash Tag Fix](./REDIS_CLUSTER_HASH_TAG_FIX.md)
- [Redis Pipeline Optimization](./REDIS_JOIN_PIPELINE_OPTIMIZATION.md)
- [Redis OTEL Connection Analysis](./REDIS_OTEL_CONNECTION_ANALYSIS.md)

---

## 📅 **변경 이력**

| 날짜 | Commit | 변경 내용 |
|---|---|---|
| 2025-10-08 | 88bdb20 | Redis health check timeout 증가 (2s → 10s) |
| 2025-10-08 | b472fb0 | Position index 키 불일치 수정 |
| 2025-10-08 | 7bb809f | queue:event 키에 hash tag 추가 |
| 2025-10-08 | 368fc4e | Readiness probe timeout 증가 (3s → 10s) |

---

**문서 작성**: 2025-10-08  
**최종 수정**: 2025-10-08  
**작성자**: Traffic Tacos Platform Team  
**심각도**: 🔴 Critical  
**우선순위**: P0 (최고 우선순위)

