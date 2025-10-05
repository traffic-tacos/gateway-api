# Gateway API Documentation

Gateway API의 전체 문서 인덱스입니다. 30k RPS를 견디는 Cloud Native 아키텍처의 설계, 구현, 배포, 발표 자료가 포함되어 있습니다.

---

## 📚 문서 카테고리

### 1. 🎯 핵심 기술 문서

#### Queue Management System

| 문서 | 설명 | 길이 | 대상 독자 |
|---|---|---|---|
| **[QUEUE_ALGORITHMS.md](QUEUE_ALGORITHMS.md)** | ETA 계산 및 Admission Control 알고리즘 상세 설명 | 중 | 개발자, 아키텍트 |
| **[QUEUE_WORKFLOW.md](QUEUE_WORKFLOW.md)** | Redis 기반 대기열 시스템 워크플로우 및 Flow 다이어그램 | 중 | 개발자, 운영팀 |

**주요 내용**:
- **ETA 계산**: Exponential Moving Average (EMA), Sliding Window Counter, Machine Learning
- **Admission Control**: Token Bucket, Leaky Bucket, Adaptive Rate Limiting
- **Redis 구조**: ZSet, Streams, Lua Scripts
- **Flow 다이어그램**: Join → Status → Enter → Admission

---

### 2. 🔧 Phase 1 구현 문서

#### Phase 1: Lua Script + Redis Streams 통합

| 문서 | 설명 | 길이 | 중요도 |
|---|---|---|---|
| **[REFACTORING_PLAN.md](REFACTORING_PLAN.md)** | 30k RPS 대응 리팩터링 계획 (피드백 기반) | 1,006줄 | ⭐⭐⭐⭐⭐ |
| **[PHASE1_IMPLEMENTATION_GUIDE.md](PHASE1_IMPLEMENTATION_GUIDE.md)** | Phase 1 구현 상세 가이드 (단계별 설명) | 710줄 | ⭐⭐⭐⭐⭐ |
| **[PHASE1_GATEWAY_INTEGRATION.md](PHASE1_GATEWAY_INTEGRATION.md)** | Gateway API 통합 완료 보고서 (Join/Status) | 556줄 | ⭐⭐⭐⭐ |
| **[PHASE1_REDIS_TEST_SUCCESS.md](PHASE1_REDIS_TEST_SUCCESS.md)** | 로컬 Redis 테스트 성공 보고서 (10/10 통과) | 411줄 | ⭐⭐⭐⭐ |
| **[PHASE1_PROGRESS.md](PHASE1_PROGRESS.md)** | Phase 1 진행 상황 추적 보고서 | 274줄 | ⭐⭐⭐ |
| **[PHASE1_DAY3_COMPLETION.md](PHASE1_DAY3_COMPLETION.md)** | Day 3 Lua Script 완료 보고서 | 367줄 | ⭐⭐⭐ |
| **[PHASE1_REVISED.md](PHASE1_REVISED.md)** | Phase 1 수정 계획 (Streams 전환) | 215줄 | ⭐⭐ |

**Phase 1 핵심 성과**:
- ✅ Lua Executor: 원자적 연산 + 멱등성 보장
- ✅ Redis Streams: Per-User FIFO 순서 보장
- ✅ Gateway API 통합: Join/Status 메서드 개선
- ✅ 테스트 완료: 10/10 통과 (0.614초)
- ✅ 성능 개선: Join API 2배 향상 (5k → 10k RPS)

**주요 기술**:
- **Lua Scripts**: `enqueue_atomic_streams.lua`, `hold_seat_atomic.lua`, `release_seat_atomic.lua`
- **Redis Streams**: Per-User FIFO, Global Position 계산
- **Sliding Window ETA**: 다중 시간 윈도우 + 신뢰도 점수
- **Token Bucket**: 공정한 입장 제어

---

### 3. 📊 분석 및 설계 문서

| 문서 | 설명 | 길이 | 중요도 |
|---|---|---|---|
| **[COMPOSITE_SCORE_ANALYSIS.md](COMPOSITE_SCORE_ANALYSIS.md)** | ZSet Composite Score 한계 분석 (float64 정밀도 문제) | 175줄 | ⭐⭐⭐⭐ |
| **[TECHNICAL_HIGHLIGHTS.md](TECHNICAL_HIGHLIGHTS.md)** | 핵심 기술 요약 (발표용 Executive Summary) | 227줄 | ⭐⭐⭐⭐ |

**주요 내용**:
- **Composite Score 한계**: float64 정밀도 → Redis Streams 전환 결정
- **기술적 도전**: 동시성, 순서 보장, 멱등성
- **해결 방안**: Lua Script, Streams, Token Bucket

---

### 4. 🚀 배포 문서

| 문서 | 설명 | 버전 | 날짜 |
|---|---|---|---|
| **[FINAL_V1.2.0_DEPLOYMENT.md](FINAL_V1.2.0_DEPLOYMENT.md)** | v1.2.0 배포 완료 (Sliding Window ETA) | v1.2.0 | 2025-10-05 |
| **[FINAL_DEPLOYMENT_REPORT.md](FINAL_DEPLOYMENT_REPORT.md)** | 배포 검증 보고서 (알고리즘 작동 확인) | v1.1.0 | 2025-10-05 |
| **[DEPLOYMENT_SUMMARY.md](DEPLOYMENT_SUMMARY.md)** | v1.1.0 배포 요약 | v1.1.0 | 2025-10-05 |

**배포 히스토리**:
- **v1.1.0**: EMA + Token Bucket 알고리즘 배포
- **v1.2.0**: Sliding Window ETA 배포
- **v1.3.0**: Lua Script + Redis Streams 통합 (Phase 1 완료)

**CI/CD 파이프라인**:
1. GitHub Actions: Test → Build → ECR Push
2. ArgoCD: Auto-sync + Rolling Update
3. Kubernetes: 5 Pods, HPA, Readiness Probe

---

### 5. 🎤 발표 자료

#### Cloud Native Architecture Presentation

| 문서 | 설명 | 길이 | 대상 독자 |
|---|---|---|---|
| **[PRESENTATION_CLOUD_NATIVE_ARCHITECTURE.md](PRESENTATION_CLOUD_NATIVE_ARCHITECTURE.md)** | 30k RPS 아키텍처 발표 자료 (메인) | 848줄 | 전체 청중 |
| **[SUMMARY_FOR_PRESENTATION.md](SUMMARY_FOR_PRESENTATION.md)** | 발표 준비 가이드 (20-30분) | 323줄 | 발표자 |
| **[EXECUTIVE_SUMMARY.md](EXECUTIVE_SUMMARY.md)** | 경영진용 요약 (비즈니스 가치 중심) | 247줄 | 경영진 |

**발표 구성**:
1. **도입**: 30k RPS 도전 과제
2. **아키텍처**: Cloud Native 설계
3. **핵심 기술**: Lua Script, Streams, Sliding Window
4. **성과**: 성능 개선, 안정성 확보
5. **Q&A**: 기술적 질문 대응

**발표 강조 포인트**:
- ✅ **원자성**: Lua Script로 Race Condition 제거
- ✅ **순서 보장**: Redis Streams Per-User FIFO
- ✅ **멱등성**: 중복 요청 자동 차단
- ✅ **성능**: Join API 2배 향상
- ✅ **관측성**: Prometheus + OpenTelemetry

---

## 🗂️ 문서 활용 가이드

### 개발자 시작 가이드

**1. 빠른 시작** (처음 프로젝트를 접하는 경우):
```
1. README.md (프로젝트 개요)
   ↓
2. TECHNICAL_HIGHLIGHTS.md (핵심 기술 요약)
   ↓
3. QUEUE_WORKFLOW.md (대기열 시스템 이해)
   ↓
4. PHASE1_IMPLEMENTATION_GUIDE.md (구현 상세)
```

**2. 상세 학습** (깊이 있는 이해 필요):
```
1. QUEUE_ALGORITHMS.md (알고리즘 이론)
   ↓
2. REFACTORING_PLAN.md (전체 설계 이해)
   ↓
3. PHASE1_GATEWAY_INTEGRATION.md (실제 코드 통합)
   ↓
4. PHASE1_REDIS_TEST_SUCCESS.md (테스트 검증)
```

---

### 운영팀 가이드

**배포 및 모니터링**:
```
1. FINAL_V1.2.0_DEPLOYMENT.md (최신 배포 상태)
   ↓
2. DEPLOYMENT_SUMMARY.md (배포 프로세스)
   ↓
3. QUEUE_WORKFLOW.md (시스템 동작 이해)
```

**트러블슈팅**:
```
1. PHASE1_REDIS_TEST_SUCCESS.md (정상 동작 기준)
   ↓
2. FINAL_DEPLOYMENT_REPORT.md (검증 방법)
```

---

### 발표자 가이드

**발표 준비** (100명 청중 앞 발표):
```
1. SUMMARY_FOR_PRESENTATION.md (발표 가이드)
   ↓
2. PRESENTATION_CLOUD_NATIVE_ARCHITECTURE.md (메인 자료)
   ↓
3. TECHNICAL_HIGHLIGHTS.md (핵심 포인트)
   ↓
4. EXECUTIVE_SUMMARY.md (경영진 질문 대응)
```

**발표 시간**:
- **20분 발표**: PRESENTATION (핵심만 발췌)
- **30분 발표**: PRESENTATION (전체) + Q&A
- **60분 워크숍**: PRESENTATION + Implementation Guide

---

## 📈 문서 통계

### 전체 문서 요약

| 카테고리 | 문서 수 | 총 라인 수 | 비율 |
|---|---|---|---|
| 핵심 기술 | 2개 | ~500줄 | 7% |
| Phase 1 구현 | 7개 | ~3,539줄 | 50% |
| 분석 및 설계 | 2개 | ~402줄 | 6% |
| 배포 | 3개 | ~300줄 | 4% |
| 발표 자료 | 3개 | ~1,418줄 | 20% |
| **합계** | **17개** | **~7,000줄** | **100%** |

### 중요도별 분류

| 중요도 | 문서 수 | 대상 독자 |
|---|---|---|
| ⭐⭐⭐⭐⭐ | 2개 | 핵심 개발자 (필독) |
| ⭐⭐⭐⭐ | 5개 | 개발자, 아키텍트 |
| ⭐⭐⭐ | 5개 | 전체 팀원 |
| ⭐⭐ | 5개 | 참고용 |

---

## 🎯 Phase 1 완료 요약

### 최종 성과

**기술적 성과**:
- ✅ Lua Executor 구현 (3개 Script)
- ✅ Redis Streams 통합 (Per-User FIFO)
- ✅ Gateway API 통합 (Join/Status)
- ✅ Sliding Window ETA (다중 윈도우)
- ✅ 테스트 완료 (10/10)
- ✅ 문서화 (17개, 7,000줄+)

**성능 개선**:
- Join API: 5k RPS → 10k RPS (2배)
- 멱등성: 중복 요청 100% 차단
- 순서 보장: Per-User FIFO 100%
- ETA 정확도: Sliding Window + 신뢰도 점수

**배포 현황**:
- v1.3.0 배포 진행 중 (GitHub Actions)
- 예상 완료: 3-5분
- 검증 대상: Lua Script + Streams 정상 작동

---

## 🔗 관련 링크

### 외부 리소스

- **GitHub Repository**: [traffic-tacos/gateway-api](https://github.com/traffic-tacos/gateway-api)
- **ArgoCD Dashboard**: https://argocd.traffictacos.store/applications/gateway-api
- **API Endpoint**: https://api.traffictacos.store
- **Swagger Documentation**: https://api.traffictacos.store/swagger/index.html

### 내부 리소스

- **Proto Contracts**: [traffic-tacos/proto-contracts](https://github.com/traffic-tacos/proto-contracts)
- **Deployment Repo**: [traffic-tacos/deployment-repo](https://github.com/traffic-tacos/deployment-repo)
- **Infrastructure**: [traffic-tacos/infrastructure](https://github.com/traffic-tacos/infrastructure)

---

## 📝 문서 업데이트 가이드

### 새 문서 추가 시

1. **파일명 규칙**: `CATEGORY_NAME.md` (대문자, 언더스코어)
2. **카테고리 추가**: 이 README.md에 링크 추가
3. **메인 README 업데이트**: 루트 README.md의 Documentation 섹션 업데이트

### 기존 문서 수정 시

1. **변경 이력 기록**: 문서 하단에 변경 이력 추가
2. **버전 관리**: Git commit 메시지에 문서 변경 내용 명시
3. **관련 문서 확인**: 연관된 문서도 함께 업데이트 필요 여부 확인

---

## 🤝 기여 가이드

문서 개선 제안:
1. 오타 발견 시: PR 생성
2. 내용 추가: Issue 생성 후 논의
3. 구조 개선: 팀 회의 후 결정

---

**Last Updated**: 2025-10-05  
**Version**: v1.3.0 (Phase 1 완료)  
**Total Documents**: 17개  
**Total Lines**: ~7,000줄

---

For questions or clarifications, contact the development team.
