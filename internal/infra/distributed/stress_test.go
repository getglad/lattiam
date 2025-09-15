//go:build integration
// +build integration

package distributed_test

import (
	"context"
	"fmt"
	"math/rand"
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

// TestConcurrentOperationsStress tests the system under high concurrent load
func TestConcurrentOperationsStress(t *testing.T) {
	t.Parallel()
	redisSetup := testutil.SetupRedis(t)
	redisOpt := testutil.ParseRedisOpt(t, redisSetup.URL)

	queue, err := distributed.NewQueue(redisSetup.URL)
	require.NoError(t, err)
	defer queue.Close()

	tracker, err := distributed.NewTracker(redisOpt)
	require.NoError(t, err)
	defer tracker.Close()

	t.Run("HighConcurrentEnqueue", func(t *testing.T) {
		const numGoroutines = 50
		const deploymentsPerGoroutine = 20
		totalExpected := numGoroutines * deploymentsPerGoroutine

		var successCount int32
		var errorCount int32
		var wg sync.WaitGroup

		// Track all deployment IDs
		deploymentIDs := make(chan string, totalExpected)

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(workerID int) {
				defer wg.Done()

				for j := 0; j < deploymentsPerGoroutine; j++ {
					deployment := testutil.CreateTestDeployment(
						fmt.Sprintf("concurrent-%d-%d", workerID, j),
					)

					// Register
					if err := tracker.Register(deployment); err != nil {
						atomic.AddInt32(&errorCount, 1)
						continue
					}

					// Enqueue
					if err := queue.Enqueue(context.Background(), deployment); err != nil {
						atomic.AddInt32(&errorCount, 1)
						continue
					}

					atomic.AddInt32(&successCount, 1)
					deploymentIDs <- deployment.ID
				}
			}(i)
		}

		// Wait for all enqueues to complete
		wg.Wait()
		close(deploymentIDs)

		// Verify counts
		assert.Equal(t, int32(totalExpected), atomic.LoadInt32(&successCount))
		assert.Equal(t, int32(0), atomic.LoadInt32(&errorCount))

		// Collect all IDs
		var allIDs []string
		for id := range deploymentIDs {
			allIDs = append(allIDs, id)
		}
		assert.Len(t, allIDs, totalExpected)

		// Start workers to process
		var processedCount int32
		testExecutor := func(ctx context.Context, d *interfaces.QueuedDeployment) error {
			atomic.AddInt32(&processedCount, 1)
			// Simulate variable processing time
			time.Sleep(time.Duration(rand.Intn(10)) * time.Millisecond)
			return nil
		}

		poolConfig := distributed.WorkerPoolConfig{
			RedisURL:    redisSetup.URL,
			Tracker:     tracker,
			Executor:    distributed.DeploymentExecutor(testExecutor),
			Concurrency: 20, // High concurrency
			QueueConfig: map[string]int{
				"deployments": 5,
			},
		}

		pool, err := distributed.NewWorkerPool(poolConfig)
		require.NoError(t, err)
		pool.Start()
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			pool.Stop(ctx)
		}()

		// Wait for processing to complete
		assert.Eventually(t, func() bool {
			return atomic.LoadInt32(&processedCount) == int32(totalExpected)
		}, 60*time.Second, 500*time.Millisecond)
	})

	t.Run("ConcurrentStatusUpdates", func(t *testing.T) {
		// Create a deployment
		deployment := testutil.CreateTestDeployment("status-stress-test")
		err := tracker.Register(deployment)
		require.NoError(t, err)

		const numGoroutines = 20
		const updatesPerGoroutine = 50
		var wg sync.WaitGroup
		var updateErrors int32

		statuses := []interfaces.DeploymentStatus{
			interfaces.DeploymentStatusQueued,
			interfaces.DeploymentStatusProcessing,
			interfaces.DeploymentStatusCompleted,
			interfaces.DeploymentStatusFailed,
		}

		// Concurrent status updates
		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()

				for j := 0; j < updatesPerGoroutine; j++ {
					// Pick random status
					status := statuses[rand.Intn(len(statuses))]

					if err := tracker.SetStatus(deployment.ID, status); err != nil {
						atomic.AddInt32(&updateErrors, 1)
					}

					// Small delay to increase contention
					time.Sleep(time.Microsecond * 100)
				}
			}()
		}

		// Concurrent status reads
		for i := 0; i < numGoroutines/2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()

				for j := 0; j < updatesPerGoroutine*2; j++ {
					_, err := tracker.GetStatus(deployment.ID)
					if err != nil {
						atomic.AddInt32(&updateErrors, 1)
					}
					time.Sleep(time.Microsecond * 50)
				}
			}()
		}

		wg.Wait()

		// Should have no errors with proper concurrency control
		assert.Equal(t, int32(0), atomic.LoadInt32(&updateErrors))

		// Final status should be valid
		finalStatus, err := tracker.GetStatus(deployment.ID)
		require.NoError(t, err)
		assert.NotNil(t, finalStatus)
		assert.Contains(t, statuses, *finalStatus)
	})

	t.Run("MixedOperationsUnderLoad", func(t *testing.T) {
		const duration = 10 * time.Second
		ctx, cancel := context.WithTimeout(context.Background(), duration)
		defer cancel()

		// Metrics
		var enqueueCount, processCount, cancelCount, listCount int32
		var errorCount int32

		// Start multiple workers
		var processedIDs sync.Map
		testExecutor := func(ctx context.Context, d *interfaces.QueuedDeployment) error {
			atomic.AddInt32(&processCount, 1)
			processedIDs.Store(d.ID, true)

			// Simulate work with variable duration
			workDuration := time.Duration(rand.Intn(50)) * time.Millisecond
			select {
			case <-time.After(workDuration):
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		// Start worker pools
		const numPools = 3
		pools := make([]*distributed.WorkerPool, numPools)
		for i := 0; i < numPools; i++ {
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
			pools[i] = pool
		}

		// Cleanup pools
		defer func() {
			for _, pool := range pools {
				stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
				pool.Stop(stopCtx)
				stopCancel()
			}
		}()

		var wg sync.WaitGroup

		// Producer goroutines
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func(producerID int) {
				defer wg.Done()

				for {
					select {
					case <-ctx.Done():
						return
					default:
						deployment := testutil.CreateTestDeployment(
							fmt.Sprintf("mixed-%d-%d", producerID, time.Now().UnixNano()),
						)

						if err := tracker.Register(deployment); err != nil {
							atomic.AddInt32(&errorCount, 1)
							continue
						}

						if err := queue.Enqueue(context.Background(), deployment); err != nil {
							atomic.AddInt32(&errorCount, 1)
							continue
						}

						atomic.AddInt32(&enqueueCount, 1)
						time.Sleep(time.Millisecond * 10)
					}
				}
			}(i)
		}

		// Random cancellation goroutine
		wg.Add(1)
		go func() {
			defer wg.Done()

			for {
				select {
				case <-ctx.Done():
					return
				case <-time.After(100 * time.Millisecond):
					// Try to cancel a random deployment
					deployments, err := tracker.List(interfaces.DeploymentFilter{
						Status: []interfaces.DeploymentStatus{interfaces.DeploymentStatusQueued},
					})
					if err != nil || len(deployments) == 0 {
						continue
					}

					// Pick random deployment to cancel
					d := deployments[rand.Intn(len(deployments))]
					if err := queue.Cancel(context.Background(), d.ID); err == nil {
						atomic.AddInt32(&cancelCount, 1)
					}
				}
			}
		}()

		// List operations goroutine
		wg.Add(1)
		go func() {
			defer wg.Done()

			for {
				select {
				case <-ctx.Done():
					return
				case <-time.After(50 * time.Millisecond):
					_, err := tracker.List(interfaces.DeploymentFilter{})
					if err == nil {
						atomic.AddInt32(&listCount, 1)
					}
				}
			}
		}()

		// Wait for duration
		wg.Wait()

		// Report metrics
		t.Logf("Stress test results after %v:", duration)
		t.Logf("  Enqueued: %d", atomic.LoadInt32(&enqueueCount))
		t.Logf("  Processed: %d", atomic.LoadInt32(&processCount))
		t.Logf("  Cancelled: %d", atomic.LoadInt32(&cancelCount))
		t.Logf("  List ops: %d", atomic.LoadInt32(&listCount))
		t.Logf("  Errors: %d", atomic.LoadInt32(&errorCount))

		// Verify system is still functional
		testDeployment := testutil.CreateTestDeployment("post-stress-test")
		err = tracker.Register(testDeployment)
		require.NoError(t, err)

		err = queue.Enqueue(context.Background(), testDeployment)
		require.NoError(t, err)

		// Should process normally
		assert.Eventually(t, func() bool {
			_, found := processedIDs.Load(testDeployment.ID)
			return found
		}, 5*time.Second, 100*time.Millisecond)
	})
}

// TestRaceConditions tests for race conditions in concurrent scenarios
func TestRaceConditions(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping race condition tests in short mode")
	}

	redisSetup := testutil.SetupRedis(t)
	redisOpt := testutil.ParseRedisOpt(t, redisSetup.URL)

	queue, err := distributed.NewQueue(redisSetup.URL)
	require.NoError(t, err)
	defer queue.Close()

	tracker, err := distributed.NewTracker(redisOpt)
	require.NoError(t, err)
	defer tracker.Close()

	t.Run("ConcurrentRegisterAndStatus", func(t *testing.T) {
		const numOperations = 100
		deployment := testutil.CreateTestDeployment("race-test-1")

		// First register
		err := tracker.Register(deployment)
		require.NoError(t, err)

		var wg sync.WaitGroup

		// Concurrent status updates
		for i := 0; i < numOperations; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()

				status := interfaces.DeploymentStatusQueued
				if i%2 == 0 {
					status = interfaces.DeploymentStatusProcessing
				}

				tracker.SetStatus(deployment.ID, status)
			}(i)
		}

		// Concurrent status reads
		for i := 0; i < numOperations; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				tracker.GetStatus(deployment.ID)
			}()
		}

		// Concurrent list operations
		for i := 0; i < numOperations/10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				tracker.List(interfaces.DeploymentFilter{})
			}()
		}

		wg.Wait()

		// System should still be consistent
		finalStatus, err := tracker.GetStatus(deployment.ID)
		require.NoError(t, err)
		assert.NotNil(t, finalStatus)
	})
}
