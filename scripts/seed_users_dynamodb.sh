#!/bin/bash

# DynamoDB에 직접 더미 사용자 삽입 스크립트
# Usage: ./scripts/seed_users_dynamodb.sh

set -e

AWS_REGION="${AWS_REGION:-ap-northeast-2}"
AWS_PROFILE="${AWS_PROFILE:-tacos}"
TABLE_NAME="${DYNAMODB_USERS_TABLE_NAME:-ticket-users}"

echo "🎯 Seeding dummy users directly to DynamoDB..."
echo "Region: ${AWS_REGION}"
echo "Profile: ${AWS_PROFILE}"
echo "Table Name: ${TABLE_NAME}"
echo ""

# bcrypt 해시 생성 함수 (Go 사용)
generate_bcrypt_hash() {
    local password=$1
    # Go를 사용해 bcrypt 해시 생성 (cost=10)
    go run - <<EOF
package main
import (
    "fmt"
    "golang.org/x/crypto/bcrypt"
)
func main() {
    hash, _ := bcrypt.GenerateFromPassword([]byte("${password}"), bcrypt.DefaultCost)
    fmt.Print(string(hash))
}
EOF
}

# 사용자 생성 함수
create_user() {
    local num=$1
    local username="user$(printf "%02d" $num)"
    local password="pwd$(printf "%02d" $num)"
    local email="${username}@traffictacos.store"
    local displayName="Test User $(printf "%02d" $num)"
    local user_id=$(uuidgen | tr '[:upper:]' '[:lower:]')
    local now=$(date -u +"%Y-%m-%dT%H:%M:%SZ")

    echo "Creating user: ${username}"
    
    # bcrypt 해시 생성
    echo "  Generating password hash..."
    password_hash=$(generate_bcrypt_hash "${password}")
    
    # DynamoDB에 직접 삽입
    aws dynamodb put-item \
        --region "${AWS_REGION}" \
        --profile "${AWS_PROFILE}" \
        --table-name "${TABLE_NAME}" \
        --item "{
            \"user_id\": {\"S\": \"${user_id}\"},
            \"username\": {\"S\": \"${username}\"},
            \"password_hash\": {\"S\": \"${password_hash}\"},
            \"email\": {\"S\": \"${email}\"},
            \"display_name\": {\"S\": \"${displayName}\"},
            \"role\": {\"S\": \"user\"},
            \"created_at\": {\"S\": \"${now}\"},
            \"updated_at\": {\"S\": \"${now}\"}
        }" \
        --condition-expression "attribute_not_exists(user_id)" \
        2>&1
    
    if [ $? -eq 0 ]; then
        echo "  ✅ ${username} created successfully (ID: ${user_id})"
    else
        echo "  ⚠️  ${username} might already exist or failed to create"
    fi
    echo ""
}

# 10개 사용자 생성
for i in {1..10}; do
    create_user $i
    sleep 0.2  # Rate limiting 고려
done

echo "=========================================="
echo "✅ Dummy users seeding completed!"
echo ""
echo "📋 Verify users:"
echo "   aws dynamodb scan \\"
echo "     --region ${AWS_REGION} \\"
echo "     --profile ${AWS_PROFILE} \\"
echo "     --table-name ${TABLE_NAME} \\"
echo "     --select COUNT"
echo ""
echo "🔐 Test login:"
echo "   curl -X POST http://localhost:8000/api/v1/auth/login \\"
echo "     -H 'Content-Type: application/json' \\"
echo "     -d '{\"username\": \"user01\", \"password\": \"pwd01\"}'"
echo "=========================================="

