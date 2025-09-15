package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/lattiam/lattiam/internal/config"
	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/internal/mocks"
)

func TestOperationsHandler_GetConfig_NoSensitiveData(t *testing.T) {
	t.Parallel()
	// Create config with sensitive paths
	cfg := config.NewServerConfig()
	cfg.Port = 8085
	cfg.Debug = false
	cfg.StateDir = "/sensitive/state/directory"
	cfg.LogFile = "/sensitive/logs/server.log"
	cfg.ProviderDir = "/sensitive/providers"
	cfg.PIDFile = "/var/run/lattiam.pid"

	// Create mocks for dependencies
	stateStore := mocks.NewMockStateStore()
	workerPool := mocks.NewWorkerPool(t)
	queue := mocks.NewDeploymentQueue(t)

	handler := NewOperationsHandler(cfg, stateStore, workerPool, queue)

	// Make request
	req := httptest.NewRequest("GET", "/api/v1/system/config", nil)
	w := httptest.NewRecorder()

	handler.GetConfig(w, req)

	// Check response
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Verify no sensitive paths are exposed
	sensitiveFields := []string{
		"state_dir", "log_file", "provider_dir", "pid_file",
		"state_path", "StateDir", "LogFile", "ProviderDir",
	}

	for _, field := range sensitiveFields {
		if value, exists := response[field]; exists {
			t.Errorf("Sensitive field '%s' exposed in config response: %v", field, value)
		}
	}

	// Verify non-sensitive fields are present
	if port, ok := response["port"].(float64); !ok || int(port) != 8085 {
		t.Errorf("Expected port 8085, got %v", response["port"])
	}

	// Verify response doesn't contain actual paths
	responseStr := w.Body.String()
	if strings.Contains(responseStr, "/sensitive") {
		t.Error("Response contains sensitive path information")
	}
}

func TestOperationsHandler_GetPaths_NoPathsExposed(t *testing.T) {
	t.Parallel()
	cfg := config.NewServerConfig()
	cfg.StateDir = "/etc/sensitive/state"
	cfg.ProviderDir = "/opt/secret/providers"
	cfg.LogFile = "/var/log/sensitive.log"

	// Create mocks for dependencies
	stateStore := mocks.NewMockStateStore()
	workerPool := mocks.NewWorkerPool(t)
	queue := mocks.NewDeploymentQueue(t)

	handler := NewOperationsHandler(cfg, stateStore, workerPool, queue)

	req := httptest.NewRequest("GET", "/api/v1/system/paths", nil)
	w := httptest.NewRecorder()

	handler.GetPaths(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Check that we get health status, not paths
	responseStr := w.Body.String()
	forbiddenStrings := []string{
		"/etc", "/opt", "/var", "/home", "/usr",
		"sensitive", "secret", ".lattiam", "~",
		cfg.StateDir, cfg.ProviderDir, cfg.LogFile,
	}

	for _, forbidden := range forbiddenStrings {
		if strings.Contains(responseStr, forbidden) {
			t.Errorf("Response contains forbidden path information: %s", forbidden)
		}
	}

	// Verify we get health status instead
	if stateStorage, ok := response["state_storage"].(map[string]interface{}); ok {
		if _, hasPath := stateStorage["path"]; hasPath {
			t.Error("state_storage should not contain 'path' field")
		}
		if _, hasConfigured := stateStorage["configured"]; !hasConfigured {
			t.Error("state_storage should contain 'configured' field")
		}
		if _, hasHealthy := stateStorage["healthy"]; !hasHealthy {
			t.Error("state_storage should contain 'healthy' field")
		}
	} else {
		t.Error("Expected state_storage in response")
	}
}

func TestOperationsHandler_GetDiskUsage_NoPathsExposed(t *testing.T) {
	t.Parallel()
	cfg := config.NewServerConfig()
	cfg.StateDir = "/mnt/data/state"
	cfg.ProviderDir = "/mnt/data/providers"

	// Create mocks for dependencies
	stateStore := mocks.NewMockStateStore()
	workerPool := mocks.NewWorkerPool(t)
	queue := mocks.NewDeploymentQueue(t)

	handler := NewOperationsHandler(cfg, stateStore, workerPool, queue)

	req := httptest.NewRequest("GET", "/api/v1/system/disk-usage", nil)
	w := httptest.NewRecorder()

	handler.GetDiskUsage(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Check response structure
	storage, ok := response["storage"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected 'storage' field in response")
	}

	// Verify storage areas have percentages but no paths
	for name, data := range storage {
		storageInfo, ok := data.(map[string]interface{})
		if !ok {
			continue
		}

		// Should have percentage
		if _, hasPercent := storageInfo["used_percent"]; !hasPercent {
			t.Errorf("Storage area '%s' missing used_percent", name)
		}

		// Should NOT have path information
		pathFields := []string{"path", "directory", "location", "base_dir"}
		for _, field := range pathFields {
			if _, hasField := storageInfo[field]; hasField {
				t.Errorf("Storage area '%s' exposes path information in field '%s'", name, field)
			}
		}
	}

	// Verify no paths in entire response
	responseStr := w.Body.String()
	if strings.Contains(responseStr, "/mnt") || strings.Contains(responseStr, cfg.StateDir) {
		t.Error("Response contains path information")
	}
}

func TestOperationsHandler_GetStorageInfo_FiltersSensitiveData(t *testing.T) {
	t.Parallel()
	cfg := config.NewServerConfig()
	cfg.StateDir = "/secure/state"
	cfg.ProviderDir = "/secure/providers"

	// Create mock state store that returns storage info
	mockStore := &mocks.MockStateStore{}
	mockStore.SetupGetStorageInfo(&interfaces.StorageInfo{
		Type:            "robust_file",
		Exists:          true,
		Writable:        true,
		DeploymentCount: 10,
		TotalSizeBytes:  1024 * 1024, // 1MB
		UsedPercent:     45.5,
	})

	// Create mocks for other dependencies
	workerPool := mocks.NewWorkerPool(t)
	queue := mocks.NewDeploymentQueue(t)

	handler := NewOperationsHandler(cfg, mockStore, workerPool, queue)

	req := httptest.NewRequest("GET", "/api/v1/system/storage", nil)
	w := httptest.NewRecorder()

	handler.GetStorageInfo(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Check state_store info
	if stateStore, ok := response["state_store"].(map[string]interface{}); ok {
		// Should NOT have base_dir or path
		if _, hasBaseDir := stateStore["base_dir"]; hasBaseDir {
			t.Error("state_store should not expose base_dir")
		}
		if _, hasPath := stateStore["path"]; hasPath {
			t.Error("state_store should not expose path")
		}

		// Should still have other info
		if stateStore["type"] != "robust_file" {
			t.Error("state_store should preserve type information")
		}
	}

	// Verify no paths in response
	responseStr := w.Body.String()
	if strings.Contains(responseStr, "/secure") {
		t.Error("Response contains sensitive path information")
	}
}

func TestOperationsHandler_AllEndpoints_ConsistentSecurity(t *testing.T) {
	t.Parallel()
	cfg := config.NewServerConfig()
	cfg.StateDir = "/production/data/state"
	cfg.ProviderDir = "/production/data/providers"
	cfg.LogFile = "/production/logs/server.log"
	cfg.PIDFile = "/production/run/server.pid"

	// Create mocks for dependencies
	stateStore := mocks.NewMockStateStore()
	workerPool := mocks.NewWorkerPool(t)
	queue := mocks.NewDeploymentQueue(t)

	handler := NewOperationsHandler(cfg, stateStore, workerPool, queue)

	endpoints := []string{
		"/api/v1/system/config",
		"/api/v1/system/paths",
		"/api/v1/system/storage",
		"/api/v1/system/runtime",
		"/api/v1/system/disk-usage",
	}

	for _, endpoint := range endpoints {
		t.Run(endpoint, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest("GET", endpoint, nil)
			w := httptest.NewRecorder()

			// Create router to handle the request
			router := chi.NewRouter()
			router.Route("/api/v1", func(r chi.Router) {
				handler.RegisterRoutes(r)
			})
			router.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("Expected status 200 for %s, got %d", endpoint, w.Code)
			}

			// Check response doesn't contain any production paths
			responseStr := w.Body.String()
			forbiddenStrings := []string{
				"/production",
				cfg.StateDir,
				cfg.ProviderDir,
				cfg.LogFile,
				cfg.PIDFile,
				"production/data",
				"production/logs",
				"production/run",
			}

			for _, forbidden := range forbiddenStrings {
				if strings.Contains(responseStr, forbidden) {
					t.Errorf("Endpoint %s exposes forbidden information: %s", endpoint, forbidden)
				}
			}
		})
	}
}

func TestOperationsHandler_GetDiskUsage_AlertsWithoutPaths(t *testing.T) {
	t.Parallel()
	// This test would require mocking syscall.Statfs, which is complex
	// For now, we'll test the alert structure
	cfg := config.NewServerConfig()
	cfg.StateDir = "/data/state"
	cfg.ProviderDir = "/data/providers"

	// Create mocks for dependencies
	stateStore := mocks.NewMockStateStore()
	workerPool := mocks.NewWorkerPool(t)
	queue := mocks.NewDeploymentQueue(t)

	handler := NewOperationsHandler(cfg, stateStore, workerPool, queue)

	req := httptest.NewRequest("GET", "/api/v1/system/disk-usage", nil)
	w := httptest.NewRecorder()

	handler.GetDiskUsage(w, req)

	var response map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Check alerts structure
	if alerts, ok := response["alerts"].([]interface{}); ok {
		for _, alert := range alerts {
			alertMap, ok := alert.(map[string]interface{})
			if !ok {
				continue
			}

			// Alerts should reference storage area, not path
			if _, hasStorage := alertMap["storage"]; !hasStorage {
				t.Error("Alert should have 'storage' field")
			}

			// Alerts should NOT have path field
			if _, hasPath := alertMap["path"]; hasPath {
				t.Error("Alert should not have 'path' field")
			}

			// Check message doesn't contain paths
			if msg, ok := alertMap["message"].(string); ok {
				if strings.Contains(msg, "/data") {
					t.Error("Alert message contains path information")
				}
			}
		}
	}
}

func TestOperationsHandler_GetStorageInfo_NoPathsLeaked(t *testing.T) { //nolint:funlen // Test function with comprehensive test cases
	t.Parallel()
	cfg := config.NewServerConfig()
	cfg.StateDir = "/sensitive/production/state"
	cfg.ProviderDir = "/sensitive/production/providers"

	// Create mock state store that returns sensitive paths
	mockStore := &mocks.MockStateStore{}
	mockStore.SetupGetStorageInfo(&interfaces.StorageInfo{
		Type:            "file",
		Exists:          true,
		Writable:        true,
		DeploymentCount: 10,
		TotalSizeBytes:  2048000,
		UsedPercent:     40.0,
	})

	// Create mocks for other dependencies
	workerPool := mocks.NewWorkerPool(t)
	queue := mocks.NewDeploymentQueue(t)

	handler := NewOperationsHandler(cfg, mockStore, workerPool, queue)

	req := httptest.NewRequest("GET", "/api/v1/system/storage", nil)
	w := httptest.NewRecorder()

	handler.GetStorageInfo(w, req)

	// Verify successful response
	if w.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", w.Code)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	responseStr := w.Body.String()

	// Verify no paths are exposed
	forbiddenStrings := []string{
		"/sensitive",
		"production",
		"deployments_path",
		"base_dir",
		"path",
		cfg.StateDir,
		cfg.ProviderDir,
	}

	for _, forbidden := range forbiddenStrings {
		if strings.Contains(responseStr, forbidden) {
			t.Errorf("Response should not contain sensitive string: '%s'\nResponse: %s", forbidden, responseStr)
		}
	}

	// Verify allowed fields are present
	stateStore, ok := response["state_store"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected state_store in response")
	}

	// Check whitelisted fields are present
	if stateStore["type"] != "file" {
		t.Error("Expected type=file in state_store")
	}
	if stateStore["exists"] != true {
		t.Error("Expected exists=true in state_store")
	}
	if stateStore["writable"] != true {
		t.Error("Expected writable=true in state_store")
	}
	if stateStore["deployment_count"] != float64(10) {
		t.Error("Expected deployment_count=10 in state_store")
	}

	// Check disk_usage only has percentage
	if diskUsage, ok := stateStore["disk_usage"].(map[string]interface{}); ok {
		if len(diskUsage) != 1 {
			t.Errorf("disk_usage should only contain used_percent, got %d fields", len(diskUsage))
		}
		if percent, ok := diskUsage["used_percent"]; !ok || percent != 40.0 {
			t.Error("Expected disk_usage to contain only used_percent=40.0")
		}
	}
}
