package protocol

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/lattiam/lattiam/pkg/logging"
)

// DebugLogger handles debug output to files
type DebugLogger struct {
	enabled  bool
	debugDir string
	mu       sync.Mutex
	writers  map[string]io.WriteCloser
	logger   *logging.Logger
}

// NewDebugLogger creates a debug logger
func NewDebugLogger() *DebugLogger {
	enabled := os.Getenv("LATTIAM_DEBUG") != ""
	debugDir := os.Getenv("LATTIAM_DEBUG_DIR")
	if debugDir == "" {
		debugDir = "./tmp/lattiam-debug"
	}

	if enabled {
		if err := os.MkdirAll(debugDir, 0o700); err != nil {
			// Create temporary logger since we're in constructor
			tempLogger := logging.NewLogger("debug")
			tempLogger.Error("Failed to create debug directory: %v", err)
		}

		// Create a main debug log
		mainLog := filepath.Join(debugDir, fmt.Sprintf("lattiam-main-%d.log", time.Now().Unix()))
		if f, err := os.Create(mainLog); err == nil { //nolint:gosec // Debug log path is constructed safely
			// Create temporary logger since we're in constructor
			tempLogger := logging.NewLogger("debug")
			tempLogger.Info("Debug logging enabled. Logs will be written to: %s", debugDir)
			tempLogger.Info("Main log: %s", mainLog)
			_ = f.Close() // Close the file as we're only creating it for now
		}
	}

	return &DebugLogger{
		enabled:  enabled,
		debugDir: debugDir,
		writers:  make(map[string]io.WriteCloser),
		logger:   logging.NewLogger("debug"),
	}
}

// LogFile returns a file path for a specific log type
func (d *DebugLogger) LogFile(name string) string {
	if !d.enabled {
		return ""
	}
	return filepath.Join(d.debugDir, fmt.Sprintf("%s-%d.log", name, time.Now().Unix()))
}

// Writer returns a writer for a specific log type
func (d *DebugLogger) Writer(name string) io.Writer {
	if !d.enabled {
		return io.Discard
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	if w, exists := d.writers[name]; exists {
		return w
	}

	logPath := d.LogFile(name)
	f, err := os.Create(logPath) // #nosec G304
	if err != nil {
		d.logger.Error("Failed to create debug log %s: %v", logPath, err)
		return io.Discard
	}

	d.writers[name] = f
	return f
}

// Close closes all open writers
func (d *DebugLogger) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()

	for name, w := range d.writers {
		if err := w.Close(); err != nil {
			d.logger.Error("Failed to close debug log %s: %v", name, err)
		}
	}
	d.writers = make(map[string]io.WriteCloser)
}

// Logf logs a formatted message to a specific log
func (d *DebugLogger) Logf(name, format string, args ...interface{}) {
	if !d.enabled {
		return
	}

	w := d.Writer(name)
	_, _ = fmt.Fprintf(w, "[%s] %s\n", time.Now().Format("15:04:05.000"), fmt.Sprintf(format, args...))
}

// SaveData saves arbitrary data to a file
func (d *DebugLogger) SaveData(name string, data []byte) error {
	if !d.enabled {
		return nil
	}

	path := filepath.Join(d.debugDir, fmt.Sprintf("%s-%d.bin", name, time.Now().Unix()))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("failed to write debug file: %w", err)
	}
	return nil
}

// IsEnabled returns whether debug logging is enabled
func (d *DebugLogger) IsEnabled() bool {
	return d.enabled
}

// DebugDir returns the debug directory path
func (d *DebugLogger) DebugDir() string {
	return d.debugDir
}

var globalDebugLogger = NewDebugLogger() //nolint:gochecknoglobals // Singleton debug logger

// GetDebugLogger returns the global debug logger
func GetDebugLogger() *DebugLogger {
	return globalDebugLogger
}

// ReinitializeDebugLogger reinitializes the global debug logger
// This is useful when environment variables are set after package initialization
func ReinitializeDebugLogger() {
	globalDebugLogger = NewDebugLogger()
}
