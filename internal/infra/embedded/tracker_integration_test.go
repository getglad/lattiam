package embedded

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/interfaces"
)

// TestTracker_ExecutorResultPersistence tests that results stored by an executor
// are not overwritten by the worker pool
//
//nolint:paralleltest // integration test with shared components
func TestTracker_ExecutorResultPersistence(t *testing.T) { //nolint:funlen // Test function with comprehensive test cases
	ctx := context.Background()

	// Create components
	tracker := NewTracker()
	queue := NewQueue(10)

	// Simulate an executor that stores results with resources
	testExecutor := func(_ context.Context, deployment *interfaces.QueuedDeployment) error {
		// Simulate the deployment executor storing a result with resources
		result := &interfaces.DeploymentResult{
			DeploymentID: deployment.ID,
			Success:      true,
			Resources: map[string]interface{}{
				"aws_instance.example": map[string]interface{}{
					"id":   "i-1234567890abcdef0",
					"type": "t2.micro",
				},
				"aws_s3_bucket.data": map[string]interface{}{
					"id":     "my-data-bucket",
					"region": "us-east-1",
				},
			},
			Outputs: map[string]interface{}{
				"instance_ip": "10.0.1.100",
			},
			CompletedAt: time.Now(),
		}

		// Store the result (simulating what deployment executor does)
		if err := tracker.SetResult(deployment.ID, result); err != nil {
			return err
		}

		return nil
	}

	// Create worker pool
	poolConfig := WorkerPoolConfig{
		Queue:      queue,
		Tracker:    tracker,
		Executor:   testExecutor,
		MinWorkers: 1,
		MaxWorkers: 1,
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
		ID:        "test-deployment-with-resources",
		Status:    interfaces.DeploymentStatusQueued,
		CreatedAt: time.Now(),
		Request: &interfaces.DeploymentRequest{
			Resources: []interfaces.Resource{
				{Type: "aws_instance", Name: "example"},
				{Type: "aws_s3_bucket", Name: "data"},
			},
		},
	}

	// Register and enqueue the deployment
	err = tracker.Register(deployment)
	require.NoError(t, err)

	err = queue.Enqueue(ctx, deployment)
	require.NoError(t, err)

	// Wait for completion
	timeout := time.After(5 * time.Second)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			t.Fatal("Timeout waiting for deployment to complete")
		case <-ticker.C:
			status, err := tracker.GetStatus(deployment.ID)
			require.NoError(t, err)

			if *status == interfaces.DeploymentStatusCompleted {
				// Verify the result has the resources that were stored by the executor
				result, err := tracker.GetResult(deployment.ID)
				require.NoError(t, err)
				require.NotNil(t, result)

				// Check that resources were preserved
				assert.Len(t, result.Resources, 2, "Should have 2 resources")
				assert.Contains(t, result.Resources, "aws_instance.example")
				assert.Contains(t, result.Resources, "aws_s3_bucket.data")

				// Check specific resource details
				instance, ok := result.Resources["aws_instance.example"].(map[string]interface{})
				assert.True(t, ok, "aws_instance should be a map")
				assert.Equal(t, "i-1234567890abcdef0", instance["id"])
				assert.Equal(t, "t2.micro", instance["type"])

				bucket, ok := result.Resources["aws_s3_bucket.data"].(map[string]interface{})
				assert.True(t, ok, "aws_s3_bucket should be a map")
				assert.Equal(t, "my-data-bucket", bucket["id"])
				assert.Equal(t, "us-east-1", bucket["region"])

				// Check outputs
				assert.Len(t, result.Outputs, 1, "Should have 1 output")
				assert.Equal(t, "10.0.1.100", result.Outputs["instance_ip"])

				return
			}
		}
	}
}
