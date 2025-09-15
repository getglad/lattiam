// Package metrics provides metrics collection and monitoring for deployment operations.
package metrics

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/lattiam/lattiam/internal/interfaces"
)

// Collector tracks system metrics
type Collector struct {
	mu sync.RWMutex

	// Counters
	deploymentsQueued    int64
	deploymentsStarted   int64
	deploymentsCompleted int64
	deploymentsFailed    int64
	deploymentsCanceled  int64

	// Timing
	deploymentDurations []time.Duration
	queueWaitTimes      []time.Duration

	// Real-time metrics
	activeWorkers int32
	queueDepth    int32

	// System info
	startTime time.Time

	// Per-deployment tracking
	deploymentStartTimes sync.Map // deploymentID -> time.Time
	deploymentQueueTimes sync.Map // deploymentID -> time.Time
}

// NewCollector creates a new metrics collector
func NewCollector() *Collector {
	return &Collector{
		startTime:           time.Now(),
		deploymentDurations: make([]time.Duration, 0, 1000),
		queueWaitTimes:      make([]time.Duration, 0, 1000),
	}
}

// RecordDeploymentQueued records when a deployment is queued
func (c *Collector) RecordDeploymentQueued(deploymentID string) {
	atomic.AddInt64(&c.deploymentsQueued, 1)
	c.deploymentQueueTimes.Store(deploymentID, time.Now())
}

// RecordDeploymentStarted records when a deployment starts processing
func (c *Collector) RecordDeploymentStarted(deploymentID string) {
	atomic.AddInt64(&c.deploymentsStarted, 1)

	// Calculate queue wait time
	if queueTime, ok := c.deploymentQueueTimes.LoadAndDelete(deploymentID); ok {
		waitTime := time.Since(queueTime.(time.Time))
		c.mu.Lock()
		c.queueWaitTimes = append(c.queueWaitTimes, waitTime)
		// Keep only last 1000 entries to avoid unbounded growth
		if len(c.queueWaitTimes) > 1000 {
			c.queueWaitTimes = c.queueWaitTimes[len(c.queueWaitTimes)-1000:]
		}
		c.mu.Unlock()
	}

	c.deploymentStartTimes.Store(deploymentID, time.Now())
}

// RecordDeploymentCompleted records when a deployment completes successfully
func (c *Collector) RecordDeploymentCompleted(deploymentID string) {
	atomic.AddInt64(&c.deploymentsCompleted, 1)
	c.recordDeploymentDuration(deploymentID)
}

// RecordDeploymentFailed records when a deployment fails
func (c *Collector) RecordDeploymentFailed(deploymentID string) {
	atomic.AddInt64(&c.deploymentsFailed, 1)
	c.recordDeploymentDuration(deploymentID)
}

// RecordDeploymentCanceled records when a deployment is canceled
func (c *Collector) RecordDeploymentCanceled(deploymentID string) {
	atomic.AddInt64(&c.deploymentsCanceled, 1)
	c.deploymentStartTimes.Delete(deploymentID)
	c.deploymentQueueTimes.Delete(deploymentID)
}

// UpdateQueueDepth updates the current queue depth
func (c *Collector) UpdateQueueDepth(depth int) {
	atomic.StoreInt32(&c.queueDepth, int32(depth)) // #nosec G115 - queue depth will never exceed int32 limits
}

// UpdateActiveWorkers updates the number of active workers
func (c *Collector) UpdateActiveWorkers(count int) {
	atomic.StoreInt32(&c.activeWorkers, int32(count)) // #nosec G115 - worker count will never exceed int32 limits
}

// GetSystemMetrics returns current system metrics
func (c *Collector) GetSystemMetrics() interfaces.SystemMetrics {
	processed := atomic.LoadInt64(&c.deploymentsCompleted) +
		atomic.LoadInt64(&c.deploymentsFailed)

	c.mu.RLock()
	avgDeploymentTime := c.calculateAverageDeploymentTimeNoLock()
	c.mu.RUnlock()

	return interfaces.SystemMetrics{
		DeploymentsProcessed:  processed,
		DeploymentsSucceeded:  atomic.LoadInt64(&c.deploymentsCompleted),
		DeploymentsFailed:     atomic.LoadInt64(&c.deploymentsFailed),
		AverageDeploymentTime: avgDeploymentTime,
		CurrentQueueDepth:     int(atomic.LoadInt32(&c.queueDepth)),
		ActiveWorkers:         int(atomic.LoadInt32(&c.activeWorkers)),
		SystemUptime:          time.Since(c.startTime),
	}
}

// GetQueueMetrics returns current queue metrics
func (c *Collector) GetQueueMetrics() interfaces.QueueMetrics {
	c.mu.RLock()
	avgWaitTime := c.calculateAverageQueueWaitTimeNoLock()
	c.mu.RUnlock()

	var oldestTime time.Time
	c.deploymentQueueTimes.Range(func(_, value interface{}) bool {
		queueTime := value.(time.Time)
		if oldestTime.IsZero() || queueTime.Before(oldestTime) {
			oldestTime = queueTime
		}
		return true
	})

	return interfaces.QueueMetrics{
		TotalEnqueued:    atomic.LoadInt64(&c.deploymentsQueued),
		TotalDequeued:    atomic.LoadInt64(&c.deploymentsStarted),
		CurrentDepth:     int(atomic.LoadInt32(&c.queueDepth)),
		AverageWaitTime:  avgWaitTime,
		OldestDeployment: oldestTime,
	}
}

// GetPoolMetrics returns current worker pool metrics
func (c *Collector) GetPoolMetrics() interfaces.PoolMetrics {
	totalJobs := atomic.LoadInt64(&c.deploymentsStarted)
	completedJobs := atomic.LoadInt64(&c.deploymentsCompleted)
	failedJobs := atomic.LoadInt64(&c.deploymentsFailed)

	// Calculate worker utilization (simplified)
	activeWorkers := float64(atomic.LoadInt32(&c.activeWorkers))
	var utilization float64
	if activeWorkers > 0 {
		// Count deployments currently being processed
		var processing int
		c.deploymentStartTimes.Range(func(_, _ interface{}) bool {
			processing++
			return true
		})
		utilization = float64(processing) / activeWorkers
		if utilization > 1.0 {
			utilization = 1.0
		}
	}

	c.mu.RLock()
	avgJobDuration := c.calculateAverageDeploymentTimeNoLock()
	avgQueueWaitTime := c.calculateAverageQueueWaitTimeNoLock()
	c.mu.RUnlock()

	return interfaces.PoolMetrics{
		TotalJobs:          totalJobs,
		CompletedJobs:      completedJobs,
		FailedJobs:         failedJobs,
		AverageJobDuration: avgJobDuration,
		WorkerUtilization:  utilization,
		QueueWaitTime:      avgQueueWaitTime,
	}
}

// recordDeploymentDuration records the duration of a deployment
func (c *Collector) recordDeploymentDuration(deploymentID string) {
	if startTime, ok := c.deploymentStartTimes.LoadAndDelete(deploymentID); ok {
		duration := time.Since(startTime.(time.Time))
		c.mu.Lock()
		c.deploymentDurations = append(c.deploymentDurations, duration)
		// Keep only last 1000 entries
		if len(c.deploymentDurations) > 1000 {
			c.deploymentDurations = c.deploymentDurations[len(c.deploymentDurations)-1000:]
		}
		c.mu.Unlock()
	}
}

// calculateAverageDeploymentTimeNoLock calculates average deployment time without acquiring lock
func (c *Collector) calculateAverageDeploymentTimeNoLock() time.Duration {
	if len(c.deploymentDurations) == 0 {
		return 0
	}

	var total time.Duration
	for _, d := range c.deploymentDurations {
		total += d
	}

	return total / time.Duration(len(c.deploymentDurations))
}

// calculateAverageQueueWaitTimeNoLock calculates average queue wait time without acquiring lock
func (c *Collector) calculateAverageQueueWaitTimeNoLock() time.Duration {
	if len(c.queueWaitTimes) == 0 {
		return 0
	}

	var total time.Duration
	for _, d := range c.queueWaitTimes {
		total += d
	}

	return total / time.Duration(len(c.queueWaitTimes))
}

// Reset resets all metrics (useful for testing)
func (c *Collector) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	atomic.StoreInt64(&c.deploymentsQueued, 0)
	atomic.StoreInt64(&c.deploymentsStarted, 0)
	atomic.StoreInt64(&c.deploymentsCompleted, 0)
	atomic.StoreInt64(&c.deploymentsFailed, 0)
	atomic.StoreInt64(&c.deploymentsCanceled, 0)
	atomic.StoreInt32(&c.queueDepth, 0)
	atomic.StoreInt32(&c.activeWorkers, 0)

	c.deploymentDurations = c.deploymentDurations[:0]
	c.queueWaitTimes = c.queueWaitTimes[:0]
	c.startTime = time.Now()

	// Clear maps
	c.deploymentStartTimes.Range(func(key, _ interface{}) bool {
		c.deploymentStartTimes.Delete(key)
		return true
	})
	c.deploymentQueueTimes.Range(func(key, _ interface{}) bool {
		c.deploymentQueueTimes.Delete(key)
		return true
	})
}
