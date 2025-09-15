//go:build !integration
// +build !integration

package config

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test constants
const (
	defaultProviderTimeout            = 2 * time.Minute
	defaultProviderStartupTimeout     = 30 * time.Second
	defaultDeploymentExecutionTimeout = 5 * time.Minute
	awsProviderTimeout                = 5 * time.Minute
	debuggingHTTPTimeout              = 10 * time.Minute
	normalHTTPTimeout                 = 5 * time.Minute
)

func TestLoadConfig(t *testing.T) { //nolint:funlen // Test function with comprehensive test cases
	// Cannot use t.Parallel() with t.Setenv
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
				// AWS environment variables may be set, affecting the default timeout
				expectedTimeout := defaultProviderTimeout
				if os.Getenv("AWS_PROFILE") != "" || os.Getenv("AWS_ACCESS_KEY_ID") != "" {
					expectedTimeout = awsProviderTimeout
				}
				assert.Equal(t, expectedTimeout, cfg.ProviderTimeout)
				assert.Equal(t, defaultProviderStartupTimeout, cfg.ProviderStartupTimeout)
				assert.Equal(t, defaultDeploymentExecutionTimeout, cfg.DeploymentExecutionTimeout)
			},
		},
		{
			name: "custom provider timeout",
			envVars: map[string]string{
				"LATTIAM_PROVIDER_TIMEOUT": "10m",
			},
			validate: func(t *testing.T, cfg *Config) {
				t.Helper()
				assert.Equal(t, 10*time.Minute, cfg.ProviderTimeout)
			},
		},
		{
			name: "custom startup timeout",
			envVars: map[string]string{
				"LATTIAM_PROVIDER_STARTUP_TIMEOUT": "1m",
			},
			validate: func(t *testing.T, cfg *Config) {
				t.Helper()
				assert.Equal(t, 1*time.Minute, cfg.ProviderStartupTimeout)
			},
		},
		{
			name: "custom deployment timeout",
			envVars: map[string]string{
				"LATTIAM_DEPLOYMENT_TIMEOUT": "15m",
			},
			validate: func(t *testing.T, cfg *Config) {
				t.Helper()
				assert.Equal(t, 15*time.Minute, cfg.DeploymentExecutionTimeout)
			},
		},
		{
			name: "AWS profile increases timeout",
			envVars: map[string]string{
				"AWS_PROFILE": "test-profile",
			},
			validate: func(t *testing.T, cfg *Config) {
				t.Helper()
				assert.Equal(t, awsProviderTimeout, cfg.ProviderTimeout)
			},
		},
		{
			name: "invalid timeout format ignored",
			envVars: map[string]string{
				"LATTIAM_PROVIDER_TIMEOUT": "invalid",
			},
			validate: func(t *testing.T, cfg *Config) {
				t.Helper()
				// AWS environment variables may be set, affecting the default timeout
				expectedTimeout := defaultProviderTimeout
				if os.Getenv("AWS_PROFILE") != "" || os.Getenv("AWS_ACCESS_KEY_ID") != "" {
					expectedTimeout = awsProviderTimeout
				}
				assert.Equal(t, expectedTimeout, cfg.ProviderTimeout)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set test environment variables
			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			// Load config
			cfg := LoadConfig()
			require.NotNil(t, cfg)

			// Validate
			tt.validate(t, cfg)
		})
	}
}

func TestGetProviderTimeout(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv
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
			expected: func() time.Duration {
				// AWS environment variables may affect base timeout
				if os.Getenv("AWS_PROFILE") != "" || os.Getenv("AWS_ACCESS_KEY_ID") != "" {
					return awsProviderTimeout
				}
				return defaultProviderTimeout
			}(),
		},
		{
			name:         "AWS default timeout",
			providerName: "aws",
			envVars:      map[string]string{},
			expected:     awsProviderTimeout,
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
			expected: func() time.Duration {
				// When AWS env vars are set, they override even explicit LATTIAM_PROVIDER_TIMEOUT to min 5m
				if os.Getenv("AWS_PROFILE") != "" || os.Getenv("AWS_ACCESS_KEY_ID") != "" {
					return awsProviderTimeout
				}
				return 3 * time.Minute
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set test environment variables
			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			// Load config and get provider timeout
			cfg := LoadConfig()
			require.NotNil(t, cfg)
			timeout := cfg.GetProviderTimeout(tt.providerName)

			assert.Equal(t, tt.expected, timeout)
		})
	}
}

func TestGetHTTPClientTimeout(t *testing.T) {
	// Cannot use t.Parallel() with t.Setenv
	tests := []struct {
		name     string
		envValue string
		expected time.Duration
	}{
		{
			name:     "no debug mode",
			envValue: "",
			expected: normalHTTPTimeout,
		},
		{
			name:     "debug mode true",
			envValue: "true",
			expected: debuggingHTTPTimeout,
		},
		{
			name:     "debug mode 1",
			envValue: "1",
			expected: debuggingHTTPTimeout,
		},
		{
			name:     "debug mode false",
			envValue: "false",
			expected: normalHTTPTimeout,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				t.Setenv("LATTIAM_DEBUG", tt.envValue)
			}

			timeout := GetHTTPClientTimeout()

			assert.Equal(t, tt.expected, timeout)
		})
	}
}
