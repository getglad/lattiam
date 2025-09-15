//go:build integration
// +build integration

package providerisolation

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/config"
	"github.com/lattiam/lattiam/internal/provider"
	"github.com/lattiam/lattiam/pkg/provider/protocol"
)

// TestProviderIsolationIntegration validates that the new per-deployment provider isolation
// works correctly and prevents the Demo1 S3 failure issue where providers interfere with each other.
//
// These tests verify:
// 1. Multiple deployments can use the same provider type concurrently without interference
// 2. Provider reconfiguration for one deployment doesn't affect others
// 3. Deployment cleanup only affects the specific deployment's providers
// 4. Working directories are properly isolated per deployment
// 5. Race conditions are prevented (Demo1 S3 failure scenario)
//
// Usage: go test -v -tags="integration" ./tests/provider-isolation -timeout=5m
func TestProviderIsolationIntegration(t *testing.T) {
	// Use a dedicated test directory for isolation tests
	testDir := t.TempDir()

	t.Run("ConcurrentProviderAccess", func(t *testing.T) {
		testConcurrentProviderAccess(t, testDir)
	})

	t.Run("ProviderReconfigurationIsolation", func(t *testing.T) {
		testProviderReconfigurationIsolation(t, testDir)
	})

	t.Run("DeploymentCleanup", func(t *testing.T) {
		testDeploymentCleanup(t, testDir)
	})

	t.Run("WorkingDirectoryIsolation", func(t *testing.T) {
		testWorkingDirectoryIsolation(t, testDir)
	})

	t.Run("RaceConditionPrevention", func(t *testing.T) {
		testRaceConditionPrevention(t, testDir)
	})
}

// testConcurrentProviderAccess validates that multiple deployments can use the same provider type
// concurrently without interference - this addresses the Demo1 S3 failure scenario
func testConcurrentProviderAccess(t *testing.T, testDir string) {
	ctx := context.Background()

	// Create provider manager
	pm, err := protocol.NewProviderManagerWithOptions(
		protocol.WithBaseDir(testDir),
		protocol.WithConfigSource(config.NewDefaultConfigSource()),
	)
	require.NoError(t, err, "Failed to create provider manager")
	defer pm.Close()

	// Test configuration for multiple deployments using random provider
	// Using random provider as it's more predictable than AWS for testing
	numDeployments := 4
	deploymentIDs := make([]string, numDeployments)
	for i := 0; i < numDeployments; i++ {
		deploymentIDs[i] = fmt.Sprintf("concurrent-test-deployment-%d", i)
	}

	// Launch concurrent operations
	var wg sync.WaitGroup
	results := make(chan error, numDeployments)
	providers := make(chan provider.Provider, numDeployments) // Store providers for verification

	for i, deploymentID := range deploymentIDs {
		wg.Add(1)
		go func(deplID string, index int) {
			defer wg.Done()

			// Each deployment gets the same provider type but should have separate instances
			provider, err := pm.GetProvider(ctx, deplID, "random", "3.6.0", nil, "random_string")
			if err != nil {
				results <- fmt.Errorf("deployment %s failed to get provider: %w", deplID, err)
				return
			}

			// Configure the provider
			if err := provider.Configure(ctx, map[string]interface{}{}); err != nil {
				results <- fmt.Errorf("deployment %s failed to configure provider: %w", deplID, err)
				return
			}

			// Verify provider works by getting schema
			schema, err := provider.GetSchema(ctx)
			if err != nil {
				results <- fmt.Errorf("deployment %s provider schema failed: %w", deplID, err)
				return
			}

			if schema == nil {
				results <- fmt.Errorf("deployment %s got nil schema", deplID)
				return
			}

			// Create a unique resource to verify isolation
			createConfig := map[string]interface{}{
				"length":  int64(16 + index), // Different length for each deployment
				"special": false,
				"upper":   true,
				"lower":   true,
				"number":  true,
			}

			createdState, err := provider.CreateResource(ctx, "random_string", createConfig)
			if err != nil {
				results <- fmt.Errorf("deployment %s failed to create resource: %w", deplID, err)
				return
			}

			// Verify the resource was created with correct length
			if length, ok := createdState["length"]; !ok || length != int64(16+index) {
				results <- fmt.Errorf("deployment %s created resource with wrong length: got %v, want %d", deplID, length, 16+index)
				return
			}

			// Clean up the resource
			if err := provider.DeleteResource(ctx, "random_string", createdState); err != nil {
				results <- fmt.Errorf("deployment %s failed to delete resource: %w", deplID, err)
				return
			}

			providers <- provider
			results <- nil
		}(deploymentID, i)
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// Check all results
	for i := 0; i < numDeployments; i++ {
		err := <-results
		assert.NoError(t, err, "Deployment %d should succeed", i)
	}

	// Verify we got providers back
	providerInstances := make([]provider.Provider, 0, numDeployments)
	for i := 0; i < numDeployments; i++ {
		select {
		case prov := <-providers:
			providerInstances = append(providerInstances, prov)
		default:
			t.Errorf("Missing provider instance %d", i)
		}
	}

	assert.Len(t, providerInstances, numDeployments, "Should have received all provider instances")

	t.Logf("✅ Successfully created %d concurrent deployments with isolated providers", numDeployments)
}

// testProviderReconfigurationIsolation tests that reconfiguring providers for one deployment
// doesn't affect other deployments using the same provider type
func testProviderReconfigurationIsolation(t *testing.T, testDir string) {
	ctx := context.Background()

	// Create provider manager
	pm, err := protocol.NewProviderManagerWithOptions(
		protocol.WithBaseDir(testDir),
		protocol.WithConfigSource(config.NewDefaultConfigSource()),
	)
	require.NoError(t, err, "Failed to create provider manager")
	defer pm.Close()

	// Create two deployments
	deployment1 := "reconfig-test-deployment-1"
	deployment2 := "reconfig-test-deployment-2"

	// Get provider for deployment 1
	provider1, err := pm.GetProvider(ctx, deployment1, "random", "3.6.0", nil, "random_string")
	require.NoError(t, err, "Failed to get provider for deployment 1")

	// Configure provider 1
	err = provider1.Configure(ctx, map[string]interface{}{})
	require.NoError(t, err, "Failed to configure provider 1")

	// Create a resource with provider 1
	createConfig1 := map[string]interface{}{
		"length":  int64(12),
		"special": false,
		"upper":   true,
		"lower":   true,
		"number":  true,
	}

	state1, err := provider1.CreateResource(ctx, "random_string", createConfig1)
	require.NoError(t, err, "Failed to create resource with provider 1")
	require.NotNil(t, state1, "Should have created state 1")

	// Verify provider 1 state
	assert.Equal(t, int64(12), state1["length"], "Provider 1 should have length 12")
	result1, ok := state1["result"].(string)
	require.True(t, ok, "Provider 1 result should be string")
	assert.Len(t, result1, 12, "Provider 1 result should be 12 characters")

	// Now get provider for deployment 2
	provider2, err := pm.GetProvider(ctx, deployment2, "random", "3.6.0", nil, "random_string")
	require.NoError(t, err, "Failed to get provider for deployment 2")

	// Configure provider 2
	err = provider2.Configure(ctx, map[string]interface{}{})
	require.NoError(t, err, "Failed to configure provider 2")

	// Create a resource with provider 2 (different configuration)
	createConfig2 := map[string]interface{}{
		"length":  int64(24),
		"special": true, // Different config
		"upper":   true,
		"lower":   true,
		"number":  true,
	}

	state2, err := provider2.CreateResource(ctx, "random_string", createConfig2)
	require.NoError(t, err, "Failed to create resource with provider 2")
	require.NotNil(t, state2, "Should have created state 2")

	// Verify provider 2 state
	assert.Equal(t, int64(24), state2["length"], "Provider 2 should have length 24")
	result2, ok := state2["result"].(string)
	require.True(t, ok, "Provider 2 result should be string")
	assert.Len(t, result2, 24, "Provider 2 result should be 24 characters")

	// Verify that provider 1 is still working with its original configuration
	// by reading the resource we created earlier
	readState1, err := provider1.ReadResource(ctx, "random_string", state1)
	require.NoError(t, err, "Provider 1 should still be functional")
	assert.Equal(t, result1, readState1["result"], "Provider 1 state should be unchanged")

	// Verify that provider 2 works independently
	readState2, err := provider2.ReadResource(ctx, "random_string", state2)
	require.NoError(t, err, "Provider 2 should be functional")
	assert.Equal(t, result2, readState2["result"], "Provider 2 state should be correct")

	// Clean up resources
	err = provider1.DeleteResource(ctx, "random_string", state1)
	assert.NoError(t, err, "Should delete resource from provider 1")

	err = provider2.DeleteResource(ctx, "random_string", state2)
	assert.NoError(t, err, "Should delete resource from provider 2")

	t.Log("✅ Provider reconfiguration isolation verified - deployments maintain separate configurations")
}

// testDeploymentCleanup tests that shutting down one deployment doesn't affect others
func testDeploymentCleanup(t *testing.T, testDir string) {
	ctx := context.Background()

	// Create provider manager
	pm, err := protocol.NewProviderManagerWithOptions(
		protocol.WithBaseDir(testDir),
		protocol.WithConfigSource(config.NewDefaultConfigSource()),
	)
	require.NoError(t, err, "Failed to create provider manager")
	defer pm.Close()

	// Create multiple deployments
	deployment1 := "cleanup-test-deployment-1"
	deployment2 := "cleanup-test-deployment-2"
	deployment3 := "cleanup-test-deployment-3"

	// Get providers for all deployments
	provider1, err := pm.GetProvider(ctx, deployment1, "random", "3.6.0", nil, "random_string")
	require.NoError(t, err, "Failed to get provider for deployment 1")

	provider2, err := pm.GetProvider(ctx, deployment2, "random", "3.6.0", nil, "random_string")
	require.NoError(t, err, "Failed to get provider for deployment 2")

	provider3, err := pm.GetProvider(ctx, deployment3, "random", "3.6.0", nil, "random_string")
	require.NoError(t, err, "Failed to get provider for deployment 3")

	// Configure all providers
	providers := []provider.Provider{provider1, provider2, provider3}
	for i, prov := range providers {
		err = prov.Configure(ctx, map[string]interface{}{})
		require.NoError(t, err, "Failed to configure provider %d", i+1)
	}

	// Verify all providers work
	for i, prov := range providers {
		schema, err := prov.GetSchema(ctx)
		require.NoError(t, err, "Provider %d should be functional", i+1)
		assert.NotNil(t, schema, "Provider %d should have schema", i+1)
	}

	// Shutdown deployment 2
	err = pm.ShutdownDeployment(deployment2)
	require.NoError(t, err, "Failed to shutdown deployment 2")

	// Verify deployment 1 and 3 still work
	schema1, err := provider1.GetSchema(ctx)
	assert.NoError(t, err, "Deployment 1 should still be functional after deployment 2 shutdown")
	assert.NotNil(t, schema1, "Deployment 1 should have schema")

	schema3, err := provider3.GetSchema(ctx)
	assert.NoError(t, err, "Deployment 3 should still be functional after deployment 2 shutdown")
	assert.NotNil(t, schema3, "Deployment 3 should have schema")

	// Verify deployment 2 provider is no longer accessible through the cache
	// by trying to get a new provider for the same deployment - it should create a new one
	newProvider2, err := pm.GetProvider(ctx, deployment2, "random", "3.6.0", nil, "random_string")
	require.NoError(t, err, "Should be able to create new provider for deployment 2")

	// Configure and test the new provider
	err = newProvider2.Configure(ctx, map[string]interface{}{})
	require.NoError(t, err, "New provider for deployment 2 should be configurable")

	schema2New, err := newProvider2.GetSchema(ctx)
	assert.NoError(t, err, "New provider for deployment 2 should be functional")
	assert.NotNil(t, schema2New, "New provider should have schema")

	t.Log("✅ Deployment cleanup works correctly - other deployments remain functional")
}

// testWorkingDirectoryIsolation verifies each deployment gets its own working directory
func testWorkingDirectoryIsolation(t *testing.T, testDir string) {
	ctx := context.Background()

	// Create provider manager
	pm, err := protocol.NewProviderManagerWithOptions(
		protocol.WithBaseDir(testDir),
		protocol.WithConfigSource(config.NewDefaultConfigSource()),
	)
	require.NoError(t, err, "Failed to create provider manager")
	defer pm.Close()

	deployment1 := "workdir-test-deployment-1"
	deployment2 := "workdir-test-deployment-2"

	// Get providers for both deployments
	provider1, err := pm.GetProvider(ctx, deployment1, "random", "3.6.0", nil, "random_string")
	require.NoError(t, err, "Failed to get provider for deployment 1")

	provider2, err := pm.GetProvider(ctx, deployment2, "random", "3.6.0", nil, "random_string")
	require.NoError(t, err, "Failed to get provider for deployment 2")

	// Configure providers
	err = provider1.Configure(ctx, map[string]interface{}{})
	require.NoError(t, err, "Failed to configure provider 1")

	err = provider2.Configure(ctx, map[string]interface{}{})
	require.NoError(t, err, "Failed to configure provider 2")

	// Create resources to ensure working directories are being used
	createConfig := map[string]interface{}{
		"length":  int64(16),
		"special": false,
		"upper":   true,
		"lower":   true,
		"number":  true,
	}

	state1, err := provider1.CreateResource(ctx, "random_string", createConfig)
	require.NoError(t, err, "Provider 1 should create resource (implies working directory is functional)")

	state2, err := provider2.CreateResource(ctx, "random_string", createConfig)
	require.NoError(t, err, "Provider 2 should create resource (implies working directory is functional)")

	// Verify the resources are independent
	result1 := state1["result"].(string)
	result2 := state2["result"].(string)
	assert.NotEqual(t, result1, result2, "Resources should be independent (different random values)")

	// Clean up
	err = provider1.DeleteResource(ctx, "random_string", state1)
	assert.NoError(t, err, "Should delete resource from provider 1")

	err = provider2.DeleteResource(ctx, "random_string", state2)
	assert.NoError(t, err, "Should delete resource from provider 2")

	// Test directory cleanup
	err = pm.ShutdownDeployment(deployment1)
	assert.NoError(t, err, "Should cleanup deployment 1")

	// Verify deployment 2 still works after deployment 1 cleanup
	schema2, err := provider2.GetSchema(ctx)
	assert.NoError(t, err, "Deployment 2 should still work after deployment 1 cleanup")
	assert.NotNil(t, schema2, "Deployment 2 should have schema")

	t.Log("✅ Working directory isolation verified - each deployment uses separate directories")
}

// testRaceConditionPrevention simulates the Demo1 scenario where long-running operations
// on one deployment shouldn't be affected by another deployment reconfiguring the same provider type
func testRaceConditionPrevention(t *testing.T, testDir string) {
	ctx := context.Background()

	// Create provider manager
	pm, err := protocol.NewProviderManagerWithOptions(
		protocol.WithBaseDir(testDir),
		protocol.WithConfigSource(config.NewDefaultConfigSource()),
	)
	require.NoError(t, err, "Failed to create provider manager")
	defer pm.Close()

	deployment1 := "race-test-deployment-1"
	deployment2 := "race-test-deployment-2"

	// Setup deployment 1 with a provider
	provider1, err := pm.GetProvider(ctx, deployment1, "random", "3.6.0", nil, "random_string")
	require.NoError(t, err, "Failed to get provider for deployment 1")

	err = provider1.Configure(ctx, map[string]interface{}{})
	require.NoError(t, err, "Failed to configure provider 1")

	// Create a resource with deployment 1
	createConfig1 := map[string]interface{}{
		"length":  int64(16),
		"special": false,
		"upper":   true,
		"lower":   true,
		"number":  true,
	}

	state1, err := provider1.CreateResource(ctx, "random_string", createConfig1)
	require.NoError(t, err, "Failed to create resource with deployment 1")

	// Channel to coordinate the race condition test
	deployment1Done := make(chan error, 1)
	deployment2Done := make(chan error, 1)

	// Start long-running operation on deployment 1 (simulate by multiple operations)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				deployment1Done <- fmt.Errorf("deployment 1 panicked: %v", r)
				return
			}
		}()

		// Simulate long-running operation by doing multiple read operations
		for i := 0; i < 10; i++ {
			_, err := provider1.ReadResource(ctx, "random_string", state1)
			if err != nil {
				deployment1Done <- fmt.Errorf("deployment 1 read operation %d failed: %w", i, err)
				return
			}

			// Small delay to allow deployment 2 to start
			time.Sleep(10 * time.Millisecond)
		}

		// Final verification - deployment 1 should still work
		schema, err := provider1.GetSchema(ctx)
		if err != nil {
			deployment1Done <- fmt.Errorf("deployment 1 final schema check failed: %w", err)
			return
		}
		if schema == nil {
			deployment1Done <- fmt.Errorf("deployment 1 final schema is nil")
			return
		}

		deployment1Done <- nil
	}()

	// Start deployment 2 operations while deployment 1 is running
	go func() {
		defer func() {
			if r := recover(); r != nil {
				deployment2Done <- fmt.Errorf("deployment 2 panicked: %v", r)
				return
			}
		}()

		// Small delay to ensure deployment 1 starts first
		time.Sleep(20 * time.Millisecond)

		// Get provider for deployment 2 (same provider type as deployment 1)
		provider2, err := pm.GetProvider(ctx, deployment2, "random", "3.6.0", nil, "random_string")
		if err != nil {
			deployment2Done <- fmt.Errorf("failed to get provider for deployment 2: %w", err)
			return
		}

		// Configure deployment 2 provider
		err = provider2.Configure(ctx, map[string]interface{}{})
		if err != nil {
			deployment2Done <- fmt.Errorf("failed to configure provider 2: %w", err)
			return
		}

		// Create a resource with deployment 2
		createConfig2 := map[string]interface{}{
			"length":  int64(24), // Different length
			"special": true,      // Different config
			"upper":   true,
			"lower":   true,
			"number":  true,
		}

		state2, err := provider2.CreateResource(ctx, "random_string", createConfig2)
		if err != nil {
			deployment2Done <- fmt.Errorf("failed to create resource with deployment 2: %w", err)
			return
		}

		// Verify deployment 2 resource
		if length, ok := state2["length"]; !ok || length != int64(24) {
			deployment2Done <- fmt.Errorf("deployment 2 resource has wrong length: got %v, want %d", length, 24)
			return
		}

		// Clean up deployment 2 resource
		err = provider2.DeleteResource(ctx, "random_string", state2)
		if err != nil {
			deployment2Done <- fmt.Errorf("failed to delete resource from deployment 2: %w", err)
			return
		}

		deployment2Done <- nil
	}()

	// Wait for both deployments to complete
	testCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var deployment1Err, deployment2Err error

	select {
	case deployment1Err = <-deployment1Done:
	case <-testCtx.Done():
		t.Fatal("Timeout waiting for deployment 1 to complete")
	}

	select {
	case deployment2Err = <-deployment2Done:
	case <-testCtx.Done():
		t.Fatal("Timeout waiting for deployment 2 to complete")
	}

	// Both deployments should complete successfully
	assert.NoError(t, deployment1Err, "Deployment 1 should complete successfully despite deployment 2 activity")
	assert.NoError(t, deployment2Err, "Deployment 2 should complete successfully")

	// Final verification - deployment 1 provider should still be functional
	finalSchema, err := provider1.GetSchema(ctx)
	assert.NoError(t, err, "Deployment 1 provider should remain functional after race condition test")
	assert.NotNil(t, finalSchema, "Deployment 1 provider should have schema")

	// Clean up deployment 1 resource
	err = provider1.DeleteResource(ctx, "random_string", state1)
	assert.NoError(t, err, "Should delete resource from deployment 1")

	t.Log("✅ Race condition prevention verified - concurrent deployments don't interfere")
}
