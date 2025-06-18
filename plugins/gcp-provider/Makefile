# Corkscrew GCP Provider Makefile
# Phase 3: Service Account Automation

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOCLEAN=$(GOCMD) clean
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod

# Build parameters
BINARY_NAME=corkscrew-gcp-provider
SA_DEPLOY_BINARY=deploy-service-account
BUILD_DIR=build
DIST_DIR=dist

# Version info
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_TIME ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

# Build flags
LDFLAGS=-ldflags "-X main.Version=$(VERSION) -X main.Commit=$(COMMIT) -X main.BuildTime=$(BUILD_TIME)"

# Platforms for cross-compilation
PLATFORMS=linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64

.PHONY: all build clean test deps lint fmt vet security help
.PHONY: build-sa-tool install-sa-tool test-sa release docker
.PHONY: generate-scripts example-deployment validate-setup

# Default target
all: clean deps test build

## Build targets

# Build main provider binary
build:
	@echo "🔨 Building GCP provider..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) .

# Build service account deployment tool (Phase 3)
build-sa-tool:
	@echo "🔨 Building service account deployment tool..."
	@mkdir -p $(BUILD_DIR)
	$(GOBUILD) $(LDFLAGS) -o $(BUILD_DIR)/$(SA_DEPLOY_BINARY) ./cmd/deploy-service-account

# Install service account tool to system PATH
install-sa-tool: build-sa-tool
	@echo "📦 Installing service account deployment tool..."
	@sudo cp $(BUILD_DIR)/$(SA_DEPLOY_BINARY) /usr/local/bin/
	@echo "✅ Installed to /usr/local/bin/$(SA_DEPLOY_BINARY)"

# Build for all platforms
build-all:
	@echo "🌍 Building for all platforms..."
	@mkdir -p $(DIST_DIR)
	@for platform in $(PLATFORMS); do \
		platform_split=($${platform//\// }); \
		GOOS=$${platform_split[0]}; \
		GOARCH=$${platform_split[1]}; \
		output_name="$(BINARY_NAME)-$$GOOS-$$GOARCH"; \
		if [ $$GOOS = "windows" ]; then output_name+=".exe"; fi; \
		echo "Building $$output_name..."; \
		GOOS=$$GOOS GOARCH=$$GOARCH $(GOBUILD) $(LDFLAGS) -o $(DIST_DIR)/$$output_name .; \
		\
		sa_output_name="$(SA_DEPLOY_BINARY)-$$GOOS-$$GOARCH"; \
		if [ $$GOOS = "windows" ]; then sa_output_name+=".exe"; fi; \
		echo "Building $$sa_output_name..."; \
		GOOS=$$GOOS GOARCH=$$GOARCH $(GOBUILD) $(LDFLAGS) -o $(DIST_DIR)/$$sa_output_name ./cmd/deploy-service-account; \
	done

## Test targets

# Run all tests
test:
	@echo "🧪 Running tests..."
	$(GOTEST) -v -race -coverprofile=coverage.out ./...

# Run tests with coverage report
test-coverage: test
	@echo "📊 Generating coverage report..."
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Run service account specific tests
test-sa:
	@echo "🧪 Running service account tests..."
	$(GOTEST) -v -race -run="TestServiceAccount.*" ./...

# Run benchmarks
benchmark:
	@echo "⚡ Running benchmarks..."
	$(GOTEST) -bench=. -benchmem ./...

# Run integration tests (requires GCP credentials)
test-integration:
	@echo "🔗 Running integration tests..."
	@if [ -z "$(GOOGLE_APPLICATION_CREDENTIALS)" ] && [ -z "$(GOOGLE_CLOUD_PROJECT)" ]; then \
		echo "⚠️  Warning: No GCP credentials found, skipping integration tests"; \
		echo "   Set GOOGLE_APPLICATION_CREDENTIALS or GOOGLE_CLOUD_PROJECT"; \
	else \
		$(GOTEST) -v -tags=integration ./...; \
	fi

## Code quality targets

# Format code
fmt:
	@echo "🎨 Formatting code..."
	$(GOCMD) fmt ./...

# Vet code
vet:
	@echo "🔍 Vetting code..."
	$(GOCMD) vet ./...

# Lint code (requires golangci-lint)
lint:
	@echo "🧹 Linting code..."
	@if command -v golangci-lint >/dev/null; then \
		golangci-lint run; \
	else \
		echo "⚠️  golangci-lint not installed. Install with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; \
	fi

# Security check (requires gosec)
security:
	@echo "🔒 Running security checks..."
	@if command -v gosec >/dev/null; then \
		gosec ./...; \
	else \
		echo "⚠️  gosec not installed. Install with: go install github.com/securecodewarrior/gosec/v2/cmd/gosec@latest"; \
	fi

## Dependency management

# Download dependencies
deps:
	@echo "📦 Downloading dependencies..."
	$(GOMOD) download
	$(GOMOD) tidy

# Update dependencies
deps-update:
	@echo "⬆️  Updating dependencies..."
	$(GOGET) -u ./...
	$(GOMOD) tidy

# Verify dependencies
deps-verify:
	@echo "✅ Verifying dependencies..."
	$(GOMOD) verify

## Service Account Automation (Phase 3)

# Generate example deployment scripts
generate-scripts:
	@echo "📜 Generating example service account scripts..."
	@mkdir -p examples/scripts
	@$(BUILD_DIR)/$(SA_DEPLOY_BINARY) \
		--project=my-gcp-project \
		--account-name=corkscrew-scanner \
		--generate-script \
		--minimal
	@mv deploy-corkscrew-sa-*.sh examples/scripts/ 2>/dev/null || true
	@echo "✅ Example scripts generated in examples/scripts/"

# Example deployment (dry run)
example-deployment:
	@echo "🚀 Example service account deployment (dry run)..."
	@if [ -z "$(PROJECT_ID)" ]; then \
		echo "❌ PROJECT_ID required. Usage: make example-deployment PROJECT_ID=my-project"; \
		exit 1; \
	fi
	@$(BUILD_DIR)/$(SA_DEPLOY_BINARY) \
		--project=$(PROJECT_ID) \
		--account-name=corkscrew-example \
		--display-name="Corkscrew Example Scanner" \
		--minimal \
		--dry-run

# Validate existing service account setup
validate-setup:
	@echo "🔍 Validating service account setup..."
	@if [ -z "$(PROJECT_ID)" ] || [ -z "$(SA_EMAIL)" ]; then \
		echo "❌ PROJECT_ID and SA_EMAIL required."; \
		echo "   Usage: make validate-setup PROJECT_ID=my-project SA_EMAIL=sa@project.iam.gserviceaccount.com"; \
		exit 1; \
	fi
	@$(BUILD_DIR)/$(SA_DEPLOY_BINARY) \
		--validate=$(SA_EMAIL) \
		--project=$(PROJECT_ID)

## Change Tracking Automation (Phase 4)

# Test change tracking functionality
test-change-tracking:
	@echo "🔍 Testing change tracking functionality..."
	$(GOTEST) -v -run="TestChangeTracking.*" ./...
	$(GOTEST) -v -run="TestGCPChangeTracker.*" ./...
	$(GOTEST) -v -run="TestChangeAnalytics.*" ./...
	$(GOTEST) -v -run="TestAlertingSystem.*" ./...

# Demo change tracking features
demo-change-tracking:
	@echo "🎯 Demonstrating change tracking features..."
	@echo "This would show change tracking capabilities:"
	@echo "  • Asset change detection"
	@echo "  • Drift detection against baselines"
	@echo "  • Real-time change monitoring"
	@echo "  • Change analytics and reporting"
	@echo "  • Intelligent alerting"
	@echo ""
	@echo "Run with: make demo-change-tracking PROJECT_ID=your-project"

# Benchmark change tracking performance
benchmark-change-tracking:
	@echo "⚡ Benchmarking change tracking performance..."
	$(GOTEST) -bench=BenchmarkChangeEvent.* -benchmem ./...
	$(GOTEST) -bench=BenchmarkChangeTracker.* -benchmem ./...
	$(GOTEST) -bench=BenchmarkAlertingSystem.* -benchmem ./...

# Test change storage with real database
test-change-storage:
	@echo "🗄️  Testing change storage functionality..."
	$(GOTEST) -v -run="TestDuckDBChangeStorage.*" ./...

# Initialize change tracking database
init-change-db:
	@echo "🗄️  Initializing change tracking database..."
	@echo "Creating change_tracking.db with required schema..."
	@go run -c "storage, _ := NewDuckDBChangeStorage(\"change_tracking.db\"); storage.Close()"

# Clean change tracking data
clean-change-data:
	@echo "🧹 Cleaning change tracking data..."
	@rm -f change_tracking.db change_tracking.db.wal
	@rm -f *.db *.db.wal
	@echo "✅ Change tracking data cleaned"

## Documentation

# Generate documentation
docs:
	@echo "📚 Generating documentation..."
	@mkdir -p docs
	@$(GOCMD) doc -all . > docs/provider-api.md
	@echo "✅ Documentation generated in docs/"

# Generate service account documentation
docs-sa:
	@echo "📚 Generating service account documentation..."
	@mkdir -p docs
	@echo "# Service Account Automation (Phase 3)" > docs/service-account-guide.md
	@echo "" >> docs/service-account-guide.md
	@echo "## Quick Start" >> docs/service-account-guide.md
	@echo "" >> docs/service-account-guide.md
	@echo "\`\`\`bash" >> docs/service-account-guide.md
	@echo "# Build the tool" >> docs/service-account-guide.md
	@echo "make build-sa-tool" >> docs/service-account-guide.md
	@echo "" >> docs/service-account-guide.md
	@echo "# Deploy service account" >> docs/service-account-guide.md
	@echo "./build/$(SA_DEPLOY_BINARY) --project=YOUR_PROJECT_ID" >> docs/service-account-guide.md
	@echo "\`\`\`" >> docs/service-account-guide.md
	@echo "✅ Service account guide generated: docs/service-account-guide.md"

## Docker targets

# Build Docker image
docker:
	@echo "🐳 Building Docker image..."
	@docker build -t corkscrew-gcp-provider:$(VERSION) .
	@docker build -t corkscrew-gcp-provider:latest .

# Build multi-platform Docker images
docker-buildx:
	@echo "🐳 Building multi-platform Docker images..."
	@docker buildx build --platform linux/amd64,linux/arm64 \
		-t corkscrew-gcp-provider:$(VERSION) \
		-t corkscrew-gcp-provider:latest \
		--push .

## Release targets

# Create release archive
release: clean build-all
	@echo "📦 Creating release archives..."
	@mkdir -p $(DIST_DIR)/archives
	@for file in $(DIST_DIR)/$(BINARY_NAME)-*; do \
		if [ -f "$$file" ]; then \
			base=$$(basename $$file); \
			platform=$${base#$(BINARY_NAME)-}; \
			archive_name="corkscrew-gcp-provider-$(VERSION)-$$platform"; \
			if [[ "$$platform" == *"windows"* ]]; then \
				zip -j "$(DIST_DIR)/archives/$$archive_name.zip" "$$file" "$(DIST_DIR)/$(SA_DEPLOY_BINARY)-$$platform"; \
			else \
				tar -czf "$(DIST_DIR)/archives/$$archive_name.tar.gz" -C $(DIST_DIR) "$$(basename $$file)" "$$(basename $(DIST_DIR)/$(SA_DEPLOY_BINARY)-$$platform.exe 2>/dev/null || basename $(DIST_DIR)/$(SA_DEPLOY_BINARY)-$$platform)"; \
			fi; \
		fi; \
	done
	@echo "✅ Release archives created in $(DIST_DIR)/archives/"

# Clean build artifacts
clean:
	@echo "🧹 Cleaning build artifacts..."
	@$(GOCLEAN)
	@rm -rf $(BUILD_DIR) $(DIST_DIR) coverage.out coverage.html
	@rm -f corkscrew-key.json deploy-corkscrew-sa-*.sh validate-corkscrew-sa-*.sh
	@rm -f corkscrew-sa-config.json test-*.json

## Utility targets

# Show help
help:
	@echo "Corkscrew GCP Provider Makefile"
	@echo "================================"
	@echo ""
	@echo "Build Targets:"
	@echo "  build           Build main provider binary"
	@echo "  build-sa-tool   Build service account deployment tool (Phase 3)"
	@echo "  build-all       Build for all platforms"
	@echo "  install-sa-tool Install SA tool to system PATH"
	@echo ""
	@echo "Test Targets:"
	@echo "  test            Run all tests"
	@echo "  test-coverage   Run tests with coverage report"
	@echo "  test-sa         Run service account specific tests"
	@echo "  test-integration Run integration tests (requires GCP credentials)"
	@echo "  benchmark       Run benchmarks"
	@echo ""
	@echo "Code Quality:"
	@echo "  fmt             Format code"
	@echo "  vet             Vet code"
	@echo "  lint            Lint code (requires golangci-lint)"
	@echo "  security        Security check (requires gosec)"
	@echo ""
	@echo "Dependencies:"
	@echo "  deps            Download dependencies"
	@echo "  deps-update     Update dependencies"
	@echo "  deps-verify     Verify dependencies"
	@echo ""
	@echo "Service Account Automation (Phase 3):"
	@echo "  generate-scripts           Generate example scripts"
	@echo "  example-deployment         Example deployment (dry run)"
	@echo "    PROJECT_ID=my-project"
	@echo "  validate-setup             Validate SA setup"
	@echo "    PROJECT_ID=my-project SA_EMAIL=sa@project.iam.gserviceaccount.com"
	@echo ""
	@echo "Change Tracking (Phase 4):"
	@echo "  test-change-tracking       Test change tracking functionality"
	@echo "  demo-change-tracking       Demo change tracking features"
	@echo "  benchmark-change-tracking  Benchmark change tracking performance"
	@echo "  test-change-storage        Test change storage with database"
	@echo "  init-change-db             Initialize change tracking database"
	@echo "  clean-change-data          Clean change tracking data"
	@echo ""
	@echo "Documentation:"
	@echo "  docs            Generate API documentation"
	@echo "  docs-sa         Generate service account guide"
	@echo ""
	@echo "Docker:"
	@echo "  docker          Build Docker image"
	@echo "  docker-buildx   Build multi-platform images"
	@echo ""
	@echo "Release:"
	@echo "  release         Create release archives"
	@echo "  clean           Clean build artifacts"
	@echo ""
	@echo "Variables:"
	@echo "  VERSION         Build version (default: git describe)"
	@echo "  PROJECT_ID      GCP project ID for examples"
	@echo "  SA_EMAIL        Service account email for validation"

# Show version info
version:
	@echo "Version: $(VERSION)"
	@echo "Commit:  $(COMMIT)"
	@echo "Built:   $(BUILD_TIME)"

# Show current status
status:
	@echo "🔍 Corkscrew GCP Provider Status"
	@echo "================================"
	@echo "Version:    $(VERSION)"
	@echo "Commit:     $(COMMIT)"
	@echo "Build Time: $(BUILD_TIME)"
	@echo ""
	@echo "Dependencies:"
	@$(GOMOD) list -m all | head -5
	@echo "..."
	@echo ""
	@echo "Build Status:"
	@if [ -f "$(BUILD_DIR)/$(BINARY_NAME)" ]; then \
		echo "✅ Provider binary: $(BUILD_DIR)/$(BINARY_NAME)"; \
	else \
		echo "❌ Provider binary: Not built"; \
	fi
	@if [ -f "$(BUILD_DIR)/$(SA_DEPLOY_BINARY)" ]; then \
		echo "✅ SA deploy tool: $(BUILD_DIR)/$(SA_DEPLOY_BINARY)"; \
	else \
		echo "❌ SA deploy tool: Not built"; \
	fi
	@echo ""
	@echo "Test Status:"
	@if [ -f "coverage.out" ]; then \
		echo "✅ Test coverage: Available"; \
		$(GOCMD) tool cover -func=coverage.out | tail -1; \
	else \
		echo "❌ Test coverage: Not available"; \
	fi

# Quick development setup
dev-setup: deps build-sa-tool test
	@echo "🚀 Development environment ready!"
	@echo ""
	@echo "Quick commands:"
	@echo "  make build-sa-tool    # Build service account tool"
	@echo "  make test-sa          # Test service account functionality"
	@echo "  make example-deployment PROJECT_ID=your-project"
	@echo ""
	@echo "Service account tool: $(BUILD_DIR)/$(SA_DEPLOY_BINARY)"

.DEFAULT_GOAL := help