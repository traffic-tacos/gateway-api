# Frontend Auth 빠른 참조 (Cheat Sheet)

## 🔐 API Endpoints

```typescript
// 회원가입 (인증 불필요 ✅)
POST /api/v1/auth/register
Body: { username, password, email, display_name }
Response: { token, user_id, username, display_name, role, expires_in }
⚠️ Authorization 헤더 추가하지 마세요!

// 로그인 (인증 불필요 ✅)
POST /api/v1/auth/login
Body: { username, password }
Response: { token, user_id, username, display_name, role, expires_in }
⚠️ Authorization 헤더 추가하지 마세요!

// 인증된 요청 (인증 필수 🔒)
Header: Authorization: Bearer <token>
Header: Idempotency-Key: <uuid> (POST/PUT/DELETE)
```

## 💾 토큰 저장

```typescript
// 저장
localStorage.setItem('auth_token', token);
localStorage.setItem('token_expires_at', (Date.now() + expires_in * 1000).toString());

// 읽기
const token = localStorage.getItem('auth_token');

// 삭제
localStorage.clear();
```

## 🎯 React Hook 사용법

```typescript
import { useAuth } from '../contexts/AuthContext';

const { user, token, login, logout, isAuthenticated } = useAuth();

// 로그인
await login('user01', 'pwd01');

// 로그아웃
logout();

// 인증 상태 확인
if (isAuthenticated) { /* ... */ }
```

## 🛡️ Protected Route

```typescript
<Route
  path="/reservations"
  element={
    <ProtectedRoute>
      <ReservationPage />
    </ProtectedRoute>
  }
/>
```

## 📡 API 호출

```typescript
import apiClient from './api/client';
import { v4 as uuidv4 } from 'uuid';

// GET (자동으로 토큰 추가됨)
const data = await apiClient.get('/reservations/123');

// POST (Idempotency-Key 자동 추가)
const result = await apiClient.post('/reservations', {
  event_id: 'evt_001',
  seat_ids: ['A-12'],
  quantity: 1
}, {
  headers: { 'Idempotency-Key': uuidv4() }
});
```

## ⚠️ 에러 처리

```typescript
try {
  await login(username, password);
} catch (err) {
  // 401: 로그인 실패
  // 409: 사용자 이미 존재
  // 429: Rate Limit
  setError(handleApiError(err));
}
```

## 🧪 테스트 계정

```
user01 / pwd01
user02 / pwd02
...
user10 / pwd10
```

## 📋 구현 순서

1. ✅ AuthContext 구현
2. ✅ Login/Register 페이지
3. ✅ ProtectedRoute
4. ✅ API Client (Axios Interceptor)
5. ✅ 토큰 만료 체크
6. ✅ 에러 처리

---

**전체 가이드**: [FRONTEND_AUTH_GUIDE.md](./FRONTEND_AUTH_GUIDE.md)

