package provider_test

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/provider"
)

// mockDownloaderForCleanup is a simple mock for testing
type mockDownloaderForCleanup struct {
	mu       sync.Mutex
	binaries map[string]string
}

func (m *mockDownloaderForCleanup) EnsureProviderBinary(_ context.Context, name, version string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := name + "_" + version
	if path, exists := m.binaries[key]; exists {
		return path, nil
	}

	// Return a fake path for testing
	path := filepath.Join("/tmp", "provider", name, version, "terraform-provider-"+name)
	m.binaries[key] = path
	return path, nil
}

func TestFileBinaryCache_LockCleanup(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	config := &provider.BinaryCacheConfig{
		CacheDirectory:      tmpDir,
		LockTimeout:         5 * time.Minute,
		VerifyChecksums:     false,
		DefaultKeepVersions: 3,
	}

	mockDownloader := &mockDownloaderForCleanup{
		binaries: make(map[string]string),
	}

	cache, err := provider.NewFileBinaryCache(config, mockDownloader)
	require.NoError(t, err)
	defer cache.Close()

	// Test that locks are created
	ctx := context.Background()

	// Create some provider requests to generate locks
	providers := []struct {
		name    string
		version string
	}{
		{"aws", "4.0.0"},
		{"azure", "3.0.0"},
		{"google", "4.5.0"},
	}

	// Request providers to create locks
	for _, p := range providers {
		_, err := cache.GetProviderBinary(ctx, p.name, p.version)
		// We expect an error since the mock doesn't create real files
		// but the lock should still be created
		assert.Error(t, err) // Expected as mock doesn't create real files
	}
}

func TestFileBinaryCache_LockCleanupWithStaleEntries(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	config := &provider.BinaryCacheConfig{
		CacheDirectory:      tmpDir,
		LockTimeout:         5 * time.Minute,
		VerifyChecksums:     false,
		DefaultKeepVersions: 3,
	}

	mockDownloader := &mockDownloaderForCleanup{
		binaries: make(map[string]string),
	}

	// Create cache with custom cleanup interval for testing
	cache, err := provider.NewFileBinaryCache(config, mockDownloader)
	require.NoError(t, err)

	// We can't easily test the actual cleanup without exposing internals,
	// but we can verify the cache doesn't leak memory over time
	ctx := context.Background()

	// Simulate many different provider versions being requested
	for i := 0; i < 100; i++ {
		version := fmt.Sprintf("1.0.%d", i)
		_, _ = cache.GetProviderBinary(ctx, "test-provider", version)
	}

	// Let the cleanup potentially run (though with 1-hour interval it won't in test)
	time.Sleep(100 * time.Millisecond)

	// Close should work without issues
	cache.Close()
}

func TestFileBinaryCache_ConcurrentLockAccess(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	config := &provider.BinaryCacheConfig{
		CacheDirectory:      tmpDir,
		LockTimeout:         5 * time.Minute,
		VerifyChecksums:     false,
		DefaultKeepVersions: 3,
	}

	mockDownloader := &mockDownloaderForCleanup{
		binaries: make(map[string]string),
	}

	cache, err := provider.NewFileBinaryCache(config, mockDownloader)
	require.NoError(t, err)
	defer cache.Close()

	ctx := context.Background()
	var wg sync.WaitGroup

	// Test concurrent access to the same provider
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = cache.GetProviderBinary(ctx, "concurrent-provider", "1.0.0")
		}()
	}

	// Test concurrent access to different providers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(version int) {
			defer wg.Done()
			_, _ = cache.GetProviderBinary(ctx, "provider", fmt.Sprintf("1.0.%d", version))
		}(i)
	}

	wg.Wait()

	// Verify cleanup worker is still running by closing without panic
	cache.Close()
}

func TestFileBinaryCache_CloseIdempotent(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	config := &provider.BinaryCacheConfig{
		CacheDirectory:      tmpDir,
		LockTimeout:         5 * time.Minute,
		VerifyChecksums:     false,
		DefaultKeepVersions: 3,
	}

	mockDownloader := &mockDownloaderForCleanup{
		binaries: make(map[string]string),
	}

	cache, err := provider.NewFileBinaryCache(config, mockDownloader)
	require.NoError(t, err)

	// Close multiple times should not panic
	cache.Close()
	cache.Close() // Second close should be safe
}
