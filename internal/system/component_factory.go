package system

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	configpkg "github.com/lattiam/lattiam/internal/config"
	"github.com/lattiam/lattiam/internal/dependency"
	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/internal/interpolation"
	"github.com/lattiam/lattiam/internal/mocks"
	"github.com/lattiam/lattiam/internal/state"
	"github.com/lattiam/lattiam/pkg/logging"
	"github.com/lattiam/lattiam/pkg/provider/protocol"
)

var logger = logging.NewLogger("component-factory")

// DefaultComponentFactory implements interfaces.ComponentFactory with sensible defaults
type DefaultComponentFactory struct{}

// Compile-time interface check
var _ interfaces.ComponentFactory = (*DefaultComponentFactory)(nil)

// NewDefaultComponentFactory creates a new DefaultComponentFactory
func NewDefaultComponentFactory() *DefaultComponentFactory {
	return &DefaultComponentFactory{}
}

// CreateStateStore creates a StateStore based on the configuration
func (f *DefaultComponentFactory) CreateStateStore(config interfaces.StateStoreConfig) (interfaces.StateStore, error) {
	switch config.Type {
	case "memory", "mock", "":
		// Use mock implementation for development/testing
		return mocks.NewMockStateStore(), nil
	case "file":
		// Use the robust file-based state store with proper locking and atomic writes
		filePath := "./deployments" // Default path
		if config.Options != nil {
			if path, ok := config.Options["path"].(string); ok {
				filePath = path
			}
		}

		// Expand tilde to home directory
		if strings.HasPrefix(filePath, "~/") {
			home, err := os.UserHomeDir()
			if err != nil {
				return nil, fmt.Errorf("failed to get home directory: %w", err)
			}
			filePath = filepath.Join(home, filePath[2:])
		}

		// Create the robust file store adapter
		adapter, err := NewRobustFileStateStoreAdapter(filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to create file state store: %w", err)
		}

		logger.Info("Created file-based state store at: %s", filePath)
		return adapter, nil
	case "aws":
		// Use AWS-based state store with S3 + DynamoDB
		if config.Options == nil {
			return nil, fmt.Errorf("AWS configuration options are required")
		}

		// Extract AWS configuration from options
		awsConfig, err := f.extractAWSConfig(config.Options)
		if err != nil {
			return nil, fmt.Errorf("failed to extract AWS configuration: %w", err)
		}

		logger.Info("Creating AWS-based state store for S3 bucket: %s, DynamoDB table: %s",
			awsConfig.S3Bucket, awsConfig.DynamoDBTable)
		store, err := state.NewAWSStateStore(*awsConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create AWS state store: %w", err)
		}
		return store, nil
	default:
		// Currently supported: "memory", "mock", "aws"
		// Future implementations: postgres, redis
		return nil, fmt.Errorf("unsupported StateStore type: %s (supported: memory, mock, aws)", config.Type)
	}
}

// createProviderManager is a helper to create a provider manager instance
func (f *DefaultComponentFactory) createProviderManager(config interfaces.ProviderManagerConfig) (*ProviderManagerAdapter, error) {
	// Check if we should use mock for testing
	if config.Options != nil {
		if useMock, ok := config.Options["use_mock"]; ok && useMock.(bool) {
			// For mocks, we'll return nil here and let the specific methods handle it
			return nil, nil
		}
	}

	// Use real provider manager with adapter
	providerDir := "./providers" // Default provider directory
	if config.Options != nil {
		if dir, ok := config.Options["provider_dir"].(string); ok {
			providerDir = dir
		}
	}

	// Prepare protocol manager options
	var protocolOpts []protocol.ProviderManagerOption

	// Always add default config source to avoid auto-detection
	protocolOpts = append(protocolOpts, protocol.WithConfigSource(configpkg.NewDefaultConfigSource()))

	// Enable binary cache for tests or if explicitly configured
	if config.Options != nil {
		if enableCache, ok := config.Options["enable_binary_cache"]; ok && enableCache.(bool) {
			protocolOpts = append(protocolOpts, protocol.WithDefaultBinaryCache())
		}
	}

	adapter, err := NewProviderManagerAdapterWithOptions(providerDir, protocolOpts...)
	if err != nil {
		return nil, err
	}

	return adapter, nil
}

// CreateDependencyResolver creates a DependencyResolver based on the configuration
func (f *DefaultComponentFactory) CreateDependencyResolver(config interfaces.DependencyResolverConfig) (interfaces.DependencyResolver, error) {
	// Check if we should use mock for testing
	if config.Options != nil {
		if useMock, ok := config.Options["use_mock"]; ok && useMock.(bool) {
			resolver := mocks.NewMockDependencyResolver()
			// Note: Mock doesn't support debug mode configuration
			return resolver, nil
		}
	}

	// Use production dependency resolver with proper topological sorting
	resolver := dependency.NewProductionDependencyResolver(config.EnableDebugMode)
	return resolver, nil
}

// CreateInterpolationResolver creates an InterpolationResolver based on the configuration
func (f *DefaultComponentFactory) CreateInterpolationResolver(config interfaces.InterpolationResolverConfig) (interfaces.InterpolationResolver, error) {
	// Check if we should use mock for testing
	if config.Options != nil {
		if useMock, ok := config.Options["use_mock"]; ok && useMock.(bool) {
			return mocks.NewMockInterpolationResolver(), nil
		}
	}

	// Create HCL-based interpolation resolver with full Terraform-style functionality
	return interpolation.NewHCLInterpolationResolver(config.EnableStrictMode), nil
}

// CreateWorkerPool creates a Pool based on the configuration
func (f *DefaultComponentFactory) CreateWorkerPool(_ interfaces.PoolConfig) (interfaces.Pool, error) {
	// This is handled differently - the Pool is created directly in the system
	// because it needs the queue and executor which are created separately
	return nil, fmt.Errorf("pool creation is handled by the system itself")
}

// CreateProviderLifecycleManager creates a ProviderLifecycleManager based on the configuration
func (f *DefaultComponentFactory) CreateProviderLifecycleManager(config interfaces.ProviderManagerConfig) (interfaces.ProviderLifecycleManager, error) {
	// Check if we should use mock
	if config.Options != nil {
		if useMock, ok := config.Options["use_mock"]; ok && useMock.(bool) {
			providerManager := mocks.NewMockProviderManager()

			// Configure mock options
			if shouldFail, ok := config.Options["should_fail"]; ok {
				if fail, ok := shouldFail.(bool); ok && fail {
					providerManager.SetShouldFail(true)
				}
			}

			return providerManager, nil
		}
	}

	// Create real provider manager
	manager, err := f.createProviderManager(config)
	if err != nil {
		return nil, err
	}
	return manager, nil
}

// CreateProviderMonitor creates a ProviderMonitor based on the configuration
func (f *DefaultComponentFactory) CreateProviderMonitor(config interfaces.ProviderManagerConfig) (interfaces.ProviderMonitor, error) {
	// Check if we should use mock
	if config.Options != nil {
		if useMock, ok := config.Options["use_mock"]; ok && useMock.(bool) {
			return mocks.NewMockProviderManager(), nil
		}
	}

	// Create real provider manager
	manager, err := f.createProviderManager(config)
	if err != nil {
		return nil, err
	}
	return manager, nil
}

// extractAWSConfig extracts AWS configuration from the options map
func (f *DefaultComponentFactory) extractAWSConfig(options map[string]interface{}) (*state.AWSStateStoreConfig, error) {
	// Extract AWS configuration from the server config that was passed through options
	serverConfig, ok := options["server_config"].(*configpkg.ServerConfig)
	if !ok {
		return nil, fmt.Errorf("server configuration not found in options")
	}

	awsCfg := &serverConfig.StateStore.AWS

	// Convert server AWS config to state store AWS config
	stateStoreConfig := &state.AWSStateStoreConfig{
		// DynamoDB settings
		DynamoDBTable:  awsCfg.DynamoDB.Table,
		DynamoDBRegion: awsCfg.DynamoDB.Region,

		// S3 settings
		S3Bucket: awsCfg.S3.Bucket,
		S3Region: awsCfg.S3.Region,
		S3Prefix: awsCfg.S3.Prefix,

		// Common settings - prefer S3 endpoint, fallback to DynamoDB endpoint
		Endpoint: awsCfg.S3.Endpoint,

		// Locking configuration
		LockingEnabled: awsCfg.DynamoDB.Locking.Enabled,
	}

	// If S3 endpoint is empty but DynamoDB endpoint is set, use DynamoDB endpoint
	if stateStoreConfig.Endpoint == "" && awsCfg.DynamoDB.Endpoint != "" {
		stateStoreConfig.Endpoint = awsCfg.DynamoDB.Endpoint
	}

	// Configure lock settings if enabled
	if stateStoreConfig.LockingEnabled {
		// Use a separate table for locks to avoid conflicts with deployment data
		lockTableName := awsCfg.DynamoDB.Table + "-locks"
		stateStoreConfig.LockConfig = &state.DynamoDBLockConfig{
			TableName: lockTableName,
			Region:    awsCfg.DynamoDB.Region,
			Endpoint:  awsCfg.DynamoDB.Endpoint,
			TTL:       time.Duration(awsCfg.DynamoDB.Locking.TTLSeconds) * time.Second,
		}
	}

	return stateStoreConfig, nil
}
