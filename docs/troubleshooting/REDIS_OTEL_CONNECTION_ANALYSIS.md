# Redis & OTEL Collector 연결 에러 분석 보고서

**작성일**: 2025-10-08  
**버전**: Gateway API v1.3.1  
**분석 대상**: 운영 환경 `tacos-app` namespace

---

## 🚨 **문제 요약**

Gateway API 로그에서 다음 두 가지 에러가 주기적으로 발생:

```log
2025/10/08 11:36:18 failed to upload metrics: Post "http://otel-collector-collector.otel-collector.svc.cluster.local:4318/v1/metrics": read tcp 10.180.6.147:44920->172.20.147.155:4318: read: connection reset by peer

{"error":"read: connection reset by peer","level":"error","msg":"Redis health check failed","ts":"2025-10-08T11:36:29.271Z"}
{"error":"read: connection reset by peer","level":"error","msg":"Redis health check failed","ts":"2025-10-08T11:36:34.312Z"}
```

---

## 📊 **현재 인프라 상태**

### **1. Gateway API Pods (tacos-app namespace)**

```bash
$ kubectl get pods -n tacos-app -l app=gateway-api
NAME                           READY   STATUS    RESTARTS   AGE
gateway-api-6cf496d6d4-2gpdh   1/1     Running   2 (15m)    17m
gateway-api-6cf496d6d4-2v8wb   1/1     Running   0          21m
... (총 72개 pods)
gateway-api-6cf496d6d4-zxbkc   1/1     Running   0          17m
```

**관찰:**
- ✅ **72개 Pod 정상 운영 중** (대규모 트래픽 대응)
- ⚠️ **일부 Pod 재시작 발생** (2-4회 Restarts)
- 🔍 **재시작 원인**: Health check 실패 또는 OOM 추정

---

### **2. OTEL Collector Pods (otel-collector namespace)**

```bash
$ kubectl get pods -n otel-collector | grep collector
NAME                                      READY   STATUS        RESTARTS   AGE
otel-collector-collector-2hmcm            0/1     Pending       0          4h36m
otel-collector-collector-2t72x            1/1     Running       0          135m
... (총 45개 pods 중 44개 Running, 1개 Pending, 1개 Terminating)
otel-collector-collector-mptkz            1/1     Terminating   0          47h
```

**관찰:**
- ✅ **44개 정상 운영 중**
- ⚠️ **1개 Pending** (리소스 부족 또는 노드 스케줄링 문제)
- ⚠️ **1개 Terminating** (47시간 전 pod, graceful shutdown 지연)

**원인 추정:**
- OTEL Collector Pod가 불안정하여 일부 Gateway 요청이 실패
- Pending/Terminating Pod로 인한 간헐적 연결 실패

---

### **3. ElastiCache Redis Cluster 상태 (AWS)**

#### **3.1 Replication Group 구성**

```bash
$ aws elasticache describe-replication-groups --profile tacos
{
  "ReplicationGroupId": "traffic-tacos-redis",
  "Status": "available",
  "ClusterEnabled": true,
  "NodeGroups": [
    {
      "NodeGroupId": "0001",
      "Status": "available",
      "Slots": "0-872,1341-3276,...",
      "NodeGroupMembers": [
        {"CacheClusterId": "traffic-tacos-redis-0001-001", "PreferredAvailabilityZone": "ap-northeast-2c"},
        {"CacheClusterId": "traffic-tacos-redis-0001-002", "PreferredAvailabilityZone": "ap-northeast-2a"}
      ]
    },
    {
      "NodeGroupId": "0002",
      "Status": "available",
      ...
    },
    {
      "NodeGroupId": "0003",
      "Status": "available",
      ...
    }
  ]
}
```

**구성:**
- ✅ **Cluster Mode 활성화** (3개 Shard)
- ✅ **각 Shard당 2개 노드** (Primary + Replica) = **총 6개 노드**
- ✅ **Multi-AZ 배포** (ap-northeast-2a, ap-northeast-2c)
- ✅ **자동 Failover 활성화**

#### **3.2 보안 설정**

```bash
{
  "ConfigurationEndpoint": {
    "Address": "clustercfg.traffic-tacos-redis.w6eqga.apn2.cache.amazonaws.com",
    "Port": 6379
  },
  "AutomaticFailover": "enabled",
  "MultiAZ": "enabled",
  "AtRestEncryptionEnabled": true,
  "TransitEncryptionEnabled": true
}
```

**보안:**
- ✅ **TLS 전송 암호화** (필수)
- ✅ **저장 시 암호화**
- ✅ **AUTH 토큰** (AWS Secrets Manager: `traffic-tacos/redis/auth-token`)

#### **3.3 최근 CPU 사용률 (11:26 ~ 11:51)**

```json
[
  {"Timestamp": "2025-10-08T11:36:00", "Average": 1.52%, "Maximum": 2.77%},
  {"Timestamp": "2025-10-08T11:41:00", "Average": 0.87%, "Maximum": 1.33%},
  {"Timestamp": "2025-10-08T11:46:00", "Average": 0.95%, "Maximum": 1.10%},
  {"Timestamp": "2025-10-08T11:51:00", "Average": 0.95%, "Maximum": 1.12%}
]
```

**분석:**
- ✅ **CPU 사용률 매우 낮음** (평균 1.5%, 최대 2.77%)
- ✅ **성능 병목 없음**
- 🔍 **Redis 자체는 건강함**

#### **3.4 Redis 연결 수 (노드별)**

```json
[
  {"Timestamp": "2025-10-08T11:32:00", "Average": 2674, "Maximum": 6329},
  {"Timestamp": "2025-10-08T11:37:00", "Average": 7294, "Maximum": 7798},
  {"Timestamp": "2025-10-08T11:42:00", "Average": 5261, "Maximum": 5304},
  {"Timestamp": "2025-10-08T11:52:00", "Average": 5385, "Maximum": 5386}
]
```

**분석:**
- ⚠️ **노드당 5,000~7,798개 연결** (피크 시)
- 🔍 **이론상 최대 연결 수 계산:**
  - Gateway Pod: 72개
  - Pool Size: 1000
  - 총 연결: 72 × 1000 = 72,000개
  - Cluster 노드: 6개
  - **노드당 연결**: 72,000 ÷ 6 = **12,000개**
- ✅ **현재 5,000~7,798개는 정상 범위** (최대의 65%)
- 🔍 **피크 시 일시적으로 7,798개까지 증가**

---

### **4. Terraform 설정 (traffic-tacos-infra-iac)**

#### **4.1 ElastiCache 모듈 설정 (main.tf)**

```hcl
module "elasticache" {
  source = "./modules/elasticache"

  cluster_name       = "traffic-tacos-redis"
  node_type          = var.redis_node_type

  # Cluster mode configuration
  cluster_mode_enabled    = true
  num_node_groups         = 5    # 5 shards (최근 99% CPU 때문에 증가)
  replicas_per_node_group = 1    # 1 replica per shard for HA

  # Auto Scaling configuration
  enable_auto_scaling    = true
  min_node_groups        = 3
  max_node_groups        = 10
  target_cpu_utilization = 70

  at_rest_encryption_enabled = true
  transit_encryption_enabled = true
  auth_token_secret_name     = "traffic-tacos/redis/auth-token"

  automatic_failover_enabled = true
  multi_az_enabled           = true
  apply_immediately          = true
}
```

**주목할 점:**
- 🔍 **Terraform 설정은 5 shards** (num_node_groups = 5)
- ⚠️ **실제 운영 환경은 3 shards** (NodeGroup 0001, 0002, 0003)
- 🚨 **불일치 원인**: Terraform apply가 안 되었거나, 수동으로 축소됨

#### **4.2 Secrets Manager 설정**

```hcl
# Secrets Manager에서 AUTH token 가져오기
data "aws_secretsmanager_secret" "redis_auth" {
  name = var.auth_token_secret_name
}

locals {
  redis_auth_value = try(
    jsondecode(data.aws_secretsmanager_secret_version.redis_auth.secret_string).password,
    data.aws_secretsmanager_secret_version.redis_auth.secret_string
  )
  
  # auth_token은 영숫자만 허용하므로 base64로 인코딩
  redis_auth_token = replace(replace(replace(
    base64encode(local.redis_auth_value),
    "=", ""),
    "+", ""),
    "/", ""
  )
}
```

**분석:**
- ✅ **AUTH token을 Secrets Manager에서 안전하게 가져옴**
- ✅ **Base64 인코딩하여 특수문자 제거** (ElastiCache 요구사항)

---

### **5. Gateway Deployment 설정 (deployment-repo)**

#### **5.1 Redis 관련 환경변수**

```yaml
env:
  # Redis Cluster Configuration
  - name: REDIS_ADDRESS
    value: clustercfg.traffic-tacos-redis.w6eqga.apn2.cache.amazonaws.com:6379
  - name: REDIS_CLUSTER_MODE
    value: "true"
  - name: REDIS_TLS_ENABLED
    value: "true"
  - name: REDIS_ROUTE_BY_LATENCY
    value: "true"
  - name: REDIS_READ_ONLY
    value: "true"
  
  # Connection Pool
  - name: REDIS_POOL_SIZE
    value: "1000"
  - name: REDIS_POOL_TIMEOUT
    value: "10s"
  - name: REDIS_MIN_IDLE_CONNS
    value: "100"
  - name: REDIS_MAX_CONN_AGE
    value: "30m"
  
  # Secrets Manager
  - name: REDIS_PASSWORD_FROM_SECRETS
    value: "true"
  - name: AWS_SECRET_NAME
    value: traffic-tacos/redis/auth-token
```

**분석:**
- ✅ **Configuration Endpoint 사용** (Cluster Mode)
- ✅ **TLS 활성화**
- ✅ **Read Replica 최적화** (Latency-based routing)
- ✅ **Pool Size 1000** (고부하 대응)
- ✅ **Secrets Manager 통합**

#### **5.2 OTEL 관련 환경변수 (❌ 문제 발견!)**

```yaml
env:
  - name: OTEL_EXPORTER_OTLP_ENDPOINT
    value: "otel-collector-collector.otel-collector:4317"  # ❌ gRPC 포트
  - name: OTEL_EXPORTER_OTLP_PROTOCOL
    value: "grpc"
```

**에러 로그:**
```log
failed to upload metrics: Post "http://otel-collector-collector.otel-collector.svc.cluster.local:4318/v1/metrics"
```

**문제:**
- ❌ **설정은 gRPC 포트 4317**
- ❌ **실제 요청은 HTTP 포트 4318**
- 🚨 **Gateway 코드에서 HTTP exporter를 사용하고 있음**

---

## 🔍 **근본 원인 분석**

### **문제 1: OTEL Collector 메트릭 전송 실패**

**원인:**
1. **포트 불일치**: 설정은 gRPC(4317), 코드는 HTTP(4318) 사용
2. **OTEL Collector Pod 불안정**: 1개 Pending, 1개 Terminating
3. **간헐적 연결 실패**: Service endpoint가 불안정한 Pod로 라우팅

**영향:**
- ⚠️ **메트릭 손실** (Prometheus에 일부 메트릭 누락)
- ⚠️ **로그 노이즈** (에러 로그 증가)
- ✅ **서비스 영향 없음** (OTEL은 비즈니스 로직과 분리)

---

### **문제 2: Redis Health Check 실패**

**원인:**
1. **간헐적 네트워크 이슈**: `connection reset by peer`
2. **가능한 원인:**
   - Redis Cluster 내부 resharding 또는 failover
   - Gateway Pod와 ElastiCache 간 네트워크 순간 단절
   - Health check timeout (현재 3초)
   - ElastiCache connection limit 근접 (노드당 7,798개 피크)

**영향:**
- ⚠️ **로그 노이즈** (5초 간격으로 에러 로그)
- ⚠️ **일부 Pod 재시작** (readinessProbe 실패)
- ✅ **비즈니스 로직 영향 없음** (실제 API 요청은 정상 처리)

---

## ✅ **해결 방안**

### **1. OTEL Collector 문제 해결 (즉시 조치)**

#### **Option A: HTTP Exporter로 통일 (권장)**

```yaml
# deployment.yaml 수정
env:
  - name: OTEL_EXPORTER_OTLP_ENDPOINT
    value: "http://otel-collector-collector.otel-collector.svc.cluster.local:4318"  # ✅ HTTP 포트
  - name: OTEL_EXPORTER_OTLP_PROTOCOL
    value: "http/protobuf"  # ✅ HTTP protocol 명시
```

#### **Option B: gRPC Exporter로 통일**

```yaml
# deployment.yaml 유지
env:
  - name: OTEL_EXPORTER_OTLP_ENDPOINT
    value: "otel-collector-collector.otel-collector:4317"
  - name: OTEL_EXPORTER_OTLP_PROTOCOL
    value: "grpc"
```

```go
// Gateway 코드 수정 필요 (internal/metrics/metrics.go)
// HTTP exporter → gRPC exporter로 변경
```

#### **Option C: OTEL 일시 비활성화 (임시)**

```yaml
# deployment.yaml
env:
  # - name: OTEL_EXPORTER_OTLP_ENDPOINT
  #   value: "otel-collector-collector.otel-collector:4317"
```

---

### **2. Redis Health Check 개선 (중기 조치)**

#### **2.1 Deployment 설정 조정**

```yaml
# deployment.yaml
readinessProbe:
  httpGet:
    path: /readyz
    port: 8000
  initialDelaySeconds: 15
  periodSeconds: 5
  timeoutSeconds: 3
  successThreshold: 1
  failureThreshold: 3  # ✅ 이미 적용됨

# 추가: Health check timeout 증가
env:
  - name: REDIS_DIAL_TIMEOUT
    value: "10s"  # 기본 5s → 10s
  - name: REDIS_READ_TIMEOUT
    value: "5s"
  - name: REDIS_WRITE_TIMEOUT
    value: "5s"
```

#### **2.2 Gateway 코드 개선 (health check 재시도)**

```go
// internal/middleware/redis.go
func (r *RedisClient) HealthCheck(ctx context.Context) error {
    for i := 0; i < 3; i++ {
        ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
        err := r.client.Ping(ctx).Err()
        cancel()
        
        if err == nil {
            return nil
        }
        
        // 재시도 전 대기
        if i < 2 {
            time.Sleep(500 * time.Millisecond)
        }
    }
    
    return fmt.Errorf("redis health check failed after 3 retries")
}
```

#### **2.3 OTEL Collector Pod 안정화**

```bash
# Pending Pod 원인 확인
kubectl describe pod -n otel-collector otel-collector-collector-2hmcm

# Terminating Pod 강제 삭제
kubectl delete pod -n otel-collector otel-collector-collector-mptkz --grace-period=0 --force

# OTEL Collector HPA 확인 및 조정
kubectl get hpa -n otel-collector
```

---

### **3. 연결 풀 최적화 (장기 조치)**

#### **3.1 Redis 연결 수 모니터링**

**현재 상태:**
- 노드당 피크: 7,798개
- 이론상 최대: 12,000개
- 사용률: **65%**

**권장:**
- ✅ **현재 설정 유지** (안전 여유분 35%)
- 🔍 **8,000개 초과 시 알림 설정**

```bash
# CloudWatch Alarm 생성
aws cloudwatch put-metric-alarm \
  --alarm-name redis-connections-high \
  --metric-name CurrConnections \
  --namespace AWS/ElastiCache \
  --statistic Maximum \
  --period 300 \
  --threshold 8000 \
  --comparison-operator GreaterThanThreshold \
  --dimensions Name=CacheClusterId,Value=traffic-tacos-redis-0001-001 \
  --profile tacos
```

#### **3.2 Pool Size 미세 조정 (필요 시)**

```yaml
# 현재: 72 pods × 1000 pool = 72,000 connections
# 노드당: 72,000 ÷ 6 = 12,000 connections

# Option 1: Pool Size 감소 (연결 수 줄임)
- name: REDIS_POOL_SIZE
  value: "800"  # 72 × 800 = 57,600 → 노드당 9,600

# Option 2: 유지 (현재 상태 안정적)
- name: REDIS_POOL_SIZE
  value: "1000"
```

---

## 📈 **모니터링 개선**

### **1. 추가 메트릭 수집**

```yaml
# Prometheus ServiceMonitor
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: gateway-api-redis-metrics
spec:
  selector:
    matchLabels:
      app: gateway-api
  endpoints:
  - port: metrics
    path: /metrics
    interval: 30s
```

### **2. Grafana Dashboard 추가**

**패널:**
- Redis health check success rate
- Redis connection pool usage (current / max)
- OTEL exporter success rate
- Gateway Pod restarts

### **3. Alert Rules**

```yaml
groups:
- name: gateway-redis-alerts
  rules:
  - alert: RedisHealthCheckFailureRate
    expr: rate(redis_health_check_failed_total[5m]) > 0.1
    for: 5m
    annotations:
      summary: "Redis health check failure rate > 10%"
  
  - alert: RedisConnectionPoolExhausted
    expr: redis_pool_current_conns / redis_pool_max_conns > 0.8
    for: 2m
    annotations:
      summary: "Redis connection pool > 80% usage"
```

---

## 🎯 **조치 우선순위**

### **우선순위 1: 즉시 조치 (오늘)**
1. ✅ **OTEL Collector 포트 불일치 수정** (deployment.yaml)
2. ✅ **Terminating Pod 강제 삭제** (47시간 전 pod)
3. ✅ **Pending Pod 원인 파악 및 해결**

### **우선순위 2: 단기 조치 (이번 주)**
1. ⏳ **Redis health check 재시도 로직 추가** (코드 수정)
2. ⏳ **Redis connection 수 알림 설정** (CloudWatch Alarm)
3. ⏳ **Grafana 대시보드 추가** (Redis 메트릭)

### **우선순위 3: 중장기 조치 (다음 스프린트)**
1. 📋 **OTEL Collector 안정성 개선** (리소스 증설, HPA 조정)
2. 📋 **Terraform 설정 동기화** (5 shards → 3 shards 반영)
3. 📋 **Circuit Breaker 패턴 도입** (Redis 장애 격리)

---

## 🔗 **관련 문서**

- [Redis CPU 최적화 (KEYS 병목)](./REDIS_CPU_OPTIMIZATION_KEYS_BOTTLENECK.md)
- [Redis Cluster Hash Tag 이슈](./REDIS_CLUSTER_HASHTAG_CROSSSLOT.md)
- [Gateway API Deployment Guide](../DEPLOYMENT_SUMMARY.md)

---

## 📝 **결론**

**현재 상태:**
- ✅ **ElastiCache Redis는 건강함** (CPU 1.5%, 연결 수 65%)
- ⚠️ **OTEL Collector 포트 불일치** (설정 gRPC, 코드 HTTP)
- ⚠️ **Redis health check 간헐적 실패** (연결 순간 끊김)
- ✅ **비즈니스 로직 영향 없음** (API 정상 작동)

**권장 조치:**
1. **OTEL 포트 불일치 즉시 수정** (deployment.yaml)
2. **Redis health check 재시도 로직 추가** (코드 개선)
3. **OTEL Collector Pod 안정화** (Pending/Terminating 해결)

**추가 고려사항:**
- Terraform 설정(5 shards)과 실제 운영(3 shards) 불일치 해소
- 피크 트래픽 시 연결 수 모니터링 강화 (현재 65% 사용)

---

**작성**: Gateway API 팀  
**문의**: [email protected]

