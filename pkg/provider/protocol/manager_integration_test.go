//go:build integration && localstack

package protocol

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/tests/helpers/localstack"
)

// getProviderConfig returns LocalStack configuration using the centralized config source
func getProviderConfig(ctx context.Context) (interfaces.ProviderConfig, error) {
	configSource := localstack.NewConfigSource()
	return configSource.GetProviderConfig(ctx, "aws", "", nil)
}

// getStandardizedCacheDir returns the standardized cache directory for all tests
func getStandardizedCacheDir() string {
	// Check environment variable first
	if cacheDir := os.Getenv("LATTIAM_TEST_PROVIDER_CACHE"); cacheDir != "" {
		return cacheDir
	}

	// Use consistent test cache directory
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".lattiam", "test-providers")
}

// TestProviderManagerIntegration tests real provider downloads and lifecycle
//
// DOWNLOAD EFFICIENCY DESIGN:
// This test suite is designed to minimize provider downloads by using a shared cache directory.
// Instead of each test downloading its own providers (which would result in ~15+ downloads),
// all tests share the same cache directory, resulting in only 1-2 total downloads:
// - aws@6.2.0: Downloaded once, reused by all AWS tests
// - random@3.6.0: Downloaded once, reused by multi-provider tests
//
// This provides realistic integration testing while keeping CI times reasonable.
func TestProviderManagerIntegration(t *testing.T) {
	t.Parallel() // Can run in parallel with shared cache

	t.Run("RealProviderDownloadAndCaching", func(t *testing.T) {
		t.Parallel()
		pm, err := GetTestProviderManager(t)
		require.NoError(t, err)
		defer pm.Close()

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		config, err := getProviderConfig(ctx)
		require.NoError(t, err, "Should get provider config")

		// First call should download and cache provider
		provider1, err := pm.GetProvider(ctx, "test-deployment-aws", "aws", "6.2.0", config, "aws_s3_bucket")
		require.NoError(t, err, "Should successfully download and create AWS provider")
		assert.NotNil(t, provider1)

		// Second call should reuse cached provider
		provider2, err := pm.GetProvider(ctx, "test-deployment-aws", "aws", "6.2.0", config, "aws_s3_bucket")
		require.NoError(t, err, "Should reuse cached provider")
		assert.NotNil(t, provider2)

		// Verify provider is functional by getting schema
		schema, err := provider1.GetSchema(ctx)
		require.NoError(t, err, "Provider should be functional")
		assert.NotNil(t, schema)
	})

	t.Run("ProviderHealthCheckDetection", func(t *testing.T) {
		t.Parallel()
		pm, err := GetTestProviderManager(t)
		require.NoError(t, err)
		defer pm.Close()

		ctx := context.Background()
		config, err := getProviderConfig(ctx)
		require.NoError(t, err, "Should get provider config")

		// Create and cache a provider
		provider, err := pm.GetProvider(ctx, "test-deployment-aws", "aws", "6.2.0", config, "aws_s3_bucket")
		require.NoError(t, err, "Should create provider successfully")

		// Verify health check works
		schema, err := provider.GetSchema(ctx)
		require.NoError(t, err, "Health check should succeed")
		assert.NotNil(t, schema)

		// Test that subsequent calls reuse the healthy provider
		provider2, err := pm.GetProvider(ctx, "test-deployment-aws", "aws", "6.2.0", config, "aws_s3_bucket")
		require.NoError(t, err, "Should reuse healthy cached provider")
		assert.NotNil(t, provider2)
	})

	t.Run("LocalStackConfigurationValidation", func(t *testing.T) {
		t.Parallel()
		pm, err := GetTestProviderManager(t)
		require.NoError(t, err)
		defer pm.Close()

		ctx := context.Background()

		// Test configuration using the centralized LocalStack config source
		localstackConfig, err := getProviderConfig(ctx)
		require.NoError(t, err, "Should get LocalStack config")

		// Test that LocalStack configuration is properly extracted and configured
		provider, err := pm.GetProvider(ctx, "test-deployment-aws", "aws", "6.2.0", localstackConfig, "aws_s3_bucket")
		require.NoError(t, err, "Should successfully create provider with LocalStack config")
		assert.NotNil(t, provider)

		// Verify provider is configured correctly
		schema, err := provider.GetSchema(ctx)
		require.NoError(t, err, "Provider with LocalStack config should be functional")
		assert.NotNil(t, schema)
	})

	t.Run("MultipleProviderTypes", func(t *testing.T) {
		t.Parallel()
		pm, err := GetTestProviderManager(t)
		require.NoError(t, err)
		defer pm.Close()

		ctx := context.Background()

		// Test configuration with multiple providers
		multiProviderConfig, err := getProviderConfig(ctx)
		require.NoError(t, err, "Should get provider config")
		// Add random provider config to the base LocalStack config
		multiProviderConfig["random"] = map[string]interface{}{
			"version": "3.6.0",
		}

		// Test AWS provider
		awsProvider, err := pm.GetProvider(ctx, "test-deployment-aws", "aws", "6.2.0", multiProviderConfig, "aws_s3_bucket")
		require.NoError(t, err, "AWS provider should be created successfully")
		assert.NotNil(t, awsProvider)

		// Test Random provider (different provider type)
		randomProvider, err := pm.GetProvider(ctx, "test-deployment-random", "random", "3.6.0", multiProviderConfig, "random_string")
		require.NoError(t, err, "Random provider should be created successfully")
		assert.NotNil(t, randomProvider)

		// Verify both providers are functional
		awsSchema, err := awsProvider.GetSchema(ctx)
		require.NoError(t, err, "AWS provider should be functional")
		assert.NotNil(t, awsSchema)

		randomSchema, err := randomProvider.GetSchema(ctx)
		require.NoError(t, err, "Random provider should be functional")
		assert.NotNil(t, randomSchema)
	})

	t.Run("ProviderVersionIsolation", func(t *testing.T) {
		t.Parallel()
		pm, err := GetTestProviderManager(t)
		require.NoError(t, err)
		defer pm.Close()

		ctx := context.Background()
		config, err := getProviderConfig(ctx)
		require.NoError(t, err, "Should get provider config")

		// Test different versions create separate cache entries
		provider620, err := pm.GetProvider(ctx, "test-deployment-aws", "aws", "6.2.0", config, "aws_s3_bucket")
		require.NoError(t, err, "Should create AWS 6.2.0 provider")

		// Note: Testing different versions requires they're available
		// For now, test that same version reuses cache
		provider620_2, err := pm.GetProvider(ctx, "test-deployment-aws", "aws", "6.2.0", config, "aws_instance")
		require.NoError(t, err, "Should reuse AWS 6.2.0 provider for different resource")

		assert.NotNil(t, provider620)
		assert.NotNil(t, provider620_2)
	})

	t.Run("ConcurrentProviderAccess", func(t *testing.T) {
		t.Parallel()
		pm, err := GetTestProviderManager(t)
		require.NoError(t, err)
		defer pm.Close()

		ctx := context.Background()
		config, err := getProviderConfig(ctx)
		require.NoError(t, err, "Should get provider config")

		// Test concurrent access to provider cache with real providers
		results := make(chan error, 3)

		for i := 0; i < 3; i++ {
			go func(id int) {
				provider, err := pm.GetProvider(ctx, "test-deployment-aws", "aws", "6.2.0", config, "aws_s3_bucket")
				if err != nil {
					results <- err
					return
				}

				// Verify provider works
				_, err = provider.GetSchema(ctx)
				results <- err
			}(i)
		}

		// Collect results - all should succeed
		for i := 0; i < 3; i++ {
			err := <-results
			assert.NoError(t, err, "Concurrent provider access should succeed")
		}
	})
}

// TestProviderManagerCleanupIntegration tests cleanup with real providers
func TestProviderManagerCleanupIntegration(t *testing.T) {
	// Use temporary directory for cleanup tests
	tempDir := t.TempDir()

	t.Run("CleanupWithRealProviders", func(t *testing.T) {
		pm, err := NewProviderManagerWithOptions(
			WithBaseDir(tempDir),
			WithConfigSource(localstack.NewConfigSource()),
		)
		require.NoError(t, err)
		defer pm.Close()

		ctx := context.Background()
		config, err := getProviderConfig(ctx)
		require.NoError(t, err, "Should get provider config")

		// Create a real provider
		provider, err := pm.GetProvider(ctx, "test-deployment-aws", "aws", "6.2.0", config, "aws_s3_bucket")
		require.NoError(t, err, "Should create provider")
		assert.NotNil(t, provider)

		// Test cleanup
		err = pm.CleanupProviders()
		assert.NoError(t, err, "CleanupProviders should succeed with real providers")

		// Test Close
		err = pm.Close()
		assert.NoError(t, err, "Close should succeed")
	})
}
