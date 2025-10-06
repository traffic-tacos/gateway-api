#!/usr/bin/env python3
"""
DynamoDB에 더미 사용자를 직접 삽입하는 스크립트
실제 bcrypt 해시를 생성하여 저장합니다.

Requirements:
    pip install boto3 bcrypt

Usage:
    python3 scripts/seed_users.py
    
Environment Variables:
    AWS_REGION (default: ap-northeast-2)
    AWS_PROFILE (default: tacos)
    DYNAMODB_USERS_TABLE_NAME (default: ticket-users)
"""

import os
import sys
import uuid
from datetime import datetime, timezone

try:
    import boto3
    import bcrypt
except ImportError:
    print("❌ Required packages not found!")
    print("   Please install: pip install boto3 bcrypt")
    sys.exit(1)

# Configuration
AWS_REGION = os.getenv("AWS_REGION", "ap-northeast-2")
AWS_PROFILE = os.getenv("AWS_PROFILE", "tacos")
TABLE_NAME = os.getenv("DYNAMODB_USERS_TABLE_NAME", "ticket-users")

print("🎯 Seeding dummy users to DynamoDB (Python Version)...")
print(f"Region: {AWS_REGION}")
print(f"Profile: {AWS_PROFILE}")
print(f"Table Name: {TABLE_NAME}")
print("")

# Initialize boto3 session
session = boto3.Session(profile_name=AWS_PROFILE, region_name=AWS_REGION)
dynamodb = session.client('dynamodb')

def generate_password_hash(password: str) -> str:
    """Generate bcrypt hash for password (cost=10)"""
    salt = bcrypt.gensalt(rounds=10)
    hashed = bcrypt.hashpw(password.encode('utf-8'), salt)
    return hashed.decode('utf-8')

def create_user(num: int):
    """Create a single user in DynamoDB"""
    username = f"user{num:02d}"
    password = f"pwd{num:02d}"
    email = f"{username}@traffictacos.store"
    display_name = f"Test User {num:02d}"
    user_id = str(uuid.uuid4())
    now = datetime.now(timezone.utc).isoformat()
    
    print(f"Creating user: {username}")
    
    # Generate password hash
    print(f"  Generating password hash for: {password}")
    password_hash = generate_password_hash(password)
    
    # Insert into DynamoDB
    try:
        dynamodb.put_item(
            TableName=TABLE_NAME,
            Item={
                'user_id': {'S': user_id},
                'username': {'S': username},
                'password_hash': {'S': password_hash},
                'email': {'S': email},
                'display_name': {'S': display_name},
                'role': {'S': 'user'},
                'created_at': {'S': now},
                'updated_at': {'S': now}
            },
            ConditionExpression='attribute_not_exists(user_id)'
        )
        print(f"  ✅ {username} created successfully (ID: {user_id})")
    except dynamodb.exceptions.ConditionalCheckFailedException:
        # Check if user exists by username
        try:
            response = dynamodb.query(
                TableName=TABLE_NAME,
                IndexName='username-index',
                KeyConditionExpression='username = :username',
                ExpressionAttributeValues={
                    ':username': {'S': username}
                }
            )
            if response['Count'] > 0:
                print(f"  ℹ️  {username} already exists in table (skipping)")
            else:
                print(f"  ⚠️  {username} failed with condition check")
        except Exception as e:
            print(f"  ❌ Failed to verify {username}: {e}")
    except Exception as e:
        print(f"  ❌ Failed to create {username}: {e}")
    
    print("")

def main():
    """Main function to create 10 dummy users"""
    try:
        # Test connection
        dynamodb.describe_table(TableName=TABLE_NAME)
        print(f"✅ Connected to DynamoDB table: {TABLE_NAME}\n")
    except Exception as e:
        print(f"❌ Failed to connect to DynamoDB: {e}")
        sys.exit(1)
    
    # Create 10 users
    for i in range(1, 11):
        create_user(i)
    
    print("=" * 50)
    print("✅ Dummy users seeding completed!")
    print("")
    print("📋 Verify users:")
    print(f"   aws dynamodb scan \\")
    print(f"     --region {AWS_REGION} \\")
    print(f"     --profile {AWS_PROFILE} \\")
    print(f"     --table-name {TABLE_NAME} \\")
    print(f"     --select COUNT")
    print("")
    print("📋 List all users:")
    print(f"   aws dynamodb scan \\")
    print(f"     --region {AWS_REGION} \\")
    print(f"     --profile {AWS_PROFILE} \\")
    print(f"     --table-name {TABLE_NAME} \\")
    print(f"     --projection-expression 'username,email,display_name'")
    print("")
    print("🔐 Test login (user01/pwd01):")
    print("   curl -X POST http://localhost:8000/api/v1/auth/login \\")
    print("     -H 'Content-Type: application/json' \\")
    print("     -d '{\"username\": \"user01\", \"password\": \"pwd01\"}'")
    print("")
    print("📋 Created users:")
    print("   user01 / pwd01")
    print("   user02 / pwd02")
    print("   user03 / pwd03")
    print("   ...")
    print("   user10 / pwd10")
    print("=" * 50)

if __name__ == "__main__":
    main()

