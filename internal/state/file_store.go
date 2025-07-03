package state

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// FileStore implements Store using JSON files
type FileStore struct {
	baseDir string
	mu      sync.RWMutex
	locks   map[string]*Lock
	locksMu sync.Mutex
	closed  bool
}

// NewFileStore creates a new file-based state store
func NewFileStore(baseDir string) (*FileStore, error) {
	// Expand ~ to home directory
	if strings.HasPrefix(baseDir, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		baseDir = filepath.Join(home, baseDir[2:])
	}

	// Create base directory if it doesn't exist
	if err := os.MkdirAll(baseDir, 0o750); err != nil {
		return nil, fmt.Errorf("failed to create state directory: %w", err)
	}

	// Create deployments subdirectory
	deploymentsDir := filepath.Join(baseDir, "deployments")
	if err := os.MkdirAll(deploymentsDir, 0o750); err != nil {
		return nil, fmt.Errorf("failed to create deployments directory: %w", err)
	}

	return &FileStore{
		baseDir: baseDir,
		locks:   make(map[string]*Lock),
	}, nil
}

// Save creates or updates a deployment
func (fs *FileStore) Save(_ context.Context, deployment *DeploymentState) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	if fs.closed {
		return ErrStoreClosed
	}

	if deployment.ID == "" {
		return ErrInvalidDeploymentID
	}

	deployment.UpdatedAt = time.Now()

	// Check if deployment already exists
	filePath := fs.deploymentPath(deployment.ID)
	if _, err := os.Stat(filePath); err == nil {
		// File exists, this is an update
		return fs.writeDeployment(deployment)
	}

	// New deployment
	deployment.CreatedAt = time.Now()
	return fs.writeDeployment(deployment)
}

// Load retrieves a deployment by ID
func (fs *FileStore) Load(_ context.Context, id string) (*DeploymentState, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	if fs.closed {
		return nil, ErrStoreClosed
	}

	if id == "" {
		return nil, ErrInvalidDeploymentID
	}

	filePath := fs.deploymentPath(id)
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrDeploymentNotFound
		}
		return nil, fmt.Errorf("failed to read deployment file: %w", err)
	}

	var deployment DeploymentState
	if err := json.Unmarshal(data, &deployment); err != nil {
		return nil, fmt.Errorf("failed to unmarshal deployment: %w", err)
	}

	return &deployment, nil
}

// Update modifies an existing deployment
func (fs *FileStore) Update(_ context.Context, deployment *DeploymentState) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	if fs.closed {
		return ErrStoreClosed
	}

	if deployment.ID == "" {
		return ErrInvalidDeploymentID
	}

	// Check if deployment exists
	filePath := fs.deploymentPath(deployment.ID)
	if _, err := os.Stat(filePath); err != nil {
		if os.IsNotExist(err) {
			return ErrDeploymentNotFound
		}
		return fmt.Errorf("failed to check deployment existence: %w", err)
	}

	deployment.UpdatedAt = time.Now()
	return fs.writeDeployment(deployment)
}

// Delete removes a deployment
func (fs *FileStore) Delete(_ context.Context, id string) error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	if fs.closed {
		return ErrStoreClosed
	}

	if id == "" {
		return ErrInvalidDeploymentID
	}

	// Remove any locks
	fs.locksMu.Lock()
	delete(fs.locks, id)
	fs.locksMu.Unlock()

	filePath := fs.deploymentPath(id)
	if err := os.Remove(filePath); err != nil {
		if os.IsNotExist(err) {
			return ErrDeploymentNotFound
		}
		return fmt.Errorf("failed to delete deployment file: %w", err)
	}

	return nil
}

// List returns all deployments, optionally filtered by status
func (fs *FileStore) List(_ context.Context, status string) ([]*DeploymentState, error) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	if fs.closed {
		return nil, ErrStoreClosed
	}

	deploymentsDir := filepath.Join(fs.baseDir, "deployments")
	entries, err := os.ReadDir(deploymentsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read deployments directory: %w", err)
	}

	// Pre-allocate with capacity for all potential JSON files
	jsonFileCount := 0
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			jsonFileCount++
		}
	}
	deployments := make([]*DeploymentState, 0, jsonFileCount)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}

		filePath := filepath.Join(deploymentsDir, entry.Name())
		data, err := os.ReadFile(filePath)
		if err != nil {
			// Skip files we can't read
			continue
		}

		var deployment DeploymentState
		if err := json.Unmarshal(data, &deployment); err != nil {
			// Skip invalid JSON files
			continue
		}

		// Apply status filter if provided
		if status != "" && deployment.Status != status {
			continue
		}

		deployments = append(deployments, &deployment)
	}

	return deployments, nil
}

// Lock acquires an exclusive lock on a deployment
func (fs *FileStore) Lock(_ context.Context, deploymentID, holderID string, duration time.Duration) (*Lock, error) {
	fs.locksMu.Lock()
	defer fs.locksMu.Unlock()

	if fs.closed {
		return nil, ErrStoreClosed
	}

	// Check if lock already exists and is not expired
	if existing, exists := fs.locks[deploymentID]; exists {
		if time.Now().Before(existing.ExpiresAt) {
			return nil, ErrLockAlreadyHeld
		}
		// Lock expired, remove it
		delete(fs.locks, deploymentID)
	}

	lock := &Lock{
		DeploymentID: deploymentID,
		HolderID:     holderID,
		AcquiredAt:   time.Now(),
		ExpiresAt:    time.Now().Add(duration),
	}

	fs.locks[deploymentID] = lock
	return lock, nil
}

// Unlock releases a deployment lock
func (fs *FileStore) Unlock(_ context.Context, deploymentID, holderID string) error {
	fs.locksMu.Lock()
	defer fs.locksMu.Unlock()

	if fs.closed {
		return ErrStoreClosed
	}

	lock, exists := fs.locks[deploymentID]
	if !exists {
		return ErrLockNotAcquired
	}

	if lock.HolderID != holderID {
		return ErrLockNotAcquired
	}

	delete(fs.locks, deploymentID)
	return nil
}

// Close closes the store and releases resources
func (fs *FileStore) Close() error {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	if fs.closed {
		return nil
	}

	fs.closed = true
	fs.locks = nil
	return nil
}

// deploymentPath returns the file path for a deployment
func (fs *FileStore) deploymentPath(id string) string {
	// Sanitize ID to prevent directory traversal
	safeID := strings.ReplaceAll(id, "/", "_")
	safeID = strings.ReplaceAll(safeID, "..", "_")
	return filepath.Join(fs.baseDir, "deployments", safeID+".json")
}

// writeDeployment atomically writes a deployment to disk
func (fs *FileStore) writeDeployment(deployment *DeploymentState) error {
	data, err := json.MarshalIndent(deployment, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal deployment: %w", err)
	}

	filePath := fs.deploymentPath(deployment.ID)
	tempPath := filePath + ".tmp"

	// Write to temporary file
	if err := os.WriteFile(tempPath, data, 0o640); err != nil {
		return fmt.Errorf("failed to write temporary file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tempPath, filePath); err != nil {
		// Clean up temp file
		_ = os.Remove(tempPath)
		return fmt.Errorf("failed to rename file: %w", err)
	}

	return nil
}

// CleanupExpiredLocks removes locks that have expired
func (fs *FileStore) CleanupExpiredLocks(_ context.Context) error {
	fs.locksMu.Lock()
	defer fs.locksMu.Unlock()

	if fs.closed {
		return ErrStoreClosed
	}

	now := time.Now()
	for id, lock := range fs.locks {
		if now.After(lock.ExpiresAt) {
			delete(fs.locks, id)
		}
	}

	return nil
}

// RecoverDeployments loads all deployments on startup
func (fs *FileStore) RecoverDeployments(ctx context.Context) ([]*DeploymentState, error) {
	return fs.List(ctx, "")
}

// Ensure FileStore implements Store interface
var _ Store = (*FileStore)(nil)
