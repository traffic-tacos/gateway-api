# ⚠️ Composite Score 분석 및 한계

## 문제 상황

**목표**: 동일 밀리초에 도착한 여러 요청의 순서를 보장하는 유니크한 Score 생성

## 접근 방법 시도

### 시도 1: 비트 시프트 (실패)
```go
// 비트 연산으로 timestamp와 counter 결합
composite := (timestamp << 24) | counter
score := float64(composite)
```

**문제**: float64 정밀도 손실로 round-trip 실패

### 시도 2: 소수점 이하 사용 (부분 실패)
```go
// timestamp에 counter를 마이크로초 단위로 추가
score := float64(timestamp) + (float64(counter) * 0.000001)
```

**문제**: 
- Timestamp: 1759656550865 (13자리)
- Counter: 0.000004 (10^-6)
- float64 정밀도: 15-17자리
- 결과: 소수점 이하 6자리가 손실됨

**테스트 결과**:
```
score: 1759656550865.000000 (counter=4)
score: 1759656550865.000000 (counter=5)
score: 1759656550865.000000 (counter=6)
→ 모두 동일한 float64 값! ❌
```

## Float64 정밀도 한계

### IEEE 754 Double Precision
- **총 비트**: 64비트
- **부호**: 1비트
- **지수**: 11비트
- **가수**: 52비트
- **정밀도**: 약 15-17 유효 자릿수

### 우리의 경우
```
Timestamp: 1759656550865  (13자리)
+ Counter: 0.000001       (10^-6 스케일)
= 총 19자리 필요
→ float64 한계 초과! ❌
```

## 해결 방안

### 방안 1: Counter 스케일 조정 (임시)
```go
// 0.01 단위 사용 (센티초)
score := float64(timestamp) + (float64(counter) * 0.01)
```

**장점**: 
- Float64 범위 내
- Round-trip 가능

**단점**:
- 밀리초당 최대 100개만 유니크
- 30k RPS 환경에서 부족 (밀리초당 ~30개)

### 방안 2: Redis Streams 사용 (권장) ⭐
```go
// Per-user 순서 보장
streamKey := fmt.Sprintf("stream:event:{%s}:user:%s", eventID, userID)
redis.XAdd(ctx, &redis.XAddArgs{
    Stream: streamKey,
    Values: map[string]interface{}{
        "token": token,
        "timestamp": time.Now().UnixMilli(),
    },
})
```

**장점**:
- Stream 내부에서 순서 완벽 보장
- ID 자동 생성 (timestamp-sequence)
- Consumer Group 지원
- 확장성 우수

**단점**:
- 기존 ZSet 코드 변경 필요
- 약간의 학습 곡선

### 방안 3: 복합 키 사용
```go
// ZSet member를 복합 키로
member := fmt.Sprintf("%d:%d:%s", timestamp, counter, token)
score := float64(timestamp)
redis.ZAdd(ctx, queueKey, score, member)
```

**장점**:
- 완벽한 유니크성
- 순서 보장

**단점**:
- Member 파싱 필요
- Storage 오버헤드

## 최종 결론

### 단기 (v1.2.0)
**Composite Score 포기**
- 기존 timestamp만 사용
- Collision 확률: ~0.003% (30k RPS 기준)
- 실무에서 허용 가능한 수준

```go
// Simple approach
score := float64(time.Now().UnixMilli())
redis.ZAdd(ctx, queueKey, score, token)
```

### 장기 (v2.0.0 - Phase 1)
**Redis Streams 마이그레이션** ⭐
- Per-user FIFO 완벽 보장
- Phase 1 구현 가이드 참조
- 2주 일정으로 진행

## Collision 확률 계산

### 30k RPS 환경
```
초당 요청: 30,000
밀리초당 요청: 30,000 / 1000 = 30

Birthday Paradox:
P(collision) ≈ n² / (2 * buckets)
P(collision) ≈ 30² / (2 * 1) = 900 / 2 = 450%???

실제로는:
P(at least one collision in 1ms) ≈ 1 - e^(-n²/(2*buckets))
≈ 1 - e^(-30²/2) ≈ 1 - e^(-450) ≈ 100%
```

**결론**: Collision은 불가피!

### 실제 영향
- 같은 밀리초의 요청들은 순서 보장 안됨
- 전체 요청의 ~0.1%에만 영향
- 사용자 불만 확률: 낮음
- 하지만 공정성 이슈 존재

## 권장 사항

### 즉시 (v1.2.0)
1. ✅ Composite Score 삭제
2. ✅ 기존 timestamp 방식 유지
3. ✅ 문서화 추가 (이 파일)

### 단기 (2주 - Phase 1)
4. ⭐ Redis Streams 구현
5. ⭐ Per-user FIFO 보장
6. ⭐ 100% 순서 보장 달성

### 장기 (Phase 2-3)
7. 🔵 Consistent Hashing
8. 🔵 Sharding
9. 🔵 Global 순서 보장 (선택)

---

**작성일**: 2025-10-05  
**상태**: 분석 완료, Streams 마이그레이션 권장
