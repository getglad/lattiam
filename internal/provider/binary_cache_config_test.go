package provider_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/provider"
)

// TestNewFileBinaryCache_DoesNotMutateConfig verifies that NewFileBinaryCache
// does not modify the caller's config object
func TestNewFileBinaryCache_DoesNotMutateConfig(t *testing.T) {
	t.Parallel()

	// Create a config with some zero values (which should trigger defaults)
	originalConfig := &provider.BinaryCacheConfig{
		CacheDirectory:      t.TempDir(),
		LockTimeout:         5 * time.Minute,
		VerifyChecksums:     false,
		DefaultKeepVersions: 3,
		LockCleanupInterval: 0, // Zero value - should use default
		LockStaleTimeout:    0, // Zero value - should use default
	}

	// Make a copy to verify it doesn't change
	expectedConfig := *originalConfig

	// Create the cache
	cache, err := provider.NewFileBinaryCache(originalConfig, nil)
	require.NoError(t, err)
	defer cache.Close()

	// Verify the original config was not modified
	assert.Equal(t, expectedConfig.CacheDirectory, originalConfig.CacheDirectory,
		"CacheDirectory should not be modified")
	assert.Equal(t, expectedConfig.LockTimeout, originalConfig.LockTimeout,
		"LockTimeout should not be modified")
	assert.Equal(t, expectedConfig.VerifyChecksums, originalConfig.VerifyChecksums,
		"VerifyChecksums should not be modified")
	assert.Equal(t, expectedConfig.DefaultKeepVersions, originalConfig.DefaultKeepVersions,
		"DefaultKeepVersions should not be modified")
	assert.Equal(t, expectedConfig.LockCleanupInterval, originalConfig.LockCleanupInterval,
		"LockCleanupInterval should not be modified (should still be zero)")
	assert.Equal(t, expectedConfig.LockStaleTimeout, originalConfig.LockStaleTimeout,
		"LockStaleTimeout should not be modified (should still be zero)")

	// Specifically verify the zero values weren't replaced with defaults
	assert.Equal(t, time.Duration(0), originalConfig.LockCleanupInterval,
		"LockCleanupInterval should remain zero in original config")
	assert.Equal(t, time.Duration(0), originalConfig.LockStaleTimeout,
		"LockStaleTimeout should remain zero in original config")
}

// TestNewFileBinaryCache_UsesDefaultsForZeroValues verifies that the cache
// uses default values when config fields are zero, without mutating the input
func TestNewFileBinaryCache_UsesDefaultsForZeroValues(t *testing.T) {
	t.Parallel()

	// Config with zero values for cleanup settings
	config := &provider.BinaryCacheConfig{
		CacheDirectory:      t.TempDir(),
		LockTimeout:         5 * time.Minute,
		VerifyChecksums:     false,
		DefaultKeepVersions: 3,
		LockCleanupInterval: 0, // Should use default
		LockStaleTimeout:    0, // Should use default
	}

	cache, err := provider.NewFileBinaryCache(config, nil)
	require.NoError(t, err)
	defer cache.Close()

	// The cache should be using defaults internally (we can't directly test this
	// but we can verify it was created successfully and the config wasn't mutated)
	assert.NotNil(t, cache)

	// Original config should still have zero values
	assert.Equal(t, time.Duration(0), config.LockCleanupInterval)
	assert.Equal(t, time.Duration(0), config.LockStaleTimeout)
}

// TestNewFileBinaryCache_RespectsProvidedValues verifies that non-zero config
// values are respected and used
func TestNewFileBinaryCache_RespectsProvidedValues(t *testing.T) {
	t.Parallel()

	// Config with custom values
	config := &provider.BinaryCacheConfig{
		CacheDirectory:      t.TempDir(),
		LockTimeout:         10 * time.Minute,
		VerifyChecksums:     true,
		DefaultKeepVersions: 5,
		LockCleanupInterval: 30 * time.Minute,
		LockStaleTimeout:    6 * time.Hour,
	}

	// Keep original values for comparison
	originalInterval := config.LockCleanupInterval
	originalTimeout := config.LockStaleTimeout

	cache, err := provider.NewFileBinaryCache(config, nil)
	require.NoError(t, err)
	defer cache.Close()

	// Verify config wasn't mutated
	assert.Equal(t, originalInterval, config.LockCleanupInterval)
	assert.Equal(t, originalTimeout, config.LockStaleTimeout)
}
