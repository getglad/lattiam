// Package embedded provides in-memory embedded infrastructure components for local deployment execution.
package embedded

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/lattiam/lattiam/internal/interfaces"
)

// Queue implements interfaces.DeploymentQueue using a Go channel
type Queue struct {
	mu          sync.RWMutex
	deployments chan *interfaces.QueuedDeployment
	cancelMap   map[string]context.CancelFunc
	closed      bool
	closeOnce   sync.Once

	// Metrics
	totalEnqueued  int64
	totalDequeued  int64
	oldestEnqueued time.Time
	totalWaitTime  time.Duration
}

// NewQueue creates a new embedded deployment queue
func NewQueue(capacity int) *Queue {
	if capacity <= 0 {
		capacity = 100 // Default capacity
	}

	return &Queue{
		deployments: make(chan *interfaces.QueuedDeployment, capacity),
		cancelMap:   make(map[string]context.CancelFunc),
	}
}

// Enqueue adds a deployment to the queue
func (q *Queue) Enqueue(ctx context.Context, deployment *interfaces.QueuedDeployment) error {
	if deployment == nil {
		return fmt.Errorf("deployment is nil")
	}
	if deployment.ID == "" {
		return fmt.Errorf("deployment ID is empty")
	}

	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return fmt.Errorf("queue is closed")
	}

	// Check if context is already canceled
	if err := ctx.Err(); err != nil {
		q.mu.Unlock()
		return fmt.Errorf("enqueue canceled: %w", err)
	}

	// Create a cancelable context for this deployment
	deployCtx, cancel := context.WithCancel(ctx)
	q.cancelMap[deployment.ID] = cancel
	q.mu.Unlock()

	// Try to enqueue
	select {
	case q.deployments <- deployment:
		// Update metrics
		q.mu.Lock()
		q.totalEnqueued++
		if q.oldestEnqueued.IsZero() || len(q.deployments) == 1 {
			q.oldestEnqueued = time.Now()
		}
		q.mu.Unlock()
		return nil
	case <-deployCtx.Done():
		// Clean up cancel function if context was canceled
		q.mu.Lock()
		delete(q.cancelMap, deployment.ID)
		q.mu.Unlock()
		return fmt.Errorf("enqueue canceled: %w", deployCtx.Err())
	default:
		// Queue is full
		q.mu.Lock()
		delete(q.cancelMap, deployment.ID)
		q.mu.Unlock()
		return fmt.Errorf("queue is full")
	}
}

// Cancel cancels a deployment in the queue
func (q *Queue) Cancel(_ context.Context, deploymentID string) error {
	if deploymentID == "" {
		return fmt.Errorf("deployment ID is empty")
	}

	q.mu.Lock()
	cancel, exists := q.cancelMap[deploymentID]
	if !exists {
		q.mu.Unlock()
		return fmt.Errorf("deployment %s not found in queue", deploymentID)
	}
	delete(q.cancelMap, deploymentID)
	q.mu.Unlock()

	// Cancel the deployment's context
	cancel()

	return nil
}

// Dequeue retrieves the next deployment from the queue
// This is an internal method used by the worker pool
func (q *Queue) Dequeue(ctx context.Context) (*interfaces.QueuedDeployment, error) {
	select {
	case deployment := <-q.deployments:
		if deployment == nil {
			return nil, fmt.Errorf("queue is closed")
		}

		// Update metrics
		q.mu.Lock()
		q.totalDequeued++
		if deployment.CreatedAt.After(time.Time{}) {
			waitTime := time.Since(deployment.CreatedAt)
			q.totalWaitTime += waitTime
		}
		// Update oldest if queue is now empty
		if len(q.deployments) == 0 {
			q.oldestEnqueued = time.Time{}
		}
		// Clean up cancel function
		delete(q.cancelMap, deployment.ID)
		q.mu.Unlock()

		return deployment, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("context canceled: %w", ctx.Err())
	}
}

// Close closes the queue
func (q *Queue) Close() {
	q.closeOnce.Do(func() {
		q.mu.Lock()
		defer q.mu.Unlock()

		q.closed = true
		close(q.deployments)

		// Cancel all pending deployments
		for _, cancel := range q.cancelMap {
			cancel()
		}
		q.cancelMap = make(map[string]context.CancelFunc)
	})
}

// Size returns the current number of deployments in the queue
func (q *Queue) Size() int {
	return len(q.deployments)
}

// Capacity returns the queue capacity
func (q *Queue) Capacity() int {
	return cap(q.deployments)
}

// GetMetrics returns queue metrics
func (q *Queue) GetMetrics() interfaces.QueueMetrics {
	q.mu.RLock()
	defer q.mu.RUnlock()

	metrics := interfaces.QueueMetrics{
		TotalEnqueued:    q.totalEnqueued,
		TotalDequeued:    q.totalDequeued,
		CurrentDepth:     len(q.deployments),
		OldestDeployment: q.oldestEnqueued,
	}

	// Calculate average wait time
	if q.totalDequeued > 0 {
		metrics.AverageWaitTime = q.totalWaitTime / time.Duration(q.totalDequeued)
	}

	return metrics
}
