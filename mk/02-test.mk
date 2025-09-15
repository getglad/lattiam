# Testing targets - clean progression from unit to full test suite

# Define test coverage scope - using ./... to cover all packages
# Filtering will be done during reporting, not collection
COVERAGE_SCOPE := ./cmd/...,./internal/...,./pkg/...,./tests/...
# OAT tests only exist in distributed package, so use targeted scope
OAT_COVERAGE_SCOPE := ./internal/infra/distributed/...

# ============================================================================
# SINGLE TEST TARGETS
# ============================================================================

# Run unit tests with coverage
test: _ensure-docs ## Run unit tests with coverage
	@echo "$(CYAN)Running unit tests...$(RESET)"
	@mkdir -p $(COVERAGE_DIR)
	@LATTIAM_TEST_MODE=true go test -v -coverprofile=$(COVERAGE_DIR)/coverage-unit.out -covermode=atomic \
		-timeout=$(TEST_TIMEOUT) -coverpkg=$(COVERAGE_SCOPE) ./cmd/... ./internal/... ./pkg/... ./tests/...
	@echo "$(CYAN)Counting tests...$(RESET)"
	@TEST_COUNT=`go test -list '.*' ./cmd/... ./internal/... ./pkg/... ./tests/... 2>/dev/null | grep -c "^Test"`; \
	echo "$(GREEN)Found $$TEST_COUNT tests across all packages.$(RESET)"
	@$(MAKE) test-coverage TYPE=unit

# Run unit tests with race detector
test-race: _ensure-docs ## Run unit tests with race detector
	@echo "$(CYAN)Running unit tests with race detector...$(RESET)"
	@LATTIAM_TEST_MODE=true go test -v -race -timeout=$(TEST_TIMEOUT) \
		$(PACKAGES_FOR_TEST)

# Run benchmarks
bench: ## Run benchmarks
	@echo "$(CYAN)Running benchmarks...$(RESET)"
	go test -bench=. -benchmem ./cmd/... ./internal/... ./pkg/... ./tests/...
	@echo "$(GREEN)✓ Benchmarks complete$(RESET)"

# Run integration tests with LocalStack
test-integration: _check-localstack _ensure-docs ## Run integration tests with LocalStack (requires LocalStack container)
	@echo "$(CYAN)Running integration tests with LocalStack...$(RESET)"
	@echo "$(YELLOW)Warning: These tests download real provider binaries and may take several minutes$(RESET)"
	@echo "$(YELLOW)Requires internet access to download providers from HashiCorp registry$(RESET)"
	@echo "$(YELLOW)Provider downloads are minimized through shared caching - each provider version downloaded only once$(RESET)"
	@echo "$(CYAN)Test output will include timestamps to identify performance bottlenecks$(RESET)"
	@mkdir -p $(COVERAGE_DIR)
	@echo "[$$(date '+%H:%M:%S')] Starting integration test execution"
	@bash -c 'set -o pipefail; \
	LATTIAM_TEST_MODE=localstack LATTIAM_USE_LOCALSTACK=true \
		go test -tags="integration localstack" -v -p 1 \
		-coverpkg=$(COVERAGE_SCOPE) -coverprofile=$(COVERAGE_DIR)/coverage-integration.out -covermode=atomic \
		-timeout=15m ./cmd/... ./internal/... ./pkg/... ./tests/... 2>&1 | \
		while IFS= read -r line; do echo "[$$(date "+%H:%M:%S")] $$line"; done'
	@echo "[$$(date '+%H:%M:%S')] Integration tests completed"
	@echo "$(CYAN)Counting integration tests...$(RESET)"
	@TEST_COUNT=`go test -tags="integration localstack" -list '.*' ./cmd/... ./internal/... ./pkg/... ./tests/... 2>/dev/null | grep -c "^Test" || echo "0"`; \
	echo "$(GREEN)Found $$TEST_COUNT integration tests.$(RESET)"
	@$(MAKE) test-coverage TYPE=integration

# Run demo acceptance tests (real HTTP API tests with LocalStack)
test-demo: _check-localstack ## Run demo acceptance tests (real HTTP API + LocalStack integration)
	@echo "$(CYAN)Running demo acceptance tests...$(RESET)"
	@echo "$(YELLOW)These tests validate complete demo workflows using real HTTP API and LocalStack$(RESET)"
	@mkdir -p $(COVERAGE_DIR)
	@\
	LATTIAM_TEST_MODE=localstack LATTIAM_USE_LOCALSTACK=true \
		go test -tags="demo localstack" -v -run "TestAPIDemoSuite" \
		-coverpkg=$(COVERAGE_SCOPE) -coverprofile=$(COVERAGE_DIR)/coverage-demo.out -covermode=atomic ./tests/acceptance
	@echo "$(GREEN)✓ Demo acceptance tests complete with coverage$(RESET)"
	@$(MAKE) test-coverage TYPE=demo

# Run AWS backend acceptance tests (validates DynamoDB and S3 during operations)
test-aws-backend: _check-localstack ## Run AWS backend acceptance tests (validates DynamoDB + S3 content)
	@echo "$(CYAN)Running AWS backend acceptance tests...$(RESET)"
	@echo "$(YELLOW)These tests validate DynamoDB and S3 content during deployment operations$(RESET)"
	@mkdir -p $(COVERAGE_DIR)
	@\
	LATTIAM_TEST_MODE=localstack LATTIAM_USE_LOCALSTACK=true \
		go test -tags="aws localstack" -v -run "TestAWSBackendValidationSuite" \
		-coverpkg=$(COVERAGE_SCOPE) -coverprofile=$(COVERAGE_DIR)/coverage-aws-backend.out -covermode=atomic ./tests/acceptance
	@echo "$(GREEN)✓ AWS backend acceptance tests complete with coverage$(RESET)"
	@$(MAKE) test-coverage TYPE=aws-backend

# ============================================================================
# OAT (OPERATIONAL ACCEPTANCE TESTS)
# ============================================================================

# Run OAT tests - tests that manipulate containers (Redis stop/start, failure scenarios)
test-oat: _check-localstack ## Run operational acceptance tests (container manipulation, failure scenarios)
	@echo "$(CYAN)Running OAT tests with LocalStack...$(RESET)"
	@echo "$(YELLOW)Warning: These tests manipulate containers and test failure scenarios$(RESET)"
	@echo "$(YELLOW)Each test gets its own Redis container for isolation$(RESET)"
	@mkdir -p $(COVERAGE_DIR)
	@LATTIAM_TEST_MODE=localstack LATTIAM_USE_LOCALSTACK=true LATTIAM_USE_INDIVIDUAL_REDIS=true \
		go test -tags="oat localstack" -v -timeout=30m -parallel=1 \
		-coverpkg=$(OAT_COVERAGE_SCOPE) -coverprofile=$(COVERAGE_DIR)/coverage-oat.out -covermode=atomic \
		./internal/infra/distributed/...
	@echo "$(CYAN)Counting OAT tests...$(RESET)"
	@TEST_COUNT=`go test -tags="oat localstack" -list '.*' ./internal/infra/distributed/... 2>/dev/null | grep -c "^Test" || echo "0"`; \
	echo "$(GREEN)Found $$TEST_COUNT OAT tests.$(RESET)"
	@$(MAKE) test-coverage TYPE=oat

# ============================================================================
# COMPOSITE TEST TARGET (test-all)
# ============================================================================

# Run unit + integration + oat + demo tests (full test suite)
.PHONY: test-all
test-all: ## Run unit + integration + oat + demo + AWS backend tests (complete test suite with coverage)
	@set -e; \
	echo "$(CYAN)Starting complete test suite...$(RESET)"; \
	$(MAKE) _test-unit-cover && \
	$(MAKE) _test-integration-cover && \
	$(MAKE) _test-oat-cover && \
	$(MAKE) _test-demo-cover && \
	$(MAKE) _test-aws-backend-cover && \
	$(MAKE) _merge-coverage-all && \
	echo "$(CYAN)━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━$(RESET)" && \
	echo "$(GREEN)✅ TEST SUITE: ALL TESTS PASSED$(RESET)" && \
	echo "$(CYAN)━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━$(RESET)" || \
	(echo "$(CYAN)━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━$(RESET)" && \
	 echo "$(RED)❌ TEST SUITE: FAILED$(RESET)" && \
	 echo "$(CYAN)━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━$(RESET)" && \
	 exit 1)

# ============================================================================
# INTERNAL HELPER TARGETS
# ============================================================================

# Private target to check for LocalStack
.PHONY: _check-localstack
_check-localstack:
	@echo "$(CYAN)Checking for LocalStack...$(RESET)"
	@# In CI or when LATTIAM_USE_LOCALSTACK is set, testcontainers handles LocalStack
	@if [ "$$LATTIAM_USE_LOCALSTACK" = "true" ]; then \
		echo "$(GREEN)✓ Testcontainers will manage LocalStack automatically$(RESET)"; \
	else \
		CONTAINER_ID=$$(docker ps -q -f name=devcontainer-localstack-1 2>/dev/null || docker ps -q -f ancestor=localstack/localstack 2>/dev/null | head -1); \
		if [ -z "$$CONTAINER_ID" ]; then \
			echo "$(YELLOW)No LocalStack container found - testcontainers will start one if needed$(RESET)"; \
		else \
			CONTAINER_NAME=$$(docker inspect -f '{{.Name}}' $$CONTAINER_ID | sed 's|^/||'); \
			echo "$(GREEN)✓ Found existing LocalStack container: $$CONTAINER_NAME$(RESET)"; \
		fi; \
	fi

# Private target for unit tests with coverage for 'test-all'
.PHONY: _test-unit-cover
_test-unit-cover: _ensure-docs
	@echo "$(CYAN)Running unit tests for full suite...$(RESET)"
	@mkdir -p $(COVERAGE_DIR)
	@LATTIAM_TEST_MODE=true go test -v -race -coverprofile=$(COVERAGE_DIR)/coverage-unit.out -covermode=atomic \
		-timeout=$(TEST_TIMEOUT) -coverpkg=$(COVERAGE_SCOPE) ./cmd/... ./internal/... ./pkg/... ./tests/...

# Private target for integration tests with coverage for 'test-all'
.PHONY: _test-integration-cover
_test-integration-cover: _check-localstack _ensure-docs
	@echo "$(CYAN)Running integration tests for full suite...$(RESET)"
	@mkdir -p $(COVERAGE_DIR)
	@\
	LATTIAM_TEST_MODE=localstack LATTIAM_USE_LOCALSTACK=true \
		go test -tags="integration localstack" -v -p 1 \
		-coverpkg=$(COVERAGE_SCOPE) -coverprofile=$(COVERAGE_DIR)/coverage-integration.out -covermode=atomic \
		-timeout=15m ./cmd/... ./internal/... ./pkg/... ./tests/... 2>&1 | \
		while IFS= read -r line; do echo "[$$(date '+%H:%M:%S')] $$line"; done

# Private target for OAT tests with coverage for 'test-all'
.PHONY: _test-oat-cover
_test-oat-cover: _check-localstack
	@echo "$(CYAN)Running OAT tests for full suite...$(RESET)"
	@mkdir -p $(COVERAGE_DIR)
	@LATTIAM_TEST_MODE=localstack LATTIAM_USE_LOCALSTACK=true LATTIAM_USE_INDIVIDUAL_REDIS=true \
		go test -tags="oat localstack" -v -timeout=30m -parallel=1 \
		-coverpkg=$(OAT_COVERAGE_SCOPE) -coverprofile=$(COVERAGE_DIR)/coverage-oat.out -covermode=atomic \
		./internal/infra/distributed/...

# Private target for demo tests with coverage for 'test-all'
.PHONY: _test-demo-cover
_test-demo-cover: _check-localstack
	@echo "$(CYAN)Running demo tests for full suite...$(RESET)"
	@mkdir -p $(COVERAGE_DIR)
	@\
	LATTIAM_TEST_MODE=localstack LATTIAM_USE_LOCALSTACK=true go test -tags="demo localstack" -v -timeout=300s \
		-coverpkg=$(COVERAGE_SCOPE) -coverprofile=$(COVERAGE_DIR)/coverage-demo.out -covermode=atomic \
		./tests/acceptance

# Private target for AWS backend tests with coverage for 'test-all'
.PHONY: _test-aws-backend-cover
_test-aws-backend-cover: _check-localstack
	@echo "$(CYAN)Running AWS backend validation tests for full suite...$(RESET)"
	@mkdir -p $(COVERAGE_DIR)
	@\
	LATTIAM_TEST_MODE=localstack LATTIAM_USE_LOCALSTACK=true \
		go test -tags="aws localstack" -v -run "TestAWSBackendValidationSuite" \
		-coverpkg=$(COVERAGE_SCOPE) -coverprofile=$(COVERAGE_DIR)/coverage-aws-backend.out -covermode=atomic \
		./tests/acceptance

# Private target to merge all coverage reports using go test -coverpkg
.PHONY: _merge-coverage-all
_merge-coverage-all:
	@echo "$(CYAN)Running all tests with unified coverage...$(RESET)"
	@go test -coverpkg=./... -coverprofile=$(COVERAGE_DIR)/coverage-all.out -timeout $(TEST_TIMEOUT) \
		./cmd/... ./internal/... ./pkg/... ./tests/acceptance/... ./tests/integration/... ./tests/provider-isolation/...
	@$(MAKE) test-coverage TYPE=all

# ============================================================================
# COVERAGE REPORTING
# ============================================================================

# Generate coverage report - can be called independently or by test targets
.PHONY: test-coverage
test-coverage: ## Generate coverage report from existing coverage files (use TYPE=unit|integration|demo|all)
	@if [ -z "$(TYPE)" ]; then \
		echo "$(RED)Error: TYPE not set for _coverage-report target$(RESET)"; \
		exit 1; \
	fi
	@if [ ! -f $(COVERAGE_DIR)/coverage-$(TYPE).out ]; then \
		echo "$(RED)Error: No coverage file found at $(COVERAGE_DIR)/coverage-$(TYPE).out$(RESET)"; \
		echo "$(YELLOW)Run 'make test TYPE=$(TYPE)' first$(RESET)"; \
		exit 1; \
	fi
	@echo "$(CYAN)Generating coverage report for $(TYPE)...$(RESET)"
	@# Clean the coverage file by removing corrupted entries and fixing partial module paths
	@grep -E "^(mode:|github\.com/lattiam/lattiam)" $(COVERAGE_DIR)/coverage-$(TYPE).out > $(COVERAGE_DIR)/coverage-$(TYPE)-clean.out || cp $(COVERAGE_DIR)/coverage-$(TYPE).out $(COVERAGE_DIR)/coverage-$(TYPE)-clean.out
	@go tool cover -html=$(COVERAGE_DIR)/coverage-$(TYPE)-clean.out -o $(COVERAGE_DIR)/coverage-$(TYPE).html 2>/dev/null || true
	@LINES=$$(wc -l < $(COVERAGE_DIR)/coverage-$(TYPE)-clean.out); \
	if [ "$$LINES" -le 1 ]; then \
		echo "$(YELLOW)No coverage data available (tests may have failed or been skipped)$(RESET)"; \
		echo "$(GREEN)✓ Coverage report: $(COVERAGE_DIR)/coverage-$(TYPE).html$(RESET)"; \
	else \
		TOTAL_COV=$$(go tool cover -func=$(COVERAGE_DIR)/coverage-$(TYPE)-clean.out 2>/dev/null | tail -1 | awk '{print $$3}' | sed 's/%//' || echo "0.0"); \
		FILTERED_COV=$$(go tool cover -func=$(COVERAGE_DIR)/coverage-$(TYPE)-clean.out 2>/dev/null | grep -v "proto/tfplugin" | grep -v "tests/helpers" | \
			awk 'NF>=3 && $$3 ~ /[0-9]+\.[0-9]+%/{count++; gsub("%","",$$3); sum+=$$3} END {if(count>0) printf "%.1f", sum/count; else print "0.0"}'); \
		echo "$(CYAN)Total coverage for $(TYPE): $$TOTAL_COV% (including generated code)$(RESET)"; \
		echo "$(GREEN)Application coverage for $(TYPE): $$FILTERED_COV% (excluding protos/helpers)$(RESET)"; \
		echo "$(GREEN)✓ Coverage report: $(COVERAGE_DIR)/coverage-$(TYPE).html$(RESET)"; \
	fi


# Clean test artifacts
clean-test-artifacts: ## Clean up test logs, output files, and temporary test directories
	@echo "$(CYAN)Cleaning test artifacts...$(RESET)"
	@./scripts/clean-test-artifacts.sh
	@echo "$(GREEN)✓ Test artifacts cleaned$(RESET)"

.PHONY: test test-demo test-aws-backend test-all test-race bench test-integration test-coverage clean-test-artifacts
.PHONY: _check-localstack _test-unit-cover _test-integration-cover _test-demo-cover _test-aws-backend-cover _merge-coverage-all
.PHONY: load-test load-test-embedded load-test-distributed