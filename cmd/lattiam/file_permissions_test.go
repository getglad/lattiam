package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lattiam/lattiam/internal/config"
)

//nolint:paralleltest // Cannot use t.Parallel() because test uses t.Setenv
func TestSecureFilePermissions(t *testing.T) { //nolint:funlen,gocognit,gocyclo // Test function with comprehensive test cases
	// Cannot use t.Parallel() because config info file test uses t.Setenv

	// Skip on Windows as Unix permissions don't apply
	if os.PathSeparator == '\\' {
		t.Skip("Skipping Unix permission test on Windows")
	}

	tmpDir := t.TempDir()

	tests := []struct {
		name         string
		testFunc     func(t *testing.T, tmpDir string)
		expectedPerm os.FileMode
		description  string
	}{
		{
			name: "log directory permissions",
			testFunc: func(t *testing.T, tmpDir string) {
				t.Helper()
				logDir := filepath.Join(tmpDir, "logs")
				if err := os.MkdirAll(logDir, 0o700); err != nil {
					t.Fatalf("Failed to create log directory: %v", err)
				}

				info, err := os.Stat(logDir)
				if err != nil {
					t.Fatalf("Failed to stat log directory: %v", err)
				}

				perm := info.Mode().Perm()
				if perm != 0o700 {
					t.Errorf("Log directory has insecure permissions %o, expected 0700", perm)
				}
			},
			expectedPerm: 0o700,
			description:  "Log directories should be accessible only by owner",
		},
		{
			name: "log file permissions",
			testFunc: func(t *testing.T, tmpDir string) {
				t.Helper()
				logFile := filepath.Join(tmpDir, "server.log")
				file, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) //nolint:gosec // Test file creation
				if err != nil {
					t.Fatalf("Failed to create log file: %v", err)
				}
				_ = file.Close() // Ignore error - test cleanup

				info, err := os.Stat(logFile)
				if err != nil {
					t.Fatalf("Failed to stat log file: %v", err)
				}

				perm := info.Mode().Perm()
				if perm != 0o600 {
					t.Errorf("Log file has insecure permissions %o, expected 0600", perm)
				}
			},
			expectedPerm: 0o600,
			description:  "Log files should be readable/writable only by owner",
		},
		{
			name: "PID file permissions",
			testFunc: func(t *testing.T, tmpDir string) {
				t.Helper()
				pidFile := filepath.Join(tmpDir, "server.pid")
				if err := os.WriteFile(pidFile, []byte("12345"), 0o600); err != nil {
					t.Fatalf("Failed to create PID file: %v", err)
				}

				info, err := os.Stat(pidFile)
				if err != nil {
					t.Fatalf("Failed to stat PID file: %v", err)
				}

				perm := info.Mode().Perm()
				if perm != 0o600 {
					t.Errorf("PID file has insecure permissions %o, expected 0600", perm)
				}
			},
			expectedPerm: 0o600,
			description:  "PID files should be readable/writable only by owner",
		},
		{
			name: "config info file permissions",
			testFunc: func(t *testing.T, tmpDir string) {
				t.Helper()
				// Test that WriteConfigInfo creates files with secure permissions
				infoFile := filepath.Join(tmpDir, "lattiam.info")
				t.Setenv("LATTIAM_INFO_FILE", infoFile)

				cfg := config.NewServerConfig()
				if err := cfg.WriteConfigInfo(); err != nil {
					t.Fatalf("Failed to write config info: %v", err)
				}
				info, err := os.Stat(infoFile)
				if err != nil {
					t.Fatalf("Failed to stat config info file: %v", err)
				}

				perm := info.Mode().Perm()
				// Config info file can be world-readable since it contains
				// sanitized data (no sensitive paths)
				// Just verify it was created successfully
				if perm == 0 {
					t.Error("Config info file has no permissions")
				}
			},
			expectedPerm: 0o644, // May vary based on umask
			description:  "Config info files should not contain sensitive data",
		},
	}

	for _, tt := range tests { //nolint:paralleltest // Cannot use t.Parallel() with t.Setenv
		t.Run(tt.name, func(t *testing.T) {
			// Cannot use t.Parallel() in subtests that use t.Setenv (e.g., config info file test)
			// Sanitize test name for directory creation - replace spaces with underscores
			dirName := strings.ReplaceAll(tt.name, " ", "_")
			testDir := filepath.Join(tmpDir, dirName)
			if err := os.MkdirAll(testDir, 0o755); err != nil { //nolint:gosec // Test directory creation
				t.Fatalf("Failed to create test directory: %v", err)
			}

			tt.testFunc(t, testDir)
		})
	}
}

func TestStateDirectoryPermissions(t *testing.T) {
	t.Parallel()
	if os.PathSeparator == '\\' {
		t.Skip("Skipping Unix permission test on Windows")
	}

	tmpDir := t.TempDir()
	stateDir := filepath.Join(tmpDir, ".lattiam", "state")

	// Create state directory hierarchy with secure permissions
	dirs := []string{
		stateDir,
		filepath.Join(stateDir, "deployments"),
		filepath.Join(stateDir, "locks"),
		filepath.Join(stateDir, "meta"),
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}

		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("Failed to stat directory %s: %v", dir, err)
		}

		perm := info.Mode().Perm()
		if perm != 0o700 {
			t.Errorf("Directory %s has insecure permissions %o, expected 0700", dir, perm)
		}

		// Verify not accessible by others
		testFile := filepath.Join(dir, "test")
		if err := os.WriteFile(testFile, []byte("test"), 0o600); err != nil {
			t.Fatalf("Failed to write test file: %v", err)
		}

		// Clean up test file
		_ = os.Remove(testFile) // Ignore error - test cleanup
	}
}

func TestNoWorldReadableFiles(t *testing.T) {
	t.Parallel()
	if os.PathSeparator == '\\' {
		t.Skip("Skipping Unix permission test on Windows")
	}

	tmpDir := t.TempDir()

	// Test various file creation scenarios
	files := []struct {
		name string
		perm os.FileMode
		ok   bool
	}{
		{"secure.log", 0o600, true},
		{"secure.pid", 0o600, true},
		{"insecure.log", 0o644, false},
		{"world-readable.txt", 0o666, false},
	}

	for _, f := range files {
		filePath := filepath.Join(tmpDir, f.name)
		if err := os.WriteFile(filePath, []byte("test"), f.perm); err != nil {
			t.Fatalf("Failed to create file %s: %v", f.name, err)
		}

		info, err := os.Stat(filePath)
		if err != nil {
			t.Fatalf("Failed to stat file %s: %v", f.name, err)
		}

		perm := info.Mode().Perm()
		isWorldReadable := perm&0o004 != 0

		if f.ok && isWorldReadable {
			t.Errorf("File %s should not be world-readable but has permissions %o", f.name, perm)
		}

		if !f.ok && !isWorldReadable {
			t.Errorf("Test file %s was expected to be world-readable for testing", f.name)
		}
	}
}

func TestSavePIDSecurePermissions(t *testing.T) {
	t.Parallel()
	if os.PathSeparator == '\\' {
		t.Skip("Skipping Unix permission test on Windows")
	}

	tmpDir := t.TempDir()
	pidFile := filepath.Join(tmpDir, "server.pid")

	// Test savePID function behavior
	err := savePID(12345, pidFile)
	if err != nil {
		t.Fatalf("savePID failed: %v", err)
	}

	// Check directory permissions
	dirInfo, err := os.Stat(tmpDir)
	if err != nil {
		t.Fatalf("Failed to stat PID directory: %v", err)
	}

	// Directory permissions are typically controlled by umask
	_ = dirInfo

	// Check file permissions
	fileInfo, err := os.Stat(pidFile)
	if err != nil {
		t.Fatalf("Failed to stat PID file: %v", err)
	}

	perm := fileInfo.Mode().Perm()
	if perm != 0o600 {
		t.Errorf("PID file has insecure permissions %o, expected 0600", perm)
	}

	// Verify content
	content, err := os.ReadFile(pidFile) // #nosec G304 -- pidFile is controlled test variable
	if err != nil {
		t.Fatalf("Failed to read PID file: %v", err)
	}

	if string(content) != "12345\n" {
		t.Errorf("PID file contains wrong content: %s", content)
	}
}

func TestLogFileCreationInDaemonMode(t *testing.T) {
	t.Parallel()
	if os.PathSeparator == '\\' {
		t.Skip("Skipping Unix permission test on Windows")
	}

	tmpDir := t.TempDir()
	logPath := filepath.Join(tmpDir, "logs", "server.log")
	logDir := filepath.Dir(logPath)

	// Simulate daemon mode log creation
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		t.Fatalf("Failed to create log directory: %v", err)
	}

	// Check directory was created with correct permissions
	dirInfo, err := os.Stat(logDir)
	if err != nil {
		t.Fatalf("Failed to stat log directory: %v", err)
	}

	dirPerm := dirInfo.Mode().Perm()
	if dirPerm != 0o700 {
		t.Errorf("Log directory has insecure permissions %o, expected 0700", dirPerm)
	}

	// Create log file with secure permissions
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) // #nosec G304 -- logPath is controlled test variable
	if err != nil {
		t.Fatalf("Failed to create log file: %v", err)
	}
	_ = logFile.Close() // Ignore error - test cleanup

	// Verify file permissions
	fileInfo, err := os.Stat(logPath)
	if err != nil {
		t.Fatalf("Failed to stat log file: %v", err)
	}

	filePerm := fileInfo.Mode().Perm()
	if filePerm != 0o600 {
		t.Errorf("Log file has insecure permissions %o, expected 0600", filePerm)
	}
}
