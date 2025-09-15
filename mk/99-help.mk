# Help target - must be last to ensure all variables are loaded

# Phony target for help
.PHONY: help clean-artifacts

# Default goal
.DEFAULT_GOAL := help

# Clean up test artifacts
clean-artifacts: ## Clean up test artifacts, logs, and temporary files
	@./scripts/cleanup-artifacts.sh

# Help target - generates a clean, organized help message from comments
help:
	@echo ""
	@echo "  $(YELLOW)Lattiam - Universal Infrastructure Management System$(RESET)"
	@echo ""
	@echo "  $(CYAN)Usage:$(RESET)"
	@echo "    make [target]"
	@echo ""
	@echo "  $(CYAN)Common Commands:$(RESET)"
	@echo "    $(GREEN)all                      $(RESET) Run fix, test, and build (complete workflow)"
	@echo "    $(GREEN)build                    $(RESET) Build unified CLI (primary component)"
	@echo "    $(GREEN)test                     $(RESET) Run unit tests with coverage"
	@echo "    $(GREEN)fix                      $(RESET) Auto-fix formatting, linting, vet, and security issues"
	@echo "    $(GREEN)qa                       $(RESET) Run complete quality assurance pipeline"
	@echo ""
	@echo "  $(CYAN)Build Targets:$(RESET)"
	@grep -h -E '^(build.*|install|clean|deps|release):.*?## .*$$' $(MAKEFILE_LIST) | \
		sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "    $(GREEN)%-25s$(RESET) %s\n", $$1, $$2}'
	@echo ""
	@echo "  $(CYAN)Testing Targets:$(RESET)"
	@grep -h -E '^test[^:]*:.*?## .*$$' $(MAKEFILE_LIST) | \
		sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "    $(GREEN)%-25s$(RESET) %s\n", $$1, $$2}'
	@echo ""
	@echo "  $(CYAN)Quality Assurance:$(RESET)"
	@grep -h -E '^(check|fix|qa|validate.*|security|bench|coverage.*|deps-check):.*?## .*$$' $(MAKEFILE_LIST) | \
		sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "    $(GREEN)%-25s$(RESET) %s\n", $$1, $$2}'
	@echo ""
	@echo "  $(CYAN)Examples:$(RESET)"
	@echo "    make all                  # Fix, test, and build the project"
	@echo "    make test-all             # Run the complete test suite"
	@echo "    make qa                   # Run all quality assurance checks"
	@echo "    make fix                  # Auto-fix formatting and linting issues"
	@echo "    make build                # Build the CLI binary"
	@echo "    make test                 # Run unit tests with coverage"
	@echo ""
