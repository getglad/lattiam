package state

import (
	"context"
	"errors"
	"time"
)

// Static errors for err113 compliance
var (
	ErrDeploymentNotFound  = errors.New("deployment not found")
	ErrDeploymentExists    = errors.New("deployment already exists")
	ErrLockNotAcquired     = errors.New("lock not acquired")
	ErrLockAlreadyHeld     = errors.New("lock already held")
	ErrStoreClosed         = errors.New("store is closed")
	ErrInvalidDeploymentID = errors.New("invalid deployment ID")
)

// DeploymentState represents the persistent state of a deployment
type DeploymentState struct {
	ID            string                 `json:"id"`
	Name          string                 `json:"name"`
	Status        string                 `json:"status"`
	Resources     []ResourceState        `json:"resources"`
	Results       []DeploymentResult     `json:"results"`
	Config        map[string]interface{} `json:"config,omitempty"`
	CreatedAt     time.Time              `json:"created_at"`
	UpdatedAt     time.Time              `json:"updated_at"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
	TerraformJSON map[string]interface{} `json:"terraform_json,omitempty"`
}

// ResourceState represents the state of a single resource
type ResourceState struct {
	Type       string                 `json:"type"`
	Name       string                 `json:"name"`
	Properties map[string]interface{} `json:"properties"`
}

// DeploymentResult represents the result of deploying a resource
type DeploymentResult struct {
	Resource ResourceState          `json:"resource"`
	State    map[string]interface{} `json:"state,omitempty"`
	Error    string                 `json:"error,omitempty"`
}

// Lock represents a deployment lock
type Lock struct {
	DeploymentID string    `json:"deployment_id"`
	HolderID     string    `json:"holder_id"`
	AcquiredAt   time.Time `json:"acquired_at"`
	ExpiresAt    time.Time `json:"expires_at"`
}

// Store defines the interface for deployment state persistence
type Store interface {
	// Save creates or updates a deployment
	Save(ctx context.Context, deployment *DeploymentState) error

	// Load retrieves a deployment by ID
	Load(ctx context.Context, id string) (*DeploymentState, error)

	// Update modifies an existing deployment
	Update(ctx context.Context, deployment *DeploymentState) error

	// Delete removes a deployment
	Delete(ctx context.Context, id string) error

	// List returns all deployments, optionally filtered by status
	List(ctx context.Context, status string) ([]*DeploymentState, error)

	// Lock acquires an exclusive lock on a deployment
	Lock(ctx context.Context, deploymentID, holderID string, duration time.Duration) (*Lock, error)

	// Unlock releases a deployment lock
	Unlock(ctx context.Context, deploymentID, holderID string) error

	// Close closes the store and releases resources
	Close() error
}

// QueryOptions defines options for listing deployments
type QueryOptions struct {
	Status        string    // Filter by status
	CreatedAfter  time.Time // Filter by creation time
	CreatedBefore time.Time // Filter by creation time
	Limit         int       // Maximum number of results
	Offset        int       // Skip results
}

// ExtendedStore provides additional query capabilities
type ExtendedStore interface {
	Store

	// ListWithOptions returns deployments matching the query options
	ListWithOptions(ctx context.Context, opts QueryOptions) ([]*DeploymentState, error)

	// Count returns the number of deployments matching the status
	Count(ctx context.Context, status string) (int, error)

	// CleanupExpiredLocks removes locks that have expired
	CleanupExpiredLocks(ctx context.Context) error
}

// StoreFactory creates Store instances
type StoreFactory interface {
	// CreateStore creates a new store instance
	CreateStore(config map[string]interface{}) (Store, error)
}

// Transaction represents a transactional operation
type Transaction interface {
	// Commit commits the transaction
	Commit() error

	// Rollback rolls back the transaction
	Rollback() error
}

// TransactionalStore supports transactional operations
type TransactionalStore interface {
	Store

	// Begin starts a new transaction
	Begin(ctx context.Context) (Transaction, error)

	// SaveTx saves a deployment within a transaction
	SaveTx(ctx context.Context, tx Transaction, deployment *DeploymentState) error

	// UpdateTx updates a deployment within a transaction
	UpdateTx(ctx context.Context, tx Transaction, deployment *DeploymentState) error

	// DeleteTx deletes a deployment within a transaction
	DeleteTx(ctx context.Context, tx Transaction, id string) error
}
