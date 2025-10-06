# Gateway API 인증 시스템 인프라 요구사항

## 📋 요약

Gateway API의 사용자 인증 시스템(Login/Register)을 위한 DynamoDB 접근 권한 및 환경 변수 설정을 Terraform으로 관리하기 위한 요구사항 문서입니다.

**현재 상태**: 수동으로 IAM Policy 생성 및 연결, Kubernetes 환경 변수 수동 추가
**목표**: 모든 인프라를 Terraform으로 관리하여 IaC 원칙 준수

---

## 🚨 긴급도

**HIGH** - 현재 수동으로 설정된 인프라가 프로덕션에서 동작 중이며, Terraform 상태 파일과 동기화되지 않아 향후 문제 발생 가능

---

## 🎯 요구사항

### 1. IAM Policy: DynamoDB Users 테이블 접근 권한

**기존 Policy**:
- `traffic-tacos-redis-secret-reader-policy` (유지)

**신규 Policy** (수동 생성됨, Terraform으로 이관 필요):
- `traffic-tacos-gateway-api-dynamodb-policy`

#### Terraform 코드 제안

```hcl
# modules/iam/gateway_api_policies.tf

# DynamoDB Users 테이블 접근 Policy
resource "aws_iam_policy" "gateway_api_dynamodb" {
  name        = "traffic-tacos-gateway-api-dynamodb-policy"
  description = "DynamoDB access for Gateway API Users table"
  
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "dynamodb:GetItem",
          "dynamodb:PutItem",
          "dynamodb:Query",
          "dynamodb:UpdateItem",
          "dynamodb:DeleteItem"
        ]
        Resource = [
          "arn:aws:dynamodb:${var.aws_region}:${var.aws_account_id}:table/ticket-users",
          "arn:aws:dynamodb:${var.aws_region}:${var.aws_account_id}:table/ticket-users/index/username-index"
        ]
      }
    ]
  })
  
  tags = {
    Project     = "traffic-tacos"
    ManagedBy   = "terraform"
    Component   = "gateway-api"
    Purpose     = "auth"
  }
}

# Gateway API Service Role에 Policy 연결
resource "aws_iam_role_policy_attachment" "gateway_api_dynamodb" {
  role       = aws_iam_role.gateway_api_service_role.name
  policy_arn = aws_iam_policy.gateway_api_dynamodb.arn
}

# 기존 Redis Secret Policy 연결 (이미 존재하는지 확인 후 추가)
resource "aws_iam_role_policy_attachment" "gateway_api_redis_secrets" {
  role       = aws_iam_role.gateway_api_service_role.name
  policy_arn = "arn:aws:iam::${var.aws_account_id}:policy/traffic-tacos-redis-secret-reader-policy"
}
```

---

### 2. Kubernetes Secret: JWT Secret

**필요성**: 자체 발급 JWT 토큰 서명용 Secret Key

**⚠️ 보안 고려사항**: 
- Secret 값은 **절대 Public Repo에 포함하지 않음**
- Secret은 **수동 생성** 또는 **별도 시스템**으로 관리 (Sealed Secrets, External Secrets Operator, Vault 등)
- Terraform은 **Secret 참조만 설정** (값은 포함 안 함)

**권장 방식 1: Kubernetes Secret 수동 생성** (현재 방식 유지):

```bash
# 1. Secret 값 생성
JWT_SECRET=$(openssl rand -base64 32)

# 2. Kubernetes Secret 생성 (수동)
kubectl create secret generic gateway-api-secrets \
  --from-literal=jwt-secret=$JWT_SECRET \
  -n tacos-app

# 3. Terraform은 Secret 참조만 관리 (값은 포함 안 함)
# modules/k8s/gateway_api_deployment.tf에서 Secret 이름만 참조
```

**권장 방식 2: Sealed Secrets** (GitOps 친화적):

```bash
# 1. Sealed Secrets Controller 설치
kubectl apply -f https://github.com/bitnami-labs/sealed-secrets/releases/download/v0.24.0/controller.yaml

# 2. Secret 암호화
echo -n "$JWT_SECRET" | kubectl create secret generic gateway-api-secrets \
  --dry-run=client \
  --from-file=jwt-secret=/dev/stdin \
  -o yaml | \
  kubeseal -o yaml > sealed-secret.yaml

# 3. Sealed Secret은 Public Repo에 커밋 가능
# 4. Sealed Secrets Controller가 자동으로 복호화하여 실제 Secret 생성
```

**권장 방식 3: External Secrets Operator + AWS Secrets Manager**:

```hcl
# modules/secrets/gateway_api.tf

# 1. Secrets Manager에 Secret Container만 생성 (값은 수동으로 추가)
resource "aws_secretsmanager_secret" "gateway_api_jwt" {
  name                    = "traffic-tacos/gateway-api/jwt-secret"
  description             = "JWT secret for Gateway API self-issued tokens"
  recovery_window_in_days = 7
  
  tags = {
    Project   = "traffic-tacos"
    ManagedBy = "terraform"
    Component = "gateway-api"
  }
}

# ❌ Secret 값은 Terraform으로 생성하지 않음 (Public Repo 노출 위험)
# ✅ 대신 수동으로 추가:
# aws secretsmanager put-secret-value \
#   --secret-id traffic-tacos/gateway-api/jwt-secret \
#   --secret-string '{"jwt-secret":"$(openssl rand -base64 32)"}'

# 2. Gateway API IAM Role에 Secret 읽기 권한 추가
resource "aws_iam_policy" "gateway_api_jwt_secret_reader" {
  name        = "traffic-tacos-gateway-api-jwt-secret-reader-policy"
  description = "Allow Gateway API to read JWT secret from Secrets Manager"
  
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "secretsmanager:GetSecretValue",
          "secretsmanager:DescribeSecret"
        ]
        Resource = aws_secretsmanager_secret.gateway_api_jwt.arn
      }
    ]
  })
}

resource "aws_iam_role_policy_attachment" "gateway_api_jwt_secret" {
  role       = aws_iam_role.gateway_api_service_role.name
  policy_arn = aws_iam_policy.gateway_api_jwt_secret_reader.arn
}

# 3. External Secrets Operator로 K8s Secret 자동 동기화
# (별도 설치 필요: https://external-secrets.io/)
```

**Terraform 코드 예시** (Secret 참조만 관리):

```hcl
# modules/k8s/gateway_api_deployment.tf

# Secret 자체는 생성하지 않고, 참조만 설정
resource "kubernetes_deployment" "gateway_api" {
  # ...
  
  spec {
    template {
      spec {
        container {
          name = "gateway-api"
          
          env {
            name = "JWT_SECRET"
            value_from {
              secret_key_ref {
                name = "gateway-api-secrets"  # ✅ Secret 이름만 참조
                key  = "jwt-secret"
              }
            }
          }
        }
      }
    }
  }
}
```

**요약**:
- ❌ Secret 값을 Terraform 코드에 포함 (Public Repo 노출 위험)
- ✅ Secret은 수동 생성 또는 Sealed Secrets/External Secrets Operator 사용
- ✅ Terraform은 IAM Policy + Secret 참조만 관리

---

### 3. Kubernetes Deployment: 환경 변수 추가

**필요한 환경 변수**:

| 변수명 | 값 | 설명 |
|---|---|---|
| `DYNAMODB_USERS_TABLE_NAME` | `ticket-users` | DynamoDB Users 테이블 이름 |
| `DYNAMODB_REGION` | `ap-northeast-2` | DynamoDB 리전 |
| `JWT_SECRET` | `Secret 참조` | JWT 서명용 Secret Key |
| `AWS_WEB_IDENTITY_TOKEN_FILE` | `/var/run/secrets/kubernetes.io/serviceaccount/token` | ServiceAccount Token 경로 (수정 필요) |

#### ArgoCD Application YAML 또는 Helm Values 수정

**Option 1: ArgoCD Application에 직접 추가**

```yaml
# k8s/overlays/production/deployment-patch.yaml

apiVersion: apps/v1
kind: Deployment
metadata:
  name: gateway-api
spec:
  template:
    spec:
      containers:
      - name: gateway-api
        env:
        # 기존 환경 변수들...
        
        # ========================================
        # Auth System 관련 환경 변수 추가
        # ========================================
        - name: DYNAMODB_USERS_TABLE_NAME
          value: "ticket-users"
        
        - name: DYNAMODB_REGION
          value: "ap-northeast-2"
        
        - name: JWT_SECRET
          valueFrom:
            secretKeyRef:
              name: gateway-api-secrets
              key: jwt-secret
        
        # ServiceAccount Token 경로 수정
        - name: AWS_WEB_IDENTITY_TOKEN_FILE
          value: "/var/run/secrets/kubernetes.io/serviceaccount/token"
```

**Option 2: Helm Values (추천)**

```yaml
# helm/gateway-api/values.yaml

env:
  # 기존 환경 변수들...
  
  # Auth System
  DYNAMODB_USERS_TABLE_NAME: "ticket-users"
  DYNAMODB_REGION: "ap-northeast-2"
  JWT_SECRET:
    valueFrom:
      secretKeyRef:
        name: gateway-api-secrets
        key: jwt-secret
  
  # ServiceAccount Token 경로 수정
  AWS_WEB_IDENTITY_TOKEN_FILE: "/var/run/secrets/kubernetes.io/serviceaccount/token"
```

---

## 🔄 마이그레이션 계획

### Phase 1: Terraform Import (기존 리소스 가져오기)

```bash
# 1. 수동 생성한 IAM Policy Import
terraform import aws_iam_policy.gateway_api_dynamodb \
  arn:aws:iam::137406935518:policy/traffic-tacos-gateway-api-dynamodb-policy

# 2. Policy Attachment Import
terraform import aws_iam_role_policy_attachment.gateway_api_dynamodb \
  traffic-tacos-gateway-api-sa-role/arn:aws:iam::137406935518:policy/traffic-tacos-gateway-api-dynamodb-policy

# 3. Kubernetes Secret Import
terraform import kubernetes_secret.gateway_api_secrets \
  tacos-app/gateway-api-secrets
```

### Phase 2: Terraform State 검증

```bash
# State 확인
terraform state list | grep gateway_api

# Drift 확인
terraform plan
```

### Phase 3: 코드 동기화

- Terraform 코드를 실제 리소스와 100% 일치시킴
- `terraform plan` 결과가 "No changes" 확인

### Phase 4: 문서화

- Terraform 코드에 주석 추가
- README 업데이트
- Runbook 작성

---

## ✅ 검증 체크리스트

### IAM 권한 검증

```bash
# 1. Gateway API Pod에서 DynamoDB 접근 테스트
kubectl exec -n tacos-app deployment/gateway-api -- \
  aws dynamodb describe-table --table-name ticket-users --region ap-northeast-2

# 예상 결과: 테이블 정보 정상 출력

# 2. Gateway API Pod에서 Secrets Manager 접근 테스트 (Secret Manager 사용 시)
kubectl exec -n tacos-app deployment/gateway-api -- \
  aws secretsmanager get-secret-value \
  --secret-id traffic-tacos/gateway-api/jwt-secret \
  --region ap-northeast-2

# 예상 결과: Secret 값 정상 출력
```

### 환경 변수 검증

```bash
# 1. 환경 변수 확인
kubectl get deployment gateway-api -n tacos-app -o jsonpath='{.spec.template.spec.containers[0].env}' | jq '.[] | select(.name | test("DYNAMODB|JWT"))'

# 예상 결과:
# DYNAMODB_USERS_TABLE_NAME: ticket-users
# DYNAMODB_REGION: ap-northeast-2
# JWT_SECRET: (Secret 참조)
# AWS_WEB_IDENTITY_TOKEN_FILE: /var/run/secrets/kubernetes.io/serviceaccount/token

# 2. Pod 재시작 후 환경 변수 확인
kubectl rollout restart deployment/gateway-api -n tacos-app
kubectl rollout status deployment/gateway-api -n tacos-app
```

### Login API 검증

```bash
# 1. Login 테스트
curl -X POST https://api.traffictacos.store/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"user01","password":"pwd01"}'

# 예상 결과:
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "user_id": "02d246ad-b91d-4f66-8a04-6eba95304d8c",
  "username": "user01",
  "display_name": "Test User 01",
  "role": "user",
  "expires_in": 86400
}

# 2. Register 테스트
curl -X POST https://api.traffictacos.store/api/v1/auth/register \
  -H "Content-Type: application/json" \
  -d '{
    "username": "testuser",
    "password": "testpass123",
    "email": "test@example.com",
    "display_name": "Test User"
  }'

# 예상 결과: 201 Created + JWT Token
```

---

## 📊 현재 수동 설정 현황

### 이미 수동으로 생성된 리소스

| 리소스 타입 | 이름 | ARN/ID | 생성 방법 |
|---|---|---|---|
| IAM Policy | `traffic-tacos-gateway-api-dynamodb-policy` | `arn:aws:iam::137406935518:policy/traffic-tacos-gateway-api-dynamodb-policy` | AWS CLI |
| IAM Role Attachment | `gateway-api-dynamodb` | `traffic-tacos-gateway-api-sa-role` + Policy | AWS CLI |
| K8s Secret | `gateway-api-secrets` | `tacos-app/gateway-api-secrets` | kubectl |
| K8s Deployment Env | `DYNAMODB_*`, `JWT_SECRET` | `gateway-api` Deployment | kubectl patch |

### Terraform으로 이관 필요한 리소스

- ✅ IAM Policy: `traffic-tacos-gateway-api-dynamodb-policy`
- ✅ IAM Role Attachment: DynamoDB Policy → Gateway API Role
- ✅ K8s Secret: `gateway-api-secrets` (또는 Secrets Manager로 마이그레이션)
- ✅ K8s Deployment 환경 변수

---

## 🔧 Terraform 변수 정의

```hcl
# variables.tf

variable "aws_account_id" {
  description = "AWS Account ID"
  type        = string
  default     = "137406935518"
}

variable "aws_region" {
  description = "AWS Region"
  type        = string
  default     = "ap-northeast-2"
}

variable "gateway_api_service_role_name" {
  description = "Gateway API IRSA Service Role Name"
  type        = string
  default     = "traffic-tacos-gateway-api-sa-role"
}

variable "dynamodb_users_table_name" {
  description = "DynamoDB Users table name"
  type        = string
  default     = "ticket-users"
}

variable "k8s_namespace" {
  description = "Kubernetes namespace for Gateway API"
  type        = string
  default     = "tacos-app"
}
```

---

## 📦 Terraform 모듈 구조 제안

```
modules/
├── iam/
│   ├── gateway_api_role.tf          # Gateway API IRSA Role
│   ├── gateway_api_policies.tf      # DynamoDB, JWT Secret Policies
│   └── outputs.tf
├── secrets/
│   ├── gateway_api_jwt.tf           # JWT Secret (Secrets Manager)
│   └── outputs.tf
└── k8s/
    ├── gateway_api_deployment.tf    # Deployment 환경 변수
    ├── gateway_api_secrets.tf       # K8s Secrets (Option)
    └── outputs.tf
```

---

## 🚀 배포 순서

1. **Terraform Import**: 수동 생성한 리소스를 Terraform State에 가져오기
2. **Terraform Plan**: Drift 확인 및 코드 동기화
3. **Terraform Apply**: 변경사항 적용 (환경 변수 추가 등)
4. **Deployment 재시작**: `kubectl rollout restart`
5. **검증**: Login API 테스트

---

## 📞 연락처

- **Backend Team**: Gateway API 팀
- **Infrastructure Team**: traffic-tacos-infra-iac 팀
- **문서 위치**: `gateway-api/docs/request/gateway-api-auth-infrastructure-requirements.md`
- **관련 PR**: gateway-api Auth 시스템 구현 (커밋 `3e17c9f`)

---

## 🔗 참고 문서

- [DynamoDB Users 테이블 요구사항](./users-table-infrastructure-requirements.md)
- [Frontend 인증 가이드](../FRONTEND_AUTH_GUIDE.md)
- [Auth 배포 이슈 분석](../AUTH_DEPLOYMENT_ISSUE.md)

---

**작성일**: 2025-10-06  
**작성자**: Gateway API Team  
**버전**: 1.0  
**상태**: DRAFT → REVIEW → APPROVED → IMPLEMENTED

