# AWS Provider Build Pipeline Makefile
# This Makefile orchestrates the build-time pipeline for the AWS provider

# Configuration
PLUGIN_NAME := aws-provider
BUILD_DIR := build
GENERATED_DIR := generated
SCHEMA_DIR := $(GENERATED_DIR)/schemas
SCANNER_DIR := $(GENERATED_DIR)/scanners
SDK_VERSION_FILE := .aws-sdk-version
GO_MODULE := github.com/jlgore/corkscrew/plugins/aws-provider

# AWS SDK Configuration
AWS_SDK_VERSION := $(shell go list -m -f '{{.Version}}' github.com/aws/aws-sdk-go-v2 2>/dev/null || echo "latest")
AWS_SDK_MODULES := github.com/aws/aws-sdk-go-v2 \
                   github.com/aws/aws-sdk-go-v2/config \
                   github.com/aws/aws-sdk-go-v2/service/ec2 \
                   github.com/aws/aws-sdk-go-v2/service/s3 \
                   github.com/aws/aws-sdk-go-v2/service/lambda \
                   github.com/aws/aws-sdk-go-v2/service/iam \
                   github.com/aws/aws-sdk-go-v2/service/rds \
                   github.com/aws/aws-sdk-go-v2/service/dynamodb

# Build Configuration
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)
CGO_ENABLED ?= 0
LDFLAGS := -ldflags "-s -w"

# Targets
.PHONY: all build clean generate analyze update-sdk check-sdk-version

# Default target
all: check-sdk-version generate build

# Check if SDK version has changed
check-sdk-version:
	@echo "Checking AWS SDK version..."
	@if [ -f $(SDK_VERSION_FILE) ]; then \
		CURRENT_VERSION=$$(cat $(SDK_VERSION_FILE)); \
		if [ "$$CURRENT_VERSION" != "$(AWS_SDK_VERSION)" ]; then \
			echo "AWS SDK version changed from $$CURRENT_VERSION to $(AWS_SDK_VERSION)"; \
			$(MAKE) update-sdk; \
		else \
			echo "AWS SDK version unchanged: $(AWS_SDK_VERSION)"; \
		fi; \
	else \
		echo "No previous SDK version found, updating..."; \
		$(MAKE) update-sdk; \
	fi

# Update AWS SDK modules
update-sdk:
	@echo "Updating AWS SDK modules..."
	@for module in $(AWS_SDK_MODULES); do \
		echo "Updating $$module..."; \
		go get -u $$module@latest; \
	done
	@go mod tidy
	@echo "$(AWS_SDK_VERSION)" > $(SDK_VERSION_FILE)
	@touch .force-regenerate

# Create necessary directories
$(BUILD_DIR) $(GENERATED_DIR) $(SCHEMA_DIR) $(SCANNER_DIR):
	@mkdir -p $@

# Run the analyzer to discover services
analyze: $(GENERATED_DIR)
	@echo "Analyzing AWS SDK for service discovery..."
	@if [ -f .force-regenerate ] || [ ! -f $(GENERATED_DIR)/services.json ]; then \
		go run ./cmd/analyzer \
			-output $(GENERATED_DIR)/services.json \
			-sdk-path $$(go list -m -f '{{.Dir}}' github.com/aws/aws-sdk-go-v2) \
			-verbose; \
	else \
		echo "Using cached service analysis"; \
	fi

# Generate scanner code - DEPRECATED: Scanner generation is no longer supported
generate-scanners: analyze $(SCANNER_DIR)
	@echo "⚠️  WARNING: Scanner generation is deprecated"
	@echo "⚠️  Analysis files are preserved for UnifiedScanner configuration collection"
	@echo "⚠️  Use CloudProvider.ScanService() instead of generated scanner plugins"
	@echo "✅ Skipping scanner generation (deprecated)"

# Generate client factory code
generate-client-factory: analyze $(GENERATED_DIR)
	@echo "Generating client factory code..."
	@if [ -f .force-regenerate ] || [ ! -f ./client_registry_generated.go ]; then \
		go run ./cmd/client-factory-generator \
			-services $(GENERATED_DIR)/services.json \
			-output ./client_registry_generated.go \
			-verbose; \
	else \
		echo "Using cached client factory"; \
	fi

# Generate schema definitions
generate-schemas: analyze $(SCHEMA_DIR)
	@echo "Generating schema definitions..."
	@if [ -f .force-regenerate ] || [ ! -d $(SCHEMA_DIR) ] || [ -z "$$(ls -A $(SCHEMA_DIR))" ]; then \
		go run ./cmd/schema-generator \
			-services $(GENERATED_DIR)/services.json \
			-output-dir $(SCHEMA_DIR) \
			-format sql; \
	else \
		echo "Using cached schema definitions"; \
	fi

# Generate Go code (client factory + schemas)
generate-go: generate-client-factory generate-schemas
	@echo "Generating Go integration code..."
	@go generate ./...

# Main generate target
generate: generate-go
	@rm -f .force-regenerate
	@echo "Code generation complete"

# Build the provider plugin
build: $(BUILD_DIR)
	@echo "Building AWS provider plugin..."
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
	rm -f .force-regenerate

# Deep clean (including SDK version tracking)
deep-clean: clean
	@echo "Deep cleaning..."
	rm -f $(SDK_VERSION_FILE)

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

# Generate documentation
.PHONY: docs

docs:
	@echo "Generating documentation..."
	@mkdir -p docs
	@go run ./cmd/doc-generator \
		-services $(GENERATED_DIR)/services.json \
		-output docs/

# Debug targets
.PHONY: debug-vars list-services

debug-vars:
	@echo "Build Configuration:"
	@echo "  PLUGIN_NAME: $(PLUGIN_NAME)"
	@echo "  BUILD_DIR: $(BUILD_DIR)"
	@echo "  GENERATED_DIR: $(GENERATED_DIR)"
	@echo "  AWS_SDK_VERSION: $(AWS_SDK_VERSION)"
	@echo "  GOOS: $(GOOS)"
	@echo "  GOARCH: $(GOARCH)"

list-services: analyze
	@echo "Discovered AWS Services:"
	@jq -r '.services[].name' $(GENERATED_DIR)/services.json 2>/dev/null || echo "No services found"

# Performance profiling
.PHONY: profile benchmark

profile: build
	@echo "Running CPU profile..."
	@mkdir -p profiles
	go test -cpuprofile=profiles/cpu.prof -memprofile=profiles/mem.prof -bench=.

benchmark:
	@echo "Running benchmarks..."
	go test -bench=. -benchmem ./...

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
	@echo "AWS Provider Build Pipeline"
	@echo ""
	@echo "Usage: make [target]"
	@echo ""
	@echo "Main targets:"
	@echo "  all              - Check SDK version, generate code, and build (default)"
	@echo "  build            - Build the provider plugin"
	@echo "  generate         - Generate all code (scanners, schemas, Go)"
	@echo "  test             - Run unit tests"
	@echo "  clean            - Clean build artifacts"
	@echo ""
	@echo "Development targets:"
	@echo "  dev              - Run in development mode with hot reload"
	@echo "  watch            - Watch files and rebuild on changes"
	@echo "  lint             - Run linters"
	@echo "  fmt              - Format code"
	@echo ""
	@echo "SDK management:"
	@echo "  update-sdk       - Update AWS SDK to latest version"
	@echo "  check-sdk-version - Check if SDK version changed"
	@echo ""
	@echo "Debug targets:"
	@echo "  debug-vars       - Show build configuration"
	@echo "  list-services    - List discovered AWS services"
	@echo ""
	@echo "For more information, see README.md"