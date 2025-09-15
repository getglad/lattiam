package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"github.com/lattiam/lattiam/internal/interfaces"
)

// Info contains basic information about a provider
type Info struct {
	Name    string
	Version string
	Status  string
}

// Provider represents a Terraform provider instance
type Provider interface {
	// Name returns the provider name
	Name() string

	// Version returns the provider version
	Version() string

	// Configure configures the provider with the given configuration
	Configure(ctx context.Context, config map[string]interface{}) error

	// GetSchema retrieves the provider's schema
	GetSchema(ctx context.Context) (*tfprotov6.GetProviderSchemaResponse, error)

	// CreateResource creates a resource
	CreateResource(ctx context.Context, resourceType string, config map[string]interface{}) (map[string]interface{}, error)

	// ReadResource reads a resource
	ReadResource(ctx context.Context, resourceType string, state map[string]interface{}) (map[string]interface{}, error)

	// UpdateResource updates a resource
	UpdateResource(ctx context.Context, resourceType string, priorState,
		config map[string]interface{}) (map[string]interface{}, error)

	// DeleteResource deletes a resource
	DeleteResource(ctx context.Context, resourceType string, state map[string]interface{}) error

	// PlanResourceChange plans changes to a resource
	PlanResourceChange(ctx context.Context, resourceType string, priorState, config map[string]interface{}) (*tfprotov6.PlanResourceChangeResponse, error)

	// ReadDataSource reads a data source
	ReadDataSource(ctx context.Context, dataSourceType string, config map[string]interface{}) (map[string]interface{}, error)

	// Close closes the provider connection
	Close() error
}

// Manager manages provider instances and lifecycle
type Manager interface {
	// GetProvider retrieves or creates a provider instance with configuration-aware caching
	// This enables provider pooling and reuse based on type, version, and configuration (Issue #24 fix)
	GetProvider(ctx context.Context, deploymentID, name, version string, config interfaces.ProviderConfig, resourceType string) (Provider, error)

	// ListProviders returns information about available providers
	ListProviders() []Info

	// DownloadProvider downloads a provider if not already available
	DownloadProvider(ctx context.Context, name, version string) error

	// Close closes all managed providers
	Close() error
}

// Instance represents a running provider instance
type Instance struct {
	Name    string
	Version string
	Address string
	Process interface{} // os.Process or similar
}

// BinaryFetcher handles provider binary downloads
type BinaryFetcher interface {
	// Download downloads a provider binary
	Download(ctx context.Context, name, version, platform string) (string, error)

	// GetLocalPath returns the local path for a provider
	GetLocalPath(name, version, platform string) string

	// IsAvailable checks if a provider is already downloaded
	IsAvailable(name, version, platform string) bool
}
