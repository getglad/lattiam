package system_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/internal/system"
)

//nolint:funlen // Comprehensive file backend integration test with multiple scenarios
func TestFileBackendIntegration(t *testing.T) {
	t.Parallel()
	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "file-backend-test-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	// Create factory
	factory := system.NewDefaultComponentFactory()

	// Create file backend configuration
	config := interfaces.StateStoreConfig{
		Type: "file",
		Options: map[string]interface{}{
			"path": tmpDir,
		},
	}

	// Create state store
	store, err := factory.CreateStateStore(config)
	require.NoError(t, err, "Should create file state store")
	require.NotNil(t, store)

	ctx := context.Background()

	// Test Ping
	err = store.Ping(ctx)
	require.NoError(t, err, "Ping should succeed")

	// Test CreateDeployment
	deployment := &interfaces.DeploymentMetadata{
		DeploymentID:  "test-deployment-1",
		Status:        interfaces.DeploymentStatusQueued,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
		ResourceCount: 0,
	}
	err = store.CreateDeployment(ctx, deployment)
	require.NoError(t, err, "Should create deployment")

	// Test GetDeployment
	retrieved, err := store.GetDeployment(ctx, "test-deployment-1")
	require.NoError(t, err, "Should retrieve deployment")
	assert.Equal(t, deployment.DeploymentID, retrieved.DeploymentID)
	assert.Equal(t, deployment.Status, retrieved.Status)

	// Test UpdateDeploymentStatus
	err = store.UpdateDeploymentStatus(ctx, "test-deployment-1", interfaces.DeploymentStatusCompleted)
	require.NoError(t, err, "Should update deployment status")

	// Verify status was updated
	retrieved, err = store.GetDeployment(ctx, "test-deployment-1")
	require.NoError(t, err)
	assert.Equal(t, interfaces.DeploymentStatusCompleted, retrieved.Status)

	// Test ListDeployments
	deployments, err := store.ListDeployments(ctx)
	require.NoError(t, err, "Should list deployments")
	assert.Len(t, deployments, 1)

	// Test SaveTerraformState
	stateData := []byte(`{"version": 4, "terraform_version": "1.5.0"}`)
	err = store.SaveTerraformState(ctx, "test-deployment-1", stateData)
	require.NoError(t, err, "Should save Terraform state")

	// Test LoadTerraformState
	loadedState, err := store.LoadTerraformState(ctx, "test-deployment-1")
	require.NoError(t, err, "Should load Terraform state")
	assert.JSONEq(t, string(stateData), string(loadedState), "State JSON should match")

	// Test Locking
	lock, err := store.LockDeployment(ctx, "test-deployment-1")
	require.NoError(t, err, "Should acquire lock")
	assert.NotNil(t, lock)

	// Try to acquire the same lock (should fail)
	lock2, err := store.LockDeployment(ctx, "test-deployment-1")
	require.Error(t, err, "Should fail to acquire already-held lock")
	assert.Nil(t, lock2)

	// Release lock
	err = store.UnlockDeployment(ctx, lock)
	require.NoError(t, err, "Should release lock")

	// Now should be able to acquire lock again
	lock3, err := store.LockDeployment(ctx, "test-deployment-1")
	require.NoError(t, err, "Should acquire lock after release")
	assert.NotNil(t, lock3)

	// Clean up
	err = store.UnlockDeployment(ctx, lock3)
	require.NoError(t, err)

	// Test DeleteTerraformState
	err = store.DeleteTerraformState(ctx, "test-deployment-1")
	require.NoError(t, err, "Should delete Terraform state")

	// Test DeleteDeployment
	err = store.DeleteDeployment(ctx, "test-deployment-1")
	require.NoError(t, err, "Should delete deployment")

	// Verify deployment is gone
	_, err = store.GetDeployment(ctx, "test-deployment-1")
	require.Error(t, err, "Should not find deleted deployment")

	// Verify files were created in the expected location
	entries, err := os.ReadDir(tmpDir)
	require.NoError(t, err)
	assert.NotEmpty(t, entries, "Should have created files/directories")
}

func TestFileBackendPersistence(t *testing.T) {
	t.Parallel()
	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "file-backend-persist-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	factory := system.NewDefaultComponentFactory()
	ctx := context.Background()

	// Create first store instance
	config := interfaces.StateStoreConfig{
		Type: "file",
		Options: map[string]interface{}{
			"path": tmpDir,
		},
	}

	store1, err := factory.CreateStateStore(config)
	require.NoError(t, err)

	// Create deployment with first store
	deployment := &interfaces.DeploymentMetadata{
		DeploymentID:  "persist-test",
		Status:        interfaces.DeploymentStatusCompleted,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
		ResourceCount: 1,
	}
	err = store1.CreateDeployment(ctx, deployment)
	require.NoError(t, err)

	// Save state with first store
	stateData := []byte(`{"version": 4, "serial": 1}`)
	err = store1.SaveTerraformState(ctx, "persist-test", stateData)
	require.NoError(t, err)

	// Create second store instance (simulating restart)
	store2, err := factory.CreateStateStore(config)
	require.NoError(t, err)

	// Should be able to retrieve deployment with second store
	retrieved, err := store2.GetDeployment(ctx, "persist-test")
	require.NoError(t, err, "Should retrieve deployment after restart")
	assert.Equal(t, deployment.DeploymentID, retrieved.DeploymentID)
	assert.Equal(t, deployment.Status, retrieved.Status)

	// Should be able to retrieve state with second store
	loadedState, err := store2.LoadTerraformState(ctx, "persist-test")
	require.NoError(t, err, "Should load state after restart")
	assert.JSONEq(t, string(stateData), string(loadedState), "State JSON should match after restart")
}

//nolint:funlen // Comprehensive concurrent file operations test
func TestFileBackendConcurrency(t *testing.T) {
	t.Parallel()
	// Create a temporary directory for testing
	tmpDir, err := os.MkdirTemp("", "file-backend-concurrent-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tmpDir) }()

	factory := system.NewDefaultComponentFactory()
	ctx := context.Background()

	config := interfaces.StateStoreConfig{
		Type: "file",
		Options: map[string]interface{}{
			"path": tmpDir,
		},
	}

	// Create multiple store instances (simulating concurrent processes)
	store1, err := factory.CreateStateStore(config)
	require.NoError(t, err)

	store2, err := factory.CreateStateStore(config)
	require.NoError(t, err)

	// Both should be able to create different deployments
	deployment1 := &interfaces.DeploymentMetadata{
		DeploymentID:  "concurrent-1",
		Status:        interfaces.DeploymentStatusQueued,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
		ResourceCount: 0,
	}
	err = store1.CreateDeployment(ctx, deployment1)
	require.NoError(t, err)

	deployment2 := &interfaces.DeploymentMetadata{
		DeploymentID:  "concurrent-2",
		Status:        interfaces.DeploymentStatusQueued,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
		ResourceCount: 0,
	}
	err = store2.CreateDeployment(ctx, deployment2)
	require.NoError(t, err)

	// Both stores should see both deployments
	deployments, err := store1.ListDeployments(ctx)
	require.NoError(t, err)
	assert.Len(t, deployments, 2)

	deployments, err = store2.ListDeployments(ctx)
	require.NoError(t, err)
	assert.Len(t, deployments, 2)

	// Test concurrent locking - only one should succeed
	lock1, err1 := store1.LockDeployment(ctx, "concurrent-1")
	lock2, err2 := store2.LockDeployment(ctx, "concurrent-1")

	// One should succeed, one should fail
	if err1 == nil {
		assert.NotNil(t, lock1)
		require.Error(t, err2)
		assert.Nil(t, lock2)
		// Clean up
		_ = store1.UnlockDeployment(ctx, lock1)
	} else {
		require.NoError(t, err2)
		assert.NotNil(t, lock2)
		// Clean up
		_ = store2.UnlockDeployment(ctx, lock2)
	}
}
