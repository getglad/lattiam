//go:build integration && !localstack
// +build integration,!localstack

package protocol

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/config"
)

// TestProviderWrapper_PlanResourceChangeDelegation tests that PlanResourceChange
// correctly delegates to UnifiedProviderClient when the build tag is present
func TestProviderWrapper_PlanResourceChangeDelegation(t *testing.T) {
	ctx := context.Background()

	// Create provider manager
	manager, err := NewProviderManagerWithOptions(
		WithBaseDir("/tmp/wrapper-delegation-test"),
		WithConfigSource(config.NewDefaultConfigSource()),
	)
	require.NoError(t, err, "Failed to create provider manager")
	defer manager.Close()

	t.Run("Random_Provider_Delegation", func(t *testing.T) {
		// Get a provider through the manager - this returns a providerWrapper
		provider, err := manager.GetProvider(ctx, "test-deployment-1", "random", "3.7.2", nil, "random_string")
		require.NoError(t, err, "Failed to get random provider")

		// Configure the provider (random doesn't need config but we do it anyway)
		err = provider.Configure(ctx, map[string]interface{}{})
		require.NoError(t, err, "Failed to configure provider")

		// Test PlanResourceChange - this should use the new delegation
		config := map[string]interface{}{
			"length":  int64(8),
			"special": false,
		}

		// Call PlanResourceChange on the wrapper - it should delegate to UnifiedProviderClient
		planResp, err := provider.PlanResourceChange(ctx, "random_string", nil, config)
		require.NoError(t, err, "PlanResourceChange should succeed")
		require.NotNil(t, planResp, "Should have plan response")
		require.NotNil(t, planResp.PlannedState, "Should have planned state")
		require.Empty(t, planResp.Diagnostics, "Should have no diagnostics")

		t.Log("✅ PlanResourceChange successfully delegated to UnifiedProviderClient")

		// Verify we can cast to providerWrapper and check unified client
		wrapper, ok := provider.(*providerWrapper)
		require.True(t, ok, "Provider should be providerWrapper")
		assert.NotNil(t, wrapper.unifiedClient, "UnifiedClient should be initialized after first use")

		_, ok = wrapper.unifiedClient.(*UnifiedProviderClient)
		assert.True(t, ok, "UnifiedClient should be of type *UnifiedProviderClient")

		t.Log("✅ Verified UnifiedProviderClient was used for delegation")
	})

	t.Run("AWS_Provider_Delegation_With_LocalStack", func(t *testing.T) {
		// Get AWS provider
		provider, err := manager.GetProvider(ctx, "test-deployment-2", "aws", "6.2.0", nil, "aws_s3_bucket")
		if err != nil {
			t.Skip("AWS provider not available")
		}

		// Configure for LocalStack
		awsConfig := map[string]interface{}{
			"region":                      "us-east-1",
			"access_key":                  "test",
			"secret_key":                  "test",
			"skip_credentials_validation": true,
			"skip_metadata_api_check":     "true",
			"skip_requesting_account_id":  true,
			"s3_use_path_style":           true,
			"endpoints": []interface{}{
				map[string]interface{}{
					"s3": "http://localstack:4566",
				},
			},
		}

		err = provider.Configure(ctx, awsConfig)
		require.NoError(t, err, "Failed to configure AWS provider")

		// Test PlanResourceChange for S3 bucket
		bucketConfig := map[string]interface{}{
			"bucket": "test-delegation-bucket",
			"tags": map[string]interface{}{
				"Test": "Delegation",
			},
		}

		// Call PlanResourceChange - should delegate to UnifiedProviderClient
		planResp, err := provider.PlanResourceChange(ctx, "aws_s3_bucket", nil, bucketConfig)
		require.NoError(t, err, "PlanResourceChange should succeed")
		require.NotNil(t, planResp, "Should have plan response")
		require.NotNil(t, planResp.PlannedState, "Should have planned state")

		// Check for errors in diagnostics
		for _, diag := range planResp.Diagnostics {
			assert.NotEqual(t, "Error", diag.Severity.String(),
				"Should not have error diagnostics: %s - %s", diag.Summary, diag.Detail)
		}

		t.Log("✅ AWS S3 bucket PlanResourceChange successfully delegated")

		// Verify unified client is being used
		wrapper, ok := provider.(*providerWrapper)
		require.True(t, ok, "Provider should be providerWrapper")
		assert.NotNil(t, wrapper.unifiedClient, "UnifiedClient should be initialized")

		t.Log("✅ Verified AWS provider uses UnifiedProviderClient for delegation")
	})
}
