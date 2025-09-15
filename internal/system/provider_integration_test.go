//go:build integration && localstack
// +build integration,localstack

package system_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/internal/system"
	"github.com/lattiam/lattiam/pkg/provider/protocol"
	"github.com/lattiam/lattiam/tests/helpers/localstack"
)

// TestMain runs before all tests and performs cleanup
func TestMain(m *testing.M) {
	// Clean up any zombie provider processes before running tests
	if err := protocol.CleanupZombieProviders(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to cleanup zombie providers: %v\n", err)
	}

	// Run tests
	code := m.Run()

	// Clean up any remaining provider processes after tests
	if err := protocol.CleanupZombieProviders(); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to cleanup providers after tests: %v\n", err)
	}

	os.Exit(code)
}

// getProviderConfig returns LocalStack configuration using the centralized config source
func getProviderConfig(ctx context.Context) (interfaces.ProviderConfig, error) {
	configSource := localstack.NewConfigSource()
	return configSource.GetProviderConfig(ctx, "aws", "", nil)
}

// TestRealProviderIntegration tests with actual provider binaries
// Run with: go test -tags=integration ./internal/system
func TestRealProviderIntegration(t *testing.T) {
	t.Run("DownloadAndUseRealAWSProvider", func(t *testing.T) {
		// Create a temporary directory for providers
		tempDir, err := os.MkdirTemp("", "provider-test-*")
		require.NoError(t, err)
		defer os.RemoveAll(tempDir)

		// Create provider manager adapter directly
		adapter, err := system.NewProviderManagerAdapter(tempDir)
		require.NoError(t, err)
		require.NotNil(t, adapter)

		// Register cleanup immediately to ensure provider processes are terminated
		t.Cleanup(func() {
			if err := adapter.Shutdown(); err != nil {
				t.Logf("Failed to shutdown adapter: %v", err)
			}
			// Extra verification that no stale processes remain
			assertNoStaleProviderProcesses(t)
		})

		// Initialize
		err = adapter.Initialize()
		require.NoError(t, err)

		// Cast to ProviderLifecycleManager for provider operations
		manager := interfaces.ProviderLifecycleManager(adapter)

		ctx := context.Background()

		// Get LocalStack configuration for AWS provider
		config, err := getProviderConfig(ctx)
		if err != nil {
			t.Fatalf("Failed to get LocalStack configuration: %v", err)
		}

		// This will attempt to download the real AWS provider with LocalStack configuration
		provider, err := manager.GetProvider(ctx, "test-deployment-1", "aws", "6.2.0", config, "aws_s3_bucket")
		// The test will fail here if:
		// 1. No internet connection
		// 2. Can't download provider
		// 3. Can't start provider process
		if err != nil {
			t.Logf("Provider download/start failed (expected in test env): %v", err)

			// Check if this is a non-retryable error (like no internet)
			errStr := strings.ToLower(err.Error())
			if strings.Contains(errStr, "404 not found") ||
				strings.Contains(errStr, "download failed") ||
				strings.Contains(errStr, "no such host") ||
				strings.Contains(errStr, "connection refused") {
				t.Skip("Skipping integration test - provider download not available in this environment")
			}

			// For other errors, fail the test
			require.NoError(t, err, "Unexpected provider error")
			return
		}

		// If we get here, we have a real provider!
		require.NotNil(t, provider)

		// Verify we have active providers
		// Cast adapter to ProviderMonitor to access ListActiveProviders
		monitor := interfaces.ProviderMonitor(adapter)
		activeProviders := monitor.ListActiveProviders()
		require.NotEmpty(t, activeProviders, "Should have active providers")
	})
}

// TestProviderBinaryManagement tests provider download and caching
func TestProviderBinaryManagement(t *testing.T) {
	t.Run("DownloadMultipleProviders", func(t *testing.T) {
		tempDir, err := os.MkdirTemp("", "provider-cache-*")
		require.NoError(t, err)
		defer os.RemoveAll(tempDir)

		// Create real provider manager
		adapter, err := system.NewProviderManagerAdapter(tempDir)
		require.NoError(t, err)

		// Register cleanup immediately
		t.Cleanup(func() {
			if adapter != nil {
				if err := adapter.Shutdown(); err != nil {
					t.Logf("Failed to shutdown adapter: %v", err)
				}
			}
			// Always verify no stale provider processes
			assertNoStaleProviderProcesses(t)
		})

		ctx := context.Background()

		// Test downloading multiple providers
		providers := []struct {
			name    string
			version string
		}{
			{"aws", "6.2.0"},
			{"google", "4.0.0"},
			{"azurerm", "3.0.0"},
		}

		for _, p := range providers {
			t.Logf("Attempting to download %s provider v%s", p.name, p.version)

			// Get appropriate configuration for each provider
			var config interfaces.ProviderConfig
			if p.name == "aws" {
				// AWS provider needs LocalStack configuration
				var err error
				config, err = getProviderConfig(ctx)
				if err != nil {
					t.Logf("Failed to get LocalStack config for AWS provider: %v", err)
					continue
				}
			}
			// Other providers (google, azurerm) use nil config since LocalStack doesn't emulate them

			provider, err := adapter.GetProvider(ctx, "test-deployment-"+p.name, p.name, p.version, config, p.name+"_instance")
			if err != nil {
				t.Logf("Failed to get %s provider: %v", p.name, err)
				continue
			}

			// Verify provider is functional
			assert.NotNil(t, provider)
		}

		// Check that providers were cached
		activeProviders := adapter.ListActiveProviders()
		t.Logf("Active providers: %d", len(activeProviders))
	})
}

// Helper function to verify no stale provider processes remain
// This catches the exact bug from demo acceptance tests
func assertNoStaleProviderProcesses(t *testing.T) {
	t.Helper()

	// Check for terraform provider processes that should be cleaned up
	cmd := exec.Command("pgrep", "-f", "terraform-provider")
	output, err := cmd.Output()

	// If pgrep finds processes, it returns exit code 0 and outputs PIDs
	// If no processes found, it returns exit code 1 with no output
	if err == nil && len(output) > 0 {
		// Found processes - this is now an error condition
		processInfo := strings.TrimSpace(string(output))

		// Get more detailed process info for debugging
		detailCmd := exec.Command("ps", "-p", processInfo, "-o", "pid,ppid,command")
		var detailOutput []byte
		if detailOutput, _ = detailCmd.Output(); len(detailOutput) > 0 {
			t.Logf("Stale terraform provider process details:\n%s", string(detailOutput))
		}

		// Count the number of processes
		pids := strings.Split(processInfo, "\n")
		processCount := len(pids)

		// Fail the test - providers should be properly cleaned up
		t.Errorf("Found %d stale terraform provider processes after test (PIDs: %s). "+
			"Provider processes must be properly cleaned up after tests complete.",
			processCount, strings.ReplaceAll(processInfo, "\n", ", "))
	}

	// Note: pgrep returning exit code 1 (no processes found) is expected and good
}
