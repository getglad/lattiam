// Package interfaces defines the core interfaces for the Lattiam system
package interfaces

import (
	"context"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

// ProviderConfigSource provides configuration for Terraform providers.
// This abstraction supports different configuration strategies (production, testing, etc.)
// through a unified interface that adapts to the deployment environment.
type ProviderConfigSource interface {
	// GetProviderConfig returns the configuration for a specific provider.
	// The configuration should be complete and ready to use, with no further
	// environment detection or merging required.
	GetProviderConfig(ctx context.Context, providerName string, version string, schema *tfprotov6.SchemaBlock) (ProviderConfig, error)

	// GetEnvironmentConfig returns environment-specific configuration that should
	// be applied to the provider process (e.g., environment variables).
	GetEnvironmentConfig(ctx context.Context, providerName string) (map[string]string, error)
}

// ProviderEndpointConfig defines endpoint configuration for a provider
type ProviderEndpointConfig struct {
	// EndpointURL is the base URL for the provider's API endpoints
	EndpointURL string

	// Services lists the services that should use the custom endpoint
	// (e.g., ["s3", "iam", "sts"] for AWS provider)
	Services []string

	// DisableSSL indicates whether SSL verification should be disabled
	DisableSSL bool
}

// ConfigurationContext provides additional context for configuration decisions
type ConfigurationContext struct {
	// ProviderBinaryPath is the path to the provider binary
	ProviderBinaryPath string

	// WorkingDirectory is the current working directory
	WorkingDirectory string

	// UserProvidedConfig is any configuration explicitly provided by the user
	UserProvidedConfig map[string]interface{}
}
