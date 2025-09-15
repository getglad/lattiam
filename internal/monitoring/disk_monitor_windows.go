//go:build windows
// +build windows

package monitoring

import (
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

// getDiskUsage returns disk usage for a path (Windows implementation)
func getDiskUsage(path string) *DiskUsage {
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
			// Parent doesn't exist either, can't get disk usage
			return nil
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
		return nil
	}

	ret, _, _ := getDiskFreeSpaceEx.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		uintptr(unsafe.Pointer(&freeBytesAvailable)),
		uintptr(unsafe.Pointer(&totalNumberOfBytes)),
		uintptr(unsafe.Pointer(&totalNumberOfFreeBytes)),
	)

	if ret == 0 {
		return nil
	}

	// Calculate disk usage
	totalBytes := uint64(totalNumberOfBytes)
	freeBytes := uint64(freeBytesAvailable)
	usedBytes := totalBytes - freeBytes
	percentUsed := float64(usedBytes) / float64(totalBytes) * 100

	return &DiskUsage{
		TotalBytes:  totalBytes,
		FreeBytes:   freeBytes,
		UsedBytes:   usedBytes,
		PercentUsed: percentUsed,
	}
}
