# Makefile for Corkscrew Cloud Resource Scanner
# Reset for proper DISCOVER → SCAN pattern implementation

# Variables
PLUGIN_DIR := ./plugins
PROTO_DIR := ./proto
INTERNAL_DIR := ./internal
CMD_DIR := ./cmd
GO_VERSION := 1.21
AWS_REGION := us-east-1

# Build directories
BUILD_DIR := ./build
BIN_DIR := $(BUILD_DIR)/bin
TEMP_DIR := $(BUILD_DIR)/temp
CACHE_DIR := $(BUILD_DIR)/cache

# Default target
.PHONY: all
all: clean setup build test

# =============================================================================
# SETUP AND CLEANUP
# =============================================================================

.PHONY: setup
setup: create-dirs install-deps generate-proto
	@echo "✅ Development environment setup complete!"

.PHONY: create-dirs
create-dirs:
	@echo "📁 Creating build directories..."
	@mkdir -p $(BIN_DIR) $(TEMP_DIR) $(CACHE_DIR) $(PLUGIN_DIR)

.PHONY: clean
clean: clean-build clean-plugins clean-proto clean-generated
	@echo "🧹 Cleanup complete!"

.PHONY: clean-build
clean-build:
	@echo "🧹 Cleaning build artifacts..."
	@rm -rf $(BUILD_DIR)

.PHONY: clean-plugins
clean-plugins:
	@echo "🧹 Cleaning plugins..."
	@rm -rf $(PLUGIN_DIR)/*

.PHONY: clean-proto
clean-proto:
	@echo "🧹 Cleaning protobuf generated files..."
	@rm -f $(INTERNAL_DIR)/proto/*.pb.go

.PHONY: clean-generated
clean-generated:
	@echo "🧹 Cleaning generated code..."
	@rm -rf ./generated

# =============================================================================
# DEPENDENCIES AND PROTOBUF
# =============================================================================

.PHONY: install-deps
install-deps:
	@echo "📦 Installing development dependencies..."
	@if command -v apt-get >/dev/null 2>&1; then \
		echo "Installing protobuf via apt..."; \
		sudo apt-get update && sudo apt-get install -y protobuf-compiler; \
	else \
		echo "Please install protobuf compiler manually"; \
	fi
	
	@echo "Installing Go protobuf plugins..."
	@go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	@go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
	
	@echo "Installing linter..."
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
	
	@echo "Initializing Go modules..."
	@go mod tidy

.PHONY: generate-proto
generate-proto:
	@echo "🔧 Generating protobuf code..."
	@cd $(INTERNAL_DIR)/proto && go generate
	@echo "✅ Protobuf generation complete"

# =============================================================================
# BUILDING
# =============================================================================

.PHONY: build
build: build-cli build-aws-plugin build-azure-plugin build-tools
	@echo "🔨 Build complete!"

.PHONY: build-cli
build-cli: generate-proto create-dirs
	@echo "🔨 Building main CLI application..."
	@cd $(CMD_DIR)/corkscrew && go build -o ../../$(BIN_DIR)/corkscrew .
	@echo "✅ CLI built: $(BIN_DIR)/corkscrew"

.PHONY: build-aws-plugin
build-aws-plugin: generate-proto create-dirs
	@echo "🔨 Building AWS plugin..."
	@if [ -d "plugins/aws-provider" ]; then \
		cd plugins/aws-provider && go build -o ../../$(BIN_DIR)/corkscrew-aws .; \
		echo "✅ AWS plugin built: $(BIN_DIR)/corkscrew-aws"; \
	else \
		echo "⚠️  AWS plugin directory not found, skipping..."; \
	fi

.PHONY: build-azure-plugin
build-azure-plugin: generate-proto create-dirs
	@echo "🔨 Building Azure plugin..."
	@if [ -d "plugins/azure-provider" ]; then \
		cd plugins/azure-provider && go mod tidy && go build -o ../../plugins/build/corkscrew-azure .; \
		chmod +x ../../plugins/build/corkscrew-azure; \
		echo "✅ Azure plugin built: plugins/build/corkscrew-azure"; \
	else \
		echo "⚠️  Azure plugin directory not found, skipping..."; \
	fi

.PHONY: build-tools
build-tools: generate-proto create-dirs
	@echo "🔨 Building development tools..."
	@if [ -d "$(CMD_DIR)/generator" ]; then \
		cd $(CMD_DIR)/generator && go build -o ../../$(BIN_DIR)/generator .; \
		echo "✅ Generator built: $(BIN_DIR)/generator"; \
	fi
	@if [ -d "$(PLUGIN_DIR)/azure-provider/cmd/analyze-azure-sdk" ]; then \
		cd $(PLUGIN_DIR)/azure-provider/cmd/analyze-azure-sdk && go build -o ../../../../$(BIN_DIR)/analyze-azure-sdk .; \
		echo "✅ Azure SDK analyzer built: $(BIN_DIR)/analyze-azure-sdk"; \
	fi

# =============================================================================
# INSTALLATION
# =============================================================================

.PHONY: install
install: install-cli install-plugins
	@echo "✅ Installation complete!"

.PHONY: install-cli
install-cli: build-cli
	@echo "📦 Installing Corkscrew CLI..."
	@mkdir -p $(HOME)/.corkscrew/bin
	@cp $(BIN_DIR)/corkscrew $(HOME)/.corkscrew/bin/
	@chmod +x $(HOME)/.corkscrew/bin/corkscrew
	@echo "✅ CLI installed to $(HOME)/.corkscrew/bin/corkscrew"
	@echo "💡 Add $(HOME)/.corkscrew/bin to your PATH to use 'corkscrew' from anywhere"

.PHONY: install-plugins
install-plugins: build-aws-plugin build-azure-plugin
	@echo "📦 Installing plugins..."
	@mkdir -p $(HOME)/.corkscrew/bin/plugin
	@if [ -f "$(BIN_DIR)/corkscrew-aws" ]; then \
		cp $(BIN_DIR)/corkscrew-aws $(HOME)/.corkscrew/bin/plugin/; \
		chmod +x $(HOME)/.corkscrew/bin/plugin/corkscrew-aws; \
		echo "✅ AWS plugin installed"; \
	fi
	@if [ -f "plugins/build/corkscrew-azure" ]; then \
		cp plugins/build/corkscrew-azure $(HOME)/.corkscrew/bin/plugin/; \
		chmod +x $(HOME)/.corkscrew/bin/plugin/corkscrew-azure; \
		echo "✅ Azure plugin installed"; \
	fi

.PHONY: uninstall
uninstall:
	@echo "🗑️  Uninstalling Corkscrew..."
	@rm -rf $(HOME)/.corkscrew
	@echo "✅ Corkscrew uninstalled"

# =============================================================================
# TESTING INFRASTRUCTURE
# =============================================================================

.PHONY: test
test: test-unit test-scanner test-integration
	@echo "✅ All tests complete!"

.PHONY: test-unit
test-unit: generate-proto
	@echo "🧪 Running unit tests..."
	@go test -v ./internal/... -short
	@echo "✅ Unit tests passed"

.PHONY: test-scanner
test-scanner: generate-proto
	@echo "🧪 Testing AWS resource lister..."
	@go test -v ./internal/scanner -run TestAWSListResource
	@echo "✅ Scanner tests passed"

.PHONY: test-integration
test-integration: build
	@echo "🧪 Running integration tests..."
	@go test -v ./internal/... -tags=integration
	@echo "✅ Integration tests passed"

.PHONY: test-plugins
test-plugins: build-aws-plugin
	@echo "🧪 Testing plugin loading..."
	@if [ -f "$(BIN_DIR)/corkscrew-aws" ]; then \
		echo "Testing AWS plugin loading..."; \
		$(BIN_DIR)/corkscrew scan --services s3 --region $(AWS_REGION) --dry-run --verbose; \
	else \
		echo "⚠️  AWS plugin not found, skipping plugin tests"; \
	fi

.PHONY: test-discover
test-discover: build
	@echo "🔍 Testing service discovery..."
	@$(BIN_DIR)/corkscrew discover --verbose

.PHONY: test-scan-dry
test-scan-dry: build
	@echo "🔍 Testing dry-run scanning..."
	@$(BIN_DIR)/corkscrew scan --services s3,ec2 --region $(AWS_REGION) --dry-run --verbose

.PHONY: test-list-dry
test-list-dry: build
	@echo "🔍 Testing dry-run resource listing..."
	@$(BIN_DIR)/corkscrew list --services s3,ec2,lambda --region $(AWS_REGION) --dry-run --verbose

.PHONY: test-validate
test-validate: build
	@echo "🔍 Testing operation validation..."
	@$(BIN_DIR)/corkscrew validate --services s3,ec2 --verbose

# =============================================================================
# SCANNING OPERATIONS (DISCOVER → SCAN PATTERN)
# =============================================================================

.PHONY: scan
scan: scan-discover scan-list scan-describe
	@echo "🔍 Complete scan finished!"

.PHONY: scan-discover
scan-discover: build
	@echo "🔍 Phase 1: Discovering AWS services..."
	@$(BIN_DIR)/corkscrew discover --output $(BUILD_DIR)/discovered-services.json --verbose

.PHONY: scan-list
scan-list: build
	@echo "🔍 Phase 2: Listing resources (parameter-free operations)..."
	@$(BIN_DIR)/corkscrew list --services s3,ec2,lambda --region $(AWS_REGION) --output $(BUILD_DIR)/resource-refs.json --verbose

.PHONY: scan-describe
scan-describe: build
	@echo "🔍 Phase 3: Describing resources (parameterized operations)..."
	@$(BIN_DIR)/corkscrew describe --input $(BUILD_DIR)/resource-refs.json --output $(BUILD_DIR)/full-resources.json --verbose

.PHONY: scan-s3
scan-s3: build
	@echo "🔍 Scanning S3 resources..."
	@$(BIN_DIR)/corkscrew scan --services s3 --region $(AWS_REGION) --output $(BUILD_DIR)/s3-resources.json --verbose

.PHONY: scan-ec2
scan-ec2: build
	@echo "🔍 Scanning EC2 resources..."
	@$(BIN_DIR)/corkscrew scan --services ec2 --region $(AWS_REGION) --output $(BUILD_DIR)/ec2-resources.json --verbose

.PHONY: scan-all
scan-all: build
	@echo "🔍 Scanning all supported services..."
	@$(BIN_DIR)/corkscrew scan --services s3,ec2,lambda,rds,dynamodb --region $(AWS_REGION) --output $(BUILD_DIR)/all-resources.json --verbose

# =============================================================================
# DRY RUN TESTING (Safe testing without AWS calls)
# =============================================================================

.PHONY: test-dry-run
test-dry-run: test-list-dry test-scan-dry test-validate
	@echo "✅ All dry-run tests complete!"

.PHONY: scan-dry-s3
scan-dry-s3: build
	@echo "🔍 Dry-run scanning S3..."
	@$(BIN_DIR)/corkscrew scan --services s3 --region $(AWS_REGION) --dry-run --verbose

.PHONY: scan-dry-ec2
scan-dry-ec2: build
	@echo "🔍 Dry-run scanning EC2..."
	@$(BIN_DIR)/corkscrew scan --services ec2 --region $(AWS_REGION) --dry-run --verbose

.PHONY: scan-dry-all
scan-dry-all: build
	@echo "🔍 Dry-run scanning all services..."
	@$(BIN_DIR)/corkscrew scan --services s3,ec2,lambda,rds,dynamodb --region $(AWS_REGION) --dry-run --verbose

# =============================================================================
# DEVELOPMENT AND DEBUGGING
# =============================================================================

.PHONY: dev-setup
dev-setup: setup
	@echo "🛠️  Setting up development environment..."
	@echo "Creating sample configuration..."
	@mkdir -p ~/.corkscrew
	@echo "region: $(AWS_REGION)" > ~/.corkscrew/config.yaml
	@echo "cache_dir: $(CACHE_DIR)" >> ~/.corkscrew/config.yaml
	@echo "plugin_dir: $(PLUGIN_DIR)" >> ~/.corkscrew/config.yaml

.PHONY: debug-discovery
debug-discovery: build
	@echo "🐛 Debugging service discovery..."
	@$(BIN_DIR)/corkscrew discover --debug --verbose

.PHONY: debug-scan
debug-scan: build
	@echo "🐛 Debugging scan operations..."
	@$(BIN_DIR)/corkscrew scan --services s3 --region $(AWS_REGION) --debug --dry-run --verbose

.PHONY: debug-list
debug-list: build
	@echo "🐛 Debugging list operations..."
	@$(BIN_DIR)/corkscrew list --services s3,ec2 --region $(AWS_REGION) --debug --dry-run --verbose

.PHONY: validate-operations
validate-operations: build
	@echo "🔍 Validating AWS operations..."
	@$(BIN_DIR)/corkscrew validate --services s3,ec2 --verbose

.PHONY: validate-s3
validate-s3: build
	@echo "🔍 Validating S3 operations..."
	@$(BIN_DIR)/corkscrew validate --services s3 --verbose

.PHONY: validate-ec2
validate-ec2: build
	@echo "🔍 Validating EC2 operations..."
	@$(BIN_DIR)/corkscrew validate --services ec2 --verbose

# =============================================================================
# CODE QUALITY
# =============================================================================

.PHONY: fmt
fmt:
	@echo "🎨 Formatting Go code..."
	@go fmt ./...

.PHONY: lint
lint:
	@echo "🔍 Linting Go code..."
	@golangci-lint run

.PHONY: vet
vet:
	@echo "🔍 Vetting Go code..."
	@go vet ./...

.PHONY: check
check: fmt vet lint test-unit test-scanner
	@echo "✅ Code quality checks passed!"

# =============================================================================
# PLUGIN DEVELOPMENT
# =============================================================================

.PHONY: generate-aws-services
generate-aws-services: build-tools
	@echo "🔧 Generating AWS service catalog..."
	@if [ -f "$(BIN_DIR)/generator" ]; then \
		$(BIN_DIR)/generator --generate-aws-services --output-dir ./generated --verbose; \
	else \
		echo "⚠️  Generator not found, please run 'make build-tools' first"; \
	fi

.PHONY: build-dynamic-plugins
build-dynamic-plugins: generate-aws-services
	@echo "🔧 Building dynamic plugins..."
	@$(BIN_DIR)/corkscrew generate-plugins --services s3,ec2,lambda --output-dir $(PLUGIN_DIR) --verbose

.PHONY: analyze-azure-sdk
analyze-azure-sdk: build-tools
	@echo "🔍 Analyzing Azure SDK for Go..."
	@if [ -f "$(BIN_DIR)/analyze-azure-sdk" ]; then \
		$(BIN_DIR)/analyze-azure-sdk -update -verbose -output $(BUILD_DIR)/azure-sdk-analysis.json; \
		echo "✅ Azure SDK analysis complete: $(BUILD_DIR)/azure-sdk-analysis.json"; \
	else \
		echo "❌ Azure SDK analyzer not found, please run 'make build-tools' first"; \
	fi

.PHONY: analyze-azure-sdk-core
analyze-azure-sdk-core: build-tools
	@echo "🔍 Analyzing Azure SDK core services..."
	@if [ -f "$(BIN_DIR)/analyze-azure-sdk" ]; then \
		$(BIN_DIR)/analyze-azure-sdk -services "compute,storage,network" -update -verbose -output $(BUILD_DIR)/azure-core-analysis.json; \
		echo "✅ Azure core services analysis complete: $(BUILD_DIR)/azure-core-analysis.json"; \
	else \
		echo "❌ Azure SDK analyzer not found, please run 'make build-tools' first"; \
	fi

.PHONY: test-azure-sdk-analyzer
test-azure-sdk-analyzer: build-tools
	@echo "🧪 Testing Azure SDK analyzer..."
	@if [ -f "scripts/test-azure-sdk-analyzer.sh" ]; then \
		./scripts/test-azure-sdk-analyzer.sh; \
	else \
		echo "❌ Test script not found"; \
	fi

# =============================================================================
# PLUGIN MANAGEMENT
# =============================================================================

.PHONY: plugin-install-aws
plugin-install-aws:
	@echo "🔌 Installing AWS provider plugin..."
	@./build/bin/corkscrew plugin install aws --all --verbose

.PHONY: plugin-install-azure
plugin-install-azure:
	@echo "🔌 Installing Azure provider plugin..."
	@./build/bin/corkscrew plugin install azure --all --verbose

.PHONY: plugin-install-aws-core
plugin-install-aws-core:
	@echo "🔌 Installing AWS provider plugin (core services)..."
	@./build/bin/corkscrew plugin install aws --services s3,ec2,lambda,rds,dynamodb --verbose

.PHONY: plugin-install-azure-core
plugin-install-azure-core:
	@echo "🔌 Installing Azure provider plugin (core services)..."
	@./build/bin/corkscrew plugin install azure --services compute,storage,network --verbose

.PHONY: plugin-list
plugin-list: build-cli
	@echo "🔌 Listing installed plugins..."
	@./build/bin/corkscrew plugin list --verbose

.PHONY: plugin-status
plugin-status: build-cli
	@echo "🔌 Checking plugin status..."
	@./build/bin/corkscrew plugin status

.PHONY: plugin-remove-aws
plugin-remove-aws: build-cli
	@echo "🗑️  Removing AWS provider plugin..."
	@./build/bin/corkscrew plugin remove aws --verbose

.PHONY: plugin-remove-azure
plugin-remove-azure: build-cli
	@echo "🗑️  Removing Azure provider plugin..."
	@./build/bin/corkscrew plugin remove azure --verbose

.PHONY: plugin-clean
plugin-clean:
	@echo "🧹 Cleaning all plugins..."
	@rm -f ./build/bin/corkscrew-*
	@rm -f ./plugins/build/corkscrew-*

# =============================================================================
# MONITORING AND STATUS
# =============================================================================

.PHONY: status
status:
	@echo "📊 Corkscrew Project Status"
	@echo "=========================="
	@echo ""
	@echo "Go version: $(shell go version 2>/dev/null || echo 'Not installed')"
	@echo "Module: $(shell head -1 go.mod 2>/dev/null || echo 'No go.mod found')"
	@echo ""
	@echo "Build artifacts:"
	@echo "  Binaries: $(shell ls $(BIN_DIR)/* 2>/dev/null | wc -l) files in $(BIN_DIR)/"
	@if [ -d "$(BIN_DIR)" ]; then \
		ls $(BIN_DIR)/* 2>/dev/null | sed 's|.*/|  - |' || echo "  (none)"; \
	fi
	@echo ""
	@echo "Plugins:"
	@echo "  Built: $(shell ls $(PLUGIN_DIR)/corkscrew-* 2>/dev/null | wc -l) plugins in $(PLUGIN_DIR)/"
	@if [ -d "$(PLUGIN_DIR)" ]; then \
		ls $(PLUGIN_DIR)/corkscrew-* 2>/dev/null | sed 's|.*/corkscrew-|  - |' || echo "  (none)"; \
	fi
	@echo ""
	@echo "Generated files:"
	@echo "  Protobuf: $(shell ls $(INTERNAL_DIR)/proto/*.pb.go 2>/dev/null | wc -l) files"
	@echo "  Scanner: $(shell ls $(INTERNAL_DIR)/scanner/*.go 2>/dev/null | wc -l) files"

.PHONY: health-check
health-check: build
	@echo "🏥 Running health checks..."
	@echo "Checking AWS credentials..."
	@aws sts get-caller-identity >/dev/null 2>&1 && echo "✅ AWS credentials OK" || echo "❌ AWS credentials not configured"
	@echo "Checking protobuf generation..."
	@[ -f "$(INTERNAL_DIR)/proto/scanner.pb.go" ] && echo "✅ Protobuf files OK" || echo "❌ Protobuf files missing"
	@echo "Checking CLI binary..."
	@[ -f "$(BIN_DIR)/corkscrew" ] && echo "✅ CLI binary OK" || echo "❌ CLI binary missing"
	@echo "Checking scanner component..."
	@[ -f "$(INTERNAL_DIR)/scanner/aws_resource_lister.go" ] && echo "✅ Scanner component OK" || echo "❌ Scanner component missing"

# =============================================================================
# HELP
# =============================================================================

.PHONY: help
help:
	@echo "🔧 Corkscrew Cloud Resource Scanner"
	@echo "=================================="
	@echo ""
	@echo "🚀 Quick Start:"
	@echo "  make setup              - Set up development environment"
	@echo "  make build              - Build all components"
	@echo "  make plugin-install-aws-core - Install AWS plugin (core services)"
	@echo "  make test-dry-run       - Safe testing without cloud calls"
	@echo ""
	@echo "🔨 Building:"
	@echo "  make build              - Build everything"
	@echo "  make build-cli          - Build CLI only"
	@echo "  make build-aws-plugin   - Build AWS plugin"
	@echo "  make build-azure-plugin - Build Azure plugin"
	@echo "  make build-tools        - Build development tools"
	@echo ""
	@echo "🔌 Plugin Management:"
	@echo "  make plugin-install-aws      - Install AWS plugin (all services)"
	@echo "  make plugin-install-azure    - Install Azure plugin (all services)"
	@echo "  make plugin-install-aws-core - Install AWS plugin (core services)"
	@echo "  make plugin-install-azure-core - Install Azure plugin (core services)"
	@echo "  make plugin-list            - List installed plugins"
	@echo "  make plugin-status          - Check plugin status"
	@echo "  make plugin-remove-aws      - Remove AWS plugin"
	@echo "  make plugin-remove-azure    - Remove Azure plugin"
	@echo "  make plugin-clean           - Clean all plugins"
	@echo ""
	@echo "🧪 Testing:"
	@echo "  make test               - Run all tests"
	@echo "  make test-unit          - Unit tests only"
	@echo "  make test-scanner       - Test AWS resource lister"
	@echo "  make test-integration   - Integration tests"
	@echo "  make test-plugins       - Plugin loading tests"
	@echo "  make test-discover      - Test service discovery"
	@echo "  make test-scan-dry      - Test dry-run scanning"
	@echo "  make test-dry-run       - All dry-run tests"
	@echo ""
	@echo "🔍 Scanning (DISCOVER → SCAN Pattern):"
	@echo "  make scan               - Full 3-phase scan"
	@echo "  make scan-discover      - Phase 1: Discover services"
	@echo "  make scan-list          - Phase 2: List resources"
	@echo "  make scan-describe      - Phase 3: Describe resources"
	@echo "  make scan-s3            - Scan S3 resources"
	@echo "  make scan-ec2           - Scan EC2 resources"
	@echo "  make scan-all           - Scan all services"
	@echo ""
	@echo "🧪 Dry-Run Testing (Safe):"
	@echo "  make scan-dry-s3        - Dry-run S3 scan"
	@echo "  make scan-dry-ec2       - Dry-run EC2 scan"
	@echo "  make scan-dry-all       - Dry-run all services"
	@echo "  make test-list-dry      - Test resource listing"
	@echo "  make test-validate      - Test operation validation"
	@echo ""
	@echo "🛠️  Development:"
	@echo "  make dev-setup          - Development environment"
	@echo "  make debug-discovery    - Debug service discovery"
	@echo "  make debug-scan         - Debug scanning"
	@echo "  make debug-list         - Debug listing"
	@echo "  make validate-operations - Validate AWS operations"
	@echo "  make validate-s3        - Validate S3 operations"
	@echo "  make validate-ec2       - Validate EC2 operations"
	@echo ""
	@echo "🎨 Code Quality:"
	@echo "  make check              - Run all quality checks"
	@echo "  make fmt                - Format code"
	@echo "  make lint               - Lint code"
	@echo "  make vet                - Vet code"
	@echo ""
	@echo "🔧 Plugin Development:"
	@echo "  make generate-aws-services   - Generate AWS service catalog"
	@echo "  make build-dynamic-plugins   - Build dynamic plugins"
	@echo "  make analyze-azure-sdk       - Analyze Azure SDK for Go"
	@echo "  make analyze-azure-sdk-core  - Analyze Azure SDK core services"
	@echo "  make test-azure-sdk-analyzer - Test Azure SDK analyzer"
	@echo ""
	@echo "📊 Monitoring:"
	@echo "  make status             - Show project status"
	@echo "  make health-check       - Run health checks"
	@echo ""
	@echo "🧹 Cleanup:"
	@echo "  make clean              - Clean everything"
	@echo "  make clean-build        - Clean build artifacts"
	@echo "  make clean-plugins      - Clean plugins"

# Set default region for AWS operations
export AWS_DEFAULT_REGION ?= $(AWS_REGION)
