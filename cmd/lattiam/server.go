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
	"syscall"
	"time"

	"github.com/lattiam/lattiam/internal/apiserver"
)

// Static errors for err113 compliance
var (
	ErrServerAlreadyRunning = errors.New("server is already running")
	ErrServerFailedToStart  = errors.New("server failed to start, check logs")
	ErrServerNotRunning     = errors.New("server is not running")
)

const (
	pidFileName = "lattiam-server.pid"
	logFileName = "lattiam-server.log"
)

func runServerForeground(port int) error {
	// Create and run the server
	server, err := apiserver.NewAPIServer(port)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	// Handle graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

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
		return nil
	case err := <-errChan:
		return err
	}
}

func runServerDaemon(port int) error {
	// Check if server is already running
	if isServerRunning() {
		return ErrServerAlreadyRunning
	}

	// Create log file
	logFile, err := os.Create(getLogPath())
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
	cmd := exec.Command(executable, "server", "start", "--port", strconv.Itoa(port))
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return fmt.Errorf("failed to start server: %w", err)
	}
	if err := logFile.Close(); err != nil {
		return fmt.Errorf("failed to close log file: %w", err)
	}

	// Save PID
	if err := savePID(cmd.Process.Pid); err != nil {
		if err := cmd.Process.Kill(); err != nil {
			// Process kill failed, but continuing - ignore error
			_ = err
		}
		return fmt.Errorf("failed to save PID: %w", err)
	}

	// Wait a moment to check if server started successfully
	time.Sleep(2 * time.Second)

	if !isServerRunning() {
		return fmt.Errorf("%w at: %s", ErrServerFailedToStart, getLogPath())
	}

	return nil
}

func stopServer() error {
	pid, err := readPID()
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
		removePID()
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

	removePID()
	return nil
}

func checkServerStatus() error {
	if !isServerRunning() {
		return nil
	}

	pid, _ := readPID()
	_ = pid // Used for status checking

	// Try to check health endpoint (default port 8084, but could be custom)
	ctx := context.Background()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://localhost:8084/api/v1/health", http.NoBody)
	if err != nil {
		return nil
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	return nil
}

func isServerRunning() bool {
	pid, err := readPID()
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

func getPIDPath() string {
	return filepath.Join(os.TempDir(), pidFileName)
}

func getLogPath() string {
	return filepath.Join(os.TempDir(), logFileName)
}

func savePID(pid int) error {
	if err := os.WriteFile(getPIDPath(), []byte(strconv.Itoa(pid)), 0o600); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}
	return nil
}

func readPID() (int, error) {
	data, err := os.ReadFile(getPIDPath())
	if err != nil {
		return 0, fmt.Errorf("failed to read PID file: %w", err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		return 0, fmt.Errorf("failed to parse PID: %w", err)
	}
	return pid, nil
}

func removePID() {
	os.Remove(getPIDPath())
}
