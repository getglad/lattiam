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
)

// TestOrphanedJobDetection tests scenarios where jobs exist in queue but not in tracker
func TestOrphanedJobDetection(t *testing.T) {
	t.Parallel()
	redisSetup := testutil.SetupRedis(t)
	redisOpt := testutil.ParseRedisOpt(t, redisSetup.URL)

	queue, err := distributed.NewQueue(redisSetup.URL)
	require.NoError(t, err)
	defer queue.Close()

	tracker, err := distributed.NewTracker(redisOpt)
	require.NoError(t, err)
	defer tracker.Close()

	t.Run("JobInQueueButNotInTracker", func(t *testing.T) {
		// Create deployment but only enqueue it (don't register in tracker)
		orphanDeployment := testutil.CreateTestDeployment("orphan-test-1")

		// Directly enqueue without registering
		err := queue.Enqueue(context.Background(), orphanDeployment)
		require.NoError(t, err)

		// Verify job is in queue
		inspector := asynq.NewInspector(redisOpt)
		defer inspector.Close()

		tasks, err := inspector.GetTaskInfo("deployments", orphanDeployment.ID)
		require.NoError(t, err)
		assert.NotNil(t, tasks)

		// Verify job is NOT in tracker
		status, err := tracker.GetStatus(orphanDeployment.ID)
		assert.Error(t, err)
		assert.Nil(t, status)

		// Create worker that handles orphaned jobs
		orphanHandled := make(chan string, 1)
		testExecutor := func(ctx context.Context, d *interfaces.QueuedDeployment) error {
			// Check if deployment exists in tracker
			status, err := tracker.GetStatus(d.ID)
			if err != nil || status == nil {
				// This is an orphaned job - register it
				err = tracker.Register(d)
				if err == nil {
					orphanHandled <- d.ID
				}
			}
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
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			pool.Stop(ctx)
		}()

		// Wait for orphan to be handled
		select {
		case id := <-orphanHandled:
			assert.Equal(t, orphanDeployment.ID, id)
		case <-time.After(5 * time.Second):
			t.Fatal("Orphaned job was not handled")
		}

		// Verify deployment now exists in tracker
		status, err = tracker.GetStatus(orphanDeployment.ID)
		require.NoError(t, err)
		assert.NotNil(t, status)
	})

	t.Run("TrackerEntryWithoutQueueJob", func(t *testing.T) {
		// Register deployment in tracker but don't enqueue
		lostDeployment := testutil.CreateTestDeployment("lost-job-1")

		err := tracker.Register(lostDeployment)
		require.NoError(t, err)

		// Set status to queued (but it's not actually in queue)
		err = tracker.SetStatus(lostDeployment.ID, interfaces.DeploymentStatusQueued)
		require.NoError(t, err)

		// Verify it's NOT in queue
		inspector := asynq.NewInspector(redisOpt)
		defer inspector.Close()

		_, err = inspector.GetTaskInfo("deployments", lostDeployment.ID)
		assert.Error(t, err) // Should not find task

		// Create a reconciliation process
		reconciled := make(chan string, 1)
		go func() {
			// Simulate periodic reconciliation
			deployments, err := tracker.List(interfaces.DeploymentFilter{
				Status: []interfaces.DeploymentStatus{interfaces.DeploymentStatusQueued},
			})
			if err != nil {
				return
			}

			for _, d := range deployments {
				// Check if actually in queue
				_, err := inspector.GetTaskInfo("deployments", d.ID)
				if err != nil {
					// Not in queue - mark as failed or re-enqueue
					tracker.SetStatus(d.ID, interfaces.DeploymentStatusFailed)
					reconciled <- d.ID
				}
			}
		}()

		// Wait for reconciliation
		select {
		case id := <-reconciled:
			assert.Equal(t, lostDeployment.ID, id)
		case <-time.After(3 * time.Second):
			t.Fatal("Lost job was not reconciled")
		}

		// Verify status was updated
		status, err := tracker.GetStatus(lostDeployment.ID)
		require.NoError(t, err)
		assert.Equal(t, interfaces.DeploymentStatusFailed, *status)
	})

	t.Run("StaleProcessingStatus", func(t *testing.T) {
		// Register deployment and set to processing, but no worker is actually processing it
		staleDeployment := testutil.CreateTestDeployment("stale-processing-1")

		err := tracker.Register(staleDeployment)
		require.NoError(t, err)

		// Manually set to processing with a past timestamp
		err = tracker.SetStatus(staleDeployment.ID, interfaces.DeploymentStatusProcessing)
		require.NoError(t, err)

		// Wait to make it "stale"
		time.Sleep(2 * time.Second)

		// Create stale detector
		staleDetected := make(chan string, 1)
		go func() {
			// Check for stale processing jobs
			deployments, err := tracker.List(interfaces.DeploymentFilter{
				Status: []interfaces.DeploymentStatus{interfaces.DeploymentStatusProcessing},
			})
			if err != nil {
				return
			}

			for _, d := range deployments {
				// Check if started more than 1 second ago (configurable threshold)
				if d.StartedAt != nil && time.Since(*d.StartedAt) > 1*time.Second {
					// This is stale - reset to queued
					tracker.SetStatus(d.ID, interfaces.DeploymentStatusQueued)
					queue.Enqueue(context.Background(), d)
					staleDetected <- d.ID
				}
			}
		}()

		// Wait for detection
		select {
		case id := <-staleDetected:
			assert.Equal(t, staleDeployment.ID, id)
		case <-time.After(3 * time.Second):
			t.Fatal("Stale job was not detected")
		}
	})

	t.Run("BatchOrphanDetection", func(t *testing.T) {
		// Create multiple orphaned jobs
		const numOrphans = 10
		orphanIDs := make([]string, numOrphans)

		for i := 0; i < numOrphans; i++ {
			deployment := testutil.CreateTestDeployment(fmt.Sprintf("batch-orphan-%d", i))
			orphanIDs[i] = deployment.ID

			// Only enqueue, don't register
			err := queue.Enqueue(context.Background(), deployment)
			require.NoError(t, err)
		}

		// Create batch orphan detector
		orphansFound := make(map[string]bool)
		detectComplete := make(chan struct{})

		go func() {
			defer close(detectComplete)

			inspector := asynq.NewInspector(redisOpt)
			defer inspector.Close()

			// Get all pending tasks
			tasks, err := inspector.ListPendingTasks("deployments")
			if err != nil {
				return
			}

			for _, task := range tasks {
				// Check if task exists in tracker
				_, err := tracker.GetStatus(task.ID)
				if err != nil {
					// This is an orphan
					orphansFound[task.ID] = true
				}
			}
		}()

		// Wait for detection to complete
		select {
		case <-detectComplete:
			// Good
		case <-time.After(5 * time.Second):
			t.Fatal("Orphan detection timed out")
		}

		// Verify all orphans were found
		assert.Len(t, orphansFound, numOrphans)
		for _, id := range orphanIDs {
			assert.True(t, orphansFound[id], "Orphan %s not detected", id)
		}
	})
}

// TestOrphanPrevention tests mechanisms to prevent orphaned jobs
func TestOrphanPrevention(t *testing.T) {
	t.Parallel()
	redisSetup := testutil.SetupRedis(t)
	redisOpt := testutil.ParseRedisOpt(t, redisSetup.URL)

	queue, err := distributed.NewQueue(redisSetup.URL)
	require.NoError(t, err)
	defer queue.Close()

	tracker, err := distributed.NewTracker(redisOpt)
	require.NoError(t, err)
	defer tracker.Close()

	t.Run("AtomicRegisterAndEnqueue", func(t *testing.T) {
		// Create a helper that atomically registers and enqueues
		atomicSubmit := func(deployment *interfaces.QueuedDeployment) error {
			// First register in tracker
			if err := tracker.Register(deployment); err != nil {
				return fmt.Errorf("failed to register: %w", err)
			}

			// Then enqueue
			if err := queue.Enqueue(context.Background(), deployment); err != nil {
				// Rollback registration on enqueue failure
				tracker.Remove(deployment.ID)
				return fmt.Errorf("failed to enqueue: %w", err)
			}

			return nil
		}

		// Test successful atomic submission
		deployment := testutil.CreateTestDeployment("atomic-test-1")
		err := atomicSubmit(deployment)
		require.NoError(t, err)

		// Verify both tracker and queue have the job
		status, err := tracker.GetStatus(deployment.ID)
		require.NoError(t, err)
		assert.NotNil(t, status)

		inspector := asynq.NewInspector(redisOpt)
		defer inspector.Close()

		task, err := inspector.GetTaskInfo("deployments", deployment.ID)
		require.NoError(t, err)
		assert.NotNil(t, task)
	})

	t.Run("TransactionalCleanup", func(t *testing.T) {
		// Test cleanup of both tracker and queue entries
		deployment := testutil.CreateTestDeployment("cleanup-test-1")

		// Register and enqueue
		err := tracker.Register(deployment)
		require.NoError(t, err)

		err = queue.Enqueue(context.Background(), deployment)
		require.NoError(t, err)

		// Create transactional cleanup
		cleanupDeployment := func(deploymentID string) error {
			// Cancel from queue first
			if err := queue.Cancel(context.Background(), deploymentID); err != nil {
				// Log but don't fail - job might already be processed
				t.Logf("Failed to cancel from queue: %v", err)
			}

			// Then remove from tracker
			if err := tracker.Remove(deploymentID); err != nil {
				return fmt.Errorf("failed to remove from tracker: %w", err)
			}

			return nil
		}

		// Perform cleanup
		err = cleanupDeployment(deployment.ID)
		require.NoError(t, err)

		// Verify both are removed
		status, err := tracker.GetStatus(deployment.ID)
		assert.Error(t, err)
		assert.Nil(t, status)

		inspector := asynq.NewInspector(redisOpt)
		defer inspector.Close()

		_, err = inspector.GetTaskInfo("deployments", deployment.ID)
		assert.Error(t, err)
	})
}
