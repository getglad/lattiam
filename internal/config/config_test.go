package config

import (
	"os"
	"testing"
	"time"
)

func TestLoadConfig(t *testing.T) {
	tests := []struct {
		name     string
		envVars  map[string]string
		validate func(*testing.T, *Config)
	}{
		{
			name:    "default values",
			envVars: map[string]string{},
			validate: func(t *testing.T, cfg *Config) {
				t.Helper()
				if cfg.ProviderTimeout != 2*time.Minute {
					t.Errorf("expected ProviderTimeout to be 2m, got %v", cfg.ProviderTimeout)
				}
				if cfg.ProviderStartupTimeout != 30*time.Second {
					t.Errorf("expected ProviderStartupTimeout to be 30s, got %v", cfg.ProviderStartupTimeout)
				}
				if cfg.DeploymentExecutionTimeout != 5*time.Minute {
					t.Errorf("expected DeploymentExecutionTimeout to be 5m, got %v", cfg.DeploymentExecutionTimeout)
				}
			},
		},
		{
			name: "custom provider timeout",
			envVars: map[string]string{
				"LATTIAM_PROVIDER_TIMEOUT": "10m",
			},
			validate: func(t *testing.T, cfg *Config) {
				t.Helper()
				if cfg.ProviderTimeout != 10*time.Minute {
					t.Errorf("expected ProviderTimeout to be 10m, got %v", cfg.ProviderTimeout)
				}
			},
		},
		{
			name: "custom startup timeout",
			envVars: map[string]string{
				"LATTIAM_PROVIDER_STARTUP_TIMEOUT": "1m",
			},
			validate: func(t *testing.T, cfg *Config) {
				t.Helper()
				if cfg.ProviderStartupTimeout != 1*time.Minute {
					t.Errorf("expected ProviderStartupTimeout to be 1m, got %v", cfg.ProviderStartupTimeout)
				}
			},
		},
		{
			name: "custom deployment timeout",
			envVars: map[string]string{
				"LATTIAM_DEPLOYMENT_TIMEOUT": "15m",
			},
			validate: func(t *testing.T, cfg *Config) {
				t.Helper()
				if cfg.DeploymentExecutionTimeout != 15*time.Minute {
					t.Errorf("expected DeploymentExecutionTimeout to be 15m, got %v", cfg.DeploymentExecutionTimeout)
				}
			},
		},
		{
			name: "AWS profile increases timeout",
			envVars: map[string]string{
				"AWS_PROFILE": "test-profile",
			},
			validate: func(t *testing.T, cfg *Config) {
				t.Helper()
				if cfg.ProviderTimeout != 5*time.Minute {
					t.Errorf("expected ProviderTimeout to be 5m with AWS profile, got %v", cfg.ProviderTimeout)
				}
			},
		},
		{
			name: "invalid timeout format ignored",
			envVars: map[string]string{
				"LATTIAM_PROVIDER_TIMEOUT": "invalid",
			},
			validate: func(t *testing.T, cfg *Config) {
				t.Helper()
				if cfg.ProviderTimeout != 2*time.Minute {
					t.Errorf("expected ProviderTimeout to be default 2m, got %v", cfg.ProviderTimeout)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear environment
			os.Clearenv()

			// Set test environment variables
			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			// Load config
			cfg := LoadConfig()

			// Validate
			tt.validate(t, cfg)
		})
	}
}

func TestGetProviderTimeout(t *testing.T) {
	tests := []struct {
		name         string
		providerName string
		envVars      map[string]string
		expected     time.Duration
	}{
		{
			name:         "default timeout",
			providerName: "google",
			envVars:      map[string]string{},
			expected:     2 * time.Minute,
		},
		{
			name:         "AWS default timeout",
			providerName: "aws",
			envVars:      map[string]string{},
			expected:     5 * time.Minute,
		},
		{
			name:         "provider-specific timeout",
			providerName: "aws",
			envVars: map[string]string{
				"LATTIAM_PROVIDER_TIMEOUT_aws": "10m",
			},
			expected: 10 * time.Minute,
		},
		{
			name:         "global timeout override",
			providerName: "azure",
			envVars: map[string]string{
				"LATTIAM_PROVIDER_TIMEOUT": "3m",
			},
			expected: 3 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear environment
			os.Clearenv()

			// Set test environment variables
			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			// Load config and get provider timeout
			cfg := LoadConfig()
			timeout := cfg.GetProviderTimeout(tt.providerName)

			if timeout != tt.expected {
				t.Errorf("expected timeout %v, got %v", tt.expected, timeout)
			}
		})
	}
}

func TestGetHTTPClientTimeout(t *testing.T) {
	tests := []struct {
		name     string
		envValue string
		expected time.Duration
	}{
		{
			name:     "no debug mode",
			envValue: "",
			expected: 5 * time.Minute,
		},
		{
			name:     "debug mode true",
			envValue: "true",
			expected: 10 * time.Minute,
		},
		{
			name:     "debug mode 1",
			envValue: "1",
			expected: 10 * time.Minute,
		},
		{
			name:     "debug mode false",
			envValue: "false",
			expected: 5 * time.Minute,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				t.Setenv("LATTIAM_DEBUG", tt.envValue)
			}

			timeout := GetHTTPClientTimeout()

			if timeout != tt.expected {
				t.Errorf("expected timeout %v, got %v", tt.expected, timeout)
			}
		})
	}
}
