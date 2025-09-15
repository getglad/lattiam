//go:build !integration
// +build !integration

package apiserver_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/apiserver"
	"github.com/lattiam/lattiam/internal/apiserver/handlers"
	"github.com/lattiam/lattiam/internal/config"
	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/internal/mocks"
	"github.com/lattiam/lattiam/tests/helpers"
)

// Test constants
const (
	testPort1   = 8085
	testPort2   = 8086
	testPort3   = 8087
	testTimeout = 30 * time.Second
)

func TestAPIServerWithComponents(t *testing.T) { //nolint:funlen // Test function with comprehensive test cases
	t.Parallel()
	// Create mock components
	queue := mocks.NewDeploymentQueue(t)
	tracker := mocks.NewDeploymentTracker(t)
	workerPool := mocks.NewWorkerPool(t)
	stateStore := mocks.NewMockStateStore()

	// Set up expectations for mocks
	tracker.On("List", mock.Anything).Return([]*interfaces.QueuedDeployment{
		{
			ID:     "deployment-123",
			Status: interfaces.DeploymentStatusProcessing,
			Request: &interfaces.DeploymentRequest{
				Metadata: map[string]interface{}{
					"name": "test-deployment",
				},
			},
		},
	}, nil)

	// Mock Register for new deployments
	tracker.On("Register", mock.Anything).Return(nil)

	// Mock GetByID for deployment fetching
	tracker.On("GetByID", "deployment-123").Return(&interfaces.QueuedDeployment{
		ID:     "deployment-123",
		Status: interfaces.DeploymentStatusProcessing,
		Request: &interfaces.DeploymentRequest{
			Metadata: map[string]interface{}{
				"name": "test-deployment",
			},
		},
	}, nil)

	// Mock GetResult for deployment state
	tracker.On("GetResult", "deployment-123").Return(&interfaces.DeploymentResult{
		Resources: map[string]interface{}{},
	}, nil)

	// Mock queue operations
	queue.On("Cancel", mock.Anything, mock.Anything).Return(nil)
	queue.On("Enqueue", mock.Anything, mock.Anything).Return(nil)
	queue.On("GetMetrics").Return(interfaces.QueueMetrics{
		TotalEnqueued:    10,
		TotalDequeued:    5,
		CurrentDepth:     5,
		AverageWaitTime:  30 * time.Second,
		OldestDeployment: time.Now().Add(-5 * time.Minute),
	})

	// Mock worker pool operations
	workerPool.On("Stop", mock.Anything).Return(nil)

	// Create API server config
	cfg := config.NewServerConfig()
	cfg.Port = testPort1

	// Create API server with components
	server, err := apiserver.NewAPIServerWithComponents(cfg, queue, tracker, workerPool, stateStore, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, server)

	// Cleanup
	ctx := context.Background()
	t.Cleanup(func() {
		_ = server.Shutdown(ctx)
	})

	t.Run("CreateDeploymentWithWorkerSystem", func(t *testing.T) {
		t.Parallel()
		// Load test fixture
		fixtureData := helpers.LoadFixture(t, helpers.Fixtures.API.IAMRoleSimple)

		var requestBody apiserver.DeploymentRequest
		err := json.Unmarshal(fixtureData, &requestBody)
		require.NoError(t, err)

		// Create request
		body, err := json.Marshal(requestBody)
		require.NoError(t, err)

		req := httptest.NewRequest("POST", "/api/v1/deployments", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		// Handle request
		server.Router().ServeHTTP(w, req)

		// Check response
		assert.Equal(t, http.StatusCreated, w.Code)

		var response map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		// Verify response contains expected fields
		assert.Contains(t, response, "id")
		assert.Contains(t, response, "status")
		assert.Equal(t, "queued", response["status"])
		assert.Contains(t, response, "created_at")
		assert.Contains(t, response, "queue_info")

		// Verify queue info
		queueInfo, ok := response["queue_info"].(map[string]interface{})
		require.True(t, ok)
		assert.Contains(t, queueInfo, "queue_depth")
		assert.Contains(t, queueInfo, "average_wait_time")

		// Verify the deployment was submitted to the queue
		// With the new architecture, we would verify through the queue mock
		// but for now we just verify the response was successful
	})

	t.Run("GetDeploymentFromWorkerSystem", func(t *testing.T) {
		t.Parallel()
		// Set up a deployment ID to test
		deploymentID := "deployment-123"

		req := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/deployments/%s", deploymentID), nil)
		w := httptest.NewRecorder()

		// Handle request
		server.Router().ServeHTTP(w, req)

		// Check response
		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, deploymentID, response["id"])
		assert.Equal(t, "processing", response["status"])
	})

	t.Run("CancelDeployment", func(t *testing.T) {
		t.Parallel()
		// Set up a deployment ID to test cancellation
		deploymentID := "deployment-456"

		req := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/deployments/%s/cancel", deploymentID), nil)
		w := httptest.NewRecorder()

		// Handle request
		server.Router().ServeHTTP(w, req)

		// Check response
		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Equal(t, deploymentID, response["id"])
		assert.Equal(t, "canceled", response["status"])

		// Verify the cancel was processed
		// With the new architecture, we would verify through the queue mock
		// but for now we just verify the response was successful
	})

	t.Run("GetQueueMetrics", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest("GET", "/api/v1/queue/metrics", nil)
		w := httptest.NewRecorder()

		// Handle request
		server.Router().ServeHTTP(w, req)

		// Check response
		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		// Check queue metrics are returned
		assert.Contains(t, response, "total_enqueued")
		assert.Contains(t, response, "total_dequeued")
		assert.Contains(t, response, "current_depth")
		assert.Contains(t, response, "average_wait_time")
		assert.Contains(t, response, "oldest_deployment")
	})

	t.Run("GetSystemHealth", func(t *testing.T) {
		t.Parallel()
		req := httptest.NewRequest("GET", "/api/v1/system/health", nil)
		w := httptest.NewRecorder()

		// Handle request
		server.Router().ServeHTTP(w, req)

		// Check response
		assert.Equal(t, http.StatusOK, w.Code)

		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)

		assert.Contains(t, response, "status")
		assert.Contains(t, response, "components")
		assert.Contains(t, response, "time")

		// Verify component health - new format uses different names
		components, ok := response["components"].(map[string]interface{})
		require.True(t, ok)
		assert.Contains(t, components, "stateStore")
		assert.Contains(t, components, "queue")
		assert.Contains(t, components, "tracker")
		assert.Contains(t, components, "workerPool")
	})
}

func TestAPIServerRequiresComponents(t *testing.T) {
	t.Parallel()
	cfg := config.NewServerConfig()
	cfg.Port = testPort2

	// Create API server without required components should fail
	t.Run("MissingQueue", func(t *testing.T) {
		t.Parallel()
		server, err := apiserver.NewAPIServerWithComponents(cfg, nil, mocks.NewDeploymentTracker(t), mocks.NewWorkerPool(t), mocks.NewMockStateStore(), nil, nil)
		require.Error(t, err)
		require.Nil(t, server)
		require.Contains(t, err.Error(), "deployment queue is required")
	})

	t.Run("MissingTracker", func(t *testing.T) {
		t.Parallel()
		server, err := apiserver.NewAPIServerWithComponents(cfg, mocks.NewDeploymentQueue(t), nil, mocks.NewWorkerPool(t), mocks.NewMockStateStore(), nil, nil)
		require.Error(t, err)
		require.Nil(t, server)
		require.Contains(t, err.Error(), "deployment tracker is required")
	})
}

// Note: parseTerraformJSON is tested through the API endpoints

func TestShutdownWithComponents(t *testing.T) {
	t.Parallel()
	// Create mock components
	queue := mocks.NewDeploymentQueue(t)
	tracker := mocks.NewDeploymentTracker(t)
	workerPool := mocks.NewWorkerPool(t)
	stateStore := mocks.NewMockStateStore()

	// Set up expectations for shutdown
	ctx := context.Background()
	workerPool.On("Stop", ctx).Return(nil)

	// Create server config
	cfg := config.NewServerConfig()
	cfg.Port = testPort3

	// Create server with components
	server, err := apiserver.NewAPIServerWithComponents(cfg, queue, tracker, workerPool, stateStore, nil, nil)
	require.NoError(t, err)

	// Shutdown should clean up gracefully
	err = server.Shutdown(ctx)
	require.NoError(t, err)

	// Server should have shut down cleanly
	// Note: Individual component cleanup is handled by their respective owners
}

// TestDeploymentHandlerResourceArrayConversion tests the resource map to array conversion
// This validates the fix for the "Should have resources" test failure
func TestDeploymentHandlerResourceArrayConversion(t *testing.T) { //nolint:funlen // Test function with comprehensive test cases
	t.Parallel()

	// Create handler instance to test the conversion logic
	handler := &handlers.DeploymentHandler{} // Using struct literal since we only need the method

	t.Run("ConvertResourceMapToArray", func(t *testing.T) {
		t.Parallel()
		// Test resource map conversion (as saved by ResourceExecutor)
		resourceMap := map[string]interface{}{
			"random_string.bucket_suffix": map[string]interface{}{
				"id":     "abc123",
				"result": "abc123",
				"length": 8,
			},
			"aws_s3_bucket.demo": map[string]interface{}{
				"id":     "lattiam-simple-bucket-abc123",
				"bucket": "lattiam-simple-bucket-abc123",
				"region": "us-east-1",
				"tags": map[string]interface{}{
					"Environment": "demo",
				},
			},
		}

		// Convert to array format (as expected by API tests)
		resources := handler.ConvertResourceMapToArray(resourceMap)

		// Validate conversion
		require.Len(t, resources, 2, "Should convert all resources")

		// Validate each resource structure
		resourcesByKey := make(map[string]map[string]interface{})
		for _, res := range resources {
			resource, ok := res.(map[string]interface{})
			require.True(t, ok, "Each resource should be a map")

			// Check required fields
			resourceType, ok := resource["type"].(string)
			require.True(t, ok, "Resource should have type field")

			resourceName, ok := resource["name"].(string)
			require.True(t, ok, "Resource should have name field")

			status, ok := resource["status"].(string)
			require.True(t, ok, "Resource should have status field")
			assert.Equal(t, "completed", status)

			state, ok := resource["state"].(map[string]interface{})
			require.True(t, ok, "Resource should have state field")
			assert.NotEmpty(t, state, "Resource state should not be empty")

			resourceKey := resourceType + "." + resourceName
			resourcesByKey[resourceKey] = resource
		}

		// Verify specific resources
		randomResource := resourcesByKey["random_string.bucket_suffix"]
		require.NotNil(t, randomResource, "Should have random_string resource")
		randomState := randomResource["state"].(map[string]interface{})
		assert.Equal(t, "abc123", randomState["result"])
		assert.Equal(t, 8, randomState["length"])

		s3Resource := resourcesByKey["aws_s3_bucket.demo"]
		require.NotNil(t, s3Resource, "Should have aws_s3_bucket resource")
		s3State := s3Resource["state"].(map[string]interface{})
		assert.Equal(t, "lattiam-simple-bucket-abc123", s3State["bucket"])
		assert.Equal(t, "us-east-1", s3State["region"])

		t.Log("✓ Resource map to array conversion working correctly")
	})

	t.Run("ConvertSimpleValues", func(t *testing.T) {
		t.Parallel()
		// Test conversion of simple values (not maps)
		resourceMap := map[string]interface{}{
			"simple_resource.test": "simple-value",
		}

		resources := handler.ConvertResourceMapToArray(resourceMap)
		require.Len(t, resources, 1)

		resource := resources[0].(map[string]interface{})
		assert.Equal(t, "simple_resource", resource["type"])
		assert.Equal(t, "test", resource["name"])
		assert.Equal(t, "completed", resource["status"])

		state := resource["state"].(map[string]interface{})
		assert.Equal(t, "simple-value", state["id"]) // Simple values become id

		t.Log("✓ Simple value conversion working correctly")
	})

	t.Run("ConvertEmptyMap", func(t *testing.T) {
		t.Parallel()
		// Test empty resource map
		resourceMap := map[string]interface{}{}
		resources := handler.ConvertResourceMapToArray(resourceMap)
		assert.Empty(t, resources, "Empty map should result in empty array")

		t.Log("✓ Empty map conversion working correctly")
	})
}
