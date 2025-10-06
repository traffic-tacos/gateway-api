# Frontend 인증 시스템 구현 가이드

**대상**: Frontend Team (reservation-web)  
**버전**: v1.0  
**작성일**: 2025-01-06  
**API Base URL**: `https://api.traffictacos.store`

---

## 📋 목차

1. [API 명세](#1-api-명세)
2. [토큰 관리 전략](#2-토큰-관리-전략)
3. [React 구현 예시](#3-react-구현-예시)
4. [Protected Routes](#4-protected-routes)
5. [API 클라이언트 구성](#5-api-클라이언트-구성)
6. [에러 처리](#6-에러-처리)
7. [보안 고려사항](#7-보안-고려사항)

---

## 1. API 명세

### 1.1 회원가입 (Register)

**Endpoint**: `POST /api/v1/auth/register`

**⚠️ 주의**: 인증이 필요 없는 공개 엔드포인트입니다. `Authorization` 헤더 불필요!

**Request**:
```typescript
interface RegisterRequest {
  username: string;      // 3-20자, 영문+숫자
  password: string;      // 최소 6자
  email: string;         // 유효한 이메일
  display_name: string;  // 표시 이름
}
```

**Request Example**:
```bash
curl -X POST https://api.traffictacos.store/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{
    "username": "testuser",
    "password": "testpass123",
    "email": "test@traffictacos.store",
    "display_name": "Test User"
  }'
```

**Response (201 Created)**:
```typescript
interface AuthResponse {
  token: string;         // JWT 토큰 (24시간 유효)
  user_id: string;       // UUID
  username: string;
  display_name: string;
  role: string;          // "user" | "admin"
  expires_in: number;    // 초 단위 (86400 = 24시간)
}
```

**Response Example**:
```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "user_id": "550e8400-e29b-41d4-a716-446655440000",
  "username": "testuser",
  "display_name": "Test User",
  "role": "user",
  "expires_in": 86400
}
```

**Error Responses**:
```typescript
// 400 Bad Request - 잘못된 입력
{
  "error": {
    "code": "INVALID_REQUEST",
    "message": "Invalid request body"
  }
}

// 409 Conflict - 사용자 이미 존재
{
  "error": {
    "code": "USERNAME_EXISTS",
    "message": "Username already exists"
  }
}
```

---

### 1.2 로그인 (Login)

**Endpoint**: `POST /api/v1/auth/login`

**⚠️ 주의**: Login과 Register는 **인증이 필요 없는 공개 엔드포인트**입니다.  
`Authorization` 헤더를 추가하지 마세요!

**Request**:
```typescript
interface LoginRequest {
  username: string;
  password: string;
}
```

**Request Example**:
```bash
curl -X POST https://api.traffictacos.store/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{
    "username": "user01",
    "password": "pwd01"
  }'
```

**Response (200 OK)**:
```typescript
// Same as RegisterResponse
interface AuthResponse {
  token: string;
  user_id: string;
  username: string;
  display_name: string;
  role: string;
  expires_in: number;
}
```

**Error Responses**:
```typescript
// 401 Unauthorized - 잘못된 자격증명
{
  "error": {
    "code": "INVALID_CREDENTIALS",
    "message": "Invalid username or password"
  }
}
```

---

### 1.3 인증된 API 호출

모든 보호된 엔드포인트는 `Authorization` 헤더가 필요합니다.

**Header**:
```http
Authorization: Bearer <JWT_TOKEN>
```

**Example**:
```bash
curl -X POST https://api.traffictacos.store/api/v1/reservations \
  -H "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9..." \
  -H "Content-Type: application/json" \
  -H "Idempotency-Key: $(uuidgen)" \
  -d '{
    "event_id": "evt_2025_1001",
    "seat_ids": ["A-12"],
    "quantity": 1
  }'
```

---

## 2. 토큰 관리 전략

### 2.1 토큰 저장

**권장: LocalStorage** (SPA의 경우)
```typescript
// 로그인 성공 시
localStorage.setItem('auth_token', response.token);
localStorage.setItem('user_id', response.user_id);
localStorage.setItem('username', response.username);
localStorage.setItem('token_expires_at', 
  (Date.now() + response.expires_in * 1000).toString()
);

// 토큰 읽기
const token = localStorage.getItem('auth_token');
```

**대안: SessionStorage** (탭 닫으면 로그아웃)
```typescript
sessionStorage.setItem('auth_token', response.token);
```

**주의사항**:
- ❌ XSS에 취약할 수 있으므로 `httpOnly` 쿠키가 이상적이지만, SPA에서는 LocalStorage가 일반적
- ✅ HTTPS 필수
- ✅ CSP (Content Security Policy) 설정 권장

### 2.2 토큰 만료 처리

```typescript
function isTokenExpired(): boolean {
  const expiresAt = localStorage.getItem('token_expires_at');
  if (!expiresAt) return true;
  
  return Date.now() > parseInt(expiresAt);
}

function clearAuth() {
  localStorage.removeItem('auth_token');
  localStorage.removeItem('user_id');
  localStorage.removeItem('username');
  localStorage.removeItem('token_expires_at');
}
```

### 2.3 자동 로그아웃

```typescript
// 만료 5분 전 경고
const EXPIRY_WARNING_MS = 5 * 60 * 1000; // 5분

function checkTokenExpiry() {
  const expiresAt = localStorage.getItem('token_expires_at');
  if (!expiresAt) return;
  
  const timeLeft = parseInt(expiresAt) - Date.now();
  
  if (timeLeft <= 0) {
    // 만료됨 - 로그아웃
    clearAuth();
    window.location.href = '/login';
  } else if (timeLeft <= EXPIRY_WARNING_MS) {
    // 곧 만료 - 경고 표시
    showExpiryWarning(Math.floor(timeLeft / 1000));
  }
}

// 1분마다 체크
setInterval(checkTokenExpiry, 60 * 1000);
```

---

## 3. React 구현 예시

### 3.1 Auth Context (Context API)

**`src/contexts/AuthContext.tsx`**:
```typescript
import React, { createContext, useContext, useState, useEffect } from 'react';

interface User {
  user_id: string;
  username: string;
  display_name: string;
  role: string;
}

interface AuthContextType {
  user: User | null;
  token: string | null;
  login: (username: string, password: string) => Promise<void>;
  register: (data: RegisterData) => Promise<void>;
  logout: () => void;
  isAuthenticated: boolean;
  isLoading: boolean;
}

interface RegisterData {
  username: string;
  password: string;
  email: string;
  display_name: string;
}

const AuthContext = createContext<AuthContextType | undefined>(undefined);

export const AuthProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const [user, setUser] = useState<User | null>(null);
  const [token, setToken] = useState<string | null>(null);
  const [isLoading, setIsLoading] = useState(true);

  // 초기 로드 시 LocalStorage에서 복원
  useEffect(() => {
    const storedToken = localStorage.getItem('auth_token');
    const storedUser = localStorage.getItem('user_id');
    const expiresAt = localStorage.getItem('token_expires_at');

    if (storedToken && storedUser && expiresAt) {
      // 만료 체크
      if (Date.now() < parseInt(expiresAt)) {
        setToken(storedToken);
        setUser({
          user_id: storedUser,
          username: localStorage.getItem('username') || '',
          display_name: localStorage.getItem('display_name') || '',
          role: localStorage.getItem('role') || 'user',
        });
      } else {
        // 만료된 토큰 제거
        localStorage.clear();
      }
    }
    setIsLoading(false);
  }, []);

  const login = async (username: string, password: string) => {
    setIsLoading(true);
    try {
      const response = await fetch('https://api.traffictacos.store/api/v1/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username, password }),
      });

      if (!response.ok) {
        const error = await response.json();
        throw new Error(error.error?.message || '로그인 실패');
      }

      const data = await response.json();
      
      // 토큰 및 사용자 정보 저장
      localStorage.setItem('auth_token', data.token);
      localStorage.setItem('user_id', data.user_id);
      localStorage.setItem('username', data.username);
      localStorage.setItem('display_name', data.display_name);
      localStorage.setItem('role', data.role);
      localStorage.setItem('token_expires_at', 
        (Date.now() + data.expires_in * 1000).toString()
      );

      setToken(data.token);
      setUser({
        user_id: data.user_id,
        username: data.username,
        display_name: data.display_name,
        role: data.role,
      });
    } finally {
      setIsLoading(false);
    }
  };

  const register = async (data: RegisterData) => {
    setIsLoading(true);
    try {
      const response = await fetch('https://api.traffictacos.store/api/v1/auth/register', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(data),
      });

      if (!response.ok) {
        const error = await response.json();
        throw new Error(error.error?.message || '회원가입 실패');
      }

      const result = await response.json();
      
      // 회원가입 후 자동 로그인
      localStorage.setItem('auth_token', result.token);
      localStorage.setItem('user_id', result.user_id);
      localStorage.setItem('username', result.username);
      localStorage.setItem('display_name', result.display_name);
      localStorage.setItem('role', result.role);
      localStorage.setItem('token_expires_at', 
        (Date.now() + result.expires_in * 1000).toString()
      );

      setToken(result.token);
      setUser({
        user_id: result.user_id,
        username: result.username,
        display_name: result.display_name,
        role: result.role,
      });
    } finally {
      setIsLoading(false);
    }
  };

  const logout = () => {
    localStorage.clear();
    setToken(null);
    setUser(null);
  };

  return (
    <AuthContext.Provider
      value={{
        user,
        token,
        login,
        register,
        logout,
        isAuthenticated: !!token && !!user,
        isLoading,
      }}
    >
      {children}
    </AuthContext.Provider>
  );
};

export const useAuth = () => {
  const context = useContext(AuthContext);
  if (!context) {
    throw new Error('useAuth must be used within AuthProvider');
  }
  return context;
};
```

---

### 3.2 Login 컴포넌트

**`src/pages/Login.tsx`**:
```typescript
import React, { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useAuth } from '../contexts/AuthContext';

export const LoginPage: React.FC = () => {
  const [username, setUsername] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const { login, isLoading } = useAuth();
  const navigate = useNavigate();

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');

    try {
      await login(username, password);
      navigate('/'); // 로그인 성공 후 홈으로 이동
    } catch (err) {
      setError(err instanceof Error ? err.message : '로그인 실패');
    }
  };

  return (
    <div className="login-container">
      <h1>로그인</h1>
      <form onSubmit={handleSubmit}>
        <div>
          <label>사용자명</label>
          <input
            type="text"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            required
            disabled={isLoading}
          />
        </div>
        <div>
          <label>비밀번호</label>
          <input
            type="password"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            required
            disabled={isLoading}
          />
        </div>
        {error && <div className="error">{error}</div>}
        <button type="submit" disabled={isLoading}>
          {isLoading ? '로그인 중...' : '로그인'}
        </button>
      </form>
      <div>
        <a href="/register">회원가입</a>
      </div>
    </div>
  );
};
```

---

### 3.3 Register 컴포넌트

**`src/pages/Register.tsx`**:
```typescript
import React, { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useAuth } from '../contexts/AuthContext';

export const RegisterPage: React.FC = () => {
  const [formData, setFormData] = useState({
    username: '',
    password: '',
    email: '',
    display_name: '',
  });
  const [error, setError] = useState('');
  const { register, isLoading } = useAuth();
  const navigate = useNavigate();

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');

    try {
      await register(formData);
      navigate('/'); // 회원가입 성공 후 홈으로 이동
    } catch (err) {
      setError(err instanceof Error ? err.message : '회원가입 실패');
    }
  };

  return (
    <div className="register-container">
      <h1>회원가입</h1>
      <form onSubmit={handleSubmit}>
        <div>
          <label>사용자명 (3-20자)</label>
          <input
            type="text"
            value={formData.username}
            onChange={(e) => setFormData({ ...formData, username: e.target.value })}
            required
            minLength={3}
            maxLength={20}
            disabled={isLoading}
          />
        </div>
        <div>
          <label>비밀번호 (최소 6자)</label>
          <input
            type="password"
            value={formData.password}
            onChange={(e) => setFormData({ ...formData, password: e.target.value })}
            required
            minLength={6}
            disabled={isLoading}
          />
        </div>
        <div>
          <label>이메일</label>
          <input
            type="email"
            value={formData.email}
            onChange={(e) => setFormData({ ...formData, email: e.target.value })}
            required
            disabled={isLoading}
          />
        </div>
        <div>
          <label>이름</label>
          <input
            type="text"
            value={formData.display_name}
            onChange={(e) => setFormData({ ...formData, display_name: e.target.value })}
            required
            disabled={isLoading}
          />
        </div>
        {error && <div className="error">{error}</div>}
        <button type="submit" disabled={isLoading}>
          {isLoading ? '회원가입 중...' : '회원가입'}
        </button>
      </form>
      <div>
        <a href="/login">이미 계정이 있으신가요?</a>
      </div>
    </div>
  );
};
```

---

## 4. Protected Routes

### 4.1 ProtectedRoute 컴포넌트

**`src/components/ProtectedRoute.tsx`**:
```typescript
import React from 'react';
import { Navigate } from 'react-router-dom';
import { useAuth } from '../contexts/AuthContext';

interface ProtectedRouteProps {
  children: React.ReactNode;
}

export const ProtectedRoute: React.FC<ProtectedRouteProps> = ({ children }) => {
  const { isAuthenticated, isLoading } = useAuth();

  if (isLoading) {
    return <div>로딩 중...</div>;
  }

  if (!isAuthenticated) {
    return <Navigate to="/login" replace />;
  }

  return <>{children}</>;
};
```

### 4.2 Router 설정

**`src/App.tsx`**:
```typescript
import { BrowserRouter, Routes, Route } from 'react-router-dom';
import { AuthProvider } from './contexts/AuthContext';
import { ProtectedRoute } from './components/ProtectedRoute';
import { LoginPage } from './pages/Login';
import { RegisterPage } from './pages/Register';
import { HomePage } from './pages/Home';
import { ReservationPage } from './pages/Reservation';

function App() {
  return (
    <BrowserRouter>
      <AuthProvider>
        <Routes>
          {/* 공개 라우트 */}
          <Route path="/login" element={<LoginPage />} />
          <Route path="/register" element={<RegisterPage />} />
          
          {/* 보호된 라우트 */}
          <Route
            path="/"
            element={
              <ProtectedRoute>
                <HomePage />
              </ProtectedRoute>
            }
          />
          <Route
            path="/reservations"
            element={
              <ProtectedRoute>
                <ReservationPage />
              </ProtectedRoute>
            }
          />
        </Routes>
      </AuthProvider>
    </BrowserRouter>
  );
}

export default App;
```

---

## 5. API 클라이언트 구성

### 5.1 Axios Interceptor

**`src/api/client.ts`**:
```typescript
import axios from 'axios';

const apiClient = axios.create({
  baseURL: 'https://api.traffictacos.store/api/v1',
  timeout: 10000,
  headers: {
    'Content-Type': 'application/json',
  },
});

// Request Interceptor: 인증이 필요한 요청에만 토큰 추가
apiClient.interceptors.request.use(
  (config) => {
    // Login/Register는 인증이 필요 없으므로 토큰 제외
    const publicPaths = ['/auth/login', '/auth/register'];
    const isPublicPath = publicPaths.some(path => config.url?.includes(path));
    
    if (!isPublicPath) {
      const token = localStorage.getItem('auth_token');
      if (token) {
        config.headers.Authorization = `Bearer ${token}`;
      }
    }
    
    return config;
  },
  (error) => {
    return Promise.reject(error);
  }
);

// Response Interceptor: 401 에러 시 자동 로그아웃
apiClient.interceptors.response.use(
  (response) => response,
  (error) => {
    if (error.response?.status === 401) {
      // 토큰 만료 또는 무효
      localStorage.clear();
      window.location.href = '/login';
    }
    return Promise.reject(error);
  }
);

export default apiClient;
```

### 5.2 API 호출 예시

**`src/api/reservations.ts`**:
```typescript
import apiClient from './client';
import { v4 as uuidv4 } from 'uuid';

export interface CreateReservationRequest {
  event_id: string;
  seat_ids: string[];
  quantity: number;
}

export const createReservation = async (data: CreateReservationRequest) => {
  const response = await apiClient.post('/reservations', data, {
    headers: {
      'Idempotency-Key': uuidv4(), // 멱등성 키 자동 생성
    },
  });
  return response.data;
};

export const getReservation = async (reservationId: string) => {
  const response = await apiClient.get(`/reservations/${reservationId}`);
  return response.data;
};

export const confirmReservation = async (reservationId: string) => {
  const response = await apiClient.post(
    `/reservations/${reservationId}/confirm`,
    {},
    {
      headers: {
        'Idempotency-Key': uuidv4(),
      },
    }
  );
  return response.data;
};
```

---

## 6. 에러 처리

### 6.1 에러 타입 정의

**`src/types/errors.ts`**:
```typescript
export interface ApiError {
  error: {
    code: string;
    message: string;
    trace_id?: string;
  };
}

export const handleApiError = (error: any): string => {
  if (error.response?.data?.error) {
    const apiError = error.response.data.error;
    
    // 에러 코드별 메시지 매핑
    const errorMessages: Record<string, string> = {
      'INVALID_CREDENTIALS': '사용자명 또는 비밀번호가 올바르지 않습니다.',
      'USERNAME_EXISTS': '이미 사용 중인 사용자명입니다.',
      'TOKEN_EXPIRED': '로그인이 만료되었습니다. 다시 로그인해주세요.',
      'INVALID_TOKEN': '유효하지 않은 인증 정보입니다.',
      'RATE_LIMITED': '너무 많은 요청을 보내셨습니다. 잠시 후 다시 시도해주세요.',
    };
    
    return errorMessages[apiError.code] || apiError.message || '알 수 없는 오류가 발생했습니다.';
  }
  
  if (error.request) {
    return '서버에 연결할 수 없습니다. 네트워크를 확인해주세요.';
  }
  
  return error.message || '알 수 없는 오류가 발생했습니다.';
};
```

### 6.2 에러 처리 적용

```typescript
import { handleApiError } from '../types/errors';

try {
  await login(username, password);
} catch (err) {
  setError(handleApiError(err));
}
```

---

## 7. 보안 고려사항

### 7.1 XSS 방어

```typescript
// 사용자 입력 sanitize (DOMPurify 사용)
import DOMPurify from 'dompurify';

const sanitizedUsername = DOMPurify.sanitize(username);
```

### 7.2 CSRF 방어

- ✅ JWT 토큰 사용으로 CSRF 위험 감소
- ✅ `SameSite=Strict` 쿠키 사용 (쿠키 방식인 경우)

### 7.3 HTTPS 강제

```typescript
// Redirect HTTP to HTTPS
if (window.location.protocol !== 'https:' && window.location.hostname !== 'localhost') {
  window.location.href = 'https:' + window.location.href.substring(window.location.protocol.length);
}
```

### 7.4 민감 정보 로깅 금지

```typescript
// ❌ BAD: 토큰 로깅
console.log('Token:', token);

// ✅ GOOD: 토큰 존재 여부만 로깅
console.log('Token exists:', !!token);
```

---

## 8. 테스트 계정

개발/테스트용 계정 (DynamoDB에 미리 생성됨):

```
user01 / pwd01
user02 / pwd02
user03 / pwd03
...
user10 / pwd10
```

**테스트 방법**:
```typescript
// Login 컴포넌트에서
await login('user01', 'pwd01');
```

---

## 9. 체크리스트

### Frontend 구현 체크리스트

- [ ] AuthContext 구현
- [ ] Login 페이지 구현
- [ ] Register 페이지 구현
- [ ] ProtectedRoute 컴포넌트 구현
- [ ] API 클라이언트 (Axios Interceptor) 구성
- [ ] 토큰 만료 체크 로직 구현
- [ ] 자동 로그아웃 구현
- [ ] 에러 처리 구현
- [ ] 로딩 상태 UI 구현
- [ ] 로그아웃 버튼 추가
- [ ] 사용자 정보 표시 (헤더/네비게이션)
- [ ] 테스트 (user01~user10 계정으로 로그인)

---

## 10. FAQ

**Q: 토큰을 LocalStorage에 저장해도 안전한가요?**  
A: XSS 공격에 취약할 수 있지만, SPA에서는 일반적인 방법입니다. 다음 조치를 취하세요:
- HTTPS 사용 필수
- CSP (Content Security Policy) 설정
- 입력 sanitization
- 신뢰할 수 없는 외부 스크립트 사용 금지

**Q: 토큰 갱신(Refresh Token)은 언제 구현하나요?**  
A: 현재는 24시간 유효한 단일 토큰을 사용합니다. 향후 Refresh Token을 추가할 예정입니다.

**Q: 소셜 로그인은 지원하나요?**  
A: 현재는 ID/PW 방식만 지원합니다. Google, Kakao, Naver 로그인은 향후 추가 예정입니다.

**Q: API 요청 시 Idempotency-Key는 언제 필요한가요?**  
A: POST, PUT, DELETE 요청 시 필수입니다. 예약 생성 등 중요한 작업에 사용됩니다.

---

## 11. 연락처

**질문 및 문의**:
- Backend Team: #traffic-tacos-backend
- Slack Channel: #traffic-tacos-frontend
- Email: gateway-api-team@traffictacos.store

---

**문서 버전**: v1.0  
**최종 수정일**: 2025-01-06  
**작성자**: Gateway API Team

