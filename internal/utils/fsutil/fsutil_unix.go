//go:build !windows
// +build !windows

package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// GetDiskUsage returns disk usage information for a given path (Unix implementation)
func GetDiskUsage(path string) (*DiskUsage, error) {
	// Try to resolve path first - macOS has /tmp -> /private/tmp symlink
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		// If we can't resolve symlinks, try the original path
		resolvedPath = path
	}

	// If path doesn't exist, try parent directory
	checkPath := resolvedPath
	if _, err := os.Stat(checkPath); err != nil {
		// Path doesn't exist, try parent directory
		checkPath = filepath.Dir(resolvedPath)
		if _, err := os.Stat(checkPath); err != nil {
			// Parent doesn't exist either, return error
			return nil, fmt.Errorf("path and parent directory do not exist: %s", path)
		}
	}

	var stat syscall.Statfs_t
	if err := syscall.Statfs(checkPath, &stat); err != nil {
		return nil, fmt.Errorf("failed to get filesystem stats: %w", err)
	}

	// Calculate disk usage
	totalBytes := stat.Blocks * uint64(stat.Bsize) // #nosec G115 - safe filesystem stat conversion
	freeBytes := stat.Bavail * uint64(stat.Bsize)  // #nosec G115 - safe filesystem stat conversion
	usedBytes := totalBytes - freeBytes
	usedPercent := float64(usedBytes) / float64(totalBytes) * 100

	return &DiskUsage{
		TotalBytes:  totalBytes,
		FreeBytes:   freeBytes,
		UsedBytes:   usedBytes,
		UsedPercent: usedPercent,
	}, nil
}
