# Users Table 인프라 요구사항

## 📋 요청 개요

**요청자**: Gateway API Team  
**요청일**: 2025-01-06  
**우선순위**: High  
**목적**: 사용자 인증 및 로그인 기능 구현을 위한 Users 테이블 생성

## 🎯 요구사항 배경

Gateway API에서 자체 JWT 발급 및 사용자 인증 기능을 제공하기 위해 DynamoDB Users 테이블이 필요합니다.

### 기능 요구사항
- 사용자 회원가입 (`POST /api/v1/auth/register`)
- 사용자 로그인 (`POST /api/v1/auth/login`)
- JWT 토큰 발급 (자체 HMAC-SHA256 서명)
- 사용자 정보 조회 (username 기반)

## 📊 테이블 스키마

### 1. Primary Table: `traffic-tacos-users`

#### Attributes
```hcl
attribute_definitions = [
  {
    name = "user_id"
    type = "S"
  },
  {
    name = "username"
    type = "S"
  }
]
```

#### Key Schema
```hcl
hash_key  = "user_id"    # Partition Key
range_key = null         # No Sort Key
```

#### Item Structure
```json
{
  "user_id": "550e8400-e29b-41d4-a716-446655440000",
  "username": "user01",
  "password_hash": "$2a$10$...",
  "email": "user01@traffictacos.store",
  "display_name": "Test User 01",
  "role": "user",
  "created_at": "2025-01-06T12:00:00Z",
  "updated_at": "2025-01-06T12:00:00Z"
}
```

### 2. Global Secondary Index: `username-index`

#### GSI Configuration
```hcl
global_secondary_indexes = [
  {
    name               = "username-index"
    hash_key           = "username"
    range_key          = null
    projection_type    = "ALL"
    read_capacity      = 0  # On-demand
    write_capacity     = 0  # On-demand
  }
]
```

#### 사용 목적
- 로그인 시 username으로 사용자 조회
- Query: `username = :username`

## 💰 Billing Mode

```hcl
billing_mode = "PAY_PER_REQUEST"  # On-demand pricing
```

**선택 이유**:
- 초기 트래픽 예측 불가
- 테스트/개발 환경에서 비용 효율적
- 프로덕션 안정화 후 Provisioned 모드로 전환 고려

## 🔒 보안 및 암호화

### 1. Encryption at Rest
```hcl
server_side_encryption {
  enabled     = true
  kms_key_arn = null  # AWS managed key
}
```

### 2. Point-in-Time Recovery (PITR)
```hcl
point_in_time_recovery {
  enabled = true
}
```

### 3. Password Storage
- **암호화 방식**: bcrypt (cost factor: 10)
- **저장 필드**: `password_hash` (JSON에서 제외)

## 🔑 IAM 권한 요구사항

### Gateway API Pod에 필요한 권한

#### IRSA (IAM Roles for Service Accounts)
```hcl
# Service Account: gateway-api
# Namespace: tickets-api

resource "aws_iam_policy" "gateway_api_dynamodb_users" {
  name        = "gateway-api-dynamodb-users-policy"
  description = "Allow Gateway API to access Users table"

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "dynamodb:GetItem",
          "dynamodb:PutItem",
          "dynamodb:Query",
          "dynamodb:Scan",
          "dynamodb:UpdateItem",
          "dynamodb:DeleteItem"
        ]
        Resource = [
          "arn:aws:dynamodb:ap-northeast-2:${var.account_id}:table/traffic-tacos-users",
          "arn:aws:dynamodb:ap-northeast-2:${var.account_id}:table/traffic-tacos-users/index/username-index"
        ]
      }
    ]
  })
}
```

#### 권한 범위
- **GetItem**: 사용자 정보 조회 (user_id 기반)
- **PutItem**: 회원가입 (조건부: user_id 중복 방지)
- **Query**: 로그인 (username-index 사용)
- **UpdateItem**: 사용자 정보 수정 (향후 기능)
- **DeleteItem**: 회원 탈퇴 (향후 기능)

## 📦 Terraform 모듈 구성 예시

### Module Call (main.tf)
```hcl
module "users_table" {
  source = "./modules/dynamodb"

  tables = [
    {
      name           = "traffic-tacos-users"
      hash_key       = "user_id"
      range_key      = null
      billing_mode   = "PAY_PER_REQUEST"
      
      attributes = [
        { name = "user_id", type = "S" },
        { name = "username", type = "S" }
      ]
      
      global_secondary_indexes = [
        {
          name               = "username-index"
          hash_key           = "username"
          range_key          = null
          projection_type    = "ALL"
        }
      ]
      
      point_in_time_recovery_enabled = true
      server_side_encryption_enabled = true
      
      tags = {
        Project     = "traffic-tacos"
        Service     = "gateway-api"
        Environment = var.environment
        ManagedBy   = "terraform"
      }
    }
  ]
}
```

## 🌍 환경 변수 설정

### Gateway API에 필요한 환경 변수

```yaml
# Kubernetes Deployment or ConfigMap
env:
  # DynamoDB Configuration
  - name: DYNAMODB_USERS_TABLE_NAME
    value: "traffic-tacos-users"
  - name: DYNAMODB_REGION
    value: "ap-northeast-2"
  
  # JWT Configuration
  - name: JWT_SECRET
    valueFrom:
      secretKeyRef:
        name: gateway-api-secrets
        key: jwt-secret
  - name: JWT_ISSUER
    value: "traffic-tacos-gateway"
  - name: JWT_AUDIENCE
    value: "traffic-tacos-api"
  
  # AWS Configuration
  - name: AWS_REGION
    value: "ap-northeast-2"
```

### Secret 생성 요청
```bash
# JWT Secret (256-bit random key)
kubectl create secret generic gateway-api-secrets \
  --from-literal=jwt-secret=$(openssl rand -base64 32) \
  -n tickets-api
```

## 📈 예상 사용량 및 비용

### 초기 예상 트래픽 (개발/테스트)
- **회원가입**: ~10 req/day
- **로그인**: ~100 req/day
- **사용자 조회**: ~1,000 req/day

### On-demand Pricing (ap-northeast-2)
- **Write**: $1.4262 per million requests
- **Read**: $0.2852 per million requests
- **Storage**: $0.283 per GB-month

**예상 월 비용**: < $1 USD (초기 테스트 단계)

## 🧪 테스트 데이터

### 더미 사용자 생성 (개발 환경)

테이블 생성 후 다음 스크립트로 테스트 사용자 생성:

```bash
# Gateway API Repository
cd /path/to/gateway-api
export API_URL=http://localhost:8000  # or https://api.traffictacos.store

# 10명의 더미 사용자 생성 (user01~user10 / pwd01~pwd10)
./scripts/create_dummy_users.sh
```

## 📝 체크리스트

### Terraform 작업
- [ ] `modules/dynamodb/` 모듈에 Users 테이블 정의 추가
- [ ] `main.tf`에서 모듈 호출
- [ ] IAM Policy 생성 (gateway-api-dynamodb-users-policy)
- [ ] IRSA 바인딩 (ServiceAccount: gateway-api)
- [ ] `terraform plan` 확인
- [ ] `terraform apply` 실행
- [ ] 테이블 생성 확인 (AWS Console/CLI)

### Kubernetes 작업
- [ ] JWT Secret 생성 (gateway-api-secrets)
- [ ] ConfigMap 업데이트 (DYNAMODB_USERS_TABLE_NAME)
- [ ] Deployment 환경 변수 추가
- [ ] ServiceAccount IRSA annotation 확인
- [ ] Pod 재시작 및 로그 확인

### 검증 작업
- [ ] 테이블 접근 권한 테스트
- [ ] 회원가입 API 테스트 (`POST /api/v1/auth/register`)
- [ ] 로그인 API 테스트 (`POST /api/v1/auth/login`)
- [ ] JWT 토큰 검증
- [ ] 더미 사용자 생성 스크립트 실행

## 🚀 배포 순서

1. **Terraform 배포** (인프라팀)
   ```bash
   cd traffic-tacos-infra-iac
   terraform plan
   terraform apply
   ```

2. **Secret 생성** (DevOps팀)
   ```bash
   kubectl create secret generic gateway-api-secrets \
     --from-literal=jwt-secret=$(openssl rand -base64 32) \
     -n tickets-api
   ```

3. **Gateway API 배포** (Application팀)
   ```bash
   cd gateway-api
   # Update Kubernetes manifests with new env vars
   kubectl apply -f k8s/
   kubectl rollout restart deployment/gateway-api -n tickets-api
   ```

4. **검증**
   ```bash
   # 회원가입 테스트
   curl -X POST https://api.traffictacos.store/api/v1/auth/register \
     -H "Content-Type: application/json" \
     -d '{
       "username": "testuser",
       "password": "testpass123",
       "email": "test@traffictacos.store",
       "display_name": "Test User"
     }'
   
   # 로그인 테스트
   curl -X POST https://api.traffictacos.store/api/v1/auth/login \
     -H "Content-Type: application/json" \
     -d '{
       "username": "testuser",
       "password": "testpass123"
     }'
   ```

## 📚 참고 문서

- [DynamoDB Best Practices](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/best-practices.html)
- [GSI Design Patterns](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/bp-indexes-general.html)
- [IRSA (IAM Roles for Service Accounts)](https://docs.aws.amazon.com/eks/latest/userguide/iam-roles-for-service-accounts.html)
- [JWT Best Practices](https://datatracker.ietf.org/doc/html/rfc8725)

## 🤝 연락처

**질문 및 문의**:
- Gateway API Team Lead: [Your Name]
- Slack Channel: #traffic-tacos-infra
- Email: gateway-api-team@traffictacos.store

---

**문서 버전**: v1.0  
**최종 수정일**: 2025-01-06  
**작성자**: Gateway API Team

