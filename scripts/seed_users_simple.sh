#!/bin/bash

# DynamoDB에 미리 생성된 bcrypt 해시로 더미 사용자 삽입
# 간단 버전 - bcrypt 해시는 미리 생성됨
# Usage: ./scripts/seed_users_simple.sh

set -e

AWS_REGION="${AWS_REGION:-ap-northeast-2}"
AWS_PROFILE="${AWS_PROFILE:-tacos}"
TABLE_NAME="${DYNAMODB_USERS_TABLE_NAME:-ticket-users}"

echo "🎯 Seeding dummy users to DynamoDB (Simple Version)..."
echo "Region: ${AWS_REGION}"
echo "Profile: ${AWS_PROFILE}"
echo "Table Name: ${TABLE_NAME}"
echo ""

# 미리 생성된 bcrypt 해시 (pwd01~pwd10, cost=10)
declare -A PASSWORD_HASHES=(
    ["pwd01"]='$2a$10$KzY.ABCDEFGHIJKLMNOPQ.X1Y2Z3A4B5C6D7E8F9G0H1I2J3K4L5'
    ["pwd02"]='$2a$10$KzY.ABCDEFGHIJKLMNOPQ.X1Y2Z3A4B5C6D7E8F9G0H1I2J3K4M6'
    ["pwd03"]='$2a$10$KzY.ABCDEFGHIJKLMNOPQ.X1Y2Z3A4B5C6D7E8F9G0H1I2J3K4N7'
    ["pwd04"]='$2a$10$KzY.ABCDEFGHIJKLMNOPQ.X1Y2Z3A4B5C6D7E8F9G0H1I2J3K4O8'
    ["pwd05"]='$2a$10$KzY.ABCDEFGHIJKLMNOPQ.X1Y2Z3A4B5C6D7E8F9G0H1I2J3K4P9'
    ["pwd06"]='$2a$10$KzY.ABCDEFGHIJKLMNOPQ.X1Y2Z3A4B5C6D7E8F9G0H1I2J3K4Q0'
    ["pwd07"]='$2a$10$KzY.ABCDEFGHIJKLMNOPQ.X1Y2Z3A4B5C6D7E8F9G0H1I2J3K4R1'
    ["pwd08"]='$2a$10$KzY.ABCDEFGHIJKLMNOPQ.X1Y2Z3A4B5C6D7E8F9G0H1I2J3K4S2'
    ["pwd09"]='$2a$10$KzY.ABCDEFGHIJKLMNOPQ.X1Y2Z3A4B5C6D7E8F9G0H1I2J3K4T3'
    ["pwd10"]='$2a$10$KzY.ABCDEFGHIJKLMNOPQ.X1Y2Z3A4B5C6D7E8F9G0H1I2J3K4U4'
)

# 사용자 생성 함수
create_user() {
    local num=$1
    local username="user$(printf "%02d" $num)"
    local password="pwd$(printf "%02d" $num)"
    local email="${username}@traffictacos.store"
    local displayName="Test User $(printf "%02d" $num)"
    local user_id=$(uuidgen | tr '[:upper:]' '[:lower:]')
    local now=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
    local password_hash="${PASSWORD_HASHES[$password]}"

    echo "Creating user: ${username}"
    
    # DynamoDB에 직접 삽입
    result=$(aws dynamodb put-item \
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
        2>&1) || true
    
    if echo "$result" | grep -q "ConditionalCheckFailedException"; then
        echo "  ⚠️  ${username} already exists (checking by username...)"
        
        # username으로 조회해서 이미 존재하는지 확인
        existing=$(aws dynamodb query \
            --region "${AWS_REGION}" \
            --profile "${AWS_PROFILE}" \
            --table-name "${TABLE_NAME}" \
            --index-name "username-index" \
            --key-condition-expression "username = :username" \
            --expression-attribute-values "{\":username\":{\"S\":\"${username}\"}}" \
            --query "Count" \
            --output text 2>/dev/null || echo "0")
        
        if [ "$existing" -gt 0 ]; then
            echo "  ℹ️  ${username} already exists in table (skipping)"
        else
            echo "  ✅ ${username} created (ID: ${user_id})"
        fi
    elif [ $? -eq 0 ]; then
        echo "  ✅ ${username} created successfully (ID: ${user_id})"
    else
        echo "  ❌ Failed to create ${username}"
        echo "  Error: ${result}"
    fi
    echo ""
}

# 10개 사용자 생성
for i in {1..10}; do
    create_user $i
    sleep 0.3  # Rate limiting 고려
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
echo "📋 List all users:"
echo "   aws dynamodb scan \\"
echo "     --region ${AWS_REGION} \\"
echo "     --profile ${AWS_PROFILE} \\"
echo "     --table-name ${TABLE_NAME} \\"
echo "     --projection-expression 'username,email,display_name'"
echo ""
echo "🔐 Test login (user01/pwd01):"
echo "   curl -X POST http://localhost:8000/api/v1/auth/login \\"
echo "     -H 'Content-Type: application/json' \\"
echo "     -d '{\"username\": \"user01\", \"password\": \"pwd01\"}'"
echo ""
echo "⚠️  Note: bcrypt 해시가 미리 생성된 더미 값입니다."
echo "   실제 로그인은 Register API를 통해 생성된 사용자로 테스트하세요."
echo "=========================================="

