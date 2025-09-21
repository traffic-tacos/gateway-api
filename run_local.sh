#!/bin/bash

# Local development script for Gateway API
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to print colored output
print_status() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check if .env file exists, if not copy from .env.local
setup_env() {
    if [ ! -f ".env" ]; then
        if [ -f ".env.local" ]; then
            print_status "Copying .env.local to .env..."
            cp .env.local .env
            print_success "Environment file created. Please edit .env with your actual JWT configuration."
        else
            print_error ".env.local file not found. Please create environment configuration."
            exit 1
        fi
    else
        print_status "Using existing .env file"
    fi
}

# Check Redis configuration and AWS connectivity
check_redis() {
    print_status "Checking Redis configuration..."

    # Source the .env file to get Redis address
    if [ -f ".env" ]; then
        REDIS_ADDRESS=$(grep "^REDIS_ADDRESS=" .env | cut -d'=' -f2)
        AWS_REGION=$(grep "^AWS_REGION=" .env | cut -d'=' -f2)
        AWS_PROFILE=$(grep "^AWS_PROFILE=" .env | cut -d'=' -f2)
    fi

    # Check if using AWS ElastiCache
    if [[ "$REDIS_ADDRESS" == *"cache.amazonaws.com"* ]]; then
        print_status "Using AWS ElastiCache: $REDIS_ADDRESS"

        # Check AWS credentials
        if [ -n "$AWS_PROFILE" ]; then
            print_status "Using AWS profile: $AWS_PROFILE"
            export AWS_PROFILE="$AWS_PROFILE"
        fi

        if [ -n "$AWS_REGION" ]; then
            print_status "Using AWS region: $AWS_REGION"
            export AWS_REGION="$AWS_REGION"
        fi

        # Verify AWS credentials
        if command -v aws >/dev/null 2>&1; then
            if aws sts get-caller-identity >/dev/null 2>&1; then
                print_success "AWS credentials are valid"
            else
                print_error "AWS credentials not configured or invalid"
                print_status "Please configure AWS credentials:"
                print_status "  aws configure --profile tacos"
                print_status "Or set AWS_ACCESS_KEY_ID and AWS_SECRET_ACCESS_KEY"
                exit 1
            fi
        else
            print_warning "AWS CLI not found. Make sure AWS credentials are properly configured."
        fi

        print_success "ElastiCache configuration ready"
    else
        print_status "Using local Redis: $REDIS_ADDRESS"
        # Original local Redis logic
        if command -v redis-cli >/dev/null 2>&1; then
            if redis-cli -h localhost ping >/dev/null 2>&1; then
                print_success "Local Redis is running"
            else
                print_warning "Local Redis is not running. Starting Redis with Docker..."
                docker run -d --name gateway-redis -p 6379:6379 redis:7-alpine
                sleep 2
                if redis-cli -h localhost ping >/dev/null 2>&1; then
                    print_success "Redis started successfully"
                else
                    print_error "Failed to start Redis"
                    exit 1
                fi
            fi
        else
            print_warning "redis-cli not found. Starting Redis with Docker..."
            docker run -d --name gateway-redis -p 6379:6379 redis:7-alpine
            sleep 2
            print_success "Redis started with Docker"
        fi
    fi
}

# Install dependencies
install_deps() {
    print_status "Installing Go dependencies..."
    go mod download
    go mod tidy
    print_success "Dependencies installed"
}

# Generate Swagger docs
generate_docs() {
    print_status "Generating Swagger documentation..."
    if command -v swag >/dev/null 2>&1; then
        make swagger
        print_success "Swagger docs generated"
    else
        print_warning "swag command not found. Installing..."
        go install github.com/swaggo/swag/cmd/swag@latest
        make swagger
        print_success "Swagger docs generated"
    fi
}

# Build the application
build_app() {
    print_status "Building application..."
    make build
    print_success "Application built successfully"
}

# Run the application
run_app() {
    print_status "Starting Gateway API..."
    print_status "Loading environment from .env file"

    # Source the .env file
    if [ -f ".env" ]; then
        export $(cat .env | grep -v '^#' | xargs)
    fi

    print_status "Server will start on http://localhost:${SERVER_PORT:-8000}"
    print_status "Swagger UI available at http://localhost:${SERVER_PORT:-8000}/swagger/index.html"
    print_status "Health check at http://localhost:${SERVER_PORT:-8000}/healthz"

    go run cmd/gateway/main.go
}

# Main function
main() {
    echo -e "${BLUE}"
    echo "=================================="
    echo "  Gateway API Local Development"
    echo "=================================="
    echo -e "${NC}"

    case "${1:-run}" in
        "setup")
            setup_env
            check_redis
            install_deps
            generate_docs
            print_success "Setup completed! Run './run_local.sh' to start the server."
            ;;
        "build")
            setup_env
            install_deps
            generate_docs
            build_app
            ;;
        "run")
            setup_env
            check_redis
            install_deps
            generate_docs
            run_app
            ;;
        "redis")
            check_redis
            ;;
        "docs")
            generate_docs
            ;;
        "clean")
            print_status "Cleaning up..."
            docker stop gateway-redis 2>/dev/null || true
            docker rm gateway-redis 2>/dev/null || true
            make clean
            print_success "Cleanup completed"
            ;;
        "help")
            echo "Usage: $0 [command]"
            echo "Commands:"
            echo "  setup  - Setup development environment"
            echo "  build  - Build the application"
            echo "  run    - Run the application (default)"
            echo "  redis  - Start Redis if not running"
            echo "  docs   - Generate Swagger documentation"
            echo "  clean  - Clean up containers and build artifacts"
            echo "  help   - Show this help"
            ;;
        *)
            print_error "Unknown command: $1"
            print_status "Use '$0 help' for available commands"
            exit 1
            ;;
    esac
}

# Run main function with all arguments
main "$@"