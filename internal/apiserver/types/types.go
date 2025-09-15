// Package types provides API request/response types and conversion utilities
package types

import (
	"fmt"
	"time"

	"github.com/lattiam/lattiam/internal/interfaces"
)

// DeploymentRequest represents an API request to create/update a deployment
type DeploymentRequest struct {
	Name          string                 `json:"name"`
	TerraformJSON map[string]interface{} `json:"terraform_json"`
	Config        *DeploymentConfig      `json:"config,omitempty"`
}

// DeploymentConfig contains deployment configuration
type DeploymentConfig struct {
	AWSProfile string `json:"aws_profile,omitempty"`
	AWSRegion  string `json:"aws_region,omitempty"`
}

// DeploymentResponse represents the API response for a deployment
type DeploymentResponse struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	Status         string          `json:"status"`
	ResourceErrors []ResourceError `json:"resource_errors,omitempty"`
}

// ResourceError represents a single resource failure with detailed information
type ResourceError struct {
	Resource string `json:"resource"`        // e.g., "aws_security_group_rule/web_http"
	Type     string `json:"type"`            // e.g., "aws_security_group_rule"
	Name     string `json:"name"`            // e.g., "web_http"
	Error    string `json:"error"`           // Full error message from provider
	Phase    string `json:"phase,omitempty"` // "plan", "apply", "dependency_resolution"
}

// RequestConverter handles conversion between API request types and internal types
type RequestConverter struct {
	defaults DeploymentDefaults
}

// DeploymentDefaults contains default values for deployment requests
type DeploymentDefaults struct {
	DryRun     bool
	Timeout    time.Duration
	MaxRetries int
}

// NewRequestConverter creates a new request converter with specified defaults
func NewRequestConverter(defaults DeploymentDefaults) *RequestConverter {
	return &RequestConverter{
		defaults: defaults,
	}
}

// NewRequestConverterWithDefaults creates a request converter with sensible defaults
func NewRequestConverterWithDefaults() *RequestConverter {
	return NewRequestConverter(DeploymentDefaults{
		DryRun:     false,
		Timeout:    30 * time.Minute,
		MaxRetries: 3,
	})
}

// ToDeploymentRequest converts an API DeploymentRequest to interfaces.DeploymentRequest
func (rc *RequestConverter) ToDeploymentRequest(
	apiReq *DeploymentRequest,
	resources []interfaces.Resource,
	dataSources []interfaces.DataSource,
) *interfaces.DeploymentRequest {
	return &interfaces.DeploymentRequest{
		Resources:   resources,
		DataSources: dataSources,
		Options:     rc.buildDeploymentOptions(apiReq.Config, apiReq.TerraformJSON),
		Metadata:    rc.buildMetadata(apiReq),
	}
}

// buildDeploymentOptions creates DeploymentOptions with defaults and any overrides
func (rc *RequestConverter) buildDeploymentOptions(config *DeploymentConfig, terraformJSON map[string]interface{}) interfaces.DeploymentOptions {
	options := interfaces.DeploymentOptions{
		DryRun:         rc.defaults.DryRun,
		Timeout:        rc.defaults.Timeout,
		MaxRetries:     rc.defaults.MaxRetries,
		ProviderConfig: make(interfaces.ProviderConfig),
	}

	// Extract provider configuration from Terraform JSON
	if providerBlock, ok := terraformJSON["provider"].(map[string]interface{}); ok {
		// Copy all provider configurations
		for providerName, providerConfig := range providerBlock {
			if configMap, ok := providerConfig.(map[string]interface{}); ok {
				// ProviderConfig expects map[string]map[string]interface{} format
				// So we need to ensure it's properly structured
				options.ProviderConfig[providerName] = configMap
			}
		}
	}

	// Apply configuration overrides if provided
	if config != nil {
		// These overrides would apply to specific providers
		if config.AWSProfile != "" {
			// Ensure AWS config exists
			if _, exists := options.ProviderConfig["aws"]; exists {
				options.ProviderConfig["aws"]["profile"] = config.AWSProfile
			} else {
				options.ProviderConfig["aws"] = map[string]interface{}{
					"profile": config.AWSProfile,
				}
			}
		}
		if config.AWSRegion != "" {
			// Ensure AWS config exists
			if _, exists := options.ProviderConfig["aws"]; exists {
				options.ProviderConfig["aws"]["region"] = config.AWSRegion
			} else {
				options.ProviderConfig["aws"] = map[string]interface{}{
					"region": config.AWSRegion,
				}
			}
		}
	}

	return options
}

// buildMetadata creates metadata map from API request
func (rc *RequestConverter) buildMetadata(apiReq *DeploymentRequest) map[string]interface{} {
	metadata := map[string]interface{}{
		"name":           apiReq.Name,
		"terraform_json": apiReq.TerraformJSON,
	}

	// Note: Config is provider configuration and is handled in buildDeploymentOptions
	// It should NOT be exposed as metadata/variables

	return metadata
}

// UpdateDefaults allows updating the default values
func (rc *RequestConverter) UpdateDefaults(defaults DeploymentDefaults) {
	rc.defaults = defaults
}

// GetDefaults returns the current default values
func (rc *RequestConverter) GetDefaults() DeploymentDefaults {
	return rc.defaults
}

// ParseTerraformJSON converts Terraform JSON into resources and data sources
func (rc *RequestConverter) ParseTerraformJSON(terraformJSON map[string]interface{}) ([]interfaces.Resource, []interfaces.DataSource, error) {
	var resources []interfaces.Resource
	var dataSources []interfaces.DataSource

	// Parse resources
	if resourceBlock, ok := terraformJSON["resource"].(map[string]interface{}); ok {
		parsedResources, err := rc.parseResourceBlock(resourceBlock)
		if err != nil {
			return nil, nil, err
		}
		resources = parsedResources
	}

	// Parse data sources
	if dataBlock, ok := terraformJSON["data"].(map[string]interface{}); ok {
		parsedDataSources, err := rc.parseDataBlock(dataBlock)
		if err != nil {
			return nil, nil, err
		}
		dataSources = parsedDataSources
	}

	if len(resources) == 0 && len(dataSources) == 0 {
		return nil, nil, fmt.Errorf("no resources or data sources found in Terraform JSON")
	}

	return resources, dataSources, nil
}

// parseResourceBlock parses the resource block from Terraform JSON
func (rc *RequestConverter) parseResourceBlock(resourceBlock map[string]interface{}) ([]interfaces.Resource, error) {
	var resources []interfaces.Resource

	for providerType, typeResources := range resourceBlock {
		typeMap, ok := typeResources.(map[string]interface{})
		if !ok {
			continue
		}

		for name, props := range typeMap {
			properties, ok := props.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("invalid resource properties for %s.%s", providerType, name)
			}
			resources = append(resources, interfaces.Resource{
				Type:       providerType,
				Name:       name,
				Properties: properties,
			})
		}
	}

	return resources, nil
}

// parseDataBlock parses the data block from Terraform JSON
func (rc *RequestConverter) parseDataBlock(dataBlock map[string]interface{}) ([]interfaces.DataSource, error) {
	var dataSources []interfaces.DataSource

	for providerType, typeData := range dataBlock {
		typeMap, ok := typeData.(map[string]interface{})
		if !ok {
			continue
		}

		for name, props := range typeMap {
			properties, ok := props.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("invalid data source properties for %s.%s", providerType, name)
			}
			dataSources = append(dataSources, interfaces.DataSource{
				Type:       providerType,
				Name:       name,
				Properties: properties,
			})
		}
	}

	return dataSources, nil
}
