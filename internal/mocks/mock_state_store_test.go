//go:build !integration
// +build !integration

package mocks_test

import (
	"context"
	"errors"
	"testing"

	"github.com/lattiam/lattiam/internal/mocks"
)

//nolint:gocognit,funlen,gocyclo // Complex mock test with multiple assertions
func TestMockStateStore(t *testing.T) {
	t.Parallel()

	// Test successful deployment state update
	t.Run("UpdateDeploymentState_Success", func(t *testing.T) {
		t.Parallel()
		// Create separate store instance for this test to avoid shared state
		store := mocks.NewMockStateStore()
		deploymentID := "test-deployment-1"
		state := map[string]interface{}{
			"status": "completed",
			"resources": map[string]interface{}{
				"aws_s3_bucket.test": map[string]interface{}{
					"id":     "test-bucket",
					"arn":    "arn:aws:s3:::test-bucket",
					"region": "us-east-1",
				},
			},
		}

		err := store.UpdateDeploymentState(deploymentID, state)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify the state was stored
		retrievedState, err := store.GetDeploymentState(deploymentID)
		if err != nil {
			t.Fatalf("unexpected error retrieving state: %v", err)
		}

		if retrievedState["status"] != "completed" {
			t.Errorf("expected status 'completed', got %v", retrievedState["status"])
		}

		// Check that the call was recorded
		calls := store.GetCalls()
		const expectedCallCount = 2 // One for UpdateDeploymentState, one for GetDeploymentState
		if len(calls) != expectedCallCount {
			t.Errorf("expected %d calls, got %d", expectedCallCount, len(calls))
		}
	})

	// Test error injection
	t.Run("UpdateDeploymentState_Error", func(t *testing.T) {
		t.Parallel()
		// Create separate store instance for this test to avoid shared state
		testStore := mocks.NewMockStateStore()
		// Configure mock to fail
		expectedErr := errors.New("simulated database error")
		testStore.SetShouldFail("UpdateDeploymentState", expectedErr)

		err := testStore.UpdateDeploymentState("test-deployment-2", map[string]interface{}{})
		if err == nil {
			t.Fatal("expected error, got nil")
		}

		if err.Error() != expectedErr.Error() {
			t.Errorf("expected error '%v', got '%v'", expectedErr, err)
		}

		// Reset error injection
		testStore.SetShouldFail("UpdateDeploymentState", nil)
	})

	// Test deployment locking
	t.Run("DeploymentLocking", func(t *testing.T) {
		t.Parallel()
		// Create separate store instance for this test to avoid shared state
		store := mocks.NewMockStateStore()
		deploymentID := "test-deployment-3"

		// Acquire lock
		lock, err := store.LockDeployment(context.Background(), deploymentID)
		if err != nil {
			t.Fatalf("unexpected error acquiring lock: %v", err)
		}

		// Ensure lock is released even if test fails
		defer func() {
			if lock != nil {
				_ = lock.Release()
			}
		}()

		// Try to acquire lock again - should fail
		_, err = store.LockDeployment(context.Background(), deploymentID)
		if err == nil {
			t.Fatal("expected error acquiring duplicate lock")
		}

		// Release lock
		err = lock.Release()
		if err != nil {
			t.Fatalf("unexpected error releasing lock: %v", err)
		}
		lock = nil // Mark as released to prevent double release

		// Should be able to acquire lock again
		lock2, err := store.LockDeployment(context.Background(), deploymentID)
		if err != nil {
			t.Fatalf("unexpected error re-acquiring lock: %v", err)
		}
		defer func() { _ = lock2.Release() }()
	})

	// Test batch operations
	t.Run("BatchOperations", func(t *testing.T) {
		t.Parallel()
		// Create separate store instance for this test to avoid shared state
		store := mocks.NewMockStateStore()
		resources := map[string]map[string]interface{}{
			"aws_s3_bucket/bucket1": {"id": "bucket1", "name": "test-bucket-1"},
			"aws_s3_bucket/bucket2": {"id": "bucket2", "name": "test-bucket-2"},
			"aws_s3_bucket/bucket3": {"id": "bucket3", "name": "test-bucket-3"},
		}

		// Update batch
		err := store.UpdateResourceStatesBatch(resources)
		if err != nil {
			t.Fatalf("unexpected error in batch update: %v", err)
		}

		// Retrieve batch
		keys := []string{"aws_s3_bucket/bucket1", "aws_s3_bucket/bucket2", "aws_s3_bucket/bucket3"}
		retrieved, err := store.GetResourceStatesBatch(keys)
		if err != nil {
			t.Fatalf("unexpected error in batch get: %v", err)
		}

		if len(retrieved) != 3 {
			t.Errorf("expected 3 resources, got %d", len(retrieved))
		}

		for key, expected := range resources {
			actual, exists := retrieved[key]
			if !exists {
				t.Errorf("resource %s not found in batch get", key)
				continue
			}
			if actual["id"] != expected["id"] {
				t.Errorf("resource %s: expected id %v, got %v", key, expected["id"], actual["id"])
			}
		}
	})

	// Verify all calls were recorded
	t.Run("CallTracking", func(t *testing.T) {
		t.Parallel()
		// Create separate store instance and perform operations to test call tracking
		testStore := mocks.NewMockStateStore()

		// Perform operations to generate calls
		_ = testStore.UpdateDeploymentState("test-deployment-1", map[string]interface{}{"status": "running"})
		_, _ = testStore.GetDeploymentState("test-deployment-1")
		lock, _ := testStore.LockDeployment(context.Background(), "test-deployment-1")
		if lock != nil {
			_ = lock.Release()
		}
		_ = testStore.UpdateResourceStatesBatch(map[string]map[string]interface{}{
			"test/resource": {"id": "test"},
		})
		_, _ = testStore.GetResourceStatesBatch([]string{"test/resource"})

		allCalls := testStore.GetCalls()
		if len(allCalls) == 0 {
			t.Error("expected calls to be recorded")
		}

		// Check that different methods were called
		methodCounts := make(map[string]int)
		for _, call := range allCalls {
			methodCounts[call.Method]++
		}

		expectedMethods := []string{
			"UpdateDeploymentState",
			"GetDeploymentState",
			"LockDeployment",
			"UnlockDeployment",
			"UpdateResourceStatesBatch",
			"GetResourceStatesBatch",
		}

		for _, method := range expectedMethods {
			if methodCounts[method] == 0 {
				t.Errorf("expected method %s to be called", method)
			}
		}
	})
}
