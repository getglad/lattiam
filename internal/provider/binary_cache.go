// Package provider provides binary cache management for Terraform provider executables.
package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lattiam/lattiam/pkg/logging"
)

// Static errors for err113 compliance
var (
	ErrBinaryCacheNotFound    = errors.New("provider binary not found in cache")
	ErrBinaryCacheCorrupted   = errors.New("cached provider binary corrupted")
	ErrBinaryCachePermissions = errors.New("cache directory permissions invalid")
	ErrBinaryCacheChecksum    = errors.New("checksum verification failed")
	ErrBinaryCacheLockTimeout = errors.New("cache lock timeout")
	ErrBinaryCacheInvalidName = errors.New("invalid provider name or version")
)

// BinaryCache interface defines the contract for provider binary caching
type BinaryCache interface {
	// GetProviderBinary returns the path to a cached provider binary, downloading if necessary
	GetProviderBinary(ctx context.Context, name, version string) (binaryPath string, err error)

	// CleanupOldVersions removes old versions of a provider, keeping only the specified number
	CleanupOldVersions(name string, keepVersions int) error

	// VerifyBinary checks if a cached binary is valid and executable
	VerifyBinary(binaryPath string, expectedChecksum string) error

	// GetCacheDirectory returns the cache directory for a provider version
	GetCacheDirectory(name, version string) string

	// ListCachedVersions returns all cached versions for a provider
	ListCachedVersions(name string) ([]string, error)
}

// BinaryCacheConfig holds configuration for the binary cache
type BinaryCacheConfig struct {
	// CacheDirectory is the root directory for cached binaries
	CacheDirectory string

	// LockTimeout is the maximum time to wait for file locks
	LockTimeout time.Duration

	// VerifyChecksums enables checksum verification after download
	VerifyChecksums bool

	// DefaultKeepVersions is the default number of versions to keep during cleanup
	DefaultKeepVersions int

	// LockCleanupInterval is how often to run the lock cleanup process
	LockCleanupInterval time.Duration

	// LockStaleTimeout is how old a lock must be before it's considered stale
	LockStaleTimeout time.Duration
}

// DefaultBinaryCacheConfig returns a sensible default configuration
func DefaultBinaryCacheConfig() *BinaryCacheConfig {
	return &BinaryCacheConfig{
		CacheDirectory:      "/var/lattiam/provider-cache",
		LockTimeout:         5 * time.Minute,
		VerifyChecksums:     true,
		DefaultKeepVersions: 3,
		LockCleanupInterval: 1 * time.Hour,
		LockStaleTimeout:    24 * time.Hour,
	}
}

// FileBinaryCache implements BinaryCache using filesystem storage
type FileBinaryCache struct {
	config          *BinaryCacheConfig
	locks           map[string]*sync.Mutex
	locksMutex      sync.RWMutex
	downloader      BinaryDownloader
	logger          *logging.Logger
	lastAccess      map[string]time.Time // Tracks last access time for locks
	cleanupInterval time.Duration        // How often to run the cleanup
	stopCleanup     chan struct{}        // Channel to stop the cleanup worker
	closeOnce       sync.Once            // Ensures Close() is idempotent
}

// BinaryDownloader interface for downloading providers (matches existing interface)
type BinaryDownloader interface {
	EnsureProviderBinary(ctx context.Context, name, version string) (string, error)
}

// NewFileBinaryCache creates a new filesystem-based binary cache
func NewFileBinaryCache(config *BinaryCacheConfig, downloader BinaryDownloader) (*FileBinaryCache, error) {
	// Start with defaults and overlay user config to avoid mutation
	defaultConfig := DefaultBinaryCacheConfig()
	if config != nil {
		// Create a copy to avoid mutating the caller's config
		configCopy := *config
		config = &configCopy

		// Apply user overrides, keeping defaults for zero values
		if config.CacheDirectory != "" {
			defaultConfig.CacheDirectory = config.CacheDirectory
		}
		if config.LockTimeout > 0 {
			defaultConfig.LockTimeout = config.LockTimeout
		}
		if config.DefaultKeepVersions > 0 {
			defaultConfig.DefaultKeepVersions = config.DefaultKeepVersions
		}
		if config.LockCleanupInterval > 0 {
			defaultConfig.LockCleanupInterval = config.LockCleanupInterval
		}
		if config.LockStaleTimeout > 0 {
			defaultConfig.LockStaleTimeout = config.LockStaleTimeout
		}
		// VerifyChecksums is boolean, use explicit check
		defaultConfig.VerifyChecksums = config.VerifyChecksums
	}
	config = defaultConfig

	// Validate configuration
	if err := validateCacheConfig(config); err != nil {
		return nil, fmt.Errorf("invalid cache configuration: %w", err)
	}

	// Ensure cache directory exists with proper permissions
	if err := ensureCacheDirectory(config.CacheDirectory); err != nil {
		return nil, fmt.Errorf("failed to create cache directory: %w", err)
	}

	cache := &FileBinaryCache{
		config:          config,
		locks:           make(map[string]*sync.Mutex),
		downloader:      downloader,
		logger:          logging.NewLogger("binary-cache"),
		lastAccess:      make(map[string]time.Time),
		cleanupInterval: config.LockCleanupInterval,
		stopCleanup:     make(chan struct{}),
	}

	// Start the background cleanup worker
	go cache.startCleanupWorker()

	return cache, nil
}

// Close stops the background cleanup worker
func (c *FileBinaryCache) Close() {
	c.closeOnce.Do(func() {
		c.logger.Debug("Stopping binary cache cleanup worker")
		// Close the channel to signal all readers to stop
		close(c.stopCleanup)
	})
}

// GetProviderBinary implements BinaryCache.GetProviderBinary
func (c *FileBinaryCache) GetProviderBinary(ctx context.Context, name, version string) (string, error) {
	if err := validateProviderNameVersion(name, version); err != nil {
		return "", err
	}

	// Get or create a lock for this provider/version combination
	lockKey := fmt.Sprintf("%s@%s", name, version)
	lock := c.getLock(lockKey)

	// Acquire exclusive lock for this provider/version
	lock.Lock()
	defer lock.Unlock()

	// Check if binary already exists in cache
	binaryPath := c.getBinaryPath(name, version)
	if c.isBinaryCached(binaryPath) {
		c.logger.Debug("Found cached provider binary: %s", binaryPath)
		return binaryPath, nil
	}

	c.logger.Info("Provider binary not cached, downloading: %s@%s", name, version)

	// Download to temporary location first
	tempBinaryPath, err := c.downloadToCache(ctx, name, version)
	if err != nil {
		return "", fmt.Errorf("failed to download provider binary: %w", err)
	}

	// Move to final location and make read-only
	if err := c.finalizeBinary(tempBinaryPath, binaryPath); err != nil {
		_ = os.Remove(tempBinaryPath) // Clean up temp file
		return "", fmt.Errorf("failed to finalize binary: %w", err)
	}

	c.logger.Debug("Successfully cached provider binary: %s", binaryPath)
	return binaryPath, nil
}

// CleanupOldVersions implements BinaryCache.CleanupOldVersions
func (c *FileBinaryCache) CleanupOldVersions(name string, keepVersions int) error {
	if err := validateProviderName(name); err != nil {
		return err
	}

	if keepVersions <= 0 {
		keepVersions = c.config.DefaultKeepVersions
	}

	providerDir := filepath.Join(c.config.CacheDirectory, name)
	if _, err := os.Stat(providerDir); os.IsNotExist(err) {
		return nil // No versions to clean up
	}

	versions, err := c.ListCachedVersions(name)
	if err != nil {
		return fmt.Errorf("failed to list cached versions: %w", err)
	}

	if len(versions) <= keepVersions {
		return nil // Nothing to clean up
	}

	// Sort versions (newest first) and remove oldest
	sort.Slice(versions, func(i, j int) bool {
		return versions[i] > versions[j] // Lexicographic sort, newest first
	})

	versionsToRemove := versions[keepVersions:]
	c.logger.Info("Cleaning up %d old versions for provider %s", len(versionsToRemove), name)

	for _, version := range versionsToRemove {
		versionDir := filepath.Join(providerDir, version)
		if err := os.RemoveAll(versionDir); err != nil {
			c.logger.Error("Failed to remove version directory %s: %v", versionDir, err)
			continue
		}
		c.logger.Info("Removed cached version: %s@%s", name, version)
	}

	return nil
}

// VerifyBinary implements BinaryCache.VerifyBinary
func (c *FileBinaryCache) VerifyBinary(binaryPath string, expectedChecksum string) error {
	// Check if file exists and is executable
	info, err := os.Stat(binaryPath)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrBinaryCacheNotFound, binaryPath)
	}

	if info.IsDir() {
		return fmt.Errorf("%w: %s is a directory", ErrBinaryCacheCorrupted, binaryPath)
	}

	// Check if file is executable
	if info.Mode()&0o111 == 0 {
		return fmt.Errorf("%w: %s is not executable", ErrBinaryCacheCorrupted, binaryPath)
	}

	// Verify checksum if provided and verification is enabled
	if c.config.VerifyChecksums && expectedChecksum != "" {
		actualChecksum, err := calculateSHA256(binaryPath)
		if err != nil {
			return fmt.Errorf("failed to calculate checksum: %w", err)
		}

		if actualChecksum != expectedChecksum {
			return fmt.Errorf("%w: expected %s, got %s", ErrBinaryCacheChecksum, expectedChecksum, actualChecksum)
		}
	}

	return nil
}

// GetCacheDirectory implements BinaryCache.GetCacheDirectory
func (c *FileBinaryCache) GetCacheDirectory(name, version string) string {
	return filepath.Join(c.config.CacheDirectory, name, version)
}

// ListCachedVersions implements BinaryCache.ListCachedVersions
func (c *FileBinaryCache) ListCachedVersions(name string) ([]string, error) {
	if err := validateProviderName(name); err != nil {
		return nil, err
	}

	providerDir := filepath.Join(c.config.CacheDirectory, name)
	entries, err := os.ReadDir(providerDir)
	if os.IsNotExist(err) {
		return []string{}, nil // No versions cached
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read provider directory: %w", err)
	}

	var versions []string
	for _, entry := range entries {
		if entry.IsDir() {
			versions = append(versions, entry.Name())
		}
	}

	return versions, nil
}

// Private helper methods

// getLock returns a mutex for the given key, creating one if it doesn't exist
func (c *FileBinaryCache) getLock(key string) *sync.Mutex {
	c.locksMutex.Lock()
	defer c.locksMutex.Unlock()

	if lock, exists := c.locks[key]; exists {
		c.lastAccess[key] = time.Now()
		return lock
	}

	lock := &sync.Mutex{}
	c.locks[key] = lock
	c.lastAccess[key] = time.Now()
	c.logger.Debug("Created new lock for key: %s", key)
	return lock
}

// getBinaryPath returns the expected path for a cached binary
func (c *FileBinaryCache) getBinaryPath(name, version string) string {
	cacheDir := c.GetCacheDirectory(name, version)
	binaryName := fmt.Sprintf("terraform-provider-%s", name)
	return filepath.Join(cacheDir, binaryName)
}

// isBinaryCached checks if a binary is already cached and valid
func (c *FileBinaryCache) isBinaryCached(binaryPath string) bool {
	info, err := os.Stat(binaryPath)
	if err != nil {
		return false
	}

	// Check if it's a file and executable
	if info.IsDir() || info.Mode()&0o111 == 0 {
		return false
	}

	return true
}

// downloadToCache downloads a provider binary to the cache directory
func (c *FileBinaryCache) downloadToCache(ctx context.Context, name, version string) (string, error) {
	// Use existing downloader to get the binary
	tempBinaryPath, err := c.downloader.EnsureProviderBinary(ctx, name, version)
	if err != nil {
		return "", fmt.Errorf("failed to download provider: %w", err)
	}

	// Verify the downloaded binary is valid
	if err := c.VerifyBinary(tempBinaryPath, ""); err != nil {
		return "", fmt.Errorf("downloaded binary failed verification: %w", err)
	}

	return tempBinaryPath, nil
}

// finalizeBinary moves a binary to its final cache location and makes it read-only
func (c *FileBinaryCache) finalizeBinary(tempPath, finalPath string) error {
	// Ensure destination directory exists
	finalDir := filepath.Dir(finalPath)
	if err := os.MkdirAll(finalDir, 0o700); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	// Move or copy the binary to final location
	if err := os.Rename(tempPath, finalPath); err != nil {
		// If rename fails (e.g., cross-device), copy instead
		if copyErr := copyFile(tempPath, finalPath); copyErr != nil {
			return fmt.Errorf("failed to copy binary to cache: %w", copyErr)
		}
		_ = os.Remove(tempPath) // Clean up temp file after successful copy
	}

	// Make binary read-only but executable
	if err := os.Chmod(finalPath, 0o500); err != nil { //nolint:gosec // Provider binaries must be executable
		return fmt.Errorf("failed to set binary permissions: %w", err)
	}

	return nil
}

// Validation and utility functions

// validateCacheConfig validates the cache configuration
func validateCacheConfig(config *BinaryCacheConfig) error {
	if config.CacheDirectory == "" {
		return errors.New("cache directory cannot be empty")
	}

	if config.LockTimeout <= 0 {
		return errors.New("lock timeout must be positive")
	}

	if config.DefaultKeepVersions < 0 {
		return errors.New("default keep versions cannot be negative")
	}

	return nil
}

// ensureCacheDirectory creates the cache directory with proper permissions
func ensureCacheDirectory(cacheDir string) error {
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Verify directory permissions
	info, err := os.Stat(cacheDir)
	if err != nil {
		return fmt.Errorf("failed to stat directory: %w", err)
	}

	if !info.IsDir() {
		return fmt.Errorf("%w: %s is not a directory", ErrBinaryCachePermissions, cacheDir)
	}

	// Check if directory is writable
	testFile := filepath.Join(cacheDir, ".write-test")
	if err := os.WriteFile(testFile, []byte("test"), 0o600); err != nil {
		return fmt.Errorf("%w: directory not writable", ErrBinaryCachePermissions)
	}
	_ = os.Remove(testFile)

	return nil
}

// validateProviderNameVersion validates provider name and version
func validateProviderNameVersion(name, version string) error {
	if err := validateProviderName(name); err != nil {
		return err
	}

	if version == "" {
		return fmt.Errorf("%w: version cannot be empty", ErrBinaryCacheInvalidName)
	}

	// Basic version format validation (allow semantic versions and dates)
	if strings.ContainsAny(version, "/\\:*?\"<>|") {
		return fmt.Errorf("%w: version contains invalid characters", ErrBinaryCacheInvalidName)
	}

	return nil
}

// validateProviderName validates provider name
func validateProviderName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: provider name cannot be empty", ErrBinaryCacheInvalidName)
	}

	// Basic name validation - prevent path traversal
	if strings.ContainsAny(name, "/\\:*?\"<>|") || name == "." || name == ".." {
		return fmt.Errorf("%w: provider name contains invalid characters", ErrBinaryCacheInvalidName)
	}

	return nil
}

// calculateSHA256 calculates the SHA256 checksum of a file
func calculateSHA256(filePath string) (string, error) {
	file, err := os.Open(filePath) // #nosec G304 - filePath is validated by caller
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer func() { _ = file.Close() }()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, file); err != nil {
		return "", fmt.Errorf("failed to read file for hashing: %w", err)
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	srcFile, err := os.Open(src) // #nosec G304 - src path is validated by caller
	if err != nil {
		return fmt.Errorf("failed to open source file: %w", err)
	}
	defer func() { _ = srcFile.Close() }()

	dstFile, err := os.Create(dst) // #nosec G304 - dst path is validated by caller
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer func() { _ = dstFile.Close() }()

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		return fmt.Errorf("failed to copy file: %w", err)
	}

	// Copy permissions from source
	srcInfo, err := srcFile.Stat()
	if err != nil {
		return fmt.Errorf("failed to get source file info: %w", err)
	}

	if err := dstFile.Chmod(srcInfo.Mode()); err != nil {
		return fmt.Errorf("failed to set destination file permissions: %w", err)
	}

	return nil
}

// startCleanupWorker runs a periodic task to clean up stale locks
func (c *FileBinaryCache) startCleanupWorker() {
	ticker := time.NewTicker(c.cleanupInterval)
	defer ticker.Stop()

	c.logger.Debug("Starting stale lock cleanup worker with interval: %s", c.cleanupInterval)

	for {
		select {
		case <-ticker.C:
			c.cleanupStaleLocks()
		case <-c.stopCleanup:
			c.logger.Debug("Cleanup worker received stop signal")
			return
		}
	}
}

// cleanupStaleLocks removes locks that haven't been accessed recently
func (c *FileBinaryCache) cleanupStaleLocks() {
	c.locksMutex.Lock()
	defer c.locksMutex.Unlock()

	// Use the configured stale timeout
	staleThreshold := c.config.LockStaleTimeout
	now := time.Now()
	cleanedCount := 0

	for key, lastAccessTime := range c.lastAccess {
		if now.Sub(lastAccessTime) > staleThreshold {
			delete(c.locks, key)
			delete(c.lastAccess, key)
			cleanedCount++
		}
	}

	if cleanedCount > 0 {
		c.logger.Info("Cleaned up %d stale lock(s). Total locks remaining: %d", cleanedCount, len(c.locks))
	}
}
