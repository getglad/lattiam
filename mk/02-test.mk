# Testing targets - clean progression from unit to full test suite

# ============================================================================
# SINGLE TEST TARGETS
# ============================================================================

# Run unit tests with coverage
test: ## Run unit tests with coverage
	@echo "$(CYAN)Running unit tests...$(RESET)"
	@mkdir -p $(COVERAGE_DIR)
	@PACKAGES=$$(go list ./cmd/... ./internal/... ./pkg/... ./tests/unit/... | grep -v "internal/proto/tfplugin|tests/helpers"); \
	LATTIAM_TEST_MODE=true go test -v -coverprofile=$(COVERAGE_DIR)/coverage.out -covermode=atomic \
		-timeout=$(TEST_TIMEOUT) $$PACKAGES
	@$(MAKE) coverage-report

# Run integration tests with LocalStack and coverage
test-integration: _check-localstack ## Run integration tests with LocalStack (includes coverage)
	@echo "$(CYAN)Running integration tests...$(RESET)"
	@mkdir -p $(COVERAGE_DIR)
	@export AWS_ENDPOINT_URL="http://$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' $(docker ps -q -f name=devcontainer-localstack-1)):4566"; \
	LATTIAM_TEST_MODE=true LATTIAM_USE_LOCALSTACK=true \
		go test -tags="integration localstack" -v -timeout=300s \
		-coverpkg=./... -coverprofile=$(COVERAGE_DIR)/coverage.out -covermode=atomic ./tests/integration/...
	@echo "$(GREEN)✓ Integration tests complete with coverage$(RESET)"
	@$(MAKE) coverage-report

# Run unit tests with race detector
test-race: ## Run unit tests with race detector
	@echo "$(CYAN)Running unit tests with race detector...$(RESET)"
	@LATTIAM_TEST_MODE=true go test -v -race -timeout=$(TEST_TIMEOUT) \
		./cmd/... ./internal/... ./pkg/... ./tests/unit/...

# Run benchmarks
bench: ## Run benchmarks
	@echo "$(CYAN)Running benchmarks...$(RESET)"
	go test -bench=. -benchmem ./...
	@echo "$(GREEN)✓ Benchmarks complete$(RESET)"

# ============================================================================
# COMPOSITE TEST TARGET (test-all)
# ============================================================================

# Run unit + integration + OAT tests (full test suite)
test-all: _test-unit-cover _test-integration-cover _test-oat-cover _merge-coverage-all coverage-report ## Run unit + integration + OAT tests (complete test suite with coverage)
	@echo "$(GREEN)✓ Complete test suite passed with merged coverage$(RESET)"

# ============================================================================
# INTERNAL HELPER TARGETS FOR 'test-all'
# ============================================================================

# Private target to check for LocalStack
.PHONY: _check-localstack
_check-localstack:
	@echo "$(CYAN)Checking for LocalStack container...$(RESET)"
	@CONTAINER_ID=$(docker ps -q -f name=devcontainer-localstack-1); \
	if [ -z "$CONTAINER_ID" ]; then \
		echo "$(RED)Error: devcontainer-localstack-1 is not running.$(RESET)"; \
		exit 1; \
	fi; \
	IP_ADDRESS=$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' $CONTAINER_ID); \
	if [ -z "$IP_ADDRESS" ]; then \
		echo "$(RED)Error: Could not get IP address for devcontainer-localstack-1.$(RESET)"; \
		exit 1; \
	fi
	@echo "$(GREEN)✓ LocalStack container found.$(RESET)"

# Private target for unit tests with coverage for 'test-all'
.PHONY: _test-unit-cover
_test-unit-cover:
	@echo "$(CYAN)Running unit tests for full suite...$(RESET)"
	@mkdir -p $(COVERAGE_DIR)
	@LATTIAM_TEST_MODE=true go test -tags localstack -v -race -coverprofile=$(COVERAGE_DIR)/coverage-unit.out -covermode=atomic \
		-timeout=$(TEST_TIMEOUT) ./...

# Private target for integration tests with coverage for 'test-all'
.PHONY: _test-integration-cover
_test-integration-cover: _check-localstack
	@echo "$(CYAN)Running integration tests for full suite...$(RESET)"
	@export AWS_ENDPOINT_URL="http://$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' $(docker ps -q -f name=devcontainer-localstack-1)):4566"; \
	LATTIAM_TEST_MODE=true LATTIAM_USE_LOCALSTACK=true go test -tags="integration localstack" -v -timeout=300s \
		-coverpkg=./... -coverprofile=$(COVERAGE_DIR)/coverage-integration.out -covermode=atomic ./tests/integration/...

# Private target for OAT tests with coverage for 'test-all'
.PHONY: _test-oat-cover
_test-oat-cover: _check-localstack
	@echo "$(CYAN)Running OAT tests for lattiam CLI...$(RESET)"
	@export AWS_ENDPOINT_URL="http://$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' $(docker ps -q -f name=devcontainer-localstack-1)):4566"; \
	cd tests/integration && LATTIAM_USE_LOCALSTACK=true go test -v -run TestLattiamCLISuite \
		-coverpkg=../../... -coverprofile=../../$(COVERAGE_DIR)/coverage-oat.out -covermode=atomic ./...

# Private target to merge all coverage reports
.PHONY: _merge-coverage-all
_merge-coverage-all:
	@echo "$(CYAN)Merging all coverage reports...$(RESET)"
	@gocovmerge $(COVERAGE_DIR)/coverage-unit.out $(COVERAGE_DIR)/coverage-integration.out $(COVERAGE_DIR)/coverage-oat.out > $(COVERAGE_DIR)/coverage.out

# ============================================================================
# COVERAGE REPORTING
# ============================================================================

# Coverage analysis helper - used by other targets
coverage-report: ## Generate coverage report from existing coverage files (excludes protos/helpers)
	@if [ ! -f $(COVERAGE_DIR)/coverage.out ]; then \
		echo "$(RED)Error: No coverage file found at $(COVERAGE_DIR)/coverage.out$(RESET)"; \
		echo "$(YELLOW)Run 'make test', 'make test-integration', or 'make test-all' first$(RESET)"; \
		exit 1; \
	fi
	@echo "$(CYAN)Generating coverage report...$(RESET)"
	@go tool cover -html=$(COVERAGE_DIR)/coverage.out -o $(COVERAGE_DIR)/coverage.html 2>/dev/null || true
	@TOTAL_COV=$(go tool cover -func=$(COVERAGE_DIR)/coverage.out 2>/dev/null | tail -1 | awk '{print $3}' | sed 's/%//' || echo "0"); \
	FILTERED_COV=$(go tool cover -func=$(COVERAGE_DIR)/coverage.out 2>/dev/null | grep -v "internal/proto/tfplugin|tests/helpers" | \
		awk '$3 ~ /[0-9]+\.[0-9]%/{count++; gsub("%","",$3); sum+=$3} END {if(count>0) printf "%.1f", sum/count; else print "0"}'); \
	echo "$(CYAN)Total coverage: $TOTAL_COV% (including generated code)$(RESET)"; \
	echo "$(GREEN)Application coverage: $FILTERED_COV% (excluding protos/helpers)$(RESET)"; \
	echo "$(GREEN)✓ Coverage report: $(COVERAGE_DIR)/coverage.html$(RESET)"

.PHONY: test test-integration test-all bench coverage-report _check-localstack _test-unit-cover _test-integration-cover _test-oat-cover _merge-coverage-all