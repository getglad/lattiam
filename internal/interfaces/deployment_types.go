package interfaces

import (
	"strings"
	"time"
)

// DeploymentID is a strongly-typed deployment identifier
type DeploymentID string

// ResourceKey is a strongly-typed resource identifier (e.g., "aws_instance.web")
type ResourceKey string

// UnifiedDeploymentRequest consolidates all deployment request types
type UnifiedDeploymentRequest struct {
	// Core identification
	ID          DeploymentID `json:"id"`
	Name        string       `json:"name"`
	RequesterID string       `json:"requester_id,omitempty"`

	// Resource configuration
	Resources   []UnifiedResource   `json:"resources,omitempty"`
	DataSources []UnifiedDataSource `json:"data_sources,omitempty"`

	// Terraform compatibility
	TerraformJSON map[string]interface{} `json:"terraform_json,omitempty"`

	// Deployment configuration
	Options  UnifiedDeploymentOptions `json:"options"`
	Tags     map[string]string        `json:"tags,omitempty"`
	Metadata map[string]interface{}   `json:"metadata,omitempty"`

	// Retry configuration
	MaxRetries     int `json:"max_retries"`
	CurrentRetries int `json:"current_retries"`

	// Timestamps
	CreatedAt time.Time `json:"created_at"`
}

// UnifiedResource consolidates resource definitions
type UnifiedResource struct {
	Type       string                 `json:"type"`       // e.g., "aws_instance"
	Name       string                 `json:"name"`       // e.g., "web"
	Key        ResourceKey            `json:"key"`        // e.g., "aws_instance.web"
	Provider   string                 `json:"provider"`   // e.g., "aws"
	Properties map[string]interface{} `json:"properties"` // Resource configuration
}

// UnifiedDataSource consolidates data source definitions
type UnifiedDataSource struct {
	Type       string                 `json:"type"`
	Name       string                 `json:"name"`
	Key        ResourceKey            `json:"key"`
	Provider   string                 `json:"provider"`
	Properties map[string]interface{} `json:"properties"`
}

// UnifiedDeploymentOptions consolidates deployment options
type UnifiedDeploymentOptions struct {
	DryRun           bool           `json:"dry_run"`
	Timeout          time.Duration  `json:"timeout"`
	ParallelismLimit int            `json:"parallelism_limit"`
	ProviderConfig   ProviderConfig `json:"provider_config,omitempty"`
	AutoApprove      bool           `json:"auto_approve"`
	RefreshOnly      bool           `json:"refresh_only"`
}

// UnifiedQueuedDeployment consolidates queued deployment types
type UnifiedQueuedDeployment struct {
	Request     *UnifiedDeploymentRequest `json:"request"`
	Status      DeploymentStatus          `json:"status"`
	CreatedAt   time.Time                 `json:"created_at"`
	StartedAt   *time.Time                `json:"started_at,omitempty"`
	CompletedAt *time.Time                `json:"completed_at,omitempty"`
	LastError   error                     `json:"last_error,omitempty"`

	// Execution tracking
	ProcessingWorkerID string    `json:"processing_worker_id,omitempty"`
	LastHealthCheck    time.Time `json:"last_health_check,omitempty"`
}

// MakeResourceKey generates a unique key for a resource from its type and name
func MakeResourceKey(resourceType, resourceName string) ResourceKey {
	return ResourceKey(resourceType + "." + resourceName)
}

// ParseResourceKey parses a resource key into type and name components
func ParseResourceKey(key ResourceKey) (resourceType string, resourceName string) {
	parts := strings.Split(string(key), ".")
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return string(key), ""
}

// Conversion helpers for deployment request types

// Update-specific types for plan-apply workflow

// UpdateRequest represents a request to update an existing deployment
type UpdateRequest struct {
	DeploymentID     string                 `json:"deployment_id"`
	RequestID        string                 `json:"request_id,omitempty"` // Correlation ID for tracing
	NewTerraformJSON map[string]interface{} `json:"new_terraform_json"`
	PlanFirst        bool                   `json:"plan_first"`   // Always true for safety
	AutoApprove      bool                   `json:"auto_approve"` // Require explicit approval for dangerous changes
	ForceUpdate      bool                   `json:"force_update"` // Skip safety checks (dangerous)
	UpdatedBy        string                 `json:"updated_by,omitempty"`
	UpdateReason     string                 `json:"update_reason,omitempty"`
}

// QueuedUpdate represents an update operation in progress
type QueuedUpdate struct {
	ID           string         `json:"id"`
	DeploymentID string         `json:"deployment_id"`
	Request      *UpdateRequest `json:"request"`
	Status       UpdateStatus   `json:"status"`
	Plan         *UpdatePlan    `json:"plan,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	StartedAt    *time.Time     `json:"started_at,omitempty"`
	CompletedAt  *time.Time     `json:"completed_at,omitempty"`
	LastError    error          `json:"last_error,omitempty"`
}

// UpdatePlan represents the calculated changes for an update
type UpdatePlan struct {
	DeploymentID     string                            `json:"deployment_id"`
	PlanID           string                            `json:"plan_id"`
	Changes          []ResourceChange                  `json:"changes"`         // All changes in the plan
	ExecutionOrder   []ResourceChange                  `json:"execution_order"` // Dependency-aware sorted order for execution
	Summary          UpdateSummary                     `json:"summary"`
	DangerousChanges []DangerousChange                 `json:"dangerous_changes,omitempty"`
	CreatedAt        time.Time                         `json:"created_at"`
	RequiresApproval bool                              `json:"requires_approval"`
	ProviderConfigs  map[string]map[string]interface{} `json:"provider_configs,omitempty"`  // Provider configurations for plan execution
	ProviderVersions map[string]string                 `json:"provider_versions,omitempty"` // Provider versions for plan execution
}

// ResourceChange represents a single resource change in the plan
type ResourceChange struct {
	ResourceKey    ResourceKey            `json:"resource_key"`              // e.g., "aws_instance.web"
	Action         ChangeAction           `json:"action"`                    // create, update, delete, replace, no-op
	Before         map[string]interface{} `json:"before,omitempty"`          // Current values
	After          map[string]interface{} `json:"after,omitempty"`           // Planned values (provider's planned state)
	ProposedState  map[string]interface{} `json:"proposed_state,omitempty"`  // User config merged with computed fields
	ReplacePaths   []string               `json:"replace_paths,omitempty"`   // Attributes causing replacement
	OriginalConfig map[string]interface{} `json:"original_config,omitempty"` // Original uninterpolated configuration
}

// ChangeAction represents the type of change for a resource
type ChangeAction string

// ChangeAction constants represent the various actions that can be taken on resources
const (
	ActionCreate  ChangeAction = "create"  // Create resource
	ActionUpdate  ChangeAction = "update"  // Modify existing resource
	ActionDelete  ChangeAction = "delete"  // Delete resource
	ActionReplace ChangeAction = "replace" // Destroy and recreate (dangerous)
	ActionNoOp    ChangeAction = "no-op"   // No changes needed
)

// UpdateSummary provides a summary of planned changes
type UpdateSummary struct {
	ToCreate  int `json:"to_create"`
	ToUpdate  int `json:"to_update"`
	ToDelete  int `json:"to_delete"`
	ToReplace int `json:"to_replace"`
	NoChanges int `json:"no_changes"`
}

// DangerousChange represents a change that could cause data loss or downtime
type DangerousChange struct {
	ResourceKey ResourceKey `json:"resource_key"`
	Reason      string      `json:"reason"`
	Impact      string      `json:"impact"`
	Mitigation  string      `json:"mitigation,omitempty"`
}

// UpdateStatus represents the status of an update operation
type UpdateStatus string

// UpdateStatus constants represent the various states of a resource update
const (
	UpdateStatusPending   UpdateStatus = "pending"
	UpdateStatusPlanning  UpdateStatus = "planning" // Generating plan
	UpdateStatusApproval  UpdateStatus = "approval" // Waiting for approval
	UpdateStatusApplying  UpdateStatus = "applying" // Executing changes
	UpdateStatusCompleted UpdateStatus = "completed"
	UpdateStatusFailed    UpdateStatus = "failed"
	UpdateStatusCanceled  UpdateStatus = "canceled"
)

// DeploymentState represents the persistent state of a deployment
type DeploymentState struct {
	DeploymentID  string                 `json:"deployment_id"`
	Name          string                 `json:"name"`
	TerraformJSON map[string]interface{} `json:"terraform_json"`
	StateFile     string                 `json:"state_file"`    // Path to .tfstate file
	StateVersion  int                    `json:"state_version"` // Terraform state version
	LastAppliedAt time.Time              `json:"last_applied_at"`
	CreatedAt     time.Time              `json:"created_at"`
	UpdatedAt     time.Time              `json:"updated_at"`
	StateBackend  string                 `json:"state_backend,omitempty"` // e.g., "s3://bucket/path/"
}
