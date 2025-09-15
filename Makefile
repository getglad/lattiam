# Lattiam - Universal Infrastructure Management System
# Main Makefile that includes all modular components

# Default target
.DEFAULT_GOAL := help

# Include all modular makefiles in order
include mk/00-config.mk
include mk/01-build.mk
include mk/02-test.mk
include mk/03-lint.mk
include mk/04-mocks.mk
include mk/05-ci.mk
include mk/99-help.mk

# Phony targets
.PHONY: all clean help docs-generate docs-serve

# Main target - delegates to build module
# (Defined in mk/01-build.mk as: all: fix test build)

# Documentation generation targets
docs-generate: ## Generate OpenAPI documentation from code annotations
	@echo "Generating OpenAPI documentation..."
	@if ! which swag > /dev/null 2>&1; then \
		echo "Installing swag..."; \
		go install github.com/swaggo/swag/cmd/swag@latest; \
	fi
	@swag init -g cmd/api-server/main.go -o docs --parseInternal --parseDependency --exclude clones,scratch,test-all

docs-serve: docs-generate ## Generate docs and start server with Swagger UI
	@echo "Starting server with Swagger UI at http://localhost:8084/swagger/index.html"
	@./build/api-server || go run cmd/api-server/main.go