package distributed

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hibiken/asynq"

	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/pkg/logging"
)

const (
	// TaskTypeDeployment is the task type for deployments
	TaskTypeDeployment = "deployment:process"
)

// Queue implements interfaces.DeploymentQueue using Asynq (Redis-backed)
type Queue struct {
	client            *asynq.Client
	redisOpt          asynq.RedisConnOpt
	logger            *logging.Logger
	resilientExecutor *QueueResilientExecutor
}

// NewQueue creates a new distributed deployment queue
func NewQueue(redisURL string) (*Queue, error) {
	if redisURL == "" {
		return nil, fmt.Errorf("redis URL is required")
	}

	// Parse Redis connection options
	redisOpt, err := asynq.ParseRedisURI(redisURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse redis URL: %w", err)
	}

	client := asynq.NewClient(redisOpt)

	// Create resilient executor
	resilientExecutor, err := NewQueueResilientExecutor(redisOpt, DefaultResilienceConfig())
	if err != nil {
		return nil, fmt.Errorf("failed to create resilient executor: %w", err)
	}

	return &Queue{
		client:            client,
		redisOpt:          redisOpt,
		logger:            logging.NewLogger("distributed-queue"),
		resilientExecutor: resilientExecutor,
	}, nil
}

// Enqueue adds a deployment to the distributed queue with resilience patterns
func (q *Queue) Enqueue(ctx context.Context, deployment *interfaces.QueuedDeployment) error {
	if deployment == nil {
		return fmt.Errorf("deployment is nil")
	}
	if deployment.ID == "" {
		return fmt.Errorf("deployment ID is empty")
	}

	// Use resilient executor for the enqueue operation
	return q.resilientExecutor.ExecuteWithResilience(ctx, fmt.Sprintf("enqueue-%s", deployment.ID), func() error {
		// Serialize deployment to JSON
		payload, err := json.Marshal(deployment)
		if err != nil {
			return fmt.Errorf("failed to marshal deployment: %w", err)
		}

		// Create asynq task
		task := asynq.NewTask(TaskTypeDeployment, payload,
			asynq.TaskID(deployment.ID),
			asynq.Queue("deployments"),
			asynq.MaxRetry(deployment.Request.Options.MaxRetries),
		)

		// Enqueue the task
		info, err := q.client.EnqueueContext(ctx, task)
		if err != nil {
			return fmt.Errorf("failed to enqueue deployment: %w", err)
		}

		// Log success
		q.logger.Info("Enqueued deployment %s, task ID: %s", deployment.ID, info.ID)
		return nil
	})
}

// Cancel cancels a deployment in the queue
func (q *Queue) Cancel(_ context.Context, deploymentID string) error {
	if deploymentID == "" {
		return fmt.Errorf("deployment ID is empty")
	}

	// Create inspector to cancel tasks
	inspector := asynq.NewInspector(q.redisOpt)
	defer func() {
		if err := inspector.Close(); err != nil {
			// Error closing inspector - logged but doesn't fail the operation
			q.logger.Warn("Failed to close inspector during cancel operation: %v", err)
		}
	}()

	// Try to cancel in different states
	// First try pending queue
	err := inspector.DeleteTask("deployments", deploymentID)
	if err == nil {
		return nil
	}

	// Try scheduled queue
	err = inspector.DeleteTask("scheduled", deploymentID)
	if err == nil {
		return nil
	}

	// Try retry queue
	err = inspector.DeleteTask("retry", deploymentID)
	if err == nil {
		return nil
	}

	// If task is already processing, we can't cancel it from here
	// The worker needs to handle cancellation
	return fmt.Errorf("deployment %s not found in any queue or already processing", deploymentID)
}

// Close closes the queue client and resilient executor
func (q *Queue) Close() error {
	// Close resilient executor first
	if q.resilientExecutor != nil {
		if err := q.resilientExecutor.Close(); err != nil {
			q.logger.Error("Failed to close resilient executor: %v", err)
		}
	}

	// Close asynq client
	err := q.client.Close()
	if err != nil {
		return fmt.Errorf("failed to close asynq client: %w", err)
	}
	return nil
}

// GetRedisClient returns the underlying Redis client options
// This is useful for creating other components that need the same Redis connection
func (q *Queue) GetRedisClient() asynq.RedisConnOpt {
	return q.redisOpt
}

// GetMetrics returns queue metrics
func (q *Queue) GetMetrics() interfaces.QueueMetrics {
	inspector := asynq.NewInspector(q.redisOpt)
	defer func() {
		if err := inspector.Close(); err != nil {
			q.logger.Error("Failed to close inspector: %v", err)
		}
	}()

	// Get queue info
	info, err := inspector.GetQueueInfo("deployments")
	if err != nil {
		q.logger.Error("Failed to get queue info: %v", err)
		return interfaces.QueueMetrics{}
	}

	// Calculate average wait time from latency (milliseconds to duration)
	avgWaitTime := info.Latency * time.Millisecond

	// Get oldest deployment time
	var oldestTime time.Time
	if info.Size > 0 {
		// Try to get the oldest pending task
		tasks, err := inspector.ListPendingTasks("deployments", asynq.PageSize(1))
		if err == nil && len(tasks) > 0 {
			// Get the task's enqueued time
			oldestTime = tasks[0].NextProcessAt
		}
	}

	return interfaces.QueueMetrics{
		TotalEnqueued:    int64(info.Processed + info.Size + info.Active),
		TotalDequeued:    int64(info.Processed),
		CurrentDepth:     info.Size,
		AverageWaitTime:  avgWaitTime,
		OldestDeployment: oldestTime,
	}
}
