# Infrastructure Requests

이 디렉토리는 Gateway API 개발을 위해 인프라팀에 요청하는 리소스 명세를 담고 있습니다.

## 📋 요청 목록

### 1. Users Table 인프라 요구사항 ✅

**문서**: [users-table-infrastructure-requirements.md](./users-table-infrastructure-requirements.md)

**요청 내용**:
- DynamoDB `traffic-tacos-users` 테이블 생성
- GSI: `username-index` (로그인용)
- IAM Policy: Gateway API → Users 테이블 접근 권한
- IRSA 설정: `gateway-api` ServiceAccount

**우선순위**: High  
**상태**: 요청 대기 중 (Pending)

**Quick Summary**:
```hcl
# Table Spec
Table Name: traffic-tacos-users
Hash Key: user_id (S)
GSI: username-index (hash_key: username)
Billing Mode: PAY_PER_REQUEST

# IAM Actions Needed
- dynamodb:GetItem
- dynamodb:PutItem
- dynamodb:Query
- dynamodb:UpdateItem
- dynamodb:DeleteItem
```

**필요한 환경 변수**:
```bash
DYNAMODB_USERS_TABLE_NAME=traffic-tacos-users
DYNAMODB_REGION=ap-northeast-2
JWT_SECRET=<secret-value>
```

---

## 🔄 요청 프로세스

1. **요청 문서 작성** (Application Team)
   - 요구사항 명세
   - 스키마 정의
   - IAM 권한 정의

2. **리뷰 및 승인** (Infrastructure Team)
   - Terraform 코드 작성
   - 보안 검토
   - 비용 추정

3. **배포** (DevOps Team)
   - `terraform apply`
   - Secret 생성
   - IRSA 설정

4. **검증** (Application Team)
   - API 테스트
   - 권한 확인
   - 문서 업데이트

## 📞 연락처

- **Slack**: #traffic-tacos-infra
- **Email**: gateway-api-team@traffictacos.store
- **Terraform Repository**: `traffic-tacos-infra-iac`

## 📚 참고 문서

- [DynamoDB Module 가이드](../../modules/dynamodb/README.md)
- [IRSA 설정 가이드](https://docs.aws.amazon.com/eks/latest/userguide/iam-roles-for-service-accounts.html)
- [Gateway API 아키텍처](../ARCHITECTURE.md)

