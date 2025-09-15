// Package provider provides configuration management for Terraform providers.
package provider

import (
	"context"
	"strings"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

// Type names used in schema definitions
const (
	typeString = "string"
)

// Configurator handles provider-specific configuration
type Configurator struct {
	// Provider defaults loaded from configuration
	defaultConfigs map[string]Defaults
}

// Defaults defines default configuration for a provider
type Defaults struct {
	// Required attributes that must be set
	RequiredAttributes map[string]interface{} `yaml:"required_attributes"`

	// Optional attributes with their defaults
	OptionalAttributes map[string]interface{} `yaml:"optional_attributes"`

	// Endpoint configuration pattern (for multi-service providers like AWS)
	EndpointServices []string `yaml:"endpoint_services,omitempty"`
}

// EnvironmentConfig allows environment-specific overrides
type EnvironmentConfig struct {
	// Attributes to override
	AttributeOverrides map[string]interface{}

	// Custom endpoint URL (for testing environments)
	EndpointURL string

	// Whether to disable SSL verification (for testing)
	DisableSSL bool
}

// NewProviderConfigurator creates a new provider configurator
func NewProviderConfigurator() *Configurator {
	return &Configurator{
		defaultConfigs: getDefaultProviderConfigs(),
	}
}

// ConfigureProvider configures a provider based on schema and environment
func (pc *Configurator) ConfigureProvider(
	_ context.Context, // ctx - reserved for future use
	providerName string,
	schema *tfprotov6.SchemaBlock,
	customConfig map[string]interface{},
	envConfig *EnvironmentConfig,
) (map[string]interface{}, error) {
	// Start with schema-based configuration
	config := pc.buildSchemaBasedConfig(schema)

	// Apply provider-specific defaults
	if defaults, exists := pc.defaultConfigs[providerName]; exists {
		pc.applyDefaults(config, defaults)
	}

	// Apply environment-specific overrides (testing, staging, etc.)
	if envConfig != nil {
		pc.applyEnvironmentConfig(config, schema, providerName, envConfig)
	}

	// Apply custom configuration last (highest priority)
	for key, value := range customConfig {
		config[key] = value
	}

	return config, nil
}

// buildSchemaBasedConfig creates configuration based on provider schema
func (pc *Configurator) buildSchemaBasedConfig(schema *tfprotov6.SchemaBlock) map[string]interface{} {
	config := make(map[string]interface{})

	// AWS provider v5.31.0 requires ALL attributes to be present, not just required ones
	// Initialize all attributes with appropriate defaults
	for _, attr := range schema.Attributes {
		config[attr.Name] = pc.getDefaultValueForAttribute(attr)
	}

	// Handle block types (nested configuration)
	for _, blockType := range schema.BlockTypes {
		// Initialize all block types as empty lists
		config[blockType.TypeName] = []interface{}{}
	}

	return config
}

// applyDefaults applies provider-specific defaults
func (pc *Configurator) applyDefaults(config map[string]interface{}, defaults Defaults) {
	// Apply required attributes
	for key, value := range defaults.RequiredAttributes {
		config[key] = value
	}

	// Apply optional attributes
	for key, value := range defaults.OptionalAttributes {
		if _, exists := config[key]; !exists {
			config[key] = value
		}
	}
}

// applyEnvironmentConfig applies environment-specific configuration
func (pc *Configurator) applyEnvironmentConfig(
	config map[string]interface{},
	schema *tfprotov6.SchemaBlock,
	providerName string,
	envConfig *EnvironmentConfig,
) {
	// Apply attribute overrides
	for key, value := range envConfig.AttributeOverrides {
		if value == nil {
			// nil value means remove the attribute
			delete(config, key)
		} else {
			config[key] = value
		}
	}

	// Configure endpoints if provider supports them and endpoint URL is provided
	// Skip if endpoints are already configured in the provider config (with actual content)
	if envConfig.EndpointURL != "" {
		// Check if endpoints is already configured (as a list or map)
		endpointsConfigured := false
		if endpointsList, ok := config["endpoints"].([]interface{}); ok && len(endpointsList) > 0 {
			endpointsConfigured = true
		} else if endpointsMap, ok := config["endpoints"].(map[string]interface{}); ok && len(endpointsMap) > 0 {
			endpointsConfigured = true
		}

		if !endpointsConfigured {
			pc.configureEndpoints(config, schema, providerName, envConfig.EndpointURL)
		}
	}
}

// configureEndpoints sets up service endpoints for providers that support them
func (pc *Configurator) configureEndpoints(
	config map[string]interface{},
	schema *tfprotov6.SchemaBlock,
	providerName string,
	endpointURL string,
) {
	// Find the endpoints block in schema
	var endpointsBlock *tfprotov6.SchemaNestedBlock
	for _, blockType := range schema.BlockTypes {
		if blockType.TypeName == "endpoints" {
			endpointsBlock = blockType
			break
		}
	}

	if endpointsBlock == nil || endpointsBlock.Block == nil {
		return
	}

	// Get services that should use the custom endpoint
	var services []string
	if defaults, exists := pc.defaultConfigs[providerName]; exists {
		services = defaults.EndpointServices
	}

	if len(services) == 0 {
		return // No services configured for this provider
	}

	// Initialize endpoints configuration with only the services we need
	endpointsConfig := make(map[string]interface{})

	// Only set endpoints for configured services
	for _, service := range services {
		// Check if this service exists in the schema
		serviceFound := false
		for _, endpointAttr := range endpointsBlock.Block.Attributes {
			if endpointAttr.Name == service {
				serviceFound = true
				break
			}
		}
		if serviceFound {
			// AWS provider v6 expects simple string URLs for each service
			endpointsConfig[service] = endpointURL
		}
	}

	// Set endpoints block
	// AWS provider v6 expects endpoints as a list containing a single map
	config["endpoints"] = []interface{}{endpointsConfig}
}

// getDefaultValueForAttribute provides appropriate defaults based on attribute
func (pc *Configurator) getDefaultValueForAttribute(attr *tfprotov6.SchemaAttribute) interface{} {
	// Use the actual schema type to determine the correct default
	switch attr.Name {
	case "region":
		return "us-east-1" // Sensible default
	case "profile":
		return ""
	case "s3_use_path_style":
		return false // AWS provider expects boolean, default to false
	case "skip_credentials_validation", "skip_region_validation", "skip_requesting_account_id":
		return false // Boolean defaults for AWS
	case "skip_metadata_api_check":
		// Check the actual schema type - v4.x uses bool, v5.x uses string
		if attr.Type != nil {
			switch attr.Type.String() {
			case "bool":
				return false // v4.x expects boolean
			case typeString:
				return "false" // v5.x expects string
			}
		}
		return false // Default to boolean for older versions
	case "insecure":
		return false // Boolean default
	case "max_retries":
		return 25 // AWS provider default
	default:
		// Use schema type-based defaults for unknown attributes
		return pc.getDefaultValueByType(attr)
	}
}

// inferDefaultFromName infers appropriate default based on attribute name patterns and schema type
func (pc *Configurator) inferDefaultFromName(name string) interface{} {
	// Boolean patterns
	if strings.HasPrefix(name, "skip_") || strings.HasPrefix(name, "enable_") ||
		strings.HasPrefix(name, "disable_") || strings.HasPrefix(name, "use_") ||
		strings.Contains(name, "_enabled") || name == "insecure" {
		return false
	}
	// List/array patterns
	if strings.Contains(name, "_files") || strings.Contains(name, "_ids") {
		return []interface{}{}
	}
	// Numeric patterns
	if strings.Contains(name, "timeout") || strings.Contains(name, "max_") ||
		strings.Contains(name, "_capacity") || strings.Contains(name, "_limit") {
		return 0
	}
	// Default to empty string for most cases
	return ""
}

// getDefaultValueByType returns appropriate default value based on schema type
func (pc *Configurator) getDefaultValueByType(attr *tfprotov6.SchemaAttribute) interface{} {
	if attr.Type == nil {
		// If type info is not available, infer from name patterns
		return pc.inferDefaultFromName(attr.Name)
	}

	switch attr.Type.String() {
	case "bool":
		return false
	case "number":
		return 0
	case typeString:
		return ""
	case "list", "set":
		return []interface{}{}
	case "map", "object":
		return map[string]interface{}{}
	default:
		// Fall back to name inference for unknown types
		return pc.inferDefaultFromName(attr.Name)
	}
}

// getDefaultProviderConfigs returns default configurations for known providers
func getDefaultProviderConfigs() map[string]Defaults {
	return map[string]Defaults{
		"aws": {
			RequiredAttributes: map[string]interface{}{
				"region": "us-east-1",
			},
			OptionalAttributes: map[string]interface{}{
				// Only production-relevant defaults here
			},
			EndpointServices: []string{}, // No services configured by default
		},
		"azure": {
			RequiredAttributes: map[string]interface{}{
				// Azure provider requirements would go here
			},
			OptionalAttributes: map[string]interface{}{},
			EndpointServices:   []string{}, // Azure doesn't use endpoint override pattern
		},
		"gcp": {
			RequiredAttributes: map[string]interface{}{
				// GCP provider requirements would go here
			},
			OptionalAttributes: map[string]interface{}{},
			EndpointServices:   []string{}, // GCP doesn't use endpoint override pattern
		},
	}
}
