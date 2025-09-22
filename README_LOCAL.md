# Local Development Guide

## Redis Configuration Options

### Option 1: Local Docker Redis (Default)
For local development without VPN access:

```bash
# Uses .env.local (already configured)
./run_local.sh
```

This will:
- Start a local Redis container automatically
- Use `localhost:6379` without authentication
- No TLS encryption required

### Option 2: AWS ElastiCache Redis
For development with AWS ElastiCache (requires VPN or bastion host):

```bash
# Copy AWS configuration
cp .env.aws .env

# Run with AWS ElastiCache
./run_local.sh
```

Prerequisites for ElastiCache:
- VPN connection to AWS VPC or bastion host access
- AWS CLI configured with `tacos` profile
- Network access to ElastiCache security group

## AWS ElastiCache Details

- **Primary Endpoint**: `master.traffic-tacos-redis.w6eqga.apn2.cache.amazonaws.com:6379`
- **Reader Endpoint**: `replica.traffic-tacos-redis.w6eqga.apn2.cache.amazonaws.com:6379`
- **Auth Token**: Stored in AWS Secrets Manager (`traffic-tacos/redis/auth-token`)
- **TLS**: Enabled (in-transit encryption)
- **Region**: `ap-northeast-2`

## Switching Between Environments

```bash
# Local Redis (no VPN required)
cp .env.local .env
./run_local.sh

# AWS ElastiCache (VPN required)
cp .env.aws .env
./run_local.sh
```

## Troubleshooting

### ElastiCache Connection Timeout
If you get `dial tcp: i/o timeout`:
- Ensure you're connected to VPN or using a bastion host
- Check security group allows inbound on port 6379
- Verify your IP is whitelisted in the security group

### AWS Credentials
```bash
# Configure AWS profile
aws configure --profile tacos

# Test credentials
aws sts get-caller-identity --profile tacos

# Test Secrets Manager access
aws secretsmanager get-secret-value \
  --secret-id traffic-tacos/redis/auth-token \
  --profile tacos \
  --region ap-northeast-2
```

### Local Redis Issues
```bash
# Check if Redis is running
docker ps | grep redis

# Start Redis manually
docker run -d --name gateway-redis -p 6379:6379 redis:7-alpine

# Check Redis logs
docker logs gateway-redis
```

## Environment Variables Summary

| Variable | Local Docker | AWS ElastiCache |
|----------|-------------|-----------------|
| `REDIS_ADDRESS` | `localhost:6379` | `master.traffic-tacos-redis.w6eqga.apn2.cache.amazonaws.com:6379` |
| `REDIS_PASSWORD` | (empty) | (from Secrets Manager) |
| `REDIS_TLS_ENABLED` | `false` | `true` |
| `REDIS_PASSWORD_FROM_SECRETS` | `false` | `true` |
| `AWS_PROFILE` | `tacos` | `tacos` |
| `AWS_REGION` | `ap-northeast-2` | `ap-northeast-2` |