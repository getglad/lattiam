package protocol

import (
	"context"
	"fmt"
	"log"
	"sync"
	"testing"
	"time"

	"github.com/lattiam/lattiam/tests/helpers/cache"
)

// TestProviderCache wraps the cache for use in tests
type TestProviderCache struct {
	mu      sync.Mutex
	cache   *cache.SharedCache
	manager *ProviderManager
}

var (
	testCacheOnce sync.Once
	testCache     *TestProviderCache
)

// GetTestCache returns the singleton test cache
func GetTestCache() *TestProviderCache {
	testCacheOnce.Do(func() {
		testCache = &TestProviderCache{
			cache: cache.GetSharedCache(),
		}
	})
	return testCache
}

// Initialize sets up the provider manager
func (c *TestProviderCache) Initialize() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.manager != nil {
		return nil // Already initialized
	}

	// Create provider manager with cache directory
	manager, err := NewProviderManagerWithOptions(
		WithBaseDir(c.cache.GetBaseDir()),
	)
	if err != nil {
		return fmt.Errorf("failed to create provider manager: %w", err)
	}

	c.manager = manager
	return nil
}

// PredownloadProvider downloads a provider if not already cached
func (c *TestProviderCache) PredownloadProvider(provider, version string) error {
	key := fmt.Sprintf("%s@%s", provider, version)

	// Check if already downloaded
	if downloaded, err := c.cache.IsDownloaded(key); downloaded {
		return err //nolint:wrapcheck // test helper function
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring lock
	if downloaded, err := c.cache.IsDownloaded(key); downloaded {
		return err //nolint:wrapcheck // test helper function
	}

	// Download the provider
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	_, err := c.manager.EnsureProviderBinary(ctx, provider, version)
	c.cache.MarkDownloaded(key, err)

	return err
}

// GetProviderManager returns a provider manager for use in tests
func (c *TestProviderCache) GetProviderManager() (*ProviderManager, error) {
	return NewProviderManagerWithOptions(
		WithBaseDir(c.cache.GetBaseDir()),
	)
}

// SetupTestProviders pre-downloads common test providers
func SetupTestProviders() {
	cache := GetTestCache()
	if err := cache.Initialize(); err != nil {
		log.Printf("Failed to initialize provider cache: %v", err)
		return
	}

	// Pre-download common providers
	providers := []struct {
		Name    string
		Version string
	}{
		{"aws", "6.2.0"},
		{"random", "3.6.0"},
	}

	log.Println("Pre-downloading common providers for tests...")
	for _, provider := range providers {
		if err := cache.PredownloadProvider(provider.Name, provider.Version); err != nil {
			log.Printf("Warning: Failed to pre-download %s@%s: %v",
				provider.Name, provider.Version, err)
		}
	}
}

// GetTestProviderManager returns a provider manager for use in tests
func GetTestProviderManager(t *testing.T) (*ProviderManager, error) {
	t.Helper()
	cache := GetTestCache()
	return cache.GetProviderManager()
}
