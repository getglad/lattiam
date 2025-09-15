package helpers

import (
	"github.com/lattiam/lattiam/internal/provider"
)

// CreateLocalStackEnvironmentConfig creates environment configuration for LocalStack testing
// Following the LocalStack documentation for manual configuration
func CreateLocalStackEnvironmentConfig(localstackURL string) *provider.EnvironmentConfig {
	return &provider.EnvironmentConfig{
		AttributeOverrides: map[string]interface{}{
			// Core attributes from LocalStack documentation
			"access_key":                  "mock_access_key",
			"secret_key":                  "mock_secret_key",
			"region":                      "us-east-1",
			"s3_use_path_style":           false, // Using virtual host style as recommended
			"skip_credentials_validation": true,
			"skip_metadata_api_check":     "true",
			"skip_requesting_account_id":  true,

			// Additional attributes to satisfy AWS provider v5.31.0 requirements
			"insecure":                           false,
			"max_retries":                        25,
			"profile":                            "",
			"shared_config_files":                []interface{}{},
			"shared_credentials_files":           []interface{}{},
			"allowed_account_ids":                []interface{}{},
			"forbidden_account_ids":              []interface{}{},
			"token":                              "",
			"use_dualstack_endpoint":             false,
			"use_fips_endpoint":                  false,
			"http_proxy":                         "",
			"https_proxy":                        "",
			"no_proxy":                           "",
			"custom_ca_bundle":                   "",
			"ec2_metadata_service_endpoint":      "",
			"ec2_metadata_service_endpoint_mode": "",
			"retry_mode":                         "",
			"s3_us_east_1_regional_endpoint":     "",
			"sts_region":                         "",
		},
		EndpointURL: localstackURL,
		DisableSSL:  false, // Not mentioned in LocalStack docs
	}
}

// CreateProductionEnvironmentConfig creates environment configuration for production
func CreateProductionEnvironmentConfig() *provider.EnvironmentConfig {
	return &provider.EnvironmentConfig{
		AttributeOverrides: map[string]interface{}{
			// Production-safe defaults
			"insecure":                    false,
			"skip_credentials_validation": false,
			"skip_region_validation":      false,
		},
		DisableSSL: false,
	}
}

// CreateStagingEnvironmentConfig creates environment configuration for staging
func CreateStagingEnvironmentConfig(customEndpoint string) *provider.EnvironmentConfig {
	return &provider.EnvironmentConfig{
		AttributeOverrides: map[string]interface{}{
			// Staging-specific overrides
			"skip_metadata_api_check": "true",
		},
		EndpointURL: customEndpoint,
		DisableSSL:  false,
	}
}
