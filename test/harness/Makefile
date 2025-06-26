# Corkscrew Test Framework Makefile

.PHONY: help build test test-all test-matrix clean install-deps setup check-env

# Default target
help:
	@echo "Corkscrew Test Framework"
	@echo "========================"
	@echo ""
	@echo "Available targets:"
	@echo "  build         - Build the test runner CLI"
	@echo "  test          - Run a single test scenario"
	@echo "  test-all      - Run all test scenarios sequentially"
	@echo "  test-matrix   - Run matrix tests in parallel"
	@echo "  benchmark     - Run performance benchmarks"
	@echo "  setup         - Setup test environment"
	@echo "  clean         - Clean up test artifacts"
	@echo "  check-env     - Check environment configuration"
	@echo ""
	@echo "Configuration variables:"
	@echo "  SCENARIO      - Test scenario (default: simple-s3)"
	@echo "  PROVIDER      - Cloud provider (default: aws)"
	@echo "  REGION        - AWS region (default: us-east-1)"
	@echo "  TIMEOUT       - Test timeout (default: 15m)"
	@echo "  KEEP_ON_FAIL  - Keep resources on failure (default: false)"
	@echo ""
	@echo "Examples:"
	@echo "  make test SCENARIO=network-stack REGION=us-west-2"
	@echo "  make test-all PROVIDER=aws"
	@echo "  make test-matrix"

# Configuration variables
SCENARIO ?= simple-s3
PROVIDER ?= aws
REGION ?= us-east-1
TIMEOUT ?= 15m
KEEP_ON_FAIL ?= false
CORKSCREW_PATH ?= ../../corkscrew

# Build the CLI runner
build:
	@echo "Building test runner CLI..."
	cd cmd/runner && go build -o runner main.go
	@echo "✅ CLI runner built: cmd/runner/runner"

# Install dependencies
install-deps:
	@echo "Installing Go dependencies..."
	go mod download
	go mod tidy
	@echo "✅ Dependencies installed"

# Setup test environment
setup: install-deps check-env
	@echo "Setting up test environment..."
	@if [ ! -f "$(CORKSCREW_PATH)" ]; then \
		echo "❌ Corkscrew binary not found at $(CORKSCREW_PATH)"; \
		echo "Please build Corkscrew first or set CORKSCREW_PATH"; \
		exit 1; \
	fi
	@echo "✅ Test environment ready"

# Check environment configuration
check-env:
	@echo "Checking environment configuration..."
	@echo "Provider: $(PROVIDER)"
	@echo "Region: $(REGION)"
	@echo "Corkscrew Path: $(CORKSCREW_PATH)"
	@if [ -z "$$AWS_REGION" ] && [ -z "$$AWS_DEFAULT_REGION" ]; then \
		echo "⚠️  AWS_REGION not set, using: $(REGION)"; \
	fi
	@if [ -z "$$PULUMI_ACCESS_TOKEN" ]; then \
		echo "⚠️  PULUMI_ACCESS_TOKEN not set, using local state"; \
	fi
	@echo "✅ Environment check complete"

# Run a single test scenario
test: setup
	@echo "Running single test scenario: $(SCENARIO)"
	go test -v \
		-scenario=$(SCENARIO) \
		-provider=$(PROVIDER) \
		-region=$(REGION) \
		-timeout=$(TIMEOUT) \
		-keepOnFail=$(KEEP_ON_FAIL) \
		-corkscrew=$(CORKSCREW_PATH)

# Run all test scenarios sequentially
test-all: setup
	@echo "Running all test scenarios..."
	@for scenario in simple-s3 network-stack compute-stack security-stack storage-stack; do \
		echo ""; \
		echo "🧪 Testing scenario: $$scenario"; \
		echo "====================================="; \
		go test -v \
			-scenario=$$scenario \
			-provider=$(PROVIDER) \
			-region=$(REGION) \
			-timeout=$(TIMEOUT) \
			-keepOnFail=$(KEEP_ON_FAIL) \
			-corkscrew=$(CORKSCREW_PATH) || exit 1; \
	done
	@echo "✅ All scenarios completed successfully"

# Run matrix tests in parallel
test-matrix: setup
	@echo "Running matrix tests in parallel..."
	go test -v -parallel=true \
		-provider=$(PROVIDER) \
		-timeout=$(TIMEOUT) \
		-corkscrew=$(CORKSCREW_PATH)

# Run specific test functions
test-validation:
	@echo "Running scenario validation tests..."
	go test -v -run TestScenarioValidation

test-regions:
	@echo "Running region parameterization tests..."
	go test -v -run TestRegionParameterization

test-pagination:
	@echo "Running pagination tests..."
	go test -v -run TestResourcePagination

test-encryption:
	@echo "Running encryption tests..."
	go test -v -run TestEncryptedResources

# Run performance benchmarks
benchmark: setup
	@echo "Running performance benchmarks..."
	go test -bench=BenchmarkTestExecution \
		-benchmem \
		-scenario=simple-s3 \
		-provider=$(PROVIDER) \
		-region=$(REGION) \
		-corkscrew=$(CORKSCREW_PATH)

# Profile memory usage
profile-mem: setup
	@echo "Profiling memory usage..."
	go test -memprofile=mem.prof \
		-scenario=storage-stack \
		-provider=$(PROVIDER) \
		-region=$(REGION) \
		-corkscrew=$(CORKSCREW_PATH)
	@echo "Memory profile saved to mem.prof"
	@echo "View with: go tool pprof mem.prof"

# Profile CPU usage
profile-cpu: setup
	@echo "Profiling CPU usage..."
	go test -cpuprofile=cpu.prof \
		-scenario=compute-stack \
		-provider=$(PROVIDER) \
		-region=$(REGION) \
		-corkscrew=$(CORKSCREW_PATH)
	@echo "CPU profile saved to cpu.prof"
	@echo "View with: go tool pprof cpu.prof"

# Run CLI tests
test-cli: build
	@echo "Testing CLI runner..."
	./cmd/runner/runner -list-scenarios
	@echo ""
	./cmd/runner/runner \
		-scenario=$(SCENARIO) \
		-region=$(REGION) \
		-verbose \
		-corkscrew=$(CORKSCREW_PATH)

# List available scenarios
list-scenarios: build
	@echo "Available test scenarios:"
	./cmd/runner/runner -list-scenarios

# Clean up test artifacts
clean:
	@echo "Cleaning up test artifacts..."
	rm -f test_*.db*
	rm -f *.prof
	rm -f test_results_*.json
	rm -f matrix_report_*.json
	rm -f cmd/runner/runner
	@echo "✅ Cleanup complete"

# Development helpers
dev-setup: setup
	@echo "Setting up development environment..."
	go install golang.org/x/tools/cmd/goimports@latest
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	@echo "✅ Development tools installed"

# Run linting
lint:
	@echo "Running linter..."
	golangci-lint run ./...

# Format code
fmt:
	@echo "Formatting code..."
	goimports -w .
	go fmt ./...

# Run tests with coverage
test-coverage: setup
	@echo "Running tests with coverage..."
	go test -v -coverprofile=coverage.out \
		-scenario=$(SCENARIO) \
		-provider=$(PROVIDER) \
		-region=$(REGION) \
		-corkscrew=$(CORKSCREW_PATH)
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report saved to coverage.html"

# Quick test (simple scenario only)
quick-test: setup
	@echo "Running quick test..."
	go test -v \
		-scenario=simple-s3 \
		-provider=$(PROVIDER) \
		-region=$(REGION) \
		-timeout=10m \
		-corkscrew=$(CORKSCREW_PATH)

# Test with short timeout for CI
ci-test: setup
	@echo "Running CI tests..."
	go test -v -short \
		-scenario=simple-s3 \
		-provider=$(PROVIDER) \
		-region=$(REGION) \
		-timeout=8m \
		-corkscrew=$(CORKSCREW_PATH)

# Generate test report
report: build
	@echo "Generating test report..."
	./cmd/runner/runner \
		-scenario=$(SCENARIO) \
		-region=$(REGION) \
		-output=json \
		-corkscrew=$(CORKSCREW_PATH) > test_report.json
	@echo "Test report saved to test_report.json"

# Debug mode (keep resources on failure)
debug: setup
	@echo "Running in debug mode (keeping resources on failure)..."
	go test -v \
		-scenario=$(SCENARIO) \
		-provider=$(PROVIDER) \
		-region=$(REGION) \
		-timeout=$(TIMEOUT) \
		-keepOnFail=true \
		-corkscrew=$(CORKSCREW_PATH)

# Show help for common issues
troubleshoot:
	@echo "Troubleshooting Guide"
	@echo "===================="
	@echo ""
	@echo "Common Issues:"
	@echo ""
	@echo "1. Pulumi token missing:"
	@echo "   export PULUMI_ACCESS_TOKEN=your-token"
	@echo "   # Or use local state: pulumi login --local"
	@echo ""
	@echo "2. AWS credentials not configured:"
	@echo "   aws configure"
	@echo "   # Or set environment variables:"
	@echo "   export AWS_ACCESS_KEY_ID=your-key"
	@echo "   export AWS_SECRET_ACCESS_KEY=your-secret"
	@echo ""
	@echo "3. Corkscrew binary not found:"
	@echo "   make -C ../.. build"
	@echo "   # Or set custom path:"
	@echo "   make test CORKSCREW_PATH=/path/to/corkscrew"
	@echo ""
	@echo "4. Resource cleanup failed:"
	@echo "   pulumi stack ls"
	@echo "   pulumi destroy -s stack-name"
	@echo ""
	@echo "5. Database locked:"
	@echo "   make clean"