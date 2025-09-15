package interfaces

import (
	"context"
)

// --- ProviderManager Interfaces ---

// UnifiedProvider combines all provider interfaces into one.
// This is returned by GetProvider and provides all provider capabilities.
// Concrete implementations are provided by the system package to avoid circular dependencies.
type UnifiedProvider interface {
	ResourceOperations
	DataSourceReader
	ResourcePlanner

	// Additional methods
	Configure(ctx context.Context, config map[string]interface{}) error
	Close() error
}

// ProviderLifecycleManager defines the core interface for acquiring and releasing provider instances.
// GetProvider returns UnifiedProvider which provides all provider capabilities.
// Providers are isolated per deployment to ensure process separation and state isolation.
type ProviderLifecycleManager interface {
	GetProvider(ctx context.Context, deploymentID, name string, version string, config ProviderConfig, resourceType string) (UnifiedProvider, error)
	ReleaseProvider(deploymentID, name string) error
	ShutdownDeployment(deploymentID string) error
}

// ProviderMonitor defines the interface for monitoring and operational tasks.
type ProviderMonitor interface {
	ListActiveProviders() []ProviderInfo
	IsHealthy() bool
	GetMetrics() ProviderManagerMetrics
}

// --- Provider Interfaces ---

// ResourceOperations defines the core CRUD operations for a resource.
type ResourceOperations interface {
	CreateResource(ctx context.Context, resourceType string, properties map[string]interface{}) (map[string]interface{}, error)
	ReadResource(ctx context.Context, resourceType string, properties map[string]interface{}) (map[string]interface{}, error)
	UpdateResource(ctx context.Context, resourceType string, properties map[string]interface{}, currentState map[string]interface{}) (map[string]interface{}, error)
	DeleteResource(ctx context.Context, resourceType string, currentState map[string]interface{}) error
}

// DataSourceReader defines the interface for reading data sources.
type DataSourceReader interface {
	ReadDataSource(ctx context.Context, dataSourceType string, config map[string]interface{}) (map[string]interface{}, error)
}

// ResourcePlanner defines the interface for planning resource changes.
type ResourcePlanner interface {
	PlanResourceChange(ctx context.Context, resourceType string, currentState, proposedState map[string]interface{}) (*ResourcePlan, error)
	ApplyResourceChange(ctx context.Context, resourceType string, plan *ResourcePlan) (map[string]interface{}, error)
}
