package apiserver

import "time"

// Server timeout constants
const (
	// DefaultServerPort is the default port for the API server
	DefaultServerPort = 8084

	// RequestTimeout is the maximum time for processing a request
	RequestTimeout = 60 * time.Second

	// ReadTimeout is the maximum duration for reading the entire request
	ReadTimeout = 15 * time.Second

	// WriteTimeout is the maximum duration for writing the response
	WriteTimeout = 15 * time.Second

	// IdleTimeout is the maximum time to wait for the next request
	IdleTimeout = 60 * time.Second

	// HealthCheckTimeout is the timeout for health check requests
	HealthCheckTimeout = 5 * time.Second

	// ShutdownTimeout is the maximum time to wait for graceful shutdown
	ShutdownTimeout = 30 * time.Second
)

// API version constants
const (
	APIVersion = "v1"
	APIPrefix  = "/api/" + APIVersion
)

// Provider cache constants
const (
	ProviderCacheDir = ".lattiam/providers"
)

// Deployment execution constants
const (
	// DeploymentExecutionTimeout is the default timeout for deployment execution
	DeploymentExecutionTimeout = 30 * time.Minute

	// DeploymentStaleThreshold is when to consider a deployment stale
	DeploymentStaleThreshold = 10 * time.Minute

	// DestructionMaxAttempts is the maximum number of attempts for resource destruction
	DestructionMaxAttempts = 5

	// DestructionRateLimit is the minimum wait time between destructions
	DestructionRateLimit = 500 * time.Millisecond

	// DestructionRateLimitMax is the maximum wait time between destructions
	DestructionRateLimitMax = 1000 * time.Millisecond

	// ResourceCountThresholdForRateLimit is the minimum resources to trigger rate limiting
	ResourceCountThresholdForRateLimit = 5
)
