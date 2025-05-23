#!/bin/bash

# Docker Development Helper Script for Corkscrew Generator
# This script provides convenient commands for Docker-based development

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
IMAGE_NAME="corkscrew:dev"
REGISTRY_IMAGE="ghcr.io/jlgore/corkscrew-generator"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Helper functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Function to show usage
show_usage() {
    cat << EOF
Docker Development Helper for Corkscrew Generator

Usage: $0 <command> [options]

Commands:
    build           Build the Docker image locally
    run             Run the container with default settings
    shell           Get a shell inside the container
    test            Run tests in container
    scan            Run a scan with specified services
    clean           Clean up Docker resources
    push            Push image to registry (requires auth)
    pull            Pull latest image from registry
    logs            Show container logs
    help            Show this help message

Examples:
    $0 build
    $0 run --help
    $0 scan s3,ec2 us-east-1
    $0 shell
    $0 clean

Environment Variables:
    AWS_PROFILE     AWS profile to use (default: default)
    AWS_REGION      AWS region (default: us-east-1)
    OUTPUT_DIR      Output directory (default: ./output)
EOF
}

# Function to build Docker image
build_image() {
    log_info "Building Docker image: $IMAGE_NAME"
    cd "$PROJECT_DIR"
    
    if docker build -t "$IMAGE_NAME" .; then
        log_success "Docker image built successfully"
    else
        log_error "Failed to build Docker image"
        exit 1
    fi
}

# Function to run container
run_container() {
    local args="$*"
    local aws_profile="${AWS_PROFILE:-default}"
    local output_dir="${OUTPUT_DIR:-$PROJECT_DIR/output}"
    
    # Create output directory if it doesn't exist
    mkdir -p "$output_dir"
    
    log_info "Running container with args: $args"
    log_info "AWS Profile: $aws_profile"
    log_info "Output Directory: $output_dir"
    
    docker run --rm \
        -v "$HOME/.aws:/home/corkscrew/.aws:ro" \
        -v "$output_dir:/app/output" \
        -e AWS_PROFILE="$aws_profile" \
        "$IMAGE_NAME" \
        $args
}

# Function to get shell access
get_shell() {
    local aws_profile="${AWS_PROFILE:-default}"
    local output_dir="${OUTPUT_DIR:-$PROJECT_DIR/output}"
    
    mkdir -p "$output_dir"
    
    log_info "Starting shell in container"
    docker run --rm -it \
        -v "$HOME/.aws:/home/corkscrew/.aws:ro" \
        -v "$output_dir:/app/output" \
        -e AWS_PROFILE="$aws_profile" \
        --entrypoint /bin/sh \
        "$IMAGE_NAME"
}

# Function to run tests
run_tests() {
    log_info "Running tests in container"
    cd "$PROJECT_DIR"
    
    docker run --rm \
        -v "$PWD:/app" \
        -w /app \
        golang:1.24-alpine \
        sh -c "apk add --no-cache make protobuf protobuf-dev && \
               go install google.golang.org/protobuf/cmd/protoc-gen-go@latest && \
               go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest && \
               make generate-proto && \
               go test ./..."
}

# Function to run scan
run_scan() {
    local services="$1"
    local region="${2:-${AWS_REGION:-us-east-1}}"
    local output_file="scan-$(date +%Y%m%d-%H%M%S).json"
    
    if [ -z "$services" ]; then
        log_error "Services parameter is required"
        echo "Usage: $0 scan <services> [region]"
        echo "Example: $0 scan s3,ec2 us-west-2"
        exit 1
    fi
    
    log_info "Scanning services: $services"
    log_info "Region: $region"
    log_info "Output file: $output_file"
    
    run_container --services "$services" --region "$region" --output "/app/output/$output_file" --verbose
    
    if [ $? -eq 0 ]; then
        log_success "Scan completed. Results saved to: output/$output_file"
    else
        log_error "Scan failed"
        exit 1
    fi
}

# Function to clean up Docker resources
clean_docker() {
    log_info "Cleaning up Docker resources"
    
    # Remove development image
    if docker image inspect "$IMAGE_NAME" >/dev/null 2>&1; then
        docker rmi "$IMAGE_NAME"
        log_success "Removed image: $IMAGE_NAME"
    fi
    
    # Clean up dangling images
    if [ "$(docker images -f dangling=true -q)" ]; then
        docker rmi $(docker images -f dangling=true -q)
        log_success "Removed dangling images"
    fi
    
    # Clean up build cache
    docker builder prune -f
    log_success "Cleaned build cache"
}

# Function to push image
push_image() {
    local tag="${1:-latest}"
    local full_image="$REGISTRY_IMAGE:$tag"
    
    log_info "Tagging image for registry: $full_image"
    docker tag "$IMAGE_NAME" "$full_image"
    
    log_info "Pushing image to registry: $full_image"
    if docker push "$full_image"; then
        log_success "Image pushed successfully"
    else
        log_error "Failed to push image"
        exit 1
    fi
}

# Function to pull image
pull_image() {
    local tag="${1:-latest}"
    local full_image="$REGISTRY_IMAGE:$tag"
    
    log_info "Pulling image from registry: $full_image"
    if docker pull "$full_image"; then
        docker tag "$full_image" "$IMAGE_NAME"
        log_success "Image pulled and tagged as $IMAGE_NAME"
    else
        log_error "Failed to pull image"
        exit 1
    fi
}

# Function to show logs
show_logs() {
    local container_name="$1"
    
    if [ -z "$container_name" ]; then
        log_error "Container name is required"
        echo "Usage: $0 logs <container_name>"
        exit 1
    fi
    
    docker logs -f "$container_name"
}

# Main script logic
case "${1:-help}" in
    build)
        build_image
        ;;
    run)
        shift
        run_container "$@"
        ;;
    shell)
        get_shell
        ;;
    test)
        run_tests
        ;;
    scan)
        shift
        run_scan "$@"
        ;;
    clean)
        clean_docker
        ;;
    push)
        shift
        push_image "$@"
        ;;
    pull)
        shift
        pull_image "$@"
        ;;
    logs)
        shift
        show_logs "$@"
        ;;
    help|--help|-h)
        show_usage
        ;;
    *)
        log_error "Unknown command: $1"
        show_usage
        exit 1
        ;;
esac
