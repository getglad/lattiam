//go:build !windows
// +build !windows

package monitoring

import (
	"os"
	"path/filepath"
	"syscall"
)

// getDiskUsage returns disk usage for a path (Unix implementation)
func getDiskUsage(path string) *DiskUsage {
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
			// Parent doesn't exist either, can't get disk usage
			return nil
		}
	}

	var stat syscall.Statfs_t
	if err := syscall.Statfs(checkPath, &stat); err != nil {
		return nil
	}

	// Calculate disk usage
	totalBytes := stat.Blocks * uint64(stat.Bsize) // #nosec G115 - safe filesystem stat conversion
	freeBytes := stat.Bavail * uint64(stat.Bsize)  // #nosec G115 - safe filesystem stat conversion
	usedBytes := totalBytes - freeBytes
	percentUsed := float64(usedBytes) / float64(totalBytes) * 100

	return &DiskUsage{
		TotalBytes:  totalBytes,
		FreeBytes:   freeBytes,
		UsedBytes:   usedBytes,
		PercentUsed: percentUsed,
	}
}
