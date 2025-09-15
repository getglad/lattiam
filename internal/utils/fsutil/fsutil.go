// Package fsutil provides common filesystem utility functions
package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
)

// DirExists checks if a path exists and is a directory.
// This is a convenience wrapper that combines os.Stat and IsDir checks,
// commonly used for health checks and storage validation.
func DirExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// IsWritable checks if a directory is writable by creating a temporary file
func IsWritable(path string) bool {
	// First check if the path exists and get its info
	info, err := os.Stat(path)
	if err != nil {
		return false
	}

	// If it's not a directory, we can't write files to it
	if !info.IsDir() {
		return false
	}

	// Check directory permissions using the mode bits
	// If the directory doesn't have write permission for the owner, it's not writable
	mode := info.Mode()
	if mode.Perm()&0o200 == 0 {
		// No write permission for owner
		return false
	}

	// Try to create a temporary file in the directory as a secondary check
	// This handles cases where permissions might be overridden by filesystem or container settings
	tempFile := filepath.Join(path, ".write_test")

	// Ensure cleanup even if something panics
	defer func() {
		_ = os.Remove(tempFile) // Ignore error - file may not exist
	}()

	file, err := os.Create(tempFile) // #nosec G304 - tempFile path is validated by caller
	if err != nil {
		return false
	}
	_ = file.Close() // Ignore error - file operation completed

	return true
}

// DiskUsage represents disk usage information
type DiskUsage struct {
	TotalBytes  uint64
	FreeBytes   uint64
	UsedBytes   uint64
	UsedPercent float64
}

// GetDiskUsage is implemented in platform-specific files:
// - fsutil_unix.go for Unix/Linux/macOS
// - fsutil_windows.go for Windows

// GetDiskUsageMap returns disk usage as a map
func GetDiskUsageMap(path string) (map[string]interface{}, error) {
	usage, err := GetDiskUsage(path)
	if err != nil {
		return nil, fmt.Errorf("failed to get disk usage: %w", err)
	}

	return map[string]interface{}{
		"total_bytes":  usage.TotalBytes,
		"free_bytes":   usage.FreeBytes,
		"used_bytes":   usage.UsedBytes,
		"used_percent": usage.UsedPercent,
	}, nil
}
