package distributed

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hibiken/asynq"

	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/pkg/logging"
)

// WorkerPool implements interfaces.WorkerPool using Asynq Server
type WorkerPool struct {
	server      *asynq.Server
	mux         *asynq.ServeMux
	tracker     *Tracker
	executor    DeploymentExecutor
	redisOpt    asynq.RedisConnOpt
	logger      *logging.Logger
	concurrency int
}

// DeploymentExecutor is the function that executes deployments
type DeploymentExecutor func(ctx context.Context, deployment *interfaces.QueuedDeployment) error

// WorkerPoolConfig configures the distributed worker pool
type WorkerPoolConfig struct {
	RedisURL    string
	Tracker     *Tracker
	Executor    DeploymentExecutor
	Concurrency int
	QueueConfig map[string]int // Queue priorities
}

// NewWorkerPool creates a new distributed worker pool
func NewWorkerPool(config WorkerPoolConfig) (*WorkerPool, error) {
	if config.RedisURL == "" {
		return nil, fmt.Errorf("redis URL is required")
	}
	if config.Tracker == nil {
		return nil, fmt.Errorf("tracker is required")
	}
	if config.Executor == nil {
		return nil, fmt.Errorf("executor is required")
	}

	// Parse Redis connection options
	redisOpt, err := asynq.ParseRedisURI(config.RedisURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse redis URL: %w", err)
	}

	// Set default concurrency if not specified
	if config.Concurrency <= 0 {
		config.Concurrency = 10
	}

	// Set default queue config if not specified
	if config.QueueConfig == nil {
		config.QueueConfig = map[string]int{
			"critical":    6,
			"deployments": 3,
			"default":     1,
		}
	}

	// Create asynq server with DLQ configuration
	server := asynq.NewServer(
		redisOpt,
		asynq.Config{
			Concurrency: config.Concurrency,
			Queues:      config.QueueConfig,
			ErrorHandler: asynq.ErrorHandlerFunc(func(_ context.Context, task *asynq.Task, err error) {
				// Create a temporary logger since this is a closure and we don't have access to pool.logger yet
				tempLogger := logging.NewLogger("distributed-worker")
				tempLogger.Error("Error processing task %s: %v", task.Type(), err)
				// Task will be automatically archived after max retries
			}),
			// Note: In asynq v0.25.1, failed tasks become "archived" (equivalent to DLQ)
			// The DLQManager in dlq_manager.go provides operations to list, requeue, and purge
			// archived tasks using Inspector.ListArchivedTasks, DeleteTask, etc.
		},
	)

	// Create handler mux
	mux := asynq.NewServeMux()

	pool := &WorkerPool{
		server:      server,
		mux:         mux,
		tracker:     config.Tracker,
		executor:    config.Executor,
		redisOpt:    redisOpt,
		concurrency: config.Concurrency,
		logger:      logging.NewLogger("distributed-worker"),
	}

	// Register task handler
	mux.HandleFunc(TaskTypeDeployment, pool.handleDeploymentTask)

	return pool, nil
}

// Start begins processing deployments from the queue
func (p *WorkerPool) Start() {
	// Start server in a goroutine
	go func() {
		if err := p.server.Start(p.mux); err != nil {
			p.logger.Error("Failed to start asynq server: %v", err)
		}
	}()
}

// Stop gracefully stops the worker pool
func (p *WorkerPool) Stop(ctx context.Context) error {
	// Shutdown the server gracefully
	p.server.Shutdown()

	// Wait for completion or timeout
	done := make(chan struct{})
	go func() {
		// Asynq server blocks until all workers finish
		p.server.Stop()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("stop timeout: %w", ctx.Err())
	}
}

// handleDeploymentTask processes a deployment task
func (p *WorkerPool) handleDeploymentTask(ctx context.Context, task *asynq.Task) error {
	// Deserialize deployment from task payload
	var deployment interfaces.QueuedDeployment
	if err := json.Unmarshal(task.Payload(), &deployment); err != nil {
		return fmt.Errorf("failed to unmarshal deployment: %w", err)
	}

	// Update status to processing
	err := p.tracker.SetStatus(deployment.ID, interfaces.DeploymentStatusProcessing)
	if err != nil {
		p.logger.Error("Failed to update status to processing: %v", err)
	}

	// Execute the deployment
	execErr := p.executor(ctx, &deployment)

	// Update final status
	var finalStatus interfaces.DeploymentStatus
	if execErr != nil {
		finalStatus = interfaces.DeploymentStatusFailed
		deployment.LastError = execErr
	} else {
		finalStatus = interfaces.DeploymentStatusCompleted
	}

	err = p.tracker.SetStatus(deployment.ID, finalStatus)
	if err != nil {
		p.logger.Error("Failed to update final status: %v", err)
	}

	// Store result if successful
	if execErr == nil {
		result := &interfaces.DeploymentResult{
			DeploymentID: deployment.ID,
			Success:      true,
			Resources:    make(map[string]interface{}), // Would be populated by executor
		}
		if err := p.tracker.SetResult(deployment.ID, result); err != nil {
			p.logger.Error("Failed to store result: %v", err)
		}
	}

	return execErr
}

// GetStats returns current worker pool statistics
func (p *WorkerPool) GetStats() (*asynq.ServerInfo, error) {
	// Create inspector to get server info
	inspector := asynq.NewInspector(p.redisOpt)
	defer func() {
		if err := inspector.Close(); err != nil {
			// Error closing inspector - logged but doesn't fail the operation
			p.logger.Warn("Failed to close inspector during stats collection: %v", err)
		}
	}()

	servers, err := inspector.Servers()
	if err != nil {
		return nil, fmt.Errorf("failed to get server info: %w", err)
	}

	// Find our server by matching concurrency
	for _, server := range servers {
		if server.Concurrency == p.concurrency {
			return server, nil
		}
	}

	return nil, fmt.Errorf("server info not found")
}
