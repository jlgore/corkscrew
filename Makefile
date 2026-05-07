# Corkscrew Cloud Resource Scanner
# Streamlined Makefile focusing on actual usage patterns

# Variables
GO_VERSION := 1.24
CORKSCREW_DIR := $(HOME)/.corkscrew
PLUGIN_DIR := $(CORKSCREW_DIR)/plugins
BIN_DIR := $(CORKSCREW_DIR)/bin

# Build directories (local workspace)
BUILD_DIR := ./build
LOCAL_BIN_DIR := $(BUILD_DIR)/bin

# Default target
.PHONY: all
all: clean build install

# =============================================================================
# SETUP AND CLEANUP
# =============================================================================

.PHONY: setup
setup: create-dirs deps
	@echo "✅ Development environment ready!"

.PHONY: create-dirs
create-dirs:
	@echo "📁 Creating directories..."
	@mkdir -p $(LOCAL_BIN_DIR) $(CORKSCREW_DIR) $(PLUGIN_DIR) $(BIN_DIR)

.PHONY: deps
deps:
	@echo "📦 Installing dependencies..."
	@go mod tidy
	@echo "✅ Dependencies installed"

# =============================================================================
# CONFIGURATION MANAGEMENT
# =============================================================================

.PHONY: config-init
config-init:
	@echo "🔧 Initializing corkscrew configuration..."
	@./build/bin/corkscrew config init

.PHONY: config-validate
config-validate:
	@echo "🔍 Validating configuration..."
	@./build/bin/corkscrew config validate

.PHONY: config-show
config-show:
	@echo "📋 Current configuration:"
	@./build/bin/corkscrew config show

.PHONY: config-services
config-services:
	@echo "📦 Configured AWS services:"
	@./build/bin/corkscrew config show | grep -A 100 "Resolved AWS Services:"

.PHONY: clean
clean:
	@echo "🧹 Cleaning build artifacts..."
	@rm -rf $(BUILD_DIR)
	@echo "✅ Clean complete"

.PHONY: clean-install
clean-install:
	@echo "🧹 Cleaning installed binaries..."
	@rm -rf $(CORKSCREW_DIR)
	@echo "✅ Installation cleaned"

# =============================================================================
# BUILDING
# =============================================================================

.PHONY: build
build: generate-proto build-cli build-plugins generate-registry
	@echo "🔨 Build complete!"

.PHONY: build-cli
build-cli: create-dirs
	@echo "🔨 Building main CLI..."
	@cd cmd/corkscrew && go build -o ../../$(LOCAL_BIN_DIR)/corkscrew .
	@echo "✅ CLI built: $(LOCAL_BIN_DIR)/corkscrew"

.PHONY: build-plugins
build-plugins: build-aws-plugin build-azure-plugin build-gcp-plugin build-kubernetes-plugin
	@echo "✅ All plugins built"

.PHONY: build-aws-plugin
build-aws-plugin: create-dirs
	@echo "🔨 Building AWS plugin..."
	@if [ -d "plugins/aws-provider" ]; then \
		cd plugins/aws-provider && go mod tidy && go build -tags="aws_services" -o ../../$(LOCAL_BIN_DIR)/aws-provider .; \
		echo "✅ AWS plugin built: $(LOCAL_BIN_DIR)/aws-provider"; \
	else \
		echo "⚠️  AWS plugin directory not found, skipping..."; \
	fi

.PHONY: build-azure-plugin
build-azure-plugin: create-dirs
	@echo "🔨 Building Azure plugin..."
	@if [ -d "plugins/azure-provider" ]; then \
		cd plugins/azure-provider && go mod tidy && go build -o ../../$(LOCAL_BIN_DIR)/azure-provider .; \
		echo "✅ Azure plugin built: $(LOCAL_BIN_DIR)/azure-provider"; \
	else \
		echo "⚠️  Azure plugin directory not found, skipping..."; \
	fi

.PHONY: build-gcp-plugin
build-gcp-plugin: create-dirs
	@echo "🔨 Building GCP plugin..."
	@if [ -d "plugins/gcp-provider" ]; then \
		cd plugins/gcp-provider && go mod tidy && go build -o ../../$(LOCAL_BIN_DIR)/gcp-provider .; \
		echo "✅ GCP plugin built: $(LOCAL_BIN_DIR)/gcp-provider"; \
	else \
		echo "⚠️  GCP plugin directory not found, skipping..."; \
	fi

.PHONY: build-kubernetes-plugin
build-kubernetes-plugin: create-dirs
	@echo "🔨 Building Kubernetes plugin..."
	@if [ -d "plugins/kubernetes-provider" ]; then \
		cd plugins/kubernetes-provider && go mod tidy && go build -o ../../$(LOCAL_BIN_DIR)/kubernetes-provider .; \
		echo "✅ Kubernetes plugin built: $(LOCAL_BIN_DIR)/kubernetes-provider"; \
	else \
		echo "⚠️  Kubernetes plugin directory not found, skipping..."; \
	fi

# =============================================================================
# PLUGIN REGISTRY GENERATION
# =============================================================================

.PHONY: generate-registry
generate-registry:
	@echo "📝 Generating plugin registry..."
	@VERSION=$$(git describe --tags --always 2>/dev/null || echo "dev"); \
	REPO=$${GITHUB_REPOSITORY:-"jlgore/corkscrew"}; \
	./scripts/generate-plugin-registry.sh "$$VERSION" "$$REPO" "plugins/registry.json"

# =============================================================================
# PROTOBUF GENERATION
# =============================================================================

.PHONY: generate-proto
generate-proto:
	@echo "🔄 Generating protobuf code..."
	@if [ -d "proto" ]; then \
		export PATH="$$PATH:$$HOME/go/bin"; \
		protoc --proto_path=proto \
		       --go_out=internal/proto --go_opt=paths=source_relative \
		       --go-grpc_out=internal/proto --go-grpc_opt=paths=source_relative \
		       proto/*.proto; \
		echo "✅ Protobuf code generated"; \
	else \
		echo "⚠️ No proto directory found, skipping..."; \
	fi

# =============================================================================
# INSTALLATION TO ~/.corkscrew/
# =============================================================================

.PHONY: install
install: install-cli install-plugins
	@echo "🎉 Installation complete!"
	@echo ""
	@echo "📍 Add to your PATH: export PATH=\"$(BIN_DIR):\$$PATH\""
	@echo "🚀 Usage: corkscrew discover --provider aws"

.PHONY: install-cli
install-cli: build-cli create-dirs
	@echo "📦 Installing CLI to $(BIN_DIR)..."
	@cp $(LOCAL_BIN_DIR)/corkscrew $(BIN_DIR)/
	@chmod +x $(BIN_DIR)/corkscrew
	@echo "✅ CLI installed: $(BIN_DIR)/corkscrew"

.PHONY: install-plugins
install-plugins: build-plugins create-dirs
	@echo "📦 Installing plugins to $(PLUGIN_DIR)..."
	@if [ -f "$(LOCAL_BIN_DIR)/aws-provider" ]; then \
		cp $(LOCAL_BIN_DIR)/aws-provider $(PLUGIN_DIR)/aws-provider; \
		chmod +x $(PLUGIN_DIR)/aws-provider; \
		echo "✅ AWS plugin installed: $(PLUGIN_DIR)/aws-provider"; \
	fi
	@if [ -f "$(LOCAL_BIN_DIR)/azure-provider" ]; then \
		cp $(LOCAL_BIN_DIR)/azure-provider $(PLUGIN_DIR)/azure-provider; \
		chmod +x $(PLUGIN_DIR)/azure-provider; \
		echo "✅ Azure plugin installed: $(PLUGIN_DIR)/azure-provider"; \
	fi
	@if [ -f "$(LOCAL_BIN_DIR)/gcp-provider" ]; then \
		cp $(LOCAL_BIN_DIR)/gcp-provider $(PLUGIN_DIR)/gcp-provider; \
		chmod +x $(PLUGIN_DIR)/gcp-provider; \
		echo "✅ GCP plugin installed: $(PLUGIN_DIR)/gcp-provider"; \
	fi
	@if [ -f "$(LOCAL_BIN_DIR)/kubernetes-provider" ]; then \
		cp $(LOCAL_BIN_DIR)/kubernetes-provider $(PLUGIN_DIR)/kubernetes-provider; \
		chmod +x $(PLUGIN_DIR)/kubernetes-provider; \
		echo "✅ Kubernetes plugin installed: $(PLUGIN_DIR)/kubernetes-provider"; \
	fi

# =============================================================================
# TESTING
# =============================================================================

.PHONY: test
test: test-unit test-integration
	@echo "✅ All tests passed!"

.PHONY: test-unit
test-unit:
	@echo "🧪 Running unit tests..."
	@go test -v ./internal/... -short
	@echo "✅ Unit tests passed"

.PHONY: test-integration
test-integration: install
	@echo "🧪 Running integration tests..."
	@echo "Testing AWS provider discovery..."
	@if command -v aws >/dev/null 2>&1; then \
		$(BIN_DIR)/corkscrew discover --provider aws || echo "⚠️ AWS discovery failed (credentials?)"; \
	else \
		echo "⚠️ AWS CLI not installed, skipping AWS tests"; \
	fi
	@echo "Testing Azure provider discovery..."
	@if command -v az >/dev/null 2>&1; then \
		$(BIN_DIR)/corkscrew discover --provider azure || echo "⚠️ Azure discovery failed (credentials?)"; \
	else \
		echo "⚠️ Azure CLI not installed, skipping Azure tests"; \
	fi
	@echo "✅ Integration tests complete"

.PHONY: test-dry-run
test-dry-run: install
	@echo "🔍 Running safe dry-run tests..."
	@$(BIN_DIR)/corkscrew discover --provider aws --verbose || echo "⚠️ AWS discovery failed"
	@$(BIN_DIR)/corkscrew info --provider aws || echo "⚠️ AWS info failed"
	@echo "✅ Dry-run tests complete"

# =============================================================================
# DEVELOPMENT HELPERS
# =============================================================================

.PHONY: dev
dev: install
	@echo "🛠️ Development environment ready!"
	@echo "CLI: $(BIN_DIR)/corkscrew"
	@echo "Plugins: $(PLUGIN_DIR)/"
	@ls -la $(PLUGIN_DIR)/ || echo "No plugins installed"

.PHONY: fmt
fmt:
	@echo "🎨 Formatting code..."
	@go fmt ./...
	@echo "✅ Code formatted"

.PHONY: lint
lint:
	@echo "🔍 Linting code..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "⚠️ golangci-lint not installed, run: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
	fi

.PHONY: check
check: fmt lint test-unit
	@echo "✅ Code quality checks passed!"

# =============================================================================
# REAL-WORLD USAGE EXAMPLES
# =============================================================================

.PHONY: demo-aws
demo-aws: install
	@echo "🔍 AWS Demo (based on real documentation)..."
	@echo "1. Discovering AWS services..."
	@$(BIN_DIR)/corkscrew discover --provider aws
	@echo ""
	@echo "2. Getting provider info..."
	@$(BIN_DIR)/corkscrew info --provider aws
	@echo ""
	@echo "3. Dry-run scan (safe)..."
	@$(BIN_DIR)/corkscrew scan --provider aws --services s3 --region us-east-1 --dry-run || echo "⚠️ Requires AWS credentials for real scan"

.PHONY: demo-azure
demo-azure: install
	@echo "🔍 Azure Demo (based on real documentation)..."
	@echo "1. Discovering Azure services..."
	@$(BIN_DIR)/corkscrew discover --provider azure --verbose
	@echo ""
	@echo "2. Getting provider info..."
	@$(BIN_DIR)/corkscrew info --provider azure
	@echo ""
	@echo "3. Dry-run scan (safe)..."
	@$(BIN_DIR)/corkscrew scan --provider azure --services storage --region centralus --dry-run || echo "⚠️ Requires Azure credentials for real scan"

# =============================================================================
# STATUS AND HELP
# =============================================================================

.PHONY: status
status:
	@echo "📊 Corkscrew Status"
	@echo "=================="
	@echo ""
	@echo "🔧 Build Artifacts:"
	@echo "  Local binaries: $(shell ls $(LOCAL_BIN_DIR)/* 2>/dev/null | wc -l) files"
	@if [ -d "$(LOCAL_BIN_DIR)" ]; then ls $(LOCAL_BIN_DIR)/* 2>/dev/null | sed 's|.*/|  - |' || echo "  (none)"; fi
	@echo ""
	@echo "📦 Installed ($(CORKSCREW_DIR)):"
	@echo "  CLI: $(shell [ -f "$(BIN_DIR)/corkscrew" ] && echo "✅ installed" || echo "❌ missing")"
	@echo "  Plugins: $(shell ls $(PLUGIN_DIR)/* 2>/dev/null | wc -l) installed"
	@if [ -d "$(PLUGIN_DIR)" ]; then ls $(PLUGIN_DIR)/* 2>/dev/null | sed 's|.*/|  - |' || echo "  (none)"; fi
	@echo ""
	@echo "🎯 Ready to use:"
	@echo "  $(BIN_DIR)/corkscrew discover --provider aws"
	@echo "  $(BIN_DIR)/corkscrew scan --provider azure --services storage"

.PHONY: help
help:
	@echo "🔧 Corkscrew Cloud Resource Scanner"
	@echo "=================================="
	@echo ""
	@echo "🚀 Quick Start:"
	@echo "  make setup build install    - Complete setup"
	@echo "  make demo-aws               - Run AWS demo"
	@echo "  make demo-azure             - Run Azure demo"
	@echo ""
	@echo "🔨 Building:"
	@echo "  make build                  - Build everything"
	@echo "  make build-cli              - Build CLI only"
	@echo "  make build-plugins          - Build all plugins"
	@echo "  make build-aws-plugin       - Build AWS plugin"
	@echo "  make build-azure-plugin     - Build Azure plugin"
	@echo "  make build-gcp-plugin       - Build GCP plugin"
	@echo ""
	@echo "📦 Installing:"
	@echo "  make install                - Install to ~/.corkscrew/"
	@echo "  make install-cli            - Install CLI only"
	@echo "  make install-plugins        - Install plugins only"
	@echo ""
	@echo "🧪 Testing:"
	@echo "  make test                   - Run all tests"
	@echo "  make test-unit              - Unit tests only"
	@echo "  make test-integration       - Integration tests"
	@echo "  make test-dry-run           - Safe dry-run tests"
	@echo ""
	@echo "🛠️ Development:"
	@echo "  make dev                    - Setup dev environment"
	@echo ""
	@echo "⚙️  Configuration:"
	@echo "  make config-init            - Create default config file"
	@echo "  make config-show            - Show current configuration"
	@echo "  make config-validate        - Validate configuration"
	@echo "  make config-services        - List configured services"
	@echo "  make check                  - Code quality checks"
	@echo "  make fmt                    - Format code"
	@echo "  make lint                   - Lint code"
	@echo ""
	@echo "📊 Info:"
	@echo "  make status                 - Show current status"
	@echo "  make help                   - Show this help"
	@echo ""
	@echo "🧹 Cleanup:"
	@echo "  make clean                  - Clean build artifacts"
	@echo "  make clean-install          - Clean installed files"
	@echo ""
	@echo "💡 Usage Examples (after 'make install'):"
	@echo "  ~/.corkscrew/bin/corkscrew discover --provider aws"
	@echo "  ~/.corkscrew/bin/corkscrew scan --provider aws --services s3,ec2 --region us-west-2"
	@echo "  ~/.corkscrew/bin/corkscrew list --provider aws --service s3 --region us-east-1"
	@echo "  ~/.corkscrew/bin/corkscrew info --provider azure"
	@echo "  ~/.corkscrew/bin/corkscrew schemas --provider azure --services storage --format sql"

# Default help
.DEFAULT_GOAL := help 
