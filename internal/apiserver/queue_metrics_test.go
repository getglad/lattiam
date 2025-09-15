package apiserver

import (
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

	"github.com/lattiam/lattiam/internal/config"
	"github.com/lattiam/lattiam/internal/infra/embedded"
	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/internal/mocks"
)

func TestQueueMetricsEndpoint(t *testing.T) { //nolint:funlen,gocyclo // Test function with comprehensive test cases
	t.Parallel()

	t.Run("EmbeddedQueue", func(t *testing.T) {
		t.Parallel()
		// Create components
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

		// Create API server
		stateStore := mocks.NewMockStateStore()
		cfg := &config.ServerConfig{Port: 8080}
		apiServer, err := NewAPIServerWithComponents(
			cfg, queue, tracker, workerPool, stateStore, nil, nil,
		)
		require.NoError(t, err)

		// Enqueue some deployments to generate metrics
		for i := 0; i < 3; i++ {
			deployment := &interfaces.QueuedDeployment{
				ID:        fmt.Sprintf("test-deployment-%d", i),
				Request:   &interfaces.DeploymentRequest{},
				Status:    interfaces.DeploymentStatusQueued,
				CreatedAt: time.Now(),
			}
			err = queue.Enqueue(context.Background(), deployment)
			require.NoError(t, err)
		}

		// Dequeue one to update dequeue metrics
		deployment, err := queue.Dequeue(context.Background())
		require.NoError(t, err)
		require.NotNil(t, deployment)

		// Test the metrics endpoint
		req := httptest.NewRequest("GET", "/api/v1/queue/metrics", nil)
		rec := httptest.NewRecorder()

		apiServer.Router().ServeHTTP(rec, req)

		// Check response
		assert.Equal(t, http.StatusOK, rec.Code)

		var response map[string]interface{}
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)

		// Verify metrics
		assert.InEpsilon(t, float64(3), response["total_enqueued"], 0.01)
		assert.InEpsilon(t, float64(1), response["total_dequeued"], 0.01)
		assert.InEpsilon(t, float64(2), response["current_depth"], 0.01)
		assert.NotEmpty(t, response["average_wait_time"])
		assert.NotEmpty(t, response["oldest_deployment"])
	})

	t.Run("MockedQueue", func(t *testing.T) {
		t.Parallel()
		// Create mock components
		queue := mocks.NewDeploymentQueue(t)
		tracker := mocks.NewDeploymentTracker(t)
		workerPool := mocks.NewWorkerPool(t)
		stateStore := mocks.NewMockStateStore()

		// Set up expectations for GetMetrics only
		queue.On("GetMetrics").Return(interfaces.QueueMetrics{
			TotalEnqueued:    100,
			TotalDequeued:    80,
			CurrentDepth:     20,
			AverageWaitTime:  45 * time.Second,
			OldestDeployment: time.Now().Add(-10 * time.Minute),
		})

		// Mock worker pool operations
		workerPool.On("Stop", mock.Anything).Return(nil)

		// Create API server config
		cfg := config.NewServerConfig()
		cfg.Port = 8081

		// Create API server with components
		server, err := NewAPIServerWithComponents(cfg, queue, tracker, workerPool, stateStore, nil, nil)
		require.NoError(t, err)
		require.NotNil(t, server)

		// Cleanup
		ctx := context.Background()
		t.Cleanup(func() {
			_ = server.Shutdown(ctx)
		})

		// Test the metrics endpoint
		req := httptest.NewRequest("GET", "/api/v1/queue/metrics", nil)
		rec := httptest.NewRecorder()

		server.Router().ServeHTTP(rec, req)

		// Check response
		assert.Equal(t, http.StatusOK, rec.Code)

		var response map[string]interface{}
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)

		// Verify metrics
		assert.InEpsilon(t, float64(100), response["total_enqueued"], 0.01)
		assert.InEpsilon(t, float64(80), response["total_dequeued"], 0.01)
		assert.InEpsilon(t, float64(20), response["current_depth"], 0.01)
		assert.Equal(t, "45s", response["average_wait_time"])
		assert.NotEmpty(t, response["oldest_deployment"])
	})
}
