package config //nolint:revive // Test file

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testAWSBackendType = "aws"
	testS3Bucket       = "test-bucket"
	testAWSRegion      = "us-east-1"
	testDynamoDBTable  = "test-table"
)

func TestAWSConfigDefaults(t *testing.T) {
	t.Parallel()
	cfg := NewServerConfig()

	// Check AWS defaults
	assert.Equal(t, "states/", cfg.StateStore.AWS.S3.Prefix)
	assert.True(t, cfg.StateStore.AWS.DynamoDB.Locking.Enabled)
	assert.Equal(t, 300, cfg.StateStore.AWS.DynamoDB.Locking.TTLSeconds)
}

func TestAWSConfigFromEnvironment(t *testing.T) { //nolint:paralleltest // Cannot use t.Parallel() with t.Setenv
	// Cannot use t.Parallel() when using t.Setenv
	// Set environment variables
	envVars := map[string]string{
		"LATTIAM_STATE_STORE":             testAWSBackendType,
		"LATTIAM_AWS_S3_BUCKET":           testS3Bucket,
		"LATTIAM_AWS_S3_REGION":           testAWSRegion,
		"LATTIAM_AWS_S3_PREFIX":           "custom-prefix/",
		"LATTIAM_AWS_S3_ENDPOINT":         "http://localhost:4566",
		"LATTIAM_AWS_DYNAMODB_TABLE":      "lattiam-deployments",
		"LATTIAM_AWS_DYNAMODB_REGION":     testAWSRegion,
		"LATTIAM_AWS_DYNAMODB_ENDPOINT":   "http://localhost:4566",
		"LATTIAM_AWS_LOCKING_ENABLED":     "true",
		"LATTIAM_AWS_LOCKING_TTL_SECONDS": "600",
	}

	// Set environment variables
	for key, value := range envVars {
		t.Setenv(key, value)
	}

	cfg := NewServerConfig()
	err := cfg.LoadFromEnv()
	require.NoError(t, err)

	// Check that values were loaded from environment
	assert.Equal(t, testAWSBackendType, cfg.StateStore.Type)
	assert.Equal(t, testS3Bucket, cfg.StateStore.AWS.S3.Bucket)
	assert.Equal(t, testAWSRegion, cfg.StateStore.AWS.S3.Region)
	assert.Equal(t, "custom-prefix/", cfg.StateStore.AWS.S3.Prefix)
	assert.Equal(t, "http://localhost:4566", cfg.StateStore.AWS.S3.Endpoint)
	assert.Equal(t, "lattiam-deployments", cfg.StateStore.AWS.DynamoDB.Table)
	assert.Equal(t, testAWSRegion, cfg.StateStore.AWS.DynamoDB.Region)
	assert.Equal(t, "http://localhost:4566", cfg.StateStore.AWS.DynamoDB.Endpoint)
	assert.True(t, cfg.StateStore.AWS.DynamoDB.Locking.Enabled)
	assert.Equal(t, 600, cfg.StateStore.AWS.DynamoDB.Locking.TTLSeconds)
}

func TestAWSValidation(t *testing.T) { //nolint:funlen // Test function with comprehensive test cases
	t.Parallel()
	tests := []struct {
		name      string
		setupFunc func(*ServerConfig)
		expectErr string
	}{
		{
			name: "valid AWS config",
			setupFunc: func(cfg *ServerConfig) {
				cfg.StateStore.Type = testAWSBackendType
				cfg.StateStore.AWS.S3.Bucket = testS3Bucket
				cfg.StateStore.AWS.S3.Region = testAWSRegion
				cfg.StateStore.AWS.DynamoDB.Table = testDynamoDBTable
				cfg.StateStore.AWS.DynamoDB.Region = testAWSRegion
			},
			expectErr: "",
		},
		{
			name: "missing S3 bucket",
			setupFunc: func(cfg *ServerConfig) {
				cfg.StateStore.Type = testAWSBackendType
				cfg.StateStore.AWS.S3.Region = testAWSRegion
				cfg.StateStore.AWS.DynamoDB.Table = testDynamoDBTable
				cfg.StateStore.AWS.DynamoDB.Region = testAWSRegion
			},
			expectErr: "AWS S3 bucket is required when using AWS state store",
		},
		{
			name: "missing S3 region",
			setupFunc: func(cfg *ServerConfig) {
				cfg.StateStore.Type = testAWSBackendType
				cfg.StateStore.AWS.S3.Bucket = testS3Bucket
				cfg.StateStore.AWS.DynamoDB.Table = testDynamoDBTable
				cfg.StateStore.AWS.DynamoDB.Region = testAWSRegion
			},
			expectErr: "AWS S3 region is required when using AWS state store",
		},
		{
			name: "missing DynamoDB table",
			setupFunc: func(cfg *ServerConfig) {
				cfg.StateStore.Type = testAWSBackendType
				cfg.StateStore.AWS.S3.Bucket = testS3Bucket
				cfg.StateStore.AWS.S3.Region = testAWSRegion
				cfg.StateStore.AWS.DynamoDB.Region = testAWSRegion
			},
			expectErr: "AWS DynamoDB table is required when using AWS state store",
		},
		{
			name: "missing DynamoDB region",
			setupFunc: func(cfg *ServerConfig) {
				cfg.StateStore.Type = testAWSBackendType
				cfg.StateStore.AWS.S3.Bucket = testS3Bucket
				cfg.StateStore.AWS.S3.Region = testAWSRegion
				cfg.StateStore.AWS.DynamoDB.Table = testDynamoDBTable
			},
			expectErr: "AWS DynamoDB region is required when using AWS state store",
		},
		{
			name: "invalid locking TTL",
			setupFunc: func(cfg *ServerConfig) {
				cfg.StateStore.Type = testAWSBackendType
				cfg.StateStore.AWS.S3.Bucket = testS3Bucket
				cfg.StateStore.AWS.S3.Region = testAWSRegion
				cfg.StateStore.AWS.DynamoDB.Table = testDynamoDBTable
				cfg.StateStore.AWS.DynamoDB.Region = testAWSRegion
				cfg.StateStore.AWS.DynamoDB.Locking.TTLSeconds = -1
			},
			expectErr: "AWS locking TTL seconds must be positive",
		},
		{
			name: "empty prefix gets default",
			setupFunc: func(cfg *ServerConfig) {
				cfg.StateStore.Type = testAWSBackendType
				cfg.StateStore.AWS.S3.Bucket = testS3Bucket
				cfg.StateStore.AWS.S3.Region = testAWSRegion
				cfg.StateStore.AWS.S3.Prefix = ""
				cfg.StateStore.AWS.DynamoDB.Table = testDynamoDBTable
				cfg.StateStore.AWS.DynamoDB.Region = testAWSRegion
			},
			expectErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := NewServerConfig()
			tt.setupFunc(cfg)

			err := cfg.Validate()

			if tt.expectErr == "" {
				require.NoError(t, err)
				// Check that empty prefix got default
				if cfg.StateStore.AWS.S3.Prefix == "" {
					assert.Equal(t, "states/", cfg.StateStore.AWS.S3.Prefix)
				}
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectErr)
			}
		})
	}
}

func TestAWSGetSanitized(t *testing.T) {
	t.Parallel()
	cfg := NewServerConfig()
	cfg.Debug = true
	cfg.StateStore.Type = testAWSBackendType
	cfg.StateStore.AWS.S3.Bucket = "test-bucket"
	cfg.StateStore.AWS.S3.Region = "us-east-1"
	cfg.StateStore.AWS.S3.Prefix = "custom-prefix/"
	cfg.StateStore.AWS.S3.Endpoint = "http://localhost:4566"
	cfg.StateStore.AWS.DynamoDB.Table = "test-table"
	cfg.StateStore.AWS.DynamoDB.Region = "us-east-1"
	cfg.StateStore.AWS.DynamoDB.Endpoint = "http://localhost:4566"
	cfg.StateStore.AWS.DynamoDB.Locking.Enabled = true
	cfg.StateStore.AWS.DynamoDB.Locking.TTLSeconds = 600

	sanitized := cfg.GetSanitized()

	// Should include AWS config in debug mode
	awsConfig, exists := sanitized["aws_config"].(map[string]interface{})
	assert.True(t, exists, "aws_config should exist in debug mode")
	assert.True(t, awsConfig["s3_bucket_configured"].(bool))
	assert.True(t, awsConfig["s3_region_configured"].(bool))
	assert.Equal(t, "custom-prefix/", awsConfig["s3_prefix"])
	assert.True(t, awsConfig["s3_endpoint_configured"].(bool))
	assert.True(t, awsConfig["dynamodb_table_configured"].(bool))
	assert.True(t, awsConfig["dynamodb_region_configured"].(bool))
	assert.True(t, awsConfig["dynamodb_endpoint_configured"].(bool))
	assert.True(t, awsConfig["locking_enabled"].(bool))
	assert.Equal(t, 600, awsConfig["locking_ttl_seconds"])
}
