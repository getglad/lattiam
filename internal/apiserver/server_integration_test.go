//go:build integration && localstack
// +build integration,localstack

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
	"github.com/lattiam/lattiam/internal/config"
	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/internal/mocks"
)

// TestAPIServerIntegration consolidates all API server integration tests
func TestAPIServerIntegration(t *testing.T) {
	t.Parallel()

	// Server Creation Tests
	t.Run("ServerCreation", func(t *testing.T) {
		t.Parallel()

		t.Run("WithComponents", func(t *testing.T) {
			t.Parallel()
			// Create mock components
			queue := mocks.NewDeploymentQueue(t)
			tracker := mocks.NewDeploymentTracker(t)
			workerPool := mocks.NewWorkerPool(t)
			stateStore := mocks.NewMockStateStore()

			// Create server config
			cfg := &config.ServerConfig{
				Port: 8100,
			}

			server, err := apiserver.NewAPIServerWithComponents(cfg, queue, tracker, workerPool, stateStore, nil, nil)
			require.NoError(t, err)
			require.NotNil(t, server)
			require.NotNil(t, server.Router())

			// Verify the server was created successfully
			assert.NotNil(t, server)
		})

		t.Run("WithoutQueue", func(t *testing.T) {
			t.Parallel()
			// Create server config
			cfg := &config.ServerConfig{
				Port: 8101,
			}

			server, err := apiserver.NewAPIServerWithComponents(cfg, nil, mocks.NewDeploymentTracker(t), mocks.NewWorkerPool(t), mocks.NewMockStateStore(), nil, nil)
			require.Error(t, err)
			require.Nil(t, server)
			require.Contains(t, err.Error(), "deployment queue is required")
		})
	})

	// HTTP API Endpoint Tests
	t.Run("HTTPEndpoints", func(t *testing.T) {
		t.Parallel()

		t.Run("CreateDeployment", func(t *testing.T) {
			t.Parallel()
			// Create mock components
			queue := mocks.NewDeploymentQueue(t)
			tracker := mocks.NewDeploymentTracker(t)
			workerPool := mocks.NewWorkerPool(t)
			stateStore := mocks.NewMockStateStore()

			// Set up queue expectations
			queue.On("Enqueue", mock.Anything, mock.Anything).Return(nil)
			queue.On("GetMetrics").Return(interfaces.QueueMetrics{
				TotalEnqueued:    10,
				TotalDequeued:    5,
				CurrentDepth:     5,
				AverageWaitTime:  30 * time.Second,
				OldestDeployment: time.Now().Add(-5 * time.Minute),
			})

			// Set up tracker expectations
			tracker.On("Register", mock.Anything).Return(nil)

			cfg := &config.ServerConfig{
				Port: 8102,
			}
			server, err := apiserver.NewAPIServerWithComponents(cfg, queue, tracker, workerPool, stateStore, nil, nil)
			require.NoError(t, err)

			requestBody := apiserver.DeploymentRequest{
				Name: "test-deployment",
				TerraformJSON: map[string]interface{}{
					"resource": map[string]interface{}{
						"aws_iam_role": map[string]interface{}{
							"test": map[string]interface{}{
								"name": "test-role",
							},
						},
					},
				},
			}

			body, err := json.Marshal(requestBody)
			require.NoError(t, err)

			req := httptest.NewRequest("POST", "/api/v1/deployments", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			server.Router().ServeHTTP(w, req)

			assert.Equal(t, http.StatusCreated, w.Code)

			var response map[string]interface{}
			err = json.Unmarshal(w.Body.Bytes(), &response)
			require.NoError(t, err)

			assert.Contains(t, response, "id")
			assert.Contains(t, response, "status")
			assert.Equal(t, "queued", response["status"])
			assert.Contains(t, response, "created_at")

			// Verify the deployment was registered and enqueued
			tracker.AssertExpectations(t)
			queue.AssertExpectations(t)
		})

		t.Run("GetDeployment", func(t *testing.T) {
			t.Parallel()
			// Create mock components
			queue := mocks.NewDeploymentQueue(t)
			tracker := mocks.NewDeploymentTracker(t)
			workerPool := mocks.NewWorkerPool(t)
			stateStore := mocks.NewMockStateStore()

			// Set up tracker to return deployment not found
			tracker.On("GetByID", "test-deployment-456").Return(nil, fmt.Errorf("deployment test-deployment-456 not found"))

			cfg := &config.ServerConfig{Port: 8103}
			server, err := apiserver.NewAPIServerWithComponents(cfg, queue, tracker, workerPool, stateStore, nil, nil)
			require.NoError(t, err)

			deploymentID := "test-deployment-456"
			req := httptest.NewRequest("GET", fmt.Sprintf("/api/v1/deployments/%s", deploymentID), nil)
			w := httptest.NewRecorder()
			server.Router().ServeHTTP(w, req)

			assert.Equal(t, http.StatusNotFound, w.Code)
		})

		t.Run("ListDeployments", func(t *testing.T) {
			t.Parallel()
			// Create mock components
			queue := mocks.NewDeploymentQueue(t)
			tracker := mocks.NewDeploymentTracker(t)
			workerPool := mocks.NewWorkerPool(t)
			stateStore := mocks.NewMockStateStore()

			// Set up tracker to return empty list
			tracker.On("List", mock.Anything).Return([]*interfaces.QueuedDeployment{}, nil)

			cfg := &config.ServerConfig{Port: 8104}
			server, err := apiserver.NewAPIServerWithComponents(cfg, queue, tracker, workerPool, stateStore, nil, nil)
			require.NoError(t, err)

			req := httptest.NewRequest("GET", "/api/v1/deployments", nil)
			w := httptest.NewRecorder()
			server.Router().ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)
			var response []interface{}
			err = json.Unmarshal(w.Body.Bytes(), &response)
			require.NoError(t, err)
			assert.Empty(t, response)
		})

		t.Run("DeleteDeployment", func(t *testing.T) {
			t.Parallel()
			// Create mock components
			queue := mocks.NewDeploymentQueue(t)
			tracker := mocks.NewDeploymentTracker(t)
			workerPool := mocks.NewWorkerPool(t)
			stateStore := mocks.NewMockStateStore()

			// Set up tracker to return not found
			tracker.On("GetStatus", "test-deployment-789").Return(nil, fmt.Errorf("deployment not found"))

			cfg := &config.ServerConfig{Port: 8105}
			server, err := apiserver.NewAPIServerWithComponents(cfg, queue, tracker, workerPool, stateStore, nil, nil)
			require.NoError(t, err)

			deploymentID := "test-deployment-789"
			req := httptest.NewRequest("DELETE", fmt.Sprintf("/api/v1/deployments/%s", deploymentID), nil)
			w := httptest.NewRecorder()
			server.Router().ServeHTTP(w, req)

			assert.NotEqual(t, http.StatusNoContent, w.Code)
		})

		t.Run("CancelDeployment", func(t *testing.T) {
			t.Parallel()
			// Create mock components
			queue := mocks.NewDeploymentQueue(t)
			tracker := mocks.NewDeploymentTracker(t)
			workerPool := mocks.NewWorkerPool(t)
			stateStore := mocks.NewMockStateStore()

			// Set up queue to return error when canceling non-existent deployment
			queue.On("Cancel", mock.Anything, "test-deployment-123").Return(fmt.Errorf("deployment not found"))

			cfg := &config.ServerConfig{Port: 8106}
			server, err := apiserver.NewAPIServerWithComponents(cfg, queue, tracker, workerPool, stateStore, nil, nil)
			require.NoError(t, err)

			deploymentID := "test-deployment-123"
			req := httptest.NewRequest("POST", fmt.Sprintf("/api/v1/deployments/%s/cancel", deploymentID), nil)
			w := httptest.NewRecorder()
			server.Router().ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)
		})

		t.Run("UpdateDeployment", func(t *testing.T) {
			t.Parallel()
			// Create mock components
			queue := mocks.NewDeploymentQueue(t)
			tracker := mocks.NewDeploymentTracker(t)
			workerPool := mocks.NewWorkerPool(t)
			stateStore := mocks.NewMockStateStore()

			// Set up tracker to return not found
			tracker.On("GetStatus", "nonexistent-id").Return(nil, fmt.Errorf("deployment not found"))

			cfg := &config.ServerConfig{Port: 8107}
			server, err := apiserver.NewAPIServerWithComponents(cfg, queue, tracker, workerPool, stateStore, nil, nil)
			require.NoError(t, err)

			updateReq := map[string]interface{}{
				"name": "test-update",
				"terraform_json": map[string]interface{}{
					"resource": map[string]interface{}{
						"aws_s3_bucket": map[string]interface{}{
							"test": map[string]interface{}{
								"bucket": "test-bucket-update",
							},
						},
					},
				},
			}

			body, err := json.Marshal(updateReq)
			require.NoError(t, err)

			req := httptest.NewRequest("PUT", "/api/v1/deployments/nonexistent-id", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			server.Router().ServeHTTP(w, req)

			assert.Equal(t, http.StatusNotFound, w.Code)

			var errorResp map[string]interface{}
			err = json.NewDecoder(w.Body).Decode(&errorResp)
			require.NoError(t, err)
			assert.Contains(t, errorResp, "error")
		})
	})

	// System Endpoints Tests
	t.Run("SystemEndpoints", func(t *testing.T) {
		t.Parallel()

		t.Run("QueueMetrics", func(t *testing.T) {
			t.Parallel()
			// Create mock components
			queue := mocks.NewDeploymentQueue(t)
			tracker := mocks.NewDeploymentTracker(t)
			workerPool := mocks.NewWorkerPool(t)
			stateStore := mocks.NewMockStateStore()

			// Set up queue metrics expectation
			queue.On("GetMetrics").Return(interfaces.QueueMetrics{
				TotalEnqueued:    15,
				TotalDequeued:    10,
				CurrentDepth:     5,
				AverageWaitTime:  45 * time.Second,
				OldestDeployment: time.Now().Add(-10 * time.Minute),
			})

			cfg := &config.ServerConfig{Port: 8108}
			server, err := apiserver.NewAPIServerWithComponents(cfg, queue, tracker, workerPool, stateStore, nil, nil)
			require.NoError(t, err)

			req := httptest.NewRequest("GET", "/api/v1/queue/metrics", nil)
			w := httptest.NewRecorder()
			server.Router().ServeHTTP(w, req)

			assert.NotEqual(t, http.StatusNotFound, w.Code)
		})

		t.Run("SystemHealth", func(t *testing.T) {
			t.Parallel()
			// Create mock components
			queue := mocks.NewDeploymentQueue(t)
			tracker := mocks.NewDeploymentTracker(t)
			workerPool := mocks.NewWorkerPool(t)
			stateStore := mocks.NewMockStateStore()

			// Set up queue metrics expectation for health check
			queue.On("GetMetrics").Return(interfaces.QueueMetrics{
				TotalEnqueued:    20,
				TotalDequeued:    18,
				CurrentDepth:     2,
				AverageWaitTime:  15 * time.Second,
				OldestDeployment: time.Now().Add(-2 * time.Minute),
			})

			// Set up tracker expectation for health check
			tracker.On("List", mock.MatchedBy(func(filter interfaces.DeploymentFilter) bool {
				return filter.CreatedAfter.Before(time.Now())
			})).Return([]*interfaces.QueuedDeployment{}, nil)

			cfg := &config.ServerConfig{Port: 8109}
			server, err := apiserver.NewAPIServerWithComponents(cfg, queue, tracker, workerPool, stateStore, nil, nil)
			require.NoError(t, err)

			req := httptest.NewRequest("GET", "/api/v1/system/health", nil)
			w := httptest.NewRecorder()
			server.Router().ServeHTTP(w, req)

			assert.Equal(t, http.StatusOK, w.Code)

			var health map[string]interface{}
			err = json.Unmarshal(w.Body.Bytes(), &health)
			require.NoError(t, err)
			assert.Contains(t, health, "status")
			assert.Contains(t, health, "components")
		})
	})

	// Validation Tests
	t.Run("Validation", func(t *testing.T) {
		t.Parallel()

		t.Run("MissingName", func(t *testing.T) {
			t.Parallel()
			// Create mock components
			queue := mocks.NewDeploymentQueue(t)
			tracker := mocks.NewDeploymentTracker(t)
			workerPool := mocks.NewWorkerPool(t)
			stateStore := mocks.NewMockStateStore()

			// No expectations needed - validation should fail before any operations

			cfg := &config.ServerConfig{Port: 8110}
			server, err := apiserver.NewAPIServerWithComponents(cfg, queue, tracker, workerPool, stateStore, nil, nil)
			require.NoError(t, err)

			requestBody := apiserver.DeploymentRequest{
				// Name is missing
				TerraformJSON: map[string]interface{}{
					"resource": map[string]interface{}{},
				},
			}
			body, _ := json.Marshal(requestBody)
			req := httptest.NewRequest("POST", "/api/v1/deployments", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			server.Router().ServeHTTP(w, req)
			assert.Equal(t, http.StatusBadRequest, w.Code)
		})

		t.Run("InvalidJSON", func(t *testing.T) {
			t.Parallel()
			// Create mock components
			queue := mocks.NewDeploymentQueue(t)
			tracker := mocks.NewDeploymentTracker(t)
			workerPool := mocks.NewWorkerPool(t)
			stateStore := mocks.NewMockStateStore()

			cfg := &config.ServerConfig{Port: 8111}
			server, err := apiserver.NewAPIServerWithComponents(cfg, queue, tracker, workerPool, stateStore, nil, nil)
			require.NoError(t, err)

			req := httptest.NewRequest("POST", "/api/v1/deployments", bytes.NewReader([]byte("invalid json")))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			server.Router().ServeHTTP(w, req)
			assert.Equal(t, http.StatusBadRequest, w.Code)
		})

		t.Run("MissingTerraformJSON", func(t *testing.T) {
			t.Parallel()
			// Create mock components
			queue := mocks.NewDeploymentQueue(t)
			tracker := mocks.NewDeploymentTracker(t)
			workerPool := mocks.NewWorkerPool(t)
			stateStore := mocks.NewMockStateStore()

			// No expectations needed - validation should fail before any operations

			cfg := &config.ServerConfig{Port: 8112}
			server, err := apiserver.NewAPIServerWithComponents(cfg, queue, tracker, workerPool, stateStore, nil, nil)
			require.NoError(t, err)

			requestBody := apiserver.DeploymentRequest{
				Name: "test",
				// TerraformJSON is missing
			}
			body, _ := json.Marshal(requestBody)
			req := httptest.NewRequest("POST", "/api/v1/deployments", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			server.Router().ServeHTTP(w, req)
			assert.Equal(t, http.StatusBadRequest, w.Code)
		})

		t.Run("InvalidResourceType", func(t *testing.T) {
			t.Parallel()
			// Create mock components
			queue := mocks.NewDeploymentQueue(t)
			tracker := mocks.NewDeploymentTracker(t)
			workerPool := mocks.NewWorkerPool(t)
			stateStore := mocks.NewMockStateStore()

			// Set up queue expectations
			queue.On("Enqueue", mock.Anything, mock.Anything).Return(nil)
			queue.On("GetMetrics").Return(interfaces.QueueMetrics{
				TotalEnqueued:    5,
				TotalDequeued:    3,
				CurrentDepth:     2,
				AverageWaitTime:  20 * time.Second,
				OldestDeployment: time.Now().Add(-3 * time.Minute),
			})

			// Set up tracker expectations
			tracker.On("Register", mock.Anything).Return(nil)

			cfg := &config.ServerConfig{Port: 8113}
			server, err := apiserver.NewAPIServerWithComponents(cfg, queue, tracker, workerPool, stateStore, nil, nil)
			require.NoError(t, err)

			invalidJSON := map[string]interface{}{
				"name": "invalid-demo",
				"terraform_json": map[string]interface{}{
					"resource": map[string]interface{}{
						"invalid_resource_type": map[string]interface{}{
							"test": map[string]interface{}{},
						},
					},
				},
			}

			body, err := json.Marshal(invalidJSON)
			require.NoError(t, err)

			req := httptest.NewRequest("POST", "/api/v1/deployments", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			server.Router().ServeHTTP(w, req)

			// API accepts invalid resource types but they fail during deployment
			assert.Equal(t, http.StatusCreated, w.Code)
		})

		t.Run("MissingRequiredFields", func(t *testing.T) {
			t.Parallel()
			// Create mock components
			queue := mocks.NewDeploymentQueue(t)
			tracker := mocks.NewDeploymentTracker(t)
			workerPool := mocks.NewWorkerPool(t)
			stateStore := mocks.NewMockStateStore()

			// Set up queue expectations
			queue.On("Enqueue", mock.Anything, mock.Anything).Return(nil)
			queue.On("GetMetrics").Return(interfaces.QueueMetrics{
				TotalEnqueued:    8,
				TotalDequeued:    6,
				CurrentDepth:     2,
				AverageWaitTime:  25 * time.Second,
				OldestDeployment: time.Now().Add(-4 * time.Minute),
			})

			// Set up tracker expectations
			tracker.On("Register", mock.Anything).Return(nil)

			cfg := &config.ServerConfig{Port: 8114}
			server, err := apiserver.NewAPIServerWithComponents(cfg, queue, tracker, workerPool, stateStore, nil, nil)
			require.NoError(t, err)

			missingFieldsJSON := map[string]interface{}{
				"name": "missing-fields-demo",
				"terraform_json": map[string]interface{}{
					"resource": map[string]interface{}{
						"aws_s3_bucket": map[string]interface{}{
							"test": map[string]interface{}{
								// Missing required 'bucket' field
							},
						},
					},
				},
			}

			body, err := json.Marshal(missingFieldsJSON)
			require.NoError(t, err)

			req := httptest.NewRequest("POST", "/api/v1/deployments", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			server.Router().ServeHTTP(w, req)

			assert.Equal(t, http.StatusCreated, w.Code)
		})
	})

	// API Endpoint Validation Tests
	t.Run("EndpointValidation", func(t *testing.T) {
		t.Parallel()

		t.Run("TrailingSlashHandling", func(t *testing.T) {
			t.Parallel()
			// Create mock components
			queue := mocks.NewDeploymentQueue(t)
			tracker := mocks.NewDeploymentTracker(t)
			workerPool := mocks.NewWorkerPool(t)
			stateStore := mocks.NewMockStateStore()

			// Set up tracker to return empty list
			tracker.On("List", mock.Anything).Return([]*interfaces.QueuedDeployment{}, nil)

			cfg := &config.ServerConfig{Port: 8115}
			server, err := apiserver.NewAPIServerWithComponents(cfg, queue, tracker, workerPool, stateStore, nil, nil)
			require.NoError(t, err)

			// Test with trailing slash
			respWithSlash := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/api/v1/deployments/", nil)
			server.Router().ServeHTTP(respWithSlash, req)
			assert.Equal(t, http.StatusOK, respWithSlash.Code)

			// Test without trailing slash
			respWithoutSlash := httptest.NewRecorder()
			req = httptest.NewRequest("GET", "/api/v1/deployments", nil)
			server.Router().ServeHTTP(respWithoutSlash, req)
			assert.Equal(t, http.StatusOK, respWithoutSlash.Code)
		})

		t.Run("404JSONResponse", func(t *testing.T) {
			t.Parallel()
			// Create mock components
			queue := mocks.NewDeploymentQueue(t)
			tracker := mocks.NewDeploymentTracker(t)
			workerPool := mocks.NewWorkerPool(t)
			stateStore := mocks.NewMockStateStore()

			cfg := &config.ServerConfig{Port: 8116}
			server, err := apiserver.NewAPIServerWithComponents(cfg, queue, tracker, workerPool, stateStore, nil, nil)
			require.NoError(t, err)

			req := httptest.NewRequest("GET", "/api/v1/nonexistent", nil)
			w := httptest.NewRecorder()
			server.Router().ServeHTTP(w, req)

			assert.Equal(t, http.StatusNotFound, w.Code)

			var errorResp map[string]interface{}
			err = json.NewDecoder(w.Body).Decode(&errorResp)
			require.NoError(t, err)
			assert.Contains(t, errorResp, "error")
			assert.Equal(t, "not_found", errorResp["error"])
		})

		t.Run("PUTEndpointExists", func(t *testing.T) {
			t.Parallel()
			// Create mock components
			queue := mocks.NewDeploymentQueue(t)
			tracker := mocks.NewDeploymentTracker(t)
			workerPool := mocks.NewWorkerPool(t)
			stateStore := mocks.NewMockStateStore()

			// Set up tracker to return not found
			tracker.On("GetStatus", "nonexistent-id").Return(nil, fmt.Errorf("deployment not found"))

			cfg := &config.ServerConfig{Port: 8117}
			server, err := apiserver.NewAPIServerWithComponents(cfg, queue, tracker, workerPool, stateStore, nil, nil)
			require.NoError(t, err)

			putReq := map[string]interface{}{
				"name": "test-update",
				"terraform_json": map[string]interface{}{
					"resource": map[string]interface{}{
						"aws_s3_bucket": map[string]interface{}{
							"test": map[string]interface{}{
								"bucket": "test-bucket-update",
							},
						},
					},
				},
			}

			body, err := json.Marshal(putReq)
			require.NoError(t, err)

			req := httptest.NewRequest("PUT", "/api/v1/deployments/nonexistent-id", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			server.Router().ServeHTTP(w, req)

			assert.Equal(t, http.StatusNotFound, w.Code)

			var errorResp map[string]interface{}
			err = json.NewDecoder(w.Body).Decode(&errorResp)
			require.NoError(t, err)
			assert.Contains(t, errorResp, "error")
		})
	})

	// Delete Operations Tests
	t.Run("DeleteOperations", func(t *testing.T) {
		t.Parallel()

		t.Run("DeleteNonExistentDeployment", func(t *testing.T) {
			t.Parallel()
			// Create mock components
			queue := mocks.NewDeploymentQueue(t)
			tracker := mocks.NewDeploymentTracker(t)
			workerPool := mocks.NewWorkerPool(t)
			stateStore := mocks.NewMockStateStore()

			// Set up tracker to return not found
			tracker.On("GetStatus", "nonexistent-id").Return(nil, fmt.Errorf("deployment not found"))

			cfg := &config.ServerConfig{Port: 8118}
			server, err := apiserver.NewAPIServerWithComponents(cfg, queue, tracker, workerPool, stateStore, nil, nil)
			require.NoError(t, err)

			req := httptest.NewRequest("DELETE", "/api/v1/deployments/nonexistent-id", nil)
			w := httptest.NewRecorder()
			server.Router().ServeHTTP(w, req)

			assert.Contains(t, []int{http.StatusNotFound, http.StatusBadRequest}, w.Code)
		})

		t.Run("DeleteStatusTransitions", func(t *testing.T) {
			t.Parallel()
			// Create mock components
			queue := mocks.NewDeploymentQueue(t)
			tracker := mocks.NewDeploymentTracker(t)
			workerPool := mocks.NewWorkerPool(t)
			stateStore := mocks.NewMockStateStore()

			// Set up tracker to return not found
			tracker.On("GetStatus", "test-deployment-delete").Return(nil, fmt.Errorf("deployment not found"))

			cfg := &config.ServerConfig{Port: 8119}
			server, err := apiserver.NewAPIServerWithComponents(cfg, queue, tracker, workerPool, stateStore, nil, nil)
			require.NoError(t, err)

			// This would test various deployment statuses and their delete behavior
			// but requires more complex mock setup
			deploymentID := "test-deployment-delete"
			req := httptest.NewRequest("DELETE", fmt.Sprintf("/api/v1/deployments/%s", deploymentID), nil)
			w := httptest.NewRecorder()
			server.Router().ServeHTTP(w, req)

			// Should return an error status since deployment doesn't exist
			assert.NotEqual(t, http.StatusNoContent, w.Code)
		})
	})

	// Shutdown Tests
	t.Run("Shutdown", func(t *testing.T) {
		t.Parallel()

		t.Run("WithComponents", func(t *testing.T) {
			t.Parallel()
			// Create mock components
			queue := mocks.NewDeploymentQueue(t)
			tracker := mocks.NewDeploymentTracker(t)
			workerPool := mocks.NewWorkerPool(t)
			stateStore := mocks.NewMockStateStore()

			// Set up shutdown expectations - only workerPool.Stop() is called
			workerPool.On("Stop", mock.Anything).Return(nil)

			cfg := &config.ServerConfig{Port: 8120}
			server, err := apiserver.NewAPIServerWithComponents(cfg, queue, tracker, workerPool, stateStore, nil, nil)
			require.NoError(t, err)

			ctx := context.Background()
			err = server.Shutdown(ctx)
			require.NoError(t, err)

			// Verify shutdown was called on worker pool
			workerPool.AssertExpectations(t)
		})
	})
}

// Helper function to create a test server with mock components
func createTestServer(t *testing.T, port int) (*apiserver.APIServer, *mocks.DeploymentQueue, *mocks.DeploymentTracker, *mocks.WorkerPool, *mocks.MockStateStore) {
	t.Helper()
	queue := mocks.NewDeploymentQueue(t)
	tracker := mocks.NewDeploymentTracker(t)
	workerPool := mocks.NewWorkerPool(t)
	stateStore := mocks.NewMockStateStore()

	cfg := &config.ServerConfig{Port: port}
	server, err := apiserver.NewAPIServerWithComponents(cfg, queue, tracker, workerPool, stateStore, nil, nil)
	require.NoError(t, err)
	return server, queue, tracker, workerPool, stateStore
}

// Helper function to make HTTP requests
func makeTestRequest(t *testing.T, server *apiserver.APIServer, method, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var reqBody *bytes.Buffer
	if body != nil {
		jsonBody, err := json.Marshal(body)
		require.NoError(t, err)
		reqBody = bytes.NewBuffer(jsonBody)
	} else {
		reqBody = bytes.NewBuffer(nil)
	}

	req := httptest.NewRequest(method, path, reqBody)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	w := httptest.NewRecorder()
	server.Router().ServeHTTP(w, req)
	return w
}

// Helper function to validate JSON error response
func validateJSONError(t *testing.T, w *httptest.ResponseRecorder, expectedCode int) map[string]interface{} {
	t.Helper()
	assert.Equal(t, expectedCode, w.Code)

	var errorResp map[string]interface{}
	err := json.NewDecoder(w.Body).Decode(&errorResp)
	require.NoError(t, err)
	assert.Contains(t, errorResp, "error")

	return errorResp
}
