package mocks

import (
	"context"

	"github.com/lattiam/lattiam/internal/interfaces"
)

// MockResourceOperations is a minimal mock for resource CRUD operations
type MockResourceOperations struct {
	CreateFunc func(ctx context.Context, resourceType string, properties map[string]interface{}) (map[string]interface{}, error)
	ReadFunc   func(ctx context.Context, resourceType string, properties map[string]interface{}) (map[string]interface{}, error)
	UpdateFunc func(ctx context.Context, resourceType string, properties map[string]interface{}, currentState map[string]interface{}) (map[string]interface{}, error)
	DeleteFunc func(ctx context.Context, resourceType string, currentState map[string]interface{}) error
}

// CreateResource creates a resource with the given properties
func (m *MockResourceOperations) CreateResource(ctx context.Context, resourceType string, properties map[string]interface{}) (map[string]interface{}, error) {
	if m.CreateFunc != nil {
		return m.CreateFunc(ctx, resourceType, properties)
	}
	return properties, nil
}

// ReadResource reads a resource's current state
func (m *MockResourceOperations) ReadResource(ctx context.Context, resourceType string, properties map[string]interface{}) (map[string]interface{}, error) {
	if m.ReadFunc != nil {
		return m.ReadFunc(ctx, resourceType, properties)
	}
	return properties, nil
}

// UpdateResource updates a resource with new properties
func (m *MockResourceOperations) UpdateResource(ctx context.Context, resourceType string, properties map[string]interface{}, currentState map[string]interface{}) (map[string]interface{}, error) {
	if m.UpdateFunc != nil {
		return m.UpdateFunc(ctx, resourceType, properties, currentState)
	}
	return properties, nil
}

// DeleteResource deletes a resource
func (m *MockResourceOperations) DeleteResource(ctx context.Context, resourceType string, currentState map[string]interface{}) error {
	if m.DeleteFunc != nil {
		return m.DeleteFunc(ctx, resourceType, currentState)
	}
	return nil
}

// MockProviderMonitor is a minimal mock for provider monitoring
type MockProviderMonitor struct {
	ActiveProviders []interfaces.ProviderInfo
	Healthy         bool
	Metrics         interfaces.ProviderManagerMetrics
}

// ListActiveProviders returns the list of active providers
func (m *MockProviderMonitor) ListActiveProviders() []interfaces.ProviderInfo {
	return m.ActiveProviders
}

// IsHealthy returns whether the provider monitor is healthy
func (m *MockProviderMonitor) IsHealthy() bool {
	return m.Healthy
}

// GetMetrics returns provider manager metrics
func (m *MockProviderMonitor) GetMetrics() interfaces.ProviderManagerMetrics {
	return m.Metrics
}

// CompositeProviderMock combines multiple interfaces for testing
// It implements UnifiedProvider by combining all interface mocks
type CompositeProviderMock struct {
	MockResourceOperations

	// DataSourceReader methods
	ReadDataSourceFunc func(ctx context.Context, dataSourceType string, config map[string]interface{}) (map[string]interface{}, error)

	// ResourcePlanner methods
	PlanResourceChangeFunc  func(ctx context.Context, resourceType string, currentState, proposedState map[string]interface{}) (*interfaces.ResourcePlan, error)
	ApplyResourceChangeFunc func(ctx context.Context, resourceType string, plan *interfaces.ResourcePlan) (map[string]interface{}, error)

	// Additional methods to satisfy UnifiedProvider interface
	ConfigureFunc func(ctx context.Context, config map[string]interface{}) error
	CloseFunc     func() error
}

// Configure implements UnifiedProvider interface
func (c *CompositeProviderMock) Configure(ctx context.Context, config map[string]interface{}) error {
	if c.ConfigureFunc != nil {
		return c.ConfigureFunc(ctx, config)
	}
	return nil
}

// Close implements UnifiedProvider interface
func (c *CompositeProviderMock) Close() error {
	if c.CloseFunc != nil {
		return c.CloseFunc()
	}
	return nil
}

// PlanResourceChange implements ResourcePlanner interface
func (c *CompositeProviderMock) PlanResourceChange(ctx context.Context, resourceType string, currentState, proposedState map[string]interface{}) (*interfaces.ResourcePlan, error) {
	if c.PlanResourceChangeFunc != nil {
		return c.PlanResourceChangeFunc(ctx, resourceType, currentState, proposedState)
	}
	return &interfaces.ResourcePlan{
		ResourceType:  resourceType,
		Action:        interfaces.PlanActionCreate,
		CurrentState:  currentState,
		ProposedState: proposedState,
		PlannedState:  proposedState,
	}, nil
}

// ApplyResourceChange implements ResourcePlanner interface
func (c *CompositeProviderMock) ApplyResourceChange(ctx context.Context, resourceType string, plan *interfaces.ResourcePlan) (map[string]interface{}, error) {
	if c.ApplyResourceChangeFunc != nil {
		return c.ApplyResourceChangeFunc(ctx, resourceType, plan)
	}
	return plan.PlannedState, nil
}

// ReadDataSource implements DataSourceReader interface
func (c *CompositeProviderMock) ReadDataSource(ctx context.Context, dataSourceType string, config map[string]interface{}) (map[string]interface{}, error) {
	if c.ReadDataSourceFunc != nil {
		return c.ReadDataSourceFunc(ctx, dataSourceType, config)
	}
	return config, nil
}

// Example usage in tests:
//
// func TestSomething(t *testing.T) {
//     // Only mock what you need
//     health := &MockHealthChecker{Healthy: true}
//
//     // Or compose multiple interfaces
//     provider := &CompositeProviderMock{
//         MockHealthChecker: MockHealthChecker{Healthy: true},
//         MockCapabilityReporter: MockCapabilityReporter{
//             Capabilities: interfaces.ProviderCapabilities{
//                 ProtocolVersion: 6,
//             },
//         },
//     }
// }
