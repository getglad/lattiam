# Mock generation targets

# Install mockery if not present
.PHONY: install-mockery
install-mockery: ## Install mockery tool
	@echo "$(CYAN)Installing mockery...$(RESET)"
	@go install github.com/vektra/mockery/v2@latest

# Generate mocks for all interfaces
.PHONY: generate-mocks
generate-mocks: install-mockery ## Generate mocks for all interfaces
	@echo "$(CYAN)Generating mocks...$(RESET)"
	@mockery --dir=internal/interfaces --name=DeploymentService --output=internal/mocks --outpkg=mocks --filename=mock_deployment_service.go
	@mockery --dir=internal/interfaces --name=DeploymentQueue --output=internal/mocks --outpkg=mocks --filename=mock_deployment_queue_generated.go
	@mockery --dir=internal/interfaces --name=StateStore --output=internal/mocks --outpkg=mocks --filename=mock_state_store_generated.go
	@mockery --dir=internal/interfaces --name=DeploymentTracker --output=internal/mocks --outpkg=mocks --filename=mock_deployment_tracker.go
	@mockery --dir=internal/interfaces --name=WorkerPool --output=internal/mocks --outpkg=mocks --filename=mock_worker_pool.go
	@mockery --dir=internal/interfaces --name=ProviderLifecycleManager --output=internal/mocks --outpkg=mocks --filename=mock_provider_lifecycle_manager_generated.go
	@mockery --dir=internal/interfaces --name=UnifiedProvider --output=internal/mocks --outpkg=mocks --filename=mock_unified_provider_generated.go
	@mockery --dir=internal/interfaces --name=ComponentFactory --output=internal/mocks --outpkg=mocks --filename=mock_component_factory.go
	@mockery --dir=internal/interfaces --name=DeploymentMetadataStore --output=internal/mocks --outpkg=mocks --filename=mock_deployment_metadata_store.go
	@mockery --dir=internal/interfaces --name=TerraformStateFileStore --output=internal/mocks --outpkg=mocks --filename=mock_terraform_state_file_store.go
	@echo "$(GREEN)✓ Mocks generated successfully$(RESET)"

# Clean generated mocks
.PHONY: clean-mocks
clean-mocks: ## Remove generated mock files
	@echo "$(CYAN)Cleaning generated mocks...$(RESET)"
	@rm -f internal/mocks/*_generated.go
	@echo "$(GREEN)✓ Generated mocks cleaned$(RESET)"