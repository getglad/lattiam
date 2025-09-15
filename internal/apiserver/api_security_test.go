package apiserver // Test file

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lattiam/lattiam/internal/config"
	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/internal/mocks"
	"github.com/lattiam/lattiam/internal/system"
)

func TestAPISecurityEndToEnd(t *testing.T) { //nolint:funlen,gocognit // Comprehensive security test
	t.Parallel()
	// Create configuration with sensitive paths
	cfg := config.NewServerConfig()
	cfg.Port = 8085
	cfg.Debug = false
	cfg.StateDir = "/production/sensitive/state"
	cfg.LogFile = "/var/log/production/lattiam.log"
	cfg.ProviderDir = "/opt/lattiam/providers"
	cfg.PIDFile = "/var/run/lattiam.pid"
	cfg.StateStore.File.Path = "/production/sensitive/state/deployments"

	// Create mock components
	queue := mocks.NewDeploymentQueue(t)
	tracker := mocks.NewDeploymentTracker(t)
	workerPool := mocks.NewWorkerPool(t)
	stateStore := mocks.NewMockStateStore()

	// Create API server with components
	server, err := NewAPIServerWithComponents(cfg, queue, tracker, workerPool, stateStore, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create API server: %v", err)
	}

	// Test all operational endpoints
	endpoints := []struct {
		path   string
		checks []string
	}{
		{
			path: "/api/v1/system/config",
			checks: []string{
				"should not contain /production",
				"should not contain /var/log",
				"should not contain /opt",
				"should not contain state_dir",
				"should not contain log_file",
				"should contain port",
				"should contain state_store",
			},
		},
		{
			path: "/api/v1/system/paths",
			checks: []string{
				"should not contain /production",
				"should not contain absolute paths",
				"should contain configured",
				"should contain healthy",
			},
		},
		{
			path: "/api/v1/system/disk-usage",
			checks: []string{
				"should not contain /production",
				"should not contain path fields",
				"should contain storage",
				"should contain thresholds",
			},
		},
		{
			path: "/api/v1/system/storage",
			checks: []string{
				"should not contain base_dir",
				"should not contain /production",
				"should contain disk_space",
			},
		},
		{
			path: "/api/v1/system/runtime",
			checks: []string{
				"should not contain paths",
				"should contain go_version",
				"should contain memory",
			},
		},
	}

	for _, ep := range endpoints {
		t.Run(ep.path, func(t *testing.T) {
			t.Parallel()
			// Make request
			req := httptest.NewRequest("GET", ep.path, nil)
			w := httptest.NewRecorder()

			server.Router().ServeHTTP(w, req)

			// Check response code
			if w.Code != http.StatusOK {
				t.Errorf("Expected status 200, got %d", w.Code)
			}

			// Get response body
			body := w.Body.String()

			// Perform security checks
			for _, check := range ep.checks {
				switch {
				case strings.HasPrefix(check, "should not contain"):
					forbidden := strings.TrimPrefix(check, "should not contain ")
					if strings.Contains(body, forbidden) {
						t.Errorf("Response contains forbidden content '%s'", forbidden)
						t.Logf("Response body: %s", body)
					}

				case strings.HasPrefix(check, "should contain"):
					expected := strings.TrimPrefix(check, "should contain ")
					if !strings.Contains(body, expected) {
						t.Errorf("Response missing expected content '%s'", expected)
						t.Logf("Response body: %s", body)
					}
				}
			}

			// Verify valid JSON
			var response map[string]interface{}
			if err := json.Unmarshal([]byte(body), &response); err != nil {
				t.Errorf("Response is not valid JSON: %v", err)
			}
		})
	}
}

func TestAPISecurityWithDebugMode(t *testing.T) {
	t.Parallel()
	// Even in debug mode, API should not expose paths
	cfg := config.NewServerConfig()
	cfg.Port = 8085
	cfg.Debug = true // Debug enabled
	cfg.StateDir = "/debug/state"
	cfg.LogFile = "/debug/logs/server.log"

	// Create mock components
	queue := mocks.NewDeploymentQueue(t)
	tracker := mocks.NewDeploymentTracker(t)
	workerPool := mocks.NewWorkerPool(t)
	stateStore := mocks.NewMockStateStore()

	server, err := NewAPIServerWithComponents(cfg, queue, tracker, workerPool, stateStore, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create API server: %v", err)
	}

	// Test config endpoint
	req := httptest.NewRequest("GET", "/api/v1/system/config", nil)
	w := httptest.NewRecorder()

	server.Router().ServeHTTP(w, req)

	body := w.Body.String()

	// Even in debug mode, should not expose actual paths
	if strings.Contains(body, "/debug") {
		t.Error("API exposes paths even in debug mode")
		t.Logf("Response: %s", body)
	}

	// Should show debug=true
	if !strings.Contains(body, `"debug":true`) {
		t.Error("Debug mode not reflected in config")
	}
}

func TestAPIPathTraversalProtection(t *testing.T) {
	t.Parallel()
	// Test that API doesn't accept path parameters that could expose filesystem
	cfg := config.NewServerConfig()
	cfg.StateDir = "/safe/state"

	// Create mock components
	queue := mocks.NewDeploymentQueue(t)
	tracker := mocks.NewDeploymentTracker(t)
	workerPool := mocks.NewWorkerPool(t)
	stateStore := mocks.NewMockStateStore()

	server, err := NewAPIServerWithComponents(cfg, queue, tracker, workerPool, stateStore, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create API server: %v", err)
	}

	// Attempt various path traversal attacks
	attacks := []string{
		"/api/v1/system/paths?path=../../../etc/passwd",
		"/api/v1/system/disk-usage?dir=/etc",
		"/api/v1/system/storage?base=../../",
		"/api/v1/system/config?override=/home/user/.ssh",
	}

	for _, attack := range attacks {
		t.Run(attack, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest("GET", attack, nil)
			w := httptest.NewRecorder()

			server.Router().ServeHTTP(w, req)

			// Should either ignore parameters or return safe response
			body := w.Body.String()

			// Check for any system paths in response
			systemPaths := []string{"/etc", "/home", "/usr", "/var", ".ssh", "passwd"}
			for _, path := range systemPaths {
				if strings.Contains(body, path) {
					t.Errorf("Attack vector exposed system path: %s", path)
					t.Logf("Attack: %s", attack)
					t.Logf("Response: %s", body)
				}
			}
		})
	}
}

func TestAPIConsistentSanitization(t *testing.T) {
	t.Parallel()
	// Ensure all endpoints use consistent sanitization
	cfg := config.NewServerConfig()
	cfg.StateDir = "/data/lattiam/state"
	cfg.ProviderDir = "/data/lattiam/providers"

	// Create a real component factory and state store
	componentFactory := system.NewDefaultComponentFactory()
	stateStoreConfig := interfaces.StateStoreConfig{
		Type: "memory",
		Options: map[string]interface{}{
			"path": cfg.StateStore.File.Path,
		},
	}

	stateStore, err := componentFactory.CreateStateStore(stateStoreConfig)
	if err != nil {
		t.Fatalf("Failed to create state store: %v", err)
	}

	// Create mock components for other dependencies
	queue := mocks.NewDeploymentQueue(t)
	tracker := mocks.NewDeploymentTracker(t)
	workerPool := mocks.NewWorkerPool(t)

	server, err := NewAPIServerWithComponents(cfg, queue, tracker, workerPool, stateStore, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create API server: %v", err)
	}

	// Collect all responses
	endpoints := []string{
		"/api/v1/system/config",
		"/api/v1/system/paths",
		"/api/v1/system/disk-usage",
		"/api/v1/system/storage",
		"/api/v1/system/runtime",
	}

	responses := make(map[string]string)

	for _, endpoint := range endpoints {
		req := httptest.NewRequest("GET", endpoint, nil)
		w := httptest.NewRecorder()

		server.Router().ServeHTTP(w, req)

		if w.Code == http.StatusOK {
			responses[endpoint] = w.Body.String()
		}
	}

	// Check that no response contains the configured paths
	for endpoint, response := range responses {
		if strings.Contains(response, "/data/lattiam") {
			t.Errorf("Endpoint %s exposes configured path", endpoint)
		}

		// Verify JSON structure is valid
		var data interface{}
		if err := json.Unmarshal([]byte(response), &data); err != nil {
			t.Errorf("Endpoint %s returns invalid JSON: %v", endpoint, err)
		}
	}
}

func TestAPIErrorsDoNotExposePaths(t *testing.T) {
	t.Parallel()
	// Test that error responses also don't expose paths
	cfg := config.NewServerConfig()
	cfg.StateDir = "/secure/state"

	// Create mock components
	queue := mocks.NewDeploymentQueue(t)
	tracker := mocks.NewDeploymentTracker(t)
	workerPool := mocks.NewWorkerPool(t)
	stateStore := mocks.NewMockStateStore()
	// Note: The mocks will handle errors through their ShouldFail mechanism

	server, err := NewAPIServerWithComponents(cfg, queue, tracker, workerPool, stateStore, nil, nil)
	if err != nil {
		t.Fatalf("Failed to create API server: %v", err)
	}

	// Request that should cause an error
	req := httptest.NewRequest("GET", "/api/v1/system/storage", nil)
	w := httptest.NewRecorder()

	server.Router().ServeHTTP(w, req)

	// Even error responses should not contain paths
	body := w.Body.String()
	if strings.Contains(body, "/secure") {
		t.Error("Error response contains sensitive path")
		t.Logf("Response: %s", body)
	}
}
