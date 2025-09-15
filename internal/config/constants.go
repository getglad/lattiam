package config

// DeploymentStatus represents the status of a deployment.
const (
	// DeploymentStatusPending is the status for a pending deployment.
	DeploymentStatusPending = "pending"
	// DeploymentStatusPlanning is the status for a deployment that is being planned.
	DeploymentStatusPlanning = "planning"
	// DeploymentStatusApplying is the status for a deployment that is being applied.
	DeploymentStatusApplying = "applying"
	// DeploymentStatusCompleted is the status for a completed deployment.
	DeploymentStatusCompleted = "completed"
	// DeploymentStatusFailed is the status for a failed deployment.
	DeploymentStatusFailed = "failed"
	// DeploymentStatusDestroying is the status for a deployment that is being destroyed.
	DeploymentStatusDestroying = "destroying"
	// DeploymentStatusDestroyed is the status for a destroyed deployment.
	DeploymentStatusDestroyed = "destroyed"
)

const (
	// DefaultLocalStackURL is the default URL for LocalStack.
	DefaultLocalStackURL = "http://localstack:4566"
	// APIBasePath is the base path for the API.
	APIBasePath = "/api/v1"
)

// API endpoint constants
const (
	APIEndpointDeployments = "/api/v1/deployments"
	APIEndpointHealth      = "/api/v1/system/health"
	APIEndpointReady       = "/api/v1/ready"
)

// Timeout constants
const (
	DefaultTimeout30s = 30 // seconds
	DefaultTimeout60s = 60 // seconds
	DefaultTimeout90s = 90 // seconds
)
