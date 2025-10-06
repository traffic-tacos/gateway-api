#!/bin/bash

# 더미 사용자 생성 스크립트
# Usage: ./scripts/create_dummy_users.sh

set -e

BASE_URL="${API_URL:-http://localhost:8000}"
REGISTER_ENDPOINT="${BASE_URL}/api/v1/auth/register"

echo "🎯 Creating 10 dummy users for testing..."
echo "API URL: ${BASE_URL}"
echo ""

# 사용자 생성 함수
create_user() {
    local num=$1
    local username="user$(printf "%02d" $num)"
    local password="pwd$(printf "%02d" $num)"
    local email="${username}@traffictacos.store"
    local displayName="Test User $(printf "%02d" $num)"

    echo "Creating user: ${username}"
    
    response=$(curl -s -w "\n%{http_code}" -X POST "${REGISTER_ENDPOINT}" \
        -H "Content-Type: application/json" \
        -d "{
            \"username\": \"${username}\",
            \"password\": \"${password}\",
            \"email\": \"${email}\",
            \"display_name\": \"${displayName}\"
        }")
    
    http_code=$(echo "$response" | tail -n1)
    body=$(echo "$response" | sed '$d')
    
    if [ "$http_code" -eq 201 ]; then
        echo "✅ ${username} created successfully"
        echo "   Token: $(echo $body | jq -r '.token' | cut -c 1-30)..."
    elif [ "$http_code" -eq 409 ]; then
        echo "⚠️  ${username} already exists (skipping)"
    else
        echo "❌ Failed to create ${username} (HTTP ${http_code})"
        echo "   Response: ${body}"
    fi
    echo ""
}

# 10개 사용자 생성
for i in {1..10}; do
    create_user $i
    sleep 0.5  # Rate limiting 고려
done

echo "=========================================="
echo "✅ Dummy users creation completed!"
echo ""
echo "📋 User Credentials:"
echo "   user01 / pwd01"
echo "   user02 / pwd02"
echo "   user03 / pwd03"
echo "   ..."
echo "   user10 / pwd10"
echo ""
echo "🔐 Login Example:"
echo "   curl -X POST ${BASE_URL}/api/v1/auth/login \\"
echo "     -H 'Content-Type: application/json' \\"
echo "     -d '{\"username\": \"user01\", \"password\": \"pwd01\"}'"
echo "=========================================="

