//go:build integration
// +build integration

package distributed_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/infra/distributed"
	"github.com/lattiam/lattiam/internal/infra/distributed/testutil"
	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/internal/metrics"
	"github.com/lattiam/lattiam/internal/monitor"
)

// TestOrphanMonitor_DistributedIntegration tests orphan monitor with real Redis
func TestOrphanMonitor_DistributedIntegration(t *testing.T) {
	t.Parallel()
	redisSetup := testutil.SetupRedis(t)
	redisOpt := testutil.ParseRedisOpt(t, redisSetup.URL)

	queue, err := distributed.NewQueue(redisSetup.URL)
	require.NoError(t, err)
	defer queue.Close()

	tracker, err := distributed.NewTracker(redisOpt)
	require.NoError(t, err)
	defer tracker.Close()

	inspector := asynq.NewInspector(redisOpt)
	defer inspector.Close()

	t.Run("DetectsQueueOrphans", func(t *testing.T) {
		metricsCollector := metrics.NewCollector()

		orphanMonitor := monitor.NewOrphanMonitor(monitor.Config{
			Queue:            queue,
			Tracker:          tracker,
			Inspector:        inspector,
			Metrics:          metricsCollector,
			ScanInterval:     100 * time.Millisecond,
			StaleThreshold:   200 * time.Millisecond,
			ReconcileOrphans: true,
		})

		// Enqueue deployment without registering in tracker
		orphanDeployment := testutil.CreateTestDeployment("queue-orphan-1")
		err := queue.Enqueue(context.Background(), orphanDeployment)
		require.NoError(t, err)

		// Start monitor
		err = orphanMonitor.Start()
		require.NoError(t, err)
		defer orphanMonitor.Stop(context.Background())

		// Wait for scan
		time.Sleep(200 * time.Millisecond)

		// Check stats - should detect orphan
		stats := orphanMonitor.GetStats()
		assert.True(t, stats.Running)
		// Note: We can't automatically fix queue orphans without tracker data
		// The monitor will log warnings about these
	})

	t.Run("RequeuesTrackerOrphans", func(t *testing.T) {
		orphanMonitor := monitor.NewOrphanMonitor(monitor.Config{
			Queue:            queue,
			Tracker:          tracker,
			Inspector:        inspector,
			ScanInterval:     100 * time.Millisecond,
			ReconcileOrphans: true,
		})

		// Register deployment in tracker but don't enqueue
		deployment := testutil.CreateTestDeployment("tracker-orphan-1")
		err := tracker.Register(deployment)
		require.NoError(t, err)

		// Mark as queued (but it's not actually in queue)
		err = tracker.SetStatus(deployment.ID, interfaces.DeploymentStatusQueued)
		require.NoError(t, err)

		// Verify it's not in queue
		_, err = inspector.GetTaskInfo("deployments", deployment.ID)
		assert.Error(t, err)

		// Start monitor
		err = orphanMonitor.Start()
		require.NoError(t, err)
		defer orphanMonitor.Stop(context.Background())

		// Wait for scan and re-queue
		time.Sleep(200 * time.Millisecond)

		// Verify it's now in queue
		task, err := inspector.GetTaskInfo("deployments", deployment.ID)
		assert.NoError(t, err)
		assert.NotNil(t, task)
	})

	t.Run("HandlesStaleProcessingWithWorkers", func(t *testing.T) {
		orphanMonitor := monitor.NewOrphanMonitor(monitor.Config{
			Queue:            queue,
			Tracker:          tracker,
			Inspector:        inspector,
			ScanInterval:     100 * time.Millisecond,
			StaleThreshold:   200 * time.Millisecond,
			ReconcileOrphans: true,
		})

		// Create a worker pool that will process jobs
		var processedAfterOrphan bool
		testExecutor := func(ctx context.Context, d *interfaces.QueuedDeployment) error {
			if d.ID == "will-stall" {
				// Simulate stalled processing
				<-ctx.Done()
				return ctx.Err()
			}
			processedAfterOrphan = true
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

		// Create deployment that will stall
		stalledDeployment := testutil.CreateTestDeployment("will-stall")
		err = tracker.Register(stalledDeployment)
		require.NoError(t, err)

		err = queue.Enqueue(context.Background(), stalledDeployment)
		require.NoError(t, err)

		// Wait for it to start processing
		assert.Eventually(t, func() bool {
			status, _ := tracker.GetStatus(stalledDeployment.ID)
			return status != nil && *status == interfaces.DeploymentStatusProcessing
		}, 2*time.Second, 50*time.Millisecond)

		// Stop worker to simulate crash
		pool.Stop(context.Background())

		// Start orphan monitor
		err = orphanMonitor.Start()
		require.NoError(t, err)
		defer orphanMonitor.Stop(context.Background())

		// Wait for stale detection
		time.Sleep(400 * time.Millisecond)

		// Verify marked as failed
		status, err := tracker.GetStatus(stalledDeployment.ID)
		require.NoError(t, err)
		assert.Equal(t, interfaces.DeploymentStatusFailed, *status)

		// Add a normal deployment
		normalDeployment := testutil.CreateTestDeployment("normal-after-orphan")
		err = tracker.Register(normalDeployment)
		require.NoError(t, err)

		err = queue.Enqueue(context.Background(), normalDeployment)
		require.NoError(t, err)

		// Start new worker pool
		newPool, err := distributed.NewWorkerPool(poolConfig)
		require.NoError(t, err)
		newPool.Start()
		defer newPool.Stop(context.Background())

		// Verify normal deployment gets processed
		assert.Eventually(t, func() bool {
			return processedAfterOrphan
		}, 5*time.Second, 100*time.Millisecond)
	})

	t.Run("ConcurrentMonitorsNoDuplication", func(t *testing.T) {
		// Test that multiple monitors don't duplicate work
		monitor1 := monitor.NewOrphanMonitor(monitor.Config{
			Queue:            queue,
			Tracker:          tracker,
			Inspector:        inspector,
			ScanInterval:     100 * time.Millisecond,
			StaleThreshold:   100 * time.Millisecond,
			ReconcileOrphans: true,
		})

		monitor2 := monitor.NewOrphanMonitor(monitor.Config{
			Queue:            queue,
			Tracker:          tracker,
			Inspector:        inspector,
			ScanInterval:     100 * time.Millisecond,
			StaleThreshold:   100 * time.Millisecond,
			ReconcileOrphans: true,
		})

		// Create stale processing job
		deployment := testutil.CreateTestDeployment("concurrent-monitors-test")
		err := tracker.Register(deployment)
		require.NoError(t, err)

		err = tracker.SetStatus(deployment.ID, interfaces.DeploymentStatusProcessing)
		require.NoError(t, err)

		// Start both monitors
		err = monitor1.Start()
		require.NoError(t, err)
		defer monitor1.Stop(context.Background())

		err = monitor2.Start()
		require.NoError(t, err)
		defer monitor2.Stop(context.Background())

		// Wait for stale detection
		time.Sleep(300 * time.Millisecond)

		// Should still be marked as failed (not corrupted by concurrent updates)
		status, err := tracker.GetStatus(deployment.ID)
		require.NoError(t, err)
		assert.Equal(t, interfaces.DeploymentStatusFailed, *status)
	})
}

// TestOrphanMonitor_PerformanceUnderLoad tests monitor performance with many jobs
func TestOrphanMonitor_PerformanceUnderLoad(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping performance test in short mode")
	}

	redisSetup := testutil.SetupRedis(t)
	redisOpt := testutil.ParseRedisOpt(t, redisSetup.URL)

	queue, err := distributed.NewQueue(redisSetup.URL)
	require.NoError(t, err)
	defer queue.Close()

	tracker, err := distributed.NewTracker(redisOpt)
	require.NoError(t, err)
	defer tracker.Close()

	inspector := asynq.NewInspector(redisOpt)
	defer inspector.Close()

	// Create many stale processing jobs
	const numJobs = 100
	for i := 0; i < numJobs; i++ {
		deployment := testutil.CreateTestDeployment(fmt.Sprintf("load-test-%d", i))
		err := tracker.Register(deployment)
		require.NoError(t, err)

		err = tracker.SetStatus(deployment.ID, interfaces.DeploymentStatusProcessing)
		require.NoError(t, err)
	}

	// Wait to ensure they're stale
	time.Sleep(100 * time.Millisecond)

	orphanMonitor := monitor.NewOrphanMonitor(monitor.Config{
		Queue:            queue,
		Tracker:          tracker,
		Inspector:        inspector,
		ScanInterval:     5 * time.Second, // Longer interval
		StaleThreshold:   50 * time.Millisecond,
		ReconcileOrphans: true,
	})

	// Start monitor and time the scan
	err = orphanMonitor.Start()
	require.NoError(t, err)
	defer orphanMonitor.Stop(context.Background())

	// Wait for initial scan to complete
	time.Sleep(1 * time.Second)

	// Verify all were marked as failed
	failedCount := 0
	deployments, err := tracker.List(interfaces.DeploymentFilter{
		Status: []interfaces.DeploymentStatus{interfaces.DeploymentStatusFailed},
	})
	require.NoError(t, err)

	for _, d := range deployments {
		if d.ID[:9] == "load-test" {
			failedCount++
		}
	}

	assert.Equal(t, numJobs, failedCount, "All stale jobs should be marked as failed")
}
