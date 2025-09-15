//go:build !integration
// +build !integration

package system

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/interfaces"
)

//nolint:funlen // Comprehensive robust file state store adapter test with error scenarios
func TestRobustFileStateStoreAdapter(t *testing.T) {
	t.Parallel()
	// Create temporary directory for test
	tempDir, err := os.MkdirTemp("", "robust_state_test_*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }() // Ignore cleanup error

	// Create robust adapter
	adapter, err := NewRobustFileStateStoreAdapter(tempDir)
	require.NoError(t, err)
	assert.NotNil(t, adapter)

	// Verify it implements the interface
	var _ interfaces.StateStore = adapter

	t.Run("AtomicWrites", func(t *testing.T) {
		t.Parallel()

		// Create a separate adapter for this test to avoid parallel test conflicts
		atomicTestDir, err := os.MkdirTemp("", "robust_atomic_test_*")
		require.NoError(t, err)
		defer func() { _ = os.RemoveAll(atomicTestDir) }() // Ignore cleanup error

		atomicAdapter, err := NewRobustFileStateStoreAdapter(atomicTestDir)
		require.NoError(t, err)

		deploymentID := "atomic-test-deployment"
		state := map[string]interface{}{
			"status":     "testing",
			"resources":  []string{"test_resource.one", "test_resource.two"},
			"created_at": time.Now().UTC().Format(time.RFC3339),
		}

		// Test atomic write
		err = atomicAdapter.UpdateDeploymentState(deploymentID, state)
		require.NoError(t, err)

		// Verify no temporary files left behind
		deploymentDir := filepath.Join(atomicTestDir, "deployments")
		entries, err := os.ReadDir(deploymentDir)
		require.NoError(t, err)

		for _, entry := range entries {
			assert.NotEqual(t, ".tmp", filepath.Ext(entry.Name()), "Temporary file should not exist: %s", entry.Name())
		}

		// Verify state was written correctly
		retrievedState, err := atomicAdapter.GetDeploymentState(deploymentID)
		require.NoError(t, err)
		assert.Equal(t, "testing", retrievedState["status"])
	})

	t.Run("StateVersioning", func(t *testing.T) {
		t.Parallel()
		deploymentID := "versioning-test"

		// First write
		state1 := map[string]interface{}{"version": 1, "data": "first"}
		err := adapter.UpdateDeploymentState(deploymentID, state1)
		require.NoError(t, err)

		// Read raw file to check versioning metadata
		deploymentFile := filepath.Join(tempDir, "deployments", deploymentID+".json")
		data, err := os.ReadFile(deploymentFile) // #nosec G304 - test file path construction
		require.NoError(t, err)

		var versionedState map[string]interface{}
		err = json.Unmarshal(data, &versionedState)
		require.NoError(t, err)

		// Verify versioning metadata exists
		assert.Contains(t, versionedState, "serial")
		assert.Contains(t, versionedState, "lineage")
		assert.Contains(t, versionedState, "updated_at")

		firstSerial := versionedState["serial"]
		lineage := versionedState["lineage"]

		// Second write should increment serial
		state2 := map[string]interface{}{"version": 2, "data": "second"}
		err = adapter.UpdateDeploymentState(deploymentID, state2)
		require.NoError(t, err)

		// Check serial incremented
		data, err = os.ReadFile(deploymentFile) // #nosec G304 - test file path construction
		require.NoError(t, err)
		err = json.Unmarshal(data, &versionedState)
		require.NoError(t, err)

		secondSerial := versionedState["serial"]
		assert.Greater(t, secondSerial, firstSerial, "Serial should increment")
		assert.Equal(t, lineage, versionedState["lineage"], "Lineage should remain the same")
	})

	t.Run("RobustLocking", func(t *testing.T) {
		t.Parallel()

		// Create a separate adapter for this test to avoid parallel test conflicts
		lockTestDir, err := os.MkdirTemp("", "robust_lock_test_*")
		require.NoError(t, err)
		defer func() { _ = os.RemoveAll(lockTestDir) }() // Ignore cleanup error

		lockAdapter, err := NewRobustFileStateStoreAdapter(lockTestDir)
		require.NoError(t, err)

		deploymentID := "locking-test"

		// First lock should succeed
		lock1, err := lockAdapter.LockDeployment(context.Background(), deploymentID)
		require.NoError(t, err)
		assert.NotNil(t, lock1)
		assert.Equal(t, deploymentID, lock1.DeploymentID())
		assert.NotEmpty(t, lock1.ID())

		// Verify lock files exist
		lockFile := filepath.Join(lockTestDir, "locks", deploymentID+".lock")
		lockInfoFile := filepath.Join(lockTestDir, "locks", deploymentID+".lock.info")

		_, err = os.Stat(lockFile)
		require.NoError(t, err, "Lock file should exist")

		_, err = os.Stat(lockInfoFile)
		require.NoError(t, err, "Lock info file should exist")

		// Second lock should fail
		lock2, err := lockAdapter.LockDeployment(context.Background(), deploymentID)
		require.Error(t, err)
		assert.Nil(t, lock2)
		assert.Contains(t, err.Error(), "already locked")

		// Release first lock
		err = lockAdapter.UnlockDeployment(context.Background(), lock1)
		require.NoError(t, err)

		// Verify lock files are cleaned up
		_, err = os.Stat(lockFile)
		assert.True(t, os.IsNotExist(err), "Lock file should be removed")

		_, err = os.Stat(lockInfoFile)
		assert.True(t, os.IsNotExist(err), "Lock info file should be removed")

		// New lock should now succeed
		lock3, err := lockAdapter.LockDeployment(context.Background(), deploymentID)
		require.NoError(t, err)
		assert.NotNil(t, lock3)

		// Clean up
		err = lockAdapter.UnlockDeployment(context.Background(), lock3)
		require.NoError(t, err)
	})

	t.Run("ConcurrentSafety", func(t *testing.T) {
		t.Parallel()
		deploymentID := "concurrent-test"
		numGoroutines := 10
		numOperationsPerGoroutine := 5

		var wg sync.WaitGroup
		errors := make(chan error, numGoroutines*numOperationsPerGoroutine)

		// Concurrent state updates
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(goroutineID int) {
				defer wg.Done()

				for j := 0; j < numOperationsPerGoroutine; j++ {
					state := map[string]interface{}{
						"goroutine": goroutineID,
						"operation": j,
						"timestamp": time.Now().UnixNano(),
					}

					if err := adapter.UpdateDeploymentState(deploymentID, state); err != nil {
						errors <- err
						return
					}
				}
			}(i)
		}

		wg.Wait()
		close(errors)

		// Check for errors
		for err := range errors {
			t.Errorf("Concurrent operation failed: %v", err)
		}

		// Verify final state is valid
		finalState, err := adapter.GetDeploymentState(deploymentID)
		require.NoError(t, err)
		assert.NotNil(t, finalState)
		assert.Contains(t, finalState, "goroutine")
		assert.Contains(t, finalState, "operation")
	})

	t.Run("ResourceOperations", func(t *testing.T) {
		t.Parallel()
		deploymentID := "resource-test"
		// Use canonical key format with dot notation
		baseResourceKey := string(interfaces.MakeResourceKey("aws_s3_bucket", "test"))
		resourceKey := deploymentID + "_" + baseResourceKey

		resourceState := map[string]interface{}{
			"bucket": "test-bucket-12345",
			"region": "us-east-1",
			"versioning": map[string]interface{}{
				"enabled": true,
			},
		}

		// Test resource state operations
		err := adapter.UpdateResourceState(resourceKey, resourceState)
		require.NoError(t, err)

		retrievedState, err := adapter.GetResourceState(resourceKey)
		require.NoError(t, err)
		assert.Equal(t, "test-bucket-12345", retrievedState["bucket"])

		// Debug: check what files exist in the resources directory
		resourceDir := filepath.Join(tempDir, "resources")
		entries, _ := os.ReadDir(resourceDir)
		t.Logf("Files in resources dir:")
		for _, entry := range entries {
			t.Logf("  - %s", entry.Name())
		}

		// Test get all resources for deployment
		allStates, err := adapter.GetAllResourceStates(deploymentID)
		require.NoError(t, err)
		// GetAllResourceStates should return keys without deployment ID prefix
		t.Logf("allStates: %+v", allStates)
		t.Logf("expectedKey: %s", baseResourceKey)
		assert.Contains(t, allStates, baseResourceKey)

		// Test resource deletion
		err = adapter.DeleteResourceState(resourceKey)
		require.NoError(t, err)

		retrievedState, err = adapter.GetResourceState(resourceKey)
		require.NoError(t, err)
		assert.Nil(t, retrievedState)
	})

	t.Run("TerraformStateCompatibility", func(t *testing.T) {
		t.Parallel()
		deploymentID := "terraform-compat-test"

		// Create some deployment and resource states
		deploymentState := map[string]interface{}{
			"status": "completed",
			"outputs": map[string]interface{}{
				"bucket_name": "my-terraform-bucket",
			},
		}
		err := adapter.UpdateDeploymentState(deploymentID, deploymentState)
		require.NoError(t, err)

		// Use canonical key format with dot notation
		resourceKey := deploymentID + "_" + string(interfaces.MakeResourceKey("aws_s3_bucket", "test"))
		resourceState := map[string]interface{}{
			"bucket": "my-terraform-bucket",
			"region": "us-west-2",
		}
		err = adapter.UpdateResourceState(resourceKey, resourceState)
		require.NoError(t, err)

		// Test Terraform state export
		tfState, err := adapter.ExportTerraformState(deploymentID)
		require.NoError(t, err)
		assert.NotNil(t, tfState)
		assert.Equal(t, 4, tfState.Version)
		assert.NotEmpty(t, tfState.Lineage)
		assert.Positive(t, tfState.Serial)
		assert.Len(t, tfState.Resources, 1)
		assert.Equal(t, "aws_s3_bucket", tfState.Resources[0].Type)
		assert.Equal(t, "test", tfState.Resources[0].Name)
	})

	t.Run("ErrorRecovery", func(t *testing.T) {
		t.Parallel()
		// Test behavior when files are corrupted or missing
		deploymentID := "error-recovery-test"

		// Try to get non-existent deployment
		_, err := adapter.GetDeploymentState(deploymentID)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")

		// Create deployment, then corrupt the file
		testState := map[string]interface{}{"status": "test"}
		err = adapter.UpdateDeploymentState(deploymentID, testState)
		require.NoError(t, err)

		// Corrupt the deployment file
		deploymentFile := filepath.Join(tempDir, "deployments", deploymentID+".json")
		err = os.WriteFile(deploymentFile, []byte("invalid json"), 0o600)
		require.NoError(t, err)

		// Should get parse error
		_, err = adapter.GetDeploymentState(deploymentID)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parse")
	})

	t.Run("HealthAndMaintenance", func(t *testing.T) {
		t.Parallel()

		// Create a separate adapter for this test to avoid parallel test conflicts
		healthTestDir, err := os.MkdirTemp("", "robust_health_test_*")
		require.NoError(t, err)
		defer func() { _ = os.RemoveAll(healthTestDir) }() // Ignore cleanup error

		healthAdapter, err := NewRobustFileStateStoreAdapter(healthTestDir)
		require.NoError(t, err)

		// Test ping
		err = healthAdapter.Ping(context.Background())
		require.NoError(t, err)

		// Test cleanup
		err = healthAdapter.Cleanup(24 * time.Hour)
		require.NoError(t, err)

		// Create old lock file for cleanup test
		lockDir := filepath.Join(healthTestDir, "locks")
		oldLockFile := filepath.Join(lockDir, "old-deployment.lock")
		err = os.WriteFile(oldLockFile, []byte("old lock"), 0o600)
		require.NoError(t, err)

		// Set old modification time
		oldTime := time.Now().Add(-48 * time.Hour)
		err = os.Chtimes(oldLockFile, oldTime, oldTime)
		require.NoError(t, err)

		// Cleanup should remove old lock
		err = healthAdapter.Cleanup(24 * time.Hour)
		require.NoError(t, err)

		_, err = os.Stat(oldLockFile)
		assert.True(t, os.IsNotExist(err), "Old lock file should be cleaned up")
	})
}

func TestComponentFactoryRobustStateStore(t *testing.T) {
	t.Parallel()
	_ = NewDefaultComponentFactory() // Factory available but file stores deprecated
}

// Test that demonstrates atomic write behavior
func TestAtomicWriteComparison(t *testing.T) {
	t.Parallel()
	tempDir, err := os.MkdirTemp("", "atomic_test_*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }() // Ignore cleanup error

	t.Run("RobustImplementationAtomicity", func(t *testing.T) {
		t.Parallel()
		adapter, err := NewRobustFileStateStoreAdapter(tempDir)
		require.NoError(t, err)

		deploymentID := "atomic-test"

		// Multiple concurrent writes
		var wg sync.WaitGroup
		numWrites := 10

		for i := 0; i < numWrites; i++ {
			wg.Add(1)
			go func(writeID int) {
				defer wg.Done()
				testState := map[string]interface{}{
					"write_id": writeID,
					"data":     make([]int, 100),
				}
				err := adapter.UpdateDeploymentState(deploymentID, testState)
				if err != nil {
					t.Errorf("Failed to update deployment state: %v", err)
				}
			}(i)
		}

		wg.Wait()

		// Verify final state is valid (not corrupted)
		finalState, err := adapter.GetDeploymentState(deploymentID)
		require.NoError(t, err)
		assert.NotNil(t, finalState)
		assert.Contains(t, finalState, "write_id")
	})
}
