package helpers

import "time"

// Test environment constants
const (
	// Environment variables
	EnvTestMode           = "LATTIAM_TEST_MODE"
	EnvAWSRegion          = "AWS_REGION"
	EnvAWSProfile         = "AWS_PROFILE"
	EnvLocalStackEndpoint = "LOCALSTACK_ENDPOINT"

	// Test modes are defined in test_config.go

	// Default values
	DefaultRegion  = "us-east-1"
	DefaultProfile = "default"
)

// Timeout constants
const (
	// API timeouts
	ShortTimeout    = 30 * time.Second
	StandardTimeout = 1 * time.Minute
	LongTimeout     = 5 * time.Minute

	// Polling intervals
	FastPollInterval     = 1 * time.Second
	StandardPollInterval = 5 * time.Second
	SlowPollInterval     = 10 * time.Second

	// Test-specific timeouts
	HealthCheckTimeout      = 5 * time.Second
	ProviderDownloadTimeout = 5 * time.Minute
	LocalStackReadyTimeout  = 30 * time.Second
)

// API endpoints
const (
	// Base paths
	APIv1BasePath = "/api/v1"

	// Health endpoints
	EndpointHealth = "/api/v1/system/health"
	EndpointReady  = "/api/v1/ready"

	// Deployment endpoints
	EndpointDeployments       = "/api/v1/deployments"
	EndpointDeploymentByID    = "/api/v1/deployments/%s"
	EndpointDeploymentDestroy = "/api/v1/deployments/%s/destroy"

	// Provider endpoints
	EndpointProviders        = "/api/v1/providers"
	EndpointProviderVersions = "/api/v1/providers/%s/versions"
)

// HTTP constants
const (
	// Headers
	HeaderContentType   = "Content-Type"
	HeaderAccept        = "Accept"
	HeaderAuthorization = "Authorization"

	// Content types
	ContentTypeJSON = "application/json"
	ContentTypeText = "text/plain"
)

// Provider constants
const (
	// Provider names
	ProviderAWS    = "aws"
	ProviderAzure  = "azurerm"
	ProviderGoogle = "google"

	// Default versions
	AWSProviderVersion    = "5.31.0"
	AzureProviderVersion  = "3.85.0"
	GoogleProviderVersion = "5.10.0"

	// Provider registry
	DefaultProviderRegistry = "https://registry.terraform.io"
)

// Resource type constants
const (
	// AWS resource types
	ResourceTypeIAMRole            = "aws_iam_role"
	ResourceTypeS3Bucket           = "aws_s3_bucket"
	ResourceTypeS3BucketVersioning = "aws_s3_bucket_versioning"
	ResourceTypeEC2Instance        = "aws_instance"
	ResourceTypeSecurityGroup      = "aws_security_group"
	ResourceTypeVPC                = "aws_vpc"
	ResourceTypeSubnet             = "aws_subnet"

	// Data source types
	DataSourceRegion         = "aws_region"
	DataSourceCallerIdentity = "aws_caller_identity"
	DataSourceAMI            = "aws_ami"
	DataSourceAZs            = "aws_availability_zones"
)

// Test resource naming
const (
	// Prefixes for test resources
	TestResourcePrefix = "lattiam-test"
	TestBucketPrefix   = "lattiam-test-bucket"
	TestRolePrefix     = "lattiam-test-role"
	TestVPCPrefix      = "lattiam-test-vpc"

	// Suffixes
	TestResourceSuffixFormat = "-%d" // Unix timestamp
)

// Deployment status constants
const (
	StatusPending    = "pending"
	StatusPlanning   = "planning"
	StatusApplying   = "applying"
	StatusCompleted  = "completed"
	StatusFailed     = "failed"
	StatusDestroying = "destroying"
	StatusDestroyed  = "destroyed"
)

// Error messages
const (
	ErrAPINotAvailable      = "API server not available"
	ErrDeploymentNotFound   = "deployment not found"
	ErrInvalidResponse      = "invalid response format"
	ErrTimeoutWaitingStatus = "timeout waiting for status: %s"
	ErrUnexpectedStatus     = "unexpected status: got %s, want %s"
)

// Test categories (for build tags)
const (
	// Build tags for selective test execution
	BuildTagIntegration = "integration"
	BuildTagUnit        = "unit"
	BuildTagAWS         = "aws"
	BuildTagLocalStack  = "localstack"
	BuildTagSlow        = "slow"
	BuildTagParallel    = "parallel"
)
