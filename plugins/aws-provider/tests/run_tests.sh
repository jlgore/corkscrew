#!/bin/bash

# AWS Provider End-to-End Test Runner Script
# Usage: ./run_tests.sh [test-type] [options]

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Default values
TEST_TYPE="all"
TIMEOUT="30m"
VERBOSE=false
COVERAGE=false
BENCHTIME="5s"

# Script directory
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

print_usage() {
    echo "Usage: $0 [test-type] [options]"
    echo ""
    echo "Test Types:"
    echo "  all                    Run all integration tests"
    echo "  pipeline              Run complete pipeline tests"
    echo "  discovery             Run discovery pipeline tests"
    echo "  scanning              Run scanning pipeline tests"
    echo "  workflow              Run provider workflow tests"
    echo "  scanner               Run scanner component tests"
    echo "  performance           Run performance tests"
    echo "  benchmarks            Run all benchmarks"
    echo "  unit                  Run unit tests only"
    echo ""
    echo "Options:"
    echo "  -v, --verbose         Verbose output"
    echo "  -c, --coverage        Generate coverage report"
    echo "  -t, --timeout DURATION    Test timeout (default: 30m)"
    echo "  -b, --benchtime DURATION  Benchmark time (default: 5s)"
    echo "  -h, --help            Show this help"
    echo ""
    echo "Examples:"
    echo "  $0 all -v -c                    # Run all tests with coverage"
    echo "  $0 pipeline -t 10m              # Run pipeline tests with 10m timeout"
    echo "  $0 benchmarks -b 10s            # Run benchmarks for 10s each"
    echo "  $0 scanning --verbose           # Run scanning tests with verbose output"
}

print_header() {
    echo -e "${BLUE}================================================${NC}"
    echo -e "${BLUE}  AWS Provider End-to-End Test Suite${NC}"
    echo -e "${BLUE}================================================${NC}"
    echo ""
}

print_section() {
    echo -e "${YELLOW}>>> $1${NC}"
    echo ""
}

print_success() {
    echo -e "${GREEN}✓ $1${NC}"
}

print_error() {
    echo -e "${RED}✗ $1${NC}"
}

print_info() {
    echo -e "${BLUE}ℹ $1${NC}"
}

check_prerequisites() {
    print_section "Checking Prerequisites"
    
    # Check if Go is installed
    if ! command -v go &> /dev/null; then
        print_error "Go is not installed or not in PATH"
        exit 1
    fi
    print_success "Go is installed: $(go version | cut -d' ' -f3)"
    
    # Check if we're in the right directory
    if [[ ! -f "$PROJECT_DIR/go.mod" ]]; then
        print_error "Not in AWS provider directory. Expected go.mod file."
        exit 1
    fi
    print_success "Project directory: $PROJECT_DIR"
    
    # Check AWS credentials
    if [[ -z "$AWS_ACCESS_KEY_ID" && -z "$AWS_PROFILE" ]]; then
        if ! aws sts get-caller-identity &> /dev/null; then
            print_error "AWS credentials not configured. Please run 'aws configure' or set environment variables."
            exit 1
        fi
    fi
    print_success "AWS credentials configured"
    
    # Set required environment variables
    export RUN_INTEGRATION_TESTS=true
    if [[ "$TEST_TYPE" == "benchmarks" || "$TEST_TYPE" == "performance" ]]; then
        export RUN_BENCHMARKS=true
    fi
    
    print_success "Environment variables set"
    echo ""
}

run_test_command() {
    local test_name="$1"
    local test_pattern="$2"
    local extra_args="$3"
    
    print_section "Running $test_name"
    
    local cmd_args="-tags=integration"
    
    if [[ "$VERBOSE" == true ]]; then
        cmd_args="$cmd_args -v"
    fi
    
    if [[ "$COVERAGE" == true ]]; then
        cmd_args="$cmd_args -coverprofile=coverage_${test_name,,}.out"
    fi
    
    cmd_args="$cmd_args -timeout $TIMEOUT"
    
    if [[ -n "$test_pattern" ]]; then
        cmd_args="$cmd_args -run $test_pattern"
    fi
    
    if [[ -n "$extra_args" ]]; then
        cmd_args="$cmd_args $extra_args"
    fi
    
    local full_cmd="go test $cmd_args ./tests"
    
    print_info "Command: $full_cmd"
    echo ""
    
    if eval "$full_cmd"; then
        print_success "$test_name completed successfully"
    else
        print_error "$test_name failed"
        return 1
    fi
    echo ""
}

run_benchmark_command() {
    local bench_name="$1"
    local bench_pattern="$2"
    
    print_section "Running $bench_name"
    
    local cmd_args="-tags=integration -bench=$bench_pattern -benchtime=$BENCHTIME"
    
    if [[ "$VERBOSE" == true ]]; then
        cmd_args="$cmd_args -v"
    fi
    
    cmd_args="$cmd_args -timeout $TIMEOUT"
    
    local full_cmd="go test $cmd_args ./tests"
    
    print_info "Command: $full_cmd"
    echo ""
    
    if eval "$full_cmd"; then
        print_success "$bench_name completed successfully"
    else
        print_error "$bench_name failed"
        return 1
    fi
    echo ""
}

generate_coverage_report() {
    if [[ "$COVERAGE" == true ]]; then
        print_section "Generating Coverage Report"
        
        # Combine coverage files if multiple exist
        local coverage_files=(coverage_*.out)
        if [[ ${#coverage_files[@]} -gt 1 ]]; then
            print_info "Combining coverage files..."
            echo "mode: set" > coverage_combined.out
            for file in "${coverage_files[@]}"; do
                if [[ -f "$file" ]]; then
                    tail -n +2 "$file" >> coverage_combined.out
                fi
            done
            coverage_file="coverage_combined.out"
        elif [[ -f "coverage_all.out" ]]; then
            coverage_file="coverage_all.out"
        else
            coverage_file="${coverage_files[0]}"
        fi
        
        if [[ -f "$coverage_file" ]]; then
            go tool cover -html="$coverage_file" -o coverage.html
            print_success "Coverage report generated: coverage.html"
            
            # Show coverage percentage
            local coverage_pct=$(go tool cover -func="$coverage_file" | grep total | awk '{print $3}')
            print_info "Total coverage: $coverage_pct"
        else
            print_error "No coverage file found"
        fi
        echo ""
    fi
}

cleanup() {
    # Clean up temporary files
    rm -f coverage_*.out
}

main() {
    # Parse arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            all|pipeline|discovery|scanning|workflow|scanner|performance|benchmarks|unit)
                TEST_TYPE="$1"
                shift
                ;;
            -v|--verbose)
                VERBOSE=true
                shift
                ;;
            -c|--coverage)
                COVERAGE=true
                shift
                ;;
            -t|--timeout)
                TIMEOUT="$2"
                shift 2
                ;;
            -b|--benchtime)
                BENCHTIME="$2"
                shift 2
                ;;
            -h|--help)
                print_usage
                exit 0
                ;;
            *)
                echo "Unknown option: $1"
                print_usage
                exit 1
                ;;
        esac
    done
    
    print_header
    
    # Change to project directory
    cd "$PROJECT_DIR"
    
    check_prerequisites
    
    # Trap to ensure cleanup
    trap cleanup EXIT
    
    local failed_tests=()
    
    case "$TEST_TYPE" in
        "all")
            print_info "Running all integration tests..."
            if ! run_test_command "All Integration Tests" "" ""; then
                failed_tests+=("All Integration Tests")
            fi
            ;;
        "pipeline")
            print_info "Running complete pipeline tests..."
            if ! run_test_command "Pipeline Tests" "TestCompleteAWSProviderPipeline" ""; then
                failed_tests+=("Pipeline Tests")
            fi
            ;;
        "discovery")
            print_info "Running discovery pipeline tests..."
            if ! run_test_command "Discovery Tests" "TestDiscoveryPipeline" ""; then
                failed_tests+=("Discovery Tests")
            fi
            ;;
        "scanning")
            print_info "Running scanning pipeline tests..."
            if ! run_test_command "Scanning Tests" "TestScanningPipeline" ""; then
                failed_tests+=("Scanning Tests")
            fi
            ;;
        "workflow")
            print_info "Running provider workflow tests..."
            if ! run_test_command "Workflow Tests" "TestCompleteProviderWorkflow" ""; then
                failed_tests+=("Workflow Tests")
            fi
            ;;
        "scanner")
            print_info "Running scanner component tests..."
            if ! run_test_command "Scanner Tests" "TestUnifiedScannerComponentIntegration" ""; then
                failed_tests+=("Scanner Tests")
            fi
            ;;
        "performance")
            print_info "Running performance tests..."
            if ! run_test_command "Performance Tests" "TestPerformanceAndReliability" ""; then
                failed_tests+=("Performance Tests")
            fi
            ;;
        "benchmarks")
            export RUN_BENCHMARKS=true
            print_info "Running all benchmarks..."
            if ! run_benchmark_command "All Benchmarks" "."; then
                failed_tests+=("All Benchmarks")
            fi
            ;;
        "unit")
            print_info "Running unit tests only..."
            if ! run_test_command "Unit Tests" "" "-short"; then
                failed_tests+=("Unit Tests")
            fi
            ;;
        *)
            print_error "Unknown test type: $TEST_TYPE"
            print_usage
            exit 1
            ;;
    esac
    
    generate_coverage_report
    
    # Summary
    print_section "Test Summary"
    if [[ ${#failed_tests[@]} -eq 0 ]]; then
        print_success "All tests passed!"
        exit 0
    else
        print_error "Some tests failed:"
        for test in "${failed_tests[@]}"; do
            echo -e "  ${RED}- $test${NC}"
        done
        exit 1
    fi
}

# Run main function
main "$@"