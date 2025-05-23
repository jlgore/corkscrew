# Makefile for Corkscrew Generator Plugin Architecture

# Variables
PLUGIN_DIR := ./plugins
PROTO_DIR := ./proto
EXAMPLES_DIR := ./examples/plugins
SERVICES := s3 ec2 rds dynamodb lambda
GO_VERSION := 1.21

# Default target
.PHONY: all
all: generate-proto build-cli build-generator build-test-client build-example-plugins

# Clean everything
.PHONY: clean
clean: clean-plugins clean-proto clean-binaries

.PHONY: clean-plugins
clean-plugins:
	rm -rf $(PLUGIN_DIR)/*

.PHONY: clean-proto
clean-proto:
	rm -f internal/proto/*.pb.go

.PHONY: clean-binaries
clean-binaries:
	rm -f cmd/plugin-test/plugin-test
	rm -f examples/plugins/*/plugin-*

# Generate protobuf code
.PHONY: generate-proto
generate-proto:
	@echo "Generating protobuf code..."
	@cd internal/proto && go generate
	@echo "Protobuf generation complete"

# Initialize Go modules
.PHONY: init
init:
	@echo "Initializing Go modules..."
	go mod tidy
	@echo "Go modules initialized"

# Build main CLI application
.PHONY: build-cli
build-cli: generate-proto
	@echo "Building main CLI application..."
	cd cmd/corkscrew && go build -o corkscrew .
	@echo "CLI application built: cmd/corkscrew/corkscrew"

# Build plugin generator
.PHONY: build-generator
build-generator: generate-proto
	@echo "Building plugin generator..."
	cd cmd/generator && go build -o generator .
	@echo "Plugin generator built: cmd/generator/generator"

# Build test client
.PHONY: build-test-client
build-test-client: generate-proto
	@echo "Building plugin test client..."
	cd cmd/plugin-test && go build -o plugin-test .
	@echo "Test client built: cmd/plugin-test/plugin-test"

# Build example S3 plugin
.PHONY: build-s3-plugin
build-s3-plugin: generate-proto
	@echo "Building S3 plugin..."
	@mkdir -p $(PLUGIN_DIR)
	cd $(EXAMPLES_DIR)/s3 && go build -o ../../../$(PLUGIN_DIR)/corkscrew-s3 .
	@echo "S3 plugin built: $(PLUGIN_DIR)/corkscrew-s3"

# Build all example plugins
.PHONY: build-example-plugins
build-example-plugins: build-s3-plugin

# Test the S3 plugin (requires AWS credentials)
.PHONY: test-s3-plugin
test-s3-plugin: build-s3-plugin build-test-client
	@echo "Testing S3 plugin..."
	./cmd/plugin-test/plugin-test --service s3 --region us-east-1 --info

# Test plugin loading without AWS calls
.PHONY: test-plugin-loading
test-plugin-loading: build-example-plugins build-test-client
	@echo "Testing plugin loading..."
	./cmd/plugin-test/plugin-test --list

# Run all tests
.PHONY: test
test: generate-proto
	@echo "Running Go tests..."
	go test ./...

# Format code
.PHONY: fmt
fmt:
	@echo "Formatting Go code..."
	go fmt ./...

# Lint code
.PHONY: lint
lint:
	@echo "Linting Go code..."
	golangci-lint run

# Install development dependencies
.PHONY: install-deps
install-deps:
	@echo "Installing development dependencies..."
	# Install protobuf compiler (platform specific)
	@if command -v brew >/dev/null 2>&1; then \
		echo "Installing protobuf via Homebrew..."; \
		brew install protobuf; \
	elif command -v apt-get >/dev/null 2>&1; then \
		echo "Installing protobuf via apt..."; \
		sudo apt-get update && sudo apt-get install -y protobuf-compiler; \
	else \
		echo "Please install protobuf compiler manually"; \
	fi
	
	# Install Go protobuf plugins
	@echo "Installing Go protobuf plugins..."
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	
	# Install linter
	@echo "Installing golangci-lint..."
	go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Development setup
.PHONY: setup
setup: install-deps init generate-proto
	@echo "Development environment setup complete!"

# Create a new plugin template
.PHONY: create-plugin-%
create-plugin-%:
	@echo "Creating plugin template for $*..."
	@mkdir -p $(EXAMPLES_DIR)/$*
	@echo "package main" > $(EXAMPLES_DIR)/$*/main.go
	@echo "" >> $(EXAMPLES_DIR)/$*/main.go
	@echo "// TODO: Implement $* plugin" >> $(EXAMPLES_DIR)/$*/main.go
	@echo "Plugin template created: $(EXAMPLES_DIR)/$*/main.go"

# Build a specific plugin from examples
.PHONY: build-plugin-%
build-plugin-%: generate-proto
	@echo "Building plugin for $*..."
	@mkdir -p $(PLUGIN_DIR)
	@if [ -d "$(EXAMPLES_DIR)/$*" ]; then \
		cd $(EXAMPLES_DIR)/$* && go build -o ../../../$(PLUGIN_DIR)/corkscrew-$* .; \
		echo "Plugin built: $(PLUGIN_DIR)/corkscrew-$*"; \
	else \
		echo "Plugin directory $(EXAMPLES_DIR)/$* does not exist"; \
		echo "Use 'make create-plugin-$*' to create a template first"; \
		exit 1; \
	fi

# Show help
.PHONY: help
help:
	@echo "Corkscrew Generator Plugin Architecture"
	@echo ""
	@echo "Available targets:"
	@echo "  setup                 - Set up development environment"
	@echo "  all                   - Build everything"
	@echo "  clean                 - Clean all generated files"
	@echo ""
	@echo "Code generation:"
	@echo "  generate-proto        - Generate protobuf Go code"
	@echo ""
	@echo "Building:"
	@echo "  build-cli             - Build the main CLI application"
	@echo "  build-generator       - Build the plugin generator"
	@echo "  build-test-client     - Build the plugin test client"
	@echo "  build-example-plugins - Build all example plugins"
	@echo "  build-plugin-<name>   - Build specific plugin"
	@echo "  build-s3-plugin       - Build S3 plugin specifically"
	@echo ""
	@echo "Plugin development:"
	@echo "  create-plugin-<name>  - Create new plugin template"
	@echo ""
	@echo "Testing:"
	@echo "  test                  - Run Go tests"
	@echo "  test-s3-plugin        - Test S3 plugin (requires AWS creds)"
	@echo "  test-plugin-loading   - Test plugin loading mechanism"
	@echo ""
	@echo "Code quality:"
	@echo "  fmt                   - Format Go code"
	@echo "  lint                  - Lint Go code"
	@echo ""
	@echo "Dependencies:"
	@echo "  install-deps          - Install development dependencies"
	@echo "  init                  - Initialize Go modules"

# Show project status
.PHONY: status
status:
	@echo "Project Status:"
	@echo "==============="
	@echo ""
	@echo "Go version: $(shell go version)"
	@echo "Module: $(shell head -1 go.mod)"
	@echo ""
	@echo "Generated files:"
	@echo "  Protobuf: $(shell ls internal/proto/*.pb.go 2>/dev/null | wc -l) files"
	@echo ""
	@echo "Built plugins:"
	@echo "  $(shell ls $(PLUGIN_DIR)/corkscrew-* 2>/dev/null | wc -l) plugins in $(PLUGIN_DIR)/"
	@if [ -d "$(PLUGIN_DIR)" ]; then \
		ls $(PLUGIN_DIR)/corkscrew-* 2>/dev/null | sed 's/.*corkscrew-/  - /' || echo "  (none)"; \
	fi
	@echo ""
	@echo "Example plugins:"
	@echo "  $(shell find $(EXAMPLES_DIR) -name main.go 2>/dev/null | wc -l) examples in $(EXAMPLES_DIR)/"
	@find $(EXAMPLES_DIR) -name main.go 2>/dev/null | sed 's|.*/\([^/]*\)/main.go|  - \1|' || echo "  (none)"
