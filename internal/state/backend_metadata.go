package state

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/lattiam/lattiam/internal/config"
)

// Backend safety validation errors
var (
	ErrBackendMismatch       = errors.New("configured backend does not match initialized backend")
	ErrBackendNotInitialized = errors.New("backend not initialized - no backend.lock file found")
	ErrBackendLockCorrupted  = errors.New("backend.lock file is corrupted or invalid")
	ErrUnsafeBackendSwitch   = errors.New("unsafe backend switch detected")
)

// BackendType represents different backend types
type BackendType string

const (
	// BackendTypeFile represents file-based backend storage
	BackendTypeFile BackendType = "file"
	// BackendTypeMemory represents in-memory backend storage for testing
	BackendTypeMemory BackendType = "memory"
	// BackendTypeAWS represents AWS-based backend storage (DynamoDB + S3)
	BackendTypeAWS BackendType = "aws"
)

// BackendMetadata tracks information about the initialized backend
type BackendMetadata struct {
	Type            BackendType            `json:"type"`
	ConfigHash      string                 `json:"config_hash"`
	InitializedAt   time.Time              `json:"initialized_at"`
	LastValidatedAt time.Time              `json:"last_validated_at"`
	DeploymentCount int                    `json:"deployment_count"`
	Configuration   map[string]interface{} `json:"configuration,omitempty"`
	Version         string                 `json:"version,omitempty"`
}

// BackendValidator handles backend safety validation
type BackendValidator interface {
	// ValidateBackend checks if the configured backend matches the initialized backend
	ValidateBackend(ctx context.Context, cfg *config.ServerConfig) error

	// InitializeBackend creates or updates the backend metadata after first successful initialization
	InitializeBackend(ctx context.Context, cfg *config.ServerConfig, deploymentCount int) error

	// UpdateMetadata updates backend metadata (e.g., deployment count)
	UpdateMetadata(ctx context.Context, updates map[string]interface{}) error

	// GetMetadata retrieves current backend metadata
	GetMetadata(ctx context.Context) (*BackendMetadata, error)

	// ForceReinitialize forces reinitialization (for migrations)
	ForceReinitialize(ctx context.Context, cfg *config.ServerConfig) error
}

// FileBackendValidator implements BackendValidator using a local file
type FileBackendValidator struct {
	lockFilePath string
}

// NewFileBackendValidator creates a new file-based backend validator
func NewFileBackendValidator(stateDir string) *FileBackendValidator {
	lockFilePath := filepath.Join(stateDir, "backend.lock")
	return &FileBackendValidator{
		lockFilePath: lockFilePath,
	}
}

// ValidateBackend implements BackendValidator
func (f *FileBackendValidator) ValidateBackend(ctx context.Context, cfg *config.ServerConfig) error {
	metadata, err := f.GetMetadata(ctx)
	if err != nil {
		if errors.Is(err, ErrBackendNotInitialized) {
			// Return the error so ValidateOrInitializeBackend can detect it
			return err
		}
		return fmt.Errorf("failed to load backend metadata: %w", err)
	}

	// Validate backend type matches
	expectedType := BackendType(cfg.StateStore.Type)
	if metadata.Type != expectedType {
		return fmt.Errorf("%w: initialized=%s, configured=%s",
			ErrBackendMismatch, metadata.Type, expectedType)
	}

	// Validate configuration hash for critical backends
	if expectedType == BackendTypeAWS {
		currentHash, err := f.calculateConfigHash(cfg)
		if err != nil {
			return fmt.Errorf("failed to calculate config hash: %w", err)
		}

		if metadata.ConfigHash != currentHash {
			return fmt.Errorf("%w: configuration has changed since initialization", ErrUnsafeBackendSwitch)
		}
	}

	// Update last validated timestamp
	_ = f.UpdateMetadata(ctx, map[string]interface{}{
		"last_validated_at": time.Now(),
	})

	return nil
}

// InitializeBackend implements BackendValidator
func (f *FileBackendValidator) InitializeBackend(_ context.Context, cfg *config.ServerConfig, deploymentCount int) error {
	configHash, err := f.calculateConfigHash(cfg)
	if err != nil {
		return fmt.Errorf("failed to calculate config hash: %w", err)
	}

	metadata := &BackendMetadata{
		Type:            BackendType(cfg.StateStore.Type),
		ConfigHash:      configHash,
		InitializedAt:   time.Now(),
		LastValidatedAt: time.Now(),
		DeploymentCount: deploymentCount,
		Configuration:   f.sanitizeConfig(cfg),
		Version:         config.AppVersion,
	}

	return f.saveMetadata(metadata)
}

// UpdateMetadata implements BackendValidator
func (f *FileBackendValidator) UpdateMetadata(ctx context.Context, updates map[string]interface{}) error {
	metadata, err := f.GetMetadata(ctx)
	if err != nil {
		return fmt.Errorf("failed to load existing metadata: %w", err)
	}

	// Apply updates
	for key, value := range updates {
		switch key {
		case "deployment_count":
			if count, ok := value.(int); ok {
				metadata.DeploymentCount = count
			}
		case "last_validated_at":
			if ts, ok := value.(time.Time); ok {
				metadata.LastValidatedAt = ts
			}
		}
	}

	return f.saveMetadata(metadata)
}

// GetMetadata implements BackendValidator
func (f *FileBackendValidator) GetMetadata(_ context.Context) (*BackendMetadata, error) {
	// Ensure directory exists
	dir := filepath.Dir(f.lockFilePath)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("failed to create state directory: %w", err)
	}

	data, err := os.ReadFile(f.lockFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrBackendNotInitialized
		}
		return nil, fmt.Errorf("failed to read backend lock file: %w", err)
	}

	var metadata BackendMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBackendLockCorrupted, err)
	}

	return &metadata, nil
}

// ForceReinitialize implements BackendValidator
func (f *FileBackendValidator) ForceReinitialize(ctx context.Context, cfg *config.ServerConfig) error {
	// Remove existing lock file
	if err := os.Remove(f.lockFilePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove existing backend lock: %w", err)
	}

	// Initialize with zero deployment count (migrations will update this)
	return f.InitializeBackend(ctx, cfg, 0)
}

// saveMetadata saves metadata to the lock file
func (f *FileBackendValidator) saveMetadata(metadata *BackendMetadata) error {
	// Ensure directory exists
	dir := filepath.Dir(f.lockFilePath)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("failed to create state directory: %w", err)
	}

	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	// Write atomically using temp file
	tmpFile := f.lockFilePath + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0o600); err != nil {
		return fmt.Errorf("failed to write temp file: %w", err)
	}

	if err := os.Rename(tmpFile, f.lockFilePath); err != nil {
		_ = os.Remove(tmpFile) // cleanup on error
		return fmt.Errorf("failed to atomic rename: %w", err)
	}

	return nil
}

// calculateConfigHash creates a hash of critical backend configuration
func (f *FileBackendValidator) calculateConfigHash(cfg *config.ServerConfig) (string, error) {
	// Only hash critical configuration that affects backend compatibility
	criticalConfig := map[string]interface{}{
		"type": cfg.StateStore.Type,
	}

	// Add type-specific critical config
	switch cfg.StateStore.Type {
	case "aws":
		criticalConfig["s3_bucket"] = cfg.StateStore.AWS.S3.Bucket
		criticalConfig["s3_region"] = cfg.StateStore.AWS.S3.Region
		criticalConfig["s3_prefix"] = cfg.StateStore.AWS.S3.Prefix
		criticalConfig["dynamodb_table"] = cfg.StateStore.AWS.DynamoDB.Table
		criticalConfig["dynamodb_region"] = cfg.StateStore.AWS.DynamoDB.Region
	case "file":
		criticalConfig["path"] = cfg.StateStore.File.Path
	}

	data, err := json.Marshal(criticalConfig)
	if err != nil {
		return "", fmt.Errorf("failed to marshal config: %w", err)
	}

	hash := sha256.Sum256(data)
	return fmt.Sprintf("%x", hash), nil
}

// sanitizeConfig returns a sanitized version of config safe for storage
func (f *FileBackendValidator) sanitizeConfig(cfg *config.ServerConfig) map[string]interface{} {
	sanitized := map[string]interface{}{
		"type": cfg.StateStore.Type,
	}

	switch cfg.StateStore.Type {
	case "aws":
		sanitized["aws"] = map[string]interface{}{
			"s3_bucket_configured":         cfg.StateStore.AWS.S3.Bucket != "",
			"s3_region":                    cfg.StateStore.AWS.S3.Region,
			"s3_prefix":                    cfg.StateStore.AWS.S3.Prefix,
			"s3_endpoint_configured":       cfg.StateStore.AWS.S3.Endpoint != "",
			"dynamodb_table_configured":    cfg.StateStore.AWS.DynamoDB.Table != "",
			"dynamodb_region":              cfg.StateStore.AWS.DynamoDB.Region,
			"dynamodb_endpoint_configured": cfg.StateStore.AWS.DynamoDB.Endpoint != "",
			"locking_enabled":              cfg.StateStore.AWS.DynamoDB.Locking.Enabled,
		}
	case "file":
		sanitized["file"] = map[string]interface{}{
			"path_configured": cfg.StateStore.File.Path != "",
		}
	}

	return sanitized
}

// NewBackendValidator creates a BackendValidator based on the configuration
func NewBackendValidator(cfg *config.ServerConfig) BackendValidator {
	// For now, always use file-based validator regardless of backend type
	// The lock file is stored in the state directory
	return NewFileBackendValidator(cfg.StateDir)
}

// ValidateOrInitializeBackend is a convenience function that validates an existing backend
// or initializes it if this is the first time
func ValidateOrInitializeBackend(ctx context.Context, cfg *config.ServerConfig, validator BackendValidator) error {
	err := validator.ValidateBackend(ctx, cfg)
	if err == nil {
		return nil // Backend is valid
	}

	if errors.Is(err, ErrBackendNotInitialized) {
		// First time initialization - this is safe
		if err := validator.InitializeBackend(ctx, cfg, 0); err != nil {
			return fmt.Errorf("failed to initialize backend: %w", err)
		}
		return nil
	}

	// Any other error is a validation failure
	return fmt.Errorf("backend validation failed: %w", err)
}
