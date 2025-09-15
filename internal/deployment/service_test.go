package deployment

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/internal/mocks"
)

const (
	testDeploymentShortID = "dep-123"
)

// createTestService is a helper function to create a service for tests
func createTestService(queue interfaces.DeploymentQueue, tracker interfaces.DeploymentTracker) (interfaces.DeploymentService, error) {
	return NewServiceWithConfig(ServiceConfig{
		Queue:   queue,
		Tracker: tracker,
	})
}

func TestNewService(t *testing.T) {
	t.Parallel()
	t.Run("ReturnsErrorWithNilQueue", func(t *testing.T) {
		t.Parallel()
		tracker := new(mocks.DeploymentTracker)
		service, err := NewServiceWithConfig(ServiceConfig{
			Queue:   nil,
			Tracker: tracker,
		})
		require.Error(t, err)
		assert.Nil(t, service)
		assert.Equal(t, "deployment queue is required", err.Error())
	})

	t.Run("ReturnsErrorWithNilTracker", func(t *testing.T) {
		t.Parallel()
		queue := new(mocks.DeploymentQueue)
		service, err := NewServiceWithConfig(ServiceConfig{
			Queue:   queue,
			Tracker: nil,
		})
		require.Error(t, err)
		assert.Nil(t, service)
		assert.Equal(t, "deployment tracker is required", err.Error())
	})

	t.Run("CreatesServiceSuccessfully", func(t *testing.T) {
		t.Parallel()
		queue := new(mocks.DeploymentQueue)
		tracker := new(mocks.DeploymentTracker)
		service, err := NewServiceWithConfig(ServiceConfig{
			Queue:   queue,
			Tracker: tracker,
		})
		require.NoError(t, err)
		assert.NotNil(t, service)
	})
}

func TestRefactoredListDeployments(t *testing.T) {
	t.Parallel()
	t.Run("SuccessfulList", func(t *testing.T) {
		t.Parallel()
		// Setup
		queue := new(mocks.DeploymentQueue)
		tracker := new(mocks.DeploymentTracker)
		service, err := createTestService(queue, tracker)
		require.NoError(t, err)

		expectedDeployments := []*interfaces.QueuedDeployment{
			{ID: "dep-1", Status: interfaces.DeploymentStatusQueued},
			{ID: "dep-2", Status: interfaces.DeploymentStatusCompleted},
		}
		filter := interfaces.DeploymentFilter{}
		tracker.On("List", filter).Return(expectedDeployments, nil)

		// Execute
		deployments, err := service.ListDeployments(filter)

		// Verify
		require.NoError(t, err)
		assert.Equal(t, expectedDeployments, deployments)
		tracker.AssertExpectations(t)
	})

	t.Run("ErrorFromTracker", func(t *testing.T) {
		t.Parallel()
		// Setup
		queue := new(mocks.DeploymentQueue)
		tracker := new(mocks.DeploymentTracker)
		service, err := createTestService(queue, tracker)
		require.NoError(t, err)

		filter := interfaces.DeploymentFilter{}
		expectedError := errors.New("database error")
		tracker.On("List", filter).Return(nil, expectedError)

		// Execute
		deployments, err := service.ListDeployments(filter)

		// Verify
		require.Error(t, err)
		assert.Nil(t, deployments)
		require.ErrorIs(t, err, expectedError)
		tracker.AssertExpectations(t)
	})
}

func TestRefactoredCreateDeployment(t *testing.T) { //nolint:funlen // Test function with comprehensive test cases
	t.Parallel()

	t.Run("SuccessfulCreation", func(t *testing.T) {
		t.Parallel()
		// Setup
		queue := new(mocks.DeploymentQueue)
		tracker := new(mocks.DeploymentTracker)
		service, err := createTestService(queue, tracker)
		require.NoError(t, err)

		request := &interfaces.DeploymentRequest{
			Resources: []interfaces.Resource{
				{Type: "aws_instance", Name: "web"},
			},
			Metadata: map[string]interface{}{
				"name": "test-deployment",
			},
		}

		// Set up expectations
		tracker.On("Register", mock.MatchedBy(func(d *interfaces.QueuedDeployment) bool {
			return d.Request == request && d.Status == interfaces.DeploymentStatusQueued
		})).Return(nil)

		queue.On("Enqueue", mock.Anything, mock.MatchedBy(func(d *interfaces.QueuedDeployment) bool {
			return d.Request == request && d.Status == interfaces.DeploymentStatusQueued
		})).Return(nil)

		// Execute
		deployment, err := service.CreateDeployment(request)

		// Verify
		require.NoError(t, err)
		assert.NotNil(t, deployment)
		assert.Equal(t, request, deployment.Request)
		assert.Equal(t, interfaces.DeploymentStatusQueued, deployment.Status)
		assert.NotEmpty(t, deployment.ID)
		assert.NotZero(t, deployment.CreatedAt)
		tracker.AssertExpectations(t)
		queue.AssertExpectations(t)
	})

	t.Run("NilRequestError", func(t *testing.T) {
		t.Parallel()
		// Setup
		queue := new(mocks.DeploymentQueue)
		tracker := new(mocks.DeploymentTracker)
		service, err := createTestService(queue, tracker)
		require.NoError(t, err)

		// Execute
		deployment, err := service.CreateDeployment(nil)

		// Verify
		require.Error(t, err)
		assert.Nil(t, deployment)
		assert.Equal(t, "deployment request is required", err.Error())
	})

	t.Run("RegisterFailure", func(t *testing.T) {
		t.Parallel()
		// Setup
		queue := new(mocks.DeploymentQueue)
		tracker := new(mocks.DeploymentTracker)
		service, err := createTestService(queue, tracker)
		require.NoError(t, err)

		request := &interfaces.DeploymentRequest{}
		expectedError := errors.New("register failed")
		tracker.On("Register", mock.Anything).Return(expectedError)

		// Execute
		deployment, err := service.CreateDeployment(request)

		// Verify
		require.Error(t, err)
		assert.Nil(t, deployment)
		assert.Contains(t, err.Error(), "register_tracker failed")
		tracker.AssertExpectations(t)
	})

	t.Run("EnqueueFailureRollback", func(t *testing.T) {
		t.Parallel()
		// Setup
		queue := new(mocks.DeploymentQueue)
		tracker := new(mocks.DeploymentTracker)
		service, err := createTestService(queue, tracker)
		require.NoError(t, err)

		request := &interfaces.DeploymentRequest{}
		expectedError := errors.New("enqueue failed")

		// Set up expectations
		tracker.On("Register", mock.Anything).Return(nil)
		queue.On("Enqueue", mock.Anything, mock.Anything).Return(expectedError)
		tracker.On("SetStatus", mock.AnythingOfType("string"), interfaces.DeploymentStatusCanceled).Return(nil) // Rollback

		// Execute
		deployment, err := service.CreateDeployment(request)

		// Verify
		require.Error(t, err)
		assert.Nil(t, deployment)
		assert.Contains(t, err.Error(), "enqueue_deployment failed")
		tracker.AssertExpectations(t)
		queue.AssertExpectations(t)
	})
}

func TestRefactoredDeleteDeployment(t *testing.T) { //nolint:funlen // Test function with comprehensive test cases
	t.Parallel()

	t.Run("DeleteCompletedDeploymentWithoutResources", func(t *testing.T) {
		t.Parallel()
		// Setup
		queue := new(mocks.DeploymentQueue)
		tracker := new(mocks.DeploymentTracker)
		service, err := createTestService(queue, tracker)
		require.NoError(t, err)

		deploymentID := testDeploymentShortID
		completedStatus := interfaces.DeploymentStatusCompleted

		// Mock expectations
		tracker.On("GetStatus", deploymentID).Return(&completedStatus, nil)
		tracker.On("GetResult", deploymentID).Return(&interfaces.DeploymentResult{
			Resources: map[string]interface{}{}, // Empty resources
		}, nil)

		// Mock for GetDeploymentByID
		tracker.On("GetByID", deploymentID).Return(&interfaces.QueuedDeployment{
			ID:      deploymentID,
			Request: &interfaces.DeploymentRequest{Resources: []interfaces.Resource{}},
		}, nil)

		tracker.On("Remove", deploymentID).Return(nil)

		// Execute
		err = service.DeleteDeployment(deploymentID)

		// Verify
		require.NoError(t, err)
		tracker.AssertExpectations(t)
	})

	t.Run("DeleteQueuedDeployment", func(t *testing.T) {
		t.Parallel()
		// Setup
		queue := new(mocks.DeploymentQueue)
		tracker := new(mocks.DeploymentTracker)
		service, err := createTestService(queue, tracker)
		require.NoError(t, err)

		deploymentID := testDeploymentShortID
		queuedStatus := interfaces.DeploymentStatusQueued

		// Mock expectations
		tracker.On("GetStatus", deploymentID).Return(&queuedStatus, nil)
		queue.On("Cancel", mock.Anything, deploymentID).Return(nil)
		tracker.On("Remove", deploymentID).Return(nil)

		// Execute
		err = service.DeleteDeployment(deploymentID)

		// Verify
		require.NoError(t, err)
		tracker.AssertExpectations(t)
		queue.AssertExpectations(t)
	})

	t.Run("DeleteProcessingDeployment", func(t *testing.T) {
		t.Parallel()
		// Setup
		queue := new(mocks.DeploymentQueue)
		tracker := new(mocks.DeploymentTracker)
		service, err := createTestService(queue, tracker)
		require.NoError(t, err)

		deploymentID := testDeploymentShortID
		processingStatus := interfaces.DeploymentStatusProcessing

		// Mock expectations
		tracker.On("GetStatus", deploymentID).Return(&processingStatus, nil)
		tracker.On("SetStatus", deploymentID, interfaces.DeploymentStatusCanceling).Return(nil)

		// Execute
		err = service.DeleteDeployment(deploymentID)

		// Verify
		require.NoError(t, err)
		tracker.AssertExpectations(t)
	})

	t.Run("EmptyDeploymentIDError", func(t *testing.T) {
		t.Parallel()
		// Setup
		queue := new(mocks.DeploymentQueue)
		tracker := new(mocks.DeploymentTracker)
		service, err := createTestService(queue, tracker)
		require.NoError(t, err)

		// Execute
		err = service.DeleteDeployment("")

		// Verify
		require.Error(t, err)
		assert.Equal(t, "deployment ID is required", err.Error())
	})
}

func TestRefactoredCancelDeployment(t *testing.T) {
	t.Parallel()

	t.Run("SuccessfulCancel", func(t *testing.T) {
		t.Parallel()
		// Setup
		queue := new(mocks.DeploymentQueue)
		tracker := new(mocks.DeploymentTracker)
		service, err := createTestService(queue, tracker)
		require.NoError(t, err)

		deploymentID := testDeploymentShortID
		queue.On("Cancel", mock.Anything, deploymentID).Return(nil)

		// Execute
		err = service.CancelDeployment(deploymentID)

		// Verify
		require.NoError(t, err)
		queue.AssertExpectations(t)
	})

	t.Run("CancelError", func(t *testing.T) {
		t.Parallel()
		// Setup
		queue := new(mocks.DeploymentQueue)
		tracker := new(mocks.DeploymentTracker)
		service, err := createTestService(queue, tracker)
		require.NoError(t, err)

		deploymentID := testDeploymentShortID
		expectedError := errors.New("cancel failed")
		queue.On("Cancel", mock.Anything, deploymentID).Return(expectedError)

		// Execute
		err = service.CancelDeployment(deploymentID)

		// Verify
		require.Error(t, err)
		require.ErrorIs(t, err, expectedError)
		queue.AssertExpectations(t)
	})
}

func TestRefactoredGetDeploymentStatus(t *testing.T) {
	t.Parallel()

	t.Run("SuccessfulGetStatus", func(t *testing.T) {
		t.Parallel()
		// Setup
		queue := new(mocks.DeploymentQueue)
		tracker := new(mocks.DeploymentTracker)
		service, err := createTestService(queue, tracker)
		require.NoError(t, err)

		deploymentID := testDeploymentShortID
		expectedStatus := interfaces.DeploymentStatusProcessing
		tracker.On("GetStatus", deploymentID).Return(&expectedStatus, nil)

		// Execute
		status, err := service.GetDeploymentStatus(deploymentID)

		// Verify
		require.NoError(t, err)
		assert.NotNil(t, status)
		assert.Equal(t, expectedStatus, *status)
		tracker.AssertExpectations(t)
	})

	t.Run("EmptyDeploymentIDError", func(t *testing.T) {
		t.Parallel()
		// Setup
		queue := new(mocks.DeploymentQueue)
		tracker := new(mocks.DeploymentTracker)
		service, err := createTestService(queue, tracker)
		require.NoError(t, err)

		// Execute
		status, err := service.GetDeploymentStatus("")

		// Verify
		require.Error(t, err)
		assert.Nil(t, status)
		assert.Equal(t, "deployment ID is required", err.Error())
	})
}

func TestRefactoredGetDeploymentState(t *testing.T) {
	t.Parallel()

	t.Run("SuccessfulGetState", func(t *testing.T) {
		t.Parallel()
		// Setup
		queue := new(mocks.DeploymentQueue)
		tracker := new(mocks.DeploymentTracker)
		service, err := createTestService(queue, tracker)
		require.NoError(t, err)

		deploymentID := testDeploymentShortID
		expectedResources := map[string]interface{}{
			"instance": map[string]interface{}{
				"id": "i-123456",
			},
		}
		result := &interfaces.DeploymentResult{
			DeploymentID: deploymentID,
			Resources:    expectedResources,
		}
		tracker.On("GetResult", deploymentID).Return(result, nil)

		// Execute
		state, err := service.GetDeploymentState(deploymentID)

		// Verify
		require.NoError(t, err)
		assert.Equal(t, expectedResources, state)
		tracker.AssertExpectations(t)
	})

	t.Run("NilResult", func(t *testing.T) {
		t.Parallel()
		// Setup
		queue := new(mocks.DeploymentQueue)
		tracker := new(mocks.DeploymentTracker)
		service, err := createTestService(queue, tracker)
		require.NoError(t, err)

		deploymentID := testDeploymentShortID
		tracker.On("GetResult", deploymentID).Return(nil, nil)

		// Execute
		state, err := service.GetDeploymentState(deploymentID)

		// Verify
		require.NoError(t, err)
		assert.Nil(t, state)
		tracker.AssertExpectations(t)
	})
}
