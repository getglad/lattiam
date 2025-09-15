//go:build !integration
// +build !integration

package interfaces_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/internal/mocks"
)

// Test that all interfaces can be used together
func TestInterfaceCompatibility(t *testing.T) {
	t.Parallel()
	// This test simply ensures all interfaces compile correctly
	// and can be used together in type definitions

	// Test that we can define variables of each interface type
	var (
		_ interfaces.StateStore               = nil
		_ interfaces.DeploymentQueue          = nil
		_ interfaces.ProviderLifecycleManager = nil
		_ interfaces.ProviderMonitor          = nil
		_ interfaces.DependencyResolver       = nil
		_ interfaces.InterpolationResolver    = nil
		_ interfaces.Pool                     = nil
		_ interfaces.ComponentFactory         = nil
		_ interfaces.UnifiedProvider          = nil
		_ interfaces.StateLock                = nil
	)

	// Test that we can use the types
	var (
		_ = interfaces.DeploymentStatusQueued
		_ = interfaces.ResourceStatePending
		_ = interfaces.ProviderStatusActive
		_ = interfaces.WorkerStatusIdle
		_ = interfaces.HealthStatusHealthy
		_ = interfaces.PlanActionCreate
	)

	// If this compiles, our interfaces are properly defined
	t.Log("All interfaces compile correctly")
}

// TestStateStoreContract verifies that StateStore implementations satisfy the interface contract
func TestStateStoreContract(t *testing.T) {
	t.Parallel()

	// Create a mock implementation
	store := mocks.NewMockStateStore()

	// Test the contract
	testStateStoreOperations(t, store)
}

// testStateStoreOperations tests common StateStore operations using the new interface
//
//nolint:gocognit,funlen,gocyclo // Comprehensive test with many scenarios
func testStateStoreOperations(t *testing.T, store interfaces.StateStore) {
	t.Helper()

	deploymentID := "test-deployment"
	ctx := context.Background()

	// Test deployment metadata operations
	t.Run("DeploymentMetadataOperations", func(t *testing.T) {
		t.Parallel()
		// Should not exist initially
		_, err := store.GetDeployment(ctx, deploymentID)
		if err == nil {
			t.Error("expected error for non-existent deployment")
		}

		// Create deployment metadata
		deployment := &interfaces.DeploymentMetadata{
			DeploymentID:  deploymentID,
			Status:        interfaces.DeploymentStatusQueued,
			CreatedAt:     time.Now(),
			UpdatedAt:     time.Now(),
			Configuration: map[string]interface{}{"test": "config"},
			ResourceCount: 1,
		}

		err = store.CreateDeployment(ctx, deployment)
		if err != nil {
			t.Fatalf("failed to create deployment: %v", err)
		}

		// Retrieve and verify
		retrieved, err := store.GetDeployment(ctx, deploymentID)
		if err != nil {
			t.Fatalf("failed to get deployment: %v", err)
		}

		if retrieved.Status != interfaces.DeploymentStatusQueued {
			t.Errorf("expected status 'queued', got %v", retrieved.Status)
		}

		// Update status
		err = store.UpdateDeploymentStatus(ctx, deploymentID, interfaces.DeploymentStatusProcessing)
		if err != nil {
			t.Fatalf("failed to update deployment status: %v", err)
		}

		// List deployments should include our deployment
		deployments, err := store.ListDeployments(ctx)
		if err != nil {
			t.Fatalf("failed to list deployments: %v", err)
		}

		found := false
		for _, dep := range deployments {
			if dep.DeploymentID == deploymentID {
				found = true
				if dep.Status != interfaces.DeploymentStatusProcessing {
					t.Errorf("expected updated status 'processing', got %v", dep.Status)
				}
				break
			}
		}
		if !found {
			t.Error("deployment not found in list")
		}

		// Delete deployment
		err = store.DeleteDeployment(ctx, deploymentID)
		if err != nil {
			t.Fatalf("failed to delete deployment: %v", err)
		}

		// Should not exist after deletion
		_, err = store.GetDeployment(ctx, deploymentID)
		if err == nil {
			t.Error("expected error after deletion")
		}
	})

	// Test Terraform state file operations
	t.Run("TerraformStateOperations", func(t *testing.T) {
		t.Parallel()
		stateDeploymentID := "terraform-state-test"
		stateData := []byte(`{"version": 4, "terraform_version": "1.0.0", "serial": 1, "lineage": "test-lineage"}`)

		// Should not exist initially
		_, err := store.LoadTerraformState(ctx, stateDeploymentID)
		if err == nil {
			t.Error("expected error for non-existent terraform state")
		}

		// Save terraform state
		err = store.SaveTerraformState(ctx, stateDeploymentID, stateData)
		if err != nil {
			t.Fatalf("failed to save terraform state: %v", err)
		}

		// Load and verify
		retrieved, err := store.LoadTerraformState(ctx, stateDeploymentID)
		if err != nil {
			t.Fatalf("failed to load terraform state: %v", err)
		}

		if string(retrieved) != string(stateData) {
			t.Errorf("terraform state data mismatch")
		}

		// Delete terraform state
		err = store.DeleteTerraformState(ctx, stateDeploymentID)
		if err != nil {
			t.Fatalf("failed to delete terraform state: %v", err)
		}

		// Should not exist after deletion
		_, err = store.LoadTerraformState(ctx, stateDeploymentID)
		if err == nil {
			t.Error("expected error after terraform state deletion")
		}
	})

	// Test locking operations
	t.Run("LockingOperations", func(t *testing.T) {
		t.Parallel()
		lockDeploymentID := "lock-test-deployment"

		// Acquire lock
		lock, err := store.LockDeployment(ctx, lockDeploymentID)
		if err != nil {
			t.Fatalf("failed to acquire lock: %v", err)
		}
		defer func() { _ = lock.Release() }()

		// Verify lock properties
		if lock.ID() == "" {
			t.Error("lock should have an ID")
		}

		if lock.DeploymentID() != lockDeploymentID {
			t.Errorf("expected deployment ID %s, got %s", lockDeploymentID, lock.DeploymentID())
		}

		if lock.CreatedAt().IsZero() {
			t.Error("lock should have creation time")
		}

		// Try to acquire again - should fail
		_, err = store.LockDeployment(ctx, lockDeploymentID)
		if err == nil {
			t.Error("expected error acquiring duplicate lock")
		}

		// Release lock
		err = lock.Release()
		if err != nil {
			t.Errorf("failed to release lock: %v", err)
		}

		// Should be able to acquire again after release
		lock2, err := store.LockDeployment(ctx, lockDeploymentID)
		if err != nil {
			t.Fatalf("failed to re-acquire lock: %v", err)
		}
		defer func() { _ = lock2.Release() }()
	})

	// Test health operations
	t.Run("HealthOperations", func(t *testing.T) {
		t.Parallel()
		// Ping should work
		err := store.Ping(ctx)
		if err != nil {
			t.Errorf("ping failed: %v", err)
		}
	})
}

// TestDeploymentQueueContract verifies DeploymentQueue implementations
func TestDeploymentQueueContract(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	mockQueue := new(mocks.DeploymentQueue)

	// Test Enqueue
	t.Run("Enqueue", func(t *testing.T) {
		deployment := &interfaces.QueuedDeployment{
			ID: "test-deployment-1",
			Request: &interfaces.DeploymentRequest{
				Resources: []interfaces.Resource{
					{
						Type: "test_resource",
						Name: "example",
						Properties: map[string]interface{}{
							"name": "test",
						},
					},
				},
			},
			Status:    interfaces.DeploymentStatusQueued,
			CreatedAt: time.Now(),
		}

		mockQueue.On("Enqueue", ctx, deployment).Return(nil).Once()

		err := mockQueue.Enqueue(ctx, deployment)
		require.NoError(t, err)
		mockQueue.AssertExpectations(t)
	})

	// Test Cancel
	t.Run("Cancel", func(t *testing.T) {
		deploymentID := "test-deployment-2"

		mockQueue.On("Cancel", ctx, deploymentID).Return(nil).Once()

		err := mockQueue.Cancel(ctx, deploymentID)
		require.NoError(t, err)
		mockQueue.AssertExpectations(t)
	})

	// Test GetMetrics
	t.Run("GetMetrics", func(t *testing.T) {
		expectedMetrics := interfaces.QueueMetrics{
			TotalEnqueued:    15,
			TotalDequeued:    13,
			CurrentDepth:     2,
			AverageWaitTime:  5 * time.Second,
			OldestDeployment: time.Now().Add(-10 * time.Minute),
		}

		mockQueue.On("GetMetrics").Return(expectedMetrics).Once()

		metrics := mockQueue.GetMetrics()
		assert.Equal(t, expectedMetrics, metrics)
		mockQueue.AssertExpectations(t)
	})

	// Test error scenarios
	t.Run("EnqueueError", func(t *testing.T) {
		deployment := &interfaces.QueuedDeployment{
			ID:        "test-deployment-error",
			Request:   &interfaces.DeploymentRequest{},
			Status:    interfaces.DeploymentStatusQueued,
			CreatedAt: time.Now(),
		}
		expectedErr := errors.New("queue full")

		mockQueue.On("Enqueue", ctx, deployment).Return(expectedErr).Once()

		err := mockQueue.Enqueue(ctx, deployment)
		require.Error(t, err)
		assert.Equal(t, expectedErr, err)
		mockQueue.AssertExpectations(t)
	})

	t.Run("CancelNotFound", func(t *testing.T) {
		deploymentID := "non-existent"
		expectedErr := errors.New("deployment not found")

		mockQueue.On("Cancel", ctx, deploymentID).Return(expectedErr).Once()

		err := mockQueue.Cancel(ctx, deploymentID)
		require.Error(t, err)
		assert.Equal(t, expectedErr, err)
		mockQueue.AssertExpectations(t)
	})
}
