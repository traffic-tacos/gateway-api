# Troubleshooting 가이드

Gateway API의 운영 중 발생할 수 있는 문제들과 해결 방법을 정리한 문서입니다.

## 📚 문서 목록

### Redis 관련
- **[REDIS_CLUSTER_HASH_TAG_FIX.md](./REDIS_CLUSTER_HASH_TAG_FIX.md)** - Redis Cluster Mode에서 Lua Script 실행 실패 문제
  - **증상**: `QUEUE_ERROR: Failed to join queue`
  - **원인**: Hash Tag 미사용으로 인한 CROSSSLOT 에러
  - **해결**: dedupeKey에 `{eventID}` hash tag 추가

## 🔍 문제 유형별 찾기

### API 에러
- Queue Join 실패 → [REDIS_CLUSTER_HASH_TAG_FIX.md](./REDIS_CLUSTER_HASH_TAG_FIX.md)

### 인프라 에러
- Redis 연결 실패 → (작성 예정)
- ElastiCache AUTH 에러 → (작성 예정)

### 성능 문제
- 느린 응답 시간 → (작성 예정)
- Hot Shard 문제 → (작성 예정)

## 📝 문서 작성 가이드

새로운 troubleshooting 문서를 작성할 때는 다음 구조를 따라주세요:

```markdown
# [문제 제목]

## 문제 개요
- 발생 시점
- 영향 범위
- 환경

## 증상 및 에러
- 클라이언트 에러 응답
- 서버 로그
- 메트릭 이상

## 기존 코드 분석
- 문제가 있던 코드
- 문제점 분석

## 근본 원인
- 기술적 배경
- 왜 발생했는지

## 해결 방법
- 수정된 코드
- 변경 사항 요약

## 추가 고려사항
- 관련 이슈
- 모니터링 포인트
- 예방 방법

## 결론
- 핵심 교훈
- 체크리스트
```

## 🚀 긴급 상황 대응

### 1. API 장애 발생 시
1. 로그 확인: `kubectl logs -n tacos-app -l app=gateway-api --tail=100`
2. Pod 상태 확인: `kubectl get pods -n tacos-app -l app=gateway-api`
3. Redis 연결 확인: `kubectl exec -it [POD] -n tacos-app -- redis-cli ping`

### 2. 롤백 절차
```bash
# ArgoCD에서 이전 버전으로 롤백
argocd app rollback gateway-api [REVISION]

# 또는 kubectl로 직접 롤백
kubectl rollout undo deployment/gateway-api -n tacos-app
```

### 3. 긴급 연락처
- **On-call**: (작성 예정)
- **Slack**: #traffic-tacos-alerts
- **PagerDuty**: (작성 예정)

---

**최종 업데이트**: 2025-10-07
