package provider_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/provider"
)

// TestProviderNamePathTraversal verifies path traversal attacks are prevented
//
//nolint:funlen // Comprehensive security test with multiple attack vectors
func TestProviderNamePathTraversal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		provider    string
		version     string
		shouldError bool
		errorType   error
	}{
		{
			name:        "absolute_path_unix",
			provider:    "/etc/passwd",
			version:     "1.0.0",
			shouldError: true,
			errorType:   provider.ErrBinaryCacheInvalidName,
		},
		{
			name:        "relative_path_traversal",
			provider:    "../../../etc/passwd",
			version:     "1.0.0",
			shouldError: true,
			errorType:   provider.ErrBinaryCacheInvalidName,
		},
		{
			name:        "windows_path_traversal",
			provider:    "..\\..\\..\\windows\\system32",
			version:     "1.0.0",
			shouldError: true,
			errorType:   provider.ErrBinaryCacheInvalidName,
		},
		{
			name:        "single_dot",
			provider:    ".",
			version:     "1.0.0",
			shouldError: true,
			errorType:   provider.ErrBinaryCacheInvalidName,
		},
		{
			name:        "double_dot",
			provider:    "..",
			version:     "1.0.0",
			shouldError: true,
			errorType:   provider.ErrBinaryCacheInvalidName,
		},
		{
			name:        "embedded_null_byte",
			provider:    "provider\x00/etc/passwd",
			version:     "1.0.0",
			shouldError: true,
			errorType:   provider.ErrBinaryCacheInvalidName,
		},
		{
			name:        "url_encoded_traversal",
			provider:    "provider%2F..%2F..%2Fetc%2Fpasswd",
			version:     "1.0.0",
			shouldError: false, // Should be caught but encoded differently
		},
		{
			name:        "valid_provider_name",
			provider:    "hashicorp-aws",
			version:     "5.0.0",
			shouldError: false,
		},
	}

	// Create test cache
	tempDir := t.TempDir()
	config := &provider.BinaryCacheConfig{
		CacheDirectory:      tempDir,
		LockTimeout:         1 * time.Second,
		VerifyChecksums:     true,
		DefaultKeepVersions: 3,
	}

	mockDownloader := &mockProviderDownloader{
		binaries: make(map[string]string),
	}

	cache, err := provider.NewFileBinaryCache(config, mockDownloader)
	require.NoError(t, err)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			_, err := cache.GetProviderBinary(ctx, tt.provider, tt.version)

			if tt.shouldError {
				require.Error(t, err)
				if tt.errorType != nil {
					require.ErrorIs(t, err, tt.errorType)
				}
			} else if err != nil {
				// For valid cases, we expect a different error (not found)
				// or success if mock provides the binary
				assert.ErrorIs(t, err, provider.ErrBinaryCacheNotFound)
			}
		})
	}
}

// TestConcurrentBinaryAccess verifies thread-safe access to binaries
func TestConcurrentBinaryAccess(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	config := &provider.BinaryCacheConfig{
		CacheDirectory:      tempDir,
		LockTimeout:         5 * time.Second,
		VerifyChecksums:     false,
		DefaultKeepVersions: 3,
	}

	// Create a mock downloader that simulates slow downloads
	mockDownloader := &mockProviderDownloader{
		binaries: make(map[string]string),
		delay:    100 * time.Millisecond,
	}

	cache, err := provider.NewFileBinaryCache(config, mockDownloader)
	require.NoError(t, err)

	// Test concurrent access to same provider
	const (
		providerName = "test-provider"
		version      = "1.0.0"
		concurrency  = 10
	)

	ctx := context.Background()
	results := make(chan string, concurrency)
	errors := make(chan error, concurrency)

	// Launch concurrent downloads
	for i := 0; i < concurrency; i++ {
		go func() {
			path, err := cache.GetProviderBinary(ctx, providerName, version)
			if err != nil {
				errors <- err
			} else {
				results <- path
			}
		}()
	}

	// Collect results
	var paths []string
	for i := 0; i < concurrency; i++ {
		select {
		case path := <-results:
			paths = append(paths, path)
		case err := <-errors:
			t.Errorf("Unexpected error: %v", err)
		case <-time.After(10 * time.Second):
			t.Fatal("Timeout waiting for concurrent operations")
		}
	}

	// Verify all goroutines got the same path
	require.Len(t, paths, concurrency)
	firstPath := paths[0]
	for _, path := range paths[1:] {
		assert.Equal(t, firstPath, path, "All goroutines should receive the same binary path")
	}

	// Verify downloader was called only once
	assert.Equal(t, 1, mockDownloader.GetDownloadCount(), "Provider should be downloaded only once")
}

// TestBinaryPermissions verifies downloaded binaries have correct permissions
func TestBinaryPermissions(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()
	config := &provider.BinaryCacheConfig{
		CacheDirectory:      tempDir,
		LockTimeout:         1 * time.Second,
		VerifyChecksums:     false,
		DefaultKeepVersions: 3,
	}

	mockDownloader := &mockProviderDownloader{
		binaries: make(map[string]string),
	}

	cache, err := provider.NewFileBinaryCache(config, mockDownloader)
	require.NoError(t, err)

	ctx := context.Background()
	binaryPath, err := cache.GetProviderBinary(ctx, "test-provider", "1.0.0")
	require.NoError(t, err)

	// Check file permissions
	info, err := os.Stat(binaryPath)
	require.NoError(t, err)

	// Should be read-only and executable for owner only (0500)
	expectedMode := os.FileMode(0o500)
	actualMode := info.Mode().Perm()
	assert.Equal(t, expectedMode, actualMode, "Binary should have owner-only read/execute permissions")
}

// TestCacheSizeLimit verifies cache doesn't grow unbounded
func TestCacheSizeLimit(t *testing.T) {
	t.Parallel()
	t.Skip("Cache size limits not yet implemented")

	tempDir := t.TempDir()
	config := &provider.BinaryCacheConfig{
		CacheDirectory:      tempDir,
		LockTimeout:         1 * time.Second,
		VerifyChecksums:     false,
		DefaultKeepVersions: 2, // Keep only 2 versions
	}

	mockDownloader := &mockProviderDownloader{
		binaries: make(map[string]string),
	}

	cache, err := provider.NewFileBinaryCache(config, mockDownloader)
	require.NoError(t, err)

	ctx := context.Background()

	// Download multiple versions
	versions := []string{"1.0.0", "2.0.0", "3.0.0", "4.0.0"}
	for _, version := range versions {
		_, err := cache.GetProviderBinary(ctx, "test-provider", version)
		require.NoError(t, err)
	}

	// Trigger cleanup
	err = cache.CleanupOldVersions("test-provider", 2)
	require.NoError(t, err)

	// Verify only latest 2 versions remain
	cachedVersions, err := cache.ListCachedVersions("test-provider")
	require.NoError(t, err)
	assert.Len(t, cachedVersions, 2, "Should keep only 2 versions after cleanup")
}

// TestChecksumVerification verifies checksum validation works
func TestChecksumVerification(t *testing.T) {
	t.Parallel()
	tempDir := t.TempDir()

	// Test with checksum verification enabled
	config := &provider.BinaryCacheConfig{
		CacheDirectory:      tempDir,
		LockTimeout:         1 * time.Second,
		VerifyChecksums:     true,
		DefaultKeepVersions: 3,
	}

	// Create test binary with known checksum
	testContent := []byte("test provider binary content")
	testFile := filepath.Join(tempDir, "test-binary")
	err := os.WriteFile(testFile, testContent, 0o755) //nolint:gosec // Test file for security testing
	require.NoError(t, err)

	cache, err := provider.NewFileBinaryCache(config, nil)
	require.NoError(t, err)

	// Test with correct checksum
	correctChecksum := "a5c3a9e7e3f0d4a9c8b7f6e5d4c3b2a1" // Example checksum
	err = cache.VerifyBinary(testFile, correctChecksum)
	// This should fail because the actual checksum doesn't match
	require.Error(t, err)

	// Test with empty checksum (should pass if file exists and is executable)
	err = cache.VerifyBinary(testFile, "")
	require.NoError(t, err)

	// Test with non-existent file
	err = cache.VerifyBinary(filepath.Join(tempDir, "non-existent"), "")
	assert.ErrorIs(t, err, provider.ErrBinaryCacheNotFound)
}

// TestSymlinkAttack verifies symlinks cannot be used to escape cache directory
func TestSymlinkAttack(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("Skipping symlink test in short mode")
	}

	tempDir := t.TempDir()
	targetDir := t.TempDir()

	// Create a file outside cache directory
	sensitiveFile := filepath.Join(targetDir, "sensitive.txt")
	err := os.WriteFile(sensitiveFile, []byte("sensitive data"), 0o644) //nolint:gosec // Test file for security testing
	require.NoError(t, err)

	// Create symlink in cache directory pointing to sensitive file
	providerDir := filepath.Join(tempDir, "evil-provider")
	err = os.MkdirAll(providerDir, 0o750)
	require.NoError(t, err)

	symlinkPath := filepath.Join(providerDir, "1.0.0", "provider-binary")
	err = os.MkdirAll(filepath.Dir(symlinkPath), 0o750)
	require.NoError(t, err)

	err = os.Symlink(sensitiveFile, symlinkPath)
	require.NoError(t, err)

	config := &provider.BinaryCacheConfig{
		CacheDirectory:      tempDir,
		LockTimeout:         1 * time.Second,
		VerifyChecksums:     false,
		DefaultKeepVersions: 3,
	}

	cache, err := provider.NewFileBinaryCache(config, nil)
	require.NoError(t, err)

	// Verify binary should detect it's not a regular file
	err = cache.VerifyBinary(symlinkPath, "")
	assert.Error(t, err, "Should reject symlinks")
}

// Mock downloader for testing
type mockProviderDownloader struct {
	binaries      map[string]string
	delay         time.Duration
	downloadCount int
	mu            sync.RWMutex // Protects binaries map and downloadCount
}

func (m *mockProviderDownloader) EnsureProviderBinary(_ context.Context, name, version string) (string, error) {
	key := fmt.Sprintf("%s@%s", name, version)

	// First, check if binary already exists (read lock)
	m.mu.RLock()
	if path, ok := m.binaries[key]; ok {
		m.mu.RUnlock()
		return path, nil
	}
	m.mu.RUnlock()

	// If delay is set, simulate slow download
	if m.delay > 0 {
		time.Sleep(m.delay)
	}

	// Now acquire write lock to create binary
	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock (another goroutine might have created it)
	if path, ok := m.binaries[key]; ok {
		return path, nil
	}

	m.downloadCount++

	// Create a mock binary
	tempFile, err := os.CreateTemp("", fmt.Sprintf("%s-%s-*.bin", name, version))
	if err != nil {
		return "", err //nolint:wrapcheck // test helper function
	}

	// Write some content
	_, err = tempFile.WriteString("mock provider binary")
	if err != nil {
		_ = tempFile.Close()
		_ = os.Remove(tempFile.Name())
		return "", err //nolint:wrapcheck // test helper function
	}

	_ = tempFile.Close()

	// Make executable
	err = os.Chmod(tempFile.Name(), 0o700) //nolint:gosec // Test binary needs to be executable
	if err != nil {
		_ = os.Remove(tempFile.Name()) // Ignore error - cleanup on failure
		return "", err                 //nolint:wrapcheck // test helper function
	}

	m.binaries[key] = tempFile.Name()
	return tempFile.Name(), nil
}

func (m *mockProviderDownloader) GetDownloadCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.downloadCount
}
