// Package protocol implements direct communication with Terraform providers
// using the Terraform plugin protocol, bypassing HCL and terraform CLI
package protocol

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"github.com/lattiam/lattiam/internal/config"
	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/internal/provider"
)

// setupProcessGroup is implemented in platform-specific files:
// - process_unix.go for Unix/Linux/macOS
// - process_windows.go for Windows

// CleanupZombieProviders kills any existing zombie terraform provider processes
// This is useful for cleaning up before integration tests to prevent hangs
func CleanupZombieProviders() error {
	if runtime.GOOS == "windows" {
		return nil // No process group cleanup needed on Windows
	}

	// Kill any terraform-provider processes that may be zombies
	cmd := exec.Command("pkill", "-f", "terraform-provider") // #nosec G204 -- Fixed command for cleanup
	err := cmd.Run()
	if err != nil {
		// pkill returns exit code 1 if no processes found, which is fine
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return nil // No processes found
		}
		return fmt.Errorf("failed to cleanup zombie providers: %w", err)
	}

	// Give processes a moment to exit after being killed
	time.Sleep(100 * time.Millisecond)

	return nil
}

// Static errors for err113 compliance
var (
	ErrProviderStartTimeout = errors.New("timeout waiting for provider to start")
)

// Common AWS environment variables for provider setup
var awsEnvVars = []string{
	"AWS_ENDPOINT_URL",
	// Service-specific endpoints for LocalStack
	"AWS_ENDPOINT_URL_S3",
	"AWS_ENDPOINT_URL_DYNAMODB",
	"AWS_ENDPOINT_URL_EC2",
	"AWS_ENDPOINT_URL_IAM",
	"AWS_ENDPOINT_URL_STS",
	"AWS_ENDPOINT_URL_LAMBDA",
	"AWS_ENDPOINT_URL_CLOUDFORMATION",
	"AWS_ENDPOINT_URL_LOGS",
	"AWS_ENDPOINT_URL_EVENTS",
	"AWS_ENDPOINT_URL_KMS",
	"AWS_ENDPOINT_URL_SECRETSMANAGER",
	"AWS_ACCESS_KEY_ID",
	"AWS_SECRET_ACCESS_KEY",
	"AWS_DEFAULT_REGION",
	"AWS_REGION",
	"AWS_PROFILE",
	"AWS_SHARED_CREDENTIALS_FILE",
	"AWS_CONFIG_FILE",
	"AWS_SKIP_CREDENTIALS_VALIDATION",
	"AWS_SKIP_METADATA_API_CHECK",
	"AWS_SKIP_REQUESTING_ACCOUNT_ID",
	"AWS_EC2_METADATA_DISABLED",
	"AWS_DISABLE_SSL",
	"LOCALSTACK_ENDPOINT",
	"NO_PROXY",
	"SSH_AUTH_SOCK",
	"HOME",
	"USER",
	"XDG_RUNTIME_DIR",
	"PATH",
}

// ProviderManager manages provider binaries and their lifecycle
type ProviderManager struct {
	// baseDir is where provider binaries are stored
	baseDir string

	// httpClient for downloading providers
	httpClient *http.Client

	// schemaCache caches provider schemas for CLI backward compatibility
	schemaCache map[string]*tfprotov6.GetProviderSchemaResponse
	// activeProviders tracks all currently running provider instances
	activeProviders   []ProviderInstanceInterface
	activeProvidersMu sync.Mutex

	// providerPool caches configured provider instances per deployment (Phase 3 - Issue #24 fix)
	// Structure: map[deploymentID]map[providerName@version]*providerWrapper
	providerPool   map[string]map[string]provider.Provider
	providerPoolMu sync.RWMutex

	// config holds application configuration
	config *config.Config

	// downloader handles provider binary downloads
	downloader ProviderDownloaderInterface

	// binaryCache provides centralized binary caching (optional)
	binaryCache provider.BinaryCache

	// configSource provides provider configuration
	configSource interfaces.ProviderConfigSource

	// securityConfig provides security settings for provider downloads
	securityConfig *provider.SecureDownloaderConfig
}

// ProviderMetadata contains information about a provider
type ProviderMetadata struct {
	Name     string
	Version  string
	URL      string
	SHA256   string
	Platform string
}

// ProviderDownloaderInterface defines the interface for provider binary downloading
type ProviderDownloaderInterface interface {
	EnsureProviderBinary(ctx context.Context, name, version string) (string, error)
}

// CachedProviderDownloader wraps a regular downloader with binary caching
type CachedProviderDownloader struct {
	cache      provider.BinaryCache
	downloader *ProviderDownloader
}

// EnsureProviderBinary implements the same interface as ProviderDownloader but uses caching
func (c *CachedProviderDownloader) EnsureProviderBinary(ctx context.Context, name, version string) (string, error) {
	path, err := c.cache.GetProviderBinary(ctx, name, version)
	if err != nil {
		return "", fmt.Errorf("failed to get provider binary from cache: %w", err)
	}
	return path, nil
}

// ProviderManagerOption configures a ProviderManager
type ProviderManagerOption func(*ProviderManager)

// WithConfigSource sets a custom configuration source
func WithConfigSource(configSource interfaces.ProviderConfigSource) ProviderManagerOption {
	return func(pm *ProviderManager) {
		pm.configSource = configSource
	}
}

// WithBinaryCache enables centralized binary caching with the given cache implementation
func WithBinaryCache(cache provider.BinaryCache) ProviderManagerOption {
	return func(pm *ProviderManager) {
		pm.binaryCache = cache
	}
}

// WithDefaultBinaryCache enables centralized binary caching with default configuration
func WithDefaultBinaryCache() ProviderManagerOption {
	return func(pm *ProviderManager) {
		// Set a special marker interface to indicate default cache is wanted
		pm.binaryCache = (*provider.FileBinaryCache)(nil) // Nil pointer of the specific type
	}
}

// WithBaseDir sets a custom base directory for provider storage
func WithBaseDir(baseDir string) ProviderManagerOption {
	return func(pm *ProviderManager) {
		pm.baseDir = baseDir
	}
}

// WithSecurityConfig sets custom security configuration for provider downloads
func WithSecurityConfig(cfg *provider.SecureDownloaderConfig) ProviderManagerOption {
	return func(pm *ProviderManager) {
		pm.securityConfig = cfg
	}
}

// NewProviderManagerWithOptions creates a provider manager with the given options
func NewProviderManagerWithOptions(opts ...ProviderManagerOption) (*ProviderManager, error) {
	// Start with defaults
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get user home directory: %w", err)
	}

	baseDir := filepath.Join(homeDir, ".lattiam", "providers")
	cfg := config.LoadConfig()

	pm := &ProviderManager{
		baseDir:      baseDir,
		httpClient:   &http.Client{Timeout: config.GetHTTPClientTimeout()},
		schemaCache:  make(map[string]*tfprotov6.GetProviderSchemaResponse),
		providerPool: make(map[string]map[string]provider.Provider),
		config:       cfg,
		downloader:   nil, // Will be set after options are applied
		binaryCache:  nil, // Will be set if cache is requested
		configSource: nil, // Will be set to default if not provided
	}

	// Apply options
	for _, opt := range opts {
		opt(pm)
	}

	// Set up downloader with final baseDir
	if err := os.MkdirAll(pm.baseDir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create provider directory: %w", err)
	}

	// Check if default cache was requested (nil pointer indicates this)
	needsDefaultCache := pm.binaryCache == (*provider.FileBinaryCache)(nil)

	// Initialize default binary cache if requested
	if needsDefaultCache {
		cacheConfig := provider.DefaultBinaryCacheConfig()
		cacheConfig.CacheDirectory = filepath.Join(pm.baseDir, "cache")

		// Create the original downloader first
		originalDownloader := NewProviderDownloaderWithConfig(pm.baseDir, &http.Client{Timeout: config.GetHTTPClientTimeout()}, pm.securityConfig)

		// Create binary cache with the downloader
		binaryCache, err := provider.NewFileBinaryCache(cacheConfig, originalDownloader)
		if err != nil {
			return nil, fmt.Errorf("failed to create binary cache: %w", err)
		}

		// Use cached downloader wrapper
		pm.downloader = &CachedProviderDownloader{
			cache:      binaryCache,
			downloader: originalDownloader,
		}
		pm.binaryCache = binaryCache
	} else {
		// No cache or custom cache - use regular downloader
		pm.downloader = NewProviderDownloaderWithConfig(pm.baseDir, &http.Client{Timeout: config.GetHTTPClientTimeout()}, pm.securityConfig)

		// If custom cache was provided, wrap the downloader
		if pm.binaryCache != nil {
			if originalDownloader, ok := pm.downloader.(*ProviderDownloader); ok {
				pm.downloader = &CachedProviderDownloader{
					cache:      pm.binaryCache,
					downloader: originalDownloader,
				}
			}
		}
	}

	// Use default config source if none provided
	if pm.configSource == nil {
		// Use clean default configuration
		pm.configSource = config.NewDefaultConfigSource()
	}

	return pm, nil
}

// GetProvider returns a cached or fresh provider instance following Terraform's caching pattern
// Providers are cached per deployment to keep processes alive across resources
//
//nolint:gocognit,gocyclo,funlen // Provider lifecycle management requires complex logic
func (pm *ProviderManager) GetProvider(
	ctx context.Context, deploymentID, name, version string, cfg interfaces.ProviderConfig, _ string,
) (provider.Provider, error) {
	cacheKey := fmt.Sprintf("%s@%s", name, version)

	// Ensure deployment working directory exists
	_, err := createDeploymentWorkingDir(deploymentID)
	if err != nil {
		return nil, fmt.Errorf("failed to create deployment working directory: %w", err)
	}

retry:
	// Check provider pool cache first (read lock)
	pm.providerPoolMu.RLock()
	deploymentProviders, deploymentExists := pm.providerPool[deploymentID]
	if deploymentExists {
		if cached, exists := deploymentProviders[cacheKey]; exists {
			pm.providerPoolMu.RUnlock()

			// Check if the cached provider is still alive by getting its schema
			testCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			if _, err := cached.GetSchema(testCtx); err != nil {
				// Cached provider is dead, create new instance
				// Remove dead provider from cache
				pm.providerPoolMu.Lock()
				// Double-check it's still there before deleting
				if deploymentProviders, deploymentExists := pm.providerPool[deploymentID]; deploymentExists {
					if stillCached, stillExists := deploymentProviders[cacheKey]; stillExists && stillCached == cached {
						delete(deploymentProviders, cacheKey)
					}
				}
				pm.providerPoolMu.Unlock()

				// Clean up dead provider from activeProviders
				pm.activeProvidersMu.Lock()
				for i, instance := range pm.activeProviders {
					if instance.Name() == name && instance.Version() == version && !instance.IsHealthy() {
						// Remove only the dead instance from the slice
						pm.activeProviders = append(pm.activeProviders[:i], pm.activeProviders[i+1:]...)
						break
					}
				}
				pm.activeProvidersMu.Unlock()

				// Retry to check if another goroutine created a new provider
				goto retry
			}
			// Reusing cached provider instance

			// Reconfigure the cached provider if configuration is provided
			if len(cfg) > 0 {
				if providerConfig, exists := cfg[name]; exists {
					// Reconfiguring cached provider
					if wrapper, ok := cached.(*providerWrapper); ok {
						if err := wrapper.Configure(ctx, providerConfig); err != nil {
							// Failed to reconfigure cached provider
							// Provider might have died during reconfiguration, remove from cache and create new one
							pm.providerPoolMu.Lock()
							delete(pm.providerPool, cacheKey)
							pm.providerPoolMu.Unlock()
							// Fall through to create a new instance instead of returning error
						} else {
							// Configuration successful, return the cached provider
							return cached, nil
						}
					} else {
						// Cached provider is not a providerWrapper
						return cached, nil
					}
				} else {
					// No config found for provider in config map
					return cached, nil
				}
			} else {
				// No configuration needed, return cached provider
				return cached, nil
			}
		} else {
			// No cached provider found in this deployment
			pm.providerPoolMu.RUnlock()
		}
	} else {
		// No deployment cache exists yet
		pm.providerPoolMu.RUnlock()
	}

	// Create new provider instance (write lock)
	pm.providerPoolMu.Lock()
	defer pm.providerPoolMu.Unlock()

	// Double-check cache after acquiring write lock
	deploymentProviders, deploymentExists = pm.providerPool[deploymentID]
	if deploymentExists {
		if cached, exists := deploymentProviders[cacheKey]; exists {
			// Re-check health even in the double-check pattern to avoid race conditions
			testCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			defer cancel()
			if _, err := cached.GetSchema(testCtx); err != nil {
				// Cached provider is dead during double-check, create new instance
				// Remove dead provider from cache
				delete(deploymentProviders, cacheKey)

				// Clean up dead provider from activeProviders
				pm.activeProvidersMu.Lock()
				for i, instance := range pm.activeProviders {
					if instance.Name() == name && instance.Version() == version && !instance.IsHealthy() {
						// Remove only the dead instance from the slice
						pm.activeProviders = append(pm.activeProviders[:i], pm.activeProviders[i+1:]...)
						break
					}
				}
				pm.activeProvidersMu.Unlock()

				// Fall through to create a new instance
			} else {
				// Reusing cached provider instance (race condition avoided)

				// Reconfigure the cached provider if configuration is provided
				if len(cfg) > 0 {
					if providerConfig, exists := cfg[name]; exists {
						// Reconfiguring cached provider
						if wrapper, ok := cached.(*providerWrapper); ok {
							if err := wrapper.Configure(ctx, providerConfig); err != nil {
								// Failed to reconfigure cached provider
								// Provider might have died during reconfiguration, remove from cache and create new one
								delete(deploymentProviders, cacheKey)
								// Fall through to create a new instance instead of returning error
							} else {
								// Configuration successful, return the cached provider
								return cached, nil
							}
						} else {
							return cached, nil
						}
					} else {
						return cached, nil
					}
				} else {
					// No configuration needed, return cached provider
					return cached, nil
				}
			}
		}
	}

	// Creating fresh provider instance

	// Download if necessary
	binPath, err := pm.downloader.EnsureProviderBinary(ctx, name, version)
	if err != nil {
		return nil, fmt.Errorf("failed to ensure provider binary: %w", err)
	}

	// Get configuration from configSource if available
	if pm.configSource != nil && cfg == nil {
		// For now, pass nil schema to config source
		// The config source should provide sensible defaults without schema
		sourceConfig, err := pm.configSource.GetProviderConfig(ctx, name, version, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to get provider config from source: %w", err)
		}

		cfg = sourceConfig
	}

	// Start provider instance with deployment-specific working directory
	provInstance, err := pm.startProviderWithConfigAndDeployment(ctx, deploymentID, name, version, binPath, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to start provider: %w", err)
	}

	// Create provider client using build-tag specific implementation
	wrapper, err := pm.createProviderClient(ctx, provInstance, name, cfg)
	if err != nil {
		return nil, err
	}

	// Before adding to cache, clean up any stale entries in activeProviders
	// This ensures we don't have duplicate entries for the same provider
	pm.activeProvidersMu.Lock()
	for i := len(pm.activeProviders) - 1; i >= 0; i-- {
		instance := pm.activeProviders[i]
		if instance.Name() == name && instance.Version() == version && instance != provInstance {
			// Only remove if the instance is unhealthy
			if !instance.IsHealthy() {
				pm.activeProviders = append(pm.activeProviders[:i], pm.activeProviders[i+1:]...)
			}
		}
	}
	pm.activeProvidersMu.Unlock()

	// Note: provInstance is already added to activeProviders in startProviderWithConfigAndDeployment,
	// so we don't need to add it again here

	// Cache the provider instance for reuse in deployment-scoped cache
	if pm.providerPool[deploymentID] == nil {
		pm.providerPool[deploymentID] = make(map[string]provider.Provider)
	}
	pm.providerPool[deploymentID][cacheKey] = wrapper

	// Provider instance created and cached

	return wrapper, nil
}

// EnsureProviderBinary ensures a provider binary is available
func (pm *ProviderManager) EnsureProviderBinary(ctx context.Context, name, version string) (string, error) {
	path, err := pm.downloader.EnsureProviderBinary(ctx, name, version)
	if err != nil {
		return "", fmt.Errorf("failed to ensure provider binary: %w", err)
	}
	return path, nil
}

// DownloadProvider downloads a provider if not already available
func (pm *ProviderManager) DownloadProvider(ctx context.Context, name, version string) error {
	_, err := pm.EnsureProviderBinary(ctx, name, version)
	return err
}

// startProviderWithConfigAndDeployment launches a provider process with specific configuration and deployment isolation
func (pm *ProviderManager) startProviderWithConfigAndDeployment(
	ctx context.Context, deploymentID, name, version, binPath string, cfg interfaces.ProviderConfig,
) (ProviderInstanceInterface, error) {
	debugLogger := GetDebugLogger()
	debugLogger.Logf("provider-start", "Starting provider %s v%s from %s for deployment %s", name, version, binPath, deploymentID)

	// Use background context for provider process to ensure it survives deployment completion
	// Providers are cached and reused across deployments, so they should not be tied to deployment lifecycle
	cmd := exec.CommandContext(context.Background(), binPath) // #nosec G204 -- binPath is validated by provider cache
	env := pm.setupBaseEnvironment()

	// Apply deployment-specific configuration first
	env = pm.applyProviderConfig(env, cfg, debugLogger)

	// Then apply AWS environment variables from the server process
	env = pm.applyAWSEnvironmentVars(env, cfg)

	// Set deployment-specific working directory
	deploymentWorkingDir := filepath.Join(os.TempDir(), "lattiam-deployments", deploymentID)
	env = append(env,
		"TF_DATA_DIR="+filepath.Join(deploymentWorkingDir, "terraform-state"),
		"TF_PLUGIN_CACHE_DIR="+filepath.Join(deploymentWorkingDir, "provider-state"))

	cmd.Env = env

	// Set up process group management to prevent zombie processes
	setupProcessGroup(cmd)

	// Enable debug logging if needed
	pm.setupDebugLogging(cmd, debugLogger, name, version)

	// Log AWS provider environment variables for troubleshooting
	if name == providerAWS && debugLogger.IsEnabled() {
		debugLogger.Logf("provider-start", "AWS provider environment variables:")
		for _, env := range cmd.Env {
			if strings.HasPrefix(env, "AWS_") || strings.HasPrefix(env, "LATTIAM_") {
				debugLogger.Logf("provider-start", "  %s", env)
			}
		}
	}

	// Create a plugin-based provider instance with the configured environment
	// The go-plugin library handles all the stdout/stderr management
	debugLogger.Logf("provider-start", "Creating plugin provider instance for %s with configured environment and deployment working dir", name)
	instance, err := NewPluginProviderInstanceWithEnv(ctx, binPath, name, version, cmd.Env)
	if err != nil {
		return nil, err
	}

	// Track the provider instance
	pm.activeProvidersMu.Lock()
	pm.activeProviders = append(pm.activeProviders, instance)
	pm.activeProvidersMu.Unlock()

	return instance, nil
}

// createDeploymentWorkingDir creates deployment-specific working directory
func createDeploymentWorkingDir(deploymentID string) (string, error) {
	baseDir := filepath.Join(os.TempDir(), "lattiam-deployments", deploymentID)

	// Create the directory structure
	providerStateDir := filepath.Join(baseDir, "provider-state")
	terraformStateDir := filepath.Join(baseDir, "terraform-state")

	if err := os.MkdirAll(providerStateDir, 0o700); err != nil {
		return "", fmt.Errorf("failed to create provider state directory: %w", err)
	}

	if err := os.MkdirAll(terraformStateDir, 0o700); err != nil {
		return "", fmt.Errorf("failed to create terraform state directory: %w", err)
	}

	return baseDir, nil
}

// cleanupDeploymentWorkingDir removes deployment-specific working directory
func cleanupDeploymentWorkingDir(deploymentID string) error {
	baseDir := filepath.Join(os.TempDir(), "lattiam-deployments", deploymentID)
	if err := os.RemoveAll(baseDir); err != nil {
		return fmt.Errorf("failed to remove deployment directory: %w", err)
	}
	return nil
}

// setEnvVar sets or updates an environment variable in a slice of env vars
func setEnvVar(env []string, key, value string) []string {
	prefix := key + "="
	for i, envVar := range env {
		if strings.HasPrefix(envVar, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

// setupBaseEnvironment sets up the base environment for the provider
func (pm *ProviderManager) setupBaseEnvironment() []string {
	const magicCookie = "d602bf8f470bc67ca7faa0386276bbdd4330efaf76d1a219cb4d6991ca9872b2"
	env := os.Environ()
	env = append(env, "TF_PLUGIN_MAGIC_COOKIE="+magicCookie)
	return env
}

// applyProviderConfig applies provider-specific configuration to environment
func (pm *ProviderManager) applyProviderConfig(env []string, cfg interfaces.ProviderConfig, debugLogger *DebugLogger) []string {
	if cfg == nil {
		return env
	}

	for providerName, providerConfig := range cfg {
		// Special handling for AWS provider to set correct environment variables
		if providerName == "aws" {
			env = pm.applyAWSProviderConfig(env, providerConfig, debugLogger)
			continue
		}

		// Generic handling for other providers
		for key, value := range providerConfig {
			envKey := strings.ToUpper(fmt.Sprintf("%s_%s", providerName, key))
			env = setEnvVar(env, envKey, fmt.Sprintf("%v", value))
			debugLogger.Logf("provider-start", "Setting %s=%v for provider %s", envKey, value, providerName)
		}
	}
	return env
}

// applyAWSProviderConfig applies AWS-specific configuration with correct environment variable names
func (pm *ProviderManager) applyAWSProviderConfig(env []string, providerConfig map[string]interface{}, debugLogger *DebugLogger) []string {
	// Map our config keys to correct AWS environment variables
	envVarMapping := map[string]string{
		"access_key": "AWS_ACCESS_KEY_ID",
		"secret_key": "AWS_SECRET_ACCESS_KEY",
		"region":     "AWS_DEFAULT_REGION",
	}

	for key, value := range providerConfig {
		// Handle endpoints specially
		if key == "endpoints" {
			if endpoints, ok := value.(map[string]interface{}); ok {
				// Set service-specific endpoint URLs
				for service, endpoint := range endpoints {
					envKey := fmt.Sprintf("AWS_ENDPOINT_URL_%s", strings.ToUpper(service))
					env = setEnvVar(env, envKey, fmt.Sprintf("%v", endpoint))
					debugLogger.Logf("provider-start", "Setting %s=%v for AWS provider", envKey, endpoint)
				}
				// Also set a general endpoint URL if S3 is configured (for LocalStack compatibility)
				if s3Endpoint, ok := endpoints["s3"]; ok {
					env = setEnvVar(env, "AWS_ENDPOINT_URL", fmt.Sprintf("%v", s3Endpoint))
					debugLogger.Logf("provider-start", "Setting AWS_ENDPOINT_URL=%v for AWS provider", s3Endpoint)
				}
			}
			continue
		}

		// Use mapped environment variable name if available
		if envKey, ok := envVarMapping[key]; ok {
			env = setEnvVar(env, envKey, fmt.Sprintf("%v", value))
			debugLogger.Logf("provider-start", "Setting %s=%v for AWS provider", envKey, value)
		} else {
			// For unmapped keys, use the generic AWS_ prefix
			envKey := fmt.Sprintf("AWS_%s", strings.ToUpper(key))
			env = setEnvVar(env, envKey, fmt.Sprintf("%v", value))
			debugLogger.Logf("provider-start", "Setting %s=%v for AWS provider", envKey, value)
		}
	}

	return env
}

// applyAWSEnvironmentVars applies AWS environment variables to the command environment
func (pm *ProviderManager) applyAWSEnvironmentVars(env []string, cfg interfaces.ProviderConfig) []string {
	for _, envVar := range awsEnvVars {
		if value := os.Getenv(envVar); value != "" {
			// Don't override if already set by config
			if pm.shouldSkipEnvVar(envVar, cfg) {
				continue
			}
			env = append(env, envVar+"="+value)
		}
	}
	return env
}

// shouldSkipEnvVar checks if an environment variable should be skipped
func (pm *ProviderManager) shouldSkipEnvVar(envVar string, cfg interfaces.ProviderConfig) bool {
	if cfg == nil {
		return false
	}

	// Check if the env var is managed by any provider config
	for providerName, providerConfig := range cfg {
		envKeyPrefix := strings.ToUpper(providerName) + "_"
		if strings.HasPrefix(envVar, envKeyPrefix) {
			configKey := strings.ToLower(strings.TrimPrefix(envVar, envKeyPrefix))
			if _, exists := providerConfig[configKey]; exists {
				return true
			}
		}
	}

	return false
}

// setupDebugLogging configures debug logging for the provider
func (pm *ProviderManager) setupDebugLogging(cmd *exec.Cmd, debugLogger *DebugLogger, name, version string) {
	if debugLogger.IsEnabled() {
		cmd.Env = append(cmd.Env, "TF_LOG=DEBUG")
		providerLogFile := debugLogger.LogFile(fmt.Sprintf("terraform-provider-%s-%s", name, version))
		cmd.Env = append(cmd.Env, "TF_LOG_PATH="+providerLogFile)
		debugLogger.Logf("provider-start", "Provider debug log: %s", providerLogFile)
	}
}

// GetCachedSchema retrieves a cached provider schema

// ListProviders returns information about available providers
func (pm *ProviderManager) ListProviders() []provider.Info {
	pm.activeProvidersMu.Lock()
	defer pm.activeProvidersMu.Unlock()

	infos := make([]provider.Info, len(pm.activeProviders))
	for i, instance := range pm.activeProviders {
		infos[i] = provider.Info{
			Name:    instance.Name(),
			Version: instance.Version(),
			Status:  "running",
		}
	}
	return infos
}

// ReleaseProvider releases a specific provider for a deployment
func (pm *ProviderManager) ReleaseProvider(deploymentID, name string) error {
	pm.providerPoolMu.Lock()
	defer pm.providerPoolMu.Unlock()

	deploymentProviders, exists := pm.providerPool[deploymentID]
	if !exists {
		// Deployment doesn't exist, nothing to release
		return nil
	}

	// Look for the provider by name (try all versions)
	var keysToRemove []string
	for cacheKey, provider := range deploymentProviders {
		// Check if this cache key belongs to the provider we want to release
		if strings.HasPrefix(cacheKey, name+"@") {
			keysToRemove = append(keysToRemove, cacheKey)

			// Stop the provider if it's a wrapper
			if wrapper, ok := provider.(*providerWrapper); ok {
				if err := wrapper.provInstance.Stop(); err != nil {
					GetDebugLogger().Logf("provider-release", "Failed to stop provider %s: %v", cacheKey, err)
				}
			}
		}
	}

	// Remove from cache
	for _, key := range keysToRemove {
		delete(deploymentProviders, key)
	}

	// Clean up from activeProviders
	pm.activeProvidersMu.Lock()
	for i := len(pm.activeProviders) - 1; i >= 0; i-- {
		instance := pm.activeProviders[i]
		if instance.Name() == name {
			pm.activeProviders = append(pm.activeProviders[:i], pm.activeProviders[i+1:]...)
		}
	}
	pm.activeProvidersMu.Unlock()

	return nil
}

// ShutdownDeployment shuts down all providers for a specific deployment
func (pm *ProviderManager) ShutdownDeployment(deploymentID string) error {
	pm.providerPoolMu.Lock()
	defer pm.providerPoolMu.Unlock()

	deploymentProviders, exists := pm.providerPool[deploymentID]
	if !exists {
		// Deployment doesn't exist, nothing to shutdown
		return nil
	}

	var errs []error

	// Stop all providers in this deployment
	for cacheKey, provider := range deploymentProviders {
		if wrapper, ok := provider.(*providerWrapper); ok {
			if err := wrapper.provInstance.Stop(); err != nil {
				errs = append(errs, fmt.Errorf("failed to stop provider %s: %w", cacheKey, err))
			}
		}
	}

	// Remove entire deployment from cache
	delete(pm.providerPool, deploymentID)

	// Clean up deployment working directory
	if err := cleanupDeploymentWorkingDir(deploymentID); err != nil {
		errs = append(errs, fmt.Errorf("failed to cleanup deployment working directory: %w", err))
	}

	// Clean up from activeProviders
	pm.activeProvidersMu.Lock()
	// Remove providers that belonged to this deployment
	// Since we don't track deploymentID in activeProviders, we can only remove unhealthy providers
	for i := len(pm.activeProviders) - 1; i >= 0; i-- {
		instance := pm.activeProviders[i]
		if !instance.IsHealthy() {
			pm.activeProviders = append(pm.activeProviders[:i], pm.activeProviders[i+1:]...)
		}
	}
	pm.activeProvidersMu.Unlock()

	if len(errs) > 0 {
		return fmt.Errorf("deployment shutdown errors: %v", errs)
	}
	return nil
}

// Close closes the provider manager and cleans up resources
func (pm *ProviderManager) Close() error {
	// Acquire locks in the same order as GetProvider() to prevent deadlock
	// Order: providerPoolMu first, then activeProvidersMu
	pm.providerPoolMu.Lock()
	pm.activeProvidersMu.Lock()

	var firstErr error

	// Clear activeProviders since we'll be stopping all providers via providerPool
	pm.activeProviders = nil
	pm.activeProvidersMu.Unlock()

	// Stop all cached provider instances (this covers all providers)
	for deploymentID, deploymentProviders := range pm.providerPool {
		for key, provider := range deploymentProviders {
			if wrapper, ok := provider.(*providerWrapper); ok {
				// Stop the provider instance
				if err := wrapper.provInstance.Stop(); err != nil {
					if firstErr == nil {
						firstErr = fmt.Errorf("failed to stop provider instance for %s in deployment %s: %w", key, deploymentID, err)
					}
				}
			}
		}
	}

	// Clear provider pool after stopping providers
	pm.providerPool = make(map[string]map[string]provider.Provider)
	pm.schemaCache = make(map[string]*tfprotov6.GetProviderSchemaResponse)

	pm.providerPoolMu.Unlock()

	return firstErr
}

// CleanupProviders stops all cached providers and clears the pool
// This should only be called during shutdown or when all operations are complete
func (pm *ProviderManager) CleanupProviders() error {
	pm.providerPoolMu.Lock()
	defer pm.providerPoolMu.Unlock()

	var errs []error
	for deploymentID, deploymentProviders := range pm.providerPool {
		for key, provider := range deploymentProviders {
			if wrapper, ok := provider.(*providerWrapper); ok {
				// Stop the provider instance
				if err := wrapper.provInstance.Stop(); err != nil {
					errs = append(errs, fmt.Errorf("failed to stop provider %s in deployment %s: %w", key, deploymentID, err))
				}
			}
		}
	}

	// Clear the provider pool
	pm.providerPool = make(map[string]map[string]provider.Provider)

	if len(errs) > 0 {
		return fmt.Errorf("cleanup errors: %v", errs)
	}
	return nil
}

// ClearProviderCache removes all cached providers without stopping them
// This is useful for tests or when providers may have died externally
func (pm *ProviderManager) ClearProviderCache() {
	pm.providerPoolMu.Lock()
	defer pm.providerPoolMu.Unlock()

	// Clearing provider cache
	pm.providerPool = make(map[string]map[string]provider.Provider)
}
