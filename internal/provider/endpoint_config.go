package provider

import (
	"os"

	"github.com/lattiam/lattiam/pkg/logging"
)

var logger = logging.NewLogger("endpoint-config")

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

// NewCustomEndpointStrategy creates a new custom endpoint strategy
func NewCustomEndpointStrategy() *CustomEndpointStrategy {
	return &CustomEndpointStrategy{
		endpoint: os.Getenv("AWS_ENDPOINT_URL"),
	}
}

// ShouldApply checks if the strategy should be applied
func (s *CustomEndpointStrategy) ShouldApply() bool {
	// Apply if a custom endpoint is set
	return s.endpoint != ""
}

// ConfigureEndpoint configures the endpoint in the environment
func (s *CustomEndpointStrategy) ConfigureEndpoint(config *EnvironmentConfig) {
	config.EndpointURL = s.endpoint
	// For custom endpoints, only set the endpoint URL
	// Let the caller configure other settings as needed
}

// GetHealthCheckURL returns the health check URL for the strategy
func (s *CustomEndpointStrategy) GetHealthCheckURL() string {
	// No standard health check for custom endpoints
	// The caller should implement their own health checks if needed
	return ""
}

// ProductionStrategy uses default AWS endpoints
type ProductionStrategy struct{}

// NewProductionStrategy creates a new production endpoint strategy
func NewProductionStrategy() *ProductionStrategy {
	return &ProductionStrategy{}
}

// ShouldApply checks if the strategy should be applied
func (s *ProductionStrategy) ShouldApply() bool {
	// Apply when no custom endpoint is set
	return os.Getenv("AWS_ENDPOINT_URL") == ""
}

// ConfigureEndpoint configures the endpoint (no-op for production)
func (s *ProductionStrategy) ConfigureEndpoint(_ *EnvironmentConfig) {
	// No special configuration needed for production AWS
	// Provider will use standard AWS SDK configuration
}

// GetHealthCheckURL returns the health check URL for production
func (s *ProductionStrategy) GetHealthCheckURL() string {
	// No health check for production AWS
	return ""
}

// GetEndpointStrategy returns the appropriate endpoint strategy
func GetEndpointStrategy() EndpointStrategy {
	strategies := []EndpointStrategy{
		NewCustomEndpointStrategy(),
		NewProductionStrategy(),
	}

	for _, strategy := range strategies {
		if strategy.ShouldApply() {
			logger.Info("Using endpoint strategy: %T", strategy)
			return strategy
		}
	}

	// Default to production
	logger.Info("Defaulting to production strategy")
	return NewProductionStrategy()
}
