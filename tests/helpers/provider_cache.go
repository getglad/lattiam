package helpers

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/lattiam/lattiam/pkg/provider/protocol"
	"github.com/lattiam/lattiam/tests/helpers/cache"
)

// SharedProviderCache wraps the cache package
type SharedProviderCache struct {
	mu      sync.Mutex
	cache   *cache.SharedCache
	manager *protocol.ProviderManager
}

var (
	sharedProviderCacheOnce sync.Once
	sharedProviderCache     *SharedProviderCache
)

// GetSharedProviderCache returns the singleton provider cache
func GetSharedProviderCache() *SharedProviderCache {
	sharedProviderCacheOnce.Do(func() {
		sharedProviderCache = &SharedProviderCache{
			cache: cache.GetSharedCache(),
		}
	})
	return sharedProviderCache
}

// Initialize sets up the provider manager
func (c *SharedProviderCache) Initialize() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.manager != nil {
		return nil // Already initialized
	}

	// Create provider manager with cache directory
	manager, err := protocol.NewProviderManagerWithOptions(
		protocol.WithBaseDir(c.cache.GetBaseDir()),
	)
	if err != nil {
		return fmt.Errorf("failed to create provider manager: %w", err)
	}

	c.manager = manager
	return nil
}

// PredownloadProvider downloads a provider if not already cached
func (c *SharedProviderCache) PredownloadProvider(provider, version string) error {
	key := fmt.Sprintf("%s@%s", provider, version)

	// Check if already downloaded
	if downloaded, err := c.cache.IsDownloaded(key); downloaded {
		if err != nil {
			return fmt.Errorf("check provider download status: %w", err)
		}
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring lock
	if downloaded, err := c.cache.IsDownloaded(key); downloaded {
		if err != nil {
			return fmt.Errorf("check provider download status after lock: %w", err)
		}
		return nil
	}

	// Download the provider
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	_, err := c.manager.EnsureProviderBinary(ctx, provider, version)
	c.cache.MarkDownloaded(key, err)

	if err != nil {
		return fmt.Errorf("ensure provider binary %s@%s: %w", provider, version, err)
	}
	return nil
}

// GetProviderManager returns a provider manager configured to use the shared cache
func (c *SharedProviderCache) GetProviderManager(t *testing.T) (*protocol.ProviderManager, error) {
	t.Helper()

	// Create a new manager instance for this test (but using shared cache directory)
	manager, err := protocol.NewProviderManagerWithOptions(
		protocol.WithBaseDir(c.cache.GetBaseDir()),
	)
	if err != nil {
		return nil, fmt.Errorf("create provider manager for test: %w", err)
	}
	return manager, nil
}

// Close cleans up the shared cache (call in TestMain after all tests)
func (c *SharedProviderCache) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.manager != nil {
		_ = c.manager.Close() // Ignore error - manager may already be closed
		c.manager = nil
	}
}
