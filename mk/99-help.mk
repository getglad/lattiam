# Help target - must be last to ensure all variables are loaded

# Phony target for help
.PHONY: help

# Default goal
.DEFAULT_GOAL := help

# Help target - generates a clean, organized help message from comments
help:
	@echo ""
	@echo "  $(YELLOW)Lattiam - Universal Infrastructure Management System$(RESET)"
	@echo ""
	@echo "  $(CYAN)Usage:$(RESET)"
	@echo "    make [target]"
	@echo ""
	@echo "  $(CYAN)Main Targets:$(RESET)"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "    $(GREEN)%-20s$(RESET) %s\n", $$1, $$2}'
	@echo ""
	@echo "  $(CYAN)Examples:$(RESET)"
	@echo "    make all           # Fix, test, and build the project"
	@echo "    make test-all      # Run the complete test suite"
	@echo "    make qa            # Run all quality assurance checks"
	@echo ""
