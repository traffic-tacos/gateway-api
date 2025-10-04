# GitHub Actions CI/CD for gateway-api

이 디렉토리는 gateway-api의 CI/CD 파이프라인을 관리합니다.

## 워크플로우

### build.yml - Build, Test and Deploy

**트리거:**
- `main`, `develop` 브랜치에 push
- `main`, `develop` 브랜치로의 Pull Request

**주요 작업:**

1. **Test & Lint**
   - Go 1.22 환경 설정
   - 의존성 다운로드 및 검증
   - 유닛 테스트 실행 (커버리지 리포트 포함)
   - golangci-lint를 통한 코드 품질 검사

2. **Build & Push Docker Image**
   - Docker Buildx를 통한 멀티 플랫폼 빌드 (linux/amd64)
   - AWS ECR에 이미지 푸시
   - 이미지 태그: `{git-short-sha}`, `latest`
   - Trivy를 통한 보안 취약점 스캔

3. **Update Deployment Manifest** (main 브랜치만)
   - `deployment-repo`에 repository_dispatch 이벤트 전송
   - 자동으로 Kubernetes manifest의 이미지 태그 업데이트
   - ArgoCD가 자동으로 변경사항을 감지하고 배포

## 필수 GitHub Secrets

이 워크플로우를 실행하려면 다음 Secrets를 설정해야 합니다:

### 1. AWS_ROLE_ARN
AWS OIDC 인증을 위한 IAM Role ARN

**설정 값:**
```
arn:aws:iam::137406935518:role/GitHubActionsRole
```

**필요한 권한:**
- `ecr:GetAuthorizationToken`
- `ecr:BatchCheckLayerAvailability`
- `ecr:GetDownloadUrlForLayer`
- `ecr:BatchGetImage`
- `ecr:PutImage`
- `ecr:InitiateLayerUpload`
- `ecr:UploadLayerPart`
- `ecr:CompleteLayerUpload`

### 2. DEPLOYMENT_REPO_TOKEN
deployment-repo에 접근하기 위한 GitHub Personal Access Token (PAT)

**필요한 권한:**
- `repo` (전체 저장소 접근)
- 또는 최소한: `repo:status`, `repo_deployment`, `public_repo`

**생성 방법:**
1. GitHub Settings → Developer settings → Personal access tokens → Tokens (classic)
2. "Generate new token (classic)" 클릭
3. 필요한 권한 선택
4. 토큰 생성 후 복사

**설정 위치:**
- Repository Settings → Secrets and variables → Actions
- "New repository secret" 클릭
- Name: `DEPLOYMENT_REPO_TOKEN`
- Secret: [생성한 PAT 붙여넣기]

## 이미지 태그 전략

### main 브랜치
```
{short-sha}  # 예: a1b2c3d
latest
```

### develop/기타 브랜치
```
{branch}-{short-sha}  # 예: develop-a1b2c3d
latest
```

## 배포 프로세스

1. **코드 Push** → `main` 브랜치
2. **GitHub Actions 실행**
   - 테스트 및 린트
   - Docker 이미지 빌드 및 ECR 푸시
3. **Repository Dispatch** → `deployment-repo`
4. **Manifest 업데이트** → `manifests/gateway-api/deployment.yaml`
5. **ArgoCD 자동 동기화** → Kubernetes 클러스터에 배포
6. **Pod 재시작** → 새 이미지로 업데이트

## 모니터링

### ArgoCD
```
https://argocd.traffictacos.store/applications/gateway-api
```

### Kubernetes
```bash
kubectl get pods -n tacos-app -l app=gateway-api
kubectl logs -n tacos-app -l app=gateway-api --tail=100
```

### ECR
```bash
aws ecr describe-images \
  --repository-name traffic-tacos-gateway-api \
  --region ap-northeast-2 \
  --query 'reverse(sort_by(imageDetails,& imagePushedAt))[:5]'
```

## 트러블슈팅

### 워크플로우 실패 시

1. **테스트 실패**
   - Actions 탭에서 로그 확인
   - 로컬에서 `go test ./...` 실행

2. **빌드 실패**
   - Dockerfile 문법 확인
   - 로컬에서 `docker build .` 테스트

3. **ECR 푸시 실패**
   - `AWS_ROLE_ARN` Secret 확인
   - IAM Role의 신뢰 관계 및 권한 확인

4. **Deployment 업데이트 실패**
   - `DEPLOYMENT_REPO_TOKEN` Secret 확인
   - PAT 권한 및 만료일 확인
   - deployment-repo의 webhook 로그 확인

## 로컬 테스트

### 유닛 테스트 실행
```bash
go test -v -race -coverprofile=coverage.out ./...
```

### Linter 실행
```bash
golangci-lint run --timeout=5m
```

### Docker 이미지 빌드
```bash
docker build --platform linux/amd64 -t gateway-api:local .
```

### Docker 이미지 실행
```bash
docker run --rm -p 8000:8000 \
  -e PORT=8000 \
  -e REDIS_ADDRESS=localhost:6379 \
  -e REDIS_TLS_ENABLED=false \
  -e JWT_JWKS_ENDPOINT=https://www.googleapis.com/oauth2/v3/certs \
  gateway-api:local
```

## Gateway API 특징

gateway-api는 Traffic Tacos의 API Gateway로서 다음 기능을 제공합니다:

- **라우팅**: 클라이언트 요청을 적절한 백엔드 서비스로 전달
- **인증/인가**: JWT 토큰 검증 및 사용자 권한 확인
- **캐싱**: Redis를 통한 응답 캐싱
- **속도 제한**: Rate limiting 기능
- **로깅/모니터링**: 요청/응답 로깅 및 메트릭 수집

### 환경 변수

| 변수명 | 설명 | 예시 |
|--------|------|------|
| `PORT` | 서버 포트 | `8000` |
| `REDIS_ADDRESS` | Redis 주소 | `master.xxx.cache.amazonaws.com:6379` |
| `REDIS_TLS_ENABLED` | Redis TLS 사용 여부 | `true` |
| `REDIS_TLS_INSECURE_SKIP_VERIFY` | TLS 인증서 검증 스킵 | `true` |
| `JWT_JWKS_ENDPOINT` | JWT 검증을 위한 JWKS 엔드포인트 | `https://www.googleapis.com/oauth2/v3/certs` |

## 참고 자료

- [GitHub Actions 문서](https://docs.github.com/en/actions)
- [AWS ECR 문서](https://docs.aws.amazon.com/ecr/)
- [ArgoCD 문서](https://argo-cd.readthedocs.io/)
- [Docker Buildx 문서](https://docs.docker.com/buildx/working-with-buildx/)

