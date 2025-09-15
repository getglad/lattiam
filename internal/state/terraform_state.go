package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// TerraformState represents a Terraform state file structure
type TerraformState struct {
	Version          int                      `json:"version"`
	TerraformVersion string                   `json:"terraform_version"`
	Serial           int                      `json:"serial"`
	Lineage          string                   `json:"lineage"`
	Outputs          map[string]interface{}   `json:"outputs"` // Terraform compatibility
	Resources        []TerraformStateResource `json:"resources"`
	CheckResults     interface{}              `json:"check_results"` // Terraform compatibility (can be null)
}

// TerraformStateResource represents a resource in Terraform state
type TerraformStateResource struct {
	Mode      string                           `json:"mode"`
	Type      string                           `json:"type"`
	Name      string                           `json:"name"`
	Provider  string                           `json:"provider"`
	Instances []TerraformStateResourceInstance `json:"instances"`
}

// TerraformStateResourceInstance represents an instance of a resource
type TerraformStateResourceInstance struct {
	SchemaVersion         int                    `json:"schema_version"`
	Attributes            map[string]interface{} `json:"attributes"`
	SensitiveAttributes   []string               `json:"sensitive_attributes"`    // Terraform compatibility
	IdentitySchemaVersion int                    `json:"identity_schema_version"` // Terraform compatibility
	Private               string                 `json:"private,omitempty"`
	Dependencies          []string               `json:"dependencies,omitempty"`
	CreateBeforeDestroy   bool                   `json:"create_before_destroy,omitempty"`
}

// TerraformStateStore manages Terraform state files separately from deployment records
type TerraformStateStore struct {
	baseDir string
	mu      sync.RWMutex
}

// NewTerraformStateStore creates a new Terraform state store
func NewTerraformStateStore(baseDir string) (*TerraformStateStore, error) {
	// Create terraform state directory
	stateDir := filepath.Join(baseDir, "terraform")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create terraform state directory: %w", err)
	}

	return &TerraformStateStore{
		baseDir: stateDir,
	}, nil
}

// SaveState saves a Terraform state for a deployment
func (ts *TerraformStateStore) SaveState(_ context.Context, deploymentID string, state *TerraformState) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	// Increment serial number
	state.Serial++

	// Marshal state
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal terraform state: %w", err)
	}

	// Write to file atomically
	filePath := ts.statePath(deploymentID)
	tempPath := filePath + ".tmp"

	if err := os.WriteFile(tempPath, data, 0o600); err != nil {
		return fmt.Errorf("failed to write terraform state: %w", err)
	}

	if err := os.Rename(tempPath, filePath); err != nil {
		_ = os.Remove(tempPath)
		return fmt.Errorf("failed to save terraform state: %w", err)
	}

	return nil
}

// LoadState loads a Terraform state for a deployment
func (ts *TerraformStateStore) LoadState(_ context.Context, deploymentID string) (*TerraformState, error) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	filePath := ts.statePath(deploymentID)
	// #nosec G304 - Path is sanitized by statePath method
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			// Return empty state for new deployments
			return &TerraformState{
				Version:          4,
				TerraformVersion: "1.5.0",
				Serial:           0,
				Lineage:          generateLineage(),
				Outputs:          map[string]interface{}{},
				Resources:        []TerraformStateResource{},
				CheckResults:     nil,
			}, nil
		}
		return nil, fmt.Errorf("failed to read terraform state: %w", err)
	}

	var state TerraformState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to unmarshal terraform state: %w", err)
	}

	return &state, nil
}

// DeleteState removes a Terraform state file
func (ts *TerraformStateStore) DeleteState(_ context.Context, deploymentID string) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	filePath := ts.statePath(deploymentID)
	if err := os.Remove(filePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete terraform state: %w", err)
	}

	return nil
}

// statePath returns the file path for a deployment's Terraform state
func (ts *TerraformStateStore) statePath(deploymentID string) string {
	// Sanitize the deployment ID to prevent path traversal
	cleanID := filepath.Clean(deploymentID)
	safeID := strings.ReplaceAll(cleanID, "/", "_")
	safeID = strings.ReplaceAll(safeID, "\\", "_")
	safeID = strings.ReplaceAll(safeID, "..", "_")
	return filepath.Join(ts.baseDir, safeID+".tfstate")
}

// generateLineage generates a unique lineage ID for a new state file
func generateLineage() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), os.Getpid())
}

// RawState stores the raw provider state data
type RawState struct {
	DeploymentID  string
	ResourceType  string
	ResourceName  string
	PrivateData   []byte // Raw msgpack from provider
	StateData     []byte // Raw msgpack state
	SchemaVersion int
}

// SaveRawState saves the raw state data from a provider
func (ts *TerraformStateStore) SaveRawState(_ context.Context, rawState *RawState) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	// Create a directory for raw states
	rawDir := filepath.Join(ts.baseDir, "raw", rawState.DeploymentID)
	if err := os.MkdirAll(rawDir, 0o700); err != nil {
		return fmt.Errorf("failed to create raw state directory: %w", err)
	}

	// Save the raw state data
	fileName := fmt.Sprintf("%s_%s.raw", rawState.ResourceType, rawState.ResourceName)
	filePath := filepath.Join(rawDir, fileName)

	data := map[string]interface{}{
		"resource_type":  rawState.ResourceType,
		"resource_name":  rawState.ResourceName,
		"schema_version": rawState.SchemaVersion,
		"private_data":   rawState.PrivateData,
		"state_data":     rawState.StateData,
		"saved_at":       time.Now().UTC(),
	}

	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal raw state: %w", err)
	}

	if err := os.WriteFile(filePath, jsonData, 0o600); err != nil {
		return fmt.Errorf("failed to write raw state: %w", err)
	}

	return nil
}

// LoadRawState loads the raw state data for a resource
func (ts *TerraformStateStore) LoadRawState(_ context.Context, deploymentID, resourceType, resourceName string) (*RawState, error) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	// Sanitize inputs to prevent path traversal
	cleanDeploymentID := filepath.Clean(deploymentID)
	safeDeploymentID := strings.ReplaceAll(cleanDeploymentID, "/", "_")
	safeDeploymentID = strings.ReplaceAll(safeDeploymentID, "\\", "_")
	safeDeploymentID = strings.ReplaceAll(safeDeploymentID, "..", "_")

	safeResourceType := strings.ReplaceAll(resourceType, "/", "_")
	safeResourceType = strings.ReplaceAll(safeResourceType, "\\", "_")
	safeResourceType = strings.ReplaceAll(safeResourceType, "..", "_")

	safeResourceName := strings.ReplaceAll(resourceName, "/", "_")
	safeResourceName = strings.ReplaceAll(safeResourceName, "\\", "_")
	safeResourceName = strings.ReplaceAll(safeResourceName, "..", "_")

	fileName := fmt.Sprintf("%s_%s.raw", safeResourceType, safeResourceName)
	filePath := filepath.Join(ts.baseDir, "raw", safeDeploymentID, fileName)

	// #nosec G304 - Path components are sanitized above
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errors.New("raw state not found")
		}
		return nil, fmt.Errorf("failed to read raw state: %w", err)
	}

	var rawData map[string]interface{}
	if err := json.Unmarshal(data, &rawData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal raw state: %w", err)
	}

	rawState := &RawState{
		DeploymentID:  deploymentID,
		ResourceType:  resourceType,
		ResourceName:  resourceName,
		SchemaVersion: int(rawData["schema_version"].(float64)),
	}

	if privateData, ok := rawData["private_data"].(string); ok {
		rawState.PrivateData = []byte(privateData)
	}

	if stateData, ok := rawData["state_data"].(string); ok {
		rawState.StateData = []byte(stateData)
	}

	return rawState, nil
}
