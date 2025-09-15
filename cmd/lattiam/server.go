package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/lattiam/lattiam/internal/apiserver"
	"github.com/lattiam/lattiam/internal/config"
	"github.com/lattiam/lattiam/internal/events"
	"github.com/lattiam/lattiam/internal/executor"
	"github.com/lattiam/lattiam/internal/logging"
	"github.com/lattiam/lattiam/internal/monitoring"
	"github.com/lattiam/lattiam/internal/system"
)

// Version can be set at build time with:
// go build -ldflags "-X main.Version=1.0.0"
var Version = "dev"

// Static errors for err113 compliance
var (
	ErrServerAlreadyRunning = errors.New("server is already running")
	ErrServerFailedToStart  = errors.New("server failed to start, check logs")
	ErrServerNotRunning     = errors.New("server is not running")
)

func runServerForeground(port int, debug bool) error { //nolint:funlen,gocyclo // Server initialization function with comprehensive setup
	// Set the version in config package
	config.AppVersion = Version

	// Create logger
	logger := logging.NewLogger("server")

	// Create server configuration
	cfg := config.NewServerConfig()
	cfg.Port = port
	cfg.Debug = debug

	// Load from environment
	if err := cfg.LoadFromEnv(); err != nil {
		return fmt.Errorf("failed to load config from environment: %w", err)
	}

	// Expand paths
	if err := cfg.ExpandPaths(); err != nil {
		return fmt.Errorf("failed to expand paths: %w", err)
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Log configuration on startup (without sensitive paths)
	logger.Infof("Starting Lattiam server v%s", Version)
	logger.Infof("Configuration:")
	logger.Infof("  Port: %d", cfg.Port)
	logger.Infof("  Debug: %t", cfg.Debug)
	logger.Infof("  State Store: %s", cfg.StateStore.Type)

	// Only log detailed paths in debug mode
	if cfg.Debug {
		logger.Debugf("State Directory: %s", cfg.StateDir)
		logger.Debugf("Log File: %s", cfg.GetLogPath())
		logger.Debugf("Provider Directory: %s", cfg.ProviderDir)
		logger.Debugf("State Path: %s", cfg.StateStore.File.Path)
	}

	// Write config info file for debugging
	if err := cfg.WriteConfigInfo(); err != nil {
		logger.Warnf("Failed to write config info: %v", err)
	}

	// Create background system components
	logger.Infof("Queue Type: %s", cfg.Queue.Type)

	// Create state store
	stateStore, err := createStateStore(cfg)
	if err != nil {
		return fmt.Errorf("failed to create state store: %w", err)
	}

	// Create provider manager
	providerManager, err := createProviderManager(cfg)
	if err != nil {
		return fmt.Errorf("failed to create provider manager: %w", err)
	}

	// Create event bus
	eventBus := events.NewEventBus()

	// Create deployment executor with the event bus
	deploymentExecutor := executor.NewWithEventBus(providerManager, stateStore, eventBus, 10*time.Minute)
	executorFunc := executor.CreateExecutorFunc(deploymentExecutor)

	// Create background system using factory
	components, err := system.NewBackgroundSystem(cfg, executorFunc)
	if err != nil {
		return fmt.Errorf("failed to create background system: %w", err)
	}

	// Connect tracker to event bus
	events.ConnectTrackerToEventBus(eventBus, components.Tracker)

	// Create interpolator
	interpolator, err := createInterpolator(cfg)
	if err != nil {
		return fmt.Errorf("failed to create interpolator: %w", err)
	}

	// Start the worker pool
	components.WorkerPool.Start()

	// Create and run the server with components
	server, err := apiserver.NewAPIServerWithComponents(
		cfg, components.Queue, components.Tracker, components.WorkerPool,
		stateStore, providerManager, interpolator,
	)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Start disk monitor in background (optional)
	monitorCtx, monitorCancel := context.WithCancel(context.Background())
	defer monitorCancel()

	if !cfg.DaemonMode {
		diskMonitor := monitoring.NewDiskMonitor(cfg)
		go diskMonitor.Start(monitorCtx)
		logger.Infof("Disk monitoring enabled (warning: %.0f%%, critical: %.0f%%)", 80.0, 90.0)
	}

	// Start server in goroutine
	errChan := make(chan error, 1)
	go func() {
		if err := server.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errChan <- err
		}
	}()

	// Wait for signal or error
	select {
	case <-sigChan:
		// Shutting down server
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			return fmt.Errorf("failed to shutdown server: %w", err)
		}

		// Shutdown components
		if err := components.Close(ctx); err != nil {
			return fmt.Errorf("failed to shutdown components: %w", err)
		}
		return nil
	case err := <-errChan:
		return err
	}
}

func runServerDaemon(port int, debug bool) error { //nolint:funlen // Daemon setup function with comprehensive initialization
	// Create logger
	logger := logging.NewLogger("server-daemon")

	// Create configuration
	cfg := config.NewServerConfig()
	cfg.Port = port
	cfg.Debug = debug
	cfg.DaemonMode = true
	if err := cfg.LoadFromEnv(); err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Note: We don't pre-check if server is running here to avoid TOCTOU race.
	// The savePID function will atomically check and create the PID file.

	// Expand paths
	if err := cfg.ExpandPaths(); err != nil {
		return fmt.Errorf("failed to expand paths: %w", err)
	}

	// Create log directory if specified with secure permissions
	logPath := cfg.GetLogPath()
	if logPath != "" {
		logDir := filepath.Dir(logPath)
		if err := os.MkdirAll(logDir, 0o700); err != nil {
			return fmt.Errorf("failed to create log directory: %w", err)
		}
	}

	// Create log file with secure permissions
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600) // #nosec G304 - logPath is from config
	if err != nil {
		return fmt.Errorf("failed to create log file: %w", err)
	}

	// Fork a child process to run the server
	executable, err := os.Executable()
	if err != nil {
		_ = logFile.Close()
		return fmt.Errorf("failed to get executable path: %w", err)
	}

	// Start the server process using ourselves
	args := []string{"server", "start", "--port", strconv.Itoa(port)}
	if debug {
		args = append(args, "--debug")
	}
	cmd := exec.Command(executable, args...) // #nosec G204 - executable is self (os.Executable), args are controlled
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	setupServerProcess(cmd)

	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("failed to start server: %w", err)
	}
	if err := logFile.Close(); err != nil {
		return fmt.Errorf("failed to close log file: %w", err)
	}

	// Save PID
	if err := savePID(cmd.Process.Pid, cfg.PIDFile); err != nil {
		if err := cmd.Process.Kill(); err != nil {
			// Process kill failed, but continuing - ignore error
			_ = err
		}
		return fmt.Errorf("failed to save PID: %w", err)
	}

	// Wait a moment to check if server started successfully
	time.Sleep(2 * time.Second)

	if !isServerRunning(cfg.PIDFile) {
		return fmt.Errorf("%w at: %s", ErrServerFailedToStart, logPath)
	}

	logger.Infof("Server started successfully in background")
	logger.Infof("Log file: %s", logPath)
	logger.Infof("PID file: %s", cfg.PIDFile)

	return nil
}

func stopServer(pidFile string) error {
	pid, err := readPIDFromFile(pidFile)
	if err != nil {
		return ErrServerNotRunning
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("failed to find process: %w", err)
	}

	// Send SIGTERM for graceful shutdown
	if err := process.Signal(syscall.SIGTERM); err != nil {
		// Process might already be dead
		removePIDFile(pidFile)
		return ErrServerNotRunning
	}

	// Wait for process to exit
	for range 10 {
		if !isProcessRunning(pid) {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	// Force kill if still running
	if isProcessRunning(pid) {
		if err := process.Kill(); err != nil {
			// Force kill failed, but continuing - ignore error
			_ = err
		}
	}

	removePIDFile(pidFile)
	return nil
}

func checkServerStatus(pidFile string, port int) error {
	if !isServerRunning(pidFile) {
		return nil
	}

	pid, _ := readPIDFromFile(pidFile)
	_ = pid // Used for status checking

	// Try to check health endpoint
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	url := fmt.Sprintf("http://localhost:%d/api/v1/system/health", port)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil
	}

	client := &http.Client{
		Timeout: 5 * time.Second,
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer func() { _ = resp.Body.Close() }() // Ignore error - response cleanup

	return nil
}

func isServerRunning(pidFile string) bool {
	pid, err := readPIDFromFile(pidFile)
	if err != nil {
		return false
	}
	return isProcessRunning(pid)
}

func isProcessRunning(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

func savePID(pid int, pidFile string) error {
	// Ensure parent directory exists with secure permissions
	pidDir := filepath.Dir(pidFile)
	if err := os.MkdirAll(pidDir, 0o700); err != nil {
		return fmt.Errorf("failed to create PID directory: %w", err)
	}

	// Use O_EXCL to atomically check and create - fails if file exists
	file, err := os.OpenFile(pidFile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) // #nosec G304 - pidFile path is from config
	if err != nil {
		if os.IsExist(err) {
			// File already exists, check if process is still running
			existingPID, readErr := readPIDFromFile(pidFile)
			if readErr == nil && isProcessRunning(existingPID) {
				return fmt.Errorf("server already running with PID %d (pid file: %s)", existingPID, pidFile)
			}
			// Stale PID file, remove and retry once
			_ = os.Remove(pidFile)                                                     // Ignore error - cleanup operation
			file, err = os.OpenFile(pidFile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600) // #nosec G304 - pidFile path is from config
			if err != nil {
				return fmt.Errorf("failed to create PID file %s after removing stale file: %w", pidFile, err)
			}
		} else {
			return fmt.Errorf("failed to create PID file %s: %w", pidFile, err)
		}
	}
	defer func() { _ = file.Close() }() // Ignore error - file cleanup in defer

	_, err = fmt.Fprintf(file, "%d\n", pid)
	if err != nil {
		// Clean up file on write error (defer will handle close)
		_ = os.Remove(pidFile) // Ignore error - cleanup on failure
		return fmt.Errorf("failed to write PID: %w", err)
	}

	return nil
}

func readPIDFromFile(pidFile string) (int, error) {
	data, err := os.ReadFile(pidFile) // #nosec G304 - pidFile path is from config
	if err != nil {
		return 0, fmt.Errorf("failed to read PID file %s: %w", pidFile, err)
	}
	// Trim whitespace including newlines
	pidStr := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return 0, fmt.Errorf("failed to parse PID from file %s (content: %q): %w", pidFile, pidStr, err)
	}
	return pid, nil
}

func removePIDFile(pidFile string) {
	_ = os.Remove(pidFile) // Ignore error - cleanup operation
}
