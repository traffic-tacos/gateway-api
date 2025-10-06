# Gateway API Auth 기능 배포 이슈

## 🔴 문제

Login API 호출 시 `INVALID_CREDENTIALS` 에러 발생:
- Endpoint: POST /api/v1/auth/login
- Body: {"username": "user01", "password": "pwd01"}
- Response: 401 Unauthorized

## ✅ 확인된 사항

1. **DynamoDB 데이터**: user01이 정상적으로 존재
   ```bash
   username: user01
   user_id: 02d246ad-b91d-4f66-8a04-6eba95304d8c
   password_hash: $2b$10$6Apj4CS4naDK3pMPqa/Sxe2nlfESKkT4cazwHx9jvZzrd74Twi2nW (검증 완료 ✅)
   ```

2. **코드**: Auth Handler가 정상적으로 구현됨
   - routes.go: AuthHandler 등록 완료
   - auth.go: Login 로직 정상
   - bcrypt 해시 검증 정상

3. **배포**: 최신 코드가 푸시됨 (커밋 18809ca, 3e17c9f)

## 🔥 근본 원인

**Kubernetes Deployment에 DynamoDB 환경 변수가 누락됨!**

Gateway API는 다음 환경 변수가 필요합니다:
- `DYNAMODB_USERS_TABLE_NAME`
- `DYNAMODB_REGION`

## 📋 해결 방법

### 1. Kubernetes Deployment YAML 수정 필요

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: gateway-api
  namespace: tacos-app
spec:
  template:
    spec:
      serviceAccountName: gateway-api  # IRSA 필요
      containers:
      - name: gateway-api
        image: 137406935518.dkr.ecr.ap-northeast-2.amazonaws.com/gateway-api:latest
        env:
        # 기존 환경 변수들...
        
        # DynamoDB 설정 추가 ✅
        - name: DYNAMODB_USERS_TABLE_NAME
          value: "ticket-users"
        - name: DYNAMODB_REGION
          value: "ap-northeast-2"
        
        # JWT Secret (K8s Secret 사용) ✅
        - name: JWT_SECRET
          valueFrom:
            secretKeyRef:
              name: gateway-api-secrets
              key: jwt-secret
```

### 2. IAM Role (IRSA) 권한 확인

ServiceAccount `gateway-api`에 다음 IAM 정책이 연결되어 있는지 확인:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "dynamodb:GetItem",
        "dynamodb:PutItem",
        "dynamodb:Query",
        "dynamodb:UpdateItem",
        "dynamodb:DeleteItem"
      ],
      "Resource": [
        "arn:aws:dynamodb:ap-northeast-2:137406935518:table/ticket-users",
        "arn:aws:dynamodb:ap-northeast-2:137406935518:table/ticket-users/index/username-index"
      ]
    }
  ]
}
```

**IAM Role ARN**: 
```
arn:aws:iam::137406935518:role/ticket-gateway-api-service-role
```

### 3. JWT Secret 생성 (K8s Secret)

```bash
kubectl create secret generic gateway-api-secrets \
  --from-literal=jwt-secret=$(openssl rand -base64 32) \
  -n tacos-app
```

## 🧪 검증 방법

배포 후 다음 명령으로 확인:

```bash
# 1. Pod 환경 변수 확인
kubectl exec -n tacos-app -it gateway-api-xxx -- env | grep DYNAMODB

# 2. Login API 테스트
curl -X POST https://api.traffictacos.store/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"user01","password":"pwd01"}'

# 예상 결과: 200 OK, JWT 토큰 반환
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "user_id": "02d246ad-b91d-4f66-8a04-6eba95304d8c",
  "username": "user01",
  "display_name": "Test User 01",
  "role": "user",
  "expires_in": 86400
}
```

## 📦 체크리스트

- [ ] Kubernetes Deployment YAML에 DynamoDB 환경 변수 추가
- [ ] JWT Secret 생성 (K8s Secret)
- [ ] IAM Role 권한 확인 (ticket-gateway-api-service-role)
- [ ] ServiceAccount Annotation 확인
- [ ] 배포 후 Login API 테스트
- [ ] Frontend 팀에 완료 통보

## 🎯 우선순위

**HIGH - 긴급**

Login/Register 기능은 전체 시스템의 기본이므로 빠른 수정이 필요합니다.

---

**작성자**: Gateway API Team  
**작성일**: 2025-10-06  
**관련 커밋**: 18809ca, 3e17c9f
