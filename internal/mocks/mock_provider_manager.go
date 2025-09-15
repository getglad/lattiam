package mocks

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/internal/provider"
	"github.com/lattiam/lattiam/internal/provider/testing"
)

// MockProviderWrapper wraps a provider.Provider to implement interfaces.UnifiedProvider
type MockProviderWrapper struct {
	Provider provider.Provider
}

// Configure configures the mock provider with the given configuration
func (w *MockProviderWrapper) Configure(ctx context.Context, config map[string]interface{}) error {
	if err := w.Provider.Configure(ctx, config); err != nil {
		return fmt.Errorf("failed to configure provider: %w", err)
	}
	return nil
}

// CreateResource creates a resource with the mock provider
func (w *MockProviderWrapper) CreateResource(ctx context.Context, resourceType string, properties map[string]interface{}) (map[string]interface{}, error) {
	result, err := w.Provider.CreateResource(ctx, resourceType, properties)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}
	return result, nil
}

// ReadResource reads a resource from the mock provider
func (w *MockProviderWrapper) ReadResource(ctx context.Context, resourceType string, properties map[string]interface{}) (map[string]interface{}, error) {
	result, err := w.Provider.ReadResource(ctx, resourceType, properties)
	if err != nil {
		return nil, fmt.Errorf("failed to read resource: %w", err)
	}
	return result, nil
}

// UpdateResource updates a resource in the mock provider
func (w *MockProviderWrapper) UpdateResource(ctx context.Context, resourceType string, properties map[string]interface{}, currentState map[string]interface{}) (map[string]interface{}, error) {
	result, err := w.Provider.UpdateResource(ctx, resourceType, properties, currentState)
	if err != nil {
		return nil, fmt.Errorf("failed to update resource: %w", err)
	}
	return result, nil
}

// DeleteResource deletes a resource from the mock provider
func (w *MockProviderWrapper) DeleteResource(ctx context.Context, resourceType string, currentState map[string]interface{}) error {
	if err := w.Provider.DeleteResource(ctx, resourceType, currentState); err != nil {
		return fmt.Errorf("failed to delete resource: %w", err)
	}
	return nil
}

// ReadDataSource reads data from a data source
func (w *MockProviderWrapper) ReadDataSource(ctx context.Context, dataSourceType string, config map[string]interface{}) (map[string]interface{}, error) {
	result, err := w.Provider.ReadDataSource(ctx, dataSourceType, config)
	if err != nil {
		return nil, fmt.Errorf("failed to read data source: %w", err)
	}
	return result, nil
}

// PlanResourceChange plans a resource change
func (w *MockProviderWrapper) PlanResourceChange(_ context.Context, resourceType string, currentState, proposedState map[string]interface{}) (*interfaces.ResourcePlan, error) {
	// Return a basic plan
	return &interfaces.ResourcePlan{
		ResourceType:  resourceType,
		CurrentState:  currentState,
		ProposedState: proposedState,
		PlannedState:  proposedState,
		Actions:       []string{"create"},
	}, nil
}

// ApplyResourceChange applies a resource change
func (w *MockProviderWrapper) ApplyResourceChange(ctx context.Context, resourceType string, plan *interfaces.ResourcePlan) (map[string]interface{}, error) {
	result, err := w.Provider.CreateResource(ctx, resourceType, plan.ProposedState)
	if err != nil {
		return nil, fmt.Errorf("failed to apply resource change: %w", err)
	}
	return result, nil
}

// IsHealthy checks if the mock provider is healthy
func (w *MockProviderWrapper) IsHealthy() bool {
	// Provider interface doesn't have IsHealthy, so assume healthy if not closed
	return true
}

// Close closes the mock provider
func (w *MockProviderWrapper) Close() error {
	if err := w.Provider.Close(); err != nil {
		return fmt.Errorf("failed to close provider: %w", err)
	}
	return nil
}

// GetLastDiagnostics retrieves diagnostics from the last operation
func (w *MockProviderWrapper) GetLastDiagnostics() []interfaces.Diagnostic {
	// Mock implementation - return empty diagnostics
	return []interfaces.Diagnostic{}
}

// GetCapabilities returns provider capabilities
func (w *MockProviderWrapper) GetCapabilities() interfaces.ProviderCapabilities {
	// Mock implementation - return basic capabilities
	return interfaces.ProviderCapabilities{
		ProtocolVersion:   6,
		SupportsRetry:     false,
		SupportsDeferred:  false,
		SupportsFunctions: false,
		MaxRetryAttempts:  1,
		CurrentRetryDelay: "0s",
	}
}

// MockProviderLifecycleManager implements only ProviderLifecycleManager
type MockProviderLifecycleManager struct {
	providers        map[string]provider.Provider
	shouldFail       bool
	failMessage      string
	getProviderCalls int
	delay            time.Duration
}

// NewMockProviderLifecycleManager creates a new mock provider lifecycle manager
func NewMockProviderLifecycleManager() *MockProviderLifecycleManager {
	return &MockProviderLifecycleManager{
		providers: make(map[string]provider.Provider),
	}
}

// GetProvider returns a mock provider for testing
func (m *MockProviderLifecycleManager) GetProvider(ctx context.Context, deploymentID, name, version string, config interfaces.ProviderConfig, _ string) (interfaces.UnifiedProvider, error) {
	m.getProviderCalls++

	// Add delay if configured
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
			// Delay completed
		case <-ctx.Done():
			// Context canceled during delay
			return nil, fmt.Errorf("context canceled during delay: %w", ctx.Err())
		}
	}

	if m.shouldFail {
		return nil, errors.New(m.failMessage)
	}

	// Check for cancellation
	if ctx.Err() != nil {
		return nil, fmt.Errorf("context canceled: %w", ctx.Err())
	}

	key := fmt.Sprintf("%s:%s:%s", deploymentID, name, version)

	// Return existing provider if available
	if p, exists := m.providers[key]; exists {
		return &MockProviderWrapper{Provider: p}, nil
	}

	// Create new mock provider based on name
	var mockProvider provider.Provider
	switch name {
	case "aws":
		mockProvider = testing.NewAWSMockProvider()
	case "random":
		mockProvider = testing.NewRandomMockProvider()
	default:
		mockProvider = testing.NewMockProvider(name, version)
	}

	// Configure the provider if needed
	if config != nil {
		if providerConfig, exists := config[name]; exists {
			if err := mockProvider.Configure(ctx, providerConfig); err != nil {
				return nil, fmt.Errorf("configure provider %s: %w", name, err)
			}
		}
	}

	// Store and return
	m.providers[key] = mockProvider
	return &MockProviderWrapper{Provider: mockProvider}, nil
}

// ReleaseProvider releases a provider instance
func (m *MockProviderLifecycleManager) ReleaseProvider(deploymentID, name string) error {
	// Simple implementation - just remove from map
	for key := range m.providers {
		// Check if key matches the deployment and provider name pattern
		if strings.HasPrefix(key, deploymentID+":"+name+":") || key == deploymentID+":"+name {
			delete(m.providers, key)
		}
	}
	return nil
}

// ShutdownDeployment shuts down all providers for a specific deployment
// ShutdownDeployment shuts down all providers for a deployment
func (m *MockProviderLifecycleManager) ShutdownDeployment(deploymentID string) error {
	// Simple implementation - remove all providers for this deployment
	for key := range m.providers {
		if strings.HasPrefix(key, deploymentID+":") {
			delete(m.providers, key)
		}
	}
	return nil
}

// SetShouldFail sets whether the next GetProvider call should fail
func (m *MockProviderLifecycleManager) SetShouldFail(shouldFail bool, message string) {
	m.shouldFail = shouldFail
	m.failMessage = message
}

// SetDelay sets a delay for GetProvider calls
func (m *MockProviderLifecycleManager) SetDelay(delay time.Duration) {
	m.delay = delay
}

// SetValidateBucketNames sets whether to validate bucket names
func (m *MockProviderLifecycleManager) SetValidateBucketNames(validate bool) {
	// Configure AWS mock provider if it exists
	for _, p := range m.providers {
		if mockProvider, ok := p.(*testing.MockProvider); ok {
			mockProvider.SetValidateBucketNames(validate)
		}
	}
}

// MockProviderManager implements full ProviderManager using composition
// This allows gradual migration while maintaining compatibility
type MockProviderManager struct {
	*MockProviderLifecycleManager
	schemas            map[string]*interfaces.ProviderSchema
	shouldFailShutdown bool
	calls              []Call // Track method calls for testing
}

// SetShouldFail sets whether the manager should fail with a default error message
func (m *MockProviderManager) SetShouldFail(shouldFail bool) {
	m.MockProviderLifecycleManager.SetShouldFail(shouldFail, "mock provider manager failure")
}

// SetDelay sets a delay for operations
func (m *MockProviderManager) SetDelay(delay time.Duration) {
	m.MockProviderLifecycleManager.SetDelay(delay)
}

// SetValidateBucketNames sets whether to validate S3 bucket names
func (m *MockProviderManager) SetValidateBucketNames(validate bool) {
	m.MockProviderLifecycleManager.SetValidateBucketNames(validate)
}

// AddProvider adds a specific provider instance (for tests that need custom providers)
func (m *MockProviderManager) AddProvider(name, version string, prov provider.Provider) {
	key := fmt.Sprintf("%s:%s", name, version)
	m.providers[key] = prov
}

// ClearDeleteCalls clears delete call tracking on all providers
func (m *MockProviderManager) ClearDeleteCalls() {
	for _, provider := range m.providers {
		if mockProvider, ok := provider.(*testing.MockProvider); ok {
			mockProvider.ClearDeleteCalls()
		}
	}
}

// GetDeleteCalls returns all delete calls from all providers
func (m *MockProviderManager) GetDeleteCalls() []string {
	var deleteCalls []string
	for key, provider := range m.providers {
		if mockProvider, ok := provider.(*testing.MockProvider); ok {
			calls := mockProvider.GetDeleteCalls()
			for _, call := range calls {
				deleteCalls = append(deleteCalls, fmt.Sprintf("%s: %s", key, call))
			}
		}
	}
	return deleteCalls
}

// NewMockProviderManager creates a new mock provider manager for testing
func NewMockProviderManager() *MockProviderManager {
	return &MockProviderManager{
		MockProviderLifecycleManager: NewMockProviderLifecycleManager(),
		schemas:                      make(map[string]*interfaces.ProviderSchema),
		calls:                        []Call{},
	}
}

// ListActiveProviders returns the list of active providers
func (m *MockProviderManager) ListActiveProviders() []interfaces.ProviderInfo {
	infos := make([]interfaces.ProviderInfo, 0, len(m.providers))
	for key := range m.providers {
		infos = append(infos, interfaces.ProviderInfo{
			Name:    key,
			Version: "mock",
			Status:  interfaces.ProviderStatusActive,
		})
	}
	return infos
}

// IsHealthy returns whether the provider manager is healthy
func (m *MockProviderManager) IsHealthy() bool {
	return !m.shouldFail
}

// GetMetrics returns provider manager metrics
func (m *MockProviderManager) GetMetrics() interfaces.ProviderManagerMetrics {
	return interfaces.ProviderManagerMetrics{
		ActiveProviders: len(m.providers),
		TotalProviders:  m.getProviderCalls,
	}
}

// Initialize initializes the provider manager
func (m *MockProviderManager) Initialize() error {
	return nil
}

// Shutdown shuts down the provider manager
func (m *MockProviderManager) Shutdown() error {
	m.calls = append(m.calls, NewCall("Shutdown", nil))
	if m.shouldFailShutdown {
		return fmt.Errorf("mock shutdown failure")
	}
	m.providers = make(map[string]provider.Provider)
	return nil
}

// SetShouldFailShutdown sets whether shutdown should fail
func (m *MockProviderManager) SetShouldFailShutdown(shouldFail bool) {
	m.shouldFailShutdown = shouldFail
}

// GetCalls returns the list of method calls made to the mock
func (m *MockProviderManager) GetCalls() []Call {
	return m.calls
}
