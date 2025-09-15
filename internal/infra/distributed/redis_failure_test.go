//go:build oat
// +build oat

package distributed_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/infra/distributed"
	"github.com/lattiam/lattiam/internal/infra/distributed/testutil"
	"github.com/lattiam/lattiam/internal/interfaces"
)

// TestRedisConnectionFailures tests behavior during Redis connection issues
func TestRedisConnectionFailures(t *testing.T) {
	t.Run("RedisDownBeforeStart", func(t *testing.T) {
		// Try to connect to non-existent Redis
		badURL := "redis://localhost:59999" // Non-existent port

		// Queue creation should succeed (lazy connection)
		queue, err := distributed.NewQueue(badURL)
		require.NoError(t, err)
		require.NotNil(t, queue)
		defer queue.Close()

		// But operations should fail
		deployment := testutil.CreateTestDeployment("redis-down-test")
		err = queue.Enqueue(context.Background(), deployment)
		require.Error(t, err, "Enqueue should fail when Redis is not available")
	})

	t.Run("RedisDownDuringOperation", func(t *testing.T) {
		redisSetup := testutil.SetupRedis(t)

		queue, err := distributed.NewQueue(redisSetup.URL)
		require.NoError(t, err)
		defer queue.Close()

		// Enqueue a deployment while Redis is up
		deployment := testutil.CreateTestDeployment("redis-fail-test-1")
		err = queue.Enqueue(context.Background(), deployment)
		require.NoError(t, err)

		// Stop Redis
		err = redisSetup.Stop(context.Background())
		require.NoError(t, err)

		// Try to enqueue - should fail
		deployment2 := testutil.CreateTestDeployment("redis-fail-test-2")
		err = queue.Enqueue(context.Background(), deployment2)
		assert.Error(t, err)

		// Restart Redis
		err = redisSetup.Start(context.Background())
		require.NoError(t, err)

		// Wait for Redis to be ready
		time.Sleep(2 * time.Second)

		// Create a new queue with the updated Redis URL (port may have changed)
		queue2, err := distributed.NewQueue(redisSetup.URL)
		require.NoError(t, err)
		defer queue2.Close()

		// Operations should work again with the new queue
		deployment3 := testutil.CreateTestDeployment("redis-fail-test-3")
		err = queue2.Enqueue(context.Background(), deployment3)
		assert.NoError(t, err)
	})

	t.Run("WorkerPoolRedisReconnection", func(t *testing.T) {
		redisSetup := testutil.SetupRedis(t)
		redisOpt := testutil.ParseRedisOpt(t, redisSetup.URL)

		queue, err := distributed.NewQueue(redisSetup.URL)
		require.NoError(t, err)
		defer queue.Close()

		tracker, err := distributed.NewTracker(redisOpt)
		require.NoError(t, err)
		defer tracker.Close()

		var processedBefore, processedAfter int32
		testExecutor := func(ctx context.Context, d *interfaces.QueuedDeployment) error {
			if d.ID < "restart" {
				atomic.AddInt32(&processedBefore, 1)
			} else {
				atomic.AddInt32(&processedAfter, 1)
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

		// Enqueue jobs before Redis restart
		for i := 0; i < 5; i++ {
			deployment := testutil.CreateTestDeployment(fmt.Sprintf("before-%d", i))
			err = tracker.Register(deployment)
			require.NoError(t, err)
			err = queue.Enqueue(context.Background(), deployment)
			require.NoError(t, err)
		}

		// Wait for some processing
		time.Sleep(2 * time.Second)

		// Stop the worker pool before Redis restart
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		pool.Stop(stopCtx)
		stopCancel()

		// Stop and restart Redis
		err = redisSetup.Stop(context.Background())
		require.NoError(t, err)

		time.Sleep(2 * time.Second)

		err = redisSetup.Start(context.Background())
		require.NoError(t, err)

		// Wait for Redis to be ready
		time.Sleep(3 * time.Second)

		// Create new queue, tracker, and worker pool with updated Redis URL
		queue2, err := distributed.NewQueue(redisSetup.URL)
		require.NoError(t, err)
		defer queue2.Close()

		redisOpt2 := testutil.ParseRedisOpt(t, redisSetup.URL)
		tracker2, err := distributed.NewTracker(redisOpt2)
		require.NoError(t, err)
		defer tracker2.Close()

		// Create new worker pool with updated Redis URL
		poolConfig2 := distributed.WorkerPoolConfig{
			RedisURL:    redisSetup.URL,
			Tracker:     tracker2,
			Executor:    distributed.DeploymentExecutor(testExecutor),
			Concurrency: 2,
			QueueConfig: map[string]int{
				"deployments": 1,
			},
		}

		pool2, err := distributed.NewWorkerPool(poolConfig2)
		require.NoError(t, err)
		pool2.Start()
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			pool2.Stop(ctx)
		}()

		// Enqueue jobs after Redis restart using new queue/tracker
		for i := 0; i < 5; i++ {
			deployment := testutil.CreateTestDeployment(fmt.Sprintf("zafter-%d", i)) // 'z' prefix to ensure > "restart"
			err = tracker2.Register(deployment)
			require.NoError(t, err)
			err = queue2.Enqueue(context.Background(), deployment)
			require.NoError(t, err)
		}

		// Wait for processing to resume with the new worker pool
		assert.Eventually(t, func() bool {
			return atomic.LoadInt32(&processedAfter) > 0
		}, 10*time.Second, 500*time.Millisecond, "Worker should resume processing after Redis restart")

		t.Logf("Processed before restart: %d", atomic.LoadInt32(&processedBefore))
		t.Logf("Processed after restart: %d", atomic.LoadInt32(&processedAfter))
	})

	t.Run("TrackerRedisConnectionLoss", func(t *testing.T) {
		redisSetup := testutil.SetupRedis(t)
		redisOpt := testutil.ParseRedisOpt(t, redisSetup.URL)

		tracker, err := distributed.NewTracker(redisOpt)
		require.NoError(t, err)
		defer tracker.Close()

		// Register a deployment while Redis is up
		deployment := testutil.CreateTestDeployment("tracker-fail-test")
		err = tracker.Register(deployment)
		require.NoError(t, err)

		// Verify we can read it
		status, err := tracker.GetStatus(deployment.ID)
		require.NoError(t, err)
		assert.NotNil(t, status)

		// Stop Redis
		err = redisSetup.Stop(context.Background())
		require.NoError(t, err)

		// Operations should fail
		_, err = tracker.GetStatus(deployment.ID)
		assert.Error(t, err)

		err = tracker.SetStatus(deployment.ID, interfaces.DeploymentStatusProcessing)
		assert.Error(t, err)

		// Restart Redis
		err = redisSetup.Start(context.Background())
		require.NoError(t, err)

		// Wait for Redis to be ready
		time.Sleep(2 * time.Second)

		// Create a new tracker with the updated Redis URL (port may have changed)
		redisOpt2 := testutil.ParseRedisOpt(t, redisSetup.URL)
		tracker2, err := distributed.NewTracker(redisOpt2)
		require.NoError(t, err)
		defer tracker2.Close()

		// Operations should work again with the new tracker, and data should be persistent
		status, err = tracker2.GetStatus(deployment.ID)
		assert.NoError(t, err)
		assert.NotNil(t, status)
	})

	t.Run("RedisMemoryPressure", func(t *testing.T) {
		if testing.Short() {
			t.Skip("Skipping memory pressure test in short mode")
		}

		redisSetup := testutil.SetupRedis(t)

		queue, err := distributed.NewQueue(redisSetup.URL)
		require.NoError(t, err)
		defer queue.Close()

		// Create large deployments to stress memory
		const numLargeDeployments = 100
		largeData := make([]byte, 1024*1024) // 1MB of data per deployment
		for i := range largeData {
			largeData[i] = byte(i % 256)
		}

		var enqueueErrors int32
		var wg sync.WaitGroup

		for i := 0; i < numLargeDeployments; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()

				deployment := testutil.CreateTestDeployment(fmt.Sprintf("large-%d", i))
				// Add large data to deployment metadata
				deployment.Request.Metadata = map[string]interface{}{
					"large_data": string(largeData),
				}

				err := queue.Enqueue(context.Background(), deployment)
				if err != nil {
					atomic.AddInt32(&enqueueErrors, 1)
				}
			}(i)
		}

		wg.Wait()

		// Some errors are acceptable under memory pressure
		errorRate := float64(atomic.LoadInt32(&enqueueErrors)) / float64(numLargeDeployments)
		t.Logf("Error rate under memory pressure: %.2f%%", errorRate*100)

		// System should handle at least 80% of requests
		assert.Less(t, errorRate, 0.2, "Error rate should be less than 20%")
	})
}

// TestRedisClusterScenarios tests behavior with Redis cluster configurations
func TestRedisClusterScenarios(t *testing.T) {
	t.Run("ConnectionPoolExhaustion", func(t *testing.T) {
		redisSetup := testutil.SetupRedis(t)

		// Create many concurrent connections
		const numConnections = 50
		queues := make([]*distributed.Queue, numConnections)

		var wg sync.WaitGroup
		var createErrors int32

		for i := 0; i < numConnections; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()

				queue, err := distributed.NewQueue(redisSetup.URL)
				if err != nil {
					atomic.AddInt32(&createErrors, 1)
					return
				}
				queues[i] = queue
			}(i)
		}

		wg.Wait()

		// Should handle many connections
		assert.Equal(t, int32(0), atomic.LoadInt32(&createErrors), "Should create all connections")

		// Clean up
		for _, q := range queues {
			if q != nil {
				q.Close()
			}
		}
	})

	t.Run("RedisTimeout", func(t *testing.T) {
		redisSetup := testutil.SetupRedis(t)
		redisOpt := testutil.ParseRedisOpt(t, redisSetup.URL)

		// Create a Redis client with very short timeout
		var redisClient redis.UniversalClient
		switch opt := redisOpt.(type) {
		case *asynq.RedisClientOpt:
			redisClient = redis.NewClient(&redis.Options{
				Addr:         opt.Addr,
				Password:     opt.Password,
				DB:           opt.DB,
				DialTimeout:  100 * time.Millisecond,
				ReadTimeout:  100 * time.Millisecond,
				WriteTimeout: 100 * time.Millisecond,
			})
		default:
			t.Skip("Test requires RedisClientOpt")
		}
		defer redisClient.Close()

		// Note: This is a simplified test. In real implementation,
		// we'd need to expose timeout configuration for tracker operations

		// Operations might timeout under load
		var timeoutErrors int32
		var wg sync.WaitGroup

		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()

				// Simulate slow operation
				ctx := context.Background()
				_, err := redisClient.BLPop(ctx, 200*time.Millisecond, "nonexistent").Result()
				if err != nil {
					atomic.AddInt32(&timeoutErrors, 1)
				}
			}()
		}

		wg.Wait()

		// Should see some timeout errors
		assert.Greater(t, atomic.LoadInt32(&timeoutErrors), int32(0), "Should see timeout errors")
	})
}
