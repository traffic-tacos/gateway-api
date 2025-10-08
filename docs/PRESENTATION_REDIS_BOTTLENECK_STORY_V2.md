# 30만 명의 트래픽, 어떻게 처리할 것인가?
## - 티켓팅 시스템 설계부터 최적화까지의 여정

---

## 📜 1장: 문제 정의

### 슬라이드
```
"인기 콘서트 티켓팅, 예매 오픈 순간"

30만 명이 동시에 접속
    ↓
초당 수만 건의 요청 폭주
    ↓
좌석은 1만 석뿐
    ↓

문제:
├─ 30만 명을 어떻게 처리할 것인가?
├─ 1만 석을 어떻게 공정하게 분배할 것인가?
├─ 시스템은 어떻게 버틸 것인가?
└─ 비용은 얼마나 들 것인가?
```

### 멘트

인기 콘서트 티켓팅을 생각해 보세요.

예매 오픈 직전, **30만 명이 동시에 대기**합니다.
오픈과 동시에 **초당 수만 건의 요청**이 몰려옵니다.
하지만 좌석은 **1만 석**뿐입니다.

이때 우리가 풀어야 할 문제는 무엇일까요?

**첫째, 트래픽 처리**
- 30만 명의 동시 접속을 어떻게 받을 것인가?
- 서버가 터지지 않으려면?

**둘째, 공정성**
- 누가 먼저 들어왔는지 어떻게 판단할 것인가?
- 봇이 아닌 실제 사용자를 어떻게 보호할 것인가?

**셋째, 재고 관리**
- 1만 석을 어떻게 오버셀 없이 판매할 것인가?
- 동시에 같은 좌석을 선택하면?

**넷째, 비용**
- 짧은 피크 때문에 인프라를 얼마나 증설해야 하나?
- 평상시에는 자원이 놀지 않을까?

이 모든 문제를 어떻게 풀 것인가?
그것이 오늘의 주제입니다.

---

## 📜 2장: 아키텍처 선택지

### 슬라이드
```
"3가지 아키텍처 패턴"

패턴 1: Direct Access (직접 접근)
Browser → Reservation API → Inventory API → DB
✅ 단순함
❌ DB가 터짐, 오버셀 위험, 봇 방어 안됨

패턴 2: Queue Service (외부 큐)
Browser → SQS/SNS → Lambda → Reservation API
✅ 비동기 처리
❌ 응답 지연, 사용자 경험 나쁨, 비용 높음

패턴 3: Gateway + Admission Control (입장 제어)
Browser → Gateway (대기열) → Reservation API
✅ 실시간 응답, 제어 가능, 공정성 보장
❌ Gateway 부하 집중 (Trade-off)
```

### 멘트

30만 명의 트래픽을 처리하는 방법에는 크게 3가지가 있습니다.

**패턴 1: Direct Access - 직접 접근**
```
Browser → Reservation API → Inventory API → DB
```

가장 단순한 방법입니다.
하지만 문제가 많습니다.
- 30만 명이 동시에 DB를 치면? → **DB가 터집니다**
- 동시에 같은 좌석을 선택하면? → **오버셀 발생**
- 봇 트래픽은? → **방어 불가능**

**패턴 2: Queue Service - 외부 큐 서비스**
```
Browser → SQS/SNS → Lambda → Reservation API
```

비동기 처리로 부하를 분산합니다.
하지만 이것도 문제가 있습니다.
- 사용자는 언제 처리되는지 모릅니다 → **응답 지연**
- 실시간 피드백이 없습니다 → **사용자 경험 나쁨**
- Lambda 호출 비용이 폭발합니다 → **비용 높음**

**패턴 3: Gateway + Admission Control - 입장 제어**
```
Browser → Gateway (대기열 관리) → Reservation API
```

Gateway에서 대기열을 관리하고, 입장을 제어합니다.
- 사용자는 자신의 순번을 **실시간으로 확인**
- Gateway가 입장을 제어해서 **뒷단 보호**
- 공정성을 **보장**

하지만 Trade-off가 있습니다.
- **Gateway에 부하가 집중**됩니다
- Gateway가 **SPOF(Single Point of Failure)**가 될 수 있습니다

우리는 **패턴 3**을 선택했습니다.
왜 그랬을까요?

---

## 📜 3장: 왜 Gateway + 입장 제어인가?

### 슬라이드
```
"우리의 설계 철학"

핵심 원칙 3가지:

1️⃣ 실시간 피드백
   "지금 몇 번째인지 알려줘야 한다"
   → Gateway에서 실시간 순번 제공

2️⃣ 뒷단 보호
   "DB와 API는 터지면 안 된다"
   → Gateway에서 입장 제어 (Rate Limiting)

3️⃣ 공정성 보장
   "먼저 온 사람이 먼저 들어가야 한다"
   → 대기열 순서 관리


Trade-off 수용:
✅ Gateway 부하 집중 → 이건 받아들이자
✅ Gateway 최적화 → 여기에 집중 투자하자
```

### 멘트

우리가 Gateway + 입장 제어를 선택한 이유는 명확합니다.

**첫째, 실시간 피드백이 필수였습니다.**
티켓팅 사용자는 "지금 몇 번째인지" 알고 싶어 합니다.
비동기 큐 방식으로는 이게 불가능합니다.
Gateway에서 대기열을 관리하면, 실시간으로 순번을 알려줄 수 있습니다.

**둘째, 뒷단 보호가 필수였습니다.**
30만 명이 동시에 DB를 치면, DB가 터집니다.
Reservation API가 동시에 1만 건을 처리하면, API가 터집니다.
Gateway에서 입장을 제어하면, 뒷단을 보호할 수 있습니다.
- 예: 초당 1,000명만 입장 허용
- Reservation API는 1,000 RPS만 받음
- 안전합니다

**셋째, 공정성이 필수였습니다.**
먼저 온 사람이 먼저 들어가야 합니다.
Gateway에서 대기열 순서를 관리하면, 공정성을 보장할 수 있습니다.

**하지만 Trade-off가 있습니다.**
Gateway에 부하가 집중됩니다.
Gateway가 터지면, 전체 시스템이 마비됩니다.

우리는 이 Trade-off를 **수용**하기로 했습니다.
대신, **Gateway 최적화에 집중 투자**하기로 했습니다.

---

## 📜 4장: Gateway + Redis 설계

### 슬라이드
```
"왜 Redis인가?"

대기열 관리에 필요한 것:
├─ 빠른 조회 (ms 단위)
├─ 순서 보장 (FIFO)
├─ 원자적 연산 (동시성 제어)
├─ TTL 자동 관리 (만료 처리)
└─ 분산 환경 지원


Redis의 강점:
✅ O(log N) 조회 (ZSET)
✅ Lua Script (원자성)
✅ TTL 자동 만료
✅ Cluster Mode (확장성)
✅ Read Replica (읽기 분산)


다른 선택지는?
❌ DynamoDB: 읽기/쓰기 비용 높음, 조회 느림
❌ PostgreSQL: 동시성 제어 어려움, 확장성 부족
❌ In-Memory: 분산 환경 지원 안됨, 데이터 유실
```

### 멘트

그렇다면, 왜 Redis를 선택했을까요?

대기열 관리에 필요한 것들을 나열해 봤습니다.
- **빠른 조회**: 사용자는 5초마다 순번을 조회합니다. ms 단위 응답 필요.
- **순서 보장**: 먼저 온 사람이 먼저 들어가야 합니다.
- **원자적 연산**: 동시에 여러 명이 들어와도, 순서가 꼬이면 안 됩니다.
- **TTL 자동 관리**: 비활성 사용자는 자동으로 제거되어야 합니다.
- **분산 환경**: Gateway 인스턴스가 여러 개여도 작동해야 합니다.

Redis는 이 모든 요구사항을 만족합니다.
- **ZSET**: O(log N) 조회, 순서 보장
- **Lua Script**: 원자적 연산 (ACID 보장)
- **TTL**: 자동 만료 처리
- **Cluster Mode**: 샤드 분산으로 확장성
- **Read Replica**: 읽기 부하 분산

**다른 선택지는 없었나요?**

**DynamoDB?**
- 쓰기 비용이 너무 높습니다 (30만 건 쓰기 = $$$$)
- 읽기도 느립니다 (수십 ms)
- 순서 보장이 어렵습니다

**PostgreSQL?**
- 동시성 제어가 어렵습니다 (Lock 경합)
- 확장성이 부족합니다 (수직 확장만 가능)

**In-Memory (map)?**
- 분산 환경을 지원하지 않습니다
- 인스턴스가 죽으면 데이터 유실

**결론: Redis가 최적이었습니다.**

---

## 📜 5장: 설계 구조

### 슬라이드
```
"Gateway + Redis 아키텍처"

Browser (30만 명)
    ↓
    ↓ Queue API (대기열)
    ↓ - Join: 대기열 참여
    ↓ - Status: 순번 조회 (5초마다)
    ↓ - Enter: 입장 시도
    ↓
┌─────────────────────────────────┐
│  Gateway API (Go + Fiber)       │
│  ┌──────────────────────────┐   │
│  │  Redis (ElastiCache)     │   │
│  │  - ZSET: 순서 관리       │   │
│  │  - Lua: 원자적 연산      │   │
│  │  - TTL: 자동 만료        │   │
│  └──────────────────────────┘   │
│                                  │
│  입장 제어 (1,000 RPS)          │
└─────────────────────────────────┘
    ↓
    ↓ 입장 허용된 사용자만 통과
    ↓
┌─────────────────────────────────┐
│  Backend Services               │
│  - Reservation API (예약)       │
│  - Inventory API (재고)         │
│  - Payment API (결제)           │
└─────────────────────────────────┘
    ↓
DynamoDB (영구 저장)
```

### 멘트

우리가 설계한 아키텍처입니다.

**Browser (30만 명)**
- 티켓팅 오픈 시 대기
- 5초마다 자신의 순번 조회 (Polling)
- 순번이 되면 입장 시도

**Gateway API (Go + Fiber)**
- Queue API 제공:
  - `POST /queue/join`: 대기열 참여
  - `GET /queue/status`: 순번 조회
  - `POST /queue/enter`: 입장 시도
- Redis로 대기열 관리
- 입장 제어: 초당 1,000명만 허용

**Redis (ElastiCache)**
- **ZSET**: 대기열 순서 관리 (timestamp 기반)
- **Lua Script**: 원자적 Enqueue/Dequeue
- **TTL**: 비활성 사용자 자동 제거 (Heartbeat 기반)

**Backend Services**
- 입장 허용된 사용자만 접근 가능
- Reservation API: 예약 생성 및 관리
- Inventory API: 재고 차감 (오버셀 방지)
- Payment API: 결제 처리

**DynamoDB**
- 예약/주문 데이터 영구 저장
- 쓰기는 초당 1,000건만 (Gateway 제어 덕분)

**핵심 아이디어:**
Gateway + Redis가 **방파제** 역할을 합니다.
30만 명의 트래픽을 받아내고,
뒷단은 안전하게 보호됩니다.

---

## 📜 6장: 초기 구현 및 테스트

### 슬라이드
```
"1차 부하 테스트: 1,000 RPS"

┌────────────────────────────────┐
│ Component       │ CPU  │ Result │
├────────────────────────────────┤
│ Gateway API     │ 5%   │ ✅      │
│ Redis           │ 10%  │ ✅      │
│ Reservation API │ 3%   │ ✅      │
│ DynamoDB        │ 5%   │ ✅      │
└────────────────────────────────┘

모든 컴포넌트: 여유로움
결론: 아키텍처가 작동한다! 🎉

다음 목표: 10,000 RPS
```

### 멘트

초기 구현을 마치고, 첫 부하 테스트를 돌렸습니다.
**1,000 RPS**로 시작했습니다.

결과는 완벽했습니다.
- Gateway API: CPU 5%
- Redis: CPU 10%
- Reservation API: CPU 3%
- DynamoDB: 쓰기 5%

모든 컴포넌트가 여유로웠습니다.

"**좋아, 아키텍처가 작동한다!**"

이제 목표는 **10,000 RPS**입니다.
점진적으로 부하를 올려보기로 했습니다.

---

## 📜 7장: 10k RPS에서 병목 발견

### 슬라이드
```
"10,000 RPS 부하 테스트 결과"

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

예상: "Gateway가 터질 줄 알았는데..."
실제: "Redis가 터졌네?"


왜? 🤔
```

### 멘트

부하를 10,000 RPS로 올렸습니다.

그리고 모니터링을 확인하는 순간,
예상과 다른 결과를 발견했습니다.

**Gateway API**: CPU 15% - 여전히 여유롭습니다.
**Reservation API**: CPU 5% - 놀고 있습니다.
**Inventory API**: CPU 3% - 더 놀고 있습니다.
**DynamoDB**: 10% - 문제없습니다.

그런데 딱 하나,
**ElastiCache Redis: CPU 100%** 🔴

예상했던 것과 달랐습니다.
우리는 "Gateway가 터질 줄" 알았습니다.
하지만 터진 건 Redis였습니다.

**왜 이런 일이 벌어진 걸까요?**

Gateway에 부하를 집중시키는 설계였으니,
Redis에 부하가 집중되는 건 어쩌면 당연한 결과였습니다.

하지만 **10,000 RPS에서 이미 한계**라니,
30,000 RPS 목표는 요원해 보였습니다.

**문제를 찾아야 했습니다.**

---

## 📜 8장: 병목의 근본 원인

### 슬라이드
```
"Redis CPU 100%, 왜?"

CloudWatch 메트릭 분석:
├─ CommandsProcessed: 초당 10만+ 건
├─ NetworkBytesIn: 비정상적으로 높음
└─ EngineCPUUtilization: 지속적 100%

pprof 프로파일링:
├─ calculateGlobalPosition() 함수
├─ Redis KEYS 명령어 호출
└─ 초당 10,000번 실행!


Redis KEYS 명령어:
❌ O(N) 시간 복잡도 (모든 키 스캔)
❌ Blocking operation (다른 명령어 대기)
❌ Cluster Mode에서 모든 샤드 스캔
❌ 10k RPS = 초당 1만번 KEYS 실행!


범인: Status API
```

### 멘트

Redis CPU 100%의 원인을 찾아야 했습니다.

**CloudWatch 메트릭을 분석**했습니다.
- `CommandsProcessed`: 초당 10만+ 건
- `NetworkBytesIn`: 비정상적으로 높음
- `EngineCPUUtilization`: 지속적으로 100%

뭔가 과도한 명령어가 실행되고 있었습니다.

**pprof로 프로파일링**했습니다.
Go의 `pprof`를 켜고, CPU 프로파일을 분석했습니다.

범인을 찾았습니다.
```go
// internal/queue/streams.go
func (sq *StreamQueue) calculateGlobalPosition(...) int {
    pattern := fmt.Sprintf("stream:event:{%s}:user:*", eventID)
    keys, err := sq.redis.Keys(ctx, pattern).Result() // 🔴 범인!
    // ...
}
```

**`calculateGlobalPosition()` 함수**가 문제였습니다.
이 함수는 **Status API**에서 호출됩니다.

**Status API**는 사용자가 자신의 순번을 조회하는 API입니다.
- 사용자는 5초마다 폴링합니다
- 1만 명이 대기 중이면? → 초당 2,000건의 Status API 호출
- 10만 명이 대기 중이면? → 초당 20,000건!

그리고 이 함수가 **`KEYS` 명령어**를 사용하고 있었습니다.

**`KEYS` 명령어의 문제점:**
- **O(N) 시간 복잡도**: 모든 키를 스캔합니다
- **Blocking operation**: 실행 중 다른 명령어를 막습니다
- **Cluster Mode**: 모든 샤드를 스캔합니다
- **프로덕션 금지**: Redis 공식 문서에서도 사용 금지

10,000 RPS Status API면,
**초당 10,000번의 KEYS 명령어**가 실행되는 겁니다.

Redis가 버틸 수 없었죠.

**왜 이런 코드를 짰을까요?**
초기에는 "빨리 만들자"에 집중했고,
성능 최적화는 나중에 하려고 했습니다.
하지만 10k RPS에서 이미 한계에 도달했습니다.

---

## 📜 9장: 해결 방법 - Position Index

### 슬라이드
```
"KEYS 제거, Index 도입"

Before (KEYS scan):
┌─────────────────────────────────┐
│ Status API 호출                 │
│   ↓                             │
│ calculateGlobalPosition()       │
│   ↓                             │
│ KEYS stream:event:{evt}:user:*  │
│   ↓ O(N) = 10,000+ keys         │
│   ↓ ~100ms (blocking)           │
│   ↓ CPU 100% 💥                 │
└─────────────────────────────────┘

After (Position Index):
┌─────────────────────────────────┐
│ Status API 호출                 │
│   ↓                             │
│ CalculateApproximatePosition()  │
│   ↓                             │
│ ZRANK queue:event:{evt}:position│
│   ↓ O(log N) = ~14 comparisons  │
│   ↓ <1ms (non-blocking)         │
│   ↓ CPU 40% ✅                  │
└─────────────────────────────────┘

성능 개선: ~100배!
```

### 멘트

해결 방법은 명확했습니다.
**KEYS를 없애고, 인덱스를 만들자.**

**Position Index**라는 개념을 도입했습니다.
ZSET 기반의 인덱스입니다.

**작동 방식:**

1. **Join 시 (대기열 참여):**
   ```go
   // Position Index에 추가
   indexKey := "queue:event:{evt}:position"
   score := time.Now().UnixNano() // timestamp
   redis.ZAdd(ctx, indexKey, redis.Z{Score: score, Member: token})
   ```

2. **Status 시 (순번 조회):**
   ```go
   // Position Index에서 rank 조회
   rank, err := redis.ZRank(ctx, indexKey, token).Result()
   position := int(rank) + 1 // 0-based → 1-based
   ```

**시간 복잡도 비교:**
- `KEYS`: O(N) - 10,000 keys면 ~100ms
- `ZRANK`: O(log N) - 10,000 keys면 ~14 comparisons, <1ms

**100배 빠릅니다!**

그리고 무엇보다,
`ZRANK`는 **non-blocking**입니다.
다른 명령어를 방해하지 않습니다.

**구현 후 테스트:**
- Redis CPU: 100% → 40%
- Status API 응답 시간: 100ms → <5ms
- 처리량: 10k RPS → 여유로움

**드디어 병목을 해결했습니다!**

---

## 📜 10장: 추가 최적화들

### 슬라이드
```
"세부 튜닝"

3가지 추가 최적화:

1️⃣ Redis Connection Pool
   PoolSize: 100 → 1000
   PoolTimeout: 4s → 10s
   → 고부하에서 connection pool timeout 제거

2️⃣ Go 메모리 최적화
   GOMEMLIMIT=450MiB
   GOGC=75
   → GC 적극 실행, HPA scale-down 정상화

3️⃣ Readiness Probe 조정
   initialDelaySeconds: 5 → 15초
   → Redis Cluster 연결 대기
   → Pod 시작 안정화
```

### 멘트

Position Index로 큰 병목을 해결했지만,
세부 튜닝이 더 필요했습니다.

**첫째, Redis Connection Pool**

고부하에서 `redis: connection pool timeout` 에러가 발생했습니다.
- 기본 Pool Size가 100으로 부족
- Timeout이 4초로 짧음

**해결:**
- `PoolSize: 1000` (10배 증가)
- `PoolTimeout: 10s` (2.5배 증가)
- `MinIdleConns: 100` (warm pool)

**둘째, Go 메모리 최적화**

테스트 후에도 메모리가 해제되지 않아,
HPA가 scale-down을 못 하는 문제가 있었습니다.

**해결:**
- `GOMEMLIMIT=450MiB`: Go GC가 메모리 한계 인식
- `GOGC=75`: 더 자주 GC 실행 (기본값 100)
- `GODEBUG=madvdontneed=1`: 메모리 즉시 반환

결과: 테스트 후 메모리 200Mi → 80Mi

**셋째, Readiness Probe**

Pod 시작 시 Redis Cluster 연결에 10초 정도 걸리는데,
Readiness Probe가 5초만 기다리고 NotReady 판정.

**해결:**
- `initialDelaySeconds: 15초` (충분한 대기)
- `periodSeconds: 5초` (체크 간격 완화)
- `failureThreshold: 3` (실패 허용 증가)

결과: Rolling update가 부드럽게 진행

---

## 📜 11장: 최종 결과

### 슬라이드
```
"30,000 RPS 달성!"

┌────────────────────────────────────────┐
│ RPS     │ Redis CPU │ Result          │
├────────────────────────────────────────┤
│ Before  (KEYS 사용)                    │
│ 10k     │ 100%      │ ❌ 한계 도달     │
│ 15k+    │ N/A       │ ❌ 불가능        │
├────────────────────────────────────────┤
│ After   (Position Index)               │
│ 10k     │ 40%       │ ✅ 여유          │
│ 20k     │ 70%       │ ✅ 안정          │
│ 30k     │ 90%       │ ✅ 목표 달성!    │
└────────────────────────────────────────┘

추가 비용: 0원 (코드 최적화만)
노드 증설: 불필요
ElastiCache: cache.m7g.xlarge × 10 nodes 유지
```

### 멘트

최종 부하 테스트 결과입니다.

**최적화 전 (KEYS 사용):**
- 10k RPS: Redis CPU 100% - 한계 도달
- 15k RPS 이상: 불가능

**최적화 후 (Position Index):**
- **10k RPS**: Redis CPU 40% - 여유롭습니다
- **20k RPS**: Redis CPU 70% - 안정적입니다
- **30k RPS**: Redis CPU 90% - **목표 달성!** 🎉

그리고 무엇보다,
**추가 비용이 들지 않았습니다.**

- Redis 노드 증설? → 불필요
- EKS 노드 증설? → 불필요
- 코드 최적화만으로 달성

**cache.m7g.xlarge × 10 nodes** 그대로
30k RPS를 처리할 수 있게 되었습니다.

---

## 📜 12장: 설계 검증

### 슬라이드
```
"아키텍처 설계가 맞았다"

우리의 가설:
"Gateway + Redis가 방파제 역할을 할 것이다"

검증 결과:
✅ Gateway + Redis가 30만 명 트래픽 받아냄
✅ 입장 제어로 뒷단 보호 (1,000 RPS로 제한)
✅ Backend CPU < 5% (매우 여유로움)
✅ DynamoDB 10% 사용 (쓰기 비용 절감)
✅ 비용 증가 없이 30k RPS 달성


Trade-off 수용:
"Gateway 부하 집중" → 최적화로 해결
"Redis 병목" → Position Index로 해결


설계 철학 검증:
✅ 실시간 피드백 (순번 조회)
✅ 뒷단 보호 (입장 제어)
✅ 공정성 보장 (대기열 관리)
✅ 비용 효율성 (코드 최적화)
```

### 멘트

가장 중요한 교훈은,
**우리의 아키텍처 설계가 맞았다**는 겁니다.

**처음 설계 단계에서 세운 가설:**
"Gateway + Redis가 방파제 역할을 할 것이다"

**검증 결과:**
- Gateway + Redis가 30만 명의 트래픽을 받아냈습니다
- 입장 제어로 뒷단을 보호했습니다 (1,000 RPS로 제한)
- Backend 서비스들은 CPU 5% 미만으로 여유로웠습니다
- DynamoDB는 10%만 사용했습니다 (쓰기 비용 절감)
- 비용 증가 없이 30k RPS를 달성했습니다

**Trade-off 수용:**
우리는 "Gateway에 부하가 집중될 것"을 알고 있었습니다.
하지만 이 Trade-off를 수용하고, 최적화에 집중했습니다.
- Position Index 도입
- Connection Pool 튜닝
- Go 메모리 최적화

**설계 철학 검증:**
1. **실시간 피드백**: 사용자는 5초마다 순번 확인 ✅
2. **뒷단 보호**: Backend CPU < 5% ✅
3. **공정성 보장**: 대기열 순서 관리 ✅
4. **비용 효율성**: 추가 비용 0원 ✅

만약 다른 아키텍처를 선택했다면?
- Direct Access: DB가 터졌을 겁니다
- Queue Service: 사용자 경험이 나빴을 겁니다

**Gateway + 입장 제어가 정답이었습니다.**

---

## 📜 13장: 설계 과정의 교훈

### 슬라이드
```
"설계부터 최적화까지의 교훈"

1️⃣ 요구사항 먼저
   "30만 명, 실시간 피드백, 공정성"
   → 아키텍처가 따라옴

2️⃣ Trade-off 인정
   "Gateway 부하 집중" → 수용하고 최적화

3️⃣ 적절한 도구 선택
   "대기열 = Redis ZSET" → 딱 맞음

4️⃣ 초기 구현 ≠ 최종
   "일단 작동하게, 그 다음 최적화"

5️⃣ 모니터링 필수
   "CloudWatch + pprof" → 병목 발견

6️⃣ 코드가 답
   "노드 증설 < 코드 최적화"

7️⃣ 검증 중요
   "설계가 맞았는지 테스트로 증명"
```

### 멘트

이번 여정에서 배운 교훈들을 정리하겠습니다.

**첫째, 요구사항이 먼저입니다.**
"30만 명을 처리해야 한다" → 기술 선택
"실시간 피드백이 필요하다" → Gateway 패턴
"공정성을 보장해야 한다" → 대기열 관리

기술이 먼저가 아니라, 요구사항이 먼저입니다.

**둘째, Trade-off를 인정해야 합니다.**
"Gateway에 부하가 집중될 것이다" → 알고 있었습니다.
하지만 이게 최선이라고 판단했습니다.
그리고 최적화에 집중했습니다.

**셋째, 적절한 도구를 선택해야 합니다.**
대기열 관리 = Redis ZSET
이만큼 딱 맞는 도구가 없었습니다.

**넷째, 초기 구현과 최종은 다릅니다.**
"일단 작동하게 만들자" → KEYS 사용
"이제 최적화하자" → Position Index

빠르게 검증하고, 점진적으로 개선하는 게 중요합니다.

**다섯째, 모니터링이 필수입니다.**
CloudWatch 없었으면, Redis CPU 100%를 몰랐을 겁니다.
pprof 없었으면, KEYS가 문제인지 몰랐을 겁니다.

**여섯째, 코드가 답입니다.**
"Redis 노드를 늘릴까?" → 비용 증가
"코드를 최적화하자" → 비용 0원

대부분의 문제는 코드 최적화로 해결할 수 있습니다.

**일곱째, 검증이 중요합니다.**
"이 설계가 맞을까?" → 부하 테스트로 증명
"정말 30k RPS를 처리할까?" → 실제로 처리함

설계가 맞았는지는, 테스트로 증명해야 합니다.

---

## 📜 14장: 실전 팁

### 슬라이드
```
"Redis 사용 시 주의사항"

🚫 절대 하지 말아야 할 것들:
1. KEYS 명령어 사용 (O(N) blocking)
   → SCAN 또는 Index 사용
   
2. 작은 Connection Pool (고부하 대비 안됨)
   → 최소 1000 이상 권장
   
3. 기본 Timeout 설정 (Redis 응답 대기 필요)
   → 10s 이상 여유있게
   
4. Cluster Mode 무시 (Hash tag 필수)
   → {key} 형식으로 같은 slot에 매핑


✅ 반드시 해야 할 것들:
1. 적절한 자료구조 (ZSET for Index)
2. Lua Script (원자적 연산)
3. TTL 설정 (자동 만료)
4. Read Replica 활용 (읽기 분산)
5. CloudWatch 모니터링 (CPU, Network)
6. pprof 프로파일링 (병목 찾기)
```

### 멘트

마지막으로, 실전 팁들을 공유하겠습니다.

**Redis 사용 시 절대 하지 말아야 할 것:**

1. **KEYS 명령어 사용**
   - Production에서 절대 금지
   - 대신 SCAN이나 Index 사용
   - 우리의 교훈: KEYS → 100배 느림

2. **작은 Connection Pool**
   - 기본값 100은 부족
   - 최소 1000 이상 권장
   - 고부하 대비 필수

3. **짧은 Timeout**
   - 2-3초는 부족
   - 10초 이상 여유있게
   - Redis 응답 대기 필요

4. **Cluster Mode 무시**
   - Hash tag 사용 필수
   - `{eventID}` 형식으로 같은 slot 매핑
   - CROSSSLOT 에러 방지

**반드시 해야 할 것:**

1. **적절한 자료구조**
   - ZSET으로 Index 만들기
   - O(log N) 조회 보장

2. **Lua Script**
   - 원자적 연산 보장
   - Race condition 방지

3. **TTL 설정**
   - 비활성 데이터 자동 만료
   - 메모리 관리

4. **Read Replica**
   - 읽기 부하 분산
   - `RouteByLatency: true`

5. **모니터링**
   - CloudWatch 메트릭 확인
   - CPU/Network/Commands 추적

6. **프로파일링**
   - pprof로 병목 찾기
   - CPU/Memory/Goroutine 분석

---

## 📜 15장: 마무리

### 슬라이드
```
"30만 명의 트래픽, 어떻게 처리했나?"

여정 요약:
1️⃣ 문제 정의: 30만 명, 실시간, 공정성
2️⃣ 설계 고민: Direct vs Queue vs Gateway
3️⃣ 우리의 선택: Gateway + Redis + 입장 제어
4️⃣ Trade-off 수용: Gateway 부하 집중
5️⃣ 구현 및 검증: 1k RPS → 작동 확인
6️⃣ 병목 발견: 10k RPS → Redis CPU 100%
7️⃣ 근본 원인: KEYS 명령어 (O(N))
8️⃣ 해결: Position Index (O(log N))
9️⃣ 세부 튜닝: Pool, Memory, Probe
🔟 결과: 30k RPS, 비용 0원


핵심 교훈:
"요구사항 → 설계 → 구현 → 검증 → 최적화"
"Trade-off를 인정하고, 집중 투자"
"병목은 예상 밖의 곳에서, 해결책은 가까운 곳에"


다음 목표: 100k RPS? Multi-region? 🚀
```

### 멘트

정리하겠습니다.

**"30만 명의 트래픽, 어떻게 처리할 것인가?"**

이 질문에서 시작했습니다.

**설계 단계:**
- 3가지 아키텍처 패턴을 비교했습니다
- Gateway + 입장 제어를 선택했습니다
- Redis ZSET으로 대기열을 관리하기로 했습니다
- Trade-off를 수용하고, 최적화에 집중하기로 했습니다

**구현 및 검증:**
- 1,000 RPS 테스트 → 작동 확인
- 10,000 RPS 테스트 → 병목 발견 (Redis CPU 100%)
- pprof 프로파일링 → 근본 원인 발견 (KEYS 명령어)

**해결 및 최적화:**
- Position Index 도입 (KEYS → ZRANK)
- 성능 100배 향상 (O(N) → O(log N))
- 세부 튜닝 (Pool, Memory, Probe)

**최종 결과:**
- 30,000 RPS 달성
- 추가 비용 0원
- 설계 검증 완료

**핵심 교훈:**

1. **요구사항이 먼저**
   - 기술이 아닌, 요구사항이 설계를 이끕니다

2. **Trade-off를 인정**
   - 완벽한 설계는 없습니다
   - Trade-off를 수용하고, 집중 투자합니다

3. **점진적 검증**
   - 빠르게 구현하고, 테스트로 검증합니다
   - 병목을 찾고, 최적화합니다

4. **코드가 답**
   - 인프라 증설보다, 코드 최적화가 먼저입니다

5. **모니터링과 프로파일링**
   - 병목은 숨어 있습니다
   - 도구로 찾아야 합니다

**그리고 무엇보다:**
병목은 예상 밖의 곳에서 발견되고,
해결책은 의외로 가까운 곳에 있습니다.

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
Architecture Pattern:
  - Gateway + Admission Control
  - Trade-off: Gateway 부하 집중 수용

Gateway (방파제):
  - Go + Fiber
  - ElastiCache Redis Cluster (5 shards, 10 nodes)
  - Position Index (ZSET) - O(log N)
  - Admission Control (1,000 RPS)

Backend (보호받는 영역):
  - Reservation: Kotlin + Spring WebFlux
  - Inventory: Go + gRPC
  - Payment: Go + gRPC

Database:
  - DynamoDB (On-demand)
  - ElastiCache Redis

Monitoring & Profiling:
  - CloudWatch (CPU, Network, Commands)
  - Prometheus + Grafana
  - pprof (Go CPU/Memory profiling)

Infrastructure:
  - EKS (Kubernetes)
  - Karpenter (Auto-scaling)
  - ArgoCD (GitOps)
```

### 설계 결정 (Design Decisions)
```yaml
Why Gateway + Admission Control?
  ✅ 실시간 피드백 (순번 조회)
  ✅ 뒷단 보호 (입장 제어)
  ✅ 공정성 보장 (대기열 관리)
  ❌ Gateway 부하 집중 (수용)

Why Redis?
  ✅ O(log N) 조회 (ZSET)
  ✅ 원자적 연산 (Lua Script)
  ✅ TTL 자동 만료
  ✅ Cluster Mode (확장성)
  ✅ Read Replica (읽기 분산)
  ❌ DynamoDB: 비용 높음, 느림
  ❌ PostgreSQL: 동시성 제어 어려움

Why Position Index?
  ✅ O(log N) vs O(N) (100배 빠름)
  ✅ Non-blocking (ZRANK)
  ✅ 간단한 구현
  ❌ KEYS: 절대 금지
```

### 성과 지표
```yaml
Performance:
  - 처리량: 10k → 30k RPS (3배 향상)
  - Redis CPU: 100% → 40% (10k RPS 기준)
  - Gateway CPU: 15% (여유)
  - Backend CPU: 5% 미만 (매우 여유)
  - Status API: 100ms → <5ms
  
Optimization:
  - KEYS → Index: ~100x faster
  - O(N) → O(log N): 시간 복잡도 개선
  - Memory: 200Mi → 80Mi (idle)
  - Readiness: Pod 시작 안정화
  
Cost:
  - 추가 비용: 0원
  - 노드 증설: 불필요
  - 코드 최적화만으로 달성
  - ElastiCache: cache.m7g.xlarge × 10 (유지)
  
Reliability:
  - HPA scale-down: 정상화
  - Rolling update: 안정화
  - Error rate: < 1%
  - Readiness probe failure: 제거
```

### 설계 검증 결과
```yaml
가설:
  "Gateway + Redis가 방파제 역할을 할 것이다"

검증:
  ✅ 30만 명 트래픽 받아냄
  ✅ 입장 제어로 뒷단 보호
  ✅ Backend CPU < 5%
  ✅ DynamoDB 10% 사용
  ✅ 비용 0원 추가
  ✅ 30k RPS 달성

Trade-off 해결:
  "Gateway 부하 집중" → Position Index 최적화
  "Redis 병목" → O(log N) 개선

설계 원칙 달성:
  ✅ 실시간 피드백
  ✅ 뒷단 보호
  ✅ 공정성 보장
  ✅ 비용 효율성
```

---

**작성**: Traffic Tacos Team  
**일자**: 2025-10-08  
**버전**: 2.0 (설계 과정 중심 재구성)  
**변경**: 결과 중심 → 설계 고민 및 Trade-off 분석 추가
