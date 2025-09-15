//go:build integration

package embedded

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/interfaces"
)

// TestEmbeddedSystem_EndToEnd verifies the complete flow of a deployment
// through the embedded system: Queue → WorkerPool → Executor → Tracker
func TestEmbeddedSystem_EndToEnd(t *testing.T) {
	ctx := context.Background()

	// Create real components - no mocks!
	tracker := NewTracker()
	queue := NewQueue(10) // buffer size of 10

	// Track if executor was called
	executorCalled := false
	testExecutor := func(ctx context.Context, deployment *interfaces.QueuedDeployment) error {
		executorCalled = true
		t.Logf("Executor called for deployment: %s", deployment.ID)

		// Simulate some work
		time.Sleep(50 * time.Millisecond)

		// In a real executor, we'd update the deployment state
		// For now, the worker pool handles status transitions
		return nil
	}

	// Create worker pool
	poolConfig := WorkerPoolConfig{
		Queue:      queue,
		Tracker:    tracker,
		Executor:   testExecutor,
		MinWorkers: 2,
		MaxWorkers: 2,
	}
	pool, err := NewWorkerPool(poolConfig)
	require.NoError(t, err)

	// Start the worker pool
	pool.Start()
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := pool.Stop(stopCtx)
		assert.NoError(t, err)
	})

	// Create a test deployment
	deployment := &interfaces.QueuedDeployment{
		ID:        "test-deployment-123",
		Status:    interfaces.DeploymentStatusQueued,
		CreatedAt: time.Now(),
		Request: &interfaces.DeploymentRequest{
			Resources: []interfaces.Resource{
				{Type: "test_resource", Name: "example"},
			},
		},
	}

	// Register and enqueue the deployment
	err = tracker.Register(deployment)
	require.NoError(t, err)

	err = queue.Enqueue(ctx, deployment)
	require.NoError(t, err)

	// First, verify initial status
	initialStatus, err := tracker.GetStatus(deployment.ID)
	require.NoError(t, err)
	t.Logf("Initial status after registration: %s", *initialStatus)

	// Poll for completion with timeout
	timeout := time.After(5 * time.Second)
	ticker := time.NewTicker(10 * time.Millisecond) // Poll more frequently
	defer ticker.Stop()

	var finalStatus *interfaces.DeploymentStatus
	statusTransitions := []interfaces.DeploymentStatus{*initialStatus}

	for {
		select {
		case <-timeout:
			t.Fatalf("Timeout waiting for deployment to complete. Status transitions seen: %v", statusTransitions)
		case <-ticker.C:
			status, err := tracker.GetStatus(deployment.ID)
			require.NoError(t, err)

			// Track status transitions
			if statusTransitions[len(statusTransitions)-1] != *status {
				statusTransitions = append(statusTransitions, *status)
				t.Logf("Status transition: %s (total transitions: %d)", *status, len(statusTransitions))
			}

			if *status == interfaces.DeploymentStatusCompleted ||
				*status == interfaces.DeploymentStatusFailed {
				finalStatus = status
				goto done
			}
		}
	}

done:
	// Verify the deployment completed successfully
	assert.NotNil(t, finalStatus)
	assert.Equal(t, interfaces.DeploymentStatusCompleted, *finalStatus)

	// Verify the executor was actually called
	assert.True(t, executorCalled, "Executor should have been called")

	// Verify status transitions
	assert.GreaterOrEqual(t, len(statusTransitions), 2, "Should have at least 2 status transitions")
	assert.Equal(t, interfaces.DeploymentStatusQueued, statusTransitions[0])
	assert.Contains(t, statusTransitions, interfaces.DeploymentStatusProcessing)
	assert.Equal(t, interfaces.DeploymentStatusCompleted, statusTransitions[len(statusTransitions)-1])

	// Verify the result was stored (with polling for async storage)
	var result *interfaces.DeploymentResult
	for i := 0; i < 10; i++ {
		result, err = tracker.GetResult(deployment.ID)
		require.NoError(t, err)
		if result != nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	assert.NotNil(t, result, "Result should be stored after deployment completes")
	if result != nil {
		assert.Equal(t, deployment.ID, result.DeploymentID)
	}
}

// TestEmbeddedSystem_ConcurrentDeployments verifies the system can handle
// multiple deployments concurrently
func TestEmbeddedSystem_ConcurrentDeployments(t *testing.T) {
	ctx := context.Background()

	// Create components
	tracker := NewTracker()
	queue := NewQueue(50)

	// Track executor calls
	executorCalls := make(chan string, 10)
	testExecutor := func(ctx context.Context, deployment *interfaces.QueuedDeployment) error {
		executorCalls <- deployment.ID
		time.Sleep(100 * time.Millisecond) // Simulate work
		return nil
	}

	// Create worker pool with multiple workers
	poolConfig := WorkerPoolConfig{
		Queue:      queue,
		Tracker:    tracker,
		Executor:   testExecutor,
		MinWorkers: 5,
		MaxWorkers: 5,
	}
	pool, err := NewWorkerPool(poolConfig)
	require.NoError(t, err)

	// Start the pool
	pool.Start()
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		pool.Stop(stopCtx)
	})

	// Create and enqueue multiple deployments
	numDeployments := 10
	deploymentIDs := make([]string, numDeployments)

	for i := 0; i < numDeployments; i++ {
		deployment := &interfaces.QueuedDeployment{
			ID:        fmt.Sprintf("deployment-%d", i),
			Status:    interfaces.DeploymentStatusQueued,
			CreatedAt: time.Now(),
			Request:   &interfaces.DeploymentRequest{},
		}
		deploymentIDs[i] = deployment.ID

		err = tracker.Register(deployment)
		require.NoError(t, err)

		err = queue.Enqueue(ctx, deployment)
		require.NoError(t, err)
	}

	// Collect executor calls
	executedDeployments := make(map[string]bool)
	timeout := time.After(10 * time.Second)

	for i := 0; i < numDeployments; i++ {
		select {
		case id := <-executorCalls:
			executedDeployments[id] = true
		case <-timeout:
			t.Fatal("Timeout waiting for all deployments to be executed")
		}
	}

	// Verify all deployments were executed
	assert.Equal(t, numDeployments, len(executedDeployments))
	for _, id := range deploymentIDs {
		assert.True(t, executedDeployments[id], "Deployment %s should have been executed", id)
	}

	// Wait for all deployments to complete
	t.Log("Waiting for all deployments to complete...")
	completionTimeout := time.After(15 * time.Second)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

waitForCompletion:
	for {
		select {
		case <-completionTimeout:
			t.Fatal("Timeout waiting for all deployments to complete")
		case <-ticker.C:
			allCompleted := true
			for _, id := range deploymentIDs {
				status, err := tracker.GetStatus(id)
				require.NoError(t, err)
				if *status != interfaces.DeploymentStatusCompleted &&
					*status != interfaces.DeploymentStatusFailed {
					allCompleted = false
					break
				}
			}
			if allCompleted {
				break waitForCompletion
			}
		}
	}

	// Now verify all deployments completed successfully
	for _, id := range deploymentIDs {
		status, err := tracker.GetStatus(id)
		require.NoError(t, err)
		assert.Equal(t, interfaces.DeploymentStatusCompleted, *status, "Deployment %s should be completed", id)
	}
}
