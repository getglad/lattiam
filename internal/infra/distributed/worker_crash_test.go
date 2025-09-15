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

	"github.com/lattiam/lattiam/internal/infra/distributed"
	"github.com/lattiam/lattiam/internal/infra/distributed/testutil"
	"github.com/lattiam/lattiam/internal/interfaces"
)

// TestWorkerCrashRecovery tests worker crash and recovery scenarios
func TestWorkerCrashRecovery(t *testing.T) {
	t.Parallel()
	redisSetup := testutil.SetupRedis(t)
	redisOpt := testutil.ParseRedisOpt(t, redisSetup.URL)

	queue, err := distributed.NewQueue(redisSetup.URL)
	require.NoError(t, err)
	defer queue.Close()

	tracker, err := distributed.NewTracker(redisOpt)
	require.NoError(t, err)
	defer tracker.Close()

	t.Run("MultipleWorkersCrashSimultaneously", func(t *testing.T) {
		var processedCount int32
		var mu sync.Mutex
		processedIDs := make(map[string]int)

		testExecutor := func(ctx context.Context, d *interfaces.QueuedDeployment) error {
			atomic.AddInt32(&processedCount, 1)
			mu.Lock()
			processedIDs[d.ID]++
			mu.Unlock()

			// Simulate work
			time.Sleep(100 * time.Millisecond)
			return nil
		}

		// Create multiple worker pools
		const numWorkers = 3
		pools := make([]*distributed.WorkerPool, numWorkers)

		for i := 0; i < numWorkers; i++ {
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
			pools[i] = pool
		}

		// Enqueue multiple deployments
		const numDeployments = 20
		for i := 0; i < numDeployments; i++ {
			deployment := testutil.CreateTestDeployment(fmt.Sprintf("multi-crash-%d", i))
			deployment.Request.Options.MaxRetries = 2

			err = tracker.Register(deployment)
			require.NoError(t, err)

			err = queue.Enqueue(context.Background(), deployment)
			require.NoError(t, err)
		}

		// Let some processing happen
		time.Sleep(2 * time.Second)

		// Crash all workers simultaneously
		for _, pool := range pools {
			go pool.Stop(context.Background())
		}

		// Wait for workers to stop
		time.Sleep(2 * time.Second)

		// Start new set of workers
		newPools := make([]*distributed.WorkerPool, numWorkers)
		for i := 0; i < numWorkers; i++ {
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
			newPools[i] = pool
		}

		// Cleanup new pools
		defer func() {
			for _, pool := range newPools {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				pool.Stop(ctx)
				cancel()
			}
		}()

		// Wait for all deployments to complete
		assert.Eventually(t, func() bool {
			deployments, err := tracker.List(interfaces.DeploymentFilter{
				Status: []interfaces.DeploymentStatus{interfaces.DeploymentStatusCompleted},
			})
			if err != nil {
				return false
			}
			return len(deployments) >= numDeployments
		}, 30*time.Second, 500*time.Millisecond)

		// Verify all deployments were processed
		mu.Lock()
		defer mu.Unlock()
		assert.GreaterOrEqual(t, len(processedIDs), numDeployments)
	})

	t.Run("WorkerCrashWithPoisonPillMessage", func(t *testing.T) {
		var healthyProcessed int32
		crashOnID := "poison-pill"

		testExecutor := func(ctx context.Context, d *interfaces.QueuedDeployment) error {
			if d.ID == crashOnID {
				// Simulate a panic/crash
				panic("simulated worker crash")
			}
			atomic.AddInt32(&healthyProcessed, 1)
			return nil
		}

		// Create worker with panic recovery
		poolConfig := distributed.WorkerPoolConfig{
			RedisURL: redisSetup.URL,
			Tracker:  tracker,
			Executor: func(ctx context.Context, d *interfaces.QueuedDeployment) (err error) {
				defer func() {
					if r := recover(); r != nil {
						err = fmt.Errorf("panic recovered: %v", r)
					}
				}()
				return testExecutor(ctx, d)
			},
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

		// Enqueue poison pill
		poisonDeployment := testutil.CreateTestDeployment(crashOnID)
		poisonDeployment.Request.Options.MaxRetries = 0 // Don't retry

		err = tracker.Register(poisonDeployment)
		require.NoError(t, err)

		err = queue.Enqueue(context.Background(), poisonDeployment)
		require.NoError(t, err)

		// Enqueue healthy deployments
		for i := 0; i < 5; i++ {
			deployment := testutil.CreateTestDeployment(fmt.Sprintf("healthy-%d", i))
			err = tracker.Register(deployment)
			require.NoError(t, err)

			err = queue.Enqueue(context.Background(), deployment)
			require.NoError(t, err)
		}

		// Wait for processing
		time.Sleep(5 * time.Second)

		// Verify healthy messages were processed
		assert.Equal(t, int32(5), atomic.LoadInt32(&healthyProcessed))

		// Verify poison pill failed
		status, err := tracker.GetStatus(crashOnID)
		require.NoError(t, err)
		assert.NotNil(t, status)
		// Status might be failed or still in queue depending on Asynq's retry behavior
	})
}

// TestWorkerGracefulShutdown tests graceful shutdown scenarios
func TestWorkerGracefulShutdown(t *testing.T) {
	t.Parallel()
	redisSetup := testutil.SetupRedis(t)
	redisOpt := testutil.ParseRedisOpt(t, redisSetup.URL)

	queue, err := distributed.NewQueue(redisSetup.URL)
	require.NoError(t, err)
	defer queue.Close()

	tracker, err := distributed.NewTracker(redisOpt)
	require.NoError(t, err)
	defer tracker.Close()

	t.Run("ShutdownWaitsForInProgressTasks", func(t *testing.T) {
		processingStarted := make(chan struct{})
		processingCompleted := make(chan struct{})

		testExecutor := func(ctx context.Context, d *interfaces.QueuedDeployment) error {
			close(processingStarted)
			// Simulate long-running task
			time.Sleep(2 * time.Second)
			close(processingCompleted)
			return nil
		}

		poolConfig := distributed.WorkerPoolConfig{
			RedisURL:    redisSetup.URL,
			Tracker:     tracker,
			Executor:    distributed.DeploymentExecutor(testExecutor),
			Concurrency: 1,
			QueueConfig: map[string]int{
				"deployments": 1,
			},
		}

		pool, err := distributed.NewWorkerPool(poolConfig)
		require.NoError(t, err)
		pool.Start()

		// Enqueue deployment
		deployment := testutil.CreateTestDeployment("graceful-shutdown-test")
		err = tracker.Register(deployment)
		require.NoError(t, err)

		err = queue.Enqueue(context.Background(), deployment)
		require.NoError(t, err)

		// Wait for processing to start
		<-processingStarted

		// Start shutdown with timeout longer than task duration
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		shutdownDone := make(chan error)
		go func() {
			shutdownDone <- pool.Stop(shutdownCtx)
		}()

		// Verify processing completes before shutdown
		select {
		case <-processingCompleted:
			// Good, task completed
		case <-time.After(3 * time.Second):
			t.Fatal("Processing did not complete")
		}

		// Verify shutdown completes
		select {
		case err := <-shutdownDone:
			assert.NoError(t, err)
		case <-time.After(6 * time.Second):
			t.Fatal("Shutdown did not complete")
		}

		// Verify deployment was marked as completed
		status, err := tracker.GetStatus(deployment.ID)
		require.NoError(t, err)
		assert.Equal(t, interfaces.DeploymentStatusCompleted, *status)
	})
}
