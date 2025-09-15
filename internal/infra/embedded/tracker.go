package embedded

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/pkg/logging"
)

// Tracker implements interfaces.DeploymentTracker using in-memory storage
type Tracker struct {
	mu          sync.RWMutex
	deployments map[string]*interfaces.QueuedDeployment
	results     map[string]*interfaces.DeploymentResult
	stateStore  interfaces.StateStore // Optional persistent storage
	logger      *logging.Logger
}

// NewTracker creates a new embedded deployment tracker
func NewTracker() *Tracker {
	return &Tracker{
		deployments: make(map[string]*interfaces.QueuedDeployment),
		results:     make(map[string]*interfaces.DeploymentResult),
		logger:      logging.NewLogger("embedded-tracker"),
	}
}

// Register adds a new deployment to the tracker
func (t *Tracker) Register(deployment *interfaces.QueuedDeployment) error {
	if deployment == nil {
		return fmt.Errorf("deployment is nil")
	}
	if deployment.ID == "" {
		return fmt.Errorf("deployment ID is empty")
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if _, exists := t.deployments[deployment.ID]; exists {
		return fmt.Errorf("deployment %s already exists", deployment.ID)
	}

	// Store a copy to prevent external modifications
	d := *deployment
	t.deployments[deployment.ID] = &d

	// Persist to StateStore if available (must be done while holding the lock)
	t.persistDeployment(&d)

	return nil
}

// GetByID returns a deployment by its ID
func (t *Tracker) GetByID(deploymentID string) (*interfaces.QueuedDeployment, error) {
	if deploymentID == "" {
		return nil, fmt.Errorf("deployment ID is empty")
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	deployment, exists := t.deployments[deploymentID]
	if !exists {
		return nil, fmt.Errorf("deployment %s not found", deploymentID)
	}

	// Return a copy to prevent external modifications
	d := *deployment
	return &d, nil
}

// GetStatus returns the status of a deployment
func (t *Tracker) GetStatus(deploymentID string) (*interfaces.DeploymentStatus, error) {
	if deploymentID == "" {
		return nil, fmt.Errorf("deployment ID is empty")
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	deployment, exists := t.deployments[deploymentID]
	if !exists {
		return nil, fmt.Errorf("deployment %s not found", deploymentID)
	}

	status := deployment.Status
	return &status, nil
}

// SetStatus updates the status of a deployment
func (t *Tracker) SetStatus(deploymentID string, status interfaces.DeploymentStatus) error {
	if deploymentID == "" {
		return fmt.Errorf("deployment ID is empty")
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	deployment, exists := t.deployments[deploymentID]
	if !exists {
		return fmt.Errorf("deployment %s not found", deploymentID)
	}

	deployment.Status = status

	// Update timestamps based on status
	now := time.Now()
	switch status {
	case interfaces.DeploymentStatusQueued:
		// No timestamp update needed for queued status
	case interfaces.DeploymentStatusProcessing:
		if deployment.StartedAt == nil {
			deployment.StartedAt = &now
		}
	case interfaces.DeploymentStatusCompleted, interfaces.DeploymentStatusFailed, interfaces.DeploymentStatusCanceled:
		if deployment.CompletedAt == nil {
			deployment.CompletedAt = &now
		}
	case interfaces.DeploymentStatusCanceling, interfaces.DeploymentStatusDestroying:
		// These are transitional states, no specific timestamp update
	case interfaces.DeploymentStatusDestroyed, interfaces.DeploymentStatusDeleted:
		if deployment.CompletedAt == nil {
			deployment.CompletedAt = &now
		}
	}

	// Persist to StateStore if available (must be done while holding the lock)
	t.persistDeployment(deployment)

	return nil
}

// GetResult returns the result of a deployment
func (t *Tracker) GetResult(deploymentID string) (*interfaces.DeploymentResult, error) {
	if deploymentID == "" {
		return nil, fmt.Errorf("deployment ID is empty")
	}

	t.mu.RLock()
	defer t.mu.RUnlock()

	result, exists := t.results[deploymentID]
	if !exists {
		return nil, nil // Not an error, just no result yet
	}

	// Return a copy to prevent external modifications
	r := interfaces.DeploymentResult{
		DeploymentID: result.DeploymentID,
		Success:      result.Success,
		Error:        result.Error,
		Resources:    nil,
		Outputs:      nil,
		CompletedAt:  result.CompletedAt,
	}

	// Deep copy the Resources map
	if result.Resources != nil {
		r.Resources = make(map[string]interface{}, len(result.Resources))
		for k, v := range result.Resources {
			r.Resources[k] = v
		}
	}

	// Deep copy the Outputs map
	if result.Outputs != nil {
		r.Outputs = make(map[string]interface{}, len(result.Outputs))
		for k, v := range result.Outputs {
			r.Outputs[k] = v
		}
	}

	return &r, nil
}

// List returns deployments matching the filter
func (t *Tracker) List(filter interfaces.DeploymentFilter) ([]*interfaces.QueuedDeployment, error) {
	t.mu.RLock()
	defer t.mu.RUnlock()

	var results []*interfaces.QueuedDeployment

	for _, deployment := range t.deployments {
		if matchesFilter(deployment, filter) {
			// Return a copy to prevent external modifications
			d := *deployment
			results = append(results, &d)
		}
	}

	return results, nil
}

// Remove deletes a deployment from the tracker
func (t *Tracker) Remove(deploymentID string) error {
	if deploymentID == "" {
		return fmt.Errorf("deployment ID is empty")
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if _, exists := t.deployments[deploymentID]; !exists {
		return fmt.Errorf("deployment %s not found", deploymentID)
	}

	delete(t.deployments, deploymentID)
	delete(t.results, deploymentID)

	// Remove from StateStore if available (must be done while holding the lock)
	if t.stateStore != nil {
		ctx := context.Background()
		if err := t.stateStore.DeleteDeployment(ctx, deploymentID); err != nil {
			t.logger.Warn("Failed to delete deployment %s from StateStore: %v", deploymentID, err)
		}
	}

	return nil
}

// SetResult stores the result of a deployment
//
//nolint:funlen,gocognit // Deployment result handling involves multiple validation and persistence steps
func (t *Tracker) SetResult(deploymentID string, result *interfaces.DeploymentResult) error {
	if deploymentID == "" {
		return fmt.Errorf("deployment ID is empty")
	}
	if result == nil {
		return fmt.Errorf("result is nil")
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if _, exists := t.deployments[deploymentID]; !exists {
		return fmt.Errorf("deployment %s not found", deploymentID)
	}

	// Log what we're receiving
	t.logger.Debug("SetResult: Received result for deployment %s: Success=%v, Resources=%d items",
		deploymentID, result.Success, len(result.Resources))

	// Store a copy to prevent external modifications
	r := interfaces.DeploymentResult{
		DeploymentID: result.DeploymentID,
		Success:      result.Success,
		Error:        result.Error,
		Resources:    nil,
		Outputs:      nil,
		CompletedAt:  result.CompletedAt,
	}

	// Deep copy the Resources map
	if result.Resources != nil {
		r.Resources = make(map[string]interface{}, len(result.Resources))
		for k, v := range result.Resources {
			r.Resources[k] = v
		}
		t.logger.Debug("SetResult: Deep copied %d resources", len(r.Resources))
	} else {
		t.logger.Debug("SetResult: Original Resources map is nil")
	}

	// Deep copy the Outputs map
	if result.Outputs != nil {
		r.Outputs = make(map[string]interface{}, len(result.Outputs))
		for k, v := range result.Outputs {
			r.Outputs[k] = v
		}
	}

	t.results[deploymentID] = &r

	// Verify what we stored
	storedResult := t.results[deploymentID]
	t.logger.Debug("SetResult: Stored result for deployment %s: Success=%v, Resources=%d items",
		deploymentID, storedResult.Success, len(storedResult.Resources))

	// Update deployment status based on result if not already in a terminal state
	if deployment, exists := t.deployments[deploymentID]; exists {
		if deployment.Status != interfaces.DeploymentStatusCompleted &&
			deployment.Status != interfaces.DeploymentStatusFailed &&
			deployment.Status != interfaces.DeploymentStatusCanceled {
			if result.Success {
				deployment.Status = interfaces.DeploymentStatusCompleted
			} else {
				deployment.Status = interfaces.DeploymentStatusFailed
			}

			// Update timestamp for terminal status
			now := time.Now()
			if deployment.CompletedAt == nil {
				deployment.CompletedAt = &now
			}
		}

		// Persist to StateStore if available (must be done while holding the lock)
		// Also persist the result so it survives restarts
		t.persistDeploymentWithResult(deployment, &r)
	}

	return nil
}

// SetError updates the error for a deployment and marks it as failed
func (t *Tracker) SetError(deploymentID string, err error) error {
	if deploymentID == "" {
		return fmt.Errorf("deployment ID is empty")
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	deployment, exists := t.deployments[deploymentID]
	if !exists {
		return fmt.Errorf("deployment %s not found", deploymentID)
	}

	deployment.LastError = err

	// Update status to failed if not already in a terminal state
	if deployment.Status != interfaces.DeploymentStatusFailed &&
		deployment.Status != interfaces.DeploymentStatusCompleted &&
		deployment.Status != interfaces.DeploymentStatusCanceled {
		deployment.Status = interfaces.DeploymentStatusFailed

		// Update timestamp for failed status
		now := time.Now()
		if deployment.CompletedAt == nil {
			deployment.CompletedAt = &now
		}
	}

	// Persist to StateStore if available (must be done while holding the lock)
	t.persistDeployment(deployment)

	return nil
}

// matchesFilter checks if a deployment matches the given filter criteria
func matchesFilter(deployment *interfaces.QueuedDeployment, filter interfaces.DeploymentFilter) bool {
	// Check status filter
	if len(filter.Status) > 0 {
		statusMatches := false
		for _, status := range filter.Status {
			if deployment.Status == status {
				statusMatches = true
				break
			}
		}
		if !statusMatches {
			return false
		}
	}

	// Check created after
	if !filter.CreatedAfter.IsZero() && deployment.CreatedAt.Before(filter.CreatedAfter) {
		return false
	}

	// Check created before
	if !filter.CreatedBefore.IsZero() && deployment.CreatedAt.After(filter.CreatedBefore) {
		return false
	}

	return true
}

// Load restores deployments from StateStore into the tracker
func (t *Tracker) Load(stateStore interfaces.StateStore) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	// Store StateStore reference for future persistence operations
	t.stateStore = stateStore

	if stateStore == nil {
		// StateStore is optional, so this is not an error
		return nil
	}

	// Get list of deployments from StateStore
	ctx := context.Background()
	deployments, err := stateStore.ListDeployments(ctx)
	if err != nil {
		return fmt.Errorf("failed to list deployments from StateStore: %w", err)
	}

	if len(deployments) == 0 {
		// No deployments to load
		return nil
	}

	// Load each deployment
	for _, deploymentMetadata := range deployments {
		if err := t.loadSingleDeployment(deploymentMetadata); err != nil {
			t.logger.Warn("Failed to load deployment %s: %v", deploymentMetadata.DeploymentID, err)
		}
	}

	t.logger.Info("Loaded %d deployments from StateStore", len(deployments))
	return nil
}

// loadSingleDeployment loads a single deployment from metadata (helper for Load)
func (t *Tracker) loadSingleDeployment(deploymentMetadata *interfaces.DeploymentMetadata) error {
	// Convert DeploymentMetadata to QueuedDeployment
	deployment, err := t.metadataToQueuedDeployment(deploymentMetadata)
	if err != nil {
		return fmt.Errorf("failed to convert deployment metadata: %w", err)
	}

	// Store deployment in tracker (overwrite any existing deployment with same ID)
	t.deployments[deployment.ID] = deployment

	// Load deployment result if it was persisted in Configuration
	if deploymentMetadata.Configuration != nil {
		if resultData, ok := deploymentMetadata.Configuration["__result__"].(map[string]interface{}); ok {
			result := t.loadResultFromData(deployment.ID, resultData)
			t.results[deployment.ID] = result
			t.logger.Debug("Loaded result for deployment %s: Success=%v, Resources=%d items",
				deployment.ID, result.Success, len(result.Resources))
		}
	}

	return nil
}

// loadResultFromData reconstructs a DeploymentResult from stored data
func (t *Tracker) loadResultFromData(deploymentID string, resultData map[string]interface{}) *interfaces.DeploymentResult {
	result := &interfaces.DeploymentResult{
		DeploymentID: deploymentID,
	}

	if success, ok := resultData["success"].(bool); ok {
		result.Success = success
	}
	if errStr, ok := resultData["error"].(string); ok && errStr != "" {
		result.Error = fmt.Errorf("%s", errStr)
	}
	if resources, ok := resultData["resources"].(map[string]interface{}); ok {
		result.Resources = resources
	}
	if outputs, ok := resultData["outputs"].(map[string]interface{}); ok {
		result.Outputs = outputs
	}
	switch v := resultData["completed_at"].(type) {
	case string:
		if completedAt, err := time.Parse(time.RFC3339, v); err == nil {
			result.CompletedAt = completedAt
		}
	case time.Time:
		result.CompletedAt = v
	}

	return result
}

// deserializeDeployment converts state map to QueuedDeployment
func (t *Tracker) deserializeDeployment(data map[string]interface{}) (*interfaces.QueuedDeployment, error) {
	// Use the same combined struct for efficient deserialization
	var combined persistenceData

	// Single marshal to JSON then unmarshal to struct - more efficient than map conversion
	jsonData, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal deployment data: %w", err)
	}

	if err := json.Unmarshal(jsonData, &combined); err != nil {
		return nil, fmt.Errorf("failed to unmarshal deployment: %w", err)
	}

	// Return the deployment portion
	if combined.QueuedDeployment == nil {
		return nil, fmt.Errorf("no deployment data found in persistence")
	}

	return combined.QueuedDeployment, nil
}

// deserializeResult converts state data to DeploymentResult
func (t *Tracker) deserializeResult(data interface{}) (*interfaces.DeploymentResult, error) {
	if data == nil {
		return nil, nil
	}

	// Handle different input types efficiently
	switch v := data.(type) {
	case *interfaces.DeploymentResult:
		// Already the correct type, return a copy
		return v, nil
	case interfaces.DeploymentResult:
		// Value type, return address
		return &v, nil
	case map[string]interface{}:
		// Map type - use direct unmarshal approach
		var result interfaces.DeploymentResult
		jsonData, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal result data: %w", err)
		}
		if err := json.Unmarshal(jsonData, &result); err != nil {
			return nil, fmt.Errorf("failed to unmarshal result: %w", err)
		}
		return &result, nil
	default:
		// Unknown type - fall back to generic marshal/unmarshal
		jsonData, err := json.Marshal(data)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal result data of type %T: %w", data, err)
		}
		var result interfaces.DeploymentResult
		if err := json.Unmarshal(jsonData, &result); err != nil {
			return nil, fmt.Errorf("failed to unmarshal result: %w", err)
		}
		return &result, nil
	}
}

// persistenceData is a combined struct that includes both deployment and optional result
type persistenceData struct {
	*interfaces.QueuedDeployment
	Result *interfaces.DeploymentResult `json:"result,omitempty"`
}

// serializeDeployment converts QueuedDeployment to state map for persistence
func (t *Tracker) serializeDeployment(deployment *interfaces.QueuedDeployment, result *interfaces.DeploymentResult) (map[string]interface{}, error) {
	// Use combined struct to eliminate double JSON marshaling
	combined := persistenceData{
		QueuedDeployment: deployment,
		Result:           result,
	}

	// Single marshal to JSON then unmarshal to map
	jsonData, err := json.Marshal(combined)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal persistence data: %w", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(jsonData, &data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal to map: %w", err)
	}

	return data, nil
}

// persistDeployment saves a deployment to StateStore if available
func (t *Tracker) persistDeployment(deployment *interfaces.QueuedDeployment) {
	// Check if we have a result for this deployment in memory
	// and preserve it when persisting
	var result *interfaces.DeploymentResult
	if t.results != nil {
		if r, exists := t.results[deployment.ID]; exists {
			result = r
		}
	}
	t.persistDeploymentWithResult(deployment, result)
}

// persistDeploymentWithResult saves a deployment and optionally its result to StateStore
func (t *Tracker) persistDeploymentWithResult(deployment *interfaces.QueuedDeployment, result *interfaces.DeploymentResult) {
	if t.stateStore == nil {
		return
	}

	// Convert QueuedDeployment to DeploymentMetadata, including result if provided
	metadata := t.queuedDeploymentToMetadata(deployment)

	// If result is provided, store it in the Configuration field
	if result != nil {
		if metadata.Configuration == nil {
			metadata.Configuration = make(map[string]interface{})
		}
		// Store the entire result in configuration so it can be restored on Load
		resultData := map[string]interface{}{
			"deployment_id": result.DeploymentID,
			"success":       result.Success,
			"resources":     result.Resources,
			"outputs":       result.Outputs,
		}
		// Convert error to string for serialization
		if result.Error != nil {
			resultData["error"] = result.Error.Error()
		}
		// Store completed_at as RFC3339 string for serialization
		if !result.CompletedAt.IsZero() {
			resultData["completed_at"] = result.CompletedAt.Format(time.RFC3339)
		}
		metadata.Configuration["__result__"] = resultData
	}

	ctx := context.Background()

	// Try to get existing deployment first
	existing, err := t.stateStore.GetDeployment(ctx, deployment.ID)
	if err != nil {
		// Deployment doesn't exist, create it
		if err := t.stateStore.CreateDeployment(ctx, metadata); err != nil {
			t.logger.Warn("Failed to create deployment %s in StateStore: %v", deployment.ID, err)
		}
	} else if existing != nil {
		// Deployment exists, we need to update it with the result
		// Since StateStore only has UpdateDeploymentStatus and not a full update method,
		// we need to delete and recreate to persist the Configuration with the result
		metadata.CreatedAt = existing.CreatedAt // Preserve original creation time

		// Delete the existing deployment
		if err := t.stateStore.DeleteDeployment(ctx, deployment.ID); err != nil {
			t.logger.Warn("Failed to delete deployment %s for update: %v", deployment.ID, err)
			return
		}

		// Recreate with updated metadata including result
		if err := t.stateStore.CreateDeployment(ctx, metadata); err != nil {
			t.logger.Warn("Failed to recreate deployment %s with result: %v", deployment.ID, err)
		}
	}
}

// metadataToQueuedDeployment converts DeploymentMetadata to QueuedDeployment
func (t *Tracker) metadataToQueuedDeployment(metadata *interfaces.DeploymentMetadata) (*interfaces.QueuedDeployment, error) {
	if metadata == nil {
		return nil, fmt.Errorf("metadata is nil")
	}

	deployment := &interfaces.QueuedDeployment{
		ID:        metadata.DeploymentID,
		Status:    metadata.Status,
		CreatedAt: metadata.CreatedAt,
		// Configuration would need to be handled separately or stored differently
		// LastError:   // Not available in metadata
		// StartedAt:   // Not available in metadata
		// CompletedAt: // Could derive from LastAppliedAt
	}

	if metadata.LastAppliedAt != nil {
		deployment.CompletedAt = metadata.LastAppliedAt
	}

	return deployment, nil
}

// queuedDeploymentToMetadata converts QueuedDeployment to DeploymentMetadata
func (t *Tracker) queuedDeploymentToMetadata(deployment *interfaces.QueuedDeployment) *interfaces.DeploymentMetadata {
	if deployment == nil {
		return nil
	}

	metadata := &interfaces.DeploymentMetadata{
		DeploymentID:  deployment.ID,
		Status:        deployment.Status,
		CreatedAt:     deployment.CreatedAt,
		UpdatedAt:     time.Now(),
		Configuration: make(map[string]interface{}), // Will be populated if needed
		ResourceCount: 0,                            // Would need to be calculated
	}

	// Preserve existing Configuration from Request metadata if it exists
	if deployment.Request != nil && deployment.Request.Metadata != nil {
		if config, ok := deployment.Request.Metadata["configuration"].(map[string]interface{}); ok {
			metadata.Configuration = config
		}
	}

	if deployment.CompletedAt != nil {
		metadata.LastAppliedAt = deployment.CompletedAt
	}

	if deployment.LastError != nil {
		metadata.ErrorMessage = deployment.LastError.Error()
	}

	return metadata
}
