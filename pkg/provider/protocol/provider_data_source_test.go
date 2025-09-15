//go:build integration
// +build integration

package protocol

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/config"
	"github.com/lattiam/lattiam/internal/interfaces"
)

// TestDataSourceOperations tests data source functionality directly
func TestDataSourceOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()
	pm, err := NewProviderManagerWithOptions(
		WithConfigSource(config.NewDefaultConfigSource()),
	)
	require.NoError(t, err)
	defer pm.Close()

	t.Run("aws_caller_identity", func(t *testing.T) {
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

		// Get provider instance for data source
		provider, err := pm.GetProvider(ctx, "test-deployment-aws", "aws", "6.2.0", config, "aws_caller_identity")
		require.NoError(t, err)

		// Try to read the data source
		// Since we're using test credentials, this will fail with auth error
		result, err := provider.ReadDataSource(ctx, "aws_caller_identity", map[string]interface{}{})

		// With test credentials, this should fail with an authentication error
		// or similar provider-specific error, not our "not implemented" error
		if err != nil {
			t.Logf("ReadDataSource error (expected with test credentials): %v", err)
			// The important thing is it's not our "not implemented" error anymore
			assert.NotContains(t, err.Error(), "data source operations are not yet implemented")
		} else {
			// If it succeeds (unlikely with test creds), verify result structure
			assert.NotNil(t, result)
			t.Logf("Data source read succeeded: %+v", result)
		}
	})
}

// TestDataSourceSchema verifies we can at least get data source schemas
func TestDataSourceSchema(t *testing.T) {
	ctx := context.Background()
	pm, err := NewProviderManagerWithOptions(
		WithConfigSource(config.NewDefaultConfigSource()),
	)
	require.NoError(t, err)
	defer pm.Close()

	t.Run("aws_provider_has_data_sources", func(t *testing.T) {
		// Configure minimal AWS provider config to avoid credential errors
		awsConfig := interfaces.ProviderConfig{
			"aws": map[string]interface{}{
				"region":                      "us-east-1",
				"skip_credentials_validation": true,
				"skip_metadata_api_check":     true,
				"skip_requesting_account_id":  true,
				"access_key":                  "test",
				"secret_key":                  "test",
			},
		}

		// Get provider and then get schema
		provider, err := pm.GetProvider(ctx, "test-deployment-aws", "aws", "6.2.0", awsConfig, "")
		require.NoError(t, err)
		defer func() {
			// Cleanup provider if needed
			if p, ok := provider.(*providerWrapper); ok && p.IsHealthy() {
				_ = p.Close()
			}
		}()

		schema, err := provider.GetSchema(ctx)
		require.NoError(t, err)

		// Check that aws_caller_identity data source exists in schema
		assert.Contains(t, schema.DataSourceSchemas, "aws_caller_identity")

		// Verify the schema has expected fields
		if callerIdentitySchema, ok := schema.DataSourceSchemas["aws_caller_identity"]; ok {
			// aws_caller_identity should have account_id, arn, user_id attributes
			attrs := make(map[string]bool)
			for _, attr := range callerIdentitySchema.Block.Attributes {
				attrs[attr.Name] = true
			}
			assert.True(t, attrs["account_id"], "aws_caller_identity should have account_id attribute")
			assert.True(t, attrs["arn"], "aws_caller_identity should have arn attribute")
			assert.True(t, attrs["user_id"], "aws_caller_identity should have user_id attribute")
		}
	})

	t.Run("random_provider_has_data_sources", func(t *testing.T) {
		// Get provider and then get schema
		provider, err := pm.GetProvider(ctx, "test-deployment-random", "random", "3.6.0", nil, "")
		require.NoError(t, err)
		defer func() {
			// Cleanup provider if needed
			if p, ok := provider.(*providerWrapper); ok && p.IsHealthy() {
				_ = p.Close()
			}
		}()

		schema, err := provider.GetSchema(ctx)
		require.NoError(t, err)

		// First, let's see what data sources are actually available
		if len(schema.DataSourceSchemas) == 0 {
			t.Log("Random provider has no data sources")
			t.Skip("Random provider doesn't have data sources in this version")
		}

		// Log available data sources
		for name := range schema.DataSourceSchemas {
			t.Logf("Available data source: %s", name)
		}
	})
}
