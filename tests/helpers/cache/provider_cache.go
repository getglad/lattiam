// Package cache provides test helpers for provider caching
package cache

import (
	"os"
	"path/filepath"
	"sync"
)

// SharedCache is a singleton cache for provider binaries
type SharedCache struct {
	mu          sync.RWMutex
	baseDir     string
	downloaded  map[string]bool
	downloadErr map[string]error
}

var (
	sharedCacheOnce sync.Once
	sharedCache     *SharedCache
)

// GetSharedCache returns the singleton provider cache
func GetSharedCache() *SharedCache {
	sharedCacheOnce.Do(func() {
		// Use environment variable if set, otherwise use default
		cacheDir := os.Getenv("LATTIAM_TEST_PROVIDER_CACHE")
		if cacheDir == "" {
			homeDir, _ := os.UserHomeDir()
			cacheDir = filepath.Join(homeDir, ".lattiam", "test-providers")
		}

		sharedCache = &SharedCache{
			baseDir:     cacheDir,
			downloaded:  make(map[string]bool),
			downloadErr: make(map[string]error),
		}

		// Create cache directory
		_ = os.MkdirAll(cacheDir, 0o750) // Ignore error - cache directory creation is best effort
	})
	return sharedCache
}

// GetBaseDir returns the cache base directory
func (c *SharedCache) GetBaseDir() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.baseDir
}

// MarkDownloaded marks a provider as downloaded
func (c *SharedCache) MarkDownloaded(key string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.downloaded[key] = true
	c.downloadErr[key] = err
}

// IsDownloaded checks if a provider has been downloaded
func (c *SharedCache) IsDownloaded(key string) (bool, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.downloaded[key] {
		return true, c.downloadErr[key]
	}
	return false, nil
}
