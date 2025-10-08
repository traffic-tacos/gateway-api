# Traffic Tacos: 30k RPS 티켓팅 시스템 발표 구조안 (V3)
## 주제별 카테고리 구성

---

## 🎯 **전체 발표 흐름 (6개 Part)**

```
Part 1: 트래픽 처리 (Traffic Handling) - 15분
Part 2: 보안 (Security) - 5분
Part 3: FinOps (Cost Optimization) - 5분
Part 4: 관측성 (Observability) - 5분
Part 5: 부하테스트 (Load Testing) - 5분
Part 6: Lesson Learned - 5분

총 40분 (Q&A 포함 50분)
```

---

## 📊 Part 1: 트래픽 처리 (Traffic Handling) - 15분

### 오프닝
```
슬라이드: "30만 명의 트래픽, 어떻게 처리할 것인가?"
- 문제 정의: 30만 명, 1만 석, 어떻게?
- 4가지 도전 과제: 트래픽, 공정성, 재고, 비용
```

### 1.1 아키텍처 선택 (3분)
```
질문: "30만 명을 어떻게 처리할 것인가?"

3가지 패턴 비교:
├─ Direct Access: DB 폭발 ❌
├─ Queue Service: 비동기, 높은 비용 ❌
└─ Gateway + Admission Control: 실시간, 제어 가능 ✅

우리의 선택: Gateway + 입장 제어
- 실시간 피드백 (순번 조회)
- 뒷단 보호 (입장 제어)
- 공정성 보장 (대기열 관리)
- Trade-off 수용: Gateway 부하 집중
```

### 1.2 Redis 선택 (2분)
```
질문: "왜 Redis인가?"

요구사항:
├─ 빠른 조회 (ms 단위)
├─ 순서 보장 (FIFO)
├─ 원자적 연산 (Lua Script)
├─ TTL 자동 관리
└─ 분산 환경 지원

Redis의 강점:
✅ ZSET: O(log N) 조회
✅ Lua Script: 원자성 보장
✅ Cluster Mode: 확장성
✅ Read Replica: 읽기 분산

vs DynamoDB (비용 높음), PostgreSQL (동시성 어려움)
```

### 1.3 용량 산정 (2분)
```
질문: "왜 1,000 RPS로 입장 제어?"

4가지 기준:
1. 좌석 수: 10k석 ÷ 1,800초 = 5.5 RPS (너무 느림 ❌)
2. 사용자 경험: 300k명 ÷ 300초 = 1,000 RPS (적절 ✅)
3. Backend 용량: 3k RPS 가능 (여유 30% ✅)
4. 비용: $2.25 (감당 가능 ✅)

결론: 사용자 경험 + 용량 + 비용의 최적 균형점
```

### 1.4 인프라 선정 (2분)
```
Redis 인스턴스:
├─ 메모리: 300MB + 버퍼 = 2GB 필요
├─ 네트워크: 60k RPS × 1KB = 60MB/s
├─ CPU: O(log N) × 60k RPS
└─ 선택: cache.m7g.xlarge × 10 nodes (5 shards)

Gateway Auto-scaling:
├─ Karpenter: 빠른 확장 (30초 내)
├─ Spot + On-demand: 비용 효율
└─ HPA 연동: CPU 60% 기준
```

### 1.5 병목 발견 & 해결 (6분)
```
초기 구현:
├─ 1k RPS: 모든 컴포넌트 OK ✅
└─ 목표: 10k RPS로 증가

병목 발견 (10k RPS):
├─ Gateway: 15% CPU ✅
├─ Backend: 5% CPU ✅
├─ DynamoDB: 10% ✅
└─ Redis: 100% CPU 🔴 (병목!)

왜? Status API에서 KEYS 명령어!
├─ O(N) blocking operation
├─ 초당 10,000번 실행
└─ Redis CPU 100% 포화

해결: Position Index (ZSET)
├─ KEYS (O(N)) → ZRANK (O(log N))
├─ 100배 성능 향상
└─ Redis CPU: 100% → 40%

결과:
├─ 10k RPS: Redis 40% (여유)
├─ 20k RPS: Redis 70% (안정)
└─ 30k RPS: Redis 90% (목표 달성!)
```

---

## 🔒 Part 2: 보안 (Security) - 5분

### 2.1 인증 & 인가 (2분)
```
JWT 인증:
├─ OIDC 기반 토큰 검증
├─ JWK 캐시 (Redis, 10분 TTL)
└─ 자체 발급 토큰 (대기열용)

Authorization:
├─ 입장 토큰 검증 (예약 전 필수)
├─ TTL 기반 자동 만료 (30초)
└─ Reservation API에서 재검증
```

### 2.2 Rate Limiting & Idempotency (2분)
```
Rate Limiting:
├─ Token Bucket 알고리즘
├─ IP/사용자별 50 RPS
├─ Redis Lua Script 기반
└─ 429 응답 (Retry-After 헤더)

Idempotency:
├─ POST/PUT/DELETE 필수
├─ UUID-v4 키
├─ Redis 저장 (TTL: 5분)
└─ 중복 요청 방지 (409 응답)
```

### 2.3 봇 방어 (1분)
```
다층 방어:
├─ CloudFront: AWS Shield (DDoS)
├─ Gateway: Rate Limiting
├─ Queue: Heartbeat 체크
└─ Reservation: Idempotency Key
```

---

## 💰 Part 3: FinOps (Cost Optimization) - 5분

### 3.1 설계 단계 비용 고려 (2분)
```
인프라 선택:
├─ Redis: Graviton (ARM64) - 20% 절감
├─ Gateway: Spot Instance - 70% 절감
├─ EKS: Karpenter 자동 확장 - 필요한 만큼만
└─ DynamoDB: On-demand - 피크 대응

용량 산정:
├─ 입장 제어 1,000 RPS → DB 부하 최소화
└─ 30분 티켓팅: $2.25 (DynamoDB 쓰기)
```

### 3.2 최적화 결과 (3분)
```
비용 절감 전략:
1. Redis 최적화 (코드 개선)
   - KEYS → Position Index
   - 노드 증설 불필요
   - 절감: $500/month

2. Karpenter Spot Instance
   - On-demand 대비 70% 절감
   - 중단율 < 2%
   - 절감: $1,200/month

3. DynamoDB 입장 제어
   - 쓰기 1,000 RPS로 제한
   - 비용 예측 가능
   - 30분 티켓팅: $2.25

총 절감: $1,700/month
추가 비용: $0 (코드 최적화로 30k RPS 달성)
```

---

## 📊 Part 4: 관측성 (Observability) - 5분

### 4.1 메트릭 수집 (2분)
```
CloudWatch:
├─ ElastiCache: CPU, Commands, Network
├─ EKS: Pod/Node 메트릭
└─ DynamoDB: Read/Write 용량

Prometheus:
├─ Gateway API: HTTP 요청/응답 시간
├─ Redis: 커넥션 풀 상태
└─ Custom: 대기열 길이, 입장률
```

### 4.2 프로파일링 (2분)
```
pprof (Go):
├─ Setup: app.Use(pprof.New())
├─ CPU 프로파일: curl /debug/pprof/profile?seconds=30
├─ 분석: go tool pprof -top cpu.prof
└─ 결과: calculateGlobalPosition() 51% CPU

병목 발견 과정:
CloudWatch (무엇이 문제?) → pprof (어디서 문제?)
```

### 4.3 분산 추적 (1분)
```
OpenTelemetry:
├─ Trace ID 전파 (모든 서비스)
├─ Span: API → Gateway → Reservation → Inventory
└─ Jaeger: 시각화
```

---

## 🧪 Part 5: 부하테스트 (Load Testing) - 5분

### 5.1 테스트 시나리오 (2분)
```
점진적 부하 증가:
├─ 1k RPS: 기능 검증 ✅
├─ 5k RPS: 초기 확장 테스트 ✅
├─ 10k RPS: 병목 발견 🔴 (Redis CPU 100%)
├─ 최적화 후 10k RPS: ✅ (Redis CPU 40%)
├─ 20k RPS: ✅ (Redis CPU 70%)
└─ 30k RPS: ✅ (Redis CPU 90%, 목표 달성!)

k6 시나리오:
- Join: 30만 명 대기
- Status: 5초마다 조회 (60k RPS)
- Enter: 1,000 RPS로 제한
- Reservation: 1,000 RPS
```

### 5.2 Karpenter 확장 테스트 (2분)
```
Scale-up 성능:
├─ 5k → 10k RPS: 30초 (5 pods 추가)
├─ 10k → 20k RPS: 1분 (10 pods 추가)
└─ 20k → 30k RPS: 2분 (10 pods 추가)

Scale-down 안정성:
├─ 30k → 10k: 5분 (15 pods 제거)
└─ 10k → 1k: 10분 (추가 5 pods 제거)

Spot Instance 효과:
├─ 중단율: < 2%
└─ 비용 절감: 70%
```

### 5.3 최종 결과 (1분)
```
성능 지표:
├─ 처리량: 30k RPS (목표 달성)
├─ Gateway CPU: 15% (여유)
├─ Redis CPU: 90% (안정)
├─ Backend CPU: < 5% (매우 여유)
└─ 에러율: < 1%

비용:
├─ 노드 증설: 불필요
├─ 코드 최적화: $0
└─ 총 추가 비용: $0
```

---

## 🎓 Part 6: Lesson Learned - 5분

### 6.1 설계 교훈 (2분)
```
1. 요구사항이 먼저
   "30만 명, 실시간, 공정성" → 아키텍처 결정

2. Trade-off를 인정
   "Gateway 부하 집중" → Karpenter로 대응

3. 적절한 도구 선택
   Redis ZSET = 대기열 관리에 최적

4. 점진적 검증
   1k → 10k → 30k RPS (단계별 테스트)
```

### 6.2 기술 교훈 (2분)
```
1. KEYS는 절대 금지
   Production에서 O(N) blocking 명령어 사용 금지

2. 모니터링 필수
   CloudWatch + pprof 없이는 병목 발견 불가능

3. 코드 > 인프라
   노드 증설보다 코드 최적화가 먼저

4. FinOps 사고방식
   설계 단계부터 비용 고려 (Spot, Graviton, 입장 제어)
```

### 6.3 다음 목표 (1분)
```
확장 계획:
├─ 100k RPS 처리
├─ Multi-region 배포
├─ Real-time 분석 대시보드
└─ 더 나은 UX (WebSocket, PWA)
```

---

## 📊 **슬라이드 구성 제안**

### Part 1: 트래픽 (15분, 15-20 슬라이드)
```
1. 오프닝: 문제 정의
2. 아키텍처 3가지 패턴
3. Gateway + 입장 제어 선택
4. Redis 선택 이유
5. 용량 산정 (1,000 RPS)
6. 인프라 선정 (Redis + Karpenter)
7. 설계 구조 다이어그램
8-10. 초기 구현 및 1k RPS 테스트
11-13. 10k RPS 병목 발견 (Redis CPU 100%)
14-15. Position Index 해결 (KEYS → ZRANK)
16-17. 최적화 결과 (30k RPS 달성)
```

### Part 2: 보안 (5분, 3-5 슬라이드)
```
1. 인증 & 인가 (JWT, 입장 토큰)
2. Rate Limiting & Idempotency
3. 다층 봇 방어 (CloudFront, Gateway, Queue)
```

### Part 3: FinOps (5분, 3-5 슬라이드)
```
1. 설계 단계 비용 고려 (Graviton, Spot, 입장 제어)
2. 비용 절감 전략 (Redis 최적화, Karpenter)
3. 최종 비용 결과 ($0 추가, $1,700/month 절감)
```

### Part 4: 관측성 (5분, 3-4 슬라이드)
```
1. CloudWatch 메트릭 (CPU, Commands, Network)
2. pprof 프로파일링 (병목 발견 과정)
3. 분산 추적 (OpenTelemetry)
```

### Part 5: 부하테스트 (5분, 4-6 슬라이드)
```
1. 테스트 시나리오 (1k → 30k RPS)
2. k6 부하 테스트 설정
3. Karpenter 확장 테스트 (Scale-up/down)
4. 최종 성능 결과
```

### Part 6: Lesson Learned (5분, 3-4 슬라이드)
```
1. 설계 교훈 (요구사항, Trade-off, 도구, 검증)
2. 기술 교훈 (KEYS 금지, 모니터링, 코드 우선, FinOps)
3. 다음 목표 (100k RPS, Multi-region)
```

---

## 🎯 **V2 vs V3 비교**

### V2 (시간순 흐름)
```
장점:
✅ 스토리텔링 자연스러움
✅ 설계 → 구현 → 최적화 과정 명확

단점:
❌ 주제가 흩어져 있음
❌ 보안/FinOps/관측성이 부각 안됨
```

### V3 (주제별 카테고리)
```
장점:
✅ 각 주제별로 집중 설명
✅ 트래픽/보안/FinOps/관측성 명확히 구분
✅ 부하테스트 결과를 독립된 파트로 강조

단점:
❌ 시간순 흐름이 약간 끊김
✅ 해결: Part 1에서 전체 여정을 포함
```

---

## 📝 **다음 작업**

V3 구조로 재작성할까요?

**Option 1: 완전히 새로운 V3 문서 작성**
- 주제별 카테고리 구조
- Part 1-6로 명확히 구분
- 각 Part마다 독립된 챕터

**Option 2: V2를 기반으로 재구성**
- 기존 내용 유지
- 챕터 순서 재배치
- Part 구분 추가

어떤 방식을 선호하시나요?
