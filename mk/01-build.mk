# Build targets for binaries and artifacts

# ============================================================================
# COMPOSITE BUILD TARGETS
# ============================================================================

# Default 'all' target runs the standard development workflow
all: fix test build ## Run fix, test, and build (complete workflow)

# Release pipeline - run complete test suite + quality checks + build for all platforms
release: test-all check build-all-platforms ## Run complete test suite + quality checks + build for release

# ============================================================================
# SINGLE BUILD TARGETS
# ============================================================================

# Download Go dependencies
deps: ## Download Go dependencies
	@echo "$(CYAN)Downloading Go dependencies...$(RESET)"
	@go mod download
	@go mod tidy
	@echo "$(GREEN)✓ Dependencies updated$(RESET)"

# Clean build artifacts
clean: ## Clean build artifacts and caches
	@echo "$(CYAN)Cleaning build artifacts...$(RESET)"
	@rm -rf $(BUILD_DIR) $(DIST_DIR) $(COVERAGE_DIR)
	@go clean -cache -testcache -modcache
	@echo "$(CYAN)Cleaning test artifacts...$(RESET)"
	@rm -f *.log *.out coverage.html coverage.txt *.coverprofile
	@rm -rf demo-test-results-*/ test-logs/ test-output/ test-results/
	@echo "$(GREEN)✓ Clean complete$(RESET)"

# Build primary target (unified CLI)
build: ## Build unified CLI (primary component)
	@echo "$(CYAN)Building unified CLI...$(RESET)"
	@mkdir -p $(BUILD_DIR)
	@go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(CMD_DIR)/$(BINARY_NAME)
	@echo "$(GREEN)✓ Built $(BUILD_DIR)/$(BINARY_NAME)$(RESET)"

# Build for all platforms using goreleaser
build-all-platforms: _check-build-tools ## Build unified CLI for all platforms (optional)
	@echo "$(CYAN)Building for all platforms...$(RESET)"
	@mkdir -p $(DIST_DIR)
	@goreleaser build --snapshot --clean
	@echo "$(GREEN)✓ Multi-platform builds complete$(RESET)"

# Install unified CLI to GOPATH/bin
install: ## Install unified CLI to GOPATH/bin
	@echo "$(CYAN)Installing Lattiam CLI...$(RESET)"
	@go install $(LDFLAGS) $(CMD_DIR)/$(BINARY_NAME)
	@echo "$(GREEN)✓ Installed $(BINARY_NAME)$(RESET)"

# Development build with race detection
build-dev: ## Build unified CLI for development with race detection
	@echo "$(CYAN)Building development version with race detection...$(RESET)"
	@mkdir -p $(BUILD_DIR)
	@go build -tags "localstack dev" -race $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-dev $(CMD_DIR)/$(BINARY_NAME)
	@echo "$(GREEN)✓ Development build complete (includes race detection)$(RESET)"

.PHONY: all release deps clean build build-all-platforms install build-dev
