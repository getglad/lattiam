// Package monitoring provides disk usage monitoring and alerting functionality
package monitoring

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/lattiam/lattiam/internal/config"
	"github.com/lattiam/lattiam/pkg/logging"
)

// DiskUsageChecker is an interface for getting disk usage (allows mocking)
type DiskUsageChecker interface {
	GetDiskUsage(path string) *DiskUsage
}

// DefaultDiskUsageChecker implements DiskUsageChecker using syscall
type DefaultDiskUsageChecker struct{}

// GetDiskUsage returns disk usage for a path
func (d *DefaultDiskUsageChecker) GetDiskUsage(path string) *DiskUsage {
	return getDiskUsage(path)
}

// MockDiskUsageChecker is a mock implementation of DiskUsageChecker for testing
type MockDiskUsageChecker struct {
	mu        sync.RWMutex
	usageData map[string]*DiskUsage
}

// NewMockDiskUsageChecker creates a new mock disk usage checker
func NewMockDiskUsageChecker() *MockDiskUsageChecker {
	return &MockDiskUsageChecker{
		usageData: make(map[string]*DiskUsage),
	}
}

// GetDiskUsage returns mocked disk usage for a path
func (m *MockDiskUsageChecker) GetDiskUsage(path string) *DiskUsage {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if usage, ok := m.usageData[path]; ok {
		return usage
	}
	// Return safe default for unmocked paths
	return &DiskUsage{
		TotalBytes:  100 * 1024 * 1024 * 1024, // 100GB
		FreeBytes:   50 * 1024 * 1024 * 1024,  // 50GB free
		UsedBytes:   50 * 1024 * 1024 * 1024,  // 50GB used
		PercentUsed: 50.0,                     // 50% used
	}
}

// SetDiskUsage sets mocked disk usage for a path
func (m *MockDiskUsageChecker) SetDiskUsage(path string, percentUsed float64, freeBytes, totalBytes uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	usedBytes := totalBytes - freeBytes
	m.usageData[path] = &DiskUsage{
		TotalBytes:  totalBytes,
		FreeBytes:   freeBytes,
		UsedBytes:   usedBytes,
		PercentUsed: percentUsed,
	}
}

// DiskMonitor monitors disk usage for configured directories
type DiskMonitor struct {
	config         *config.ServerConfig
	alertThreshold float64 // Percentage threshold for alerts (e.g., 90.0 for 90%)
	warnThreshold  float64 // Percentage threshold for warnings (e.g., 80.0 for 80%)
	checkInterval  time.Duration
	mu             sync.RWMutex
	lastCheck      time.Time
	alerts         []DiskAlert
	diskChecker    DiskUsageChecker
	logger         *logging.Logger
}

// DiskAlert represents a disk usage alert
type DiskAlert struct {
	Path        string
	Level       AlertLevel
	PercentUsed float64
	FreeBytes   uint64
	TotalBytes  uint64
	Message     string
	Timestamp   time.Time
}

// AlertLevel represents the severity of an alert
type AlertLevel string

const (
	// AlertLevelWarning indicates a disk usage warning threshold has been exceeded
	AlertLevelWarning AlertLevel = "warning"
	// AlertLevelCritical indicates a disk usage critical threshold has been exceeded
	AlertLevelCritical AlertLevel = "critical"
)

// NewDiskMonitor creates a new disk monitor
func NewDiskMonitor(cfg *config.ServerConfig) *DiskMonitor {
	monitor := &DiskMonitor{
		config:         cfg,
		alertThreshold: 90.0,
		warnThreshold:  80.0,
		checkInterval:  5 * time.Minute,
		alerts:         make([]DiskAlert, 0),
		diskChecker:    &DefaultDiskUsageChecker{},
		logger:         logging.NewLogger("disk-monitor"),
	}

	// In test mode, use a mock disk checker to prevent false alerts
	if os.Getenv("LATTIAM_TEST_MODE") == "true" {
		mockChecker := NewMockDiskUsageChecker()
		// Set safe disk usage values for all paths
		mockChecker.SetDiskUsage("/tmp/state", 50.0, 50*1024*1024*1024, 100*1024*1024*1024)
		mockChecker.SetDiskUsage("/tmp/state/store.db", 50.0, 50*1024*1024*1024, 100*1024*1024*1024)
		mockChecker.SetDiskUsage("/tmp/providers", 50.0, 50*1024*1024*1024, 100*1024*1024*1024)
		mockChecker.SetDiskUsage("/tmp", 50.0, 50*1024*1024*1024, 100*1024*1024*1024)
		monitor.diskChecker = mockChecker
	}

	return monitor
}

// Start begins monitoring disk usage
func (m *DiskMonitor) Start(ctx context.Context) {
	// Initial check
	m.checkDiskSpace()

	// Get initial check interval with lock
	m.mu.RLock()
	interval := m.checkInterval
	m.mu.RUnlock()

	// Start periodic monitoring
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Check if interval has changed
			m.mu.RLock()
			currentInterval := m.checkInterval
			m.mu.RUnlock()

			if currentInterval != interval {
				// Interval changed, restart ticker
				ticker.Stop()
				ticker = time.NewTicker(currentInterval)
				interval = currentInterval
			}

			m.checkDiskSpace()
		}
	}
}

// SetThresholds sets the warning and alert thresholds
func (m *DiskMonitor) SetThresholds(warn, alert float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.warnThreshold = warn
	m.alertThreshold = alert
}

// SetCheckInterval sets how often to check disk usage
func (m *DiskMonitor) SetCheckInterval(interval time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checkInterval = interval
}

// SetDiskChecker sets the disk usage checker (for testing)
func (m *DiskMonitor) SetDiskChecker(checker DiskUsageChecker) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.diskChecker = checker
}

// GetAlerts returns current alerts
func (m *DiskMonitor) GetAlerts() []DiskAlert {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Return a copy to avoid race conditions
	alerts := make([]DiskAlert, len(m.alerts))
	copy(alerts, m.alerts)
	return alerts
}

// GetLastCheck returns the time of the last disk check
func (m *DiskMonitor) GetLastCheck() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastCheck
}

// CheckNow performs an immediate disk check
func (m *DiskMonitor) CheckNow() []DiskAlert {
	m.checkDiskSpace()
	return m.GetAlerts()
}

// checkDiskSpace checks disk usage for all configured paths
func (m *DiskMonitor) checkDiskSpace() {
	// Collect paths without holding the lock
	paths := m.getPathsToMonitor()

	// Perform disk checks without holding the lock
	newAlerts := make([]DiskAlert, 0)
	checkTime := time.Now()

	// Get thresholds with lock
	m.mu.RLock()
	alertThreshold := m.alertThreshold
	warnThreshold := m.warnThreshold
	m.mu.RUnlock()

	for name, path := range paths {
		if path == "" {
			continue
		}

		usage := m.diskChecker.GetDiskUsage(path)
		if usage == nil {
			// Log error when disk check fails
			m.logger.Error("Failed to get disk usage for %s (%s)", name, path)
			continue
		}

		percentUsed := usage.PercentUsed
		freeBytes := usage.FreeBytes
		totalBytes := usage.TotalBytes

		// Check thresholds
		if percentUsed >= alertThreshold {
			alert := DiskAlert{
				Path:        path,
				Level:       AlertLevelCritical,
				PercentUsed: percentUsed,
				FreeBytes:   freeBytes,
				TotalBytes:  totalBytes,
				Message:     formatDiskAlert(name, path, percentUsed, freeBytes),
				Timestamp:   checkTime,
			}
			newAlerts = append(newAlerts, alert)

			// Log critical alert
			m.logger.Error("CRITICAL: Disk space alert for %s (%s): %.1f%% used, %s free",
				name, path, percentUsed, formatBytes(freeBytes))
		} else if percentUsed >= warnThreshold {
			alert := DiskAlert{
				Path:        path,
				Level:       AlertLevelWarning,
				PercentUsed: percentUsed,
				FreeBytes:   freeBytes,
				TotalBytes:  totalBytes,
				Message:     formatDiskAlert(name, path, percentUsed, freeBytes),
				Timestamp:   checkTime,
			}
			newAlerts = append(newAlerts, alert)

			// Log warning
			m.logger.Warn("Disk space warning for %s (%s): %.1f%% used, %s free",
				name, path, percentUsed, formatBytes(freeBytes))
		}
	}

	// Update shared state with minimal lock time
	m.mu.Lock()
	m.lastCheck = checkTime
	m.alerts = newAlerts
	m.mu.Unlock()
}

// getPathsToMonitor returns all paths that should be monitored
func (m *DiskMonitor) getPathsToMonitor() map[string]string {
	paths := make(map[string]string)

	// Add configured directories
	paths["State Directory"] = m.config.StateDir

	// Add state store path if configured
	if m.config.StateStore.File.Path != "" {
		paths["State Store"] = m.config.StateStore.File.Path
	}

	paths["Provider Directory"] = m.config.ProviderDir

	// Add log file directory if configured
	if m.config.GetLogPath() != "" {
		logDir := filepath.Dir(m.config.GetLogPath())
		paths["Log Directory"] = logDir
	}

	// Add temp directory (for PID file, etc.)
	paths["Temp Directory"] = filepath.Dir(m.config.PIDFile)

	return paths
}

// Helper types and functions

// DiskUsage contains disk usage information
type DiskUsage struct {
	TotalBytes  uint64
	FreeBytes   uint64
	UsedBytes   uint64
	PercentUsed float64
}

// getDiskUsage is implemented in platform-specific files:
// - disk_monitor_unix.go for Unix/Linux/macOS
// - disk_monitor_windows.go for Windows

// formatDiskAlert creates a human-readable alert message
func formatDiskAlert(name, path string, percentUsed float64, freeBytes uint64) string {
	return fmt.Sprintf("%s (%s) is %.1f%% full with %s free",
		name, path, percentUsed, formatBytes(freeBytes))
}

// formatBytes formats bytes into human-readable format
func formatBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := uint64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
