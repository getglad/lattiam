package helpers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/lattiam/lattiam/pkg/provider/protocol"
	"github.com/lattiam/lattiam/tests/helpers/localstack"
)

// ProviderDownloader manages provider downloads for tests
type ProviderDownloader struct {
	mu           sync.Mutex
	downloadOnce map[string]*sync.Once
	downloadErr  map[string]error
	providerDir  string
}

// createProviderManager creates a provider manager with explicit LocalStack configuration
func createProviderManager(providerDir string) (*protocol.ProviderManager, error) {
	var opts []protocol.ProviderManagerOption
	opts = append(opts, protocol.WithBaseDir(providerDir), protocol.WithConfigSource(localstack.NewConfigSource()))

	pm, err := protocol.NewProviderManagerWithOptions(opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create provider manager: %w", err)
	}
	return pm, nil
}

// NewProviderDownloader creates a new provider downloader for tests
func NewProviderDownloader(providerDir string) *ProviderDownloader {
	return &ProviderDownloader{
		downloadOnce: make(map[string]*sync.Once),
		downloadErr:  make(map[string]error),
		providerDir:  providerDir,
	}
}

// DefaultProviderDownloader uses the same directory as the application
var DefaultProviderDownloader = func() *ProviderDownloader { //nolint:gochecknoglobals // Test helper singleton
	homeDir, _ := os.UserHomeDir()
	providerDir := filepath.Join(homeDir, ".lattiam", "providers")
	return NewProviderDownloader(providerDir)
}()

// EnsureProvider downloads a provider if not already present
func (pd *ProviderDownloader) EnsureProvider(t *testing.T, providerName, version string) error {
	t.Helper()

	key := providerName + "@" + version

	pd.mu.Lock()
	if pd.downloadOnce[key] == nil {
		pd.downloadOnce[key] = &sync.Once{}
	}
	once := pd.downloadOnce[key]
	pd.mu.Unlock()

	once.Do(func() {
		t.Logf("Downloading provider %s version %s to %s", providerName, version, pd.providerDir)

		// Create provider directory if it doesn't exist
		if err := os.MkdirAll(pd.providerDir, 0o750); err != nil {
			pd.downloadErr[key] = err
			return
		}

		// Create provider manager with explicit configuration
		manager, err := createProviderManager(pd.providerDir)
		if err != nil {
			pd.downloadErr[key] = err
			return
		}
		defer func() { _ = manager.Close() }() // Ignore error - manager may already be closed

		// Download provider with timeout
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		_, err = manager.EnsureProviderBinary(ctx, providerName, version)
		pd.downloadErr[key] = err

		if err == nil {
			t.Logf("Successfully downloaded provider %s version %s", providerName, version)
		}
	})

	return pd.downloadErr[key]
}

// SkipIfProviderUnavailable skips the test if provider download fails
func (pd *ProviderDownloader) SkipIfProviderUnavailable(t *testing.T, providerName, version string) {
	t.Helper()

	if err := pd.EnsureProvider(t, providerName, version); err != nil {
		t.Skipf("Skipping test - provider %s version %s unavailable: %v", providerName, version, err)
	}
}

// EnsureAWSProvider is a convenience method for the common AWS provider
func EnsureAWSProvider(t *testing.T) error {
	t.Helper()
	return DefaultProviderDownloader.EnsureProvider(t, "aws", "6.2.0")
}

// SkipIfAWSUnavailable skips the test if AWS provider is unavailable
func SkipIfAWSUnavailable(t *testing.T) {
	t.Helper()
	DefaultProviderDownloader.SkipIfProviderUnavailable(t, "aws", "6.2.0")
}

// SetupProviders pre-downloads providers before running tests
func SetupProviders(m *testing.M) int {
	// Pre-download common providers to shared location (same as app)
	homeDir, _ := os.UserHomeDir()
	providerDir := filepath.Join(homeDir, ".lattiam", "providers")

	// Check if AWS provider already exists
	awsProviderPath := filepath.Join(providerDir, "registry.terraform.io", "hashicorp", "aws", "6.2.0")
	if _, err := os.Stat(awsProviderPath); err == nil {
		// Provider already downloaded, skip
		return m.Run()
	}

	// Only download if not already present
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err := os.MkdirAll(providerDir, 0o750); err != nil {
		// Failed to create provider directory
		return 1
	}

	// Create provider manager with explicit LocalStack configuration
	manager, err := createProviderManager(providerDir)
	if err == nil {
		// Pre-download AWS provider
		if _, err := manager.EnsureProviderBinary(ctx, "aws", "6.2.0"); err != nil {
			// Pre-download failed, will download on demand
			_ = err
		}
		// Add other providers as needed
		// manager.EnsureProviderBinary(ctx, "random", "3.6.0")
		_ = manager.Close() // Ignore error - manager may already be closed
	}

	return m.Run()
}

// CleanupProviders removes downloaded providers (call in TestMain after tests)
// Note: We don't clean up ~/.lattiam/providers since it's shared with the app
func CleanupProviders() {
	// Intentionally not removing ~/.lattiam/providers
	// as it's a shared cache with the application
}
