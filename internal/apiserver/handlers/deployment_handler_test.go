//go:build !integration
// +build !integration

package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/apiserver/handlers"
	"github.com/lattiam/lattiam/internal/apiserver/types"
	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/internal/mocks"
)

const (
	testDeploymentID = "dep-123456"
)

// TestCreateDeployment tests the CreateDeployment handler
func TestCreateDeployment(t *testing.T) { //nolint:funlen // Test function with comprehensive test cases
	t.Parallel()

	t.Run("SuccessfulCreation", func(t *testing.T) {
		t.Parallel()
		// Create mock deployment service
		deploymentService := new(mocks.DeploymentService)

		// Set up expectations
		expectedDeployment := &interfaces.QueuedDeployment{
			ID:        testDeploymentID,
			Status:    interfaces.DeploymentStatusQueued,
			CreatedAt: time.Now(),
			Request: &interfaces.DeploymentRequest{
				Metadata: map[string]interface{}{
					"name": "test-deployment",
				},
			},
		}
		deploymentService.On("CreateDeployment", mock.AnythingOfType("*interfaces.DeploymentRequest")).Return(expectedDeployment, nil)

		// Mock GetQueueMetrics call
		expectedMetrics := interfaces.QueueMetrics{
			CurrentDepth:    5,
			AverageWaitTime: 10 * time.Second,
		}
		deploymentService.On("GetQueueMetrics").Return(expectedMetrics)

		// Create handler
		handler, handlerErr := handlers.NewDeploymentHandler(deploymentService, types.NewRequestConverterWithDefaults())
		require.NoError(t, handlerErr)

		// Create request
		reqBody := map[string]interface{}{
			"name": "test-deployment",
			"terraform_json": map[string]interface{}{
				"resource": map[string]interface{}{
					"aws_instance": map[string]interface{}{
						"web": map[string]interface{}{
							"ami":           "ami-123456",
							"instance_type": "t2.micro",
						},
					},
				},
			},
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest("POST", "/api/v1/deployments", bytes.NewReader(body))
		rec := httptest.NewRecorder()

		// Execute
		handler.CreateDeployment(rec, req)

		// Verify
		assert.Equal(t, http.StatusCreated, rec.Code)

		var response map[string]interface{}
		err := json.NewDecoder(rec.Body).Decode(&response)
		require.NoError(t, err)
		assert.Equal(t, testDeploymentID, response["id"])
		assert.Equal(t, "queued", response["status"])

		// Verify queue_info is included
		assert.Contains(t, response, "queue_info")
		queueInfo, ok := response["queue_info"].(map[string]interface{})
		require.True(t, ok)
		assert.InEpsilon(t, float64(5), queueInfo["queue_depth"], 0.01)
		assert.InEpsilon(t, float64(10), queueInfo["average_wait_time"], 0.01)

		deploymentService.AssertExpectations(t)
	})

	t.Run("ValidationError", func(t *testing.T) {
		t.Parallel()
		// Create mock deployment service (no expectations as it shouldn't be called)
		deploymentService := new(mocks.DeploymentService)

		// Create handler
		handler, handlerErr := handlers.NewDeploymentHandler(deploymentService, types.NewRequestConverterWithDefaults())
		require.NoError(t, handlerErr)

		// Create request with missing name
		reqBody := map[string]interface{}{
			"terraform_json": map[string]interface{}{},
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest("POST", "/api/v1/deployments", bytes.NewReader(body))
		rec := httptest.NewRecorder()

		// Execute
		handler.CreateDeployment(rec, req)

		// Verify
		assert.Equal(t, http.StatusBadRequest, rec.Code)

		var response map[string]interface{}
		err := json.NewDecoder(rec.Body).Decode(&response)
		require.NoError(t, err)
		assert.Equal(t, "validation_failed", response["error"])

		// Service should not have been called
		deploymentService.AssertNotCalled(t, "CreateDeployment")
	})
}

// TestGetDeployment tests the GetDeployment handler
func TestGetDeployment(t *testing.T) { //nolint:funlen // Test function with comprehensive test cases
	t.Parallel()

	t.Run("ExistingDeployment", func(t *testing.T) {
		t.Parallel()
		// Create mock deployment service
		deploymentService := new(mocks.DeploymentService)

		// Set up expectations
		deploymentID := testDeploymentID
		deployment := &interfaces.QueuedDeployment{
			ID:        deploymentID,
			Status:    interfaces.DeploymentStatusCompleted,
			CreatedAt: time.Now(),
			Request: &interfaces.DeploymentRequest{
				Metadata: map[string]interface{}{
					"name": "test-deployment",
				},
			},
		}
		deploymentService.On("GetDeploymentByID", deploymentID).Return(deployment, nil)

		// Mock state for completed deployment
		state := map[string]interface{}{
			"resources": map[string]interface{}{
				"aws_instance.web": map[string]interface{}{
					"id": "i-1234567890abcdef0",
				},
			},
		}
		deploymentService.On("GetDeploymentState", deploymentID).Return(state, nil)

		// Create handler
		handler, handlerErr := handlers.NewDeploymentHandler(deploymentService, types.NewRequestConverterWithDefaults())
		require.NoError(t, handlerErr)

		// Create request
		req := httptest.NewRequest("GET", "/api/v1/deployments/"+deploymentID, nil)
		rec := httptest.NewRecorder()

		// Add URL parameters
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", deploymentID)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		// Execute
		handler.GetDeployment(rec, req)

		// Verify
		assert.Equal(t, http.StatusOK, rec.Code)

		var response map[string]interface{}
		err := json.NewDecoder(rec.Body).Decode(&response)
		require.NoError(t, err)
		assert.Equal(t, deploymentID, response["id"])
		assert.Equal(t, "completed", response["status"])
		assert.NotNil(t, response["resources"])

		deploymentService.AssertExpectations(t)
	})

	t.Run("DeploymentWithNilRequest", func(t *testing.T) {
		t.Parallel()
		// Create mock deployment service
		deploymentService := new(mocks.DeploymentService)

		// Set up expectations - deployment with nil Request field
		deploymentID := "test-nil-request"
		deployment := &interfaces.QueuedDeployment{
			ID:        deploymentID,
			Status:    interfaces.DeploymentStatusCompleted,
			CreatedAt: time.Now().Add(-1 * time.Hour),
			Request:   nil, // This is the critical case - nil Request
		}
		deploymentService.On("GetDeploymentByID", deploymentID).Return(deployment, nil)
		deploymentService.On("GetDeploymentState", deploymentID).Return(map[string]interface{}{
			"deployment_id": deploymentID,
			"resources":     []interface{}{},
		}, nil)

		// Create handler
		handler, handlerErr := handlers.NewDeploymentHandler(deploymentService, types.NewRequestConverterWithDefaults())
		require.NoError(t, handlerErr)

		// Create request
		req := httptest.NewRequest("GET", "/api/v1/deployments/"+deploymentID, nil)
		rec := httptest.NewRecorder()

		// Add URL parameters
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", deploymentID)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		// Execute - should not panic
		handler.GetDeployment(rec, req)

		// Verify
		assert.Equal(t, http.StatusOK, rec.Code)

		var response map[string]interface{}
		err := json.NewDecoder(rec.Body).Decode(&response)
		require.NoError(t, err)
		assert.Equal(t, deploymentID, response["id"])
		assert.Equal(t, "completed", response["status"])
		assert.NotContains(t, response, "name") // Should not have name since Request is nil

		deploymentService.AssertExpectations(t)
	})

	t.Run("NonExistentDeployment", func(t *testing.T) {
		t.Parallel()
		// Create mock deployment service
		deploymentService := new(mocks.DeploymentService)

		// Set up expectations
		deploymentID := "non-existent"
		deploymentService.On("GetDeploymentByID", deploymentID).Return(nil, errors.New("deployment non-existent not found"))

		// Create handler
		handler, handlerErr := handlers.NewDeploymentHandler(deploymentService, types.NewRequestConverterWithDefaults())
		require.NoError(t, handlerErr)

		// Create request
		req := httptest.NewRequest("GET", "/api/v1/deployments/"+deploymentID, nil)
		rec := httptest.NewRecorder()

		// Add URL parameters
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", deploymentID)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		// Execute
		handler.GetDeployment(rec, req)

		// Verify
		assert.Equal(t, http.StatusNotFound, rec.Code)

		var response map[string]interface{}
		err := json.NewDecoder(rec.Body).Decode(&response)
		require.NoError(t, err)
		assert.Equal(t, "not_found", response["error"])

		deploymentService.AssertExpectations(t)
	})
}

// TestListDeployments tests the ListDeployments handler
func TestListDeployments(t *testing.T) { // Test function with comprehensive test cases
	t.Parallel()

	t.Run("SuccessfulList", func(t *testing.T) {
		t.Parallel()
		// Create mock deployment service
		deploymentService := new(mocks.DeploymentService)

		// Set up expectations
		deployments := []*interfaces.QueuedDeployment{
			{
				ID:        "dep-1",
				Status:    interfaces.DeploymentStatusCompleted,
				CreatedAt: time.Now(),
				Request: &interfaces.DeploymentRequest{
					Metadata: map[string]interface{}{"name": "deployment-1"},
				},
			},
			{
				ID:        "dep-2",
				Status:    interfaces.DeploymentStatusQueued,
				CreatedAt: time.Now(),
				Request: &interfaces.DeploymentRequest{
					Metadata: map[string]interface{}{"name": "deployment-2"},
				},
			},
		}
		deploymentService.On("ListDeployments", mock.AnythingOfType("interfaces.DeploymentFilter")).Return(deployments, nil)

		// Mock state for completed deployment
		state1 := map[string]interface{}{
			"aws_instance.web": map[string]interface{}{
				"id":     "i-1234567890abcdef0",
				"status": "completed",
			},
		}
		deploymentService.On("GetDeploymentState", "dep-1").Return(state1, nil)

		// Create handler
		handler, handlerErr := handlers.NewDeploymentHandler(deploymentService, types.NewRequestConverterWithDefaults())
		require.NoError(t, handlerErr)

		// Create request
		req := httptest.NewRequest("GET", "/api/v1/deployments", nil)
		rec := httptest.NewRecorder()

		// Execute
		handler.ListDeployments(rec, req)

		// Verify
		assert.Equal(t, http.StatusOK, rec.Code)

		var response []map[string]interface{}
		err := json.NewDecoder(rec.Body).Decode(&response)
		require.NoError(t, err)
		assert.Len(t, response, 2)
		assert.Equal(t, "dep-1", response[0]["id"])
		assert.Equal(t, "dep-2", response[1]["id"])

		deploymentService.AssertExpectations(t)
	})
}

// TestDeleteDeployment tests the DeleteDeployment handler
func TestDeleteDeployment(t *testing.T) { // Test function with comprehensive test cases
	t.Parallel()

	t.Run("DeleteWithoutResources", func(t *testing.T) {
		t.Parallel()
		// Create mock deployment service
		deploymentService := new(mocks.DeploymentService)

		// Set up expectations
		deploymentID := testDeploymentID
		completedStatus := interfaces.DeploymentStatusCompleted
		deploymentService.On("GetDeploymentStatus", deploymentID).Return(&completedStatus, nil)
		deploymentService.On("DeploymentNeedsDestruction", deploymentID).Return(false, nil)
		deploymentService.On("DeleteDeployment", deploymentID).Return(nil)

		// Create handler
		handler, handlerErr := handlers.NewDeploymentHandler(deploymentService, types.NewRequestConverterWithDefaults())
		require.NoError(t, handlerErr)

		// Create request
		req := httptest.NewRequest("DELETE", "/api/v1/deployments/"+deploymentID, nil)
		rec := httptest.NewRecorder()

		// Add URL parameters
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", deploymentID)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		// Execute
		handler.DeleteDeployment(rec, req)

		// Verify
		assert.Equal(t, http.StatusNoContent, rec.Code)
		deploymentService.AssertExpectations(t)
	})

	t.Run("DeleteWithResources", func(t *testing.T) {
		t.Parallel()
		// Create mock deployment service
		deploymentService := new(mocks.DeploymentService)

		// Set up expectations
		deploymentID := testDeploymentID
		completedStatus := interfaces.DeploymentStatusCompleted
		deploymentService.On("GetDeploymentStatus", deploymentID).Return(&completedStatus, nil)
		deploymentService.On("DeploymentNeedsDestruction", deploymentID).Return(true, nil)
		deploymentService.On("DeleteDeployment", deploymentID).Return(nil)

		// Create handler
		handler, handlerErr := handlers.NewDeploymentHandler(deploymentService, types.NewRequestConverterWithDefaults())
		require.NoError(t, handlerErr)

		// Create request
		req := httptest.NewRequest("DELETE", "/api/v1/deployments/"+deploymentID, nil)
		rec := httptest.NewRecorder()

		// Add URL parameters
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("id", deploymentID)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

		// Execute
		handler.DeleteDeployment(rec, req)

		// Verify
		assert.Equal(t, http.StatusAccepted, rec.Code)

		var response map[string]interface{}
		err := json.NewDecoder(rec.Body).Decode(&response)
		require.NoError(t, err)
		assert.Contains(t, response["message"], "Infrastructure destruction initiated")
		assert.Equal(t, "destroying", response["status"])

		deploymentService.AssertExpectations(t)
	})
}
