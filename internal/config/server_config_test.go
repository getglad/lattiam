package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewServerConfig(t *testing.T) {
	t.Parallel()
	cfg := NewServerConfig()

	// Test default values
	if cfg.Port != 8080 {
		t.Errorf("Expected default port 8080, got %d", cfg.Port)
	}
	if cfg.Debug != false {
		t.Errorf("Expected default debug false, got %v", cfg.Debug)
	}
	if cfg.StateStore.Type != "file" {
		t.Errorf("Expected default state store type 'file', got %s", cfg.StateStore.Type)
	}
}

func TestLoadFromEnv(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv()

	// Set test environment variables using t.Setenv for automatic cleanup
	t.Setenv("LATTIAM_PORT", "9090")
	t.Setenv("LATTIAM_DEBUG", "true")
	t.Setenv("LATTIAM_STATE_DIR", "/custom/state")
	t.Setenv("LATTIAM_LOG_FILE", "/custom/log.log")
	t.Setenv("LATTIAM_PROVIDER_DIR", "/custom/providers")

	cfg := NewServerConfig()
	if err := cfg.LoadFromEnv(); err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}

	// Verify values loaded from environment
	if cfg.Port != 9090 {
		t.Errorf("Expected port 9090 from env, got %d", cfg.Port)
	}
	if cfg.Debug != true {
		t.Errorf("Expected debug true from env, got %v", cfg.Debug)
	}
	if cfg.StateDir != "/custom/state" {
		t.Errorf("Expected state dir '/custom/state' from env, got %s", cfg.StateDir)
	}
	if cfg.LogFile != "/custom/log.log" {
		t.Errorf("Expected log file '/custom/log.log' from env, got %s", cfg.LogFile)
	}
	if cfg.ProviderDir != "/custom/providers" {
		t.Errorf("Expected provider dir '/custom/providers' from env, got %s", cfg.ProviderDir)
	}
}

func TestExpandPaths(t *testing.T) {
	t.Parallel()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("Cannot get home directory: %v", err)
	}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "home directory expansion",
			input:    "~/test",
			expected: filepath.Join(home, "test"),
		},
		{
			name:     "absolute path unchanged",
			input:    "/absolute/path",
			expected: "/absolute/path",
		},
		{
			name:     "relative path unchanged",
			input:    "relative/path",
			expected: "relative/path",
		},
		{
			name:     "empty path unchanged",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := NewServerConfig()
			cfg.StateDir = tt.input

			if err := cfg.ExpandPaths(); err != nil {
				t.Fatalf("ExpandPaths failed: %v", err)
			}

			if cfg.StateDir != tt.expected {
				t.Errorf("Expected expanded path %s, got %s", tt.expected, cfg.StateDir)
			}
		})
	}
}

func TestValidate(t *testing.T) { //nolint:funlen // Test function with comprehensive test cases
	t.Parallel()
	tests := []struct {
		name      string
		setupFunc func(*ServerConfig)
		wantErr   bool
		errMsg    string
	}{
		{
			name:      "valid config",
			setupFunc: func(_ *ServerConfig) {},
			wantErr:   false,
		},
		{
			name: "invalid port - negative",
			setupFunc: func(cfg *ServerConfig) {
				cfg.Port = -1
			},
			wantErr: true,
			errMsg:  "invalid port",
		},
		{
			name: "invalid port - too high",
			setupFunc: func(cfg *ServerConfig) {
				cfg.Port = 70000
			},
			wantErr: true,
			errMsg:  "invalid port",
		},
		{
			name: "empty state directory",
			setupFunc: func(cfg *ServerConfig) {
				cfg.StateDir = ""
			},
			wantErr: true,
			errMsg:  "state directory cannot be empty",
		},
		{
			name: "empty provider directory",
			setupFunc: func(cfg *ServerConfig) {
				cfg.ProviderDir = ""
			},
			wantErr: true,
			errMsg:  "provider directory cannot be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := NewServerConfig()
			tt.setupFunc(cfg)

			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}

			if err != nil && tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("Expected error containing '%s', got '%s'", tt.errMsg, err.Error())
			}
		})
	}
}

func TestGetSanitized(t *testing.T) {
	t.Parallel()
	cfg := NewServerConfig()
	cfg.Port = 9090
	cfg.Debug = false
	cfg.StateDir = "/sensitive/state/path"
	cfg.LogFile = "/sensitive/log/path"
	cfg.ProviderDir = "/sensitive/provider/path"
	cfg.PIDFile = "/sensitive/pid/file"

	sanitized := cfg.GetSanitized()

	// Check non-sensitive fields are present
	if port, ok := sanitized["port"].(int); !ok || port != 9090 {
		t.Errorf("Expected port 9090 in sanitized config, got %v", sanitized["port"])
	}
	if debug, ok := sanitized["debug"].(bool); !ok || debug != false {
		t.Errorf("Expected debug false in sanitized config, got %v", sanitized["debug"])
	}

	// Check sensitive paths are NOT present when debug is false
	sensitiveFields := []string{"state_dir", "log_file", "provider_dir", "pid_file", "state_path"}
	for _, field := range sensitiveFields {
		if _, exists := sanitized[field]; exists {
			t.Errorf("Sensitive field '%s' should not be in sanitized config when debug=false", field)
		}
	}

	// Test with debug mode
	cfg.Debug = true
	sanitized = cfg.GetSanitized()

	// In debug mode, we should see configuration status but not actual paths
	if stateConfigured, ok := sanitized["state_configured"].(bool); !ok || !stateConfigured {
		t.Errorf("Expected state_configured=true in debug mode, got %v", sanitized["state_configured"])
	}

	// Even in debug mode, actual paths should NOT be exposed
	for _, field := range sensitiveFields {
		if _, exists := sanitized[field]; exists {
			t.Errorf("Sensitive field '%s' should not be in sanitized config even in debug mode", field)
		}
	}
}

func TestGetLogPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		logFile    string
		daemonMode bool
		expected   string
	}{
		{
			name:       "explicit log file",
			logFile:    "/custom/log.log",
			daemonMode: false,
			expected:   "/custom/log.log",
		},
		{
			name:       "daemon mode default",
			logFile:    "",
			daemonMode: true,
			expected:   filepath.Join(os.TempDir(), "lattiam-server.log"),
		},
		{
			name:       "foreground mode default",
			logFile:    "",
			daemonMode: false,
			expected:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := NewServerConfig()
			cfg.LogFile = tt.logFile
			cfg.DaemonMode = tt.daemonMode

			result := cfg.GetLogPath()
			if result != tt.expected {
				t.Errorf("Expected log path '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestWriteConfigInfo(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv()

	// Create temp directory for test
	tmpDir := t.TempDir()
	testInfoPath := filepath.Join(tmpDir, "lattiam.info")

	// Use t.Setenv for automatic cleanup
	t.Setenv("LATTIAM_INFO_FILE", testInfoPath)

	cfg := NewServerConfig()
	cfg.Port = 8085
	cfg.Debug = true
	cfg.StateDir = "/test/state"

	// Write config info
	if err := cfg.WriteConfigInfo(); err != nil {
		t.Fatalf("WriteConfigInfo failed: %v", err)
	}

	// Read and verify the file
	infoPath := testInfoPath
	data, err := os.ReadFile(infoPath) // #nosec G304 - Test file with controlled path
	if err != nil {
		t.Fatalf("Failed to read config info file: %v", err)
	}

	var info map[string]interface{}
	if err := json.Unmarshal(data, &info); err != nil {
		t.Fatalf("Failed to parse config info: %v", err)
	}

	// Verify structure
	if _, ok := info["started_at"].(string); !ok {
		t.Error("Expected started_at field in config info")
	}
	if _, ok := info["pid"].(float64); !ok {
		t.Error("Expected pid field in config info")
	}
	if _, ok := info["version"].(string); !ok {
		t.Error("Expected version field in config info")
	}

	// Verify config is sanitized
	configData, ok := info["config"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected config field in config info")
	}

	// Should have non-sensitive fields
	if port, ok := configData["port"].(float64); !ok || int(port) != 8085 {
		t.Errorf("Expected port 8085 in config info, got %v", configData["port"])
	}

	// Should NOT have sensitive paths
	if _, exists := configData["state_dir"]; exists {
		t.Error("Config info should not contain state_dir")
	}
}

func TestEnvironmentVariablePrecedence(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv()
	// Test that environment variables override defaults
	t.Setenv("LATTIAM_PORT", "7777")

	cfg := NewServerConfig()
	if err := cfg.LoadFromEnv(); err != nil {
		t.Fatalf("LoadFromEnv failed: %v", err)
	}

	if cfg.Port != 7777 {
		t.Errorf("Environment variable should override default, expected 7777, got %d", cfg.Port)
	}
}

func TestInvalidEnvironmentVariables(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv()
	tests := []struct {
		name    string
		envVar  string
		value   string
		wantErr bool
	}{
		{
			name:    "invalid port",
			envVar:  "LATTIAM_PORT",
			value:   "not-a-number",
			wantErr: true,
		},
		{
			name:    "invalid debug",
			envVar:  "LATTIAM_DEBUG",
			value:   "not-a-bool",
			wantErr: true,
		},
		{
			name:    "valid string path",
			envVar:  "LATTIAM_STATE_DIR",
			value:   "/valid/path",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Cannot use t.Parallel() with t.Setenv()
			// Use t.Setenv for automatic cleanup
			t.Setenv(tt.envVar, tt.value)

			cfg := NewServerConfig()
			err := cfg.LoadFromEnv()

			if (err != nil) != tt.wantErr {
				t.Errorf("LoadFromEnv() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
