package monitoring

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/lattiam/lattiam/internal/config"
)

func TestDiskMonitor_Thresholds(t *testing.T) {
	t.Parallel()
	cfg := createTestConfig()
	monitor := NewDiskMonitor(cfg)

	// Set up mock checker immediately to avoid checking real disk usage
	mockChecker := NewMockDiskUsageChecker()
	// Set safe default values for all test paths
	mockChecker.SetDiskUsage("/tmp/state", 50.0, 50*1024*1024*1024, 100*1024*1024*1024)
	mockChecker.SetDiskUsage("/tmp/state/store.db", 50.0, 50*1024*1024*1024, 100*1024*1024*1024)
	mockChecker.SetDiskUsage("/tmp/providers", 50.0, 50*1024*1024*1024, 100*1024*1024*1024)
	mockChecker.SetDiskUsage("/tmp", 50.0, 50*1024*1024*1024, 100*1024*1024*1024)
	monitor.SetDiskChecker(mockChecker)

	// Test default thresholds
	monitor.CheckNow()
	alerts := monitor.GetAlerts()
	if len(alerts) != 0 {
		t.Errorf("Expected no alerts with default setup, got %d", len(alerts))
	}

	// Test setting custom thresholds
	monitor.SetThresholds(70.0, 85.0)

	// Update mock with disk usage at 75% (between warn and alert)
	mockChecker.SetDiskUsage("/tmp/state", 75.0, 25*1024*1024*1024, 100*1024*1024*1024)

	alerts = monitor.CheckNow()

	if len(alerts) != 1 {
		t.Fatalf("Expected 1 alert, got %d", len(alerts))
	}

	if alerts[0].Level != AlertLevelWarning {
		t.Errorf("Expected warning level, got %s", alerts[0].Level)
	}

	if alerts[0].PercentUsed != 75.0 {
		t.Errorf("Expected 75%% usage, got %.1f%%", alerts[0].PercentUsed)
	}
}

func TestDiskMonitor_AlertLevels(t *testing.T) {
	t.Parallel()
	cfg := createTestConfig()
	monitor := NewDiskMonitor(cfg)
	mockChecker := NewMockDiskUsageChecker()
	monitor.SetDiskChecker(mockChecker)

	// Test no alert (50% usage)
	mockChecker.SetDiskUsage("/tmp/state", 50.0, 50*1024*1024*1024, 100*1024*1024*1024)
	alerts := monitor.CheckNow()
	if len(alerts) != 0 {
		t.Errorf("Expected no alerts at 50%% usage, got %d", len(alerts))
	}

	// Test warning level (85% usage)
	mockChecker.SetDiskUsage("/tmp/state", 85.0, 15*1024*1024*1024, 100*1024*1024*1024)
	alerts = monitor.CheckNow()
	if len(alerts) != 1 || alerts[0].Level != AlertLevelWarning {
		t.Errorf("Expected 1 warning at 85%% usage, got %d alerts", len(alerts))
	}

	// Test critical level (95% usage)
	mockChecker.SetDiskUsage("/tmp/state", 95.0, 5*1024*1024*1024, 100*1024*1024*1024)
	alerts = monitor.CheckNow()
	if len(alerts) != 1 || alerts[0].Level != AlertLevelCritical {
		t.Errorf("Expected 1 critical alert at 95%% usage, got %d alerts", len(alerts))
	}
}

func TestDiskMonitor_NonExistentPath(t *testing.T) {
	t.Parallel()
	cfg := createTestConfig()
	monitor := NewDiskMonitor(cfg)
	mockChecker := NewMockDiskUsageChecker()
	monitor.SetDiskChecker(mockChecker)

	// Don't set any disk usage data - simulates non-existent paths
	alerts := monitor.CheckNow()

	// Should not crash and should return no alerts
	if len(alerts) != 0 {
		t.Errorf("Expected no alerts for non-existent paths, got %d", len(alerts))
	}
}

func TestDiskMonitor_MultipleDirectories(t *testing.T) {
	t.Parallel()
	cfg := createTestConfig()
	cfg.StateDir = "/var/lattiam/state"
	cfg.StateStore.File.Path = "/var/lattiam/store/state.db"
	cfg.ProviderDir = "/opt/lattiam/providers"

	monitor := NewDiskMonitor(cfg)
	mockChecker := NewMockDiskUsageChecker()
	monitor.SetDiskChecker(mockChecker)

	// Set different usage levels for different directories
	mockChecker.SetDiskUsage("/var/lattiam/state", 91.0, 9*1024*1024*1024, 100*1024*1024*1024)
	mockChecker.SetDiskUsage("/var/lattiam/store/state.db", 85.0, 15*1024*1024*1024, 100*1024*1024*1024)
	mockChecker.SetDiskUsage("/opt/lattiam/providers", 70.0, 30*1024*1024*1024, 100*1024*1024*1024)

	alerts := monitor.CheckNow()

	// Should have 2 alerts: 1 critical and 1 warning
	if len(alerts) != 2 {
		t.Fatalf("Expected 2 alerts, got %d", len(alerts))
	}

	// Count alert levels
	criticalCount := 0
	warningCount := 0
	for _, alert := range alerts {
		switch alert.Level {
		case AlertLevelCritical:
			criticalCount++
		case AlertLevelWarning:
			warningCount++
		}
	}

	if criticalCount != 1 {
		t.Errorf("Expected 1 critical alert, got %d", criticalCount)
	}
	if warningCount != 1 {
		t.Errorf("Expected 1 warning alert, got %d", warningCount)
	}
}

func TestDiskMonitor_ConcurrentAccess(t *testing.T) {
	t.Parallel()
	cfg := createTestConfig()
	monitor := NewDiskMonitor(cfg)
	mockChecker := NewMockDiskUsageChecker()
	monitor.SetDiskChecker(mockChecker)

	mockChecker.SetDiskUsage("/tmp/state", 85.0, 15*1024*1024*1024, 100*1024*1024*1024)

	// Run concurrent operations
	var wg sync.WaitGroup
	errors := make(chan error, 10)

	// Multiple readers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				alerts := monitor.GetAlerts()
				_ = alerts
				time.Sleep(time.Millisecond)
			}
		}()
	}

	// Multiple checkers
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(_ int) {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				monitor.CheckNow()
				time.Sleep(time.Millisecond * 2)
			}
		}(i)
	}

	// Threshold setters
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 5; j++ {
				monitor.SetThresholds(75.0+float64(j), 85.0+float64(j))
				time.Sleep(time.Millisecond * 3)
			}
		}()
	}

	wg.Wait()
	close(errors)

	// Check for any errors
	for err := range errors {
		t.Errorf("Concurrent access error: %v", err)
	}
}

func TestDiskMonitor_CheckInterval(t *testing.T) {
	t.Parallel()
	cfg := createTestConfig()
	monitor := NewDiskMonitor(cfg)

	// Test default interval
	lastCheck := monitor.GetLastCheck()
	if !lastCheck.IsZero() {
		t.Error("Expected zero time for last check before any checks")
	}

	// Set custom interval
	monitor.SetCheckInterval(100 * time.Millisecond)

	// Perform a check
	monitor.CheckNow()
	firstCheck := monitor.GetLastCheck()
	if firstCheck.IsZero() {
		t.Error("Expected non-zero time after check")
	}

	// Wait a bit and check again
	time.Sleep(50 * time.Millisecond)
	monitor.CheckNow()
	secondCheck := monitor.GetLastCheck()

	if !secondCheck.After(firstCheck) {
		t.Error("Expected second check to be after first check")
	}
}

func TestDiskMonitor_Start(t *testing.T) {
	t.Parallel()
	cfg := createTestConfig()
	monitor := NewDiskMonitor(cfg)
	mockChecker := NewMockDiskUsageChecker()
	monitor.SetDiskChecker(mockChecker)

	// Set a very short check interval for testing
	monitor.SetCheckInterval(50 * time.Millisecond)

	// Set up disk usage that will trigger alerts
	mockChecker.SetDiskUsage("/tmp/state", 91.0, 9*1024*1024*1024, 100*1024*1024*1024)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start monitoring in background
	go monitor.Start(ctx)

	// Wait for a few check cycles
	time.Sleep(200 * time.Millisecond)

	// Should have alerts
	alerts := monitor.GetAlerts()
	if len(alerts) == 0 {
		t.Error("Expected alerts after periodic monitoring")
	}

	// Cancel and verify monitoring stops
	cancel()
	time.Sleep(100 * time.Millisecond)

	// Clear alerts and verify no new ones are generated
	monitor.CheckNow() // This clears and regenerates
	time.Sleep(100 * time.Millisecond)
	// The alerts should remain the same (no new periodic checks)
}

func TestDiskMonitor_AlertMessage(t *testing.T) {
	t.Parallel()
	cfg := createTestConfig()
	monitor := NewDiskMonitor(cfg)
	mockChecker := NewMockDiskUsageChecker()
	monitor.SetDiskChecker(mockChecker)

	// Set disk usage
	mockChecker.SetDiskUsage("/tmp/state", 92.5, 7884996198, 105226698752) // ~7.3GB free of ~98GB

	alerts := monitor.CheckNow()
	if len(alerts) != 1 {
		t.Fatalf("Expected 1 alert, got %d", len(alerts))
	}

	alert := alerts[0]

	// Check alert fields
	if alert.Path != "/tmp/state" {
		t.Errorf("Expected path /tmp/state, got %s", alert.Path)
	}

	if alert.PercentUsed != 92.5 {
		t.Errorf("Expected 92.5%% used, got %.1f%%", alert.PercentUsed)
	}

	if alert.FreeBytes != 7884996198 {
		t.Errorf("Expected 7884996198 free bytes, got %d", alert.FreeBytes)
	}

	if alert.TotalBytes != 105226698752 {
		t.Errorf("Expected 105226698752 total bytes, got %d", alert.TotalBytes)
	}

	// Check message format
	if alert.Message == "" {
		t.Error("Expected non-empty alert message")
	}

	// Verify timestamp is recent
	if time.Since(alert.Timestamp) > time.Second {
		t.Error("Alert timestamp should be recent")
	}
}

// Helper function to create test configuration
func createTestConfig() *config.ServerConfig {
	cfg := config.NewServerConfig()
	cfg.StateDir = "/tmp/state"
	cfg.StateStore.File.Path = "/tmp/state/store.db"
	cfg.ProviderDir = "/tmp/providers"
	cfg.PIDFile = "/tmp/lattiam.pid"
	return cfg
}
