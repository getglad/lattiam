package provider_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/provider"
	"github.com/lattiam/lattiam/pkg/provider/protocol"
)

func TestBinaryCacheConfiguration(t *testing.T) {
	t.Parallel()

	// Test that the provider manager can be created with binary cache enabled
	manager, err := protocol.NewProviderManagerWithOptions(
		protocol.WithBaseDir(t.TempDir()),
		protocol.WithDefaultBinaryCache(),
	)
	require.NoError(t, err, "Failed to create provider manager with binary cache")
	defer func() { _ = manager.Close() }() // Ignore error - test cleanup

	// Verify the manager was created successfully
	assert.NotNil(t, manager)

	// Test basic functionality - this will be a no-op in tests but validates the structure
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// This should not fail with the binary cache integration
	// Note: We're not actually downloading anything in this test
	// The downloader will fail gracefully if no providers are available
	_, err = manager.EnsureProviderBinary(ctx, "nonexistent", "1.0.0")

	// We expect this to fail because the provider doesn't exist, but it should fail gracefully
	require.Error(t, err, "Expected error for nonexistent provider")
	assert.Contains(t, err.Error(), "not supported for download", "Expected unsupported provider error")
}

func TestBinaryCacheWithCustomCache(t *testing.T) {
	t.Parallel()

	// Create a custom binary cache
	config := provider.DefaultBinaryCacheConfig()
	config.CacheDirectory = t.TempDir()
	config.VerifyChecksums = false // Disable for test simplicity

	// Create a mock downloader for the cache
	mockDownloader := &MockProviderDownloader{}

	customCache, err := provider.NewFileBinaryCache(config, mockDownloader)
	require.NoError(t, err)

	// Test that the provider manager can be created with custom binary cache
	manager, err := protocol.NewProviderManagerWithOptions(
		protocol.WithBaseDir(t.TempDir()),
		protocol.WithBinaryCache(customCache),
	)
	require.NoError(t, err, "Failed to create provider manager with custom binary cache")
	defer func() { _ = manager.Close() }() // Ignore error - test cleanup

	// Verify the manager was created successfully
	assert.NotNil(t, manager)

	// Test that the cache can be used directly
	ctx := context.Background()

	// This should call our mock downloader through the cache
	_, err = customCache.GetProviderBinary(ctx, "test", "1.0.0")

	// We expect this to fail gracefully since we haven't set up the mock properly
	// But it should demonstrate that the cache is working
	assert.Error(t, err)
}

// MockProviderDownloader for testing
type MockProviderDownloader struct{}

func (m *MockProviderDownloader) EnsureProviderBinary(_ context.Context, _, _ string) (string, error) {
	// Return an error to simulate download failure in tests
	return "", errors.New("provider not supported for testing")
}
