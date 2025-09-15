package interfaces

import (
	"time"
)

// Types for deployment queue functionality
// These types are used by DeploymentQueue and related interfaces

// QueuedDeployment represents a deployment in the queue
type QueuedDeployment struct {
	ID           string             `json:"id"`
	RequestID    string             `json:"request_id,omitempty"` // Correlation ID for distributed tracing
	Request      *DeploymentRequest `json:"request"`
	Status       DeploymentStatus   `json:"status"`
	CreatedAt    time.Time          `json:"created_at"`
	StartedAt    *time.Time         `json:"started_at,omitempty"`
	CompletedAt  *time.Time         `json:"completed_at,omitempty"`
	LastError    error              `json:"last_error,omitempty"`
	RetryCount   int                `json:"retry_count"`
	DeletedAt    *time.Time         `json:"deleted_at,omitempty"`
	DeletedBy    string             `json:"deleted_by,omitempty"`
	DeleteReason string             `json:"delete_reason,omitempty"`
}

// DeploymentRequest represents a request to deploy resources
type DeploymentRequest struct {
	Resources   []Resource             `json:"resources"`
	DataSources []DataSource           `json:"data_sources,omitempty"`
	Options     DeploymentOptions      `json:"options"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// Resource represents an infrastructure resource to be deployed
type Resource struct {
	Type       string                 `json:"type"`
	Name       string                 `json:"name"`
	Properties map[string]interface{} `json:"properties"`
}

// DataSource represents a data source to be read
type DataSource struct {
	Type       string                 `json:"type"`
	Name       string                 `json:"name"`
	Properties map[string]interface{} `json:"properties"`
}

// DeploymentOptions configures deployment behavior
type DeploymentOptions struct {
	DryRun         bool           `json:"dry_run"`
	Timeout        time.Duration  `json:"timeout"`
	MaxRetries     int            `json:"max_retries"`
	ProviderConfig ProviderConfig `json:"provider_config,omitempty"`
}

// DeploymentStatus represents the status of a deployment
type DeploymentStatus string

// DeploymentStatus constants represent the various states of a deployment
const (
	DeploymentStatusQueued     DeploymentStatus = "queued"
	DeploymentStatusProcessing DeploymentStatus = "processing"
	DeploymentStatusCompleted  DeploymentStatus = "completed"
	DeploymentStatusFailed     DeploymentStatus = "failed"
	DeploymentStatusCanceled   DeploymentStatus = "canceled"
	DeploymentStatusCanceling  DeploymentStatus = "canceling"
	DeploymentStatusDestroying DeploymentStatus = "destroying"
	DeploymentStatusDestroyed  DeploymentStatus = "destroyed"
	DeploymentStatusDeleted    DeploymentStatus = "deleted"
)

// DeploymentFilter provides filtering options for querying deployments
type DeploymentFilter struct {
	Status        []DeploymentStatus
	CreatedAfter  time.Time
	CreatedBefore time.Time
}

// QueueMetrics provides metrics about the deployment queue
type QueueMetrics struct {
	TotalEnqueued    int64
	TotalDequeued    int64
	CurrentDepth     int
	AverageWaitTime  time.Duration
	OldestDeployment time.Time
}

// QueueCall represents a call to the DeploymentQueue for tracking in mocks
type QueueCall struct {
	Method       string
	DeploymentID string
	Timestamp    time.Time
	Error        error
}
