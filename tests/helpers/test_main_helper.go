package helpers

import (
	"testing"

	"github.com/lattiam/lattiam/pkg/logging"
	"github.com/lattiam/lattiam/pkg/provider/protocol"
)

var logger = logging.NewLogger("test-helper")

// CommonProviders lists providers commonly used in tests
var CommonProviders = []struct {
	Name    string
	Version string
}{
	{"aws", "6.2.0"},
	{"random", "3.6.0"},
}

// SetupTestMain initializes the shared provider cache and pre-downloads providers
// Call this from TestMain in packages that use providers
func SetupTestMain(m *testing.M) int {
	// Initialize shared cache
	cache := GetSharedProviderCache()
	if err := cache.Initialize(); err != nil {
		logger.Error("Failed to initialize provider cache: %v", err)
		return 1
	}

	// Pre-download common providers
	logger.Info("Pre-downloading common providers for tests...")
	for _, provider := range CommonProviders {
		if err := cache.PredownloadProvider(provider.Name, provider.Version); err != nil {
			logger.Warn("Failed to pre-download %s@%s: %v",
				provider.Name, provider.Version, err)
			// Continue anyway - tests will skip if provider unavailable
		}
	}

	// Run tests
	code := m.Run()

	// Cleanup
	cache.Close()

	return code
}

// GetTestProviderManager returns a provider manager for use in tests
// It uses the shared cache directory to avoid redundant downloads
func GetTestProviderManager(t *testing.T) (*protocol.ProviderManager, error) {
	t.Helper()
	cache := GetSharedProviderCache()
	return cache.GetProviderManager(t)
}

// SkipIfProviderUnavailable skips the test if the provider cannot be downloaded
func SkipIfProviderUnavailable(t *testing.T, provider, version string) {
	t.Helper()
	cache := GetSharedProviderCache()
	if err := cache.PredownloadProvider(provider, version); err != nil {
		t.Skipf("Provider %s@%s unavailable: %v", provider, version, err)
	}
}
