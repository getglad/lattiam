package apiserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/config"
	"github.com/lattiam/lattiam/internal/infra/embedded"
	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/internal/mocks"
)

func TestSystemHealthEndpoint(t *testing.T) { //nolint:funlen // Test function with comprehensive test cases
	t.Parallel()

	t.Run("HealthySystem", func(t *testing.T) {
		t.Parallel()
		// Create real components
		queue := embedded.NewQueue(10)
		tracker := embedded.NewTracker()
		workerPool, err := embedded.NewWorkerPool(embedded.WorkerPoolConfig{
			MinWorkers: 1,
			MaxWorkers: 2,
			Queue:      queue,
			Tracker:    tracker,
			Executor: func(_ context.Context, _ *interfaces.QueuedDeployment) error {
				return nil
			},
		})
		require.NoError(t, err)
		stateStore := mocks.NewMockStateStore()

		// Create API server
		cfg := &config.ServerConfig{Port: 8080}
		apiServer, err := NewAPIServerWithComponents(cfg, queue, tracker, workerPool, stateStore, nil, nil)
		require.NoError(t, err)

		// Test the health endpoint
		req := httptest.NewRequest("GET", "/api/v1/system/health", nil)
		rec := httptest.NewRecorder()

		apiServer.Router().ServeHTTP(rec, req)

		// Check response
		assert.Equal(t, http.StatusOK, rec.Code)

		var response map[string]interface{}
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)

		// Verify response structure
		assert.Equal(t, "healthy", response["status"])
		assert.NotEmpty(t, response["time"])

		// Check components
		components := response["components"].(map[string]interface{})
		assert.NotNil(t, components["queue"])
		assert.NotNil(t, components["tracker"])
		assert.NotNil(t, components["workerPool"])
		assert.NotNil(t, components["stateStore"])

		// Check queue component details
		queueHealth := components["queue"].(map[string]interface{})
		assert.Equal(t, "healthy", queueHealth["status"])
		assert.Contains(t, queueHealth, "depth")
		assert.Contains(t, queueHealth, "enqueued")
		assert.Contains(t, queueHealth, "dequeued")

		// Check system metrics
		system := response["system"].(map[string]interface{})
		assert.Contains(t, system, "goroutines")
		assert.Contains(t, system, "memory")

		// Check version
		version := response["version"].(map[string]interface{})
		assert.Equal(t, "v1", version["api"])
	})

	t.Run("DegradedSystem", func(t *testing.T) {
		t.Parallel()
		// Create mock components with failures
		queue := mocks.NewDeploymentQueue(t)
		tracker := mocks.NewDeploymentTracker(t)
		workerPool := mocks.NewWorkerPool(t)
		stateStore := mocks.NewMockStateStore()

		// Set up mocks to simulate unhealthy components
		queue.On("GetMetrics").Return(interfaces.QueueMetrics{
			CurrentDepth: 2000, // High depth triggers warning
		})

		tracker.On("List", mock.Anything).Return(nil, fmt.Errorf("tracker connection failed"))

		// Mock state store Ping to return error
		stateStore.SetShouldFail("Ping", fmt.Errorf("connection refused"))

		// Mock worker pool operations
		workerPool.On("Stop", mock.Anything).Return(nil)

		// Create API server
		cfg := &config.ServerConfig{Port: 8081}
		apiServer, err := NewAPIServerWithComponents(cfg, queue, tracker, workerPool, stateStore, nil, nil)
		require.NoError(t, err)

		// Cleanup
		ctx := context.Background()
		t.Cleanup(func() {
			_ = apiServer.Shutdown(ctx)
		})

		// Test the health endpoint
		req := httptest.NewRequest("GET", "/api/v1/system/health", nil)
		rec := httptest.NewRecorder()

		apiServer.Router().ServeHTTP(rec, req)

		// Check response
		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)

		var response map[string]interface{}
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)

		// Verify degraded status
		assert.Equal(t, "degraded", response["status"])

		// Check unhealthy components
		components := response["components"].(map[string]interface{})

		queueHealth := components["queue"].(map[string]interface{})
		assert.Equal(t, "warning", queueHealth["status"])
		assert.Contains(t, queueHealth, "message")

		trackerHealth := components["tracker"].(map[string]interface{})
		assert.Equal(t, "unhealthy", trackerHealth["status"])
		assert.Contains(t, trackerHealth, "message")

		stateStoreHealth := components["stateStore"].(map[string]interface{})
		assert.Equal(t, "unhealthy", stateStoreHealth["status"])
		assert.Contains(t, stateStoreHealth, "message")
	})
}
