package state

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

//nolint:paralleltest,gocognit,gocyclo // integration tests use shared resources, comprehensive test suite with multiple scenarios
func TestFileStore(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()

	store, err := NewFileStore(tempDir)
	if err != nil {
		t.Fatalf("Failed to create file store: %v", err)
	}
	defer func() {
		_ = store.Close()
	}()
	//nolint:paralleltest // integration tests use shared resources
	t.Run("Save and Load", func(t *testing.T) {
		deployment := &DeploymentState{
			ID:     "test-deployment-1",
			Name:   "Test Deployment",
			Status: "pending",
			Resources: []ResourceState{
				{
					Type: "aws_s3_bucket",
					Name: "test-bucket",
					Properties: map[string]interface{}{
						"bucket": "test-bucket-123",
					},
				},
			},
		}

		// Save deployment
		if err := store.Save(ctx, deployment); err != nil {
			t.Fatalf("Failed to save deployment: %v", err)
		}

		// Load deployment
		loaded, err := store.Load(ctx, deployment.ID)
		if err != nil {
			t.Fatalf("Failed to load deployment: %v", err)
		}

		if loaded.ID != deployment.ID {
			t.Errorf("Expected ID %s, got %s", deployment.ID, loaded.ID)
		}
		if loaded.Name != deployment.Name {
			t.Errorf("Expected name %s, got %s", deployment.Name, loaded.Name)
		}
		if loaded.Status != deployment.Status {
			t.Errorf("Expected status %s, got %s", deployment.Status, loaded.Status)
		}
	})

	//nolint:paralleltest // integration tests use shared resources
	t.Run("Update", func(t *testing.T) {
		deployment := &DeploymentState{
			ID:     "test-deployment-2",
			Name:   "Test Deployment 2",
			Status: "pending",
		}

		// Save initial deployment
		if err := store.Save(ctx, deployment); err != nil {
			t.Fatalf("Failed to save deployment: %v", err)
		}

		// Update deployment
		deployment.Status = "completed"
		if err := store.Update(ctx, deployment); err != nil {
			t.Fatalf("Failed to update deployment: %v", err)
		}

		// Load and verify
		loaded, err := store.Load(ctx, deployment.ID)
		if err != nil {
			t.Fatalf("Failed to load deployment: %v", err)
		}

		if loaded.Status != "completed" {
			t.Errorf("Expected status completed, got %s", loaded.Status)
		}
	})

	//nolint:paralleltest // integration tests use shared resources
	t.Run("Delete", func(t *testing.T) {
		deployment := &DeploymentState{
			ID:     "test-deployment-3",
			Name:   "Test Deployment 3",
			Status: "pending",
		}

		// Save deployment
		if err := store.Save(ctx, deployment); err != nil {
			t.Fatalf("Failed to save deployment: %v", err)
		}

		// Delete deployment
		if err := store.Delete(ctx, deployment.ID); err != nil {
			t.Fatalf("Failed to delete deployment: %v", err)
		}

		// Try to load - should fail
		_, err := store.Load(ctx, deployment.ID)
		if !errors.Is(err, ErrDeploymentNotFound) {
			t.Errorf("Expected ErrDeploymentNotFound, got %v", err)
		}
	})

	//nolint:paralleltest // integration tests use shared resources
	t.Run("List", func(t *testing.T) {
		// Clear existing deployments
		deploymentsDir := filepath.Join(tempDir, "deployments")
		os.RemoveAll(deploymentsDir)
		if err = os.MkdirAll(deploymentsDir, 0o750); err != nil {
			t.Fatalf("Failed to create deployments directory: %v", err)
		}

		// Save multiple deployments
		deployments := []*DeploymentState{
			{ID: "dep-1", Name: "Deployment 1", Status: "pending"},
			{ID: "dep-2", Name: "Deployment 2", Status: "completed"},
			{ID: "dep-3", Name: "Deployment 3", Status: "failed"},
			{ID: "dep-4", Name: "Deployment 4", Status: "completed"},
		}

		for _, dep := range deployments {
			if err := store.Save(ctx, dep); err != nil {
				t.Fatalf("Failed to save deployment %s: %v", dep.ID, err)
			}
		}

		// List all
		all, err := store.List(ctx, "")
		if err != nil {
			t.Fatalf("Failed to list deployments: %v", err)
		}
		if len(all) != 4 {
			t.Errorf("Expected 4 deployments, got %d", len(all))
		}

		// List by status
		completed, err := store.List(ctx, "completed")
		if err != nil {
			t.Fatalf("Failed to list completed deployments: %v", err)
		}
		if len(completed) != 2 {
			t.Errorf("Expected 2 completed deployments, got %d", len(completed))
		}
	})

	//nolint:paralleltest // integration tests use shared resources
	t.Run("Lock and Unlock", func(t *testing.T) {
		deploymentID := "test-deployment-lock"
		holderID := "holder-1"

		// Acquire lock
		lock, err := store.Lock(ctx, deploymentID, holderID, 5*time.Second)
		if err != nil {
			t.Fatalf("Failed to acquire lock: %v", err)
		}

		// Try to acquire again - should fail
		_, err = store.Lock(ctx, deploymentID, "holder-2", 5*time.Second)
		if !errors.Is(err, ErrLockAlreadyHeld) {
			t.Errorf("Expected ErrLockAlreadyHeld, got %v", err)
		}

		// Verify lock properties
		if lock.DeploymentID != deploymentID {
			t.Errorf("Expected deployment ID %s, got %s", deploymentID, lock.DeploymentID)
		}
		if lock.HolderID != holderID {
			t.Errorf("Expected holder ID %s, got %s", holderID, lock.HolderID)
		}

		// Unlock
		if err := store.Unlock(ctx, deploymentID, holderID); err != nil {
			t.Fatalf("Failed to unlock: %v", err)
		}

		// Now should be able to lock again
		lock2, err := store.Lock(ctx, deploymentID, "holder-2", 5*time.Second)
		if err != nil {
			t.Fatalf("Failed to acquire lock after unlock: %v", err)
		}
		if lock2.HolderID != "holder-2" {
			t.Errorf("Expected holder-2, got %s", lock2.HolderID)
		}
	})

	//nolint:paralleltest // integration tests use shared resources
	t.Run("Expired Lock", func(t *testing.T) {
		deploymentID := "test-deployment-expired"

		// Acquire lock with short duration
		_, err := store.Lock(ctx, deploymentID, "holder-1", 100*time.Millisecond)
		if err != nil {
			t.Fatalf("Failed to acquire lock: %v", err)
		}

		// Wait for lock to expire
		time.Sleep(200 * time.Millisecond)

		// Should be able to acquire new lock
		lock, err := store.Lock(ctx, deploymentID, "holder-2", 5*time.Second)
		if err != nil {
			t.Fatalf("Failed to acquire lock after expiry: %v", err)
		}
		if lock.HolderID != "holder-2" {
			t.Errorf("Expected holder-2, got %s", lock.HolderID)
		}
	})

	//nolint:paralleltest // integration tests use shared resources
	t.Run("File Persistence", func(t *testing.T) {
		deployment := &DeploymentState{
			ID:     "test-persistence",
			Name:   "Persistence Test",
			Status: "completed",
		}

		// Save deployment
		if err := store.Save(ctx, deployment); err != nil {
			t.Fatalf("Failed to save deployment: %v", err)
		}

		// Close store
		store.Close()

		// Create new store instance
		store2, err := NewFileStore(tempDir)
		if err != nil {
			t.Fatalf("Failed to create new store: %v", err)
		}
		defer store2.Close()

		// Load deployment
		loaded, err := store2.Load(ctx, deployment.ID)
		if err != nil {
			t.Fatalf("Failed to load deployment from new store: %v", err)
		}

		if loaded.ID != deployment.ID {
			t.Errorf("Expected ID %s, got %s", deployment.ID, loaded.ID)
		}
		if loaded.Status != deployment.Status {
			t.Errorf("Expected status %s, got %s", deployment.Status, loaded.Status)
		}
	})
}

//nolint:paralleltest,gocognit // integration tests use shared resources, edge case testing with multiple scenarios
func TestFileStoreEdgeCases(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()

	store, err := NewFileStore(tempDir)
	if err != nil {
		t.Fatalf("Failed to create file store: %v", err)
	}
	defer func() {
		_ = store.Close()
	}()

	//nolint:paralleltest // integration tests use shared resources
	t.Run("Invalid Deployment ID", func(t *testing.T) {
		deployment := &DeploymentState{
			ID:     "",
			Name:   "Invalid",
			Status: "pending",
		}

		err := store.Save(ctx, deployment)
		if !errors.Is(err, ErrInvalidDeploymentID) {
			t.Errorf("Expected ErrInvalidDeploymentID, got %v", err)
		}
	})

	//nolint:paralleltest // integration tests use shared resources
	t.Run("Load Non-existent", func(t *testing.T) {
		_, err := store.Load(ctx, "non-existent-id")
		if !errors.Is(err, ErrDeploymentNotFound) {
			t.Errorf("Expected ErrDeploymentNotFound, got %v", err)
		}
	})

	//nolint:paralleltest // integration tests use shared resources
	t.Run("Update Non-existent", func(t *testing.T) {
		deployment := &DeploymentState{
			ID:     "non-existent",
			Name:   "Test",
			Status: "pending",
		}

		err := store.Update(ctx, deployment)
		if !errors.Is(err, ErrDeploymentNotFound) {
			t.Errorf("Expected ErrDeploymentNotFound, got %v", err)
		}
	})

	//nolint:paralleltest // integration tests use shared resources
	t.Run("Delete Non-existent", func(t *testing.T) {
		err := store.Delete(ctx, "non-existent-id")
		if !errors.Is(err, ErrDeploymentNotFound) {
			t.Errorf("Expected ErrDeploymentNotFound, got %v", err)
		}
	})

	//nolint:paralleltest // integration tests use shared resources
	t.Run("Closed Store Operations", func(t *testing.T) {
		store.Close()

		deployment := &DeploymentState{
			ID:     "test",
			Name:   "Test",
			Status: "pending",
		}

		// All operations should return ErrStoreClosed
		if err := store.Save(ctx, deployment); !errors.Is(err, ErrStoreClosed) {
			t.Errorf("Save: expected ErrStoreClosed, got %v", err)
		}

		if _, err := store.Load(ctx, "test"); !errors.Is(err, ErrStoreClosed) {
			t.Errorf("Load: expected ErrStoreClosed, got %v", err)
		}

		if err := store.Update(ctx, deployment); !errors.Is(err, ErrStoreClosed) {
			t.Errorf("Update: expected ErrStoreClosed, got %v", err)
		}

		if err := store.Delete(ctx, "test"); !errors.Is(err, ErrStoreClosed) {
			t.Errorf("Delete: expected ErrStoreClosed, got %v", err)
		}

		if _, err := store.List(ctx, ""); !errors.Is(err, ErrStoreClosed) {
			t.Errorf("List: expected ErrStoreClosed, got %v", err)
		}

		if _, err := store.Lock(ctx, "test", "holder", time.Second); !errors.Is(err, ErrStoreClosed) {
			t.Errorf("Lock: expected ErrStoreClosed, got %v", err)
		}

		if err := store.Unlock(ctx, "test", "holder"); !errors.Is(err, ErrStoreClosed) {
			t.Errorf("Unlock: expected ErrStoreClosed, got %v", err)
		}
	})
}

//nolint:paralleltest // integration tests use shared resources
func TestFileStoreHomeDirectory(t *testing.T) {
	// Test ~ expansion
	store, err := NewFileStore("~/.lattiam/test-state")
	if err != nil {
		t.Fatalf("Failed to create file store with ~ path: %v", err)
	}
	defer func() {
		_ = store.Close()
	}()

	// Verify base directory was created
	home, _ := os.UserHomeDir()
	expectedPath := filepath.Join(home, ".lattiam/test-state/deployments")
	if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
		t.Errorf("Expected directory %s to exist", expectedPath)
	}

	// Clean up
	os.RemoveAll(filepath.Join(home, ".lattiam/test-state"))
}
