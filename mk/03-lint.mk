# Quality assurance targets - formatting, linting, and security

# ============================================================================
# COMPOSITE QA TARGETS
# ============================================================================

# Auto-fix formatting and linting issues
fix: _run-vet _run-goimports-fix _run-golangci-lint-fix security ## Auto-fix formatting, linting, vet, and security issues
	@echo "$(GREEN)✓ Go code formatted, linted, and validated$(RESET)"

# Check code quality (validation only - no auto-fix)
check: _run-vet _run-golangci-lint-check security ## Run code quality validation (linting, vet, security - no auto-fix)
	@echo "$(GREEN)✓ Go validation complete$(RESET)"

# Comprehensive security scanning
security: _run-govulncheck _run-nancy ## Run additional security scans (govulncheck + nancy) - gosec handled by golangci-lint
	@echo "$(GREEN)✓ Security scanning complete$(RESET)"

# Complete quality assurance pipeline
qa: check validate-openapi ## Run complete quality assurance pipeline (format + lint + security + vulnerabilities + OpenAPI)
	@echo "$(GREEN)✓ Quality assurance complete$(RESET)"

# ============================================================================
# INTERNAL HELPER TARGETS
# ============================================================================

# Private target for dependency checking
.PHONY: deps-check
deps-check: ## Check if required tools are installed
	@echo "$(CYAN)Checking Go development dependencies...$(RESET)"
	@command -v goimports >/dev/null 2>&1 || (echo "$(RED)ERROR: goimports not found$(RESET)" && echo "$(YELLOW)Install with: go install golang.org/x/tools/cmd/goimports@latest$(RESET)" && exit 1)
	@command -v golangci-lint >/dev/null 2>&1 || (echo "$(RED)ERROR: golangci-lint not found$(RESET)" && echo "$(YELLOW)Install with: curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin$(RESET)" && exit 1)
	@command -v govulncheck >/dev/null 2>&1 || (echo "$(RED)ERROR: govulncheck not found$(RESET)" && echo "$(YELLOW)Install with: go install golang.org/x/vuln/cmd/govulncheck@latest$(RESET)" && exit 1)
	@command -v nancy >/dev/null 2>&1 || (echo "$(RED)ERROR: nancy not found$(RESET)" && echo "$(YELLOW)Install with: go install github.com/sonatypecommunity/nancy@latest$(RESET)" && exit 1)
	@command -v goreleaser >/dev/null 2>&1 || (echo "$(RED)ERROR: goreleaser not found$(RESET)" && echo "$(YELLOW)Install with: go install github.com/goreleaser/goreleaser@latest$(RESET)" && exit 1)
	@command -v gocovmerge >/dev/null 2>&1 || (echo "$(RED)ERROR: gocovmerge not found$(RESET)" && echo "$(YELLOW)Install with: go install github.com/wadey/gocovmerge@latest$(RESET)" && exit 1)
	@echo "$(GREEN)✓ All Go dependencies found$(RESET)"

# Private target for go vet
.PHONY: _run-vet
_run-vet:
	@echo "$(YELLOW)  → Running go vet...$(RESET)"
	@go vet ./...

# Private target for goimports
.PHONY: _run-goimports-fix
_run-goimports-fix: deps-check
	@echo "$(YELLOW)  → Formatting Go code...$(RESET)"
	@goimports -w -local github.com/lattiam/lattiam .

# Private target for golangci-lint check
.PHONY: _run-golangci-lint-check
_run-golangci-lint-check: deps-check
	@echo "$(YELLOW)  → Running linting (validation only)...$(RESET)"
	@golangci-lint run ./...

# Private target for golangci-lint fix
.PHONY: _run-golangci-lint-fix
_run-golangci-lint-fix: deps-check
	@echo "$(YELLOW)  → Running linting with auto-fix...$(RESET)"
	@golangci-lint run --fix ./...

# Private target for govulncheck
.PHONY: _run-govulncheck
_run-govulncheck: deps-check
	@echo "$(YELLOW)  → Running govulncheck...$(RESET)"
	@govulncheck ./... || echo "$(YELLOW)Warning: govulncheck failed (possibly due to network restrictions)$(RESET)"

# Private target for nancy
.PHONY: _run-nancy
_run-nancy: deps-check
	@echo "$(YELLOW)  → Running nancy...$(RESET)"
	@go list -json -deps | nancy sleuth || echo "$(YELLOW)Warning: nancy found potential vulnerabilities$(RESET)"

# Private target for validating OpenAPI spec
.PHONY: validate-openapi
validate-openapi: ## Validate OpenAPI specification
	@echo "$(CYAN)Validating OpenAPI specification...$(RESET)"
	@./scripts/validate-openapi.sh

.PHONY: fix check security qa validate-openapi deps-check _run-vet _run-goimports-fix _run-golangci-lint-check _run-golangci-lint-fix _run-govulncheck _run-nancy
