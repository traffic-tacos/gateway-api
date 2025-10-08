# Redis CPU 100% 최적화 가이드

## 🔴 문제 상황

### 증상
- **Redis CPU**: 99.8% (거의 포화)
- **Gateway API**: `redis: connection pool timeout`, `context deadline exceeded`
- **K8s Pods**: `CrashLoopBackOff` (Redis 연결 실패로 인한 초기화 실패)
- **사용자 영향**: 500 Internal Server Error

### 근본 원인
1. **높은 Redis 명령 빈도**: Join API가 **7개의 개별 Redis 호출**
   - 10,000 RPS * 7 calls = **70,000 commands/sec**
2. **Redis 과부하**: CPU 99.8% → 응답 지연 → Connection pool exhaustion
3. **악순환**: 신규 Pod 부팅 실패 → 더 많은 Pod 생성 시도 → Redis 부하 가중

---

## ⚡ 해결 방안: 3단계 전략

### **1단계: 즉시 적용 (애플리케이션 레벨)** ⏱️ 1-2시간

#### 1.1 Circuit Breaker 패턴 (최우선 권장) ⭐⭐⭐

**목적**: Redis 장애 시 fallback 로직으로 서비스 유지

**구현**:
```go
// internal/middleware/circuit_breaker.go
type CircuitBreaker struct {
    client            redis.UniversalClient
    state             CircuitBreakerState  // CLOSED, OPEN, HALF_OPEN
    failureCount      int
    maxFailures       int           // 5회 연속 실패 시 OPEN
    resetTimeout      time.Duration // 10초 후 HALF_OPEN 재시도
    halfOpenSuccesses int           // 3회 성공 시 CLOSED
}
```

**적용 예시**:
```go
// Before: Direct Redis call
result, err := q.luaExecutor.EnqueueAtomic(ctx, req.EventID, req.UserID, dedupeKey)

// After: With Circuit Breaker
err := circuitBreaker.Execute(ctx, func() error {
    result, err = q.luaExecutor.EnqueueAtomic(ctx, req.EventID, req.UserID, dedupeKey)
    return err
})

if err != nil {
    // Fallback: Return degraded response
    if circuitBreaker.GetState() == StateOpen {
        return c.Status(503).JSON(ErrorResponse{
            Error: ErrorDetail{
                Code:    "SERVICE_UNAVAILABLE",
                Message: "System is temporarily overloaded. Please retry in 10 seconds.",
            },
        })
    }
}
```

**효과**:
- ✅ Redis 장애 시 **500 → 503** (더 의미 있는 에러)
- ✅ Redis 부하 감소 (실패 시 요청 차단)
- ✅ **자동 복구** (HALF_OPEN → CLOSED)

---

#### 1.2 Redis Pipeline 최적화 (이미 적용 완료) ⭐⭐⭐

**문제**: Join API에서 7개의 개별 Redis 호출
```go
// Before: 7 round trips
SET queue:waiting:{token}           // 1
ZADD queue:event:{eventID}         // 2
EXPIRE queue:event:{eventID}       // 3
SET heartbeat:{token}              // 4
ZADD position_index:{eventID}     // 5
EXPIRE position_index:{eventID}   // 6
ZRANK position_index:{eventID}    // 7 (async로 이동)
```

**해결책**: Pipeline으로 배치 처리
```go
pipe := q.redisClient.Pipeline()

// 1. Store queue data
pipe.Set(ctx, queueKey, queueDataBytes, 30*time.Minute)

// 2-3. Add to ZSET with TTL
pipe.ZAdd(ctx, eventQueueKey, redis.Z{Score: score, Member: waitingToken})
pipe.Expire(ctx, eventQueueKey, 1*time.Hour)

// 4. Heartbeat
pipe.Set(ctx, heartbeatKey, "alive", 5*time.Minute)

// 5-6. Position index
pipe.ZAdd(ctx, positionIndexKey, redis.Z{Score: score, Member: waitingToken})
pipe.Expire(ctx, positionIndexKey, 1*time.Hour)

// Execute all in a single round trip
pipe.Exec(ctx)

// 7. ZRANK을 비동기로 처리 (Join 응답 블로킹 방지)
go func() {
    position, _ := q.streamQueue.GetGlobalPosition(bgCtx, req.EventID, req.UserID, result.StreamID)
}()
```

**효과**:
- ✅ **7 calls → 2 calls** (Lua script + Pipeline)
- ✅ Redis 명령 수: **70,000 → 20,000 commands/sec** (71% 감소)
- ✅ 예상 Redis CPU: **99.8% → ~30%**

---

#### 1.3 Connection Pool 최적화 ⭐⭐

**현재 설정**:
```yaml
REDIS_POOL_SIZE: "1000"
REDIS_POOL_TIMEOUT: "10s"
REDIS_MIN_IDLE_CONNS: "100"
REDIS_DIAL_TIMEOUT: "10s"
REDIS_READ_TIMEOUT: "5s"
REDIS_CONN_MAX_IDLE_TIME: "4m"
```

**권장 설정** (고부하 환경):
```yaml
REDIS_POOL_SIZE: "2000"              # ⬆️ 1000 → 2000
REDIS_POOL_TIMEOUT: "15s"            # ⬆️ 10s → 15s
REDIS_MIN_IDLE_CONNS: "200"          # ⬆️ 100 → 200
REDIS_DIAL_TIMEOUT: "15s"            # ⬆️ 10s → 15s
REDIS_READ_TIMEOUT: "8s"             # ⬆️ 5s → 8s
REDIS_WRITE_TIMEOUT: "8s"            # 🆕 추가
REDIS_MAX_RETRIES: "5"               # ⬆️ 3 → 5
REDIS_MIN_RETRY_BACKOFF: "50ms"      # 🆕 추가
REDIS_MAX_RETRY_BACKOFF: "500ms"     # 🆕 추가
```

**효과**:
- ✅ Connection pool timeout 감소
- ✅ 일시적 지연 허용 (retry backoff)
- ✅ Pod 초기화 안정성 향상

---

#### 1.4 Graceful Degradation ⭐

**전략**: Redis 장애 시 필수 기능만 제공

```go
// Queue Join API fallback
if circuitBreaker.GetState() == StateOpen {
    // Return estimated position without Redis
    return c.Status(fiber.StatusAccepted).JSON(JoinQueueResponse{
        WaitingToken: "fallback_" + uuid.New().String(),
        PositionHint: 10000, // Conservative estimate
        Status:       "waiting",
        Message:      "Queue position approximate (system degraded)",
    })
}
```

**효과**:
- ✅ **500 에러 → 부분 기능 제공**
- ✅ 사용자 경험 개선 (완전 실패보다 나음)

---

### **2단계: 단기 최적화 (인프라 레벨)** ⏱️ 1-2일

#### 2.1 ElastiCache 오토스케일링 개선 ⭐⭐⭐

**현재 문제**:
- CPU 70% 트리거 → 신규 노드 추가까지 **5-10분**
- 이미 Redis가 포화 상태 → **너무 늦음**

**해결책 1: 더 공격적인 스케일링**

```hcl
# modules/elasticache/main.tf
resource "aws_appautoscaling_policy" "redis_cpu_target_tracking" {
  target_tracking_scaling_policy_configuration {
    predefined_metric_specification {
      predefined_metric_type = "ElastiCachePrimaryEngineCPUUtilization"
    }
    
    target_value       = 50           # ✅ 70% → 50% (더 빠른 트리거)
    scale_in_cooldown  = 300          # 5분 - scale in 대기
    scale_out_cooldown = 30           # ✅ 60s → 30s (더 빠른 scale out)
  }
}
```

**해결책 2: CloudWatch 알람 + Step Scaling (병렬 실행)**

```hcl
# CPU 80% 초과 시 즉시 +2 노드 추가
resource "aws_cloudwatch_metric_alarm" "redis_cpu_high" {
  alarm_name          = "redis-cpu-critical"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1                    # ✅ 1분 내 판단
  metric_name         = "EngineCPUUtilization"
  namespace           = "AWS/ElastiCache"
  period              = 60
  statistic           = "Average"
  threshold           = 80
  alarm_actions       = [aws_appautoscaling_policy.redis_step_scaling.arn]
}

resource "aws_appautoscaling_policy" "redis_step_scaling" {
  step_scaling_policy_configuration {
    adjustment_type = "ChangeInCapacity"
    cooldown        = 60
    
    step_adjustment {
      metric_interval_lower_bound = 0
      metric_interval_upper_bound = 10
      scaling_adjustment          = 1  # CPU 80-90% → +1 노드
    }
    
    step_adjustment {
      metric_interval_lower_bound = 10
      scaling_adjustment          = 2  # CPU 90%+ → +2 노드
    }
  }
}
```

**효과**:
- ✅ 스케일 아웃 **10분 → 2-3분**
- ✅ 급격한 트래픽 증가 대응
- ✅ CPU 50% 기준으로 예방적 스케일링

---

#### 2.2 Read Replica 활용 강화 ⭐⭐

**현재 상태**: Read Replica 3개 (샤드당 1개)

**최적화 전략**:
1. **Status API는 Read Replica로만 라우팅**
   ```go
   // Status API에서만 ReadOnly 활성화
   if isStatusAPI {
       redisClient = redis.NewClusterClient(&redis.ClusterOptions{
           ReadOnly: true,  // ✅ Replica로만 읽기
       })
   }
   ```

2. **Read Replica 추가** (샤드당 1개 → 2개)
   ```hcl
   resource "aws_elasticache_replication_group" "redis_cluster" {
     replicas_per_node_group = 2  # ✅ 1 → 2
   }
   ```

**효과**:
- ✅ Primary 노드 CPU 부하 분산 (읽기 50% 감소)
- ✅ Status API 성능 개선

---

#### 2.3 Pre-warming + Reserved Capacity ⭐

**문제**: 부하 테스트나 피크 시간 예측 가능

**해결책**: CloudWatch Event + Lambda로 예측적 스케일링

```python
# lambda/pre_scale_redis.py
import boto3

def lambda_handler(event, context):
    """
    티켓 오픈 10분 전 Redis 노드 미리 증설
    """
    autoscaling = boto3.client('application-autoscaling')
    
    # 현재 노드 수 확인
    response = autoscaling.describe_scalable_targets(
        ServiceNamespace='elasticache',
        ResourceIds=['replication-group/traffic-tacos-redis']
    )
    
    current_capacity = response['ScalableTargets'][0]['DesiredCapacity']
    
    # +2 노드 미리 추가
    autoscaling.register_scalable_target(
        ServiceNamespace='elasticache',
        ResourceId='replication-group/traffic-tacos-redis',
        ScalableTargetAction={
            'MinCapacity': current_capacity + 2,
            'MaxCapacity': current_capacity + 2
        }
    )
```

**CloudWatch Event Rule**:
```hcl
resource "aws_cloudwatch_event_rule" "ticket_open_pre_scale" {
  name                = "ticket-open-pre-scale"
  description         = "Scale Redis 10 minutes before ticket open"
  schedule_expression = "cron(50 8 * * ? *)"  # 09:00 오픈 → 08:50 스케일링
}
```

**효과**:
- ✅ 피크 타임 전 **예방적 확장**
- ✅ 오토스케일링 지연 문제 해결

---

### **3단계: 장기 전략 (아키텍처 개선)** ⏱️ 1-4주

#### 3.1 노드 타입 업그레이드 ⭐⭐

**현재**: `cache.r7g.large` (2 vCPU, 13.07 GiB)

**권장**: `cache.r7g.xlarge` (4 vCPU, 26.32 GiB)

**비용 vs 성능**:
```
r7g.large:  $0.141/hour * 3 shards * 2 nodes = $0.846/hour
r7g.xlarge: $0.282/hour * 3 shards * 2 nodes = $1.692/hour

추가 비용: $0.846/hour = $613/month
```

**효과**:
- ✅ CPU 여유 2배 증가
- ✅ Connection 처리 능력 향상
- ✅ 오토스케일링 빈도 감소

---

#### 3.2 샤드 수 증가 (Re-sharding) ⭐⭐⭐

**현재**: 3 shards

**권장**: 5 shards

**구현**:
```hcl
resource "aws_elasticache_replication_group" "redis_cluster" {
  num_node_groups = 5  # ✅ 3 → 5
  replicas_per_node_group = 2
}
```

**효과**:
- ✅ 데이터 분산 개선 (shard당 부하 40% 감소)
- ✅ CPU 병렬 처리 능력 향상
- ✅ **동시 처리량 67% 증가** (3 → 5 shards)

**주의사항**:
- ⚠️ Re-sharding 중 **성능 저하** (10-30분)
- ⚠️ Hash slot 재분배로 일시적 지연 발생
- ⚠️ **피크 시간 외 실행 필수**

---

#### 3.3 Karpenter-style Proactive Scaling ⭐

**개념**: Gateway Pod 증가 → Redis 노드도 자동 증가

**구현**:
```python
# lambda/karpenter_style_redis_scaling.py
def monitor_gateway_pods():
    """
    Gateway Pod 수 모니터링 → Redis 노드 예측적 증가
    """
    eks = boto3.client('eks')
    
    # Gateway Pod 수 확인
    gateway_pods = get_pod_count(namespace='tacos-app', label='app=gateway-api')
    
    # Pod 수에 비례한 Redis 노드 계산
    # 10 Gateway Pods당 Redis 노드 1개
    required_redis_nodes = max(3, gateway_pods // 10)
    
    # Redis 노드 조정
    scale_redis_cluster(required_redis_nodes)
```

**효과**:
- ✅ Gateway 증가 → Redis도 동시 증가
- ✅ **사전 예방적 스케일링**

---

## 📊 종합 비교

| 전략 | 적용 시간 | 비용 | 효과 | 우선순위 |
|---|---|---|---|---|
| **Circuit Breaker** | 1시간 | 무료 | 500 → 503 | ⭐⭐⭐ |
| **Pipeline 최적화** | 완료 | 무료 | 70K → 20K cmd/s | ⭐⭐⭐ |
| **Connection Pool 증가** | 10분 | 무료 | Timeout 감소 | ⭐⭐⭐ |
| **오토스케일링 개선** | 1일 | 무료 | Scale 10분 → 2분 | ⭐⭐⭐ |
| **Read Replica 추가** | 1일 | +$300/월 | Primary CPU 50% 감소 | ⭐⭐ |
| **노드 타입 업그레이드** | 2일 | +$613/월 | CPU 여유 2배 | ⭐⭐ |
| **샤드 수 증가** | 1주 | +$1,000/월 | 처리량 67% 증가 | ⭐⭐⭐ |
| **Pre-warming Lambda** | 3일 | 무료 | 예방적 확장 | ⭐⭐ |

---

## 🎯 권장 실행 순서

### **Week 1: 긴급 조치** (비용 무료)
1. ✅ Pipeline 최적화 배포 (이미 완료)
2. ⏳ Circuit Breaker 구현 및 배포
3. ⏳ Connection Pool 설정 조정
4. ⏳ ElastiCache 오토스케일링 개선 (50%, 30s)

### **Week 2: 단기 개선** (비용 +$300/월)
5. Read Replica 추가 (샤드당 2개)
6. Pre-warming Lambda 구현 (티켓 오픈 시간 기반)

### **Week 3-4: 장기 전략** (비용 +$1,613/월)
7. 노드 타입 업그레이드 (r7g.large → r7g.xlarge)
8. 샤드 수 증가 (3 → 5)

---

## 📈 예상 성능 개선

**현재 상태** (Pipeline 최적화 전):
- Redis CPU: **99.8%**
- Redis Commands: **70,000/sec**
- Gateway 에러율: **~30%** (500 에러)

**Pipeline 최적화 후** (Week 1):
- Redis CPU: **~30%** (71% 감소)
- Redis Commands: **20,000/sec** (71% 감소)
- Gateway 에러율: **~5%** (Circuit Breaker fallback)

**Full 최적화 후** (Week 4):
- Redis CPU: **~15-20%** (안정)
- Redis Commands: **20,000/sec** (5 shards로 분산)
- Gateway 에러율: **< 1%**
- 오토스케일링: **사전 예방적 대응**

---

## 🔧 실행 가능한 Next Steps

1. **즉시 실행**:
   ```bash
   # Circuit Breaker 배포
   git add internal/middleware/circuit_breaker.go
   git commit -m "feat: Add Circuit Breaker for Redis resilience"
   git push
   
   # Connection Pool 설정 조정
   kubectl edit configmap gateway-api-config -n tacos-app
   kubectl rollout restart deployment/gateway-api -n tacos-app
   ```

2. **오토스케일링 개선**:
   ```bash
   cd ../traffic-tacos-infra-iac
   # modules/elasticache/main.tf 수정
   terraform plan
   terraform apply
   ```

3. **모니터링 강화**:
   ```bash
   # CloudWatch 대시보드 추가
   aws cloudwatch put-dashboard --dashboard-name Redis-Health --dashboard-body file://redis-dashboard.json
   ```

---

**문서 작성**: 2025-10-08
**최종 수정**: 2025-10-08
**작성자**: Traffic Tacos Platform Team

