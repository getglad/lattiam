package protocol

import (
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

const (
	blockNameEndpoints = "endpoints"
)

// DynamicSchemaConverter handles schema-based configuration completion for the unified client
// This version is cty-free and works directly with tfprotov6 types
type DynamicSchemaConverter struct {
	resourceSchemas   map[string]*tfprotov6.Schema
	dataSourceSchemas map[string]*tfprotov6.Schema
	providerSchema    *tfprotov6.Schema
}

// NewDynamicSchemaConverter creates a new converter with provider schemas
func NewDynamicSchemaConverter(providerSchema *tfprotov6.GetProviderSchemaResponse) *DynamicSchemaConverter {
	return &DynamicSchemaConverter{
		resourceSchemas:   providerSchema.ResourceSchemas,
		dataSourceSchemas: providerSchema.DataSourceSchemas,
		providerSchema:    providerSchema.Provider,
	}
}

// CompleteProviderConfig completes a provider config with null values for missing attributes
// This is a cty-free implementation that works directly with map[string]interface{} values
//
//nolint:gocognit,gocyclo // Complex provider configuration with dynamic schema completion
func (c *DynamicSchemaConverter) CompleteProviderConfig(config map[string]interface{}, providerName string) (map[string]interface{}, error) {
	if c.providerSchema == nil || c.providerSchema.Block == nil {
		return config, nil
	}

	// Create a complete config with all attributes
	completeConfig := make(map[string]interface{})

	// Initialize all attributes with null values
	for _, attr := range c.providerSchema.Block.Attributes {
		if configVal, exists := config[attr.Name]; exists {
			completeConfig[attr.Name] = configVal
		} else {
			completeConfig[attr.Name] = nil
		}
	}

	// Handle nested blocks - set them to empty arrays if required
	for _, block := range c.providerSchema.Block.BlockTypes {
		if configVal, exists := config[block.TypeName]; exists {
			// Debug log for endpoints
			if block.TypeName == blockNameEndpoints {
				logger := GetDebugLogger()
				logger.Logf("provider-config", "Found endpoints block for provider %s: %+v", providerName, configVal)
			}

			// Special handling for endpoints block
			if block.TypeName == blockNameEndpoints && providerName == providerAWS {
				// Check if it's already a list (correct format)
				if endpointsList, ok := configVal.([]interface{}); ok {
					// Already in correct format, use as-is
					completeConfig[block.TypeName] = endpointsList
					logger := GetDebugLogger()
					logger.Logf("provider-config", "Endpoints already in list format: %+v", endpointsList)
				} else if endpointsMap, ok := configVal.(map[string]interface{}); ok && len(endpointsMap) > 0 {
					// Convert simple map to set format expected by schema
					// Create a single object with all endpoint fields
					endpointObj := make(map[string]interface{})

					// Initialize all attributes in the endpoints block to null
					if block.Block != nil {
						for _, attr := range block.Block.Attributes {
							endpointObj[attr.Name] = nil
						}
					}

					// Set the values we have
					for service, endpoint := range endpointsMap {
						endpointObj[service] = endpoint
					}

					// Wrap in array for Set
					completeConfig[block.TypeName] = []interface{}{endpointObj}
					logger := GetDebugLogger()
					logger.Logf("provider-config", "Converted endpoints map to list format: %+v", completeConfig[block.TypeName])
				} else {
					completeConfig[block.TypeName] = []interface{}{}
				}
			} else {
				completeConfig[block.TypeName] = configVal
			}
		} else {
			// Use empty array for blocks
			completeConfig[block.TypeName] = []interface{}{}
		}
	}

	// Copy any extra values from config that aren't in schema
	for key, val := range config {
		if _, exists := completeConfig[key]; !exists {
			completeConfig[key] = val
		}
	}

	return completeConfig, nil
}
