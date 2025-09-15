//go:build integration
// +build integration

package integration

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/apiserver"
	"github.com/lattiam/lattiam/internal/config"
	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/internal/mocks"
	"github.com/lattiam/lattiam/internal/monitoring"
)

// TestServerSecurityIntegration tests the complete security configuration
// including file permissions, API sanitization, and disk monitoring
func TestServerSecurityIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	t.Run("FilePermissionsAndLogging", func(t *testing.T) {
		// Create temporary directories for the test
		tempDir := t.TempDir()
		stateDir := filepath.Join(tempDir, "state")
		logFile := filepath.Join(tempDir, "server.log")

		// Set up configuration
		cfg := config.NewServerConfig()
		cfg.StateDir = stateDir
		cfg.LogFile = logFile
		cfg.Debug = false // Ensure we're not in debug mode
		cfg.Port = 9084   // Use a different port to avoid conflicts

		// Ensure state directory exists with proper permissions
		err := os.MkdirAll(stateDir, 0o700)
		require.NoError(t, err)

		// Create mock components
		queue := mocks.NewDeploymentQueue(t)
		tracker := mocks.NewDeploymentTracker(t)
		workerPool := mocks.NewWorkerPool(t)
		stateStore := mocks.NewMockStateStore()
		providerManager := mocks.NewMockProviderManager()
		interpolator := mocks.NewMockInterpolationResolver()

		// Start server
		server, err := apiserver.NewAPIServerWithComponents(
			cfg, queue, tracker, workerPool, stateStore, providerManager, interpolator)
		require.NoError(t, err)

		// Start server in background
		serverDone := make(chan error, 1)
		go func() {
			serverDone <- server.Start()
		}()

		// Wait for server to be ready
		time.Sleep(500 * time.Millisecond)

		// Test 1: Verify file permissions
		t.Run("VerifyFilePermissions", func(t *testing.T) {
			// Check state directory permissions
			info, err := os.Stat(stateDir)
			require.NoError(t, err)
			// Verify secure permissions
			assert.Equal(t, os.FileMode(0o700), info.Mode().Perm(), "State directory should have 0700 permissions")

			// Check log file permissions if it exists
			if _, err := os.Stat(logFile); err == nil {
				info, err := os.Stat(logFile)
				require.NoError(t, err)
				assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "Log file should have 0600 permissions")
			}
		})

		// Test 2: Verify API endpoints don't expose paths
		t.Run("VerifyAPISanitization", func(t *testing.T) {
			endpoints := []string{
				"/api/v1/system/config",
				"/api/v1/system/paths",
				"/api/v1/system/disk-usage",
				"/api/v1/system/storage",
			}

			for _, endpoint := range endpoints {
				resp, err := http.Get("http://localhost:9084" + endpoint)
				require.NoError(t, err, "Failed to query %s", endpoint)

				body, err := io.ReadAll(resp.Body)
				resp.Body.Close()
				require.NoError(t, err)

				// Verify no actual paths are exposed
				bodyStr := string(body)
				assert.NotContains(t, bodyStr, stateDir, "Endpoint %s should not expose state directory", endpoint)
				assert.NotContains(t, bodyStr, logFile, "Endpoint %s should not expose log file path", endpoint)
				assert.NotContains(t, bodyStr, tempDir, "Endpoint %s should not expose temp directory", endpoint)
			}
		})

		// Shutdown server gracefully
		if server != nil {
			// Since the server doesn't have a Shutdown method, we'll rely on test cleanup
			// The server will be stopped when the test ends
		}
	})

	t.Run("DiskMonitoringIntegration", func(t *testing.T) {
		// Create configuration
		cfg := config.NewServerConfig()
		cfg.StateDir = "/tmp/test-state"
		cfg.Port = 9085 // Different port

		// Create disk monitor
		monitor := monitoring.NewDiskMonitor(cfg)

		// Create mock disk checker that simulates high disk usage
		mockChecker := &MockDiskUsageChecker{}
		mockChecker.SetDiskUsage("/tmp/test-state", 92.0, 8*1024*1024*1024, 100*1024*1024*1024)
		monitor.SetDiskChecker(mockChecker)

		// Check for alerts
		alerts := monitor.CheckNow()

		// Verify alerts were generated
		require.Len(t, alerts, 1, "Should have one alert for high disk usage")
		assert.Equal(t, monitoring.AlertLevelCritical, alerts[0].Level)
		assert.Equal(t, 92.0, alerts[0].PercentUsed)

		// Test monitoring in background
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Set short check interval for testing
		monitor.SetCheckInterval(100 * time.Millisecond)

		// Start monitoring
		go monitor.Start(ctx)

		// Wait for a few check cycles
		time.Sleep(350 * time.Millisecond)

		// Should still have alerts
		currentAlerts := monitor.GetAlerts()
		assert.NotEmpty(t, currentAlerts, "Monitor should maintain alerts")

		// Update disk usage to normal levels
		mockChecker.SetDiskUsage("/tmp/test-state", 50.0, 50*1024*1024*1024, 100*1024*1024*1024)

		// Wait for next check
		time.Sleep(150 * time.Millisecond)

		// Alerts should be cleared
		currentAlerts = monitor.GetAlerts()
		assert.Empty(t, currentAlerts, "Alerts should be cleared when disk usage is normal")

		cancel()
	})

	t.Run("ConfigurationLoadingIntegration", func(t *testing.T) {
		// Set test values using t.Setenv for automatic cleanup
		t.Setenv("LATTIAM_PORT", "8888")
		t.Setenv("LATTIAM_DEBUG", "true")
		t.Setenv("LATTIAM_STATE_DIR", "/custom/state")

		cfg := config.NewServerConfig()
		err := cfg.LoadFromEnv()
		require.NoError(t, err)

		assert.Equal(t, 8888, cfg.Port)
		assert.True(t, cfg.Debug)
		assert.Equal(t, "/custom/state", cfg.StateDir)

		// Test validation
		err = cfg.Validate()
		require.NoError(t, err)

		// Test invalid configuration
		cfg.Port = 99999
		err = cfg.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid port")

		// Test sanitization in debug mode
		cfg.Port = 8080
		cfg.Debug = true
		sanitized := cfg.GetSanitized()

		// In debug mode, should still not expose actual paths
		_, hasStateDir := sanitized["state_dir"]
		assert.False(t, hasStateDir, "Sanitized config should not have state_dir even in debug mode")

		// Should have indicators
		assert.True(t, sanitized["state_configured"].(bool))
	})
}

// TestServerStartupAndShutdown tests the complete server lifecycle
func TestServerStartupAndShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create configuration
	cfg := config.NewServerConfig()
	cfg.Port = 9086
	cfg.StateDir = t.TempDir()

	// Expand paths
	err := cfg.ExpandPaths()
	require.NoError(t, err)

	// Ensure state directory exists
	err = os.MkdirAll(cfg.StateDir, 0o700)
	require.NoError(t, err)

	// Create mock components
	queue := mocks.NewDeploymentQueue(t)
	tracker := mocks.NewDeploymentTracker(t)
	workerPool := mocks.NewWorkerPool(t)
	stateStore := mocks.NewMockStateStore()
	providerManager := mocks.NewMockProviderManager()
	interpolator := mocks.NewMockInterpolationResolver()

	// Set up mock for GetMetrics method (needed for health endpoint)
	queue.On("GetMetrics").Return(interfaces.QueueMetrics{
		TotalEnqueued:    0,
		TotalDequeued:    0,
		CurrentDepth:     0,
		AverageWaitTime:  0,
		OldestDeployment: time.Time{},
	})

	// Set up mock for tracker List method (needed for health endpoint)
	tracker.On("List", mock.Anything).Return([]*interfaces.QueuedDeployment{}, nil)

	// Set up mock for worker pool Stop method (needed for server shutdown)
	workerPool.On("Stop", mock.Anything).Return(nil)

	// Create server
	server, err := apiserver.NewAPIServerWithComponents(
		cfg, queue, tracker, workerPool, stateStore, providerManager, interpolator)
	require.NoError(t, err)

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- server.Start()
	}()

	// Wait for server to start
	time.Sleep(500 * time.Millisecond)

	// Verify server is running
	resp, err := http.Get("http://localhost:9086/api/v1/system/health")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// Properly shutdown the server
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = server.Shutdown(ctx)
	require.NoError(t, err)

	// Assert mock expectations were met
	queue.AssertExpectations(t)
	tracker.AssertExpectations(t)
}

// MockDiskUsageChecker is a mock implementation for testing
type MockDiskUsageChecker struct {
	usageData map[string]*monitoring.DiskUsage
}

func (m *MockDiskUsageChecker) GetDiskUsage(path string) *monitoring.DiskUsage {
	if usage, ok := m.usageData[path]; ok {
		return usage
	}
	return nil
}

func (m *MockDiskUsageChecker) SetDiskUsage(path string, percentUsed float64, freeBytes, totalBytes uint64) {
	if m.usageData == nil {
		m.usageData = make(map[string]*monitoring.DiskUsage)
	}

	usedBytes := totalBytes - freeBytes
	m.usageData[path] = &monitoring.DiskUsage{
		TotalBytes:  totalBytes,
		FreeBytes:   freeBytes,
		UsedBytes:   usedBytes,
		PercentUsed: percentUsed,
	}
}
