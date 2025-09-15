package monitor

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/infra/embedded"
	"github.com/lattiam/lattiam/internal/interfaces"
)

func TestOrphanMonitor_StaleProcessingDetection(t *testing.T) {
	t.Parallel()
	// Create components
	tracker := embedded.NewTracker()
	queue := embedded.NewQueue(100)

	// Create monitor with short intervals for testing
	monitor := NewOrphanMonitor(Config{
		Queue:            queue,
		Tracker:          tracker,
		ScanInterval:     100 * time.Millisecond,
		StaleThreshold:   200 * time.Millisecond,
		ReconcileOrphans: true,
	})

	// Create a deployment and mark it as processing
	deployment := &interfaces.QueuedDeployment{
		ID:        "stale-test-1",
		Status:    interfaces.DeploymentStatusQueued,
		CreatedAt: time.Now(),
		Request:   &interfaces.DeploymentRequest{},
	}

	err := tracker.Register(deployment)
	require.NoError(t, err)

	// Mark as processing
	err = tracker.SetStatus(deployment.ID, interfaces.DeploymentStatusProcessing)
	require.NoError(t, err)

	// Start monitor
	err = monitor.Start()
	require.NoError(t, err)
	defer func() {
		_ = monitor.Stop(context.Background())
	}()

	// Wait for stale threshold + scan interval
	time.Sleep(400 * time.Millisecond)

	// Check that deployment was marked as failed
	status, err := tracker.GetStatus(deployment.ID)
	require.NoError(t, err)
	assert.Equal(t, interfaces.DeploymentStatusFailed, *status)

	// Check stats
	stats := monitor.GetStats()
	assert.True(t, stats.Running)
	// OrphanCount may be 0 if scan just completed, check that LastScan is recent
	assert.WithinDuration(t, time.Now(), stats.LastScan, 500*time.Millisecond)
}

func TestOrphanMonitor_StartStop(t *testing.T) {
	t.Parallel()
	monitor := NewOrphanMonitor(Config{
		Queue:        embedded.NewQueue(10),
		Tracker:      embedded.NewTracker(),
		ScanInterval: 1 * time.Second,
	})

	// Start monitor
	err := monitor.Start()
	require.NoError(t, err)

	// Check it's running
	stats := monitor.GetStats()
	assert.True(t, stats.Running)

	// Try to start again - should error
	err = monitor.Start()
	require.Error(t, err)

	// Stop monitor
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err = monitor.Stop(ctx)
	require.NoError(t, err)

	// Check it's stopped
	stats = monitor.GetStats()
	assert.False(t, stats.Running)

	// Stop again - should be no-op
	err = monitor.Stop(context.Background())
	require.NoError(t, err)
}

func TestOrphanMonitor_InitialScan(t *testing.T) {
	t.Parallel()
	tracker := embedded.NewTracker()
	queue := embedded.NewQueue(100)

	// Create stale processing deployment before starting monitor
	deployment := &interfaces.QueuedDeployment{
		ID:        "pre-existing-stale",
		Status:    interfaces.DeploymentStatusQueued,
		CreatedAt: time.Now().Add(-1 * time.Hour), // Old deployment
		Request:   &interfaces.DeploymentRequest{},
	}

	err := tracker.Register(deployment)
	require.NoError(t, err)

	// Mark as processing
	err = tracker.SetStatus(deployment.ID, interfaces.DeploymentStatusProcessing)
	require.NoError(t, err)

	// Wait a bit to ensure StartedAt is in the past
	time.Sleep(10 * time.Millisecond)

	monitor := NewOrphanMonitor(Config{
		Queue:            queue,
		Tracker:          tracker,
		ScanInterval:     1 * time.Minute,      // Long interval
		StaleThreshold:   5 * time.Millisecond, // Short threshold (less than our wait time)
		ReconcileOrphans: true,
	})

	// Start monitor
	err = monitor.Start()
	require.NoError(t, err)
	defer func() {
		_ = monitor.Stop(context.Background())
	}()

	// Initial scan should happen immediately
	time.Sleep(100 * time.Millisecond)

	// Check that deployment was marked as failed
	status, err := tracker.GetStatus(deployment.ID)
	require.NoError(t, err)
	assert.Equal(t, interfaces.DeploymentStatusFailed, *status)
}

func TestOrphanMonitor_NoReconciliation(t *testing.T) {
	t.Parallel()
	tracker := embedded.NewTracker()
	queue := embedded.NewQueue(100)

	// Create monitor with reconciliation disabled
	monitor := NewOrphanMonitor(Config{
		Queue:            queue,
		Tracker:          tracker,
		ScanInterval:     50 * time.Millisecond,
		StaleThreshold:   10 * time.Millisecond,
		ReconcileOrphans: false, // Disabled
	})

	// Create deployment that will become stale
	deployment := &interfaces.QueuedDeployment{
		ID:        "no-reconcile-test",
		Status:    interfaces.DeploymentStatusQueued,
		CreatedAt: time.Now(),
		Request:   &interfaces.DeploymentRequest{},
	}

	err := tracker.Register(deployment)
	require.NoError(t, err)

	// Mark as processing - this sets StartedAt to current time
	err = tracker.SetStatus(deployment.ID, interfaces.DeploymentStatusProcessing)
	require.NoError(t, err)

	// Wait for deployment to become stale (longer than StaleThreshold)
	time.Sleep(20 * time.Millisecond)

	// Start monitor
	err = monitor.Start()
	require.NoError(t, err)
	defer func() {
		_ = monitor.Stop(context.Background())
	}()

	// Wait for at least one scan cycle to complete
	time.Sleep(100 * time.Millisecond)

	// Status should still be processing (no reconciliation)
	status, err := tracker.GetStatus(deployment.ID)
	require.NoError(t, err)
	assert.Equal(t, interfaces.DeploymentStatusProcessing, *status)

	// But orphan should be detected (scan should have found the stale deployment)
	stats := monitor.GetStats()
	assert.Positive(t, stats.OrphanCount, "Expected to detect stale deployment as orphan even without reconciliation")
}

func TestOrphanMonitor_TrackerOrphanRequeue(t *testing.T) {
	t.Parallel()
	tracker := embedded.NewTracker()
	queue := embedded.NewQueue(100)

	// For embedded mode, we can't really test tracker orphans without queue inspection
	// This would require distributed mode with asynq Inspector
	// We'll test the re-queue logic instead

	monitor := NewOrphanMonitor(Config{
		Queue:            queue,
		Tracker:          tracker,
		ScanInterval:     100 * time.Millisecond,
		ReconcileOrphans: true,
	})

	// Create deployment marked as queued but not actually in queue
	deployment := &interfaces.QueuedDeployment{
		ID:        "requeue-test",
		Status:    interfaces.DeploymentStatusQueued,
		CreatedAt: time.Now(),
		Request:   &interfaces.DeploymentRequest{},
	}

	err := tracker.Register(deployment)
	require.NoError(t, err)

	// Manually call the handler (since we can't trigger it without Inspector)
	monitor.handleTrackerOrphan(deployment)

	// Should be in queue now
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	dequeued, err := queue.Dequeue(ctx)
	require.NoError(t, err)
	assert.Equal(t, deployment.ID, dequeued.ID)
}

func TestOrphanMonitor_ConcurrentScans(t *testing.T) {
	t.Parallel()
	tracker := embedded.NewTracker()
	queue := embedded.NewQueue(100)

	monitor := NewOrphanMonitor(Config{
		Queue:            queue,
		Tracker:          tracker,
		ScanInterval:     50 * time.Millisecond,
		StaleThreshold:   1 * time.Millisecond,
		ReconcileOrphans: true,
	})

	// Create multiple stale deployments
	for i := 0; i < 10; i++ {
		deployment := &interfaces.QueuedDeployment{
			ID:        fmt.Sprintf("concurrent-%d", i),
			Status:    interfaces.DeploymentStatusQueued,
			CreatedAt: time.Now(),
			Request:   &interfaces.DeploymentRequest{},
		}

		err := tracker.Register(deployment)
		require.NoError(t, err)

		err = tracker.SetStatus(deployment.ID, interfaces.DeploymentStatusProcessing)
		require.NoError(t, err)
	}

	// Start monitor
	err := monitor.Start()
	require.NoError(t, err)

	// Let multiple scans happen
	time.Sleep(200 * time.Millisecond)

	// Stop monitor
	err = monitor.Stop(context.Background())
	require.NoError(t, err)

	// All deployments should be marked as failed
	for i := 0; i < 10; i++ {
		status, err := tracker.GetStatus(fmt.Sprintf("concurrent-%d", i))
		require.NoError(t, err)
		assert.Equal(t, interfaces.DeploymentStatusFailed, *status)
	}
}

//nolint:funlen // Comprehensive exponential backoff test with timing verification
func TestOrphanMonitor_ExponentialBackoff(t *testing.T) {
	t.Parallel()
	tracker := embedded.NewTracker()
	queue := embedded.NewQueue(100)

	// Create monitor with short intervals for testing
	baseInterval := 50 * time.Millisecond
	maxBackoff := 400 * time.Millisecond
	monitor := NewOrphanMonitor(Config{
		Queue:            queue,
		Tracker:          tracker,
		ScanInterval:     baseInterval,
		StaleThreshold:   1 * time.Hour, // High threshold so nothing is stale
		ReconcileOrphans: true,
		MaxBackoff:       maxBackoff,
	})

	// Track scan times by monitoring stats changes
	var scanTimes []time.Time
	var scanTimesMu sync.Mutex
	var lastScanTime time.Time

	// Start monitor
	err := monitor.Start()
	require.NoError(t, err)
	defer func() {
		_ = monitor.Stop(context.Background())
	}()

	// Monitor scan completions by checking stats
	go func() {
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()

		for {
			stats := monitor.GetStats()
			if !stats.LastScan.IsZero() && stats.LastScan.After(lastScanTime) {
				scanTimesMu.Lock()
				scanTimes = append(scanTimes, stats.LastScan)
				lastScanTime = stats.LastScan
				scanTimesMu.Unlock()
			}

			<-ticker.C
		}
	}()

	// Wait for several scans with no orphans
	// Should see increasing intervals: 50ms, 100ms, 150ms, 225ms, 337ms (capped at 400ms)
	time.Sleep(1200 * time.Millisecond)

	scanTimesMu.Lock()
	scans := len(scanTimes)
	scanTimesCopy := make([]time.Time, len(scanTimes))
	copy(scanTimesCopy, scanTimes)
	scanTimesMu.Unlock()

	// Should have at least 4 scans
	require.GreaterOrEqual(t, scans, 4, "Should have completed at least 4 scans")

	// Verify intervals are increasing (with some tolerance for timing)
	for i := 1; i < len(scanTimesCopy)-1; i++ {
		interval := scanTimesCopy[i+1].Sub(scanTimesCopy[i])
		prevInterval := scanTimesCopy[i].Sub(scanTimesCopy[i-1])

		// Each interval should be larger than the previous (with 20ms tolerance)
		assert.Greater(t, interval+20*time.Millisecond, prevInterval,
			"Interval %d (%v) should be greater than interval %d (%v)",
			i+1, interval, i, prevInterval)
	}

	// Now create an orphan to trigger reset
	deployment := &interfaces.QueuedDeployment{
		ID:        "reset-backoff-test",
		Status:    interfaces.DeploymentStatusQueued,
		CreatedAt: time.Now().Add(-2 * time.Hour), // Old enough to be stale
		Request:   &interfaces.DeploymentRequest{},
	}

	err = tracker.Register(deployment)
	require.NoError(t, err)

	err = tracker.SetStatus(deployment.ID, interfaces.DeploymentStatusProcessing)
	require.NoError(t, err)

	// Update stale threshold to make this deployment stale
	monitor.mu.Lock()
	monitor.staleThreshold = 1 * time.Millisecond
	monitor.mu.Unlock()

	// Record the scan time before the deployment will be detected
	scanTimesMu.Lock()
	_ = len(scanTimes) // Just to mark the point where we add the deployment
	scanTimesMu.Unlock()

	// Create a channel to signal when we've detected the reset
	resetDetected := make(chan bool, 1)

	// Monitor for the reset
	go func() {
		for {
			monitor.mu.RLock()
			interval := monitor.currentInterval
			multiplier := monitor.backoffMultiplier
			monitor.mu.RUnlock()

			// If we've reset to base, signal and exit
			if interval == baseInterval && multiplier == 1.0 {
				select {
				case resetDetected <- true:
				default:
				}
				return
			}

			time.Sleep(10 * time.Millisecond)
		}
	}()

	// Wait for the orphan to be detected and interval to reset
	select {
	case <-resetDetected:
		// Good, reset detected
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for interval reset")
	}

	// Verify the deployment was marked as failed
	status, err := tracker.GetStatus(deployment.ID)
	require.NoError(t, err)
	assert.Equal(t, interfaces.DeploymentStatusFailed, *status)
}

func TestOrphanMonitor_BackoffWithMaximum(t *testing.T) {
	t.Parallel()
	tracker := embedded.NewTracker()
	queue := embedded.NewQueue(100)

	// Create monitor with very short max backoff
	baseInterval := 20 * time.Millisecond
	maxBackoff := 50 * time.Millisecond
	monitor := NewOrphanMonitor(Config{
		Queue:            queue,
		Tracker:          tracker,
		ScanInterval:     baseInterval,
		StaleThreshold:   1 * time.Hour, // High threshold so nothing is stale
		ReconcileOrphans: true,
		MaxBackoff:       maxBackoff,
	})

	// Start monitor
	err := monitor.Start()
	require.NoError(t, err)
	defer func() {
		_ = monitor.Stop(context.Background())
	}()

	// Wait for enough scans to hit max backoff
	// First interval: 20ms, then 40ms, then 50ms (capped)
	time.Sleep(200 * time.Millisecond)

	// Get current interval - should be at max
	monitor.mu.RLock()
	currentInterval := monitor.currentInterval
	monitor.mu.RUnlock()

	assert.Equal(t, maxBackoff, currentInterval, "Should be at max backoff")
}

//nolint:funlen // Test requires comprehensive backoff scenarios
func TestOrphanMonitor_BackoffResetOnOrphans(t *testing.T) {
	t.Parallel()
	tracker := embedded.NewTracker()
	queue := embedded.NewQueue(100)

	baseInterval := 50 * time.Millisecond
	monitor := NewOrphanMonitor(Config{
		Queue:            queue,
		Tracker:          tracker,
		ScanInterval:     baseInterval,
		StaleThreshold:   100 * time.Millisecond,
		ReconcileOrphans: true,
		MaxBackoff:       500 * time.Millisecond,
	})

	// Start monitor
	err := monitor.Start()
	require.NoError(t, err)
	defer func() {
		_ = monitor.Stop(context.Background())
	}()

	// Wait for backoff to increase (no orphans)
	time.Sleep(200 * time.Millisecond)

	// Check interval has increased
	monitor.mu.RLock()
	increasedInterval := monitor.currentInterval
	monitor.mu.RUnlock()
	assert.Greater(t, increasedInterval, baseInterval, "Interval should have increased")

	// Create a deployment that will become stale
	deployment := &interfaces.QueuedDeployment{
		ID:        "backoff-reset-test",
		Status:    interfaces.DeploymentStatusQueued,
		CreatedAt: time.Now(),
		Request:   &interfaces.DeploymentRequest{},
	}

	err = tracker.Register(deployment)
	require.NoError(t, err)

	// Mark deployment as processing to make it stale
	err = tracker.SetStatus(deployment.ID, interfaces.DeploymentStatusProcessing)
	require.NoError(t, err)

	// Create a channel to signal when we've detected the reset
	resetDetected := make(chan bool, 1)

	// Monitor for the reset
	go func() {
		for {
			monitor.mu.RLock()
			interval := monitor.currentInterval
			multiplier := monitor.backoffMultiplier
			monitor.mu.RUnlock()

			// Check if deployment was marked as failed
			status, _ := tracker.GetStatus(deployment.ID)
			if status != nil && *status == interfaces.DeploymentStatusFailed {
				// If we've reset to base, signal and exit
				if interval == baseInterval && multiplier == 1.0 {
					select {
					case resetDetected <- true:
					default:
					}
					return
				}
			}

			time.Sleep(10 * time.Millisecond)
		}
	}()

	// Wait for the orphan to be detected and interval to reset
	select {
	case <-resetDetected:
		// Good, reset detected
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for interval reset after orphan detection")
	}
}

func TestOrphanMonitor_DefaultMaxBackoff(t *testing.T) {
	t.Parallel()
	tracker := embedded.NewTracker()
	queue := embedded.NewQueue(100)

	// Create monitor without specifying MaxBackoff
	baseInterval := 10 * time.Millisecond
	monitor := NewOrphanMonitor(Config{
		Queue:            queue,
		Tracker:          tracker,
		ScanInterval:     baseInterval,
		StaleThreshold:   1 * time.Hour,
		ReconcileOrphans: true,
		// MaxBackoff not specified - should default to 10x base
	})

	// Check that max backoff was set to 10x base interval
	monitor.mu.RLock()
	assert.Equal(t, baseInterval*10, monitor.maxBackoff)
	monitor.mu.RUnlock()
}
