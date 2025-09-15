//go:build oat
// +build oat

package distributed_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/infra/distributed"
	"github.com/lattiam/lattiam/internal/infra/distributed/testutil"
)

// TestDistributedQueue_ConnectionResilience tests queue behavior during Redis failures
// This is an OAT test because it manipulates container lifecycle
func TestDistributedQueue_ConnectionResilience(t *testing.T) {
	redisSetup := testutil.SetupRedis(t)

	queue, err := distributed.NewQueue(redisSetup.URL)
	require.NoError(t, err)
	defer queue.Close()

	// Enqueue a deployment
	deployment := testutil.CreateTestDeployment("resilience-test-1")
	err = queue.Enqueue(context.Background(), deployment)
	require.NoError(t, err)

	// Stop Redis container
	ctx := context.Background()
	err = redisSetup.Stop(ctx)
	require.NoError(t, err)

	// Try to enqueue - should fail
	deployment2 := testutil.CreateTestDeployment("resilience-test-2")
	err = queue.Enqueue(ctx, deployment2)
	assert.Error(t, err)

	// Restart Redis
	err = redisSetup.Start(ctx)
	require.NoError(t, err)

	// Create a new queue with the updated Redis URL (port may have changed)
	queue2, err := distributed.NewQueue(redisSetup.URL)
	require.NoError(t, err)
	defer queue2.Close()

	// Wait for Redis to be ready and verify operations work with new queue
	assert.Eventually(t, func() bool {
		deployment3 := testutil.CreateTestDeployment("resilience-test-3")
		err := queue2.Enqueue(ctx, deployment3)
		return err == nil
	}, 10*time.Second, 100*time.Millisecond)
}
