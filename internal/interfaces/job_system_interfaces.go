package interfaces

import (
	"context"
	"time"
)

// DeploymentTracker manages the state and metadata of deployments.
// It provides deployment registration, status tracking, and result storage.
type DeploymentTracker interface {
	Register(deployment *QueuedDeployment) error
	GetByID(deploymentID string) (*QueuedDeployment, error)
	GetStatus(deploymentID string) (*DeploymentStatus, error)
	SetStatus(deploymentID string, status DeploymentStatus) error
	GetResult(deploymentID string) (*DeploymentResult, error)
	SetResult(deploymentID string, result *DeploymentResult) error
	List(filter DeploymentFilter) ([]*QueuedDeployment, error)
	Remove(deploymentID string) error
	// Load restores deployments from persistent storage (if supported).
	// StateStore parameter is optional and may be nil.
	Load(stateStore StateStore) error
}

// DeploymentQueue is responsible for enqueueing and managing deployments.
// It provides a simple, focused interface for queue operations.
type DeploymentQueue interface {
	Enqueue(ctx context.Context, deployment *QueuedDeployment) error
	Cancel(ctx context.Context, deploymentID string) error
	GetMetrics() QueueMetrics
}

// WorkerPool manages the lifecycle of background workers.
// It provides a simple interface for worker pool operations.
type WorkerPool interface {
	Start()
	Stop(ctx context.Context) error
}

// DeploymentResult represents the result of a completed deployment
type DeploymentResult struct {
	DeploymentID string                 `json:"deployment_id"`
	Success      bool                   `json:"success"`
	Error        error                  `json:"error,omitempty"`
	Resources    map[string]interface{} `json:"resources"`         // Resource states after deployment
	Outputs      map[string]interface{} `json:"outputs,omitempty"` // Output values from deployment
	CompletedAt  time.Time              `json:"completed_at"`
}
