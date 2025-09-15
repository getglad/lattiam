// Package monitor provides monitoring and reconciliation services for deployment queue operations.
package monitor

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/hibiken/asynq"

	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/internal/logging"
	"github.com/lattiam/lattiam/internal/metrics"
)

// OrphanMonitor monitors for orphaned jobs and reconciles them
type OrphanMonitor struct {
	queue     interfaces.DeploymentQueue
	tracker   interfaces.DeploymentTracker
	inspector *asynq.Inspector
	metrics   *metrics.Collector
	logger    *logging.Logger

	// Configuration
	scanInterval     time.Duration
	staleThreshold   time.Duration
	reconcileOrphans bool
	maxBackoff       time.Duration // Maximum scan interval with backoff

	// State
	mu                sync.RWMutex
	running           bool
	ctx               context.Context
	cancel            context.CancelFunc
	wg                sync.WaitGroup
	lastScan          time.Time
	orphanCount       int
	currentInterval   time.Duration // Current scan interval with backoff
	backoffMultiplier float64       // Current backoff multiplier
}

// Config holds configuration for the orphan monitor
type Config struct {
	Queue            interfaces.DeploymentQueue
	Tracker          interfaces.DeploymentTracker
	Inspector        *asynq.Inspector // For distributed mode
	Metrics          *metrics.Collector
	ScanInterval     time.Duration
	StaleThreshold   time.Duration // How long before "processing" is considered stale
	ReconcileOrphans bool          // Whether to automatically fix orphans
	MaxBackoff       time.Duration // Maximum scan interval when no orphans found (0 = 10x base interval)
}

// NewOrphanMonitor creates a new orphan monitor
func NewOrphanMonitor(cfg Config) *OrphanMonitor {
	if cfg.ScanInterval == 0 {
		cfg.ScanInterval = 1 * time.Minute
	}
	if cfg.StaleThreshold == 0 {
		cfg.StaleThreshold = 10 * time.Minute
	}
	if cfg.MaxBackoff == 0 {
		// Default to 10x the base interval
		cfg.MaxBackoff = cfg.ScanInterval * 10
	}

	return &OrphanMonitor{
		queue:             cfg.Queue,
		tracker:           cfg.Tracker,
		inspector:         cfg.Inspector,
		metrics:           cfg.Metrics,
		logger:            logging.NewLogger("OrphanMonitor"),
		scanInterval:      cfg.ScanInterval,
		staleThreshold:    cfg.StaleThreshold,
		reconcileOrphans:  cfg.ReconcileOrphans,
		maxBackoff:        cfg.MaxBackoff,
		currentInterval:   cfg.ScanInterval, // Start with base interval
		backoffMultiplier: 1.0,              // Start with no backoff
	}
}

// Start begins monitoring for orphaned jobs
func (m *OrphanMonitor) Start() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return fmt.Errorf("monitor already running")
	}

	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.running = true

	m.wg.Add(1)
	go m.monitorLoop()

	m.logger.Infof("Started with scan interval %v, stale threshold %v",
		m.scanInterval, m.staleThreshold)

	return nil
}

// Stop stops the monitor
func (m *OrphanMonitor) Stop(ctx context.Context) error {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return nil
	}
	m.running = false
	m.cancel()
	m.mu.Unlock()

	// Wait for monitor loop to finish or context to expire
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("monitor shutdown timeout: %w", ctx.Err())
	}
}

// GetStats returns current monitoring statistics
func (m *OrphanMonitor) GetStats() Stats {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return Stats{
		Running:     m.running,
		LastScan:    m.lastScan,
		OrphanCount: m.orphanCount,
	}
}

// monitorLoop is the main monitoring loop
func (m *OrphanMonitor) monitorLoop() {
	defer m.wg.Done()

	// Initial scan
	m.performScan()

	for {
		// Get current interval with lock
		m.mu.RLock()
		interval := m.currentInterval
		m.mu.RUnlock()

		// Use a timer instead of ticker for dynamic intervals
		timer := time.NewTimer(interval)

		select {
		case <-m.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			m.performScan()
		}
	}
}

// performScan checks for and handles orphaned jobs
func (m *OrphanMonitor) performScan() {
	startTime := time.Now()
	m.logger.Debugf("Starting orphan scan")

	var orphansFound int

	// Check for jobs in queue but not in tracker (distributed mode only)
	if m.inspector != nil {
		orphansFound += m.scanQueueOrphans()
	}

	// Check for stale processing jobs
	orphansFound += m.scanStaleProcessing()

	// Check for tracker entries without queue jobs
	orphansFound += m.scanTrackerOrphans()

	// Update stats and adjust backoff
	m.mu.Lock()
	m.lastScan = time.Now()
	m.orphanCount = orphansFound

	// Adjust backoff based on whether orphans were found
	if orphansFound == 0 {
		// No orphans - increase interval up to max
		oldInterval := m.currentInterval
		if m.backoffMultiplier < 2.0 {
			m.backoffMultiplier = 2.0
		} else {
			m.backoffMultiplier *= 1.5 // Slower increase after first doubling
		}

		newInterval := time.Duration(float64(m.scanInterval) * m.backoffMultiplier)
		if newInterval > m.maxBackoff {
			newInterval = m.maxBackoff
		}
		m.currentInterval = newInterval

		// Only log if interval actually changed
		if oldInterval != m.currentInterval {
			m.logger.Infof("No orphans found, increasing scan interval to %v (multiplier: %.1f)",
				m.currentInterval, m.backoffMultiplier)
		}
	} else {
		// Orphans found - reset to base interval
		if m.backoffMultiplier > 1.0 {
			m.logger.Infof("Orphans found, resetting scan interval to base %v", m.scanInterval)
		}
		m.backoffMultiplier = 1.0
		m.currentInterval = m.scanInterval
	}
	m.mu.Unlock()

	m.logger.Infof("Scan complete in %v, found %d orphans",
		time.Since(startTime), orphansFound)
}

// scanQueueOrphans finds jobs in queue but not in tracker
func (m *OrphanMonitor) scanQueueOrphans() int {
	if m.inspector == nil {
		return 0
	}

	orphansFound := 0

	// Check all queues (only check queues that exist)
	queues := []string{"deployments"} // Primary queue
	// Add other queues if they're being used
	for _, queueName := range queues {
		tasks, err := m.inspector.ListPendingTasks(queueName)
		if err != nil {
			m.logger.Errorf("Error listing pending tasks in %s: %v", queueName, err)
			continue
		}

		for _, task := range tasks {
			// Check if task exists in tracker
			_, err := m.tracker.GetStatus(task.ID)
			if err != nil {
				// This is an orphan - job in queue but not in tracker
				orphansFound++
				m.logger.Warnf("Found orphaned job in queue: %s", task.ID)

				if m.reconcileOrphans {
					m.handleQueueOrphan(task.ID, queueName)
				}
			}
		}
	}

	return orphansFound
}

// scanStaleProcessing finds jobs stuck in processing state
func (m *OrphanMonitor) scanStaleProcessing() int {
	orphansFound := 0

	// Get stale threshold with lock
	m.mu.RLock()
	staleThreshold := m.staleThreshold
	m.mu.RUnlock()

	// Get all processing deployments
	deployments, err := m.tracker.List(interfaces.DeploymentFilter{
		Status: []interfaces.DeploymentStatus{interfaces.DeploymentStatusProcessing},
	})
	if err != nil {
		m.logger.Errorf("Error listing processing deployments: %v", err)
		return 0
	}

	now := time.Now()
	for _, deployment := range deployments {
		if deployment.StartedAt != nil {
			processingTime := now.Sub(*deployment.StartedAt)
			if processingTime > staleThreshold {
				orphansFound++
				m.logger.Warnf("Found stale processing job: %s (processing for %v)",
					deployment.ID, processingTime)

				if m.reconcileOrphans {
					m.handleStaleProcessing(deployment)
				}
			}
		}
	}

	return orphansFound
}

// scanTrackerOrphans finds tracker entries without corresponding queue jobs
func (m *OrphanMonitor) scanTrackerOrphans() int {
	if m.inspector == nil {
		return 0 // Can't check without inspector
	}

	orphansFound := 0

	// Get all queued deployments from tracker
	deployments, err := m.tracker.List(interfaces.DeploymentFilter{
		Status: []interfaces.DeploymentStatus{interfaces.DeploymentStatusQueued},
	})
	if err != nil {
		m.logger.Errorf("Error listing queued deployments: %v", err)
		return 0
	}

	for _, deployment := range deployments {
		// Check if job exists in any queue
		found := false
		for _, queueName := range []string{"deployments"} {
			_, err := m.inspector.GetTaskInfo(queueName, deployment.ID)
			if err == nil {
				found = true
				break
			}
		}

		if !found {
			orphansFound++
			m.logger.Warnf("Found tracker orphan (not in queue): %s", deployment.ID)

			if m.reconcileOrphans {
				m.handleTrackerOrphan(deployment)
			}
		}
	}

	return orphansFound
}

// handleQueueOrphan handles a job that's in queue but not in tracker
func (m *OrphanMonitor) handleQueueOrphan(jobID, queueName string) {
	m.logger.Infof("Handling queue orphan: %s", jobID)

	// Option 1: Remove from queue (if we can't reconstruct the deployment)
	// Note: This requires queue to support removal by ID, which our interface doesn't have

	// Option 2: Log for manual intervention
	m.logger.Warnf("Job %s in queue %s has no tracker entry. Manual intervention required.",
		jobID, queueName)

	// Track metric if available
	// TODO: Add specific orphan metric when metrics interface is extended
}

// handleStaleProcessing handles a job stuck in processing state
func (m *OrphanMonitor) handleStaleProcessing(deployment *interfaces.QueuedDeployment) {
	m.logger.Infof("Handling stale processing job: %s", deployment.ID)

	// Mark as failed
	err := m.tracker.SetStatus(deployment.ID, interfaces.DeploymentStatusFailed)
	if err != nil {
		m.logger.Errorf("Error marking stale job as failed: %v", err)
		return
	}

	// Log error message (tracker doesn't support SetError)
	m.logger.Warnf("Job %s stuck in processing state for %v",
		deployment.ID, time.Since(*deployment.StartedAt))

	// Re-queue if it has retries left
	if deployment.Request != nil && deployment.Request.Options.MaxRetries > 0 {
		// Note: Our current interface doesn't support re-queueing with retry count
		m.logger.Warnf("Job %s should be retried but interface doesn't support re-queueing",
			deployment.ID)
	}
}

// handleTrackerOrphan handles a tracker entry without a queue job
func (m *OrphanMonitor) handleTrackerOrphan(deployment *interfaces.QueuedDeployment) {
	m.logger.Infof("Handling tracker orphan: %s", deployment.ID)

	// Re-enqueue the job
	ctx := context.Background()
	err := m.queue.Enqueue(ctx, deployment)
	if err != nil {
		m.logger.Errorf("Error re-enqueueing orphan: %v", err)
		// Mark as failed if we can't re-queue
		if statusErr := m.tracker.SetStatus(deployment.ID, interfaces.DeploymentStatusFailed); statusErr != nil {
			m.logger.Errorf("Failed to set status to failed for %s: %v", deployment.ID, statusErr)
		}
		m.logger.Errorf("Orphaned job %s could not be re-queued: %v", deployment.ID, err)
	} else {
		m.logger.Infof("Successfully re-queued orphaned job: %s", deployment.ID)
	}
}

// Stats contains monitoring statistics
type Stats struct {
	Running     bool
	LastScan    time.Time
	OrphanCount int
}
