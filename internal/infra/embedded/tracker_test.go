package embedded

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/interfaces"
)

func TestTracker_Register(t *testing.T) {
	t.Parallel()

	t.Run("SuccessfulRegister", func(t *testing.T) {
		t.Parallel()
		tracker := NewTracker()
		deployment := &interfaces.QueuedDeployment{
			ID:        "dep-123",
			Status:    interfaces.DeploymentStatusQueued,
			CreatedAt: time.Now(),
		}

		err := tracker.Register(deployment)
		require.NoError(t, err)

		// Verify it was registered
		status, err := tracker.GetStatus(deployment.ID)
		require.NoError(t, err)
		assert.Equal(t, interfaces.DeploymentStatusQueued, *status)
	})

	t.Run("NilDeployment", func(t *testing.T) {
		t.Parallel()
		tracker := NewTracker()
		err := tracker.Register(nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "deployment is nil")
	})

	t.Run("EmptyID", func(t *testing.T) {
		t.Parallel()
		tracker := NewTracker()
		deployment := &interfaces.QueuedDeployment{
			Status: interfaces.DeploymentStatusQueued,
		}
		err := tracker.Register(deployment)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "deployment ID is empty")
	})

	t.Run("DuplicateID", func(t *testing.T) {
		t.Parallel()
		tracker := NewTracker()
		deployment := &interfaces.QueuedDeployment{
			ID:     "dep-123",
			Status: interfaces.DeploymentStatusQueued,
		}

		err := tracker.Register(deployment)
		require.NoError(t, err)

		// Try to register again
		err = tracker.Register(deployment)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already exists")
	})
}

func TestTracker_GetStatus(t *testing.T) {
	t.Parallel()

	t.Run("ExistingDeployment", func(t *testing.T) {
		t.Parallel()
		tracker := NewTracker()
		deployment := &interfaces.QueuedDeployment{
			ID:     "dep-123",
			Status: interfaces.DeploymentStatusProcessing,
		}
		require.NoError(t, tracker.Register(deployment))

		status, err := tracker.GetStatus(deployment.ID)
		require.NoError(t, err)
		assert.Equal(t, interfaces.DeploymentStatusProcessing, *status)
	})

	t.Run("NonExistentDeployment", func(t *testing.T) {
		t.Parallel()
		tracker := NewTracker()
		status, err := tracker.GetStatus("non-existent")
		require.Error(t, err)
		assert.Nil(t, status)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("EmptyID", func(t *testing.T) {
		t.Parallel()
		tracker := NewTracker()
		status, err := tracker.GetStatus("")
		require.Error(t, err)
		assert.Nil(t, status)
		assert.Contains(t, err.Error(), "deployment ID is empty")
	})
}

func TestTracker_SetStatus(t *testing.T) {
	t.Parallel()

	t.Run("UpdateStatus", func(t *testing.T) {
		t.Parallel()
		tracker := NewTracker()
		deployment := &interfaces.QueuedDeployment{
			ID:     "dep-123",
			Status: interfaces.DeploymentStatusQueued,
		}
		require.NoError(t, tracker.Register(deployment))

		// Update status
		err := tracker.SetStatus(deployment.ID, interfaces.DeploymentStatusProcessing)
		require.NoError(t, err)

		// Verify update
		status, err := tracker.GetStatus(deployment.ID)
		require.NoError(t, err)
		assert.Equal(t, interfaces.DeploymentStatusProcessing, *status)
	})

	t.Run("UpdateTimestamps", func(t *testing.T) {
		t.Parallel()
		tracker := NewTracker()
		deployment := &interfaces.QueuedDeployment{
			ID:     "dep-123",
			Status: interfaces.DeploymentStatusQueued,
		}
		require.NoError(t, tracker.Register(deployment))

		// Update to processing
		err := tracker.SetStatus(deployment.ID, interfaces.DeploymentStatusProcessing)
		require.NoError(t, err)

		// Get deployment and check timestamp
		deployments, err := tracker.List(interfaces.DeploymentFilter{})
		require.NoError(t, err)
		require.Len(t, deployments, 1)
		assert.NotNil(t, deployments[0].StartedAt)

		// Update to completed
		err = tracker.SetStatus(deployment.ID, interfaces.DeploymentStatusCompleted)
		require.NoError(t, err)

		// Check completed timestamp
		deployments, err = tracker.List(interfaces.DeploymentFilter{})
		require.NoError(t, err)
		require.Len(t, deployments, 1)
		assert.NotNil(t, deployments[0].CompletedAt)
	})
}

//nolint:funlen // Comprehensive test requiring multiple scenarios and validations
func TestTracker_List(t *testing.T) {
	t.Parallel()

	t.Run("ListAll", func(t *testing.T) {
		t.Parallel()
		tracker := NewTracker()

		// Register multiple deployments
		deps := []*interfaces.QueuedDeployment{
			{ID: "dep-1", Status: interfaces.DeploymentStatusQueued, CreatedAt: time.Now()},
			{ID: "dep-2", Status: interfaces.DeploymentStatusProcessing, CreatedAt: time.Now()},
			{ID: "dep-3", Status: interfaces.DeploymentStatusCompleted, CreatedAt: time.Now()},
		}

		for _, dep := range deps {
			require.NoError(t, tracker.Register(dep))
		}

		// List all
		results, err := tracker.List(interfaces.DeploymentFilter{})
		require.NoError(t, err)
		assert.Len(t, results, 3)
	})

	t.Run("FilterByStatus", func(t *testing.T) {
		t.Parallel()
		tracker := NewTracker()

		// Register deployments with different statuses
		deps := []*interfaces.QueuedDeployment{
			{ID: "dep-1", Status: interfaces.DeploymentStatusQueued, CreatedAt: time.Now()},
			{ID: "dep-2", Status: interfaces.DeploymentStatusProcessing, CreatedAt: time.Now()},
			{ID: "dep-3", Status: interfaces.DeploymentStatusQueued, CreatedAt: time.Now()},
		}

		for _, dep := range deps {
			require.NoError(t, tracker.Register(dep))
		}

		// Filter by queued status
		results, err := tracker.List(interfaces.DeploymentFilter{
			Status: []interfaces.DeploymentStatus{interfaces.DeploymentStatusQueued},
		})
		require.NoError(t, err)
		assert.Len(t, results, 2)
		for _, result := range results {
			assert.Equal(t, interfaces.DeploymentStatusQueued, result.Status)
		}
	})

	t.Run("FilterByTime", func(t *testing.T) {
		t.Parallel()
		tracker := NewTracker()
		now := time.Now()

		// Register deployments at different times
		deps := []*interfaces.QueuedDeployment{
			{ID: "dep-1", Status: interfaces.DeploymentStatusQueued, CreatedAt: now.Add(-2 * time.Hour)},
			{ID: "dep-2", Status: interfaces.DeploymentStatusQueued, CreatedAt: now.Add(-1 * time.Hour)},
			{ID: "dep-3", Status: interfaces.DeploymentStatusQueued, CreatedAt: now},
		}

		for _, dep := range deps {
			require.NoError(t, tracker.Register(dep))
		}

		// Filter by created after
		results, err := tracker.List(interfaces.DeploymentFilter{
			CreatedAfter: now.Add(-90 * time.Minute),
		})
		require.NoError(t, err)
		assert.Len(t, results, 2) // dep-2 and dep-3
	})
}

func TestTracker_Remove(t *testing.T) {
	t.Parallel()

	t.Run("SuccessfulRemove", func(t *testing.T) {
		t.Parallel()
		tracker := NewTracker()
		deployment := &interfaces.QueuedDeployment{
			ID:     "dep-123",
			Status: interfaces.DeploymentStatusQueued,
		}
		require.NoError(t, tracker.Register(deployment))

		// Remove
		err := tracker.Remove(deployment.ID)
		require.NoError(t, err)

		// Verify it's gone
		status, err := tracker.GetStatus(deployment.ID)
		require.Error(t, err)
		assert.Nil(t, status)
	})

	t.Run("NonExistentDeployment", func(t *testing.T) {
		t.Parallel()
		tracker := NewTracker()
		err := tracker.Remove("non-existent")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestTracker_ResultManagement(t *testing.T) {
	t.Parallel()

	t.Run("SetAndGetResult", func(t *testing.T) {
		t.Parallel()
		tracker := NewTracker()
		deployment := &interfaces.QueuedDeployment{
			ID:     "dep-123",
			Status: interfaces.DeploymentStatusQueued,
		}
		require.NoError(t, tracker.Register(deployment))

		// Set result
		result := &interfaces.DeploymentResult{
			DeploymentID: deployment.ID,
			Success:      true,
			Resources: map[string]interface{}{
				"instance": map[string]interface{}{"id": "i-123"},
			},
		}
		err := tracker.SetResult(deployment.ID, result)
		require.NoError(t, err)

		// Get result
		gotResult, err := tracker.GetResult(deployment.ID)
		require.NoError(t, err)
		assert.NotNil(t, gotResult)
		assert.Equal(t, result.DeploymentID, gotResult.DeploymentID)
		assert.Equal(t, result.Success, gotResult.Success)
		assert.Equal(t, result.Resources, gotResult.Resources)
	})

	t.Run("NoResultYet", func(t *testing.T) {
		t.Parallel()
		tracker := NewTracker()
		deployment := &interfaces.QueuedDeployment{
			ID:     "dep-123",
			Status: interfaces.DeploymentStatusQueued,
		}
		require.NoError(t, tracker.Register(deployment))

		// Get result before setting
		result, err := tracker.GetResult(deployment.ID)
		require.NoError(t, err)
		assert.Nil(t, result)
	})
}
