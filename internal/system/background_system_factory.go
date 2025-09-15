// Package system provides system-level components and factories
package system

import (
	"context"
	"fmt"

	"github.com/lattiam/lattiam/internal/config"
	"github.com/lattiam/lattiam/internal/infra/distributed"
	"github.com/lattiam/lattiam/internal/infra/embedded"
	"github.com/lattiam/lattiam/internal/interfaces"
)

// BackgroundSystemComponents holds all the components of the background system
type BackgroundSystemComponents struct {
	Queue         interfaces.DeploymentQueue
	Tracker       interfaces.DeploymentTracker
	WorkerPool    interfaces.WorkerPool
	StateStore    interfaces.StateStore
	OrphanMonitor interface {
		Start() error
		Stop(context.Context) error
	} // Optional orphan monitor
}

// NewBackgroundSystem creates the appropriate background system based on configuration
func NewBackgroundSystem(cfg *config.ServerConfig, executor DeploymentExecutor) (*BackgroundSystemComponents, error) {
	if cfg == nil {
		return nil, fmt.Errorf("configuration is required")
	}
	if executor == nil {
		return nil, fmt.Errorf("executor is required")
	}

	switch cfg.Queue.Type {
	case "embedded":
		return newEmbeddedSystem(cfg, executor)
	case "distributed":
		return newDistributedSystem(cfg, executor)
	default:
		return nil, fmt.Errorf("unsupported queue type: %s", cfg.Queue.Type)
	}
}

// DeploymentExecutor is the function that executes deployments
type DeploymentExecutor func(ctx context.Context, deployment *interfaces.QueuedDeployment) error

// newEmbeddedSystem creates an embedded (in-process) background system
func newEmbeddedSystem(cfg *config.ServerConfig, executor DeploymentExecutor) (*BackgroundSystemComponents, error) {
	// Create components
	tracker := embedded.NewTracker()
	queue := embedded.NewQueue(100) // Default capacity

	// Create StateStore if configured
	var stateStore interfaces.StateStore
	if cfg.StateStore.Type != "" && cfg.StateStore.Type != "none" {
		factory := NewDefaultComponentFactory()
		stateConfig := interfaces.StateStoreConfig{
			Type:    cfg.StateStore.Type,
			Options: make(map[string]interface{}),
		}

		// Configure StateStore options based on type
		switch cfg.StateStore.Type {
		case "file":
			if cfg.StateStore.File.Path != "" {
				stateConfig.Options["path"] = cfg.StateStore.File.Path
			}
		case "redis":
			// For Redis, we would use the same Redis URL as the queue
			if cfg.Queue.RedisURL != "" {
				stateConfig.Options["redis_url"] = cfg.Queue.RedisURL
			}
		case "aws":
			// For AWS, pass the server config so it can extract AWS settings
			stateConfig.Options["server_config"] = cfg
		}

		var err error
		stateStore, err = factory.CreateStateStore(stateConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create state store: %w", err)
		}

		// Load existing deployments from StateStore into tracker
		if err := tracker.Load(stateStore); err != nil {
			return nil, fmt.Errorf("failed to load deployments from state store: %w", err)
		}
	}

	// Create worker pool
	poolConfig := embedded.WorkerPoolConfig{
		MinWorkers: 1,
		MaxWorkers: 4,
		Queue:      queue,
		Tracker:    tracker,
		Executor:   embedded.DeploymentExecutor(executor),
	}

	pool, err := embedded.NewWorkerPool(poolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create embedded worker pool: %w", err)
	}

	return &BackgroundSystemComponents{
		Queue:      queue,
		Tracker:    tracker,
		WorkerPool: pool,
		StateStore: stateStore, // May be nil if not configured
	}, nil
}

// newDistributedSystem creates a distributed (Redis-backed) background system
func newDistributedSystem(cfg *config.ServerConfig, executor DeploymentExecutor) (*BackgroundSystemComponents, error) {
	if cfg.Queue.RedisURL == "" {
		return nil, fmt.Errorf("redis URL is required for distributed mode")
	}

	// Create queue
	queue, err := distributed.NewQueue(cfg.Queue.RedisURL)
	if err != nil {
		return nil, fmt.Errorf("failed to create distributed queue: %w", err)
	}

	// Create tracker using queue's Redis connection
	tracker, err := distributed.NewTracker(queue.GetRedisClient())
	if err != nil {
		return nil, fmt.Errorf("failed to create distributed tracker: %w", err)
	}

	// Create worker pool
	poolConfig := distributed.WorkerPoolConfig{
		RedisURL:    cfg.Queue.RedisURL,
		Tracker:     tracker,
		Executor:    distributed.DeploymentExecutor(executor),
		Concurrency: 10, // Default concurrency
		QueueConfig: map[string]int{
			"critical":    6,
			"deployments": 3,
			"default":     1,
		},
	}

	pool, err := distributed.NewWorkerPool(poolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create distributed worker pool: %w", err)
	}

	return &BackgroundSystemComponents{
		Queue:      queue,
		Tracker:    tracker,
		WorkerPool: pool,
	}, nil
}

// Close gracefully shuts down all components
func (c *BackgroundSystemComponents) Close(ctx context.Context) error {
	// Stop orphan monitor first if present
	if c.OrphanMonitor != nil {
		if err := c.OrphanMonitor.Stop(ctx); err != nil {
			return fmt.Errorf("failed to stop orphan monitor: %w", err)
		}
	}

	// Stop worker pool
	if err := c.WorkerPool.Stop(ctx); err != nil {
		return fmt.Errorf("failed to stop worker pool: %w", err)
	}

	// Close queue if it has a Close method
	if closer, ok := c.Queue.(interface{ Close() error }); ok {
		if err := closer.Close(); err != nil {
			return fmt.Errorf("failed to close queue: %w", err)
		}
	}

	// Close tracker if it has a Close method
	if closer, ok := c.Tracker.(interface{ Close() error }); ok {
		if err := closer.Close(); err != nil {
			return fmt.Errorf("failed to close tracker: %w", err)
		}
	}

	return nil
}
