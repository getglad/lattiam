# Quality assurance targets - formatting, linting, and security

# ============================================================================
# COMPOSITE QA TARGETS
# ============================================================================

# Auto-fix formatting and linting issues
fix: _ensure-docs _run-vet _run-goimports-fix _run-golangci-lint-fix security ## Auto-fix formatting, linting, vet, and security issues
	@echo "$(GREEN)✓ Go code formatted, linted, and validated$(RESET)"

# Check code quality (validation only - no auto-fix)
check: _ensure-docs _run-vet _run-golangci-lint-check security ## Run code quality validation (linting, vet, security - no auto-fix)
	@echo "$(GREEN)✓ Go validation complete$(RESET)"

# Comprehensive security scanning
security: _run-govulncheck _run-nancy ## Run additional security scans (govulncheck + nancy) - gosec handled by golangci-lint
	@echo "$(GREEN)✓ Security scanning complete$(RESET)"

# Complete quality assurance pipeline
qa: check test-quality ## Run complete quality assurance pipeline (format + lint + security + vulnerabilities + test quality)
	@echo "$(GREEN)✓ Quality assurance complete$(RESET)"

# ============================================================================
# INTERNAL HELPER TARGETS
# ============================================================================

# Private targets for specific tool dependency checking
.PHONY: _check-lint-tools
_check-lint-tools: ## Check if linting tools are installed
	@command -v goimports >/dev/null 2>&1 || (echo "$(RED)ERROR: goimports not found$(RESET)" && echo "$(YELLOW)Install with: go install golang.org/x/tools/cmd/goimports@latest$(RESET)" && exit 1)
	@command -v golangci-lint >/dev/null 2>&1 || (echo "$(RED)ERROR: golangci-lint not found$(RESET)" && echo "$(YELLOW)Install with: curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin$(RESET)" && exit 1)

.PHONY: _check-security-tools
_check-security-tools: ## Check if security tools are installed
	@command -v govulncheck >/dev/null 2>&1 || (echo "$(RED)ERROR: govulncheck not found$(RESET)" && echo "$(YELLOW)Install with: go install golang.org/x/vuln/cmd/govulncheck@latest$(RESET)" && exit 1)
	@command -v nancy >/dev/null 2>&1 || (echo "$(RED)ERROR: nancy not found$(RESET)" && echo "$(YELLOW)Install with: go install github.com/sonatype-nexus-community/nancy@latest$(RESET)" && exit 1)

.PHONY: _check-build-tools  
_check-build-tools: ## Check if build tools are installed
	@command -v goreleaser >/dev/null 2>&1 || (echo "$(RED)ERROR: goreleaser not found$(RESET)" && echo "$(YELLOW)Install with: go install github.com/goreleaser/goreleaser@latest$(RESET)" && exit 1)

# Legacy target for backward compatibility - checks all tools
.PHONY: deps-check
deps-check: _check-lint-tools _check-security-tools _check-build-tools ## Check if required tools are installed
	@echo "$(CYAN)Checking Go development dependencies...$(RESET)"
	@echo "$(GREEN)✓ All Go dependencies found$(RESET)"

# Private target for go vet
.PHONY: _run-vet
_run-vet:
	@echo "$(YELLOW)  → Running go vet...$(RESET)"
	@go vet $$(go list ./... | grep -v /scratch)

# Private target for goimports
.PHONY: _run-goimports-fix
_run-goimports-fix: _check-lint-tools
	@echo "$(YELLOW)  → Formatting Go code...$(RESET)"
	@goimports -w -local github.com/lattiam/lattiam .

# Private target for golangci-lint check
.PHONY: _run-golangci-lint-check
_run-golangci-lint-check: _check-lint-tools
	@echo "$(YELLOW)  → Running linting (validation only)...$(RESET)"
	@golangci-lint run ./cmd/... ./internal/... ./pkg/... ./tests/...

# Private target for golangci-lint fix
.PHONY: _run-golangci-lint-fix
_run-golangci-lint-fix: _check-lint-tools
	@echo "$(YELLOW)  → Running linting with auto-fix...$(RESET)"
	@golangci-lint run --fix ./cmd/... ./internal/... ./pkg/... ./tests/...

# Private target for govulncheck
.PHONY: _run-govulncheck
_run-govulncheck: _check-security-tools
	@echo "$(YELLOW)  → Running govulncheck...$(RESET)"
	@govulncheck ./...

# Private target for nancy
.PHONY: _run-nancy
_run-nancy: _check-security-tools
	@echo "$(YELLOW)  → Running nancy...$(RESET)"
	@go list -json -deps | nancy sleuth



# Check for non-deterministic test assertions
.PHONY: test-quality
test-quality: ## Check test quality (prevent non-deterministic assertions)
	@echo "$(CYAN)Checking test quality...$(RESET)"
	@echo "$(YELLOW)  → Checking for non-deterministic assertions...$(RESET)"
	@if grep -rn "at least\|GreaterOrEqual\|NotEmpty.*should.*at least\|Positive.*should.*at least" tests/ internal/ --include="*_test.go" | grep -v "^Binary file"; then \
		echo "$(YELLOW)⚠ Found non-deterministic test assertions$(RESET)"; \
		echo "$(YELLOW)Consider replacing with exact expectations (e.g., use Equal instead of GreaterOrEqual)$(RESET)"; \
	else \
		echo "$(GREEN)  ✓ No non-deterministic assertions found$(RESET)"; \
	fi
	@echo "$(YELLOW)  → Checking for skipped tests...$(RESET)"
	@if grep -rn "t.Skip\|s.T().Skip" tests/ internal/ --include="*_test.go" | grep -v "// Skip"; then \
		echo "$(YELLOW)WARNING: Found skipped tests$(RESET)"; \
	else \
		echo "$(GREEN)  ✓ No skipped tests found$(RESET)"; \
	fi

.PHONY: fix check security qa deps-check _check-lint-tools _check-security-tools _check-build-tools _run-vet _run-goimports-fix _run-golangci-lint-check _run-golangci-lint-fix _run-govulncheck _run-nancy test-quality
