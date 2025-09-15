package provider

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockDownloaderForLockTest is a simple mock for testing
type mockDownloaderForLockTest struct {
	mu       sync.Mutex
	binaries map[string]string
}

func (m *mockDownloaderForLockTest) EnsureProviderBinary(_ context.Context, name, version string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := name + "_" + version
	if path, exists := m.binaries[key]; exists {
		return path, nil
	}

	// Return an error since we're not creating real files
	return "", fmt.Errorf("mock downloader: provider not found")
}

// TestFileBinaryCache_GetLockUpdatesLastAccess tests that getLock updates the lastAccess map
func TestFileBinaryCache_GetLockUpdatesLastAccess(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	config := &BinaryCacheConfig{
		CacheDirectory:      tmpDir,
		LockTimeout:         5 * time.Minute,
		VerifyChecksums:     false,
		DefaultKeepVersions: 3,
		LockCleanupInterval: 1 * time.Hour,
		LockStaleTimeout:    24 * time.Hour,
	}

	mockDownloader := &mockDownloaderForLockTest{
		binaries: make(map[string]string),
	}

	cache, err := NewFileBinaryCache(config, mockDownloader)
	require.NoError(t, err)
	defer cache.Close()

	// Get a lock for a key
	key := "test-provider@1.0.0"
	lock1 := cache.getLock(key)
	assert.NotNil(t, lock1)

	// Check that lastAccess was updated
	cache.locksMutex.RLock()
	accessTime1, exists := cache.lastAccess[key]
	cache.locksMutex.RUnlock()
	assert.True(t, exists, "lastAccess should contain the key")
	assert.WithinDuration(t, time.Now(), accessTime1, 100*time.Millisecond)

	// Wait a bit and get the lock again
	time.Sleep(50 * time.Millisecond)

	lock2 := cache.getLock(key)
	assert.Equal(t, lock1, lock2, "should return the same lock instance")

	// Check that lastAccess was updated again
	cache.locksMutex.RLock()
	accessTime2, exists := cache.lastAccess[key]
	cache.locksMutex.RUnlock()
	assert.True(t, exists, "lastAccess should still contain the key")
	assert.True(t, accessTime2.After(accessTime1), "access time should be updated")
}

// TestFileBinaryCache_CleanupStaleLocks tests the cleanup logic
func TestFileBinaryCache_CleanupStaleLocks(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	config := &BinaryCacheConfig{
		CacheDirectory:      tmpDir,
		LockTimeout:         5 * time.Minute,
		VerifyChecksums:     false,
		DefaultKeepVersions: 3,
		LockCleanupInterval: 50 * time.Millisecond,  // Fast cleanup for testing
		LockStaleTimeout:    200 * time.Millisecond, // Increased to avoid timing issues
	}

	mockDownloader := &mockDownloaderForLockTest{
		binaries: make(map[string]string),
	}

	cache, err := NewFileBinaryCache(config, mockDownloader)
	require.NoError(t, err)
	defer cache.Close()

	// Create some locks
	oldKey := "old-provider@1.0.0"
	newKey := "new-provider@2.0.0"

	// Create both locks
	_ = cache.getLock(oldKey)
	_ = cache.getLock(newKey)

	// Manually set the old lock's access time to be very old
	cache.locksMutex.Lock()
	cache.lastAccess[oldKey] = time.Now().Add(-500 * time.Millisecond) // Much older than stale timeout
	// Keep the new lock's time fresh (just created)
	freshTime := time.Now()
	cache.lastAccess[newKey] = freshTime
	cache.locksMutex.Unlock()

	// Verify both locks exist
	cache.locksMutex.RLock()
	assert.Len(t, cache.locks, 2, "should have 2 locks")
	assert.Len(t, cache.lastAccess, 2, "should have 2 access times")
	cache.locksMutex.RUnlock()

	// Wait for cleanup to run at least once (cleanup interval is 50ms)
	time.Sleep(100 * time.Millisecond)

	// Check that old lock was removed and new lock remains
	cache.locksMutex.RLock()
	_, oldExists := cache.locks[oldKey]
	_, newExists := cache.locks[newKey]
	_, oldAccessExists := cache.lastAccess[oldKey]
	_, newAccessExists := cache.lastAccess[newKey]
	cache.locksMutex.RUnlock()

	assert.False(t, oldExists, "old lock should be removed")
	assert.True(t, newExists, "new lock should remain (was fresh)")
	assert.False(t, oldAccessExists, "old access time should be removed")
	assert.True(t, newAccessExists, "new access time should remain")
}

// TestFileBinaryCache_CleanupKeepsFreshLocks tests that cleanup doesn't remove fresh locks
func TestFileBinaryCache_CleanupKeepsFreshLocks(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	config := &BinaryCacheConfig{
		CacheDirectory:      tmpDir,
		LockTimeout:         5 * time.Minute,
		VerifyChecksums:     false,
		DefaultKeepVersions: 3,
		LockCleanupInterval: 50 * time.Millisecond,  // Fast cleanup for testing
		LockStaleTimeout:    500 * time.Millisecond, // Longer timeout
	}

	mockDownloader := &mockDownloaderForLockTest{
		binaries: make(map[string]string),
	}

	cache, err := NewFileBinaryCache(config, mockDownloader)
	require.NoError(t, err)
	defer cache.Close()

	// Create multiple fresh locks
	keys := []string{
		"provider1@1.0.0",
		"provider2@2.0.0",
		"provider3@3.0.0",
	}

	for _, key := range keys {
		_ = cache.getLock(key)
	}

	// Verify all locks exist
	cache.locksMutex.RLock()
	initialLockCount := len(cache.locks)
	initialAccessCount := len(cache.lastAccess)
	cache.locksMutex.RUnlock()

	assert.Equal(t, 3, initialLockCount, "should have 3 locks")
	assert.Equal(t, 3, initialAccessCount, "should have 3 access times")

	// Wait for cleanup to run multiple times
	time.Sleep(200 * time.Millisecond)

	// All locks should still exist (they're all fresh)
	cache.locksMutex.RLock()
	finalLockCount := len(cache.locks)
	finalAccessCount := len(cache.lastAccess)
	cache.locksMutex.RUnlock()

	assert.Equal(t, 3, finalLockCount, "all locks should remain")
	assert.Equal(t, 3, finalAccessCount, "all access times should remain")
}

// TestFileBinaryCache_ManualCleanup tests calling cleanupStaleLocks directly
func TestFileBinaryCache_ManualCleanup(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	config := &BinaryCacheConfig{
		CacheDirectory:      tmpDir,
		LockTimeout:         5 * time.Minute,
		VerifyChecksums:     false,
		DefaultKeepVersions: 3,
		LockCleanupInterval: 1 * time.Hour, // Long interval so auto-cleanup won't run
		LockStaleTimeout:    100 * time.Millisecond,
	}

	mockDownloader := &mockDownloaderForLockTest{
		binaries: make(map[string]string),
	}

	cache, err := NewFileBinaryCache(config, mockDownloader)
	require.NoError(t, err)
	defer cache.Close()

	// Create locks with different ages
	veryOldKey := "very-old@1.0.0"
	oldKey := "old@1.0.0"
	freshKey := "fresh@1.0.0"

	_ = cache.getLock(veryOldKey)
	_ = cache.getLock(oldKey)
	_ = cache.getLock(freshKey)

	// Set different access times
	cache.locksMutex.Lock()
	cache.lastAccess[veryOldKey] = time.Now().Add(-500 * time.Millisecond)
	cache.lastAccess[oldKey] = time.Now().Add(-150 * time.Millisecond)
	// freshKey keeps its current time
	cache.locksMutex.Unlock()

	// Manually trigger cleanup
	cache.cleanupStaleLocks()

	// Check results
	cache.locksMutex.RLock()
	_, veryOldExists := cache.locks[veryOldKey]
	_, oldExists := cache.locks[oldKey]
	_, freshExists := cache.locks[freshKey]
	cache.locksMutex.RUnlock()

	assert.False(t, veryOldExists, "very old lock should be removed")
	assert.False(t, oldExists, "old lock should be removed")
	assert.True(t, freshExists, "fresh lock should remain")
}

// TestFileBinaryCache_NoGoroutineLeaks tests that Close() properly stops the cleanup worker
func TestFileBinaryCache_NoGoroutineLeaks(t *testing.T) { //nolint:paralleltest // Goroutine leak test requires sequential execution
	// Note: This test doesn't use t.Parallel() to avoid interference from other tests

	// Get initial goroutine count
	initialGoroutines := countGoroutines()

	tmpDir := t.TempDir()
	config := &BinaryCacheConfig{
		CacheDirectory:      tmpDir,
		LockTimeout:         5 * time.Minute,
		VerifyChecksums:     false,
		DefaultKeepVersions: 3,
		LockCleanupInterval: 50 * time.Millisecond,
		LockStaleTimeout:    24 * time.Hour,
	}

	mockDownloader := &mockDownloaderForLockTest{
		binaries: make(map[string]string),
	}

	// Create and close multiple caches
	for i := 0; i < 5; i++ {
		cache, err := NewFileBinaryCache(config, mockDownloader)
		require.NoError(t, err)

		// Use the cache a bit
		_ = cache.getLock(fmt.Sprintf("provider@%d.0.0", i))

		// Close should stop the goroutine
		cache.Close()
	}

	// Give goroutines time to exit
	time.Sleep(300 * time.Millisecond)

	// Check that we don't have leaked goroutines
	finalGoroutines := countGoroutines()

	// Allow for some variance (other tests might be running in parallel)
	// We created 5 caches and closed them, so we shouldn't have 5+ extra goroutines
	leakedGoroutines := finalGoroutines - initialGoroutines
	assert.Less(t, leakedGoroutines, 5,
		"should not leak goroutines (initial: %d, final: %d, leaked: %d)",
		initialGoroutines, finalGoroutines, leakedGoroutines)
}

// countGoroutines returns the current number of goroutines
func countGoroutines() int {
	// This is a simple implementation
	return runtime.NumGoroutine()
}
