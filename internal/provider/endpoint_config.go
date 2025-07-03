package provider

import (
	"log"
	"os"
	"strings"
)

// EndpointStrategy defines how to configure provider endpoints
type EndpointStrategy interface {
	// ShouldApply returns true if this strategy should be used
	ShouldApply() bool

	// ConfigureEndpoint modifies the environment config for the endpoint
	ConfigureEndpoint(config *EnvironmentConfig)

	// GetHealthCheckURL returns the health check URL, or empty string if not applicable
	GetHealthCheckURL() string
}

// CustomEndpointStrategy configures providers for custom endpoints
type CustomEndpointStrategy struct {
	endpoint string
}

func NewCustomEndpointStrategy() *CustomEndpointStrategy {
	return &CustomEndpointStrategy{
		endpoint: os.Getenv("AWS_ENDPOINT_URL"),
	}
}

func (s *CustomEndpointStrategy) ShouldApply() bool {
	// Apply if endpoint is set and LocalStack strategy doesn't apply
	if s.endpoint == "" {
		return false
	}

	// Check if LocalStack strategy would apply (if included in build)
	if includeLocalStackStrategy() {
		localStack := NewLocalStackStrategy()
		return !localStack.ShouldApply()
	}

	// If LocalStack not included, apply for any custom endpoint
	return true
}

func (s *CustomEndpointStrategy) ConfigureEndpoint(config *EnvironmentConfig) {
	config.EndpointURL = s.endpoint

	// Detect if this is a LocalStack endpoint and apply LocalStack configuration
	if s.isLocalStackEndpoint() {
		s.configureLocalStack(config)
	}
	// For other custom endpoints, only set the endpoint
	// Let the user configure other settings via environment
}

func (s *CustomEndpointStrategy) GetHealthCheckURL() string {
	if s.isLocalStackEndpoint() {
		return s.endpoint + "/_localstack/health"
	}
	// No standard health check for other custom endpoints
	return ""
}

// isLocalStackEndpoint detects if the endpoint is LocalStack
func (s *CustomEndpointStrategy) isLocalStackEndpoint() bool {
	return s.endpoint != "" && (strings.Contains(strings.ToLower(s.endpoint), "localstack") ||
		strings.Contains(s.endpoint, ":4566"))
}

// configureLocalStack applies LocalStack-specific configuration
func (s *CustomEndpointStrategy) configureLocalStack(config *EnvironmentConfig) {
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

// ProductionStrategy uses default AWS endpoints
type ProductionStrategy struct{}

func NewProductionStrategy() *ProductionStrategy {
	return &ProductionStrategy{}
}

func (s *ProductionStrategy) ShouldApply() bool {
	// Apply when no custom endpoint is set
	return os.Getenv("AWS_ENDPOINT_URL") == ""
}

func (s *ProductionStrategy) ConfigureEndpoint(_ *EnvironmentConfig) {
	// No special configuration needed for production AWS
	// Provider will use standard AWS SDK configuration
}

func (s *ProductionStrategy) GetHealthCheckURL() string {
	// No health check for production AWS
	return ""
}

// GetEndpointStrategy returns the appropriate endpoint strategy
func GetEndpointStrategy() EndpointStrategy {
	var strategies []EndpointStrategy

	// Only include LocalStack strategy if build tag is present
	if includeLocalStackStrategy() {
		strategies = append(strategies, NewLocalStackStrategy())
		log.Printf("LocalStack strategy included in build")
	} else {
		log.Printf("LocalStack strategy NOT included in build")
	}

	strategies = append(strategies,
		NewCustomEndpointStrategy(),
		NewProductionStrategy(),
	)

	for _, strategy := range strategies {
		if strategy.ShouldApply() {
			log.Printf("Using endpoint strategy: %T", strategy)
			return strategy
		}
	}

	// Default to production
	log.Printf("Defaulting to production strategy")
	return NewProductionStrategy()
}
