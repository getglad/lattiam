package embedded

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/interfaces"
)

func TestTracker_GetByID(t *testing.T) { //nolint:funlen // Test function with comprehensive test cases
	t.Parallel()
	t.Run("GetExistingDeployment", func(t *testing.T) {
		t.Parallel()
		tracker := NewTracker()

		// Register a deployment
		deployment := &interfaces.QueuedDeployment{
			ID:     "test-deployment",
			Status: interfaces.DeploymentStatusQueued,
			Request: &interfaces.DeploymentRequest{
				Resources: []interfaces.Resource{
					{Type: "aws_s3_bucket", Name: "test"},
				},
			},
		}

		err := tracker.Register(deployment)
		require.NoError(t, err)

		// Get by ID
		retrieved, err := tracker.GetByID("test-deployment")
		require.NoError(t, err)
		require.NotNil(t, retrieved)

		// Verify it's the same deployment
		assert.Equal(t, deployment.ID, retrieved.ID)
		assert.Equal(t, deployment.Status, retrieved.Status)
		assert.Len(t, retrieved.Request.Resources, len(deployment.Request.Resources))
	})

	t.Run("GetNonExistentDeployment", func(t *testing.T) {
		t.Parallel()
		tracker := NewTracker()

		// Try to get non-existent deployment
		retrieved, err := tracker.GetByID("non-existent")
		require.Error(t, err)
		assert.Nil(t, retrieved)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("GetWithEmptyID", func(t *testing.T) {
		t.Parallel()
		tracker := NewTracker()

		// Try with empty ID
		retrieved, err := tracker.GetByID("")
		require.Error(t, err)
		assert.Nil(t, retrieved)
		assert.Contains(t, err.Error(), "empty")
	})

	t.Run("ReturnsCopy", func(t *testing.T) {
		t.Parallel()
		tracker := NewTracker()

		// Register a deployment
		deployment := &interfaces.QueuedDeployment{
			ID:     "test-deployment",
			Status: interfaces.DeploymentStatusQueued,
		}

		err := tracker.Register(deployment)
		require.NoError(t, err)

		// Get by ID
		retrieved1, err := tracker.GetByID("test-deployment")
		require.NoError(t, err)

		// Modify the retrieved deployment
		retrieved1.Status = interfaces.DeploymentStatusCompleted

		// Get again and verify it wasn't modified
		retrieved2, err := tracker.GetByID("test-deployment")
		require.NoError(t, err)
		assert.Equal(t, interfaces.DeploymentStatusQueued, retrieved2.Status)
	})
}
