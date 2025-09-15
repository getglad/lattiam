//go:build integration
// +build integration

package distributed_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/infra/distributed"
	"github.com/lattiam/lattiam/internal/infra/distributed/testutil"
	"github.com/lattiam/lattiam/internal/interfaces"
)

func TestDistributedTracker_BasicOperations(t *testing.T) {
	t.Parallel()

	// Setup Redis container
	redisSetup := testutil.SetupRedis(t)

	// Parse Redis connection options using our helper
	redisOpt := testutil.ParseRedisOpt(t, redisSetup.URL)

	// Create tracker
	tracker, err := distributed.NewTracker(redisOpt)
	require.NoError(t, err)
	defer tracker.Close()

	t.Run("Register", func(t *testing.T) {
		deployment := testutil.CreateTestDeployment("tracker-test-1")

		// Register deployment
		err := tracker.Register(deployment)
		assert.NoError(t, err)

		// Get status
		status, err := tracker.GetStatus(deployment.ID)
		require.NoError(t, err)
		assert.NotNil(t, status)
		assert.Equal(t, interfaces.DeploymentStatusQueued, *status)

		// List should include it
		deployments, err := tracker.List(interfaces.DeploymentFilter{})
		require.NoError(t, err)
		assert.Len(t, deployments, 1)
		assert.Equal(t, deployment.ID, deployments[0].ID)
	})

	t.Run("StatusTransitions", func(t *testing.T) {
		deployment := testutil.CreateTestDeployment("tracker-test-status")

		// Register
		err := tracker.Register(deployment)
		require.NoError(t, err)

		// Update to processing
		err = tracker.SetStatus(deployment.ID, interfaces.DeploymentStatusProcessing)
		assert.NoError(t, err)

		status, err := tracker.GetStatus(deployment.ID)
		require.NoError(t, err)
		assert.Equal(t, interfaces.DeploymentStatusProcessing, *status)

		// Update to completed
		err = tracker.SetStatus(deployment.ID, interfaces.DeploymentStatusCompleted)
		assert.NoError(t, err)

		status, err = tracker.GetStatus(deployment.ID)
		require.NoError(t, err)
		assert.Equal(t, interfaces.DeploymentStatusCompleted, *status)
	})

	t.Run("GetByID_PreservesAllFields", func(t *testing.T) {
		deployment := testutil.CreateTestDeployment("tracker-getbyid-test")
		originalCreatedAt := deployment.CreatedAt

		// Register deployment
		err := tracker.Register(deployment)
		require.NoError(t, err)

		// Retrieve using GetByID
		retrieved, err := tracker.GetByID(deployment.ID)
		require.NoError(t, err)
		require.NotNil(t, retrieved)

		// Verify all fields are preserved
		assert.Equal(t, deployment.ID, retrieved.ID)
		assert.Equal(t, deployment.Status, retrieved.Status)
		assert.Equal(t, originalCreatedAt.Unix(), retrieved.CreatedAt.Unix())
		assert.NotZero(t, retrieved.CreatedAt, "CreatedAt should not be zero time")

		// Verify Request is preserved if it was set
		if deployment.Request != nil {
			assert.NotNil(t, retrieved.Request, "Request should be preserved")
			if deployment.Request.Metadata != nil {
				assert.NotNil(t, retrieved.Request.Metadata)
				assert.Equal(t, deployment.Request.Metadata, retrieved.Request.Metadata)
			}
		}
	})

	t.Run("Result", func(t *testing.T) {
		deployment := testutil.CreateTestDeployment("tracker-test-result")

		// Register
		err := tracker.Register(deployment)
		require.NoError(t, err)

		// No result initially
		result, err := tracker.GetResult(deployment.ID)
		assert.NoError(t, err)
		assert.Nil(t, result)

		// Set status to completed (normally done by worker)
		err = tracker.SetStatus(deployment.ID, interfaces.DeploymentStatusCompleted)
		require.NoError(t, err)

		// In the real system, the executor would store results
		// For now, verify we can retrieve nil without error
		result, err = tracker.GetResult(deployment.ID)
		assert.NoError(t, err)
	})

	t.Run("List", func(t *testing.T) {
		// Clear existing deployments
		deployments, _ := tracker.List(interfaces.DeploymentFilter{})
		for _, d := range deployments {
			tracker.Remove(d.ID)
		}

		// Register multiple deployments
		for i := 0; i < 5; i++ {
			d := testutil.CreateTestDeployment(fmt.Sprintf("list-test-%d", i))
			if i%2 == 0 {
				d.Status = interfaces.DeploymentStatusCompleted
			}
			err := tracker.Register(d)
			require.NoError(t, err)

			if i%2 == 0 {
				err = tracker.SetStatus(d.ID, interfaces.DeploymentStatusCompleted)
				require.NoError(t, err)
			}
		}

		// List all
		deployments, err := tracker.List(interfaces.DeploymentFilter{})
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(deployments), 5)

		// List by status
		deployments, err = tracker.List(interfaces.DeploymentFilter{
			Status: []interfaces.DeploymentStatus{interfaces.DeploymentStatusCompleted},
		})
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(deployments), 2)
	})

	t.Run("Remove", func(t *testing.T) {
		deployment := testutil.CreateTestDeployment("tracker-test-remove")

		// Register
		err := tracker.Register(deployment)
		require.NoError(t, err)

		// Verify exists
		status, err := tracker.GetStatus(deployment.ID)
		require.NoError(t, err)
		assert.NotNil(t, status)

		// Remove
		err = tracker.Remove(deployment.ID)
		assert.NoError(t, err)

		// Should not exist
		status, err = tracker.GetStatus(deployment.ID)
		assert.Error(t, err)
		assert.Nil(t, status)
	})

	t.Run("InvalidOperations", func(t *testing.T) {
		// Register nil
		err := tracker.Register(nil)
		assert.Error(t, err)

		// Register with empty ID
		deployment := testutil.CreateTestDeployment("")
		deployment.ID = ""
		err = tracker.Register(deployment)
		assert.Error(t, err)

		// Get status of non-existent
		status, err := tracker.GetStatus("non-existent")
		assert.Error(t, err)
		assert.Nil(t, status)

		// Set status of non-existent
		err = tracker.SetStatus("non-existent", interfaces.DeploymentStatusCompleted)
		assert.Error(t, err)
	})
}

func TestDistributedTracker_Expiration(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping expiration test in short mode")
	}

	redisSetup := testutil.SetupRedis(t)
	redisOpt := testutil.ParseRedisOpt(t, redisSetup.URL)

	tracker, err := distributed.NewTracker(redisOpt)
	require.NoError(t, err)
	defer tracker.Close()

	// Register deployment
	deployment := testutil.CreateTestDeployment("expiration-test")
	err = tracker.Register(deployment)
	require.NoError(t, err)

	// Verify it exists
	status, err := tracker.GetStatus(deployment.ID)
	require.NoError(t, err)
	assert.NotNil(t, status)

	// Note: In real tests, we would need to wait 7 days or modify Redis TTL
	// For now, we just verify the deployment was stored
	// The TTL is set internally by the tracker implementation
}
