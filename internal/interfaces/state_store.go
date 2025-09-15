// Package interfaces defines the core interfaces for the Lattiam system
package interfaces

import (
	"context"
	"time"
)

// DeploymentMetadataStore handles deployment metadata operations (DynamoDB-style).
// This interface manages deployment lifecycle, status, and metadata but not Terraform state files.
type DeploymentMetadataStore interface {
	// Core deployment operations
	CreateDeployment(ctx context.Context, deployment *DeploymentMetadata) error
	GetDeployment(ctx context.Context, deploymentID string) (*DeploymentMetadata, error)
	ListDeployments(ctx context.Context) ([]*DeploymentMetadata, error)
	UpdateDeploymentStatus(ctx context.Context, deploymentID string, status DeploymentStatus) error
	DeleteDeployment(ctx context.Context, deploymentID string) error

	// Health check
	Ping(ctx context.Context) error
}

// TerraformStateFileStore handles Terraform state file operations (S3-style).
// This interface manages raw Terraform state file storage and retrieval.
type TerraformStateFileStore interface {
	// SaveTerraformState saves a Terraform state file for a deployment
	SaveTerraformState(ctx context.Context, deploymentID string, stateData []byte) error

	// LoadTerraformState retrieves a Terraform state file for a deployment
	LoadTerraformState(ctx context.Context, deploymentID string) ([]byte, error)

	// DeleteTerraformState removes a Terraform state file for a deployment
	DeleteTerraformState(ctx context.Context, deploymentID string) error

	// Health check
	Ping(ctx context.Context) error
}

// StateStore combines metadata and state file operations for full functionality.
// This is the main interface that most components will use.
type StateStore interface {
	DeploymentMetadataStore
	TerraformStateFileStore

	// Locking for concurrent access (could be implemented via either backend)
	LockDeployment(ctx context.Context, deploymentID string) (StateLock, error)
	UnlockDeployment(ctx context.Context, lock StateLock) error
}

// StateLock represents a deployment lock for concurrent access control
type StateLock interface {
	ID() string
	DeploymentID() string
	CreatedAt() time.Time
	Release() error
}

// TerraformState represents Terraform-compatible state format
// This is a simplified version - in production, use the actual Terraform state types
type TerraformState struct {
	Version          int                    `json:"version"`
	TerraformVersion string                 `json:"terraform_version"`
	Serial           uint64                 `json:"serial"`
	Lineage          string                 `json:"lineage"`
	Outputs          map[string]interface{} `json:"outputs"`
	Resources        []TerraformResource    `json:"resources"`
}

// TerraformResource represents a resource in Terraform state
type TerraformResource struct {
	Module    string                      `json:"module,omitempty"`
	Mode      string                      `json:"mode"`
	Type      string                      `json:"type"`
	Name      string                      `json:"name"`
	Provider  string                      `json:"provider"`
	Instances []TerraformResourceInstance `json:"instances"`
}

// TerraformResourceInstance represents an instance of a resource
type TerraformResourceInstance struct {
	SchemaVersion int                    `json:"schema_version"`
	Attributes    map[string]interface{} `json:"attributes"`
	Private       string                 `json:"private,omitempty"`
}

// DeploymentMetadata represents the metadata for a deployment (stored in DynamoDB)
type DeploymentMetadata struct {
	DeploymentID  string                 `json:"deployment_id"`
	Status        DeploymentStatus       `json:"status"`
	CreatedAt     time.Time              `json:"created_at"`
	UpdatedAt     time.Time              `json:"updated_at"`
	Configuration map[string]interface{} `json:"configuration,omitempty"`
	ResourceCount int                    `json:"resource_count"`
	LastAppliedAt *time.Time             `json:"last_applied_at,omitempty"`
	ErrorMessage  string                 `json:"error_message,omitempty"`
}

// DeploymentStatus is defined in deployment_queue.go
// Using the existing DeploymentStatus type and constants

// StorageInfo provides information about storage backend (temporary for migration)
type StorageInfo struct {
	Type            string  `json:"type"`
	Exists          bool    `json:"exists"`
	Writable        bool    `json:"writable"`
	DeploymentCount int     `json:"deployment_count"`
	TotalSizeBytes  int64   `json:"total_size_bytes"`
	UsedPercent     float64 `json:"used_percent"`
}
