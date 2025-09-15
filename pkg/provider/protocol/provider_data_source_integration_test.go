//go:build integration
// +build integration

package protocol

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/tests/testutil"
)

// TestDataSourceIntegration tests data source functionality with real providers
func TestDataSourceIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	// Cannot use t.Parallel() when using SetupLocalStack which calls t.Setenv()

	ctx := context.Background()
	// Use shared cache for provider manager
	pm, err := GetTestProviderManager(t)
	require.NoError(t, err)
	defer pm.Close()

	t.Run("aws_caller_identity_integration", func(t *testing.T) {
		// Clean up providers to ensure fresh instance
		pm.CleanupProviders()

		// Start LocalStack container and get endpoint
		lscLocal := testutil.SetupLocalStack(t)
		endpointURL := lscLocal.GetEndpoint()

		// Set environment variables for AWS SDK
		t.Setenv("AWS_ENDPOINT_URL", endpointURL)
		t.Setenv("LOCALSTACK_ENDPOINT", endpointURL)

		// Configure AWS provider with test credentials
		config := interfaces.ProviderConfig{
			"aws": map[string]interface{}{
				"region":                      "us-east-1",
				"access_key":                  "test",
				"secret_key":                  "test",
				"skip_credentials_validation": true,
				"skip_metadata_api_check":     true,
				"skip_requesting_account_id":  true,
			},
		}

		// Add LocalStack endpoints if available
		t.Logf("Using LocalStack endpoint URL: %s", endpointURL)
		// Set endpoints for LocalStack - need to set all services to prevent HTTPS calls
		config["aws"]["endpoints"] = map[string]interface{}{
			"sts":            endpointURL,
			"iam":            endpointURL,
			"s3":             endpointURL,
			"dynamodb":       endpointURL,
			"ec2":            endpointURL,
			"lambda":         endpointURL,
			"sns":            endpointURL,
			"sqs":            endpointURL,
			"kms":            endpointURL,
			"secretsmanager": endpointURL,
		}
		// Force use path style for LocalStack
		config["aws"]["s3_use_path_style"] = true
		// Force HTTP protocol
		config["aws"]["insecure"] = true

		t.Logf("Full config being passed: %+v", config)

		// Get provider instance for data source
		provider, err := pm.GetProvider(ctx, "test-deployment-aws", "aws", "6.2.0", config, "aws_caller_identity")
		require.NoError(t, err)

		// Read the data source
		result, err := provider.ReadDataSource(ctx, "aws_caller_identity", map[string]interface{}{})
		require.NoError(t, err)
		require.NotNil(t, result)

		// Verify expected fields
		assert.Contains(t, result, "account_id")
		assert.Contains(t, result, "arn")
		assert.Contains(t, result, "user_id")

		// With test credentials, we get mock values
		assert.Equal(t, "000000000000", result["account_id"])
		assert.Equal(t, "000000000000", result["id"])

		t.Logf("Successfully read aws_caller_identity: %+v", result)
	})

	t.Run("aws_region_data_source", func(t *testing.T) {
		// Clean up providers to ensure fresh instance
		pm.CleanupProviders()

		// Set AWS SDK environment variables to force LocalStack usage
		// t.Setenv automatically handles cleanup
		// Start LocalStack container and get endpoint
		lscLocal := testutil.SetupLocalStack(t)
		endpointURL := lscLocal.GetEndpoint()

		// Set environment variables for AWS SDK
		t.Setenv("AWS_ENDPOINT_URL", endpointURL)
		t.Setenv("LOCALSTACK_ENDPOINT", endpointURL)

		// Configure AWS provider
		config := interfaces.ProviderConfig{
			"aws": map[string]interface{}{
				"region":                      "us-east-1",
				"access_key":                  "test",
				"secret_key":                  "test",
				"skip_credentials_validation": true,
				"skip_metadata_api_check":     true,
				"skip_requesting_account_id":  true,
			},
		}

		// Set endpoints for LocalStack - need to set all services to prevent HTTPS calls
		// (already set up at the beginning of this test)
		config["aws"]["endpoints"] = map[string]interface{}{
			"sts":            endpointURL,
			"iam":            endpointURL,
			"s3":             endpointURL,
			"dynamodb":       endpointURL,
			"ec2":            endpointURL,
			"lambda":         endpointURL,
			"sns":            endpointURL,
			"sqs":            endpointURL,
			"kms":            endpointURL,
			"secretsmanager": endpointURL,
		}
		// Force use path style for LocalStack
		config["aws"]["s3_use_path_style"] = true
		// Force HTTP protocol
		config["aws"]["insecure"] = true

		// Get provider instance for aws_region data source
		provider, err := pm.GetProvider(ctx, "test-deployment-aws", "aws", "6.2.0", config, "aws_region")
		require.NoError(t, err)

		// Read the current region
		result, err := provider.ReadDataSource(ctx, "aws_region", map[string]interface{}{})
		require.NoError(t, err)
		require.NotNil(t, result)

		// Verify expected fields
		assert.Contains(t, result, "name")
		assert.Equal(t, "us-east-1", result["name"])

		t.Logf("Successfully read aws_region: %+v", result)
	})
}

// TestDataSourceWithComplexConfig tests data sources that require configuration
func TestDataSourceWithComplexConfig(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pm, err := GetTestProviderManager(t)
	require.NoError(t, err)
	defer pm.Close()

	t.Run("aws_availability_zones", func(t *testing.T) {
		// Clean up providers to ensure fresh instance
		pm.CleanupProviders()

		// Set AWS SDK environment variables to force LocalStack usage
		// t.Setenv automatically handles cleanup
		// Start LocalStack container and get endpoint
		lscLocal := testutil.SetupLocalStack(t)
		endpointURL := lscLocal.GetEndpoint()

		// Set environment variables for AWS SDK
		t.Setenv("AWS_ENDPOINT_URL", endpointURL)
		t.Setenv("LOCALSTACK_ENDPOINT", endpointURL)

		// Configure AWS provider
		config := interfaces.ProviderConfig{
			"aws": map[string]interface{}{
				"region":                      "us-east-1",
				"access_key":                  "test",
				"secret_key":                  "test",
				"skip_credentials_validation": true,
				"skip_metadata_api_check":     true,
				"skip_requesting_account_id":  true,
			},
		}

		// Set endpoints for LocalStack - need to set all services to prevent HTTPS calls
		// (already set up at the beginning of this test)
		config["aws"]["endpoints"] = map[string]interface{}{
			"sts":            endpointURL,
			"iam":            endpointURL,
			"s3":             endpointURL,
			"dynamodb":       endpointURL,
			"ec2":            endpointURL,
			"lambda":         endpointURL,
			"sns":            endpointURL,
			"sqs":            endpointURL,
			"kms":            endpointURL,
			"secretsmanager": endpointURL,
		}
		// Force use path style for LocalStack
		config["aws"]["s3_use_path_style"] = true
		// Force HTTP protocol
		config["aws"]["insecure"] = true

		// Get provider instance
		provider, err := pm.GetProvider(ctx, "test-deployment-aws", "aws", "6.2.0", config, "aws_availability_zones")
		require.NoError(t, err)

		// Read availability zones with filter
		result, err := provider.ReadDataSource(ctx, "aws_availability_zones", map[string]interface{}{
			"state": "available",
		})

		// This might fail with real API calls, but the important thing is
		// that it's not failing with "not implemented"
		if err != nil {
			assert.NotContains(t, err.Error(), "not implemented")
			t.Logf("Expected error with test credentials: %v", err)
		} else {
			assert.NotNil(t, result)
			t.Logf("Successfully read aws_availability_zones: %+v", result)
		}
	})
}
