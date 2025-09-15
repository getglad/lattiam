package integration_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/config"
	"github.com/lattiam/lattiam/internal/state"
	"github.com/lattiam/lattiam/tests/helpers"
)

//nolint:funlen // Comprehensive backend safety validation test with multiple scenarios
func TestBackendSafetyValidation(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping backend safety integration tests in short mode")
	}

	suite := helpers.NewTestSuite(t)
	defer suite.Cleanup()

	t.Run("first_time_initialization", func(t *testing.T) {
		t.Parallel()
		// Create a clean state directory
		stateDir := suite.CreateTempDir("backend_safety_first_init")

		cfg := &config.ServerConfig{
			StateDir: stateDir,
			StateStore: config.StateStoreConfig{
				Type: "file",
				File: config.FileStoreConfig{
					Path: filepath.Join(stateDir, "deployments"),
				},
			},
		}

		validator := state.NewBackendValidator(cfg)

		// First validation should initialize the backend
		err := state.ValidateOrInitializeBackend(context.Background(), cfg, validator)
		require.NoError(t, err, "First time initialization should succeed")

		// Verify backend metadata was created
		metadata, err := validator.GetMetadata(context.Background())
		require.NoError(t, err, "Should be able to get metadata after initialization")
		assert.Equal(t, state.BackendTypeFile, metadata.Type)
		assert.Equal(t, 0, metadata.DeploymentCount)
		assert.False(t, metadata.InitializedAt.IsZero())
	})

	t.Run("backend_type_mismatch_prevention", func(t *testing.T) {
		t.Parallel()
		// Create a clean state directory and initialize with file backend
		stateDir := suite.CreateTempDir("backend_safety_mismatch")

		fileCfg := &config.ServerConfig{
			StateDir: stateDir,
			StateStore: config.StateStoreConfig{
				Type: "file",
				File: config.FileStoreConfig{
					Path: filepath.Join(stateDir, "deployments"),
				},
			},
		}

		validator := state.NewBackendValidator(fileCfg)

		// Initialize with file backend
		err := validator.InitializeBackend(context.Background(), fileCfg, 5)
		require.NoError(t, err, "File backend initialization should succeed")

		// Try to validate with AWS backend configuration
		awsCfg := &config.ServerConfig{
			StateDir: stateDir, // Same state dir
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

		validator2 := state.NewBackendValidator(awsCfg)

		// Validation should fail with backend mismatch
		err = validator2.ValidateBackend(context.Background(), awsCfg)
		require.Error(t, err, "Should fail when backend type changes")
		require.ErrorIs(t, err, state.ErrBackendMismatch, "Should get backend mismatch error")
		assert.Contains(t, err.Error(), "initialized=file, configured=aws")
	})

	t.Run("aws_config_change_detection", func(t *testing.T) {
		t.Parallel()
		// Create a clean state directory and initialize with AWS backend
		stateDir := suite.CreateTempDir("backend_safety_aws_config")

		originalCfg := &config.ServerConfig{
			StateDir: stateDir,
			StateStore: config.StateStoreConfig{
				Type: "aws",
				AWS: config.AWSStoreConfig{
					S3: config.AWSS3Config{
						Bucket: "original-bucket",
						Region: "us-east-1",
						Prefix: "states/",
					},
					DynamoDB: config.AWSDynamoDBConfig{
						Table:  "original-table",
						Region: "us-east-1",
					},
				},
			},
		}

		validator := state.NewBackendValidator(originalCfg)

		// Initialize with original AWS config
		err := validator.InitializeBackend(context.Background(), originalCfg, 3)
		require.NoError(t, err, "AWS backend initialization should succeed")

		// Try to validate with changed AWS configuration
		changedCfg := &config.ServerConfig{
			StateDir: stateDir, // Same state dir
			StateStore: config.StateStoreConfig{
				Type: "aws",
				AWS: config.AWSStoreConfig{
					S3: config.AWSS3Config{
						Bucket: "different-bucket", // Changed bucket
						Region: "us-east-1",
						Prefix: "states/",
					},
					DynamoDB: config.AWSDynamoDBConfig{
						Table:  "original-table",
						Region: "us-east-1",
					},
				},
			},
		}

		validator2 := state.NewBackendValidator(changedCfg)

		// Validation should fail due to config change
		err = validator2.ValidateBackend(context.Background(), changedCfg)
		require.Error(t, err, "Should fail when AWS config changes")
		assert.ErrorIs(t, err, state.ErrUnsafeBackendSwitch, "Should get unsafe backend switch error")
	})

	t.Run("force_reinitialization", func(t *testing.T) {
		t.Parallel()
		// Create a clean state directory and initialize
		stateDir := suite.CreateTempDir("backend_safety_force_reinit")

		originalCfg := &config.ServerConfig{
			StateDir: stateDir,
			StateStore: config.StateStoreConfig{
				Type: "file",
				File: config.FileStoreConfig{
					Path: filepath.Join(stateDir, "deployments"),
				},
			},
		}

		validator := state.NewBackendValidator(originalCfg)

		// Initialize with original config
		err := validator.InitializeBackend(context.Background(), originalCfg, 10)
		require.NoError(t, err, "Original initialization should succeed")

		// Verify initial metadata
		metadata, err := validator.GetMetadata(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 10, metadata.DeploymentCount)
		initialTime := metadata.InitializedAt

		// Force reinitialization with different config
		newCfg := &config.ServerConfig{
			StateDir: stateDir, // Same state dir
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

		validator2 := state.NewBackendValidator(newCfg)

		err = validator2.ForceReinitialize(context.Background(), newCfg)
		require.NoError(t, err, "Force reinitialization should succeed")

		// Verify metadata was updated
		metadata, err = validator2.GetMetadata(context.Background())
		require.NoError(t, err)
		assert.Equal(t, state.BackendTypeAWS, metadata.Type)
		assert.Equal(t, 0, metadata.DeploymentCount) // Reset to 0
		assert.True(t, metadata.InitializedAt.After(initialTime), "Should have new initialization time")
	})

	t.Run("metadata_updates", func(t *testing.T) {
		t.Parallel()
		// Create a clean state directory
		stateDir := suite.CreateTempDir("backend_safety_metadata_updates")

		cfg := &config.ServerConfig{
			StateDir: stateDir,
			StateStore: config.StateStoreConfig{
				Type: "file",
				File: config.FileStoreConfig{
					Path: filepath.Join(stateDir, "deployments"),
				},
			},
		}

		validator := state.NewBackendValidator(cfg)

		// Initialize
		err := validator.InitializeBackend(context.Background(), cfg, 5)
		require.NoError(t, err)

		// Update deployment count
		err = validator.UpdateMetadata(context.Background(), map[string]interface{}{
			"deployment_count": 15,
		})
		require.NoError(t, err, "Metadata update should succeed")

		// Verify update
		metadata, err := validator.GetMetadata(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 15, metadata.DeploymentCount)

		// Update validation time
		newTime := time.Now()
		err = validator.UpdateMetadata(context.Background(), map[string]interface{}{
			"last_validated_at": newTime,
		})
		require.NoError(t, err, "Validation time update should succeed")

		// Verify validation time update
		metadata, err = validator.GetMetadata(context.Background())
		require.NoError(t, err)
		assert.True(t, metadata.LastValidatedAt.After(newTime.Add(-1*time.Second)),
			"Last validated time should be updated")
	})

	t.Run("corrupted_backend_lock", func(t *testing.T) {
		t.Parallel()
		// Create a clean state directory
		stateDir := suite.CreateTempDir("backend_safety_corrupted")

		// Create a corrupted backend.lock file
		lockFile := filepath.Join(stateDir, "backend.lock")
		err := os.MkdirAll(stateDir, 0o750)
		require.NoError(t, err)

		err = os.WriteFile(lockFile, []byte("invalid json content"), 0o600)
		require.NoError(t, err)

		cfg := &config.ServerConfig{
			StateDir: stateDir,
			StateStore: config.StateStoreConfig{
				Type: "file",
				File: config.FileStoreConfig{
					Path: filepath.Join(stateDir, "deployments"),
				},
			},
		}

		validator := state.NewBackendValidator(cfg)

		// Getting metadata should fail with corruption error
		_, err = validator.GetMetadata(context.Background())
		require.Error(t, err, "Should fail with corrupted lock file")
		assert.ErrorIs(t, err, state.ErrBackendLockCorrupted, "Should get lock corrupted error")
	})

	t.Run("successful_validation_after_init", func(t *testing.T) {
		t.Parallel()
		// Create a clean state directory and initialize
		stateDir := suite.CreateTempDir("backend_safety_success")

		cfg := &config.ServerConfig{
			StateDir: stateDir,
			StateStore: config.StateStoreConfig{
				Type: "file",
				File: config.FileStoreConfig{
					Path: filepath.Join(stateDir, "deployments"),
				},
			},
		}

		validator := state.NewBackendValidator(cfg)

		// Initialize
		err := validator.InitializeBackend(context.Background(), cfg, 8)
		require.NoError(t, err)

		// Subsequent validations should succeed
		err = validator.ValidateBackend(context.Background(), cfg)
		require.NoError(t, err, "Validation should succeed with matching config")

		// Multiple validations should work
		err = validator.ValidateBackend(context.Background(), cfg)
		require.NoError(t, err, "Second validation should also succeed")

		// Verify last validated time gets updated
		metadata1, err := validator.GetMetadata(context.Background())
		require.NoError(t, err)

		time.Sleep(10 * time.Millisecond) // Small delay to ensure time difference

		err = validator.ValidateBackend(context.Background(), cfg)
		require.NoError(t, err)

		metadata2, err := validator.GetMetadata(context.Background())
		require.NoError(t, err)

		assert.True(t, metadata2.LastValidatedAt.After(metadata1.LastValidatedAt),
			"Last validated time should be updated on each validation")
	})
}

//nolint:funlen // Comprehensive backend metadata file handling test with error scenarios
func TestBackendMetadataFileHandling(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping backend metadata file tests in short mode")
	}

	suite := helpers.NewTestSuite(t)
	defer suite.Cleanup()

	t.Run("directory_creation", func(t *testing.T) {
		t.Parallel()
		// Test that validator creates directory if it doesn't exist
		baseDir := suite.CreateTempDir("backend_metadata_dir")
		nonExistentDir := filepath.Join(baseDir, "nested", "path", "to", "state")

		cfg := &config.ServerConfig{
			StateDir: nonExistentDir,
			StateStore: config.StateStoreConfig{
				Type: "file",
				File: config.FileStoreConfig{
					Path: filepath.Join(nonExistentDir, "deployments"),
				},
			},
		}

		validator := state.NewBackendValidator(cfg)

		// Initialize should create the directory
		err := validator.InitializeBackend(context.Background(), cfg, 1)
		require.NoError(t, err, "Should create directory and initialize")

		// Verify directory and file were created
		lockFile := filepath.Join(nonExistentDir, "backend.lock")
		assert.FileExists(t, lockFile, "Backend lock file should exist")

		// Verify we can read the metadata
		metadata, err := validator.GetMetadata(context.Background())
		require.NoError(t, err)
		assert.Equal(t, state.BackendTypeFile, metadata.Type)
	})

	t.Run("atomic_writes", func(t *testing.T) {
		t.Parallel()
		// Test that metadata writes are atomic (using temp file + rename)
		stateDir := suite.CreateTempDir("backend_metadata_atomic")

		cfg := &config.ServerConfig{
			StateDir: stateDir,
			StateStore: config.StateStoreConfig{
				Type: "file",
				File: config.FileStoreConfig{
					Path: filepath.Join(stateDir, "deployments"),
				},
			},
		}

		validator := state.NewBackendValidator(cfg)

		// Initialize
		err := validator.InitializeBackend(context.Background(), cfg, 1)
		require.NoError(t, err)

		// Update multiple times rapidly to test atomicity
		for i := 2; i <= 10; i++ {
			err = validator.UpdateMetadata(context.Background(), map[string]interface{}{
				"deployment_count": i,
			})
			require.NoError(t, err, "Update %d should succeed", i)

			// Verify we can always read valid metadata (no partial writes)
			metadata, err := validator.GetMetadata(context.Background())
			require.NoError(t, err, "Should always be able to read metadata")
			assert.Equal(t, i, metadata.DeploymentCount, "Should have updated count")
		}

		// Verify no temp files are left behind
		files, err := os.ReadDir(stateDir)
		require.NoError(t, err)

		for _, file := range files {
			assert.NotEqual(t, ".tmp", filepath.Ext(file.Name()),
				"No temp files should remain: %s", file.Name())
		}
	})
}
