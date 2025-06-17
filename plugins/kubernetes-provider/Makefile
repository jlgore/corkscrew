# Kubernetes Provider Build Pipeline Makefile

# Configuration
PLUGIN_NAME := kubernetes-provider
BUILD_DIR := build
GENERATED_DIR := generated
GO_MODULE := github.com/jlgore/corkscrew/plugins/kubernetes-provider

# Build Configuration
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
CGO_ENABLED ?= 1
LDFLAGS := -ldflags "-s -w"

# Targets
.PHONY: all build clean test generate test-change-tracking demo-change-tracking benchmark-change-tracking

# Default target
all: generate build

# Create necessary directories
$(BUILD_DIR) $(GENERATED_DIR):
	@mkdir -p $@

# Generate code
generate: $(GENERATED_DIR)
	@echo "Generating Kubernetes provider code..."
	@go generate ./...

# Build the provider plugin
build: $(BUILD_DIR)
	@echo "Building Kubernetes provider plugin..."
	CGO_ENABLED=$(CGO_ENABLED) GOOS=$(GOOS) GOARCH=$(GOARCH) \
		go build $(LDFLAGS) \
		-o $(BUILD_DIR)/$(PLUGIN_NAME) \
		.
	@echo "Build complete: $(BUILD_DIR)/$(PLUGIN_NAME)"

# Run tests
test:
	@echo "Running tests..."
	go test -v ./...

# Run integration tests
integration-test: build
	@echo "Running integration tests..."
	go test -v -tags=integration ./tests/...

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -rf $(BUILD_DIR) $(GENERATED_DIR)

# Development helpers
.PHONY: dev watch lint fmt

# Run in development mode with file watching
dev:
	@echo "Starting development mode..."
	@which air > /dev/null || (echo "Installing air..." && go install github.com/cosmtrek/air@latest)
	air

# Watch for changes and rebuild
watch:
	@echo "Watching for changes..."
	@which reflex > /dev/null || (echo "Installing reflex..." && go install github.com/cespare/reflex@latest)
	reflex -r '\.(go|json)$$' -s -- sh -c '$(MAKE) generate build'

# Run linters
lint:
	@echo "Running linters..."
	@which golangci-lint > /dev/null || (echo "Installing golangci-lint..." && go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest)
	golangci-lint run

# Format code
fmt:
	@echo "Formatting code..."
	go fmt ./...
	gofmt -s -w .

# Debug targets
.PHONY: debug-vars

debug-vars:
	@echo "Build Configuration:"
	@echo "  PLUGIN_NAME: $(PLUGIN_NAME)"
	@echo "  BUILD_DIR: $(BUILD_DIR)"
	@echo "  GENERATED_DIR: $(GENERATED_DIR)"
	@echo "  GOOS: $(GOOS)"
	@echo "  GOARCH: $(GOARCH)"

# Performance profiling
.PHONY: profile benchmark

profile: build
	@echo "Running CPU profile..."
	@mkdir -p profiles
	go test -cpuprofile=profiles/cpu.prof -memprofile=profiles/mem.prof -bench=.

benchmark:
	@echo "Running benchmarks..."
	go test -bench=. -benchmem ./...

# Change tracking targets
test-change-tracking:
	@echo "🔍 Testing change tracking functionality..."
	go test -v -run="TestChangeTracking.*|Test.*ChangeTracker.*" ./...

demo-change-tracking:
	@echo "🎯 Demonstrating change tracking features..."
	@echo "Run with: make demo-change-tracking KUBECONFIG=/path/to/kubeconfig NAMESPACE=default"
	@if [ -z "$(KUBECONFIG)" ]; then \
		echo "⚠️  Warning: KUBECONFIG not set, using default ~/.kube/config"; \
		KUBECONFIG=~/.kube/config; \
	fi
	@if [ -z "$(NAMESPACE)" ]; then \
		NAMESPACE=default; \
	fi
	@echo "Monitoring Kubernetes cluster for changes in namespace $(NAMESPACE)..."
	@go run . -demo-change-tracking -kubeconfig=$(KUBECONFIG) -namespace=$(NAMESPACE)

benchmark-change-tracking:
	@echo "⚡ Benchmarking change tracking performance..."
	go test -bench=BenchmarkChangeEvent.* -benchmem ./...

# Kubernetes-specific targets
.PHONY: check-cluster test-rbac

check-cluster:
	@echo "🔍 Checking Kubernetes cluster access..."
	@kubectl cluster-info || (echo "❌ Cannot access Kubernetes cluster. Please check your kubeconfig." && exit 1)
	@echo "✅ Kubernetes cluster is accessible"

test-rbac:
	@echo "🔐 Testing RBAC permissions..."
	@kubectl auth can-i get pods --all-namespaces
	@kubectl auth can-i watch pods --all-namespaces
	@kubectl auth can-i get events --all-namespaces

# Docker support
.PHONY: docker-build docker-test

docker-build:
	@echo "Building Docker image..."
	docker build -t $(PLUGIN_NAME):latest .

docker-test: docker-build
	@echo "Running tests in Docker..."
	docker run --rm $(PLUGIN_NAME):latest make test

# Help target
help:
	@echo "Kubernetes Provider Build Pipeline"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Main targets:"
	@echo "  all              - Generate code and build (default)"
	@echo "  build            - Build the provider plugin"
	@echo "  generate         - Generate all code"
	@echo "  test             - Run unit tests"
	@echo "  clean            - Clean build artifacts"
	@echo ""
	@echo "Development targets:"
	@echo "  dev              - Run in development mode with hot reload"
	@echo "  watch            - Watch files and rebuild on changes"
	@echo "  lint             - Run linters"
	@echo "  fmt              - Format code"
	@echo ""
	@echo "Kubernetes targets:"
	@echo "  check-cluster    - Check Kubernetes cluster access"
	@echo "  test-rbac        - Test RBAC permissions"
	@echo ""
	@echo "Debug targets:"
	@echo "  debug-vars       - Show build configuration"
	@echo ""
	@echo "Change tracking targets:"
	@echo "  test-change-tracking     - Test change tracking functionality"
	@echo "  demo-change-tracking     - Demo change tracking features (optional KUBECONFIG and NAMESPACE)"
	@echo "  benchmark-change-tracking - Benchmark change tracking performance"
	@echo ""
	@echo "For more information, see README.md"