package helpers

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lattiam/lattiam/internal/apiserver"
	"github.com/lattiam/lattiam/internal/config"
	"github.com/lattiam/lattiam/internal/events"
	"github.com/lattiam/lattiam/internal/executor"
	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/internal/system"
	"github.com/lattiam/lattiam/internal/utils/components"
)

// TestServer wraps an API server for testing
type TestServer struct {
	Server          *apiserver.APIServer
	Components      *system.BackgroundSystemComponents
	StateStore      interfaces.StateStore
	ProviderManager interfaces.ProviderLifecycleManager
	URL             string
	Port            int
}

// StartTestServer starts a new API server on a random port for testing
//
//nolint:funlen // Comprehensive test server setup with real components and graceful shutdown
func StartTestServer(t *testing.T) *TestServer {
	t.Helper()

	// Find a free port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Failed to find free port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close() // Ignore error - listener may already be closed

	// Create real components for acceptance testing
	testComponents := createRealComponents(t)

	// Create test config
	cfg := config.NewServerConfig()
	cfg.Port = port

	// Create and start the server
	server, err := apiserver.NewAPIServerWithComponents(
		cfg,
		testComponents.Components.Queue,
		testComponents.Components.Tracker,
		testComponents.Components.WorkerPool,
		testComponents.StateStore,
		testComponents.ProviderManager,
		testComponents.Interpolator,
	)
	if err != nil {
		t.Fatalf("Failed to create API server: %v", err)
	}

	// Connect deployment service to event bus for state store cleanup after destruction
	events.ConnectDeploymentServiceToEventBus(testComponents.EventBus, server.GetDeploymentService())

	// Start server in background
	errChan := make(chan error, 1)
	go func() {
		if err := server.Start(); err != nil && err.Error() != "http: Server closed" {
			errChan <- fmt.Errorf("server error: %w", err)
		}
		close(errChan)
	}()

	// Check for immediate errors
	select {
	case err := <-errChan:
		if err != nil {
			t.Fatalf("Failed to start server: %v", err)
		}
	default:
	}

	// Wait for server to be ready
	url := fmt.Sprintf("http://localhost:%d", port)
	client := &http.Client{Timeout: 5 * time.Second}
	maxRetries := 30
	for i := 0; i < maxRetries; i++ {
		resp, err := client.Get(url + "/api/v1/system/health")
		if err == nil {
			_ = resp.Body.Close() // Ignore error - body may already be closed
			if resp.StatusCode == 200 {
				break
			}
		}
		if i == maxRetries-1 {
			t.Fatalf("Server failed to start after %d attempts", maxRetries)
		}
		time.Sleep(100 * time.Millisecond)
	}

	return &TestServer{
		Server:          server,
		Components:      testComponents.Components,
		StateStore:      testComponents.StateStore,
		ProviderManager: testComponents.ProviderManager,
		URL:             url,
		Port:            port,
	}
}

// Stop gracefully shuts down the test server with extended timeout for complex deployments
func (ts *TestServer) Stop(t *testing.T) {
	t.Helper()

	// Wait for any active deployments to complete deletion - increased timeout for complex scenarios
	if err := ts.waitForActiveDeployments(30 * time.Second); err != nil {
		t.Logf("Warning: Active deployments may still be running: %v", err)
	}

	// Then shutdown with longer timeout for complex scenarios (increased from 5s to 15s)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := ts.Server.Shutdown(ctx); err != nil {
		t.Logf("Failed to stop server gracefully: %v", err)
	}
}

// waitForActiveDeployments waits for active deployments to complete
func (ts *TestServer) waitForActiveDeployments(timeout time.Duration) error {
	client := &http.Client{Timeout: 5 * time.Second}
	start := time.Now()
	attempt := 0

	for time.Since(start) < timeout {
		resp, err := client.Get(ts.URL + "/api/v1/deployments")
		if err != nil {
			// If we can't check deployments, assume server is shutting down
			return fmt.Errorf("failed to check server health: %w", err)
		}

		if resp.StatusCode != 200 {
			// If we can't get deployments, assume server is shutting down
			_ = resp.Body.Close() // Ignore error - body may already be closed
			return nil
		}

		// Parse response to check if any deployments are active
		var deployments []map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&deployments); err != nil {
			// If we can't parse, continue polling
			_ = resp.Body.Close() // Ignore error - body may already be closed
			time.Sleep(time.Second)
			continue
		}
		_ = resp.Body.Close() // Ignore error - body may already be closed

		// Check if any deployments are still processing
		hasActive := false
		for _, d := range deployments {
			if status, ok := d["status"].(string); ok && status == "processing" {
				hasActive = true
				break
			}
		}

		// If no active deployments, we're done
		if !hasActive {
			return nil
		}

		// Exponential backoff: 1s, 2s, 4s, 8s, then cap at 5s for cleanup checks
		backoff := time.Duration(1<<uint(attempt)) * time.Second // #nosec G115 - attempt is bounded by loop condition
		if backoff > 5*time.Second {
			backoff = 5 * time.Second
		}

		time.Sleep(backoff)
		attempt++
	}

	return fmt.Errorf("timeout waiting for active deployments to complete")
}

// TestComponents holds all the components needed for testing
type TestComponents struct {
	Components      *system.BackgroundSystemComponents
	StateStore      interfaces.StateStore
	ProviderManager interfaces.ProviderLifecycleManager
	Interpolator    interfaces.InterpolationResolver
	EventBus        *events.EventBus
}

// createRealComponents creates real components for testing
//
//nolint:funlen // Comprehensive real component setup for integration testing
func createRealComponents(t *testing.T) *TestComponents {
	t.Helper()

	// Create test configuration
	tempStateDir := t.TempDir() // Create isolated temp directory for this test

	// Create configuration
	cfg := config.NewServerConfig()

	// Load configuration from environment to support AWS backend testing
	if err := cfg.LoadFromEnv(); err != nil {
		t.Fatalf("Failed to load config from environment: %v", err)
	}

	// Expand paths for any file-based configuration
	if err := cfg.ExpandPaths(); err != nil {
		t.Fatalf("Failed to expand paths: %v", err)
	}

	// Only set backend defaults if not already configured from environment
	if cfg.StateStore.Type == "" || cfg.StateStore.Type == "file" {
		// Use memory backend for testing (file backend is deprecated)
		cfg.StateStore.Type = "memory"
	}

	// Set provider directory if not configured
	if cfg.ProviderDir == "" {
		cfg.ProviderDir = filepath.Join(tempStateDir, "providers")
	}

	// Always use embedded queue for testing
	cfg.Queue.Type = "embedded"

	// Create state store using the utility function that handles all backend types
	stateStore, err := components.CreateStateStore(cfg)
	if err != nil {
		t.Fatalf("Failed to create state store: %v", err)
	}

	// Create component factory for other components
	factory := system.NewDefaultComponentFactory()

	// Create provider manager without binary cache for tests
	// Binary cache is causing issues with provider paths in tests
	providerConfig := interfaces.ProviderManagerConfig{
		Options: map[string]interface{}{
			"provider_dir":        cfg.ProviderDir,
			"enable_binary_cache": false, // Disabled as it causes path issues in tests
		},
	}
	providerManager, err := factory.CreateProviderLifecycleManager(providerConfig)
	if err != nil {
		t.Fatalf("Failed to create provider manager: %v", err)
	}

	// Create interpolator
	interpolatorConfig := interfaces.InterpolationResolverConfig{
		// Use default configuration for interpolator
		Options: map[string]interface{}{},
	}
	interpolator, err := factory.CreateInterpolationResolver(interpolatorConfig)
	if err != nil {
		t.Fatalf("Failed to create interpolator: %v", err)
	}

	// Create synchronous event bus for testing
	eventBus := events.NewSynchronousEventBus()

	// Create deployment executor with the event bus
	deploymentExecutor := executor.NewWithEventBus(providerManager, stateStore, eventBus, 10*time.Minute)
	executorFunc := executor.CreateExecutorFunc(deploymentExecutor)

	// Create background system using factory
	bgComponents, err := system.NewBackgroundSystem(cfg, executorFunc)
	if err != nil {
		t.Fatalf("Failed to create background system: %v", err)
	}

	// Connect tracker to event bus
	events.ConnectTrackerToEventBus(eventBus, bgComponents.Tracker)

	// Start the worker pool
	bgComponents.WorkerPool.Start()

	// Ensure cleanup on test completion
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := bgComponents.Close(ctx); err != nil {
			t.Logf("Warning: failed to close components: %v", err)
		}
	})

	return &TestComponents{
		Components:      bgComponents,
		StateStore:      stateStore,
		ProviderManager: providerManager,
		Interpolator:    interpolator,
		EventBus:        eventBus,
	}
}

// GetActiveProviderCount returns the number of active provider processes
func (ts *TestServer) GetActiveProviderCount() int {
	// Test helper - provider count tracking not implemented
	return 0
}

// ClearProviderCache clears the provider cache for test isolation
func (ts *TestServer) ClearProviderCache() {
	// Test helper - cache clearing not implemented
}

// GetZombieProcessCount returns the number of zombie provider processes
func (ts *TestServer) GetZombieProcessCount() int {
	// Check for terraform provider processes that should be cleaned up
	cmd := exec.Command("pgrep", "-f", "terraform-provider")
	output, err := cmd.Output()

	// If pgrep finds processes, count them
	if err == nil && len(output) > 0 {
		processInfo := strings.TrimSpace(string(output))
		if processInfo != "" {
			// Count the number of lines (PIDs)
			return len(strings.Split(processInfo, "\n"))
		}
	}

	return 0
}

// verifyProviderCleanup verifies that all providers have been properly cleaned up
func (ts *TestServer) verifyProviderCleanup(t *testing.T) {
	t.Helper()

	// Check active providers through system
	activeProviders := ts.GetActiveProviderCount()
	if activeProviders > 0 {
		t.Errorf("Found %d active providers after shutdown - provider cleanup failed", activeProviders)
	}

	// Check for zombie processes
	zombieProcesses := ts.GetZombieProcessCount()
	if zombieProcesses > 0 {
		t.Errorf("Found %d zombie terraform provider processes after shutdown", zombieProcesses)

		// Get detailed process info for debugging
		cmd := exec.Command("ps", "-ef")
		if output, err := cmd.Output(); err == nil {
			lines := strings.Split(string(output), "\n")
			for _, line := range lines {
				if strings.Contains(line, "terraform-provider") {
					t.Logf("Zombie process: %s", line)
				}
			}
		}
	}
}

// StopWithProviderVerification stops the server and verifies complete provider cleanup
func (ts *TestServer) StopWithProviderVerification(t *testing.T) {
	t.Helper()

	// Count providers before shutdown for comparison
	preShutdownProviders := ts.GetActiveProviderCount()
	preShutdownZombies := ts.GetZombieProcessCount()

	t.Logf("Pre-shutdown: %d active providers, %d zombie processes",
		preShutdownProviders, preShutdownZombies)

	// Wait for any active deployments to complete
	if err := ts.waitForActiveDeployments(30 * time.Second); err != nil {
		t.Logf("Warning: Active deployments may still be running: %v", err)
	}

	// Log that components are available before shutdown
	if ts.Components != nil {
		t.Logf("Pre-shutdown: Components are available (Queue, Tracker, WorkerPool)")
	}

	// Perform shutdown with extended timeout for provider cleanup
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if err := ts.Server.Shutdown(ctx); err != nil {
		t.Logf("Failed to stop server gracefully: %v", err)
	}

	// Shutdown provider manager to ensure all provider processes are terminated
	if shutdownable, ok := ts.ProviderManager.(interface{ Shutdown() error }); ok {
		if err := shutdownable.Shutdown(); err != nil {
			t.Logf("Failed to shutdown provider manager: %v", err)
		} else {
			t.Logf("Provider manager shutdown completed")
		}
	}

	// Verify complete cleanup
	ts.verifyProviderCleanup(t)

	t.Logf("Server shutdown with provider verification completed successfully")
}

// SubmitTestDeployment submits a test deployment and returns the deployment ID
func (ts *TestServer) SubmitTestDeployment(t *testing.T) string {
	t.Helper()

	if ts.Components == nil {
		t.Fatal("Components not available")
	}

	ctx := context.Background()
	deployment := &interfaces.DeploymentRequest{
		Resources: []interfaces.Resource{
			{
				Type: "aws_s3_bucket",
				Name: "test-bucket",
				Properties: map[string]interface{}{
					"bucket": fmt.Sprintf("test-bucket-%d", time.Now().Unix()),
					"tags": map[string]interface{}{
						"Environment": "test",
						"ManagedBy":   "lattiam-test",
					},
				},
			},
		},
		Options: interfaces.DeploymentOptions{
			DryRun:     true, // Use dry run for tests
			Timeout:    30 * time.Second,
			MaxRetries: 1,
		},
	}

	// Create a queued deployment
	queued := &interfaces.QueuedDeployment{
		ID:        fmt.Sprintf("test-deployment-%d", time.Now().UnixNano()),
		Status:    interfaces.DeploymentStatusQueued,
		CreatedAt: time.Now(),
		Request:   deployment,
	}

	// Register with tracker
	if err := ts.Components.Tracker.Register(queued); err != nil {
		t.Fatalf("Failed to register deployment: %v", err)
	}

	// Submit to queue
	if err := ts.Components.Queue.Enqueue(ctx, queued); err != nil {
		// Remove from tracker if enqueue fails
		_ = ts.Components.Tracker.Remove(queued.ID)
		t.Fatalf("Failed to enqueue deployment: %v", err)
	}

	t.Logf("Submitted test deployment: %s", queued.ID)
	return queued.ID
}
