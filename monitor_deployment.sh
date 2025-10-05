#!/bin/bash

# Gateway API Deployment Monitor
# 새 버전 배포를 모니터링하고 검증하는 스크립트

set -e

SERVICE_NAME="gateway-api"
NAMESPACE="tacos-app"
ECR_REPO="traffic-tacos-gateway-api"
REGION="ap-northeast-2"
PROFILE="tacos"

echo "========================================="
echo "Gateway API Deployment Monitor"
echo "========================================="
echo ""

# 1. GitHub Actions 최신 실행 확인
echo "📊 Checking GitHub Actions status..."
echo "Visit: https://github.com/traffic-tacos/gateway-api/actions"
echo ""

# 2. ECR 최신 이미지 확인
echo "🐳 Checking latest ECR images..."
aws ecr describe-images \
  --repository-name $ECR_REPO \
  --region $REGION \
  --profile $PROFILE \
  --query 'sort_by(imageDetails,& imagePushedAt)[-5:].[imageTags[0],imagePushedAt]' \
  --output table
echo ""

# 3. 현재 배포된 이미지 확인
echo "🎯 Current deployed image:"
kubectl get deployment $SERVICE_NAME -n $NAMESPACE \
  -o jsonpath='{.spec.template.spec.containers[0].image}'
echo ""
echo ""

# 4. Pod 상태 확인
echo "📦 Current Pods:"
kubectl get pods -n $NAMESPACE -l app=$SERVICE_NAME
echo ""

# 5. ReplicaSet 확인
echo "📋 ReplicaSets (latest 3):"
kubectl get rs -n $NAMESPACE -l app=$SERVICE_NAME \
  --sort-by=.metadata.creationTimestamp | tail -4
echo ""

# 6. Rollout 상태 확인
echo "🔄 Rollout status:"
kubectl rollout status deployment/$SERVICE_NAME -n $NAMESPACE --timeout=10s 2>&1 || echo "No rollout in progress"
echo ""

# 7. Health check
echo "❤️ Health check:"
curl -s https://api.traffictacos.store/healthz | jq . || echo "Health check failed"
echo ""

# 8. 최근 로그 확인
echo "📝 Recent logs (last 10 lines):"
kubectl logs -n $NAMESPACE -l app=$SERVICE_NAME --tail=10 --since=2m 2>&1 | head -20
echo ""

echo "========================================="
echo "Monitor completed at $(date)"
echo "========================================="
