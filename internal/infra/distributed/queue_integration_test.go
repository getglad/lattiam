//go:build integration
// +build integration

package distributed_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/infra/distributed"
	"github.com/lattiam/lattiam/internal/infra/distributed/testutil"
	"github.com/lattiam/lattiam/internal/interfaces"
)

func TestDistributedQueue_BasicOperations(t *testing.T) {
	t.Parallel()

	// Setup Redis container
	redisSetup := testutil.SetupRedis(t)

	// Create queue
	queue, err := distributed.NewQueue(redisSetup.URL)
	require.NoError(t, err)
	defer queue.Close()

	ctx := context.Background()

	t.Run("Enqueue", func(t *testing.T) {
		deployment := testutil.CreateTestDeployment("test-enqueue-1")

		// Enqueue deployment
		err := queue.Enqueue(ctx, deployment)
		assert.NoError(t, err)

		// Parse Redis URL for inspector
		redisOpt, err := asynq.ParseRedisURI(redisSetup.URL)
		require.NoError(t, err)

		// Verify task exists in Redis using Asynq inspector
		inspector := asynq.NewInspector(redisOpt)
		defer inspector.Close()

		// Check pending tasks
		tasks, err := inspector.GetTaskInfo("deployments", deployment.ID)
		require.NoError(t, err)
		assert.NotNil(t, tasks)
		assert.Equal(t, deployment.ID, tasks.ID)
		assert.Equal(t, "deployments", tasks.Queue)
	})

	t.Run("EnqueueMultiple", func(t *testing.T) {
		deployments := make([]*interfaces.QueuedDeployment, 5)
		for i := 0; i < 5; i++ {
			deployments[i] = testutil.CreateTestDeployment(fmt.Sprintf("test-multi-%d", i))
		}

		// Enqueue all
		for _, d := range deployments {
			err := queue.Enqueue(ctx, d)
			require.NoError(t, err)
		}

		// Verify all exist
		redisOpt, err := asynq.ParseRedisURI(redisSetup.URL)
		require.NoError(t, err)
		inspector := asynq.NewInspector(redisOpt)
		defer inspector.Close()

		queues, err := inspector.Queues()
		require.NoError(t, err)
		assert.Contains(t, queues, "deployments")

		// Get queue info
		queueInfo, err := inspector.GetQueueInfo("deployments")
		require.NoError(t, err)
		assert.GreaterOrEqual(t, queueInfo.Size, 5)
	})

	t.Run("Cancel", func(t *testing.T) {
		deployment := testutil.CreateTestDeployment("test-cancel-1")

		// Enqueue
		err := queue.Enqueue(ctx, deployment)
		require.NoError(t, err)

		// Cancel
		err = queue.Cancel(ctx, deployment.ID)
		assert.NoError(t, err)

		// Verify task is deleted
		redisOpt, err := asynq.ParseRedisURI(redisSetup.URL)
		require.NoError(t, err)
		inspector := asynq.NewInspector(redisOpt)
		defer inspector.Close()

		_, err = inspector.GetTaskInfo("deployments", deployment.ID)
		assert.Error(t, err) // Should not exist
	})

	t.Run("CancelNonExistent", func(t *testing.T) {
		err := queue.Cancel(ctx, "non-existent-id")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found in any queue")
	})

	t.Run("InvalidOperations", func(t *testing.T) {
		// Nil deployment
		err := queue.Enqueue(ctx, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "deployment is nil")

		// Empty ID
		deployment := testutil.CreateTestDeployment("")
		deployment.ID = ""
		err = queue.Enqueue(ctx, deployment)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "deployment ID is empty")

		// Cancel empty ID
		err = queue.Cancel(ctx, "")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "deployment ID is empty")
	})
}

func TestDistributedQueue_LargePayload(t *testing.T) {
	t.Parallel()
	redisSetup := testutil.SetupRedis(t)

	queue, err := distributed.NewQueue(redisSetup.URL)
	require.NoError(t, err)
	defer queue.Close()

	// Create large deployment
	deployment := testutil.CreateLargeDeployment("large-payload-test")

	// Should handle large payloads
	ctx := context.Background()
	err = queue.Enqueue(ctx, deployment)
	assert.NoError(t, err)

	// Verify it was stored correctly
	redisOpt, err := asynq.ParseRedisURI(redisSetup.URL)
	require.NoError(t, err)

	inspector := asynq.NewInspector(redisOpt)
	defer inspector.Close()

	taskInfo, err := inspector.GetTaskInfo("deployments", deployment.ID)
	require.NoError(t, err)
	assert.NotNil(t, taskInfo)
	assert.Greater(t, len(taskInfo.Payload), 1000) // Should be a large payload
}
