package metrics

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCollector_RecordDeploymentLifecycle(t *testing.T) {
	t.Parallel()
	c := NewCollector()

	// Record deployment queued
	c.RecordDeploymentQueued("deploy1")

	metrics := c.GetSystemMetrics()
	assert.Equal(t, int64(0), metrics.DeploymentsProcessed)

	queueMetrics := c.GetQueueMetrics()
	assert.Equal(t, int64(1), queueMetrics.TotalEnqueued)
	assert.Equal(t, int64(0), queueMetrics.TotalDequeued)

	// Record deployment started
	time.Sleep(10 * time.Millisecond) // Ensure some queue wait time
	c.RecordDeploymentStarted("deploy1")

	queueMetrics = c.GetQueueMetrics()
	assert.Equal(t, int64(1), queueMetrics.TotalDequeued)
	assert.Greater(t, queueMetrics.AverageWaitTime, time.Duration(0))

	// Record deployment completed
	time.Sleep(20 * time.Millisecond) // Ensure some processing time
	c.RecordDeploymentCompleted("deploy1")

	metrics = c.GetSystemMetrics()
	assert.Equal(t, int64(1), metrics.DeploymentsProcessed)
	assert.Equal(t, int64(1), metrics.DeploymentsSucceeded)
	assert.Equal(t, int64(0), metrics.DeploymentsFailed)
	assert.Greater(t, metrics.AverageDeploymentTime, time.Duration(0))
}

func TestCollector_RecordDeploymentFailed(t *testing.T) {
	t.Parallel()
	c := NewCollector()

	c.RecordDeploymentQueued("deploy1")
	c.RecordDeploymentStarted("deploy1")
	c.RecordDeploymentFailed("deploy1")

	metrics := c.GetSystemMetrics()
	assert.Equal(t, int64(1), metrics.DeploymentsProcessed)
	assert.Equal(t, int64(0), metrics.DeploymentsSucceeded)
	assert.Equal(t, int64(1), metrics.DeploymentsFailed)
}

func TestCollector_RecordDeploymentCanceled(t *testing.T) {
	t.Parallel()
	c := NewCollector()

	c.RecordDeploymentQueued("deploy1")
	c.RecordDeploymentCanceled("deploy1")

	// Canceled deployments should not affect processed count
	metrics := c.GetSystemMetrics()
	assert.Equal(t, int64(0), metrics.DeploymentsProcessed)
	assert.Equal(t, int64(0), metrics.DeploymentsSucceeded)
	assert.Equal(t, int64(0), metrics.DeploymentsFailed)
}

func TestCollector_QueueDepthAndActiveWorkers(t *testing.T) {
	t.Parallel()
	c := NewCollector()

	c.UpdateQueueDepth(5)
	c.UpdateActiveWorkers(3)

	metrics := c.GetSystemMetrics()
	assert.Equal(t, 5, metrics.CurrentQueueDepth)
	assert.Equal(t, 3, metrics.ActiveWorkers)
}

func TestCollector_AverageCalculations(t *testing.T) {
	t.Parallel()
	c := NewCollector()

	// Process multiple deployments with different times
	for i := 0; i < 5; i++ {
		deployID := fmt.Sprintf("deploy%d", i)
		c.RecordDeploymentQueued(deployID)
		time.Sleep(10 * time.Millisecond)
		c.RecordDeploymentStarted(deployID)
		time.Sleep(20 * time.Millisecond)
		if i%2 == 0 {
			c.RecordDeploymentCompleted(deployID)
		} else {
			c.RecordDeploymentFailed(deployID)
		}
	}

	metrics := c.GetSystemMetrics()
	assert.Equal(t, int64(5), metrics.DeploymentsProcessed)
	assert.Equal(t, int64(3), metrics.DeploymentsSucceeded)
	assert.Equal(t, int64(2), metrics.DeploymentsFailed)
	assert.Greater(t, metrics.AverageDeploymentTime, 15*time.Millisecond)

	queueMetrics := c.GetQueueMetrics()
	assert.Greater(t, queueMetrics.AverageWaitTime, 5*time.Millisecond)
}

func TestCollector_PoolMetrics(t *testing.T) {
	t.Parallel()
	c := NewCollector()

	// Set up some deployments
	c.RecordDeploymentQueued("deploy1")
	c.RecordDeploymentQueued("deploy2")
	c.RecordDeploymentStarted("deploy1")
	c.RecordDeploymentStarted("deploy2")

	c.UpdateActiveWorkers(2)

	poolMetrics := c.GetPoolMetrics()
	assert.Equal(t, int64(2), poolMetrics.TotalJobs)
	assert.InEpsilon(t, float64(1.0), poolMetrics.WorkerUtilization, 0.01) // 2 jobs / 2 workers

	// Complete one deployment
	c.RecordDeploymentCompleted("deploy1")

	poolMetrics = c.GetPoolMetrics()
	assert.Equal(t, int64(1), poolMetrics.CompletedJobs)
	assert.InEpsilon(t, float64(0.5), poolMetrics.WorkerUtilization, 0.01) // 1 job / 2 workers
}

func TestCollector_SystemUptime(t *testing.T) {
	t.Parallel()
	c := NewCollector()

	time.Sleep(100 * time.Millisecond)

	metrics := c.GetSystemMetrics()
	assert.Greater(t, metrics.SystemUptime, 90*time.Millisecond)
	assert.Less(t, metrics.SystemUptime, 200*time.Millisecond)
}

func TestCollector_OldestDeployment(t *testing.T) {
	t.Parallel()
	c := NewCollector()

	now := time.Now()
	c.RecordDeploymentQueued("deploy1")
	time.Sleep(50 * time.Millisecond)
	c.RecordDeploymentQueued("deploy2")

	queueMetrics := c.GetQueueMetrics()
	assert.WithinDuration(t, now, queueMetrics.OldestDeployment, 10*time.Millisecond)

	// Start processing the oldest
	c.RecordDeploymentStarted("deploy1")

	queueMetrics = c.GetQueueMetrics()
	assert.WithinDuration(t, now.Add(50*time.Millisecond), queueMetrics.OldestDeployment, 10*time.Millisecond)
}

func TestCollector_Reset(t *testing.T) {
	t.Parallel()
	c := NewCollector()

	// Add some data
	c.RecordDeploymentQueued("deploy1")
	c.RecordDeploymentStarted("deploy1")
	c.RecordDeploymentCompleted("deploy1")
	c.UpdateQueueDepth(5)
	c.UpdateActiveWorkers(3)

	// Reset
	c.Reset()

	// Verify everything is reset
	metrics := c.GetSystemMetrics()
	assert.Equal(t, int64(0), metrics.DeploymentsProcessed)
	assert.Equal(t, int64(0), metrics.DeploymentsSucceeded)
	assert.Equal(t, int64(0), metrics.DeploymentsFailed)
	assert.Equal(t, 0, metrics.CurrentQueueDepth)
	assert.Equal(t, 0, metrics.ActiveWorkers)
	assert.Equal(t, time.Duration(0), metrics.AverageDeploymentTime)

	queueMetrics := c.GetQueueMetrics()
	assert.Equal(t, int64(0), queueMetrics.TotalEnqueued)
	assert.Equal(t, int64(0), queueMetrics.TotalDequeued)
	assert.True(t, queueMetrics.OldestDeployment.IsZero())
}

func TestCollector_ConcurrentAccess(t *testing.T) {
	t.Parallel()
	c := NewCollector()

	// Test concurrent access without strict timing requirements
	// This tests that the collector is thread-safe

	var wg sync.WaitGroup
	const numGoroutines = 5
	const opsPerGoroutine = 10

	// Launch multiple goroutines that perform various operations
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			for j := 0; j < opsPerGoroutine; j++ {
				deployID := fmt.Sprintf("deploy-%d-%d", id, j)

				// Queue deployment
				c.RecordDeploymentQueued(deployID)

				// Start deployment
				c.RecordDeploymentStarted(deployID)

				// Randomly complete or fail
				if (id+j)%2 == 0 {
					c.RecordDeploymentCompleted(deployID)
				} else {
					c.RecordDeploymentFailed(deployID)
				}

				// Update metrics
				c.UpdateActiveWorkers(id)
				c.UpdateQueueDepth(j)

				// Read metrics
				c.GetSystemMetrics()
				c.GetQueueMetrics()
				c.GetPoolMetrics()
			}
		}(i)
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// Verify metrics are consistent
	metrics := c.GetSystemMetrics()
	totalOps := int64(numGoroutines * opsPerGoroutine)

	// All deployments should be processed
	assert.Equal(t, totalOps, metrics.DeploymentsProcessed)

	// Succeeded + Failed should equal total processed
	assert.Equal(t, totalOps, metrics.DeploymentsSucceeded+metrics.DeploymentsFailed)

	// Queue metrics should be consistent
	queueMetrics := c.GetQueueMetrics()
	assert.Equal(t, totalOps, queueMetrics.TotalEnqueued)
	assert.Equal(t, totalOps, queueMetrics.TotalDequeued)
}

func TestCollector_MemoryBoundedStorage(t *testing.T) {
	t.Parallel()
	c := NewCollector()

	// Add more than 1000 deployments
	for i := 0; i < 1500; i++ {
		deployID := fmt.Sprintf("deploy%d", i)
		c.RecordDeploymentQueued(deployID)
		c.RecordDeploymentStarted(deployID)
		c.RecordDeploymentCompleted(deployID)
	}

	// Verify metrics are still calculated correctly
	metrics := c.GetSystemMetrics()
	assert.Greater(t, metrics.AverageDeploymentTime, time.Duration(0))

	queueMetrics := c.GetQueueMetrics()
	assert.Greater(t, queueMetrics.AverageWaitTime, time.Duration(0))

	// The internal storage should be bounded
	// We can't directly test this without exposing internals,
	// but the test should not consume excessive memory
}
