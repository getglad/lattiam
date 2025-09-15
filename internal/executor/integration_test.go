package executor_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/events"
	"github.com/lattiam/lattiam/internal/executor"
	"github.com/lattiam/lattiam/internal/infra/embedded"
	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/internal/mocks"
)

//nolint:paralleltest // integration test with shared state
func TestExecutorIntegration_EndToEnd(t *testing.T) { //nolint:funlen // Test function with comprehensive test cases
	// Create real components
	tracker := embedded.NewTracker()
	queue := embedded.NewQueue(10)

	// Create mock provider manager and state store
	mockProviderManager := new(mocks.ProviderLifecycleManager)
	mockProvider := new(mocks.UnifiedProvider)
	mockStateStore := mocks.NewMockStateStore()

	// Setup provider expectations
	mockProviderManager.On("GetProvider", mock.Anything, "test-deployment-1", "test", "", mock.Anything, "test_resource").Return(mockProvider, nil)
	mockProviderManager.On("ReleaseProvider", "test-deployment-1", "test").Return(nil)
	mockProvider.On("CreateResource", mock.Anything, "test_resource", mock.Anything).Return(
		map[string]interface{}{
			"id":     "test-123",
			"status": "created",
		},
		nil,
	)

	// Create event bus and connect tracker
	eventBus := events.NewEventBus()
	events.ConnectTrackerToEventBus(eventBus, tracker)

	// Create executor
	deploymentExecutor := executor.NewWithEventBus(mockProviderManager, mockStateStore, eventBus, 5*time.Minute)
	executorFunc := executor.CreateExecutorFunc(deploymentExecutor)

	// Create worker pool
	poolConfig := embedded.WorkerPoolConfig{
		MinWorkers: 1,
		MaxWorkers: 2,
		Queue:      queue,
		Tracker:    tracker,
		Executor:   embedded.DeploymentExecutor(executorFunc),
	}

	pool, err := embedded.NewWorkerPool(poolConfig)
	require.NoError(t, err)

	// Start the pool
	pool.Start()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = pool.Stop(ctx)
	}()

	// Create a deployment
	deployment := &interfaces.QueuedDeployment{
		ID:        "test-deployment-1",
		Status:    interfaces.DeploymentStatusQueued,
		CreatedAt: time.Now(),
		Request: &interfaces.DeploymentRequest{
			Resources: []interfaces.Resource{
				{
					Type: "test_resource",
					Name: "my-resource",
					Properties: map[string]interface{}{
						"name": "test",
					},
				},
			},
		},
	}

	// Register and enqueue
	err = tracker.Register(deployment)
	require.NoError(t, err)

	err = queue.Enqueue(context.Background(), deployment)
	require.NoError(t, err)

	// Wait for processing
	assert.Eventually(t, func() bool {
		status, err := tracker.GetStatus(deployment.ID)
		if err != nil {
			return false
		}
		return status != nil && *status == interfaces.DeploymentStatusCompleted
	}, 2*time.Second, 50*time.Millisecond, "Deployment should complete")

	// Verify deployment was processed correctly
	status, err := tracker.GetStatus(deployment.ID)
	require.NoError(t, err)
	assert.Equal(t, interfaces.DeploymentStatusCompleted, *status)

	// Verify all mocks were called
	mockProviderManager.AssertExpectations(t)
	mockProvider.AssertExpectations(t)
}

//nolint:paralleltest // integration test with shared state
func TestExecutorIntegration_WithErrors(t *testing.T) { //nolint:funlen // Test function with comprehensive test cases
	// Create real components
	tracker := embedded.NewTracker()
	queue := embedded.NewQueue(10)

	// Create mock provider manager and state store
	mockProviderManager := new(mocks.ProviderLifecycleManager)
	mockStateStore := mocks.NewMockStateStore()

	// Setup provider to fail
	mockProviderManager.On("GetProvider", mock.Anything, "test-deployment-fail", "failing", "", mock.Anything, "failing_resource").Return(nil, assert.AnError)

	// Create event bus and connect tracker
	eventBus := events.NewEventBus()
	events.ConnectTrackerToEventBus(eventBus, tracker)

	// Create executor
	deploymentExecutor := executor.NewWithEventBus(mockProviderManager, mockStateStore, eventBus, 5*time.Minute)
	executorFunc := executor.CreateExecutorFunc(deploymentExecutor)

	// Create worker pool
	poolConfig := embedded.WorkerPoolConfig{
		MinWorkers: 1,
		MaxWorkers: 2,
		Queue:      queue,
		Tracker:    tracker,
		Executor:   embedded.DeploymentExecutor(executorFunc),
	}

	pool, err := embedded.NewWorkerPool(poolConfig)
	require.NoError(t, err)

	// Start the pool
	pool.Start()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = pool.Stop(ctx)
	}()

	// Create a deployment that will fail
	deployment := &interfaces.QueuedDeployment{
		ID:        "test-deployment-fail",
		Status:    interfaces.DeploymentStatusQueued,
		CreatedAt: time.Now(),
		Request: &interfaces.DeploymentRequest{
			Resources: []interfaces.Resource{
				{
					Type: "failing_resource",
					Name: "will-fail",
					Properties: map[string]interface{}{
						"name": "test",
					},
				},
			},
		},
	}

	// Register and enqueue
	err = tracker.Register(deployment)
	require.NoError(t, err)

	err = queue.Enqueue(context.Background(), deployment)
	require.NoError(t, err)

	// Wait for processing
	assert.Eventually(t, func() bool {
		status, err := tracker.GetStatus(deployment.ID)
		if err != nil {
			return false
		}
		return status != nil && *status == interfaces.DeploymentStatusFailed
	}, 2*time.Second, 50*time.Millisecond, "Deployment should fail")

	// Verify deployment failed
	status, err := tracker.GetStatus(deployment.ID)
	require.NoError(t, err)
	assert.Equal(t, interfaces.DeploymentStatusFailed, *status)

	// Verify error was recorded
	deployments, err := tracker.List(interfaces.DeploymentFilter{})
	require.NoError(t, err)
	require.Len(t, deployments, 1)

	// Find our deployment
	var failedDeployment *interfaces.QueuedDeployment
	for _, d := range deployments {
		if d.ID == "test-deployment-fail" {
			failedDeployment = d
			break
		}
	}
	require.NotNil(t, failedDeployment, "Should find the failed deployment")
	require.Error(t, failedDeployment.LastError)
	assert.Contains(t, failedDeployment.LastError.Error(), "failing_resource.will-fail failed")

	// Verify all mocks were called
	mockProviderManager.AssertExpectations(t)
}
