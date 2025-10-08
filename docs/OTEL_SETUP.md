# OpenTelemetry (OTEL) Collector 설정 가이드

Gateway API는 HTTP traces와 metrics를 OpenTelemetry Collector로 전송할 수 있도록 구성되어 있습니다.

## 📊 기능

- **Distributed Tracing**: HTTP 요청 트레이싱을 OTLP HTTP 프로토콜로 Collector에 전송
- **Metrics**: Prometheus metrics와 함께 OTLP metrics도 Collector로 전송
- **자동 계측**: `otelfiber` 미들웨어를 통한 HTTP 요청 자동 트레이싱

## 🔧 환경 변수 설정

```bash
# OpenTelemetry 설정
export OBSERVABILITY_OTLP_ENDPOINT="localhost:4318"  # OTLP HTTP endpoint (without http://)
export OBSERVABILITY_TRACING_ENABLED="true"          # Tracing 활성화
export OBSERVABILITY_SAMPLE_RATE="1.0"               # Sampling rate (0.0 ~ 1.0)
```

### 주요 환경 변수

| 변수명 | 설명 | 기본값 | 예제 |
|--------|------|--------|------|
| `OBSERVABILITY_OTLP_ENDPOINT` | OTLP Collector HTTP endpoint | `http://localhost:4318` | `otel-collector.monitoring:4318` |
| `OBSERVABILITY_TRACING_ENABLED` | Tracing 활성화 여부 | `true` | `true` / `false` |
| `OBSERVABILITY_SAMPLE_RATE` | Trace sampling 비율 | `0.1` | `1.0` (100%), `0.5` (50%) |

## 🐳 OTEL Collector 설정 예제

### Docker Compose로 OTEL Collector 실행

```yaml
version: '3.8'

services:
  otel-collector:
    image: otel/opentelemetry-collector-contrib:latest
    command: ["--config=/etc/otel-collector-config.yaml"]
    volumes:
      - ./otel-collector-config.yaml:/etc/otel-collector-config.yaml
    ports:
      - "4317:4317"   # OTLP gRPC
      - "4318:4318"   # OTLP HTTP
      - "55679:55679" # zpages
      - "13133:13133" # health_check
    networks:
      - monitoring

  gateway-api:
    build: .
    environment:
      - OBSERVABILITY_OTLP_ENDPOINT=otel-collector:4318
      - OBSERVABILITY_TRACING_ENABLED=true
      - OBSERVABILITY_SAMPLE_RATE=1.0
    ports:
      - "8000:8000"
    depends_on:
      - otel-collector
    networks:
      - monitoring

networks:
  monitoring:
    driver: bridge
```

### OTEL Collector 설정 파일 (`otel-collector-config.yaml`)

```yaml
receivers:
  otlp:
    protocols:
      http:
        endpoint: 0.0.0.0:4318
      grpc:
        endpoint: 0.0.0.0:4317

processors:
  batch:
    timeout: 10s
    send_batch_size: 1024
  
  memory_limiter:
    check_interval: 1s
    limit_mib: 512

exporters:
  # Jaeger로 traces 전송
  jaeger:
    endpoint: jaeger:14250
    tls:
      insecure: true
  
  # Prometheus로 metrics 전송
  prometheus:
    endpoint: 0.0.0.0:8889
  
  # 로깅 (개발용)
  logging:
    loglevel: debug

service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [memory_limiter, batch]
      exporters: [jaeger, logging]
    
    metrics:
      receivers: [otlp]
      processors: [memory_limiter, batch]
      exporters: [prometheus, logging]
```

## ☸️ Kubernetes 배포

### OTEL Collector Deployment

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: otel-collector-config
  namespace: monitoring
data:
  otel-collector-config.yaml: |
    receivers:
      otlp:
        protocols:
          http:
            endpoint: 0.0.0.0:4318
          grpc:
            endpoint: 0.0.0.0:4317
    
    processors:
      batch:
        timeout: 10s
        send_batch_size: 1024
      memory_limiter:
        check_interval: 1s
        limit_mib: 512
    
    exporters:
      jaeger:
        endpoint: jaeger.monitoring.svc.cluster.local:14250
        tls:
          insecure: true
      prometheus:
        endpoint: 0.0.0.0:8889
      logging:
        loglevel: info
    
    service:
      pipelines:
        traces:
          receivers: [otlp]
          processors: [memory_limiter, batch]
          exporters: [jaeger, logging]
        metrics:
          receivers: [otlp]
          processors: [memory_limiter, batch]
          exporters: [prometheus, logging]

---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: otel-collector
  namespace: monitoring
spec:
  replicas: 2
  selector:
    matchLabels:
      app: otel-collector
  template:
    metadata:
      labels:
        app: otel-collector
    spec:
      containers:
      - name: otel-collector
        image: otel/opentelemetry-collector-contrib:latest
        args:
          - "--config=/etc/otel-collector-config.yaml"
        ports:
        - containerPort: 4317
          name: otlp-grpc
        - containerPort: 4318
          name: otlp-http
        - containerPort: 8889
          name: prometheus
        volumeMounts:
        - name: config
          mountPath: /etc/otel-collector-config.yaml
          subPath: otel-collector-config.yaml
        resources:
          requests:
            memory: "256Mi"
            cpu: "200m"
          limits:
            memory: "512Mi"
            cpu: "500m"
      volumes:
      - name: config
        configMap:
          name: otel-collector-config

---
apiVersion: v1
kind: Service
metadata:
  name: otel-collector
  namespace: monitoring
spec:
  selector:
    app: otel-collector
  ports:
  - name: otlp-grpc
    port: 4317
    targetPort: 4317
  - name: otlp-http
    port: 4318
    targetPort: 4318
  - name: prometheus
    port: 8889
    targetPort: 8889
  type: ClusterIP
```

### Gateway API Deployment에 환경 변수 추가

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: gateway-api
  namespace: tickets-api
spec:
  template:
    spec:
      containers:
      - name: gateway-api
        image: your-registry/gateway-api:latest
        env:
        - name: OBSERVABILITY_OTLP_ENDPOINT
          value: "otel-collector.monitoring.svc.cluster.local:4318"
        - name: OBSERVABILITY_TRACING_ENABLED
          value: "true"
        - name: OBSERVABILITY_SAMPLE_RATE
          value: "0.1"  # 10% sampling for production
```

## 🔍 검증

### 1. Gateway API 로그 확인

```bash
# 로컬
./gateway

# Kubernetes
kubectl logs -f deployment/gateway-api -n tickets-api
```

성공 시 다음과 같은 로그가 출력됩니다:
```
INFO[0000] OpenTelemetry tracing initialized            otlp_endpoint="localhost:4318" sample_rate=1
INFO[0000] OTLP metrics exporter initialized            otlp_endpoint="localhost:4318"
```

### 2. OTEL Collector 헬스 체크

```bash
# HTTP health check
curl http://localhost:13133/

# Collector 로그 확인
docker logs otel-collector

# Kubernetes
kubectl logs -f deployment/otel-collector -n monitoring
```

### 3. Traces 확인

Jaeger UI에서 traces를 확인할 수 있습니다:
```bash
# Jaeger UI (예시)
http://localhost:16686
```

### 4. Metrics 확인

Prometheus에서 OTLP metrics를 확인:
```bash
# Prometheus UI에서 확인
http://localhost:9090

# Query 예시:
# - gateway_api_http_requests_total
# - gateway_api_request_duration_seconds
```

## 📝 주요 변경 사항

### 1. Tracing (`internal/middleware/tracing.go`)

- OTLP HTTP exporter 사용
- Trace context propagation 지원
- Configurable sampling rate

### 2. Metrics (`internal/metrics/metrics.go`)

- Prometheus metrics (기존)
- OTLP metrics exporter 추가
- 10초마다 자동 export

### 3. Main (`cmd/gateway/main.go`)

- OTLP exporters 초기화
- Graceful shutdown 지원
- 에러 핸들링 개선

## 🎯 Best Practices

### Production 환경

```bash
# Sampling rate를 낮춰 오버헤드 감소
export OBSERVABILITY_SAMPLE_RATE="0.1"  # 10%

# TLS 활성화 (collector 설정에서)
# otlptracehttp.WithTLSClientConfig()
```

### Development 환경

```bash
# 모든 trace 수집
export OBSERVABILITY_SAMPLE_RATE="1.0"  # 100%

# 로컬 collector
export OBSERVABILITY_OTLP_ENDPOINT="localhost:4318"
```

## 🐛 Troubleshooting

### OTLP Collector 연결 실패

```bash
# Collector endpoint 확인
nslookup otel-collector.monitoring.svc.cluster.local

# 네트워크 연결 테스트
curl http://otel-collector:4318/v1/traces

# Gateway API 로그 확인
kubectl logs -f deployment/gateway-api -n tickets-api | grep -i otel
```

### Traces가 보이지 않음

1. Sampling rate 확인: `OBSERVABILITY_SAMPLE_RATE` 값이 너무 낮지 않은지 확인
2. Tracing 활성화 확인: `OBSERVABILITY_TRACING_ENABLED=true`
3. Collector pipeline 확인: `otel-collector-config.yaml`의 traces pipeline 설정

### Metrics가 보이지 않음

1. OTLP metrics exporter 초기화 로그 확인
2. Collector의 metrics pipeline 확인
3. Prometheus scrape 설정 확인

## 📚 참고 자료

- [OpenTelemetry Go SDK](https://opentelemetry.io/docs/instrumentation/go/)
- [OTLP Specification](https://opentelemetry.io/docs/specs/otlp/)
- [OpenTelemetry Collector](https://opentelemetry.io/docs/collector/)
- [otelfiber Middleware](https://github.com/gofiber/contrib/tree/main/otelfiber)
