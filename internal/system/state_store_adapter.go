package system

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/internal/utils"
	"github.com/lattiam/lattiam/internal/utils/fsutil"
)

// RobustFileStateStoreAdapter is a production-grade file-based state store
// that implements atomic writes, proper locking, and state versioning
type RobustFileStateStoreAdapter struct {
	baseDir         string
	locks           map[string]*RobustFileLock
	locksMutex      sync.RWMutex
	deployments     map[string]*DeploymentMeta
	metaMutex       sync.RWMutex
	dirMutex        sync.Mutex             // Synchronize directory creation
	deploymentMutex map[string]*sync.Mutex // Per-deployment write synchronization
	mutexesMutex    sync.RWMutex           // Protect deployment mutex map
}

// DeploymentMeta tracks state metadata for conflict detection
type DeploymentMeta struct {
	Serial  uint64    `json:"serial"`
	Lineage string    `json:"lineage"`
	Updated time.Time `json:"updated"`
	Version int       `json:"version"`
}

// RobustFileLock provides cross-platform file locking
type RobustFileLock struct {
	id           string
	deploymentID string
	createdAt    time.Time
	lockFile     *os.File
	lockPath     string
	metaPath     string
}

// NewRobustFileStateStoreAdapter creates a production-ready file state store
func NewRobustFileStateStoreAdapter(baseDir string) (*RobustFileStateStoreAdapter, error) {
	// Ensure all required directories exist
	dirs := []string{
		baseDir,
		filepath.Join(baseDir, "deployments"),
		filepath.Join(baseDir, "resources"),
		filepath.Join(baseDir, "locks"),
		filepath.Join(baseDir, "meta"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	adapter := &RobustFileStateStoreAdapter{
		baseDir:         baseDir,
		locks:           make(map[string]*RobustFileLock),
		deployments:     make(map[string]*DeploymentMeta),
		deploymentMutex: make(map[string]*sync.Mutex),
	}

	// Load existing metadata
	if err := adapter.loadDeploymentMetadata(); err != nil {
		return nil, fmt.Errorf("failed to load deployment metadata: %w", err)
	}

	return adapter, nil
}

// atomicWriteJSON writes data atomically using temp file + rename pattern
func (f *RobustFileStateStoreAdapter) atomicWriteJSON(filePath string, data interface{}) error {
	// Marshal data
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal data: %w", err)
	}

	// Ensure parent directory exists with synchronization to prevent race conditions
	parentDir := filepath.Dir(filePath)
	f.dirMutex.Lock()
	if err := os.MkdirAll(parentDir, 0o700); err != nil {
		f.dirMutex.Unlock()
		return fmt.Errorf("failed to create parent directory: %w", err)
	}
	f.dirMutex.Unlock()

	// Write to temporary file
	tempPath := filePath + ".tmp"
	if err := os.WriteFile(tempPath, jsonData, 0o600); err != nil {
		return fmt.Errorf("failed to write temporary file: %w", err)
	}

	// Atomic rename (this is the critical atomic operation)
	if err := os.Rename(tempPath, filePath); err != nil {
		_ = os.Remove(tempPath) // Clean up on failure
		return fmt.Errorf("failed to commit atomic write: %w", err)
	}

	return nil
}

// generateLineage creates a unique lineage identifier
func (f *RobustFileStateStoreAdapter) generateLineage() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		// Fall back to timestamp-based lineage if random read fails
		return fmt.Sprintf("lineage-%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x-%x-%x-%x-%x", bytes[0:4], bytes[4:6], bytes[6:8], bytes[8:10], bytes[10:16])
}

// loadDeploymentMetadata loads existing deployment metadata from disk
func (f *RobustFileStateStoreAdapter) loadDeploymentMetadata() error {
	metaDir := filepath.Join(f.baseDir, "meta")
	entries, err := os.ReadDir(metaDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No metadata directory yet
		}
		return fmt.Errorf("failed to read metadata directory: %w", err)
	}

	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".meta.json") {
			continue
		}

		deploymentID := strings.TrimSuffix(entry.Name(), ".meta.json")
		metaPath := filepath.Join(metaDir, entry.Name())

		data, err := os.ReadFile(metaPath) // #nosec G304 - metaPath is constructed from controlled baseDir
		if err != nil {
			continue // Skip problematic metadata files
		}

		var meta DeploymentMeta
		if err := json.Unmarshal(data, &meta); err != nil {
			continue // Skip corrupted metadata
		}

		f.metaMutex.Lock()
		f.deployments[deploymentID] = &meta
		f.metaMutex.Unlock()
	}

	return nil
}

// saveDeploymentMetadata saves deployment metadata atomically
func (f *RobustFileStateStoreAdapter) saveDeploymentMetadata(deploymentID string, meta *DeploymentMeta) error {
	metaPath := filepath.Join(f.baseDir, "meta", deploymentID+".meta.json")
	return f.atomicWriteJSON(metaPath, meta)
}

// getOrCreateDeploymentMutex gets or creates a per-deployment mutex
func (f *RobustFileStateStoreAdapter) getOrCreateDeploymentMutex(deploymentID string) *sync.Mutex {
	f.mutexesMutex.RLock()
	if mutex, exists := f.deploymentMutex[deploymentID]; exists {
		f.mutexesMutex.RUnlock()
		return mutex
	}
	f.mutexesMutex.RUnlock()

	f.mutexesMutex.Lock()
	defer f.mutexesMutex.Unlock()

	// Double-check after acquiring write lock
	if mutex, exists := f.deploymentMutex[deploymentID]; exists {
		return mutex
	}

	// Create new mutex
	mutex := &sync.Mutex{}
	f.deploymentMutex[deploymentID] = mutex
	return mutex
}

// getOrCreateDeploymentMeta gets existing metadata or creates new metadata
func (f *RobustFileStateStoreAdapter) getOrCreateDeploymentMeta(deploymentID string) *DeploymentMeta {
	f.metaMutex.Lock()
	defer f.metaMutex.Unlock()

	if meta, exists := f.deployments[deploymentID]; exists {
		return meta
	}

	// Create new metadata
	meta := &DeploymentMeta{
		Serial:  0,
		Lineage: f.generateLineage(),
		Updated: time.Now().UTC(),
		Version: 1,
	}

	f.deployments[deploymentID] = meta
	return meta
}

// Deployment-level operations

// UpdateDeploymentState updates the state for a deployment
func (f *RobustFileStateStoreAdapter) UpdateDeploymentState(deploymentID string, state map[string]interface{}) error {
	// Use per-deployment mutex to prevent concurrent writes to the same deployment
	deploymentMutex := f.getOrCreateDeploymentMutex(deploymentID)
	deploymentMutex.Lock()
	defer deploymentMutex.Unlock()

	// Get or create metadata
	meta := f.getOrCreateDeploymentMeta(deploymentID)

	// Increment serial number for change tracking
	f.metaMutex.Lock()
	meta.Serial++
	meta.Updated = time.Now().UTC()
	f.metaMutex.Unlock()

	// Create versioned state
	versionedState := map[string]interface{}{
		"serial":     meta.Serial,
		"lineage":    meta.Lineage,
		"version":    meta.Version,
		"updated_at": meta.Updated.Format(time.RFC3339),
		"state":      state,
	}

	// Write state atomically
	statePath := filepath.Join(f.baseDir, "deployments", deploymentID+".json")
	if err := f.atomicWriteJSON(statePath, versionedState); err != nil {
		return fmt.Errorf("failed to save deployment state: %w", err)
	}

	// Save metadata atomically
	if err := f.saveDeploymentMetadata(deploymentID, meta); err != nil {
		return fmt.Errorf("failed to save deployment metadata: %w", err)
	}

	return nil
}

// GetDeploymentState retrieves the state for a deployment
func (f *RobustFileStateStoreAdapter) GetDeploymentState(deploymentID string) (map[string]interface{}, error) {
	statePath := filepath.Join(f.baseDir, "deployments", deploymentID+".json")

	data, err := os.ReadFile(statePath) // #nosec G304 - statePath is constructed from controlled baseDir
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("deployment %s not found", deploymentID)
		}
		return nil, fmt.Errorf("failed to read deployment state: %w", err)
	}

	var versionedState map[string]interface{}
	if err := json.Unmarshal(data, &versionedState); err != nil {
		return nil, fmt.Errorf("failed to parse deployment state: %w", err)
	}

	// Extract the actual state from versioned wrapper
	if state, ok := versionedState["state"].(map[string]interface{}); ok {
		return state, nil
	}

	// Handle unversioned state format by returning the data directly
	return versionedState, nil
}

// DeleteDeploymentState removes a deployment's state
func (f *RobustFileStateStoreAdapter) DeleteDeploymentState(deploymentID string) error {
	// Remove deployment file
	deploymentFile := filepath.Join(f.baseDir, "deployments", deploymentID+".json")
	if err := os.Remove(deploymentFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete deployment state: %w", err)
	}

	// Remove metadata
	metaFile := filepath.Join(f.baseDir, "meta", deploymentID+".meta.json")
	if err := os.Remove(metaFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete deployment metadata: %w", err)
	}

	// Remove from memory
	f.metaMutex.Lock()
	delete(f.deployments, deploymentID)
	f.metaMutex.Unlock()

	// Clean up associated resource states
	resourcePrefix := deploymentID + "_"
	resourceDir := filepath.Join(f.baseDir, "resources")
	entries, err := os.ReadDir(resourceDir)
	if err == nil {
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), resourcePrefix) {
				_ = os.Remove(filepath.Join(resourceDir, entry.Name()))
			}
		}
	}

	return nil
}

// Resource-level operations (simplified - delegate to wrapped implementation)

// UpdateResourceState updates the state for a resource
func (f *RobustFileStateStoreAdapter) UpdateResourceState(resourceKey string, state map[string]interface{}) error {
	// Add atomic write to resource operations
	// Resource keys use dot notation (e.g., "aws_s3_bucket.demo")
	// which is filesystem-safe and doesn't need escaping
	resourcePath := filepath.Join(f.baseDir, "resources", resourceKey+".json")
	return f.atomicWriteJSON(resourcePath, state)
}

// GetResourceState retrieves the state for a resource
func (f *RobustFileStateStoreAdapter) GetResourceState(resourceKey string) (map[string]interface{}, error) {
	// Resource keys use dot notation (e.g., "aws_s3_bucket.demo")
	// which is filesystem-safe and doesn't need escaping
	resourcePath := filepath.Join(f.baseDir, "resources", resourceKey+".json")

	data, err := os.ReadFile(resourcePath) // #nosec G304 - resourcePath is constructed from controlled baseDir
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read resource state: %w", err)
	}

	var state map[string]interface{}
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("failed to parse resource state: %w", err)
	}

	return state, nil
}

// GetAllResourceStates retrieves all resource states for a deployment
func (f *RobustFileStateStoreAdapter) GetAllResourceStates(deploymentID string) (map[string]map[string]interface{}, error) {
	resourceDir := filepath.Join(f.baseDir, "resources")
	entries, err := os.ReadDir(resourceDir)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]map[string]interface{}), nil
		}
		return nil, fmt.Errorf("failed to read resource directory: %w", err)
	}

	result := make(map[string]map[string]interface{})
	prefix := deploymentID + "_"

	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), prefix) || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		fullKey := strings.TrimSuffix(entry.Name(), ".json")
		state, err := f.GetResourceState(fullKey)
		if err == nil && state != nil {
			// Remove deployment ID prefix to get the actual resource key
			resourceKey := strings.TrimPrefix(fullKey, prefix)
			// Resource keys already use dot notation, no conversion needed
			result[resourceKey] = state
		}
	}

	return result, nil
}

// DeleteResourceState removes a resource's state
func (f *RobustFileStateStoreAdapter) DeleteResourceState(resourceKey string) error {
	// Resource keys use dot notation (e.g., "aws_s3_bucket.demo")
	// which is filesystem-safe and doesn't need escaping
	resourcePath := filepath.Join(f.baseDir, "resources", resourceKey+".json")
	if err := os.Remove(resourcePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete resource state: %w", err)
	}
	return nil
}

// UpdateResourceStatesBatch updates multiple resource states in a batch operation
func (f *RobustFileStateStoreAdapter) UpdateResourceStatesBatch(states map[string]map[string]interface{}) error {
	for key, state := range states {
		if err := f.UpdateResourceState(key, state); err != nil {
			return fmt.Errorf("failed to update resource %s in batch: %w", key, err)
		}
	}
	return nil
}

// GetResourceStatesBatch retrieves multiple resource states in a batch
func (f *RobustFileStateStoreAdapter) GetResourceStatesBatch(resourceKeys []string) (map[string]map[string]interface{}, error) {
	result := make(map[string]map[string]interface{})

	for _, key := range resourceKeys {
		state, err := f.GetResourceState(key)
		if err == nil && state != nil {
			result[key] = state
		}
	}

	return result, nil
}

// Robust file locking implementation

func (f *RobustFileStateStoreAdapter) lockDeployment(deploymentID string) (interfaces.StateLock, error) {
	f.locksMutex.Lock()
	defer f.locksMutex.Unlock()

	// Check if already locked
	if _, exists := f.locks[deploymentID]; exists {
		return nil, fmt.Errorf("deployment %s is already locked", deploymentID)
	}

	lockDir := filepath.Join(f.baseDir, "locks")
	lockPath := filepath.Join(lockDir, deploymentID+".lock")
	metaPath := filepath.Join(lockDir, deploymentID+".lock.info")

	// Create lock file with exclusive access
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600) // #nosec G304 - lockPath is constructed from controlled baseDir
	if err != nil {
		if os.IsExist(err) {
			return nil, fmt.Errorf("deployment %s is already locked by another process", deploymentID)
		}
		return nil, fmt.Errorf("failed to create lock file: %w", err)
	}

	// Apply platform-specific file lock
	if err := lockFile(file); err != nil {
		_ = file.Close()
		_ = os.Remove(lockPath)
		return nil, fmt.Errorf("failed to acquire file lock: %w", err)
	}

	// Create lock object
	lockID := fmt.Sprintf("lock-%s-%d", deploymentID, time.Now().UnixNano())
	lock := &RobustFileLock{
		id:           lockID,
		deploymentID: deploymentID,
		createdAt:    time.Now().UTC(),
		lockFile:     file,
		lockPath:     lockPath,
		metaPath:     metaPath,
	}

	// Write lock metadata
	lockInfo := map[string]interface{}{
		"id":            lockID,
		"deployment_id": deploymentID,
		"created_at":    lock.createdAt.Format(time.RFC3339),
		"process_id":    os.Getpid(),
		"hostname":      getHostname(),
	}

	if err := f.atomicWriteJSON(metaPath, lockInfo); err != nil {
		_ = unlockFile(file)
		_ = file.Close()
		_ = os.Remove(lockPath)
		return nil, fmt.Errorf("failed to write lock metadata: %w", err)
	}

	// Store lock
	f.locks[deploymentID] = lock
	return lock, nil
}

func (f *RobustFileStateStoreAdapter) unlockDeployment(lock interfaces.StateLock) error {
	f.locksMutex.Lock()
	defer f.locksMutex.Unlock()

	robustLock, ok := lock.(*RobustFileLock)
	if !ok {
		return fmt.Errorf("invalid lock type")
	}

	// Remove from locks map
	delete(f.locks, robustLock.deploymentID)

	// Release the lock
	return robustLock.Release()
}

// RobustFileLock methods

// ID returns the lock ID
func (l *RobustFileLock) ID() string { return l.id }

// DeploymentID returns the deployment ID for this lock
func (l *RobustFileLock) DeploymentID() string { return l.deploymentID }

// CreatedAt returns when the lock was created
func (l *RobustFileLock) CreatedAt() time.Time { return l.createdAt }

// Release releases the lock
func (l *RobustFileLock) Release() error {
	// Release platform-specific lock
	if err := unlockFile(l.lockFile); err != nil {
		return fmt.Errorf("failed to release file lock: %w", err)
	}

	// Close and remove lock file
	_ = l.lockFile.Close()
	_ = os.Remove(l.lockPath)
	_ = os.Remove(l.metaPath)

	return nil
}

// Terraform state compatibility (simplified)

// ExportTerraformState exports a deployment's state in Terraform format
func (f *RobustFileStateStoreAdapter) ExportTerraformState(deploymentID string) (*interfaces.TerraformState, error) {
	// Verify deployment exists
	_, err := f.GetDeploymentState(deploymentID)
	if err != nil {
		return nil, err
	}

	resourceStates, err := f.GetAllResourceStates(deploymentID)
	if err != nil {
		return nil, err
	}

	meta := f.getOrCreateDeploymentMeta(deploymentID)

	tfState := &interfaces.TerraformState{
		Version:          4,
		TerraformVersion: "1.5.0",
		Serial:           meta.Serial,
		Lineage:          meta.Lineage,
		Outputs:          make(map[string]interface{}),
		Resources:        []interfaces.TerraformResource{},
	}

	// Convert resources to Terraform format
	for resourceKey, state := range resourceStates {
		// Resource keys should have deployment ID prefix now
		keyWithoutPrefix := strings.TrimPrefix(resourceKey, deploymentID+"_")
		// Use ParseResourceKey to handle dot notation
		resourceType, resourceName := interfaces.ParseResourceKey(interfaces.ResourceKey(keyWithoutPrefix))
		if resourceType == "" || resourceName == "" {
			continue
		}

		tfResource := interfaces.TerraformResource{
			Mode:     "managed",
			Type:     resourceType,
			Name:     resourceName,
			Provider: fmt.Sprintf("provider[%q]", utils.ExtractProviderName(resourceType)),
			Instances: []interfaces.TerraformResourceInstance{
				{
					SchemaVersion: 1,
					Attributes:    state,
				},
			},
		}

		tfState.Resources = append(tfState.Resources, tfResource)
	}

	return tfState, nil
}

// ImportTerraformState imports a Terraform state into the store
func (f *RobustFileStateStoreAdapter) ImportTerraformState(deploymentID string, state *interfaces.TerraformState) error {
	// Import state with proper versioning
	deploymentStateData := map[string]interface{}{
		"terraform_state": state,
		"imported_at":     time.Now().UTC().Format(time.RFC3339),
	}

	if err := f.UpdateDeploymentState(deploymentID, deploymentStateData); err != nil {
		return err
	}

	// Import individual resources
	for _, resource := range state.Resources {
		for i, instance := range resource.Instances {
			// Use MakeResourceKey to ensure consistent key format (with dot notation)
			baseKey := string(interfaces.MakeResourceKey(resource.Type, resource.Name))
			resourceKey := fmt.Sprintf("%s_%s", deploymentID, baseKey)
			if len(resource.Instances) > 1 {
				resourceKey = fmt.Sprintf("%s[%d]", resourceKey, i)
			}

			resourceState := map[string]interface{}{
				"type":           resource.Type,
				"name":           resource.Name,
				"provider":       resource.Provider,
				"schema_version": instance.SchemaVersion,
				"attributes":     instance.Attributes,
				"imported_at":    time.Now().UTC().Format(time.RFC3339),
			}

			if err := f.UpdateResourceState(resourceKey, resourceState); err != nil {
				return fmt.Errorf("failed to import resource %s: %w", resourceKey, err)
			}
		}
	}

	return nil
}

// Health and maintenance

func (f *RobustFileStateStoreAdapter) ping() error {
	testFile := filepath.Join(f.baseDir, ".ping")
	if err := f.atomicWriteJSON(testFile, map[string]interface{}{"ping": "ok"}); err != nil {
		return fmt.Errorf("state store not accessible: %w", err)
	}
	_ = os.Remove(testFile)
	return nil
}

// Cleanup removes old deployment states
func (f *RobustFileStateStoreAdapter) Cleanup(olderThan time.Duration) error {
	cutoff := time.Now().Add(-olderThan)

	// Clean up old lock files
	lockDir := filepath.Join(f.baseDir, "locks")
	if entries, err := os.ReadDir(lockDir); err == nil {
		for _, entry := range entries {
			if info, err := entry.Info(); err == nil {
				if info.ModTime().Before(cutoff) {
					_ = os.Remove(filepath.Join(lockDir, entry.Name()))
				}
			}
		}
	}

	return nil
}

// GetStorageInfo returns information about the storage backend
// GetStorageInfo returns storage information
func (f *RobustFileStateStoreAdapter) GetStorageInfo() *interfaces.StorageInfo {
	info := &interfaces.StorageInfo{
		Type:     "robust_file",
		Exists:   fsutil.DirExists(f.baseDir),
		Writable: fsutil.IsWritable(f.baseDir),
	}

	// Count deployments
	stateDir := filepath.Join(f.baseDir, "states")
	if entries, err := os.ReadDir(stateDir); err == nil {
		deploymentCount := 0
		totalSize := int64(0)

		for _, entry := range entries {
			if strings.HasSuffix(entry.Name(), ".json") {
				deploymentCount++
				if fileInfo, err := entry.Info(); err == nil {
					totalSize += fileInfo.Size()
				}
			}
		}

		info.DeploymentCount = deploymentCount
		info.TotalSizeBytes = totalSize
	}

	// Get disk usage percentage
	if usage, err := fsutil.GetDiskUsage(f.baseDir); err == nil {
		info.UsedPercent = usage.UsedPercent
	}

	return info
}

// Helper functions

func getHostname() string {
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "unknown"
	}
	return hostname
}

// New StateStore interface methods (adapter implementations)

// CreateDeployment creates a new deployment metadata entry
func (f *RobustFileStateStoreAdapter) CreateDeployment(_ context.Context, deploy *interfaces.DeploymentMetadata) error {
	// Convert to legacy format and store
	legacyState := map[string]interface{}{
		"deployment_id":  deploy.DeploymentID,
		"status":         deploy.Status,
		"created_at":     deploy.CreatedAt,
		"updated_at":     deploy.UpdatedAt,
		"configuration":  deploy.Configuration,
		"resource_count": deploy.ResourceCount,
	}
	if deploy.LastAppliedAt != nil {
		legacyState["last_applied_at"] = *deploy.LastAppliedAt
	}
	if deploy.ErrorMessage != "" {
		legacyState["error_message"] = deploy.ErrorMessage
	}

	return f.UpdateDeploymentState(deploy.DeploymentID, legacyState)
}

// GetDeployment retrieves deployment metadata by ID
func (f *RobustFileStateStoreAdapter) GetDeployment(_ context.Context, deploymentID string) (*interfaces.DeploymentMetadata, error) {
	legacyState, err := f.GetDeploymentState(deploymentID)
	if err != nil {
		return nil, err
	}

	// Convert from legacy format
	deploy := &interfaces.DeploymentMetadata{
		DeploymentID:  deploymentID,
		ResourceCount: 0,
	}

	if status, ok := legacyState["status"].(string); ok {
		deploy.Status = interfaces.DeploymentStatus(status)
	}

	if createdAt, ok := legacyState["created_at"].(time.Time); ok {
		deploy.CreatedAt = createdAt
	}

	if updatedAt, ok := legacyState["updated_at"].(time.Time); ok {
		deploy.UpdatedAt = updatedAt
	}

	if config, ok := legacyState["configuration"].(map[string]interface{}); ok {
		deploy.Configuration = config
	}

	if count, ok := legacyState["resource_count"].(int); ok {
		deploy.ResourceCount = count
	}

	if lastApplied, ok := legacyState["last_applied_at"].(time.Time); ok {
		deploy.LastAppliedAt = &lastApplied
	}

	if errMsg, ok := legacyState["error_message"].(string); ok {
		deploy.ErrorMessage = errMsg
	}

	return deploy, nil
}

// ListDeployments returns all deployment metadata (new interface)
func (f *RobustFileStateStoreAdapter) ListDeployments(_ context.Context) ([]*interfaces.DeploymentMetadata, error) {
	// Use legacy method to get deployment IDs
	deploymentIDs, err := f.listDeploymentIDs()
	if err != nil {
		return nil, err
	}

	var deployments []*interfaces.DeploymentMetadata
	for _, id := range deploymentIDs {
		if deploy, err := f.GetDeployment(context.Background(), id); err == nil {
			deployments = append(deployments, deploy)
		}
	}

	return deployments, nil
}

// listDeploymentIDs is the legacy ListDeployments method renamed
func (f *RobustFileStateStoreAdapter) listDeploymentIDs() ([]string, error) {
	f.metaMutex.RLock()
	defer f.metaMutex.RUnlock()

	stateDir := filepath.Join(f.baseDir, "deployments")
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("failed to read deployments directory: %w", err)
	}

	var deploymentIDs []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			deploymentID := strings.TrimSuffix(entry.Name(), ".json")
			deploymentIDs = append(deploymentIDs, deploymentID)
		}
	}

	return deploymentIDs, nil
}

// UpdateDeploymentStatus updates the status of a deployment
func (f *RobustFileStateStoreAdapter) UpdateDeploymentStatus(_ context.Context, deploymentID string, status interfaces.DeploymentStatus) error {
	// Get current state
	currentState, err := f.GetDeploymentState(deploymentID)
	if err != nil {
		return err
	}

	// Update status and updated_at
	currentState["status"] = string(status)
	currentState["updated_at"] = time.Now()

	return f.UpdateDeploymentState(deploymentID, currentState)
}

// DeleteDeployment deletes a deployment (updated signature)
func (f *RobustFileStateStoreAdapter) DeleteDeployment(_ context.Context, deploymentID string) error {
	// Use legacy method
	return f.DeleteDeploymentState(deploymentID)
}

// SaveTerraformState saves Terraform state data
func (f *RobustFileStateStoreAdapter) SaveTerraformState(_ context.Context, deploymentID string, stateData []byte) error {
	// Write to terraform state file
	stateFile := filepath.Join(f.baseDir, "terraform_states", fmt.Sprintf("%s.json", deploymentID))
	return f.atomicWriteJSON(stateFile, json.RawMessage(stateData))
}

// LoadTerraformState loads Terraform state data
func (f *RobustFileStateStoreAdapter) LoadTerraformState(_ context.Context, deploymentID string) ([]byte, error) {
	stateFile := filepath.Join(f.baseDir, "terraform_states", fmt.Sprintf("%s.json", deploymentID))
	data, err := os.ReadFile(stateFile) // #nosec G304 - stateFile is constructed from controlled baseDir
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("terraform state not found for deployment %s", deploymentID)
		}
		return nil, fmt.Errorf("failed to read terraform state: %w", err)
	}
	return data, nil
}

// DeleteTerraformState deletes Terraform state data
func (f *RobustFileStateStoreAdapter) DeleteTerraformState(_ context.Context, deploymentID string) error {
	stateFile := filepath.Join(f.baseDir, "terraform_states", fmt.Sprintf("%s.json", deploymentID))
	if err := os.Remove(stateFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete terraform state: %w", err)
	}
	return nil
}

// LockDeployment creates a deployment lock (new interface signature)
func (f *RobustFileStateStoreAdapter) LockDeployment(_ context.Context, deploymentID string) (interfaces.StateLock, error) {
	// Use legacy implementation
	return f.lockDeployment(deploymentID)
}

// UnlockDeployment removes a deployment lock (new interface signature)
func (f *RobustFileStateStoreAdapter) UnlockDeployment(_ context.Context, lock interfaces.StateLock) error {
	// Use legacy implementation
	return f.unlockDeployment(lock)
}

// Ping checks if the store is accessible (new interface signature)
func (f *RobustFileStateStoreAdapter) Ping(_ context.Context) error {
	// Use legacy implementation
	return f.ping()
}

// Ensure RobustFileStateStoreAdapter implements StateStore interface
var _ interfaces.StateStore = (*RobustFileStateStoreAdapter)(nil)
