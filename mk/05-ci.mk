# Local CI testing
# This module provides helper targets for testing CI locally with act

# ============================================================================
# LOCAL CI TESTING WITH ACT
# ============================================================================
# NOTE: Optimized for M-series Macs (ARM64 architecture)
# The configuration defaults to ARM64 for better performance on Apple Silicon

# Run complete CI pipeline locally (all critical jobs)
ci-local: ## Run complete CI pipeline locally with act (all critical jobs)
	@echo "$(CYAN)Running complete CI pipeline locally with act...$(RESET)"
	@echo "$(YELLOW)→ Running PR validation...$(RESET)"
	@./scripts/local-ci.sh --workflow ci.yml --job pr-validation || true
	@echo "$(YELLOW)→ Running integration tests...$(RESET)"
	@./scripts/local-ci.sh --workflow ci.yml --job integration-tests || true
	@echo "$(YELLOW)→ Running quality assurance...$(RESET)"
	@./scripts/local-ci.sh --workflow ci.yml --job quality-assurance || true
	@echo "$(YELLOW)→ Running race detection...$(RESET)"
	@./scripts/local-ci.sh --workflow ci.yml --job race-detection || true
	@echo "$(GREEN)✅ Complete local CI pipeline finished$(RESET)"

# Run only integration tests locally (faster, focused testing)
ci-local-integration: ## Run only integration tests locally (faster alternative to full CI)
	@echo "$(CYAN)Running integration tests locally with act...$(RESET)"
	@./scripts/local-ci.sh --workflow ci.yml --job integration-tests
	@echo "$(GREEN)✅ Integration tests completed$(RESET)"

# Run specific CI job locally
ci-local-qa: ## Run quality assurance CI job locally
	@echo "$(CYAN)Running quality assurance locally with act...$(RESET)"
	@./scripts/local-ci.sh --workflow ci.yml --job quality-assurance
	@echo "$(GREEN)✅ Quality assurance completed$(RESET)"

ci-local-pr: ## Run PR validation CI job locally
	@echo "$(CYAN)Running PR validation locally with act...$(RESET)"
	@./scripts/local-ci.sh --workflow ci.yml --job pr-validation
	@echo "$(GREEN)✅ PR validation completed$(RESET)"

ci-local-race: ## Run race detection CI job locally
	@echo "$(CYAN)Running race detection locally with act...$(RESET)"
	@./scripts/local-ci.sh --workflow ci.yml --job race-detection
	@echo "$(GREEN)✅ Race detection completed$(RESET)"

.PHONY: ci-local ci-local-integration ci-local-qa ci-local-pr ci-local-race
