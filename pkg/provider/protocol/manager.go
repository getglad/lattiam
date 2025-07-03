// Package protocol implements direct communication with Terraform providers
// using the Terraform plugin protocol, bypassing HCL and terraform CLI
package protocol

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"github.com/lattiam/lattiam/internal/config"
)

// Static errors for err113 compliance
var (
	ErrProviderStartTimeout = errors.New("timeout waiting for provider to start")
)

// ProviderManager manages provider binaries and their lifecycle
type ProviderManager struct {
	// baseDir is where provider binaries are stored
	baseDir string

	// httpClient for downloading providers
	httpClient *http.Client

	// schemaCache caches provider schemas for CLI backward compatibility
	schemaCache map[string]*tfprotov6.GetProviderSchemaResponse
	schemaMu    sync.RWMutex

	// activeProviders tracks all currently running provider instances
	activeProviders   []*ProviderInstance
	activeProvidersMu sync.Mutex

	// config holds application configuration
	config *config.Config
}

// ProviderInstance represents a running provider process
type ProviderInstance struct {
	Name    string
	Version string
	Path    string
	cmd     *exec.Cmd
	address string
	stopped bool
	mu      sync.Mutex

	// Protocol connection will be added later
	// conn *grpc.ClientConn
}

// Stop stops the provider process synchronously and ensures it's only done once.
func (p *ProviderInstance) Stop() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.stopped {
		return nil // Already stopped
	}

	if p.cmd == nil || p.cmd.Process == nil {
		p.stopped = true
		return nil // Nothing to stop
	}

	// Kill the process
	if err := p.cmd.Process.Kill(); err != nil {
		// Log the error but proceed to Wait, as the process might already be gone
		GetDebugLogger().Logf("provider-stop", "Failed to kill provider process %d: %v", p.cmd.Process.Pid, err)
	}

	// Wait for the process to exit and release resources
	// This is crucial for preventing zombie processes and ensuring cleanup.
	if err := p.cmd.Wait(); err != nil {
		// We expect an error because we killed the process.
		// A successful Wait() here would be unusual.
		// We can log this for debugging, but it's not a failure condition.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// The program has exited with an error, which is expected after a Kill.
			GetDebugLogger().Logf("provider-stop", "Provider process %d exited as expected: %s", p.cmd.Process.Pid, exitErr)
		} else {
			// Other Wait error (e.g., waiting on a process that was never started).
			// This could be the "Wait was already called" error.
			GetDebugLogger().Logf("provider-stop", "Error waiting for provider process: %v", err)
		}
	}

	p.stopped = true
	return nil
}

// ProviderMetadata contains information about a provider
type ProviderMetadata struct {
	Name     string
	Version  string
	URL      string
	SHA256   string
	Platform string
}

// NewProviderManager creates a provider manager with a default cache location
func NewProviderManager(baseDirOverride ...string) (*ProviderManager, error) {
	var baseDir string
	if len(baseDirOverride) > 0 && baseDirOverride[0] != "" {
		baseDir = baseDirOverride[0]
	} else {
		// Use default shared cache location
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get user home directory: %w", err)
		}
		baseDir = filepath.Join(homeDir, ".lattiam", "providers")
	}

	if err := os.MkdirAll(baseDir, 0o750); err != nil {
		return nil, fmt.Errorf("failed to create provider directory: %w", err)
	}

	cfg := config.LoadConfig()

	return &ProviderManager{
		baseDir:     baseDir,
		httpClient:  &http.Client{Timeout: config.GetHTTPClientTimeout()},
		schemaCache: make(map[string]*tfprotov6.GetProviderSchemaResponse),
		config:      cfg,
	}, nil
}

// ProviderConfig contains provider-specific configuration
type ProviderConfig map[string]map[string]interface{}

// GetProvider returns a NEW provider instance each time (no caching)
func (pm *ProviderManager) GetProvider(ctx context.Context, name, version string) (*ProviderInstance, error) {
	return pm.GetProviderWithConfig(ctx, name, version, nil)
}

// GetProviderWithConfig returns a NEW provider instance with specific configuration
func (pm *ProviderManager) GetProviderWithConfig(
	ctx context.Context, name, version string, config ProviderConfig,
) (*ProviderInstance, error) {
	// Download if necessary
	binPath, err := pm.EnsureProviderBinary(ctx, name, version)
	if err != nil {
		return nil, fmt.Errorf("failed to ensure provider binary: %w", err)
	}

	// Start a NEW provider instance
	provider, err := pm.startProviderWithConfig(ctx, name, version, binPath, config)
	if err != nil {
		return nil, fmt.Errorf("failed to start provider: %w", err)
	}

	return provider, nil
}

// startProviderWithConfig launches a provider process with specific configuration
func (pm *ProviderManager) startProviderWithConfig(
	ctx context.Context, name, version, binPath string, config ProviderConfig,
) (*ProviderInstance, error) {
	debugLogger := GetDebugLogger()
	debugLogger.Logf("provider-start", "Starting provider %s v%s from %s", name, version, binPath)

	cmd := exec.CommandContext(ctx, binPath)
	env := pm.setupBaseEnvironment()

	// Pass AWS environment variables to the provider if they're set
	// This is crucial for LocalStack testing and AWS profile authentication
	awsEnvVars := []string{
		"AWS_ENDPOINT_URL",
		"AWS_ACCESS_KEY_ID",
		"AWS_SECRET_ACCESS_KEY",
		"AWS_DEFAULT_REGION",
		"AWS_REGION",
		"AWS_PROFILE",
		"AWS_SHARED_CREDENTIALS_FILE",
		"AWS_CONFIG_FILE",
		"AWS_SKIP_CREDENTIALS_VALIDATION",
		"AWS_SKIP_METADATA_API_CHECK",
		"AWS_SKIP_REQUESTING_ACCOUNT_ID",
		// Authentication-related environment variables
		"SSH_AUTH_SOCK",
		"HOME",
		"USER",
		"XDG_RUNTIME_DIR",
		"PATH",
	}

	// Apply deployment-specific configuration first
	env = pm.applyProviderConfig(env, config, debugLogger)

	// Then apply any other AWS environment variables from the server process
	for _, envVar := range awsEnvVars {
		if value := os.Getenv(envVar); value != "" {
			// Don't override if already set by config
			if pm.shouldSkipEnvVar(envVar, config) {
				continue
			}
			env = append(env, envVar+"="+value)
		}
	}

	cmd.Env = env

	// Enable debug logging if needed
	pm.setupDebugLogging(cmd, debugLogger, name, version)

	// Launch and wait for provider to be ready
	return pm.launchProvider(cmd, name, version, binPath)
}

// StartProvider is a convenience function to start a provider process directly
//
//nolint:funlen // Provider initialization logic
func StartProvider(ctx context.Context, binPath string) (*ProviderInstance, error) {
	debugLogger := GetDebugLogger()
	name := filepath.Base(binPath)
	version := "unknown"
	cfg := config.LoadConfig()
	// Terraform providers expect this magic cookie
	const magicCookie = "d602bf8f470bc67ca7faa0386276bbdd4330efaf76d1a219cb4d6991ca9872b2"

	cmd := exec.CommandContext(ctx, binPath)
	env := os.Environ()
	env = append(env, "TF_PLUGIN_MAGIC_COOKIE="+magicCookie)

	// Pass AWS environment variables to the provider if they're set
	// This is crucial for LocalStack testing and AWS profile authentication
	awsEnvVars := []string{
		"AWS_ENDPOINT_URL",
		"AWS_ACCESS_KEY_ID",
		"AWS_SECRET_ACCESS_KEY",
		"AWS_DEFAULT_REGION",
		"AWS_REGION",
		"AWS_PROFILE",
		"AWS_SHARED_CREDENTIALS_FILE",
		"AWS_CONFIG_FILE",
		"AWS_SKIP_CREDENTIALS_VALIDATION",
		"AWS_SKIP_METADATA_API_CHECK",
		"AWS_SKIP_REQUESTING_ACCOUNT_ID",
		// Authentication-related environment variables
		"SSH_AUTH_SOCK",
		"HOME",
		"USER",
		"XDG_RUNTIME_DIR",
		"PATH",
	}

	for _, envVar := range awsEnvVars {
		if value := os.Getenv(envVar); value != "" {
			env = append(env, envVar+"="+value)
		}
	}

	cmd.Env = env

	// Enable debug logging if LATTIAM_DEBUG is set
	if debugLogger.IsEnabled() {
		cmd.Env = append(cmd.Env, "TF_LOG=DEBUG")
		providerLogFile := debugLogger.LogFile(fmt.Sprintf("terraform-provider-%s-%s", name, version))
		cmd.Env = append(cmd.Env, "TF_LOG_PATH="+providerLogFile)
		debugLogger.Logf("provider-start", "Provider debug log: %s", providerLogFile)
	}

	// Set up pipes to capture provider output
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start the provider
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start provider: %w", err)
	}

	// Read the provider's address from stdout
	addressChan := make(chan string, 1)
	errorChan := make(chan error, 1)

	go func() {
		buf := make([]byte, 1024)
		n, err := stdout.Read(buf)
		if err != nil {
			errorChan <- fmt.Errorf("failed to read provider output: %w", err)
			return
		}
		output := string(buf[:n])
		debugLogger.Logf("provider-start", "Provider stdout: %s", output)
		addressChan <- output
	}()

	// Always capture stderr to debug log
	go func() {
		stderrWriter := debugLogger.Writer(fmt.Sprintf("provider-stderr-%s-%s", name, version))
		if debugLogger.IsEnabled() {
			// Copy to both stderr and debug log
			_, _ = io.Copy(io.MultiWriter(os.Stderr, stderrWriter), stderr)
		} else {
			// Only copy to debug log
			_, _ = io.Copy(stderrWriter, stderr)
		}
	}()

	// Wait for address or error
	select {
	case address := <-addressChan:
		return &ProviderInstance{
			Name:    "provider",
			Version: "unknown",
			Path:    binPath,
			cmd:     cmd,
			address: address,
		}, nil
	case err := <-errorChan:
		_ = cmd.Process.Kill()
		_ = cmd.Wait() // Ensure resources are released and I/O goroutines can exit.
		return nil, err
	case <-time.After(cfg.ProviderStartupTimeout):
		_ = cmd.Process.Kill()
		_ = cmd.Wait() // Ensure resources are released and I/O goroutines can exit.
		return nil, ErrProviderStartTimeout
	}
}

// setEnvVar sets or updates an environment variable in a slice of env vars
func setEnvVar(env []string, key, value string) []string {
	prefix := key + "="
	for i, envVar := range env {
		if strings.HasPrefix(envVar, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

// setupBaseEnvironment sets up the base environment for the provider
func (pm *ProviderManager) setupBaseEnvironment() []string {
	const magicCookie = "d602bf8f470bc67ca7faa0386276bbdd4330efaf76d1a219cb4d6991ca9872b2"
	env := os.Environ()
	env = append(env, "TF_PLUGIN_MAGIC_COOKIE="+magicCookie)
	return env
}

// applyProviderConfig applies provider-specific configuration to environment
func (pm *ProviderManager) applyProviderConfig(env []string, config ProviderConfig, debugLogger *DebugLogger) []string {
	if config == nil {
		return env
	}

	for providerName, providerConfig := range config {
		for key, value := range providerConfig {
			envKey := strings.ToUpper(fmt.Sprintf("%s_%s", providerName, key))
			env = setEnvVar(env, envKey, fmt.Sprintf("%v", value))
			debugLogger.Logf("provider-start", "Setting %s=%v for provider %s", envKey, value, providerName)
		}
	}
	return env
}

// shouldSkipEnvVar checks if an environment variable should be skipped
func (pm *ProviderManager) shouldSkipEnvVar(envVar string, config ProviderConfig) bool {
	if config == nil {
		return false
	}

	// Check if the env var is managed by any provider config
	for providerName, providerConfig := range config {
		envKeyPrefix := strings.ToUpper(providerName) + "_"
		if strings.HasPrefix(envVar, envKeyPrefix) {
			configKey := strings.ToLower(strings.TrimPrefix(envVar, envKeyPrefix))
			if _, exists := providerConfig[configKey]; exists {
				return true
			}
		}
	}

	return false
}

// setupDebugLogging configures debug logging for the provider
func (pm *ProviderManager) setupDebugLogging(cmd *exec.Cmd, debugLogger *DebugLogger, name, version string) {
	if debugLogger.IsEnabled() {
		cmd.Env = append(cmd.Env, "TF_LOG=DEBUG")
		providerLogFile := debugLogger.LogFile(fmt.Sprintf("terraform-provider-%s-%s", name, version))
		cmd.Env = append(cmd.Env, "TF_LOG_PATH="+providerLogFile)
		debugLogger.Logf("provider-start", "Provider debug log: %s", providerLogFile)
	}
}

// launchProvider starts the provider process and waits for its address
func (pm *ProviderManager) launchProvider(cmd *exec.Cmd, name, version, binPath string) (*ProviderInstance, error) {
	// Set up pipes to capture provider output
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start the provider
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start provider: %w", err)
	}

	// Setup channels for reading provider output
	addressChan := make(chan string, 1)
	errorChan := make(chan error, 1)

	// Start goroutines to read output
	pm.readProviderOutput(stdout, addressChan, errorChan, GetDebugLogger())
	pm.captureProviderStderr(stderr, name, version)

	// Wait for provider to be ready
	instance, err := pm.waitForProvider(cmd, name, version, binPath, addressChan, errorChan)
	if err != nil {
		return nil, err
	}

	pm.activeProvidersMu.Lock()
	pm.activeProviders = append(pm.activeProviders, instance)
	pm.activeProvidersMu.Unlock()

	return instance, nil
}

// readProviderOutput reads the provider's address from stdout and then
// continues to drain the pipe to prevent the provider from blocking.
func (pm *ProviderManager) readProviderOutput(stdout io.Reader, addressChan chan string, errorChan chan error, debugLogger *DebugLogger) {
	go func() {
		// Use a bufio.Reader to robustly read the first line and then drain the rest.
		reader := bufio.NewReader(stdout)

		// Read the first line to get the address.
		line, err := reader.ReadString('\n')
		if err != nil {
			errorChan <- fmt.Errorf("failed to read provider address line: %w", err)
			return
		}

		// Send the address.
		address := strings.TrimSpace(line)
		debugLogger.Logf("provider-start", "Provider stdout: %s", address)
		addressChan <- address

		// Continuously drain the rest of the stdout to the debug log to prevent blocking.
		// This is critical for providers that are verbose on stdout during operations.
		_, err = io.Copy(debugLogger.Writer("provider-stdout-drain"), reader)
		if err != nil {
			debugLogger.Logf("provider-stdout-drain", "Error draining provider stdout: %v", err)
		}
	}()
}

// captureProviderStderr captures provider stderr output
func (pm *ProviderManager) captureProviderStderr(stderr io.Reader, name, version string) {
	debugLogger := GetDebugLogger()
	go func() {
		stderrWriter := debugLogger.Writer(fmt.Sprintf("provider-stderr-%s-%s", name, version))
		if debugLogger.IsEnabled() {
			// Copy to both stderr and debug log
			_, _ = io.Copy(io.MultiWriter(os.Stderr, stderrWriter), stderr)
		} else {
			// Only copy to debug log
			_, _ = io.Copy(stderrWriter, stderr)
		}
	}()
}

// waitForProvider waits for the provider to be ready
func (pm *ProviderManager) waitForProvider(
	cmd *exec.Cmd, name, version, binPath string,
	addressChan chan string, errorChan chan error,
) (*ProviderInstance, error) {
	select {
	case address := <-addressChan:
		return &ProviderInstance{
			Name:    name,
			Version: version,
			Path:    binPath,
			cmd:     cmd,
			address: address,
		}, nil
	case err := <-errorChan:
		_ = cmd.Process.Kill()
		_ = cmd.Wait() // Ensure resources are released and I/O goroutines can exit.
		return nil, err
	case <-time.After(pm.config.ProviderStartupTimeout):
		_ = cmd.Process.Kill()
		_ = cmd.Wait() // Ensure resources are released and I/O goroutines can exit.
		return nil, ErrProviderStartTimeout
	}
}

// Close closes the provider manager and cleans up resources
func (pm *ProviderManager) Close() error {
	pm.activeProvidersMu.Lock()
	defer pm.activeProvidersMu.Unlock()

	var firstErr error
	for _, instance := range pm.activeProviders {
		if err := instance.Stop(); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("failed to stop provider %s: %w", instance.Name, err)
			} else {
				firstErr = fmt.Errorf("%w; failed to stop provider %s: %w", firstErr, instance.Name, err)
			}
		}
	}
	pm.activeProviders = nil // Clear the slice
	return firstErr
}
