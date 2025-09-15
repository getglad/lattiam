package config

import (
	"os"
	"strconv"
	"time"
)

const (
	awsProviderName = "aws"
)

// Config holds configuration for the application
type Config struct {
	// Provider timeout configuration
	ProviderTimeout            time.Duration
	ProviderStartupTimeout     time.Duration
	DeploymentExecutionTimeout time.Duration
}

// LoadConfig loads configuration from environment variables
func LoadConfig() *Config {
	cfg := &Config{
		// Default timeouts
		ProviderTimeout:            2 * time.Minute,
		ProviderStartupTimeout:     30 * time.Second,
		DeploymentExecutionTimeout: 5 * time.Minute,
	}

	// Load provider timeout from LATTIAM_PROVIDER_TIMEOUT
	if timeout := os.Getenv("LATTIAM_PROVIDER_TIMEOUT"); timeout != "" {
		if duration, err := time.ParseDuration(timeout); err == nil {
			cfg.ProviderTimeout = duration
		}
	}

	// Load provider startup timeout
	if timeout := os.Getenv("LATTIAM_PROVIDER_STARTUP_TIMEOUT"); timeout != "" {
		if duration, err := time.ParseDuration(timeout); err == nil {
			cfg.ProviderStartupTimeout = duration
		}
	}

	// Load deployment execution timeout
	if timeout := os.Getenv("LATTIAM_DEPLOYMENT_TIMEOUT"); timeout != "" {
		if duration, err := time.ParseDuration(timeout); err == nil {
			cfg.DeploymentExecutionTimeout = duration
		}
	}

	// AWS provider gets longer default timeout
	if os.Getenv("AWS_PROFILE") != "" || os.Getenv("AWS_ACCESS_KEY_ID") != "" {
		if cfg.ProviderTimeout < 5*time.Minute {
			cfg.ProviderTimeout = 5 * time.Minute
		}
	}

	return cfg
}

// GetProviderTimeout returns the timeout for a specific provider
func (c *Config) GetProviderTimeout(providerName string) time.Duration {
	// Check for provider-specific timeout
	envKey := "LATTIAM_PROVIDER_TIMEOUT_" + providerName
	if timeout := os.Getenv(envKey); timeout != "" {
		if duration, err := time.ParseDuration(timeout); err == nil {
			return duration
		}
	}

	// AWS gets a longer default timeout
	if providerName == awsProviderName && c.ProviderTimeout < 5*time.Minute {
		return 5 * time.Minute
	}

	return c.ProviderTimeout
}

// GetHTTPClientTimeout returns timeout for HTTP client based on debug mode
func GetHTTPClientTimeout() time.Duration {
	// Check if LATTIAM_DEBUG is set to a truthy value
	debug := os.Getenv("LATTIAM_DEBUG")
	if debug != "" {
		// Parse as bool
		if b, err := strconv.ParseBool(debug); err == nil && b {
			return 10 * time.Minute
		}
		// Also accept "1" as true
		if debug == "1" {
			return 10 * time.Minute
		}
	}
	return 5 * time.Minute
}
