//go:build windows
// +build windows

package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

// GetDiskUsage returns disk usage information for a given path (Windows implementation)
func GetDiskUsage(path string) (*DiskUsage, error) {
	// Try to resolve path first
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

	// Get Windows disk space using GetDiskFreeSpaceEx
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getDiskFreeSpaceEx := kernel32.NewProc("GetDiskFreeSpaceExW")

	var freeBytesAvailable int64
	var totalNumberOfBytes int64
	var totalNumberOfFreeBytes int64

	pathPtr, err := syscall.UTF16PtrFromString(checkPath)
	if err != nil {
		return nil, fmt.Errorf("failed to convert path to UTF16: %w", err)
	}

	ret, _, err := getDiskFreeSpaceEx.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(&freeBytesAvailable)),
		uintptr(unsafe.Pointer(&totalNumberOfBytes)),
		uintptr(unsafe.Pointer(&totalNumberOfFreeBytes)),
	)

	if ret == 0 {
		return nil, fmt.Errorf("GetDiskFreeSpaceEx failed: %w", err)
	}

	// Calculate disk usage
	totalBytes := uint64(totalNumberOfBytes)
	freeBytes := uint64(freeBytesAvailable)
	usedBytes := totalBytes - freeBytes
	usedPercent := float64(usedBytes) / float64(totalBytes) * 100

	return &DiskUsage{
		TotalBytes:  totalBytes,
		FreeBytes:   freeBytes,
		UsedBytes:   usedBytes,
		UsedPercent: usedPercent,
	}, nil
}
