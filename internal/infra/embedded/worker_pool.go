package embedded

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/gammazero/workerpool"

	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/pkg/logging"
)

// WorkerPool implements interfaces.WorkerPool using gammazero/workerpool
type WorkerPool struct {
	pool     *workerpool.WorkerPool
	queue    *Queue
	tracker  *Tracker
	executor DeploymentExecutor
	logger   *logging.Logger
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	started  bool
	mu       sync.Mutex
}

// DeploymentExecutor is the function that executes deployments
type DeploymentExecutor func(ctx context.Context, deployment *interfaces.QueuedDeployment) error

// WorkerPoolConfig configures the worker pool
type WorkerPoolConfig struct {
	MinWorkers int
	MaxWorkers int
	Queue      *Queue
	Tracker    *Tracker
	Executor   DeploymentExecutor
}

// NewWorkerPool creates a new embedded worker pool
func NewWorkerPool(config WorkerPoolConfig) (*WorkerPool, error) {
	if config.Queue == nil {
		return nil, fmt.Errorf("queue is required")
	}
	if config.Tracker == nil {
		return nil, fmt.Errorf("tracker is required")
	}
	if config.Executor == nil {
		return nil, fmt.Errorf("executor is required")
	}

	if config.MinWorkers <= 0 {
		config.MinWorkers = 1
	}
	if config.MaxWorkers < config.MinWorkers {
		config.MaxWorkers = config.MinWorkers * 2
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &WorkerPool{
		pool:     workerpool.New(config.MaxWorkers),
		queue:    config.Queue,
		tracker:  config.Tracker,
		executor: config.Executor,
		logger:   logging.NewLogger("embedded-worker"),
		ctx:      ctx,
		cancel:   cancel,
	}, nil
}

// Start begins processing deployments from the queue
func (p *WorkerPool) Start() {
	p.mu.Lock()
	if p.started {
		p.mu.Unlock()
		return
	}
	p.started = true
	p.mu.Unlock()

	// Start the main processing loop
	p.wg.Add(1)
	go p.processLoop()
}

// Stop gracefully stops the worker pool
func (p *WorkerPool) Stop(ctx context.Context) error {
	p.mu.Lock()
	if !p.started {
		p.mu.Unlock()
		return nil
	}
	p.mu.Unlock()

	// Cancel the processing loop
	p.cancel()

	// Wait for the processing loop to finish
	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	// Wait for completion or timeout
	select {
	case <-done:
		// Success
	case <-ctx.Done():
		return fmt.Errorf("stop timeout: %w", ctx.Err())
	}

	// Stop the underlying worker pool
	p.pool.StopWait()

	return nil
}

// processLoop continuously dequeues and processes deployments
func (p *WorkerPool) processLoop() {
	defer p.wg.Done()

	// Add panic recovery for the entire loop
	defer func() {
		if r := recover(); r != nil {
			p.logger.Error("Worker pool process loop panicked: %v", r)
			// Optionally restart the loop or notify monitoring
		}
	}()

	for {
		select {
		case <-p.ctx.Done():
			return
		default:
			// Try to dequeue a deployment
			deployment, err := p.queue.Dequeue(p.ctx)
			if err != nil {
				// Context canceled or queue closed
				if p.ctx.Err() != nil {
					return
				}
				continue
			}

			// Submit to worker pool
			p.pool.Submit(func() {
				p.processDeployment(deployment)
			})
		}
	}
}

// processDeployment handles a single deployment
func (p *WorkerPool) processDeployment(deployment *interfaces.QueuedDeployment) {
	// Add panic recovery to prevent worker pool crashes
	defer func() {
		if r := recover(); r != nil {
			// Log the panic
			p.logger.Error("Worker pool panic while processing deployment %s: %v", deployment.ID, r)

			// Mark deployment as failed
			panicErr := fmt.Errorf("panic during execution: %v", r)

			// Update the error in the tracker
			if err := p.tracker.SetError(deployment.ID, panicErr); err != nil {
				p.logger.Error("Failed to set error after panic: %v", err)
			}

			if err := p.tracker.SetStatus(deployment.ID, interfaces.DeploymentStatusFailed); err != nil {
				p.logger.Error("Failed to update status after panic: %v", err)
			}
		}
	}()

	// Update status to processing
	err := p.tracker.SetStatus(deployment.ID, interfaces.DeploymentStatusProcessing)
	if err != nil {
		// Log error but continue
		p.logger.Error("Failed to update status to processing: %v", err)
	}

	// Execute the deployment
	execErr := p.executor(p.ctx, deployment)

	// Update final status
	var finalStatus interfaces.DeploymentStatus
	if execErr != nil {
		finalStatus = interfaces.DeploymentStatusFailed
		// Log the actual error
		p.logger.Error("Deployment %s failed: %v", deployment.ID, execErr)
		// Update the error in the tracker
		if err := p.tracker.SetError(deployment.ID, execErr); err != nil {
			p.logger.Error("Failed to set error for deployment %s: %v", deployment.ID, err)
		}
	} else {
		finalStatus = interfaces.DeploymentStatusCompleted
	}

	err = p.tracker.SetStatus(deployment.ID, finalStatus)
	if err != nil {
		// Log error
		p.logger.Error("Failed to update final status: %v", err)
	}

	// Store result if successful and not already stored by executor
	if execErr == nil {
		// Check if a result has already been stored by the executor
		existingResult, _ := p.tracker.GetResult(deployment.ID)
		if existingResult == nil {
			// Only store a default result if the executor didn't store one
			result := &interfaces.DeploymentResult{
				DeploymentID: deployment.ID,
				Success:      true,
				Resources:    make(map[string]interface{}), // Empty since executor didn't provide resources
				CompletedAt:  time.Now(),
			}
			_ = p.tracker.SetResult(deployment.ID, result)
		}
	}
}

// GetWorkerCount returns the current number of active workers
func (p *WorkerPool) GetWorkerCount() int {
	return p.pool.Size()
}

// GetQueuedCount returns the number of queued deployments
func (p *WorkerPool) GetQueuedCount() int {
	return p.pool.WaitingQueueSize()
}
