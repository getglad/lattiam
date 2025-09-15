package provider

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFileBinaryCache_ActiveLocksKeptAlive tests that locks accessed periodically
// are kept alive and not removed by the cleanup goroutine
func TestFileBinaryCache_ActiveLocksKeptAlive(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	config := &BinaryCacheConfig{
		CacheDirectory:      tmpDir,
		LockTimeout:         5 * time.Minute,
		VerifyChecksums:     false,
		DefaultKeepVersions: 3,
		LockCleanupInterval: 50 * time.Millisecond,  // Fast cleanup
		LockStaleTimeout:    150 * time.Millisecond, // Short stale timeout
	}

	mockDownloader := &mockDownloaderForLockTest{
		binaries: make(map[string]string),
	}

	cache, err := NewFileBinaryCache(config, mockDownloader)
	require.NoError(t, err)
	defer cache.Close()

	// Keys to test
	activeKey := "active-provider@1.0.0"
	inactiveKey := "inactive-provider@1.0.0"

	// Create both locks
	_ = cache.getLock(activeKey)
	_ = cache.getLock(inactiveKey)

	// Start a goroutine that periodically accesses the active lock
	stopRefreshing := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(40 * time.Millisecond) // Refresh more frequently than stale timeout
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				// Keep accessing the active lock to refresh its timestamp
				_ = cache.getLock(activeKey)
			case <-stopRefreshing:
				return
			}
		}
	}()

	// Let the system run for several cleanup cycles
	// Cleanup runs every 50ms, stale timeout is 150ms
	// We'll wait for 400ms (8 cleanup cycles)
	time.Sleep(400 * time.Millisecond)

	// Stop refreshing the active lock
	close(stopRefreshing)
	wg.Wait()

	// Check the results
	cache.locksMutex.RLock()
	_, activeExists := cache.locks[activeKey]
	_, inactiveExists := cache.locks[inactiveKey]
	_, activeAccessExists := cache.lastAccess[activeKey]
	_, inactiveAccessExists := cache.lastAccess[inactiveKey]
	cache.locksMutex.RUnlock()

	// Active lock should still exist (it was being refreshed)
	assert.True(t, activeExists, "active lock should remain (was being refreshed)")
	assert.True(t, activeAccessExists, "active access time should remain")

	// Inactive lock should be removed (wasn't accessed for 400ms)
	assert.False(t, inactiveExists, "inactive lock should be removed (wasn't refreshed)")
	assert.False(t, inactiveAccessExists, "inactive access time should be removed")
}

// TestFileBinaryCache_RealWorldSimulation simulates a more realistic scenario
// with multiple providers being accessed at different rates
//
//nolint:funlen // Comprehensive real-world simulation test with concurrent operations
func TestFileBinaryCache_RealWorldSimulation(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	config := &BinaryCacheConfig{
		CacheDirectory:      tmpDir,
		LockTimeout:         5 * time.Minute,
		VerifyChecksums:     false,
		DefaultKeepVersions: 3,
		LockCleanupInterval: 100 * time.Millisecond, // Moderate cleanup interval
		LockStaleTimeout:    300 * time.Millisecond, // Moderate stale timeout
	}

	mockDownloader := &mockDownloaderForLockTest{
		binaries: make(map[string]string),
	}

	cache, err := NewFileBinaryCache(config, mockDownloader)
	require.NoError(t, err)
	defer cache.Close()

	// Simulate different access patterns
	frequentKey := "frequent@1.0.0"     // Accessed frequently
	occasionalKey := "occasional@1.0.0" // Accessed occasionally
	rareKey := "rare@1.0.0"             // Accessed once and forgotten

	// Create all locks
	_ = cache.getLock(frequentKey)
	_ = cache.getLock(occasionalKey)
	_ = cache.getLock(rareKey)

	// Start goroutines with different access patterns
	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Frequent access (every 50ms)
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_ = cache.getLock(frequentKey)
			case <-stop:
				return
			}
		}
	}()

	// Occasional access (every 200ms)
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_ = cache.getLock(occasionalKey)
			case <-stop:
				return
			}
		}
	}()

	// Run for 600ms (6 cleanup cycles, 2x stale timeout)
	time.Sleep(600 * time.Millisecond)
	close(stop)
	wg.Wait()

	// Check final state
	cache.locksMutex.RLock()
	_, frequentExists := cache.locks[frequentKey]
	_, occasionalExists := cache.locks[occasionalKey]
	_, rareExists := cache.locks[rareKey]
	cache.locksMutex.RUnlock()

	// Expectations
	assert.True(t, frequentExists, "frequently accessed lock should remain")
	assert.True(t, occasionalExists, "occasionally accessed lock should remain (200ms < 300ms timeout)")
	assert.False(t, rareExists, "rarely accessed lock should be removed (600ms > 300ms timeout)")
}

// TestFileBinaryCache_GetProviderBinaryKeepsLockAlive tests that normal usage
// through GetProviderBinary keeps locks alive
//
//nolint:funlen // Comprehensive test for lock lifecycle management
func TestFileBinaryCache_GetProviderBinaryKeepsLockAlive(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	config := &BinaryCacheConfig{
		CacheDirectory:      tmpDir,
		LockTimeout:         5 * time.Minute,
		VerifyChecksums:     false,
		DefaultKeepVersions: 3,
		LockCleanupInterval: 50 * time.Millisecond,
		LockStaleTimeout:    150 * time.Millisecond,
	}

	mockDownloader := &mockDownloaderForLockTest{
		binaries: make(map[string]string),
	}

	cache, err := NewFileBinaryCache(config, mockDownloader)
	require.NoError(t, err)
	defer cache.Close()

	ctx := context.Background()

	// Provider that will be accessed repeatedly
	activeProvider := "active"
	activeVersion := "1.0.0"

	// Provider that won't be accessed
	inactiveProvider := "inactive"
	inactiveVersion := "1.0.0"

	// Initial access to both (will fail but creates locks)
	_, _ = cache.GetProviderBinary(ctx, activeProvider, activeVersion)
	_, _ = cache.GetProviderBinary(ctx, inactiveProvider, inactiveVersion)

	// Keep accessing the active provider
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(40 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				_, _ = cache.GetProviderBinary(ctx, activeProvider, activeVersion)
			case <-stop:
				return
			}
		}
	}()

	// Wait for several cleanup cycles
	time.Sleep(400 * time.Millisecond)
	close(stop)
	wg.Wait()

	// Check lock states
	activeLockKey := activeProvider + "@" + activeVersion
	inactiveLockKey := inactiveProvider + "@" + inactiveVersion

	cache.locksMutex.RLock()
	_, activeExists := cache.locks[activeLockKey]
	_, inactiveExists := cache.locks[inactiveLockKey]
	cache.locksMutex.RUnlock()

	assert.True(t, activeExists, "active provider lock should remain")
	assert.False(t, inactiveExists, "inactive provider lock should be removed")
}
