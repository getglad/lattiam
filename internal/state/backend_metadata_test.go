package state

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/config"
)

func TestFileBackendValidator_InitializeBackend(t *testing.T) {
	t.Parallel()
	tempDir, err := os.MkdirTemp("", "backend_test")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()

	validator := NewFileBackendValidator(tempDir)

	cfg := &config.ServerConfig{
		StateDir: tempDir,
		StateStore: config.StateStoreConfig{
			Type: "file",
			File: config.FileStoreConfig{
				Path: filepath.Join(tempDir, "deployments"),
			},
		},
	}

	ctx := context.Background()
	err = validator.InitializeBackend(ctx, cfg, 5)
	require.NoError(t, err, "Should initialize backend successfully")

	// Verify lock file was created
	lockFile := filepath.Join(tempDir, "backend.lock")
	assert.FileExists(t, lockFile, "Backend lock file should exist")

	// Verify metadata content
	data, err := os.ReadFile(lockFile) // #nosec G304 - Test file with controlled path
	require.NoError(t, err)

	var metadata BackendMetadata
	err = json.Unmarshal(data, &metadata)
	require.NoError(t, err, "Should be able to parse metadata JSON")

	assert.Equal(t, BackendTypeFile, metadata.Type)
	assert.Equal(t, 5, metadata.DeploymentCount)
	assert.False(t, metadata.InitializedAt.IsZero())
	assert.False(t, metadata.LastValidatedAt.IsZero())
	assert.NotEmpty(t, metadata.ConfigHash)
	assert.Equal(t, config.AppVersion, metadata.Version)
}

//nolint:funlen // Test function with comprehensive validation scenarios
func TestFileBackendValidator_ValidateBackend(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("validate_uninitialized_backend", func(t *testing.T) {
		t.Parallel()
		// Create isolated environment for this test
		tempDir, err := os.MkdirTemp("", "backend_validate_uninit_test")
		require.NoError(t, err)
		defer func() { _ = os.RemoveAll(tempDir) }()

		validator := NewFileBackendValidator(tempDir)
		cfg := &config.ServerConfig{
			StateDir: tempDir,
			StateStore: config.StateStoreConfig{
				Type: "file",
				File: config.FileStoreConfig{
					Path: filepath.Join(tempDir, "deployments"),
				},
			},
		}

		// Should fail with ErrBackendNotInitialized
		err = validator.ValidateBackend(ctx, cfg)
		require.Error(t, err, "Validation should fail for uninitialized backend")
		assert.ErrorIs(t, err, ErrBackendNotInitialized)
	})

	t.Run("validate_after_initialization", func(t *testing.T) {
		t.Parallel()
		// Create isolated environment for this test
		tempDir, err := os.MkdirTemp("", "backend_validate_init_test")
		require.NoError(t, err)
		defer func() { _ = os.RemoveAll(tempDir) }()

		validator := NewFileBackendValidator(tempDir)
		cfg := &config.ServerConfig{
			StateDir: tempDir,
			StateStore: config.StateStoreConfig{
				Type: "file",
				File: config.FileStoreConfig{
					Path: filepath.Join(tempDir, "deployments"),
				},
			},
		}

		// Initialize first
		err = validator.InitializeBackend(ctx, cfg, 3)
		require.NoError(t, err)

		// Validation should succeed with same config
		err = validator.ValidateBackend(ctx, cfg)
		assert.NoError(t, err, "Validation should succeed with matching config")
	})

	t.Run("validate_backend_type_mismatch", func(t *testing.T) {
		t.Parallel()
		// Create a separate temp directory for this test
		mismatchDir, err := os.MkdirTemp("", "backend_mismatch_test")
		require.NoError(t, err)
		defer func() { _ = os.RemoveAll(mismatchDir) }()

		mismatchValidator := NewFileBackendValidator(mismatchDir)

		// Initialize with file backend first
		fileCfg := &config.ServerConfig{
			StateDir: mismatchDir,
			StateStore: config.StateStoreConfig{
				Type: "file",
				File: config.FileStoreConfig{
					Path: filepath.Join(mismatchDir, "deployments"),
				},
			},
		}
		err = mismatchValidator.InitializeBackend(ctx, fileCfg, 3)
		require.NoError(t, err)

		// Now try to validate with different backend type
		awsCfg := &config.ServerConfig{
			StateDir: mismatchDir, // Same directory
			StateStore: config.StateStoreConfig{
				Type: "aws",
				AWS: config.AWSStoreConfig{
					S3: config.AWSS3Config{
						Bucket: "test-bucket",
						Region: "us-east-1",
						Prefix: "states/",
					},
					DynamoDB: config.AWSDynamoDBConfig{
						Table:  "test-table",
						Region: "us-east-1",
					},
				},
			},
		}

		err = mismatchValidator.ValidateBackend(ctx, awsCfg)
		require.Error(t, err, "Should fail with backend type mismatch")
		require.ErrorIs(t, err, ErrBackendMismatch)
		assert.Contains(t, err.Error(), "initialized=file, configured=aws")
	})
}

func TestFileBackendValidator_UpdateMetadata(t *testing.T) {
	t.Parallel()
	tempDir, err := os.MkdirTemp("", "backend_update_test")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()

	validator := NewFileBackendValidator(tempDir)

	cfg := &config.ServerConfig{
		StateDir: tempDir,
		StateStore: config.StateStoreConfig{
			Type: "file",
			File: config.FileStoreConfig{
				Path: filepath.Join(tempDir, "deployments"),
			},
		},
	}

	ctx := context.Background()

	// Initialize first
	err = validator.InitializeBackend(ctx, cfg, 5)
	require.NoError(t, err)

	// Update deployment count
	err = validator.UpdateMetadata(ctx, map[string]interface{}{
		"deployment_count": 10,
	})
	require.NoError(t, err, "Should update deployment count")

	// Verify update
	metadata, err := validator.GetMetadata(ctx)
	require.NoError(t, err)
	assert.Equal(t, 10, metadata.DeploymentCount)

	// Update last validated time
	newTime := time.Now()
	err = validator.UpdateMetadata(ctx, map[string]interface{}{
		"last_validated_at": newTime,
	})
	require.NoError(t, err, "Should update last validated time")

	// Verify validation time update
	metadata, err = validator.GetMetadata(ctx)
	require.NoError(t, err)
	assert.True(t, metadata.LastValidatedAt.After(newTime.Add(-1*time.Second)),
		"Last validated time should be updated")
}

func TestFileBackendValidator_GetMetadata(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("get_metadata_uninitialized", func(t *testing.T) {
		t.Parallel()
		tempDir, err := os.MkdirTemp("", "backend_get_test_uninit")
		require.NoError(t, err)
		defer func() { _ = os.RemoveAll(tempDir) }()

		validator := NewFileBackendValidator(tempDir)
		_, err = validator.GetMetadata(ctx)
		assert.ErrorIs(t, err, ErrBackendNotInitialized, "Should get not initialized error")
	})

	t.Run("get_metadata_after_init", func(t *testing.T) {
		t.Parallel()
		tempDir, err := os.MkdirTemp("", "backend_get_test_init")
		require.NoError(t, err)
		defer func() { _ = os.RemoveAll(tempDir) }()

		validator := NewFileBackendValidator(tempDir)
		cfg := &config.ServerConfig{
			StateDir: tempDir,
			StateStore: config.StateStoreConfig{
				Type: "file",
				File: config.FileStoreConfig{
					Path: filepath.Join(tempDir, "deployments"),
				},
			},
		}

		err = validator.InitializeBackend(ctx, cfg, 7)
		require.NoError(t, err)

		metadata, err := validator.GetMetadata(ctx)
		require.NoError(t, err)

		assert.Equal(t, BackendTypeFile, metadata.Type)
		assert.Equal(t, 7, metadata.DeploymentCount)
		assert.NotEmpty(t, metadata.ConfigHash)
	})

	t.Run("get_metadata_corrupted_file", func(t *testing.T) {
		t.Parallel()
		tempDir, err := os.MkdirTemp("", "backend_get_test_corrupt")
		require.NoError(t, err)
		defer func() { _ = os.RemoveAll(tempDir) }()

		// Create corrupted lock file
		lockFile := filepath.Join(tempDir, "backend.lock")
		err = os.WriteFile(lockFile, []byte("invalid json"), 0o600)
		require.NoError(t, err)

		validator := NewFileBackendValidator(tempDir)
		_, err = validator.GetMetadata(ctx)
		assert.ErrorIs(t, err, ErrBackendLockCorrupted, "Should get corrupted error")
	})
}

func TestFileBackendValidator_ForceReinitialize(t *testing.T) {
	t.Parallel()
	tempDir, err := os.MkdirTemp("", "backend_force_test")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()

	validator := NewFileBackendValidator(tempDir)
	ctx := context.Background()

	// Initialize with file backend
	fileCfg := &config.ServerConfig{
		StateDir: tempDir,
		StateStore: config.StateStoreConfig{
			Type: "file",
			File: config.FileStoreConfig{
				Path: filepath.Join(tempDir, "deployments"),
			},
		},
	}

	err = validator.InitializeBackend(ctx, fileCfg, 10)
	require.NoError(t, err)

	// Verify initial state
	metadata, err := validator.GetMetadata(ctx)
	require.NoError(t, err)
	assert.Equal(t, BackendTypeFile, metadata.Type)
	assert.Equal(t, 10, metadata.DeploymentCount)
	initialTime := metadata.InitializedAt

	// Force reinitialize with AWS backend config
	awsCfg := &config.ServerConfig{
		StateDir: tempDir, // Same directory
		StateStore: config.StateStoreConfig{
			Type: "aws",
			AWS: config.AWSStoreConfig{
				S3: config.AWSS3Config{
					Bucket: "new-bucket",
					Region: "us-east-1",
					Prefix: "states/",
				},
				DynamoDB: config.AWSDynamoDBConfig{
					Table:  "new-table",
					Region: "us-east-1",
				},
			},
		},
	}

	err = validator.ForceReinitialize(ctx, awsCfg)
	require.NoError(t, err, "Force reinitialization should succeed")

	// Verify reinitialization
	metadata, err = validator.GetMetadata(ctx)
	require.NoError(t, err)
	assert.Equal(t, BackendTypeAWS, metadata.Type)
	assert.Equal(t, 0, metadata.DeploymentCount) // Reset to 0
	assert.True(t, metadata.InitializedAt.After(initialTime), "Should have new initialization time")
}

//nolint:funlen // Comprehensive hash calculation test with multiple scenarios
func TestFileBackendValidator_calculateConfigHash(t *testing.T) {
	t.Parallel()
	tempDir, err := os.MkdirTemp("", "backend_hash_test")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()

	validator := NewFileBackendValidator(tempDir)

	t.Run("file_backend_hash", func(t *testing.T) {
		t.Parallel()
		cfg1 := &config.ServerConfig{
			StateStore: config.StateStoreConfig{
				Type: "file",
				File: config.FileStoreConfig{
					Path: "/path/to/deployments",
				},
			},
		}

		cfg2 := &config.ServerConfig{
			StateStore: config.StateStoreConfig{
				Type: "file",
				File: config.FileStoreConfig{
					Path: "/path/to/deployments", // Same path
				},
			},
		}

		cfg3 := &config.ServerConfig{
			StateStore: config.StateStoreConfig{
				Type: "file",
				File: config.FileStoreConfig{
					Path: "/different/path", // Different path
				},
			},
		}

		hash1, err := validator.calculateConfigHash(cfg1)
		require.NoError(t, err)

		hash2, err := validator.calculateConfigHash(cfg2)
		require.NoError(t, err)

		hash3, err := validator.calculateConfigHash(cfg3)
		require.NoError(t, err)

		assert.Equal(t, hash1, hash2, "Same config should produce same hash")
		assert.NotEqual(t, hash1, hash3, "Different config should produce different hash")
	})

	t.Run("aws_backend_hash", func(t *testing.T) {
		t.Parallel()
		cfg1 := &config.ServerConfig{
			StateStore: config.StateStoreConfig{
				Type: "aws",
				AWS: config.AWSStoreConfig{
					S3: config.AWSS3Config{
						Bucket: "bucket-1",
						Region: "us-east-1",
						Prefix: "states/",
					},
					DynamoDB: config.AWSDynamoDBConfig{
						Table:  "table-1",
						Region: "us-east-1",
					},
				},
			},
		}

		cfg2 := &config.ServerConfig{
			StateStore: config.StateStoreConfig{
				Type: "aws",
				AWS: config.AWSStoreConfig{
					S3: config.AWSS3Config{
						Bucket: "bucket-2", // Different bucket
						Region: "us-east-1",
						Prefix: "states/",
					},
					DynamoDB: config.AWSDynamoDBConfig{
						Table:  "table-1",
						Region: "us-east-1",
					},
				},
			},
		}

		hash1, err := validator.calculateConfigHash(cfg1)
		require.NoError(t, err)

		hash2, err := validator.calculateConfigHash(cfg2)
		require.NoError(t, err)

		assert.NotEqual(t, hash1, hash2, "Different AWS config should produce different hash")
		assert.NotEmpty(t, hash1, "Hash should not be empty")
		assert.NotEmpty(t, hash2, "Hash should not be empty")
	})
}

//nolint:funlen // Comprehensive config sanitization test
func TestFileBackendValidator_sanitizeConfig(t *testing.T) {
	t.Parallel()
	tempDir, err := os.MkdirTemp("", "backend_sanitize_test")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()

	validator := NewFileBackendValidator(tempDir)

	t.Run("sanitize_file_config", func(t *testing.T) {
		t.Parallel()
		cfg := &config.ServerConfig{
			StateStore: config.StateStoreConfig{
				Type: "file",
				File: config.FileStoreConfig{
					Path: "/sensitive/path/to/files",
				},
			},
		}

		sanitized := validator.sanitizeConfig(cfg)

		assert.Equal(t, "file", sanitized["type"])
		fileConfig, exists := sanitized["file"].(map[string]interface{})
		require.True(t, exists, "Should have file config")
		assert.Equal(t, true, fileConfig["path_configured"], "Should show path is configured")
		assert.NotContains(t, sanitized, "/sensitive/path", "Should not contain sensitive path")
	})

	t.Run("sanitize_aws_config", func(t *testing.T) {
		t.Parallel()
		cfg := &config.ServerConfig{
			StateStore: config.StateStoreConfig{
				Type: "aws",
				AWS: config.AWSStoreConfig{
					S3: config.AWSS3Config{
						Bucket: "sensitive-bucket-name",
						Region: "us-east-1",
						Prefix: "states/",
					},
					DynamoDB: config.AWSDynamoDBConfig{
						Table:  "sensitive-table-name",
						Region: "us-east-1",
					},
				},
			},
		}

		sanitized := validator.sanitizeConfig(cfg)

		assert.Equal(t, "aws", sanitized["type"])
		awsConfig, exists := sanitized["aws"].(map[string]interface{})
		require.True(t, exists, "Should have AWS config")

		assert.Equal(t, true, awsConfig["s3_bucket_configured"], "Should show S3 bucket is configured")
		assert.Equal(t, "us-east-1", awsConfig["s3_region"], "Should include region")
		assert.Equal(t, "states/", awsConfig["s3_prefix"], "Should include prefix")
		assert.Equal(t, true, awsConfig["dynamodb_table_configured"], "Should show DynamoDB table is configured")
		assert.Equal(t, "us-east-1", awsConfig["dynamodb_region"], "Should include DynamoDB region")

		// Should not contain sensitive names
		assert.NotContains(t, sanitized, "sensitive-bucket-name")
		assert.NotContains(t, sanitized, "sensitive-table-name")
	})
}

func TestNewBackendValidator(t *testing.T) {
	t.Parallel()
	cfg := &config.ServerConfig{
		StateDir: "/test/path",
		StateStore: config.StateStoreConfig{
			Type: "file",
		},
	}

	validator := NewBackendValidator(cfg)
	require.NotNil(t, validator, "Should create validator")

	// Should return FileBackendValidator regardless of backend type
	fileValidator, ok := validator.(*FileBackendValidator)
	require.True(t, ok, "Should return FileBackendValidator")
	assert.Contains(t, fileValidator.lockFilePath, "/test/path/backend.lock")
}

//nolint:paralleltest // Subtests share validator state and must run sequentially
func TestValidateOrInitializeBackend(t *testing.T) {
	t.Parallel()
	tempDir, err := os.MkdirTemp("", "backend_validate_init_test")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(tempDir) }()

	cfg := &config.ServerConfig{
		StateDir: tempDir,
		StateStore: config.StateStoreConfig{
			Type: "file",
			File: config.FileStoreConfig{
				Path: filepath.Join(tempDir, "deployments"),
			},
		},
	}

	validator := NewFileBackendValidator(tempDir)
	ctx := context.Background()

	t.Run("initialize_on_first_call", func(t *testing.T) {
		// Cannot run in parallel - tests share validator state
		// First call should initialize
		err := ValidateOrInitializeBackend(ctx, cfg, validator)
		require.NoError(t, err, "Should initialize successfully")

		// Verify metadata was created
		metadata, err := validator.GetMetadata(ctx)
		require.NoError(t, err)
		assert.Equal(t, BackendTypeFile, metadata.Type)
		assert.Equal(t, 0, metadata.DeploymentCount) // Default initialization count
	})

	t.Run("validate_on_subsequent_calls", func(t *testing.T) {
		// Cannot run in parallel - tests share validator state
		// Subsequent calls should validate successfully
		err := ValidateOrInitializeBackend(ctx, cfg, validator)
		assert.NoError(t, err, "Should validate successfully")
	})

	t.Run("fail_on_mismatch", func(t *testing.T) {
		// Cannot run in parallel - tests share validator state
		// Change config type
		mismatchCfg := &config.ServerConfig{
			StateDir: tempDir,
			StateStore: config.StateStoreConfig{
				Type: "aws",
				AWS: config.AWSStoreConfig{
					S3: config.AWSS3Config{
						Bucket: "test-bucket",
						Region: "us-east-1",
						Prefix: "states/",
					},
					DynamoDB: config.AWSDynamoDBConfig{
						Table:  "test-table",
						Region: "us-east-1",
					},
				},
			},
		}

		err := ValidateOrInitializeBackend(ctx, mismatchCfg, validator)
		require.Error(t, err, "Should fail with mismatched config")
		require.ErrorIs(t, err, ErrBackendMismatch)
	})
}
