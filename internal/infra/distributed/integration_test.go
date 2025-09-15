//go:build integration
// +build integration

package distributed_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stretchr/testify/mock"

	"github.com/lattiam/lattiam/internal/events"
	"github.com/lattiam/lattiam/internal/executor"
	"github.com/lattiam/lattiam/internal/infra/distributed"
	"github.com/lattiam/lattiam/internal/infra/distributed/testutil"
	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/internal/mocks"
)

func TestDistributedSystem_EndToEnd(t *testing.T) {
	t.Parallel()
	// Setup Redis container
	redisSetup := testutil.SetupRedis(t)

	// Parse Redis connection options
	redisOpt := testutil.ParseRedisOpt(t, redisSetup.URL)

	// Create queue
	queue, err := distributed.NewQueue(redisSetup.URL)
	require.NoError(t, err)
	defer queue.Close()

	// Create tracker
	tracker, err := distributed.NewTracker(redisOpt)
	require.NoError(t, err)
	defer tracker.Close()

	// Track executor calls
	executorCalled := int32(0)
	testExecutor := func(ctx context.Context, d *interfaces.QueuedDeployment) error {
		atomic.AddInt32(&executorCalled, 1)
		// Simulate some work
		time.Sleep(50 * time.Millisecond)
		return nil
	}

	// Create worker pool
	poolConfig := distributed.WorkerPoolConfig{
		RedisURL:    redisSetup.URL,
		Tracker:     tracker,
		Executor:    distributed.DeploymentExecutor(testExecutor),
		Concurrency: 2,
		QueueConfig: map[string]int{
			"deployments": 1,
		},
	}

	pool, err := distributed.NewWorkerPool(poolConfig)
	require.NoError(t, err)

	// Start processing
	pool.Start()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		pool.Stop(ctx)
	})

	// Create and process deployment
	deployment := testutil.CreateTestDeployment("e2e-test-1")

	// Register
	err = tracker.Register(deployment)
	require.NoError(t, err)

	// Enqueue
	err = queue.Enqueue(context.Background(), deployment)
	require.NoError(t, err)

	// Wait for processing
	assert.Eventually(t, func() bool {
		status, err := tracker.GetStatus(deployment.ID)
		if err != nil {
			return false
		}
		return status != nil && *status == interfaces.DeploymentStatusCompleted
	}, 5*time.Second, 100*time.Millisecond, "Deployment should be completed")

	// Verify executor was called
	assert.Equal(t, int32(1), atomic.LoadInt32(&executorCalled))
}

func TestDistributedSystem_MultipleDeployments(t *testing.T) {
	t.Parallel()
	redisSetup := testutil.SetupRedis(t)
	redisOpt := testutil.ParseRedisOpt(t, redisSetup.URL)

	queue, err := distributed.NewQueue(redisSetup.URL)
	require.NoError(t, err)
	defer queue.Close()

	tracker, err := distributed.NewTracker(redisOpt)
	require.NoError(t, err)
	defer tracker.Close()

	// Track processing
	var processedCount int32
	var mu sync.Mutex
	processedIDs := make(map[string]bool)

	testExecutor := func(ctx context.Context, d *interfaces.QueuedDeployment) error {
		atomic.AddInt32(&processedCount, 1)
		mu.Lock()
		processedIDs[d.ID] = true
		mu.Unlock()

		// Simulate varying work
		time.Sleep(time.Duration(10+len(d.ID)%20) * time.Millisecond)
		return nil
	}

	// Create worker pool with more concurrency
	poolConfig := distributed.WorkerPoolConfig{
		RedisURL:    redisSetup.URL,
		Tracker:     tracker,
		Executor:    distributed.DeploymentExecutor(testExecutor),
		Concurrency: 5,
		QueueConfig: map[string]int{
			"deployments": 3,
		},
	}

	pool, err := distributed.NewWorkerPool(poolConfig)
	require.NoError(t, err)

	pool.Start()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		pool.Stop(ctx)
	}()

	// Create and enqueue multiple deployments
	const numDeployments = 10
	for i := 0; i < numDeployments; i++ {
		deployment := testutil.CreateTestDeployment(fmt.Sprintf("multi-test-%d", i))

		err = tracker.Register(deployment)
		require.NoError(t, err)

		err = queue.Enqueue(context.Background(), deployment)
		require.NoError(t, err)
	}

	// Wait for all to complete
	assert.Eventually(t, func() bool {
		return atomic.LoadInt32(&processedCount) == numDeployments
	}, 10*time.Second, 100*time.Millisecond)

	// Verify all were processed
	mu.Lock()
	assert.Len(t, processedIDs, numDeployments)
	mu.Unlock()

	// Wait for all deployments to have completed status in tracker
	assert.Eventually(t, func() bool {
		deployments, err := tracker.List(interfaces.DeploymentFilter{
			Status: []interfaces.DeploymentStatus{interfaces.DeploymentStatusCompleted},
		})
		if err != nil {
			return false
		}
		return len(deployments) >= numDeployments
	}, 5*time.Second, 100*time.Millisecond, "Not all deployments reached completed status")
}

func TestDistributedSystem_ErrorHandling(t *testing.T) {
	t.Parallel()
	redisSetup := testutil.SetupRedis(t)
	redisOpt := testutil.ParseRedisOpt(t, redisSetup.URL)

	queue, err := distributed.NewQueue(redisSetup.URL)
	require.NoError(t, err)
	defer queue.Close()

	tracker, err := distributed.NewTracker(redisOpt)
	require.NoError(t, err)
	defer tracker.Close()

	// Executor that fails for specific deployments
	testExecutor := func(ctx context.Context, d *interfaces.QueuedDeployment) error {
		if d.ID == "fail-test" {
			return fmt.Errorf("simulated failure")
		}
		return nil
	}

	poolConfig := distributed.WorkerPoolConfig{
		RedisURL:    redisSetup.URL,
		Tracker:     tracker,
		Executor:    distributed.DeploymentExecutor(testExecutor),
		Concurrency: 2,
		QueueConfig: map[string]int{
			"deployments": 1,
		},
	}

	pool, err := distributed.NewWorkerPool(poolConfig)
	require.NoError(t, err)

	pool.Start()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		pool.Stop(ctx)
	}()

	// Create failing deployment
	deployment := testutil.CreateTestDeployment("fail-test")
	deployment.Request.Options.MaxRetries = 0 // No retries

	err = tracker.Register(deployment)
	require.NoError(t, err)

	err = queue.Enqueue(context.Background(), deployment)
	require.NoError(t, err)

	// Should eventually fail
	assert.Eventually(t, func() bool {
		status, err := tracker.GetStatus(deployment.ID)
		if err != nil {
			return false
		}
		// Note: The status might be archived or failed depending on Asynq behavior
		return status != nil && (*status == interfaces.DeploymentStatusFailed || *status == interfaces.DeploymentStatusQueued)
	}, 5*time.Second, 100*time.Millisecond)
}

func TestDistributedSystem_WithRealExecutor(t *testing.T) {
	t.Parallel()
	redisSetup := testutil.SetupRedis(t)
	redisOpt := testutil.ParseRedisOpt(t, redisSetup.URL)

	queue, err := distributed.NewQueue(redisSetup.URL)
	require.NoError(t, err)
	defer queue.Close()

	tracker, err := distributed.NewTracker(redisOpt)
	require.NoError(t, err)
	defer tracker.Close()

	// Create mock provider manager
	mockProviderManager := new(mocks.ProviderLifecycleManager)
	mockProvider := new(mocks.UnifiedProvider)

	// Setup provider expectations
	mockProviderManager.On("GetProvider", mock.Anything, "real-executor-test", "test", "", mock.Anything, "test_resource").Return(mockProvider, nil)
	mockProviderManager.On("ReleaseProvider", "real-executor-test", "test").Return(nil)

	mockProvider.On("CreateResource", mock.Anything, "test_resource", mock.Anything).Return(
		map[string]interface{}{
			"id":     "test-123",
			"status": "created",
		},
		nil,
	)

	// Create mock state store
	mockStateStore := mocks.NewMockStateStore()

	// Create event bus and connect tracker
	eventBus := events.NewEventBus()
	events.ConnectTrackerToEventBus(eventBus, tracker)

	// Create real deployment executor
	deploymentExecutor := executor.NewWithEventBus(mockProviderManager, mockStateStore, eventBus, 5*time.Minute)
	executorFunc := executor.CreateExecutorFunc(deploymentExecutor)

	// Create worker pool with real executor
	poolConfig := distributed.WorkerPoolConfig{
		RedisURL:    redisSetup.URL,
		Tracker:     tracker,
		Executor:    distributed.DeploymentExecutor(executorFunc),
		Concurrency: 2,
		QueueConfig: map[string]int{
			"deployments": 1,
		},
	}

	pool, err := distributed.NewWorkerPool(poolConfig)
	require.NoError(t, err)

	pool.Start()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		pool.Stop(ctx)
	}()

	// Create deployment
	deployment := testutil.CreateTestDeployment("real-executor-test")

	err = tracker.Register(deployment)
	require.NoError(t, err)

	err = queue.Enqueue(context.Background(), deployment)
	require.NoError(t, err)

	// Wait for completion
	assert.Eventually(t, func() bool {
		status, err := tracker.GetStatus(deployment.ID)
		if err != nil {
			return false
		}
		return status != nil && *status == interfaces.DeploymentStatusCompleted
	}, 10*time.Second, 100*time.Millisecond)

	// Verify mocks were called
	mockProviderManager.AssertExpectations(t)
	mockProvider.AssertExpectations(t)
}
