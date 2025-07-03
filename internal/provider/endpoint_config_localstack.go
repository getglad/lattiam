//go:build localstack || dev
// +build localstack dev

package provider

import (
	"os"
	"strings"
)

// LocalStackStrategy configures providers for LocalStack
type LocalStackStrategy struct {
	endpoint string
}

func NewLocalStackStrategy() *LocalStackStrategy {
	return &LocalStackStrategy{
		endpoint: os.Getenv("AWS_ENDPOINT_URL"),
	}
}

func (s *LocalStackStrategy) ShouldApply() bool {
	// Apply if endpoint is set and contains "localstack" or uses port 4566 (standard LocalStack port)
	return s.endpoint != "" && (strings.Contains(strings.ToLower(s.endpoint), "localstack") ||
		strings.Contains(s.endpoint, ":4566"))
}

func (s *LocalStackStrategy) ConfigureEndpoint(config *EnvironmentConfig) {
	config.EndpointURL = s.endpoint
	config.DisableSSL = true

	// LocalStack-specific settings
	config.AttributeOverrides["s3_use_path_style"] = true
	config.AttributeOverrides["skip_credentials_validation"] = true
	config.AttributeOverrides["skip_region_validation"] = true
	config.AttributeOverrides["skip_requesting_account_id"] = true
	config.AttributeOverrides["skip_metadata_api_check"] = true

	// Always set test credentials for LocalStack
	config.AttributeOverrides["access_key"] = "test"
	config.AttributeOverrides["secret_key"] = "test"
	config.AttributeOverrides["region"] = "us-east-1"

	// Don't set allowed_account_ids for LocalStack - it causes validation errors
	// LocalStack handles account ID validation internally
}

func (s *LocalStackStrategy) GetHealthCheckURL() string {
	return s.endpoint + "/_localstack/health"
}

func includeLocalStackStrategy() bool {
	return true
}
