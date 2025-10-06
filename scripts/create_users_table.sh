#!/bin/bash

# DynamoDB Users 테이블 생성 스크립트
# Usage: ./scripts/create_users_table.sh

set -e

AWS_REGION="${AWS_REGION:-ap-northeast-2}"
AWS_PROFILE="${AWS_PROFILE:-tacos}"
TABLE_NAME="${DYNAMODB_USERS_TABLE_NAME:-traffic-tacos-users}"

echo "🚀 Creating DynamoDB Users table..."
echo "Region: ${AWS_REGION}"
echo "Profile: ${AWS_PROFILE}"
echo "Table Name: ${TABLE_NAME}"
echo ""

# 테이블 생성
aws dynamodb create-table \
    --region "${AWS_REGION}" \
    --profile "${AWS_PROFILE}" \
    --table-name "${TABLE_NAME}" \
    --attribute-definitions \
        AttributeName=user_id,AttributeType=S \
        AttributeName=username,AttributeType=S \
    --key-schema \
        AttributeName=user_id,KeyType=HASH \
    --billing-mode PAY_PER_REQUEST \
    --global-secondary-indexes \
        "[
            {
                \"IndexName\": \"username-index\",
                \"KeySchema\": [{\"AttributeName\":\"username\",\"KeyType\":\"HASH\"}],
                \"Projection\":{\"ProjectionType\":\"ALL\"}
            }
        ]" \
    --tags \
        Key=Project,Value=traffic-tacos \
        Key=Environment,Value=development \
        Key=ManagedBy,Value=script

if [ $? -eq 0 ]; then
    echo ""
    echo "✅ Table created successfully!"
    echo ""
    echo "Waiting for table to become ACTIVE..."
    
    aws dynamodb wait table-exists \
        --region "${AWS_REGION}" \
        --profile "${AWS_PROFILE}" \
        --table-name "${TABLE_NAME}"
    
    echo ""
    echo "✅ Table is now ACTIVE!"
    echo ""
    echo "📋 Table Details:"
    aws dynamodb describe-table \
        --region "${AWS_REGION}" \
        --profile "${AWS_PROFILE}" \
        --table-name "${TABLE_NAME}" \
        --query 'Table.[TableName,TableStatus,ItemCount,TableSizeBytes]' \
        --output table
else
    echo ""
    echo "❌ Failed to create table!"
    echo "Note: If table already exists, this is expected."
    exit 1
fi

echo ""
echo "=========================================="
echo "✅ Setup completed!"
echo ""
echo "Next steps:"
echo "1. Set environment variable:"
echo "   export DYNAMODB_USERS_TABLE_NAME=${TABLE_NAME}"
echo ""
echo "2. Run dummy users creation:"
echo "   ./scripts/create_dummy_users.sh"
echo "=========================================="

