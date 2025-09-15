package deployment

import (
	"os"
	"strings"

	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/internal/utils"
	"github.com/lattiam/lattiam/pkg/provider/protocol"
)

// ProviderVersionResolver handles provider version resolution from multiple sources
type ProviderVersionResolver struct {
	// Provider versions from terraform.required_providers block
	terraformVersions map[string]string
}

// NewProviderVersionResolver creates a new provider version resolver
func NewProviderVersionResolver(terraformJSON map[string]interface{}) *ProviderVersionResolver {
	resolver := &ProviderVersionResolver{
		terraformVersions: make(map[string]string),
	}

	// Parse terraform.required_providers block if present
	if terraform, ok := terraformJSON["terraform"].(map[string]interface{}); ok {
		if requiredProviders, ok := terraform["required_providers"].(map[string]interface{}); ok {
			for provider, config := range requiredProviders {
				if providerConfig, ok := config.(map[string]interface{}); ok {
					if version, ok := providerConfig["version"].(string); ok {
						resolver.terraformVersions[provider] = version
					}
				}
			}
		}
	}

	return resolver
}

// GetProviderVersion resolves the version for a provider from multiple sources
// Priority order:
// 1. Override passed directly (highest priority)
// 2. Environment variable (e.g., AWS_PROVIDER_VERSION)
// 3. Terraform required_providers block
// 4. Default hardcoded version
func (r *ProviderVersionResolver) GetProviderVersion(providerName, override string) string {
	// 1. Direct override has highest priority
	if override != "" {
		debugLogger := protocol.GetDebugLogger()
		debugLogger.Logf("provider-version", "Using override version for %s: %s", providerName, override)
		return override
	}

	// 2. Check environment variable
	envVar := strings.ToUpper(providerName) + "_PROVIDER_VERSION"
	if version := os.Getenv(envVar); version != "" {
		debugLogger := protocol.GetDebugLogger()
		debugLogger.Logf("provider-version", "Using env version for %s: %s", providerName, version)
		return version
	}

	// 3. Check terraform.required_providers
	if version, ok := r.terraformVersions[providerName]; ok {
		debugLogger := protocol.GetDebugLogger()
		debugLogger.Logf("provider-version", "Using terraform JSON version for %s: %s", providerName, version)
		return version
	}

	// 4. Fall back to defaults
	defaultVersion := getDefaultProviderVersion(providerName)
	debugLogger := protocol.GetDebugLogger()
	debugLogger.Logf("provider-version", "Using default version for %s: %s", providerName, defaultVersion)
	return defaultVersion
}

// getDefaultProviderVersion returns the default version for a provider
func getDefaultProviderVersion(providerName string) string {
	switch providerName {
	case "aws":
		return "6.0.0" // Default major version
	case "azurerm":
		return "3.85.0"
	case "google":
		return "5.10.0"
	case "null":
		return "3.2.2"
	case "random":
		return "3.6.0"
	default:
		return "unspecified"
	}
}

// GetProviderVersionsMap returns all provider versions for a deployment
func (r *ProviderVersionResolver) GetProviderVersionsMap(resources []interfaces.Resource) map[string]string {
	versions := make(map[string]string)

	// Get unique providers from resources
	providers := make(map[string]bool)
	for _, resource := range resources {
		providerName := utils.ExtractProviderName(resource.Type)
		providers[providerName] = true
	}

	// Resolve version for each provider
	for provider := range providers {
		versions[provider] = r.GetProviderVersion(provider, "")
	}

	return versions
}
