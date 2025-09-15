//go:build integration
// +build integration

package protocol

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/tests/helpers/localstack"
	"github.com/lattiam/lattiam/tests/testutil"
)

// TestProviderResourceCreate_RealProvider tests resource creation with real providers
// This is an integration test that skips the API/worker layers
func TestProviderResourceCreate_RealProvider(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	// Cannot run in parallel because aws_s3_bucket_localstack subtest needs to set environment variables

	ctx := context.Background()
	pm, err := GetTestProviderManager(t)
	require.NoError(t, err)
	defer pm.Close()

	t.Run("random_string_basic", func(t *testing.T) {
		// Get a random provider instance
		providerInstance, err := pm.GetProvider(ctx, "test-deployment-1", "random", "3.6.0",
			interfaces.ProviderConfig{
				"random": map[string]interface{}{},
			}, "random_string")
		require.NoError(t, err)

		// Create a random string resource directly
		result, err := providerInstance.CreateResource(ctx, "random_string", map[string]interface{}{
			"length": 8,
		})

		// This catches the msgpack format issue without involving API/workers
		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.NotEmpty(t, result["id"])
		assert.NotEmpty(t, result["result"])
	})

	t.Run("aws_s3_bucket_schema_debug", func(t *testing.T) {
		// First, debug the schema structure to understand the issue
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

		provider, err := pm.GetProvider(ctx, "test-deployment-2", "aws", "6.2.0", config, "aws_s3_bucket")
		if err != nil {
			t.Skip("AWS provider not available")
		}

		// Get the schema through the provider interface
		schema, err := provider.GetSchema(ctx)
		require.NoError(t, err)

		if s3Schema, ok := schema.ResourceSchemas["aws_s3_bucket"]; ok {
			t.Logf("aws_s3_bucket schema has %d attributes and %d block types",
				len(s3Schema.Block.Attributes), len(s3Schema.Block.BlockTypes))

			// Check grant block structure by looking through BlockTypes array
			for _, blockType := range s3Schema.Block.BlockTypes {
				if blockType.TypeName == "grant" {
					t.Logf("Grant is a block type with nesting: %d", blockType.Nesting)
					if blockType.Block != nil {
						for _, attr := range blockType.Block.Attributes {
							t.Logf("  Grant attribute %s: type=%v", attr.Name, attr.Type)
						}
					}
				}
			}

			// Check specific fields that should be blocks
			checkFields := []string{"cors_rule", "lifecycle_rule", "logging", "versioning", "website"}
			for _, field := range checkFields {
				// Check if field is in attributes (it shouldn't be)
				for _, attr := range s3Schema.Block.Attributes {
					if attr.Name == field {
						t.Logf("WARNING: %s is in Attributes (should be BlockTypes)", field)
					}
				}
				// Check if field is in block types (it should be)
				for _, blockType := range s3Schema.Block.BlockTypes {
					if blockType.TypeName == field {
						t.Logf("CORRECT: %s is in BlockTypes with nesting: %d", field, blockType.Nesting)
					}
				}
			}
		}
	})

	t.Run("aws_s3_bucket_localstack", func(t *testing.T) {
		// Start LocalStack container and get endpoint
		lsc := testutil.SetupLocalStack(t)
		endpointURL := lsc.GetEndpoint()

		// Set environment variables for AWS SDK
		t.Setenv("AWS_ENDPOINT_URL", endpointURL)
		t.Setenv("LOCALSTACK_ENDPOINT", endpointURL)

		// Use LocalStack configuration
		configSource := localstack.NewConfigSource()
		config, err := configSource.GetProviderConfig(ctx, "aws", "6.2.0", nil)
		require.NoError(t, err, "Failed to get LocalStack config")

		providerInstance, err := pm.GetProvider(ctx, "test-deployment-3", "aws", "6.2.0", config, "aws_s3_bucket")
		require.NoError(t, err, "Failed to get AWS provider instance")

		// Create S3 bucket directly - this is where msgpack format errors occur
		// Use a unique bucket name to avoid conflicts
		bucketName := fmt.Sprintf("test-direct-integration-%d", time.Now().Unix())
		result, err := providerInstance.CreateResource(ctx, "aws_s3_bucket", map[string]interface{}{
			"bucket": bucketName,
		})
		// If LocalStack returns 403 Forbidden, skip the test
		if err != nil {
			t.Logf("CreateResource error: %v", err)
			if strings.Contains(err.Error(), "403") || strings.Contains(err.Error(), "Forbidden") {
				t.Skip("LocalStack returned 403 Forbidden - check LocalStack permissions")
			}
			// For any other error, fail the test
			assert.NoError(t, err)
			return
		}

		// Only check results if creation succeeded
		assert.NotNil(t, result)
		assert.NotEmpty(t, result["id"])
		assert.Equal(t, bucketName, result["bucket"])
	})
}
