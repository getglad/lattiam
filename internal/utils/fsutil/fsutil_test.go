package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDirExists(t *testing.T) {
	t.Parallel()
	// Test with existing directory
	tmpDir := t.TempDir()
	if !DirExists(tmpDir) {
		t.Errorf("DirExists(%s) = false, want true", tmpDir)
	}

	// Test with non-existent directory
	nonExistent := filepath.Join(tmpDir, "does-not-exist")
	if DirExists(nonExistent) {
		t.Errorf("DirExists(%s) = true, want false", nonExistent)
	}

	// Test with file (not directory)
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0o600); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}
	if DirExists(testFile) {
		t.Errorf("DirExists(%s) = true for file, want false", testFile)
	}
}

func TestIsWritable(t *testing.T) {
	t.Parallel()
	// Test with writable directory
	tmpDir := t.TempDir()
	if !IsWritable(tmpDir) {
		t.Errorf("IsWritable(%s) = false, want true", tmpDir)
	}

	// Test with non-existent directory
	nonExistent := filepath.Join(tmpDir, "does-not-exist")
	if IsWritable(nonExistent) {
		t.Errorf("IsWritable(%s) = true for non-existent dir, want false", nonExistent)
	}

	// Skip permission test on Windows
	if os.PathSeparator == '\\' {
		t.Skip("Skipping Unix permission test on Windows")
	}

	// Test with read-only directory (Unix only)
	readOnlyDir := filepath.Join(tmpDir, "readonly")
	if err := os.MkdirAll(readOnlyDir, 0o500); err != nil {
		t.Fatalf("Failed to create read-only directory: %v", err)
	}
	if IsWritable(readOnlyDir) {
		t.Errorf("IsWritable(%s) = true for read-only dir, want false", readOnlyDir)
	}
}

func TestGetDiskUsage(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	usage, err := GetDiskUsage(tmpDir)
	if err != nil {
		t.Fatalf("GetDiskUsage(%s) failed: %v", tmpDir, err)
	}

	// Basic sanity checks
	if usage.TotalBytes == 0 {
		t.Error("TotalBytes should not be 0")
	}
	if usage.UsedPercent < 0 || usage.UsedPercent > 100 {
		t.Errorf("UsedPercent %f is out of range [0,100]", usage.UsedPercent)
	}
	if usage.UsedBytes > usage.TotalBytes {
		t.Error("UsedBytes should not exceed TotalBytes")
	}
	if usage.FreeBytes > usage.TotalBytes {
		t.Error("FreeBytes should not exceed TotalBytes")
	}

	// Test with non-existent path
	_, err = GetDiskUsage("/non/existent/path")
	if err == nil {
		t.Error("GetDiskUsage should fail for non-existent path")
	}
}

func TestGetDiskUsageMap(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	usageMap, err := GetDiskUsageMap(tmpDir)
	if err != nil {
		t.Fatalf("Failed to get disk usage: %v", err)
	}

	// Check that all expected fields are present
	requiredFields := []string{"total_bytes", "free_bytes", "used_bytes", "used_percent"}
	for _, field := range requiredFields {
		if _, ok := usageMap[field]; !ok {
			t.Errorf("Missing required field: %s", field)
		}
	}

	// Test error case
	_, err = GetDiskUsageMap("/non/existent/path")
	if err == nil {
		t.Error("Expected error for non-existent path")
	}
}
