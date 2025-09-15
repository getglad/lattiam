// Package utils provides utility functions for the Lattiam system
package utils

import "strings"

// ExtractProviderName extracts the provider name from a resource or data source type
// e.g., "aws_instance" -> "aws", "google_compute_instance" -> "google"
func ExtractProviderName(resourceType string) string {
	// Extract the part before the first underscore
	parts := strings.SplitN(resourceType, "_", 2)
	if len(parts) > 0 {
		return parts[0]
	}
	return resourceType
}

// ExtractProviderVersions gets provider versions from terraform JSON
func ExtractProviderVersions(terraformJSON map[string]interface{}) map[string]string { //nolint:gocognit // Complex nested map navigation
	versions := make(map[string]string)

	// First check terraform.required_providers
	if terraform, ok := terraformJSON["terraform"].(map[string]interface{}); ok {
		if requiredProviders, ok := terraform["required_providers"].(map[string]interface{}); ok {
			for provider, config := range requiredProviders {
				switch v := config.(type) {
				case string:
					versions[provider] = v
				case map[string]interface{}:
					if version, ok := v["version"].(string); ok {
						versions[provider] = version
					}
				}
			}
		}
	}

	// Also check provider blocks for version field
	if providers, ok := terraformJSON["provider"].(map[string]interface{}); ok {
		for providerName, providerConfig := range providers {
			// Only set version if not already found in required_providers
			if _, exists := versions[providerName]; !exists {
				if config, ok := providerConfig.(map[string]interface{}); ok {
					if version, ok := config["version"].(string); ok {
						versions[providerName] = version
					}
				}
			}
		}
	}

	return versions
}
