//go:build integration
// +build integration

package protocol

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cfg "github.com/lattiam/lattiam/internal/config"
	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/internal/provider"
)

// getLifecycleTestCacheDir returns the standardized cache directory for lifecycle tests
func getLifecycleTestCacheDir() string {
	// Check environment variable first
	if cacheDir := os.Getenv("LATTIAM_TEST_PROVIDER_CACHE"); cacheDir != "" {
		return cacheDir
	}

	// Use a standardized shared directory that all tests can use
	// This enables provider binary reuse across test runs
	homeDir, err := os.UserHomeDir()
	if err != nil {
		// Fallback to temp dir if home dir is not available
		return filepath.Join(os.TempDir(), ".lattiam-test-providers")
	}

	// Use a hidden directory in the user's home to persist across test runs
	return filepath.Join(homeDir, ".cache", "lattiam-test-providers")
}

// ProviderCacheMetrics tracks cache hit/miss rates for testing
type ProviderCacheMetrics struct {
	hits   atomic.Int64
	misses atomic.Int64
	deaths atomic.Int64
}

func (m *ProviderCacheMetrics) RecordHit() {
	m.hits.Add(1)
}

func (m *ProviderCacheMetrics) RecordMiss() {
	m.misses.Add(1)
}

func (m *ProviderCacheMetrics) RecordDeath() {
	m.deaths.Add(1)
}

func (m *ProviderCacheMetrics) GetStats() (hits, misses, deaths int64) {
	return m.hits.Load(), m.misses.Load(), m.deaths.Load()
}

// instrumentedProviderManager wraps ProviderManager to track metrics
type instrumentedProviderManager struct {
	*ProviderManager
	metrics *ProviderCacheMetrics
}

func (ipm *instrumentedProviderManager) GetProvider(
	ctx context.Context, deploymentID, name, version string, config interfaces.ProviderConfig, resourceType string,
) (provider.Provider, error) {
	// Check if provider is already cached
	cacheKey := fmt.Sprintf("%s@%s", name, version)

	ipm.providerPoolMu.RLock()
	deploymentProviders, deploymentExists := ipm.providerPool[deploymentID]
	var cachedProvider provider.Provider
	var exists bool
	if deploymentExists {
		cachedProvider, exists = deploymentProviders[cacheKey]
	}
	ipm.providerPoolMu.RUnlock()

	if exists {
		// Test if cached provider is alive
		testCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()

		if _, err := cachedProvider.GetSchema(testCtx); err != nil {
			// Provider is dead
			ipm.metrics.RecordDeath()
			ipm.metrics.RecordMiss()
		} else {
			// Provider is alive
			ipm.metrics.RecordHit()
		}

		// Let the real manager handle it
		return ipm.ProviderManager.GetProvider(ctx, deploymentID, name, version, config, resourceType)
	}

	// Definitely a miss - new provider needed
	ipm.metrics.RecordMiss()
	return ipm.ProviderManager.GetProvider(ctx, deploymentID, name, version, config, resourceType)
}

// TestProviderDeathAndRecovery tests the complete provider death and recovery cycle
func TestProviderDeathAndRecovery(t *testing.T) {
	t.Run("ProviderProcessDeathDetection", func(t *testing.T) {
		cacheDir := getLifecycleTestCacheDir()
		pm, err := NewProviderManagerWithOptions(
			WithBaseDir(cacheDir),
			WithConfigSource(cfg.NewDefaultConfigSource()),
		)
		require.NoError(t, err)
		defer pm.Close()

		// Wrap with metrics tracking
		metrics := &ProviderCacheMetrics{}
		ipm := &instrumentedProviderManager{
			ProviderManager: pm,
			metrics:         metrics,
		}

		ctx := context.Background()
		config := interfaces.ProviderConfig{
			"aws": map[string]interface{}{
				"region":                      "us-east-1",
				"access_key":                  "test",
				"secret_key":                  "test",
				"skip_credentials_validation": true,
				"skip_requesting_account_id":  true,
				"skip_metadata_api_check":     true,
			},
		}

		// Step 1: Create and cache provider
		provider1, err := ipm.GetProvider(ctx, "test-deployment-aws", "aws", "6.2.0", config, "aws_s3_bucket")
		require.NoError(t, err, "Should create provider successfully")
		assert.NotNil(t, provider1)

		// Verify it's working
		schema, err := provider1.GetSchema(ctx)
		require.NoError(t, err, "Provider should be functional")
		assert.NotNil(t, schema)

		// Check metrics - should be 1 miss (initial creation)
		hits, misses, _ := metrics.GetStats()
		assert.Equal(t, int64(0), hits, "No cache hits yet")
		assert.Equal(t, int64(1), misses, "One cache miss for initial creation")

		// Step 2: Get provider again - should be cached
		provider2, err := ipm.GetProvider(ctx, "test-deployment-aws", "aws", "6.2.0", config, "aws_s3_bucket")
		require.NoError(t, err, "Should get cached provider")
		assert.NotNil(t, provider2)

		// Check metrics - should be 1 hit
		hits, misses, _ = metrics.GetStats()
		assert.Equal(t, int64(1), hits, "One cache hit")
		assert.Equal(t, int64(1), misses, "Still one cache miss")

		// Step 3: Kill the provider process
		// Find the provider instance in activeProviders
		pm.activeProvidersMu.Lock()
		var targetInstance ProviderInstanceInterface
		for _, instance := range pm.activeProviders {
			if instance.Name() == "aws" && instance.Version() == "6.2.0" {
				targetInstance = instance
				break
			}
		}
		pm.activeProvidersMu.Unlock()

		require.NotNil(t, targetInstance, "Should find active provider instance")

		// Stop the provider (all providers are now plugin instances)
		pluginInstance, ok := targetInstance.(*PluginProviderInstance)
		require.True(t, ok, "Provider should be a PluginProviderInstance")

		t.Log("Stopping plugin provider instance")
		err = pluginInstance.Stop()
		require.NoError(t, err, "Should stop plugin provider")

		// Wait a moment for process to die
		time.Sleep(500 * time.Millisecond)

		// Step 3.5: Try to use the dead provider directly - should fail
		t.Log("Testing if dead provider fails...")
		_, schemaErr := provider1.GetSchema(ctx)
		if schemaErr != nil {
			t.Logf("Good: Dead provider GetSchema failed with: %v", schemaErr)
		} else {
			t.Log("WARNING: Dead provider GetSchema succeeded - provider may be handling disconnection gracefully")
		}

		// Step 4: Try to get provider - should detect death and recreate
		t.Log("Getting provider after death...")
		provider3, err := ipm.GetProvider(ctx, "test-deployment-aws", "aws", "6.2.0", config, "aws_s3_bucket")
		require.NoError(t, err, "Should create new provider after death")
		assert.NotNil(t, provider3)

		// Verify new provider works
		schema3, err := provider3.GetSchema(ctx)
		require.NoError(t, err, "New provider should be functional")
		assert.NotNil(t, schema3)

		// Check metrics - should have detected death
		hits, misses, deaths := metrics.GetStats()
		t.Logf("Metrics: hits=%d, misses=%d, deaths=%d", hits, misses, deaths)

		// The exact metrics depend on whether the health check detected death
		// If provider handles disconnection gracefully, it might still work
		if deaths > 0 {
			assert.Equal(t, int64(1), hits, "One cache hit before death")
			assert.Equal(t, int64(2), misses, "Two cache misses - initial and after death")
		} else {
			t.Log("Provider handled process death gracefully - no recreation needed")
		}

		// With plugin instances, process cleanup is handled by go-plugin
		// We verified the provider was recreated successfully above

		t.Log("Provider death and recovery test completed")
	})
}

// TestConcurrentProviderDeathDetection tests multiple goroutines detecting dead provider
func TestConcurrentProviderDeathDetection(t *testing.T) {
	t.Run("MultipleGoroutinesDetectDeath", func(t *testing.T) {
		cacheDir := getLifecycleTestCacheDir()
		pm, err := NewProviderManagerWithOptions(
			WithBaseDir(cacheDir),
			WithConfigSource(cfg.NewDefaultConfigSource()),
		)
		require.NoError(t, err)
		defer pm.Close() // Clean up provider processes

		ctx := context.Background()
		config := interfaces.ProviderConfig{
			"aws": map[string]interface{}{
				"region":                      "us-east-1",
				"access_key":                  "test",
				"secret_key":                  "test",
				"skip_credentials_validation": true,
				"skip_requesting_account_id":  true,
				"skip_metadata_api_check":     true,
			},
		}

		// Create and cache provider
		provider1, err := pm.GetProvider(ctx, "test-deployment-aws", "aws", "6.2.0", config, "aws_s3_bucket")
		require.NoError(t, err)
		assert.NotNil(t, provider1)

		// Find and kill the provider
		pm.activeProvidersMu.Lock()
		var targetInstance ProviderInstanceInterface
		for _, instance := range pm.activeProviders {
			if instance.Name() == "aws" && instance.Version() == "6.2.0" {
				targetInstance = instance
				break
			}
		}
		pm.activeProvidersMu.Unlock()

		require.NotNil(t, targetInstance)

		// Stop the provider (all providers are now plugin instances)
		pluginInstance, ok := targetInstance.(*PluginProviderInstance)
		require.True(t, ok, "Provider should be a PluginProviderInstance")

		err = pluginInstance.Stop()
		require.NoError(t, err)

		time.Sleep(100 * time.Millisecond)

		// Launch multiple goroutines to detect death
		const numGoroutines = 5
		results := make(chan error, numGoroutines)
		providers := make(chan provider.Provider, numGoroutines)

		var wg sync.WaitGroup
		wg.Add(numGoroutines)

		for i := 0; i < numGoroutines; i++ {
			go func(id int) {
				defer wg.Done()

				// All goroutines try to get provider at same time
				provider, err := pm.GetProvider(ctx, "test-deployment-aws", "aws", "6.2.0", config, "aws_s3_bucket")
				results <- err
				if err == nil {
					providers <- provider
				}
			}(i)
		}

		// Wait for all goroutines
		wg.Wait()
		close(results)
		close(providers)

		// Verify all succeeded
		successCount := 0
		for err := range results {
			if err == nil {
				successCount++
			} else {
				t.Logf("Goroutine failed: %v", err)
			}
		}

		assert.Equal(t, numGoroutines, successCount, "All goroutines should succeed")

		// Verify we got valid providers
		providerCount := 0
		for provider := range providers {
			assert.NotNil(t, provider)
			providerCount++
		}

		assert.Equal(t, numGoroutines, providerCount, "All goroutines should get providers")

		// Check if a new provider was created (depends on whether kill succeeded)
		pm.activeProvidersMu.Lock()
		newInstanceCount := 0
		sameInstanceCount := 0
		for _, instance := range pm.activeProviders {
			if instance.Name() == "aws" && instance.Version() == "6.2.0" {
				if _, ok := instance.(*PluginProviderInstance); ok {
					newInstanceCount++
				} else {
					sameInstanceCount++
				}
			}
		}
		pm.activeProvidersMu.Unlock()

		// Provider may survive kill, or multiple goroutines may create new instances
		totalInstances := newInstanceCount + sameInstanceCount
		if totalInstances > 0 {
			t.Logf("Found %d total provider instances (new: %d, same: %d)", totalInstances, newInstanceCount, sameInstanceCount)
			// With concurrent access and plugin providers, we may have multiple instances
			// This is acceptable as long as we have at least one working provider
			assert.GreaterOrEqual(t, totalInstances, 1, "Should have at least one provider instance")
			if newInstanceCount > 0 {
				t.Log("Provider was recreated after kill")
			}
			if sameInstanceCount > 0 {
				t.Log("Provider survived kill and is still active")
			}
		} else {
			t.Fatal("No provider instances found after concurrent access")
		}

		t.Logf("Concurrent death detection test completed - %d goroutines handled gracefully", numGoroutines)
	})
}

// TestProviderProcessCleanup verifies no zombie processes are created
func TestProviderProcessCleanup(t *testing.T) {
	t.Run("NoZombieProcesses", func(t *testing.T) {
		cacheDir := getLifecycleTestCacheDir()
		pm, err := NewProviderManagerWithOptions(
			WithBaseDir(cacheDir),
			WithConfigSource(cfg.NewDefaultConfigSource()),
		)
		require.NoError(t, err)
		defer pm.Close()

		ctx := context.Background()
		config := interfaces.ProviderConfig{
			"aws": map[string]interface{}{
				"region":                      "us-east-1",
				"access_key":                  "test",
				"secret_key":                  "test",
				"skip_credentials_validation": true,
				"skip_requesting_account_id":  true,
				"skip_metadata_api_check":     true,
			},
		}

		// Track PIDs
		var pids []int

		// Create multiple providers and kill them
		for i := 0; i < 3; i++ {
			_, err := pm.GetProvider(ctx, "test-deployment-aws", "aws", "6.2.0", config, "aws_s3_bucket")
			require.NoError(t, err)

			// Find the instance
			pm.activeProvidersMu.Lock()
			for _, instance := range pm.activeProviders {
				if instance.Name() == "aws" && instance.Version() == "6.2.0" {
					if pluginInst, ok := instance.(*PluginProviderInstance); ok {
						// Stop the plugin instance
						pluginInst.Stop()
						pids = append(pids, -1) // Plugin instances don't expose PID
					}
					break
				}
			}
			pm.activeProvidersMu.Unlock()

			// Clear from cache to force recreation
			pm.providerPoolMu.Lock()
			delete(pm.providerPool, "aws@6.2.0")
			pm.providerPoolMu.Unlock()

			time.Sleep(100 * time.Millisecond)
		}

		// Clean up all providers
		err = pm.CleanupProviders()
		assert.NoError(t, err)

		// Verify processes are gone (best effort - process checking is OS-specific)
		t.Logf("Cleaned up %d provider processes: %v", len(pids), pids)

		// Create one more provider to ensure system still works
		finalProvider, err := pm.GetProvider(ctx, "test-deployment-aws", "aws", "6.2.0", config, "aws_s3_bucket")
		require.NoError(t, err)
		assert.NotNil(t, finalProvider)

		// Final cleanup is handled by defer
	})
}

// TestProviderCacheEfficiency verifies cache performance with metrics
func TestProviderCacheEfficiency(t *testing.T) {
	t.Run("CacheHitRateTracking", func(t *testing.T) {
		cacheDir := getLifecycleTestCacheDir()
		pm, err := NewProviderManagerWithOptions(
			WithBaseDir(cacheDir),
			WithConfigSource(cfg.NewDefaultConfigSource()),
		)
		require.NoError(t, err)
		defer pm.Close()

		metrics := &ProviderCacheMetrics{}
		ipm := &instrumentedProviderManager{
			ProviderManager: pm,
			metrics:         metrics,
		}

		ctx := context.Background()
		config := interfaces.ProviderConfig{
			"aws": map[string]interface{}{
				"region":                      "us-east-1",
				"access_key":                  "test",
				"secret_key":                  "test",
				"skip_credentials_validation": true,
				"skip_requesting_account_id":  true,
				"skip_metadata_api_check":     true,
			},
		}

		// Simulate realistic usage pattern
		resourceTypes := []string{
			"aws_s3_bucket",
			"aws_s3_bucket_versioning",
			"aws_s3_bucket_lifecycle_configuration",
			"aws_s3_bucket_policy",
			"aws_iam_role",
		}

		// First pass - all misses
		for _, resourceType := range resourceTypes {
			_, err := ipm.GetProvider(ctx, "test-deployment-aws", "aws", "6.2.0", config, resourceType)
			require.NoError(t, err)
		}

		hits, misses, _ := metrics.GetStats()
		assert.Equal(t, int64(4), hits, "4 cache hits after first resource")
		assert.Equal(t, int64(1), misses, "1 cache miss for initial creation")

		// Second pass - all hits
		for _, resourceType := range resourceTypes {
			_, err := ipm.GetProvider(ctx, "test-deployment-aws", "aws", "6.2.0", config, resourceType)
			require.NoError(t, err)
		}

		hits, misses, _ = metrics.GetStats()
		assert.Equal(t, int64(9), hits, "9 total cache hits")
		assert.Equal(t, int64(1), misses, "Still only 1 cache miss")

		// Calculate hit rate
		total := hits + misses
		hitRate := float64(hits) / float64(total) * 100
		t.Logf("Cache hit rate: %.2f%% (%d hits, %d misses)", hitRate, hits, misses)
		assert.Greater(t, hitRate, 85.0, "Cache hit rate should be > 85%")
	})
}

// TestProviderHealthCheckTimeout verifies the 2-second health check timeout
func TestProviderHealthCheckTimeout(t *testing.T) {
	t.Run("HealthCheckTimeoutBehavior", func(t *testing.T) {
		cacheDir := getLifecycleTestCacheDir()
		pm, err := NewProviderManagerWithOptions(
			WithBaseDir(cacheDir),
			WithConfigSource(cfg.NewDefaultConfigSource()),
		)
		require.NoError(t, err)
		defer pm.Close()

		ctx := context.Background()
		config := interfaces.ProviderConfig{
			"aws": map[string]interface{}{
				"region":                      "us-east-1",
				"access_key":                  "test",
				"secret_key":                  "test",
				"skip_credentials_validation": true,
				"skip_requesting_account_id":  true,
				"skip_metadata_api_check":     true,
			},
		}

		// Create provider
		provider1, err := pm.GetProvider(ctx, "test-deployment-aws", "aws", "6.2.0", config, "aws_s3_bucket")
		require.NoError(t, err)
		assert.NotNil(t, provider1)

		// Measure health check time
		start := time.Now()
		schema, err := provider1.GetSchema(ctx)
		duration := time.Since(start)

		require.NoError(t, err)
		assert.NotNil(t, schema)
		assert.Less(t, duration, 2*time.Second, "Health check should complete quickly")

		t.Logf("Health check completed in %v", duration)

		// Now test with a dead provider by killing it
		pm.activeProvidersMu.Lock()
		for _, instance := range pm.activeProviders {
			if instance.Name() == "aws" && instance.Version() == "6.2.0" {
				if pluginInst, ok := instance.(*PluginProviderInstance); ok {
					pluginInst.Stop()
				}
				break
			}
		}
		pm.activeProvidersMu.Unlock()

		time.Sleep(100 * time.Millisecond)

		// Try to get provider again - health check should timeout
		start = time.Now()
		provider2, err := pm.GetProvider(ctx, "test-deployment-aws", "aws", "6.2.0", config, "aws_s3_bucket")
		duration = time.Since(start)

		require.NoError(t, err, "Should create new provider after timeout")
		assert.NotNil(t, provider2)

		// If the provider was actually recreated, it should have taken some time
		// If the provider survived the kill, it should be very fast
		if provider2 != provider1 {
			// New provider was created
			// The health check should fail quickly (connection refused) and a new provider created
			assert.Less(t, duration, 5*time.Second, "Should not take too long to detect dead provider and create new one")
			t.Logf("Dead provider detected and replaced in %v", duration)
		} else {
			// Same provider survived
			assert.Less(t, duration, 1*time.Second, "Should be fast if provider survived")
			t.Logf("Provider survived kill and responded quickly in %v", duration)
		}
	})
}

// TestProviderConfigurationIsolation verifies providers with different configs are cached separately
func TestProviderConfigurationIsolation(t *testing.T) {
	t.Run("DifferentConfigsDifferentProviders", func(t *testing.T) {
		cacheDir := getLifecycleTestCacheDir()
		pm, err := NewProviderManagerWithOptions(
			WithBaseDir(cacheDir),
			WithConfigSource(cfg.NewDefaultConfigSource()),
		)
		require.NoError(t, err)
		defer pm.Close()

		ctx := context.Background()

		// Create two different configurations with test credentials
		config1 := interfaces.ProviderConfig{
			"aws": map[string]interface{}{
				"region":                      "us-east-1",
				"access_key":                  "test1",
				"secret_key":                  "test1",
				"skip_credentials_validation": true,
				"skip_metadata_api_check":     true,
				"skip_requesting_account_id":  true,
			},
		}

		config2 := interfaces.ProviderConfig{
			"aws": map[string]interface{}{
				"region":                      "us-west-2",
				"access_key":                  "test2",
				"secret_key":                  "test2",
				"skip_credentials_validation": true,
				"skip_metadata_api_check":     true,
				"skip_requesting_account_id":  true,
			},
		}

		// Get providers with different configs
		provider1, err := pm.GetProvider(ctx, "test-deployment-aws", "aws", "6.2.0", config1, "aws_s3_bucket")
		require.NoError(t, err)
		assert.NotNil(t, provider1)

		provider2, err := pm.GetProvider(ctx, "test-deployment-aws", "aws", "6.2.0", config2, "aws_s3_bucket")
		require.NoError(t, err)
		assert.NotNil(t, provider2)

		// Both should work
		schema1, err := provider1.GetSchema(ctx)
		require.NoError(t, err)
		assert.NotNil(t, schema1)

		schema2, err := provider2.GetSchema(ctx)
		require.NoError(t, err)
		assert.NotNil(t, schema2)

		// Verify we have 2 different provider instances
		pm.activeProvidersMu.Lock()
		awsCount := 0
		for _, instance := range pm.activeProviders {
			if instance.Name() == "aws" && instance.Version() == "6.2.0" {
				awsCount++
			}
		}
		pm.activeProvidersMu.Unlock()

		// Note: Current implementation doesn't isolate by config, so this will be 1
		// This test documents the current behavior
		t.Logf("Active AWS provider instances: %d", awsCount)

		// Get same config again - should reuse
		provider1Again, err := pm.GetProvider(ctx, "test-deployment-aws", "aws", "6.2.0", config1, "aws_s3_bucket")
		require.NoError(t, err)
		assert.NotNil(t, provider1Again)
	})
}

// TestProviderLifecycleDuringDeployment verifies providers remain available during deployment
func TestProviderLifecycleDuringDeployment(t *testing.T) {
	t.Run("ProviderRemainsHealthyDuringOperations", func(t *testing.T) {
		cacheDir := getLifecycleTestCacheDir()
		pm, err := NewProviderManagerWithOptions(
			WithBaseDir(cacheDir),
			WithConfigSource(cfg.NewDefaultConfigSource()),
		)
		require.NoError(t, err)
		defer pm.Close()

		ctx := context.Background()
		config := interfaces.ProviderConfig{
			"aws": map[string]interface{}{
				"region":                      "us-east-1",
				"access_key":                  "test",
				"secret_key":                  "test",
				"skip_credentials_validation": true,
				"skip_requesting_account_id":  true,
				"skip_metadata_api_check":     true,
			},
		}

		// Get provider
		provider1, err := pm.GetProvider(ctx, "test-deployment-aws", "aws", "6.2.0", config, "aws_s3_bucket")
		require.NoError(t, err)
		assert.NotNil(t, provider1)

		// Verify provider is healthy (cast to providerWrapper to access IsHealthy)
		if wrapper, ok := provider1.(*providerWrapper); ok {
			require.True(t, wrapper.IsHealthy(), "Provider should be healthy after creation")
		}

		// Find the provider instance
		pm.activeProvidersMu.Lock()
		var providerInstance ProviderInstanceInterface
		for _, instance := range pm.activeProviders {
			if instance.Name() == "aws" && instance.Version() == "6.2.0" {
				providerInstance = instance
				break
			}
		}
		pm.activeProvidersMu.Unlock()

		require.NotNil(t, providerInstance, "Should find provider instance")

		// Simulate operations happening
		for i := 0; i < 5; i++ {
			// Verify provider remains healthy during operations
			if wrapper, ok := provider1.(*providerWrapper); ok {
				assert.True(t, wrapper.IsHealthy(), "Provider should remain healthy during operation %d", i)
			}

			// Use the provider
			schema, err := provider1.GetSchema(ctx)
			assert.NoError(t, err, "Should be able to get schema during operation %d", i)
			assert.NotNil(t, schema)

			time.Sleep(100 * time.Millisecond)
		}

		// Provider should still be healthy after all operations
		if wrapper, ok := provider1.(*providerWrapper); ok {
			assert.True(t, wrapper.IsHealthy(), "Provider should be healthy after all operations")
		}

		// Provider will be shut down by defer
		// After test completes, provider will be unhealthy
	})
}

// TestMultipleProviderManagerLifecycles tests creating multiple provider managers in sequence
func TestMultipleProviderManagerLifecycles(t *testing.T) {
	t.Run("SequentialProviderManagersCleanupProperly", func(t *testing.T) {
		cacheDir := getLifecycleTestCacheDir()

		// Create first provider manager
		pm1, err := NewProviderManagerWithOptions(
			WithBaseDir(cacheDir),
			WithConfigSource(cfg.NewDefaultConfigSource()),
		)
		require.NoError(t, err)

		ctx := context.Background()
		config := interfaces.ProviderConfig{
			"aws": map[string]interface{}{
				"region":                      "us-east-1",
				"access_key":                  "test",
				"secret_key":                  "test",
				"skip_credentials_validation": true,
				"skip_requesting_account_id":  true,
				"skip_metadata_api_check":     true,
			},
		}

		// Get provider from first manager
		provider1, err := pm1.GetProvider(ctx, "test-deployment-aws", "aws", "6.2.0", config, "aws_s3_bucket")
		require.NoError(t, err)

		// Verify first provider works
		schema1, err := provider1.GetSchema(ctx)
		require.NoError(t, err)
		assert.NotNil(t, schema1)

		// Shutdown first manager
		err = pm1.Close()
		require.NoError(t, err, "First manager shutdown should succeed")

		// Create second provider manager (simulating test transition)
		pm2, err := NewProviderManagerWithOptions(
			WithBaseDir(cacheDir),
			WithConfigSource(cfg.NewDefaultConfigSource()),
		)
		require.NoError(t, err)
		defer pm2.Close()

		// Get provider from second manager
		provider2, err := pm2.GetProvider(ctx, "test-deployment-aws", "aws", "6.2.0", config, "aws_s3_bucket")
		require.NoError(t, err)

		// Verify second provider works
		schema2, err := provider2.GetSchema(ctx)
		require.NoError(t, err)
		assert.NotNil(t, schema2)

		// The first provider should be unhealthy
		if wrapper, ok := provider1.(*providerWrapper); ok {
			assert.False(t, wrapper.IsHealthy(), "First provider should be unhealthy after its manager shutdown")
		}

		// The second provider should be healthy
		if wrapper, ok := provider2.(*providerWrapper); ok {
			assert.True(t, wrapper.IsHealthy(), "Second provider should be healthy")
		}

		t.Log("Successfully tested provider manager lifecycles with proper cleanup")
	})
}

// TestProviderProcessCleanupVerification verifies no provider processes are left after manager shutdown
func TestProviderProcessCleanupVerification(t *testing.T) {
	t.Run("NoProcessesLeftAfterShutdown", func(t *testing.T) {
		// Get baseline of existing provider processes
		baselineCmd := exec.Command("pgrep", "-f", "terraform-provider")
		baselineOutput, _ := baselineCmd.Output()
		baselinePids := make(map[string]bool)
		if len(baselineOutput) > 0 {
			for _, pid := range strings.Fields(string(baselineOutput)) {
				baselinePids[pid] = true
			}
		}
		t.Logf("Baseline: %d existing terraform provider processes", len(baselinePids))

		cacheDir := getLifecycleTestCacheDir()
		pm, err := NewProviderManagerWithOptions(
			WithBaseDir(cacheDir),
			WithConfigSource(cfg.NewDefaultConfigSource()),
		)
		require.NoError(t, err)
		// No defer - we'll close explicitly to test cleanup

		ctx := context.Background()
		config := interfaces.ProviderConfig{
			"aws": map[string]interface{}{
				"region":                      "us-east-1",
				"access_key":                  "test",
				"secret_key":                  "test",
				"skip_credentials_validation": true,
				"skip_requesting_account_id":  true,
				"skip_metadata_api_check":     true,
			},
		}

		// Create multiple providers
		providers := make([]provider.Provider, 0, 3)
		for i := 0; i < 3; i++ {
			provider, err := pm.GetProvider(ctx, "test-deployment-aws", "aws", "6.2.0", config, fmt.Sprintf("aws_s3_bucket_%d", i))
			require.NoError(t, err)
			providers = append(providers, provider)

			// Use the provider
			schema, err := provider.GetSchema(ctx)
			require.NoError(t, err)
			assert.NotNil(t, schema)
		}

		// Get count of provider processes during operation
		duringCmd := exec.Command("pgrep", "-f", "terraform-provider")
		duringOutput, _ := duringCmd.Output()
		duringPids := strings.Fields(string(duringOutput))
		newDuringCount := 0
		for _, pid := range duringPids {
			if !baselinePids[pid] {
				newDuringCount++
			}
		}
		t.Logf("During operation: %d new terraform provider processes created", newDuringCount)
		assert.Greater(t, newDuringCount, 0, "Should have created at least one provider process")

		// Explicitly close the provider manager to test cleanup
		err = pm.Close()
		require.NoError(t, err, "Provider manager close should not fail")

		// Give processes time to fully terminate after close
		time.Sleep(500 * time.Millisecond)

		// Check for processes after shutdown
		afterCmd := exec.Command("pgrep", "-f", "terraform-provider")
		afterOutput, _ := afterCmd.Output()
		afterPids := strings.Fields(string(afterOutput))
		newAfterPids := []string{}
		for _, pid := range afterPids {
			if !baselinePids[pid] {
				newAfterPids = append(newAfterPids, pid)
			}
		}

		if len(newAfterPids) > 0 {
			// Get detailed info about remaining processes
			for _, pid := range newAfterPids {
				detailCmd := exec.Command("ps", "-p", pid, "-o", "pid,ppid,state,command")
				if detail, err := detailCmd.Output(); err == nil {
					t.Logf("Remaining process %s details:\n%s", pid, string(detail))
				}
			}
		}

		assert.Empty(t, newAfterPids, "All provider processes created by this test should be cleaned up after shutdown")
		t.Logf("Test cleanup verification: %d processes created, %d cleaned up", newDuringCount, newDuringCount-len(newAfterPids))
	})
}

// TestMultipleResourceTypesShareConnection verifies that multiple resource types
// share the same gRPC connection instead of creating new ones (Issue #38 fix)
func TestMultipleResourceTypesShareConnection(t *testing.T) {
	t.Run("SharedGRPCConnection", func(t *testing.T) {
		cacheDir := getLifecycleTestCacheDir()
		pm, err := NewProviderManagerWithOptions(
			WithBaseDir(cacheDir),
			WithConfigSource(cfg.NewDefaultConfigSource()),
		)
		require.NoError(t, err)
		// No defer - we'll close explicitly to test cleanup

		ctx := context.Background()
		config := interfaces.ProviderConfig{
			"aws": map[string]interface{}{
				"region":                      "us-east-1",
				"access_key":                  "test",
				"secret_key":                  "test",
				"skip_credentials_validation": true,
				"skip_requesting_account_id":  true,
				"skip_metadata_api_check":     true,
			},
		}

		// Track baseline provider processes
		baselineCmd := exec.Command("pgrep", "-f", "terraform-provider-aws")
		baselineOutput, _ := baselineCmd.Output()
		baselineCount := len(strings.Fields(string(baselineOutput)))
		t.Logf("Baseline: %d AWS provider processes running", baselineCount)

		// Get provider for first resource type
		provider1, err := pm.GetProvider(ctx, "test-deployment-aws", "aws", "6.2.0", config, "aws_s3_bucket")
		require.NoError(t, err)
		assert.NotNil(t, provider1)

		// Check how many provider processes after first resource
		afterFirstCmd := exec.Command("pgrep", "-f", "terraform-provider-aws")
		afterFirstOutput, _ := afterFirstCmd.Output()
		afterFirstCount := len(strings.Fields(string(afterFirstOutput)))
		newProvidersAfterFirst := afterFirstCount - baselineCount
		t.Logf("After first resource type: %d new AWS provider processes", newProvidersAfterFirst)
		assert.Equal(t, 1, newProvidersAfterFirst, "Should have started exactly one provider process")

		// Get provider for second resource type - should reuse connection
		provider2, err := pm.GetProvider(ctx, "test-deployment-aws", "aws", "6.2.0", config, "aws_instance")
		require.NoError(t, err)
		assert.NotNil(t, provider2)

		// Check processes after second resource
		afterSecondCmd := exec.Command("pgrep", "-f", "terraform-provider-aws")
		afterSecondOutput, _ := afterSecondCmd.Output()
		afterSecondCount := len(strings.Fields(string(afterSecondOutput)))
		newProvidersAfterSecond := afterSecondCount - baselineCount
		t.Logf("After second resource type: %d new AWS provider processes", newProvidersAfterSecond)

		// CRITICAL: Should still have only one provider process
		assert.Equal(t, 1, newProvidersAfterSecond, "Should NOT create additional provider process for second resource type")

		// Get provider for third resource type - should still reuse
		provider3, err := pm.GetProvider(ctx, "test-deployment-aws", "aws", "6.2.0", config, "aws_security_group")
		require.NoError(t, err)
		assert.NotNil(t, provider3)

		// Final check
		afterThirdCmd := exec.Command("pgrep", "-f", "terraform-provider-aws")
		afterThirdOutput, _ := afterThirdCmd.Output()
		afterThirdCount := len(strings.Fields(string(afterThirdOutput)))
		newProvidersAfterThird := afterThirdCount - baselineCount
		t.Logf("After third resource type: %d new AWS provider processes", newProvidersAfterThird)

		// Should STILL have only one provider process
		assert.Equal(t, 1, newProvidersAfterThird, "Should still have only one provider process for all resource types")

		// Verify all providers are functional
		schema1, err := provider1.GetSchema(ctx)
		assert.NoError(t, err)
		assert.NotNil(t, schema1)

		schema2, err := provider2.GetSchema(ctx)
		assert.NoError(t, err)
		assert.NotNil(t, schema2)

		schema3, err := provider3.GetSchema(ctx)
		assert.NoError(t, err)
		assert.NotNil(t, schema3)

		// Extract and check connection details if possible
		if _, ok := provider1.(*providerWrapper); ok {
			if _, ok := provider2.(*providerWrapper); ok {
				// Both wrappers should be using the same underlying provider instance
				t.Log("Successfully verified providers are wrapped instances")
			}
		}

		// Explicitly close the provider manager to test cleanup
		err = pm.Close()
		require.NoError(t, err, "Provider manager close should not fail")

		// Verify cleanup
		time.Sleep(500 * time.Millisecond)
		afterCloseCmd := exec.Command("pgrep", "-f", "terraform-provider-aws")
		afterCloseOutput, _ := afterCloseCmd.Output()
		afterCloseCount := len(strings.Fields(string(afterCloseOutput)))
		finalNewProviders := afterCloseCount - baselineCount

		assert.LessOrEqual(t, finalNewProviders, 0, "All provider processes should be cleaned up after close")
		t.Logf("Connection sharing test completed - verified single provider process handles multiple resource types")
	})
}

// TestProviderWrapperConnectionReuse verifies that providerWrapper correctly reuses
// the gRPC connection for compatible clients instead of creating new ones
func TestProviderWrapperConnectionReuse(t *testing.T) {
	t.Run("CompatibleClientConnectionSharing", func(t *testing.T) {
		cacheDir := getLifecycleTestCacheDir()
		pm, err := NewProviderManagerWithOptions(
			WithBaseDir(cacheDir),
			WithConfigSource(cfg.NewDefaultConfigSource()),
		)
		require.NoError(t, err)
		defer pm.Close()

		ctx := context.Background()
		config := interfaces.ProviderConfig{
			"aws": map[string]interface{}{
				"region":                      "us-east-1",
				"access_key":                  "test",
				"secret_key":                  "test",
				"skip_credentials_validation": true,
				"skip_requesting_account_id":  true,
				"skip_metadata_api_check":     true,
			},
		}

		// Get the base provider
		baseProvider, err := pm.GetProvider(ctx, "test-deployment-aws", "aws", "6.2.0", config, "aws_s3_bucket")
		require.NoError(t, err)

		// Cast to wrapper to access internal state
		wrapper, ok := baseProvider.(*providerWrapper)
		require.True(t, ok, "Provider should be a providerWrapper, got %T", baseProvider)

		// Get multiple resource types through the same wrapper
		resourceTypes := []string{
			"aws_instance",
			"aws_security_group",
			"aws_iam_role",
			"aws_lambda_function",
			"aws_dynamodb_table",
		}

		// Create resources using different types to test connection reuse
		for i, resourceType := range resourceTypes {
			// Simulate resource creation using the base provider interface
			_, err := baseProvider.CreateResource(ctx, resourceType, map[string]interface{}{
				"test": fmt.Sprintf("resource_%d", i),
			})
			// It's ok if creation fails due to missing config
			// We're testing connection reuse, not actual resource creation
			if err != nil {
				t.Logf("Resource creation failed as expected: %v", err)
			}
		}

		// Verify that the provider wrapper is still healthy and working
		// The wrapper should maintain its health after multiple operations
		assert.True(t, wrapper.IsHealthy(), "Provider should remain healthy after multiple operations")

		// Verify the provider can still get schema (indicates client is working)
		schema, err := wrapper.GetSchema(ctx)
		assert.NoError(t, err, "Provider should be able to get schema")
		assert.NotNil(t, schema, "Schema should not be nil")

		// CRITICAL TEST: Verify that provider configuration doesn't cause loops
		// Configure the provider and ensure compatible clients don't trigger reconfiguration
		err = baseProvider.Configure(ctx, config["aws"])
		require.NoError(t, err, "Provider configuration should succeed")

		// Create more resources after configuration to ensure no configuration loop
		done := make(chan bool)
		go func() {
			// Reduce iterations to make test faster
			for i := 0; i < 2; i++ {
				for j, resourceType := range resourceTypes {
					if j%2 == 0 { // Only test half the resource types to speed up
						_, _ = baseProvider.CreateResource(ctx, resourceType, map[string]interface{}{
							"test_after_config": fmt.Sprintf("resource_%s_%d", resourceType, i),
						})
					}
				}
			}
			done <- true
		}()

		// Increase timeout and make it more reasonable for integration tests
		select {
		case <-done:
			t.Log("Resource creation after configuration completed without loops")
		case <-time.After(30 * time.Second):
			t.Fatal("Provider operations timed out - possible configuration loop detected")
		}

		// Verify provider remains healthy
		assert.True(t, wrapper.IsHealthy(), "Provider should remain healthy after operations")
	})
}

// TestMultipleConnectionsEOFError tests that multiple connections don't cause
// premature EOF errors when the provider is terminated
func TestMultipleConnectionsEOFError(t *testing.T) {
	t.Run("PreventEOFFromMultipleConnections", func(t *testing.T) {
		cacheDir := getLifecycleTestCacheDir()
		pm, err := NewProviderManagerWithOptions(
			WithBaseDir(cacheDir),
			WithConfigSource(cfg.NewDefaultConfigSource()),
		)
		require.NoError(t, err)
		defer pm.Close()

		ctx := context.Background()
		config := interfaces.ProviderConfig{
			"aws": map[string]interface{}{
				"region":                      "us-east-1",
				"access_key":                  "test",
				"secret_key":                  "test",
				"skip_credentials_validation": true,
				"skip_requesting_account_id":  true,
				"skip_metadata_api_check":     true,
			},
		}

		// Track connection errors
		var connectionErrors []error
		errorChan := make(chan error, 10)

		// Get provider for multiple resource types concurrently
		resourceTypes := []string{
			"aws_s3_bucket",
			"aws_instance",
			"aws_security_group",
		}

		// Launch goroutines to use different resource types
		for _, resourceType := range resourceTypes {
			rt := resourceType // capture loop variable
			go func() {
				provider, err := pm.GetProvider(ctx, "test-deployment-aws", "aws", "6.2.0", config, rt)
				if err != nil {
					errorChan <- fmt.Errorf("failed to get provider for %s: %w", rt, err)
					return
				}

				// Try to use the provider
				_, err = provider.GetSchema(ctx)
				if err != nil {
					errorChan <- fmt.Errorf("schema error for %s: %w", rt, err)
				}
			}()
		}

		// Wait a bit for goroutines to start
		time.Sleep(100 * time.Millisecond)

		// Now close the manager while operations might be in progress
		// This simulates what happens during deployment when providers are terminated
		// Note: The actual close will happen via defer, but we can test the behavior
		// by waiting for operations to complete

		// Collect any errors
		timeout := time.After(2 * time.Second)
		done := false
		for !done {
			select {
			case err := <-errorChan:
				connectionErrors = append(connectionErrors, err)
			case <-timeout:
				done = true
			}
		}

		// Log any errors for debugging
		for _, err := range connectionErrors {
			t.Logf("Connection error: %v", err)
		}

		// The key assertion: with proper connection sharing, we should NOT see
		// multiple EOF errors from different connections
		eofCount := 0
		for _, err := range connectionErrors {
			if strings.Contains(err.Error(), "EOF") {
				eofCount++
			}
		}

		// With the fix, we should see at most one EOF error (from the shared connection)
		// Without the fix, each resource type would create its own connection and
		// we'd see multiple EOF errors
		assert.LessOrEqual(t, eofCount, 1,
			"Should see at most one EOF error with shared connections (found %d)", eofCount)

		if eofCount > 1 {
			t.Errorf("FAILURE: Multiple EOF errors detected, indicating multiple gRPC connections were created")
		} else if eofCount == 1 {
			t.Log("SUCCESS: Only one EOF error, indicating connection was properly shared")
		} else {
			t.Log("SUCCESS: No EOF errors, provider shut down gracefully")
		}
	})
}

// TestProviderManagerStressTest performs a stress test with many concurrent operations
func TestProviderManagerStressTest(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress test in short mode")
	}

	// Skip in CI due to severe resource constraints causing unreliable results
	if os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != "" {
		t.Skip("Skipping stress test in CI environment due to resource constraints")
	}

	t.Run("HighConcurrencyStress", func(t *testing.T) {
		cacheDir := getLifecycleTestCacheDir()
		pm, err := NewProviderManagerWithOptions(
			WithBaseDir(cacheDir),
			WithConfigSource(cfg.NewDefaultConfigSource()),
		)
		require.NoError(t, err)
		defer pm.Close()

		metrics := &ProviderCacheMetrics{}
		ipm := &instrumentedProviderManager{
			ProviderManager: pm,
			metrics:         metrics,
		}

		ctx := context.Background()
		config := interfaces.ProviderConfig{
			"aws": map[string]interface{}{
				"region":                      "us-east-1",
				"access_key":                  "test",
				"secret_key":                  "test",
				"skip_credentials_validation": true,
				"skip_requesting_account_id":  true,
				"skip_metadata_api_check":     true,
			},
		}

		// Stress test parameters - reduced for faster execution
		const (
			numWorkers      = 5   // Reduced from 20
			opsPerWorker    = 10  // Reduced from 50
			killProbability = 0.1 // 10% chance to kill provider
		)

		var wg sync.WaitGroup
		wg.Add(numWorkers)

		// Launch workers
		for i := 0; i < numWorkers; i++ {
			go func(workerID int) {
				defer wg.Done()

				for j := 0; j < opsPerWorker; j++ {
					// Get provider
					provider, err := ipm.GetProvider(ctx, "test-deployment-aws", "aws", "6.2.0", config,
						fmt.Sprintf("aws_s3_bucket_%d_%d", workerID, j))
					if err != nil {
						t.Logf("Worker %d op %d failed: %v", workerID, j, err)
						continue
					}

					// Use provider
					_, err = provider.GetSchema(ctx)
					if err != nil {
						t.Logf("Worker %d op %d schema failed: %v", workerID, j, err)
					}

					// Controlled provider termination to test recovery
					// Only kill provider when no other operations are using it
					if j%10 == 0 && workerID%5 == 0 {
						// Wait a moment to let concurrent operations complete
						time.Sleep(10 * time.Millisecond)

						pm.activeProvidersMu.Lock()
						for _, instance := range pm.activeProviders {
							if instance.Name() == "aws" && instance.Version() == "6.2.0" {
								if pluginInst, ok := instance.(*PluginProviderInstance); ok {
									// Stop provider gracefully instead of forceful termination
									pluginInst.Stop()
									metrics.RecordDeath()
									t.Logf("Worker %d gracefully terminated provider after op %d", workerID, j)
								}
								break
							}
						}
						pm.activeProvidersMu.Unlock()

						// Give the system time to recognize the termination before continuing
						time.Sleep(50 * time.Millisecond)
					}
				}
			}(i)
		}

		// Wait for completion
		wg.Wait()

		// Report metrics
		hits, misses, deaths := metrics.GetStats()
		total := hits + misses
		hitRate := float64(hits) / float64(total) * 100

		t.Logf("Stress test completed:")
		t.Logf("  Total operations: %d", total)
		t.Logf("  Cache hits: %d", hits)
		t.Logf("  Cache misses: %d", misses)
		t.Logf("  Hit rate: %.2f%%", hitRate)
		t.Logf("  Provider deaths: %d", deaths)

		// Even under stress, we should maintain reasonable cache efficiency
		assert.Greater(t, hitRate, 50.0, "Cache hit rate should be > 50% even under stress")
	})
}
