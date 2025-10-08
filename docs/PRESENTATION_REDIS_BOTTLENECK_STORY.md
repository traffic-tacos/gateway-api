# 티켓팅 시스템의 예상 밖 병목: Redis가 모든 것을 받아냈다

## 📜 1장: 티켓 오픈, 1분 전

### 슬라이드
```
"티켓 오픈, 1분 전"

수십만 명이 동시에 새로고침을 누릅니다.
과연, 어디가 먼저 무너질까요?


우리의 예상:
├─ Database가 쓰기 부하로 폭발? 💾
├─ 서버 CPU가 100%로 터질까? ⚡
└─ 네트워크 대역폭이 부족할까? 🌐

(멘트: 한번 맞춰보세요...)
```

### 멘트

여러분, 티켓팅 사이트를 떠올려 보세요.
예매 오픈 직전, **수십만 명이 동시에 새로고침**을 누릅니다.

이때 시스템은 어디에서 가장 먼저 무너질까요?

우리는 자연스럽게 생각했습니다.
- "DynamoDB가 쓰기 부하를 못 버티겠지?"
- "EKS 노드의 CPU가 100%가 되겠지?"
- "Inventory API에서 재고 처리가 터지겠지?"

**그런데 실제로는 완전히 달랐습니다.**

---

## 📜 2장: 예상과 달랐던 실제 병목

### 슬라이드
```
"1만 RPS 부하 테스트 결과"

┌─────────────────────────────────────────┐
│ Component          │ CPU    │ Status   │
├─────────────────────────────────────────┤
│ Gateway API        │ 15%    │ ✅ OK     │
│ Reservation API    │ 5%     │ ✅ OK     │
│ Inventory API      │ 3%     │ ✅ OK     │
│ Payment API        │ 2%     │ ✅ OK     │
│ DynamoDB          │ 10%    │ ✅ OK     │
├─────────────────────────────────────────┤
│ ElastiCache Redis  │ 100%   │ 🔴 병목!  │
└─────────────────────────────────────────┘

모든 부하를 Redis가 받아냈다!
```

### 멘트

1만 RPS로 부하 테스트를 돌렸습니다.
그리고 모니터링을 보는 순간, 예상이 완전히 빗나갔다는 걸 알았죠.

**Gateway API**: CPU 15% - 여유롭습니다.
**Reservation API**: CPU 5% - 거의 놀고 있습니다.
**Inventory API**: CPU 3% - 더 놀고 있습니다.
**DynamoDB**: 읽기/쓰기 모두 10% - 전혀 문제없습니다.

그런데 딱 하나,
**ElastiCache Redis가 CPU 100%**에 도달했습니다.

왜 이런 일이 벌어졌을까요?

---

## 📜 3장: 왜 Redis가 병목이 되었나?

### 슬라이드
```
"트래픽 흐름의 진실"

Browser (수십만 명)
    ↓ 새로고침 폭탄 💣
Gateway API + Redis
    ↓ 입장 제어 (1,000 RPS로 제한) 🚪
Reservation API
    ↓ 차분히 처리 😌
Inventory API
    ↓ 여유롭게 재고 차감 ✅
DynamoDB
    ↓ 문제없음 ✅


핵심: Gateway + Redis가 모든 폭탄을 받아냈다!
```

### 멘트

이유는 간단했습니다.

우리 아키텍처는 **Gateway API에서 입장 제어(Admission Control)**를 하도록 설계되어 있었습니다.

**수십만 명의 트래픽이 Gateway로 몰려옵니다.**
- 대기열에 Join 요청: 1만 RPS
- Status 조회 (5초마다 폴링): 수만 RPS
- Enter 시도: 수천 RPS

**Gateway는 Redis를 사용해서:**
1. 대기열 위치를 계산하고
2. Heartbeat로 활성 사용자를 체크하고
3. Token Bucket으로 입장을 제어합니다

**그리고 입장 허용된 사용자만** 뒷단으로 내보냅니다.
- 입장 허용: 초당 1,000명
- Reservation API: 1,000 RPS만 받음
- Inventory API: 1,000 RPS만 처리
- 모두 여유롭습니다

**즉, Redis가 방파제 역할을 한 겁니다.**
수십만 명의 트래픽을 받아내고,
뒷단은 안전하게 보호했죠.

하지만 그 대가로, **Redis CPU가 100%**가 되어버렸습니다.

---

## 📜 4장: 근본 원인 - KEYS 명령어

### 슬라이드
```
"범인은 Status API였다"

Status API (초당 1만 건):
Browser → GET /queue/status?token=xxx
    ↓
calculateGlobalPosition()
    ↓
Redis KEYS stream:event:{evt}:user:*  🔴 O(N) Blocking!
    ↓
모든 스트림 키 스캔 (10,000+ keys)
    ↓
Redis CPU 100% 💥


KEYS 명령어:
- O(N) 시간 복잡도 (모든 키 스캔)
- Blocking operation (다른 명령어 대기)
- Cluster Mode에서 모든 샤드 스캔
- 1만 RPS = 초당 1만번 KEYS 실행!
```

### 멘트

문제를 파고들었습니다.
코드를 분석한 결과, **Status API**가 범인이었습니다.

사용자는 대기열에 있는 동안 5초마다 자신의 순번을 조회합니다.
1만 명이 동시에 대기 중이라면?
**초당 2,000건의 Status API 요청**이 들어옵니다.

그리고 Status API는 내부적으로 `calculateGlobalPosition()`을 호출하는데,
이 함수가 **Redis KEYS 명령어**를 사용했습니다.

```go
// 😱 문제의 코드
keys, err := redis.Keys(ctx, "stream:event:{evt}:user:*").Result()
// 모든 스트림 키를 스캔!
```

**KEYS 명령어는 Production에서 절대 사용하면 안 되는 명령어입니다.**
- O(N) 시간 복잡도: 키가 많을수록 느려짐
- Blocking operation: 실행 중 다른 명령어 차단
- Redis Cluster에서는 모든 샤드를 스캔

1만 RPS Status API면,
**초당 1만 번의 KEYS 명령어가 실행**되는 겁니다.

Redis가 버틸 수 없었죠.

---

## 📜 5장: 해결 방법 - Position Index

### 슬라이드
```
"O(N) → O(log N): 100배 빠르게"

Before (KEYS scan):
KEYS stream:event:{evt}:user:*  // 모든 키 스캔
    ↓ O(N) = 10,000 keys
    ↓ ~100ms (blocking)
    ↓ CPU 100% 💥

After (Position Index):
ZRANK queue:event:{evt}:position token  // ZSET 조회
    ↓ O(log N) = 4 comparisons
    ↓ <1ms (non-blocking)
    ↓ CPU 40% ✅


성능 개선: ~100배!
```

### 멘트

해결 방법은 명확했습니다.
**KEYS를 없애고, 인덱스를 만들자.**

우리는 **Position Index**라는 ZSET 기반 인덱스를 도입했습니다.

**작동 방식:**
1. 사용자가 Join할 때: 
   - ZSET에 (token, timestamp) 추가
   - `ZADD queue:event:{evt}:position timestamp token`

2. Status API에서 위치 조회:
   - ZSET에서 rank 조회
   - `ZRANK queue:event:{evt}:position token`
   - 결과: 순번 (0-based)

**시간 복잡도:**
- KEYS: O(N) - 10,000 keys면 100ms
- ZRANK: O(log N) - 10,000 keys면 <1ms

**100배 빠릅니다!**

그리고 무엇보다,
**ZRANK는 non-blocking**입니다.
다른 명령어를 방해하지 않죠.

---

## 📜 6장: 추가 최적화들

### 슬라이드
```
"Redis뿐만이 아니었다"

3가지 추가 최적화:

1️⃣ Go 메모리 최적화
   GOMEMLIMIT=450MiB
   GOGC=75
   → HPA scale-down 정상화

2️⃣ Readiness Probe 조정
   initialDelaySeconds: 5 → 15초
   → Redis Cluster 연결 대기
   → Pod 시작 안정화

3️⃣ Connection Pool 튜닝
   PoolSize: 100 → 1000
   PoolTimeout: 4s → 10s
   → 고부하 처리 개선
```

### 멘트

Redis 최적화만으로는 부족했습니다.
세 가지를 더 해결해야 했죠.

**첫째, Go 메모리 문제.**
테스트 후에도 메모리가 해제되지 않아,
HPA가 scale-down을 못 하는 문제가 있었습니다.
- `GOMEMLIMIT=450MiB` 설정으로 GC가 적극적으로 메모리 회수
- `GOGC=75` 설정으로 더 자주 GC 실행
- 결과: 테스트 후 메모리 200Mi → 80Mi로 감소

**둘째, Readiness Probe.**
Pod가 시작할 때 Redis Cluster 연결에 10초 정도 걸리는데,
Readiness Probe가 5초만 기다리고 NotReady로 판정했습니다.
- `initialDelaySeconds: 15초`로 증가
- Redis Cluster 연결 완료 후 트래픽 받도록 수정
- 결과: Rolling update가 부드럽게 진행

**셋째, Connection Pool.**
고부하에서 `redis: connection pool timeout` 에러가 발생했습니다.
- Connection Pool 크기: 100 → 1000
- Timeout: 4초 → 10초
- 결과: 30k RPS에서도 안정적

---

## 📜 7장: 결과

### 슬라이드
```
"30k RPS 달성!"

┌────────────────────────────────────────┐
│ RPS     │ Redis CPU │ Result          │
├────────────────────────────────────────┤
│ Before                                 │
│ 10k     │ 100%      │ ❌ 한계 도달     │
│ 15k+    │ N/A       │ ❌ 불가능        │
├────────────────────────────────────────┤
│ After                                  │
│ 10k     │ 40%       │ ✅ 여유          │
│ 20k     │ 70%       │ ✅ 안정          │
│ 30k     │ 90%       │ ✅ 목표 달성!    │
└────────────────────────────────────────┘

비용 증가: 0원 (코드 최적화만)
노드 증설: 불필요
```

### 멘트

결과는 놀라웠습니다.

**최적화 전:**
- 10k RPS에서 Redis CPU 100%
- 더 이상 트래픽을 받을 수 없었습니다
- 목표인 30k RPS는 꿈도 못 꿨죠

**최적화 후:**
- 10k RPS: Redis CPU 40% - 여유롭습니다
- 20k RPS: Redis CPU 70% - 안정적입니다
- **30k RPS: Redis CPU 90% - 목표 달성!**

그리고 무엇보다,
**추가 비용이 들지 않았습니다.**

Redis 노드를 증설하지 않았습니다.
그냥 코드를 최적화했을 뿐입니다.

**cache.m7g.xlarge × 10 nodes** 그대로
30k RPS를 처리할 수 있게 되었습니다.

---

## 📜 8장: 아키텍처의 승리

### 슬라이드
```
"입장 제어 아키텍처의 위력"

Browser (100k users 동시 접속)
    ↓
    ↓ 수십만 RPS 🌊
    ↓
┌─────────────────────────┐
│  Gateway + Redis        │ ← 방파제 역할
│  (입장 제어)             │
│  - 대기열 관리           │
│  - 위치 계산             │
│  - 입장 허용 (1k RPS)   │
└─────────────────────────┘
    ↓
    ↓ 1,000 RPS만 통과 💧
    ↓
┌─────────────────────────┐
│  Backend Services       │ ← 안전하게 보호됨
│  - Reservation API      │   CPU 5%
│  - Inventory API        │   CPU 3%
│  - Payment API          │   CPU 2%
│  - DynamoDB            │   10% 사용
└─────────────────────────┘

핵심: Gateway가 모든 부하를 받아내고,
      뒷단을 보호한다!
```

### 멘트

이번 경험에서 가장 중요한 교훈은,
**아키텍처 설계가 맞았다**는 겁니다.

Gateway에서 **입장 제어(Admission Control)**를 하는 설계 덕분에:

**수십만 명의 트래픽이 Gateway로 몰려와도:**
- Redis가 방파제 역할을 합니다
- 대기열을 관리하고
- 위치를 계산하고
- **입장을 제어합니다**

**뒷단 서비스들은:**
- 초당 1,000명만 처리하면 됩니다
- CPU는 5% 미만으로 여유롭습니다
- DynamoDB는 10%만 사용합니다
- **스케일링이 필요 없습니다**

만약 입장 제어 없이 모든 트래픽을 뒷단으로 흘려보냈다면?
- Reservation API가 터졌을 겁니다
- DynamoDB 쓰기 비용이 폭발했을 겁니다
- 전체 시스템이 다운되었을 겁니다

**하지만 우리는 Gateway + Redis에 집중 투자했고,**
**그 결과 전체 시스템이 안정적으로 작동했습니다.**

---

## 📜 9장: 예상 밖의 교훈들

### 슬라이드
```
"예상과 실제의 차이"

❌ 예상했던 것들:
├─ "DB가 터질 거야"
├─ "서버 CPU가 100%될 거야"
├─ "노드를 더 늘려야 해"
└─ "비용이 많이 들겠지"

✅ 실제로 일어난 일들:
├─ Redis가 병목이었다
├─ KEYS 명령어가 범인이었다
├─ 코드 최적화로 100배 개선
├─ 추가 비용 0원
└─ 뒷단은 오히려 여유로웠다

핵심: "병목은 예상 밖의 곳에서 발견된다"
```

### 멘트

이번 경험에서 배운 교훈들입니다.

**첫째, 병목은 예상과 다른 곳에서 발생합니다.**
우리는 DB나 서버 CPU를 걱정했지만,
실제로는 Redis가 병목이었습니다.

**둘째, 작은 코드가 큰 문제를 만들 수 있습니다.**
딱 한 줄의 `KEYS` 명령어가,
전체 시스템의 처리량을 10배 제한했습니다.

**셋째, 모니터링이 없으면 발견할 수 없습니다.**
CloudWatch 메트릭을 보지 않았다면,
Redis CPU 100%를 몰랐을 겁니다.

**넷째, 프로파일링 도구가 중요합니다.**
`pprof`로 코드를 분석했기에,
정확히 어디가 문제인지 찾을 수 있었습니다.

**다섯째, 비용은 코드 최적화로 줄일 수 있습니다.**
노드를 늘리는 게 답이 아니었습니다.
코드를 고치는 게 답이었죠.

---

## 📜 10장: 실전 팁

### 슬라이드
```
"Redis 사용 시 주의사항"

🚫 절대 하지 말아야 할 것들:
1. KEYS 명령어 사용 (O(N) blocking)
2. 작은 Connection Pool (고부하 대비 안됨)
3. 기본 Timeout 설정 (Redis 응답 대기 필요)
4. Cluster Mode 무시 (Hash tag 필수)

✅ 반드시 해야 할 것들:
1. SCAN + ZSET Index 사용
2. Connection Pool 크게 설정 (1000+)
3. Timeout 여유있게 (10s+)
4. Read Replica 활용
5. CloudWatch 모니터링
6. pprof 프로파일링
```

### 멘트

마지막으로, 실전 팁들을 공유하겠습니다.

**Redis 사용 시 절대 하지 말아야 할 것:**

1. **KEYS 명령어 사용**
   - Production에서 절대 금지
   - 대신 SCAN이나 Index 사용

2. **작은 Connection Pool**
   - 기본값 100은 부족함
   - 최소 1000 이상 권장

3. **짧은 Timeout**
   - 2-3초는 부족
   - 10초 이상 여유있게

4. **Cluster Mode 무시**
   - Hash tag 사용 필수
   - CROSSSLOT 에러 주의

**반드시 해야 할 것:**

1. **적절한 자료구조**
   - ZSET으로 Index 만들기
   - O(log N) 조회 보장

2. **프로파일링**
   - pprof로 병목 찾기
   - CPU/Memory/Goroutine 분석

3. **모니터링**
   - CloudWatch 메트릭 확인
   - CPU/Network/Commands 추적

4. **문서화**
   - Troubleshooting 가이드 작성
   - 팀 지식 공유

---

## 📜 11장: 마무리

### 슬라이드
```
"30k RPS, 그리고 그 너머"

이번 여정에서 배운 것:

✅ Gateway + Redis가 방파제 역할
✅ 입장 제어로 뒷단 보호
✅ KEYS → Index로 100배 개선
✅ 코드 최적화로 비용 0원
✅ 아키텍처 설계의 중요성


"병목은 예상 밖의 곳에서,
 해결책은 의외로 가까운 곳에 있다"


다음 목표: 100k RPS? 🚀
```

### 멘트

정리하겠습니다.

우리는 30k RPS 티켓팅 시스템을 만들었습니다.

**예상과 달랐던 것:**
- DB나 서버가 아닌, Redis가 병목이었습니다
- Gateway가 모든 트래픽을 받아냈습니다
- 뒷단은 오히려 여유로웠습니다

**해결 방법:**
- KEYS 명령어를 Position Index로 교체
- 시간 복잡도: O(N) → O(log N)
- 성능 개선: 100배
- 추가 비용: 0원

**핵심 교훈:**
입장 제어 아키텍처 덕분에,
수십만 명의 트래픽을 안전하게 처리했고,
뒷단 서비스들은 보호받았습니다.

**그리고 무엇보다:**
병목은 예상 밖의 곳에서 발견되고,
해결책은 의외로 가까운 곳에 있다는 것을 배웠습니다.

다음 목표는 100k RPS일까요?
아니면 Multi-region 배포일까요?

여러분의 시스템에도 숨겨진 병목이 있을 겁니다.
찾아서 해결하는 그 과정이,
바로 엔지니어링의 재미 아닐까요?

감사합니다! 🚀

---

## 📊 부록: 기술 스택 & 성과

### 기술 스택
```yaml
Frontend:
  - React + Vite (SSG)
  - CloudFront + S3

Gateway (방파제):
  - Go + Fiber
  - ElastiCache Redis Cluster (5 shards, 10 nodes)
  - Position Index (ZSET)
  - Admission Control

Backend (보호받는 영역):
  - Reservation: Kotlin + Spring WebFlux
  - Inventory: Go + gRPC
  - Payment: Go + gRPC

Database:
  - DynamoDB (On-demand)
  - ElastiCache Redis

Monitoring:
  - CloudWatch
  - Prometheus + Grafana
  - pprof (Go profiling)

Infrastructure:
  - EKS (Kubernetes)
  - Karpenter (Auto-scaling)
  - ArgoCD (GitOps)
```

### 성과 지표
```yaml
Performance:
  - 처리량: 10k → 30k RPS (3배 향상)
  - Redis CPU: 100% → 40% (10k RPS 기준)
  - Gateway CPU: 15% (여유)
  - Backend CPU: 5% 미만 (매우 여유)
  
Optimization:
  - KEYS → Index: ~100x faster
  - Memory 최적화: 200Mi → 80Mi (idle)
  - Readiness: Pod 시작 안정화
  
Cost:
  - 추가 비용: 0원
  - 노드 증설: 불필요
  - 코드 최적화만으로 달성
  
Reliability:
  - HPA scale-down: 정상화
  - Rolling update: 안정화
  - Error rate: < 1%
```

---

**작성**: Traffic Tacos Team  
**일자**: 2025-10-08  
**버전**: 2.0 (실전 경험 기반 재구성)
