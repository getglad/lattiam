package helpers

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
)

const (
	// DefaultLocalStackURL is the default endpoint for LocalStack.
	DefaultLocalStackURL = "http://localstack:4566"
)

// Static errors for err113 compliance
var (
	ErrLocalStackNotAvailable = errors.New("LocalStack not available after retries")
)

// TestMode defines the testing environment mode
type TestMode string

const (
	// TestModeLocalStack uses LocalStack for testing
	TestModeLocalStack TestMode = "localstack"
	// TestModeAWS uses real AWS services for testing
	TestModeAWS TestMode = "aws"
)

// TestConfig holds configuration for test execution
type TestConfig struct {
	Mode           TestMode
	AWSRegion      string
	TestTimeout    time.Duration
	CleanupEnabled bool
	ResourcePrefix string
}

// GetTestMode returns the current test mode from environment variables
func GetTestMode() TestMode {
	mode := strings.ToLower(os.Getenv("LATTIAM_TEST_MODE"))
	switch mode {
	case "aws", "real":
		return TestModeAWS
	case "localstack", "local":
		return TestModeLocalStack
	default:
		// Default to LocalStack for safety
		return TestModeLocalStack
	}
}

// NewTestConfig creates a test configuration
func NewTestConfig() *TestConfig {
	mode := GetTestMode()

	config := &TestConfig{
		Mode:           mode,
		AWSRegion:      getEnvOrDefault("AWS_REGION", "us-east-1"),
		TestTimeout:    10 * time.Minute,
		CleanupEnabled: getEnvOrDefault("LATTIAM_TEST_CLEANUP", "true") == "true",
		ResourcePrefix: getEnvOrDefault("LATTIAM_TEST_PREFIX", "lattiam-test"),
	}

	// Adjust timeout based on mode
	if mode == TestModeAWS {
		config.TestTimeout = 5 * time.Minute
	}

	return config
}

// ApplyLocalStackEnvConfig sets up LocalStack environment variables and waits for it to be ready
func (tc *TestConfig) ApplyLocalStackEnvConfig(t TestingT) {
	// Set a default LocalStack endpoint if not provided or if the hostname is empty.
	// This handles cases where AWS_ENDPOINT_URL might be set to http://:4566 due to
	// hostname resolution issues.
	endpointURL := os.Getenv("AWS_ENDPOINT_URL")
	if endpointURL == "" || strings.Contains(endpointURL, "://:") {
		os.Setenv("AWS_ENDPOINT_URL", DefaultLocalStackURL)
		endpointURL = DefaultLocalStackURL
	}
	t.Logf("Configuring LocalStack environment. AWS_ENDPOINT_URL: %s", endpointURL)

	// Set LocalStack-specific environment variables if not already set
	tc.setEnvIfNotSet(t, "AWS_ACCESS_KEY_ID", "test")
	tc.setEnvIfNotSet(t, "AWS_SECRET_ACCESS_KEY", "test")
	tc.setEnvIfNotSet(t, "AWS_REGION", tc.AWSRegion)
	tc.setEnvIfNotSet(t, "AWS_DEFAULT_REGION", tc.AWSRegion)

	// Disable SSL verification for LocalStack
	tc.setEnvIfNotSet(t, "AWS_DISABLE_SSL", "true")

	// Skip metadata checks for LocalStack
	tc.setEnvIfNotSet(t, "AWS_SKIP_METADATA_API_CHECK", "true")

	// Ensure NO_PROXY includes localhost for LocalStack connections
	tc.ensureNoProxyLocalhost(t)

	// Wait for LocalStack to be ready
	ctx, cancel := context.WithTimeout(context.Background(), LocalStackReadyTimeout)
	defer cancel()

	if err := tc.WaitForLocalStack(ctx); err != nil {
		t.Fatalf("LocalStack not ready: %v", err)
	}
}

// setEnvIfNotSet sets an environment variable only if it's not already set
func (tc *TestConfig) setEnvIfNotSet(t TestingT, key, value string) {
	if os.Getenv(key) == "" {
		os.Setenv(key, value)
		t.Logf("Set %s=%s", key, value)
	}
}

// ensureNoProxyLocalhost ensures NO_PROXY includes localhost for LocalStack
func (tc *TestConfig) ensureNoProxyLocalhost(t TestingT) {
	noProxy := os.Getenv("NO_PROXY")
	if noProxy == "" {
		os.Setenv("NO_PROXY", "localhost,127.0.0.1,localstack,devcontainer-localstack-1")
		t.Logf("Set NO_PROXY=localhost,127.0.0.1,localstack,devcontainer-localstack-1")
	} else if noProxy != "*" {
		// Add localhost entries if not present
		needsUpdate := false
		entries := []string{"localhost", "127.0.0.1", "localstack", "devcontainer-localstack-1"}
		for _, entry := range entries {
			if !contains(noProxy, entry) {
				noProxy += "," + entry
				needsUpdate = true
			}
		}
		if needsUpdate {
			os.Setenv("NO_PROXY", noProxy)
			t.Logf("Updated NO_PROXY=%s", noProxy)
		}
	}
}

// IsAWS returns true if running in real AWS mode
func (tc *TestConfig) IsAWS() bool {
	return tc.Mode == TestModeAWS
}

// IsLocalStack returns true if running against LocalStack
func (tc *TestConfig) IsLocalStack() bool {
	return tc.Mode == TestModeLocalStack
}

// GetAWSConfig returns an AWS configuration for the current test mode
func (tc *TestConfig) GetAWSConfig(ctx context.Context) (aws.Config, error) {
	if tc.IsLocalStack() {
		return tc.getLocalStackConfig(ctx)
	}
	return tc.getRealAWSConfig(ctx)
}

// getLocalStackConfig returns AWS config pointing to LocalStack
func (tc *TestConfig) getLocalStackConfig(ctx context.Context) (aws.Config, error) {
	endpointURL := os.Getenv("AWS_ENDPOINT_URL")
	if endpointURL == "" {
		return aws.Config{}, errors.New("AWS_ENDPOINT_URL not set for LocalStack mode")
	}

	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(tc.AWSRegion),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
		// Use the new BaseEndpoint approach for custom endpoints
		awsconfig.WithBaseEndpoint(endpointURL),
	)
	if err != nil {
		return aws.Config{}, fmt.Errorf("failed to load LocalStack config: %w", err)
	}

	return cfg, nil
}

// getRealAWSConfig returns standard AWS config for real AWS
func (tc *TestConfig) getRealAWSConfig(ctx context.Context) (aws.Config, error) {
	// Check if AWS_ENDPOINT_URL is set (useful for using LocalStack with AWS mode)
	if endpointURL := os.Getenv("AWS_ENDPOINT_URL"); endpointURL != "" {
		cfg, err := awsconfig.LoadDefaultConfig(ctx,
			awsconfig.WithRegion(tc.AWSRegion),
			// Use the new BaseEndpoint approach for custom endpoints
			awsconfig.WithBaseEndpoint(endpointURL),
		)
		if err != nil {
			return aws.Config{}, fmt.Errorf("failed to load AWS config with custom endpoint: %w", err)
		}
		return cfg, nil
	}

	// Standard AWS configuration
	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(tc.AWSRegion),
	)
	if err != nil {
		return aws.Config{}, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return cfg, nil
}

// GenerateTestResourceName creates a unique resource name for testing
func GenerateTestResourceName(resourceType string) string {
	// Use nanoseconds for better uniqueness in concurrent scenarios
	timestamp := time.Now().UnixNano()
	// Convert to a more readable format (last 10 digits of nano + random component)
	uniqueID := strconv.FormatInt(timestamp%10000000000, 10)
	return fmt.Sprintf("lattiam-test-%s-%s", resourceType, uniqueID)
}

// ShouldSkipTest returns true if the test should be skipped based on mode
func (tc *TestConfig) ShouldSkipTest(requiredMode TestMode, t TestingT) bool {
	if tc.Mode != requiredMode {
		t.Skipf("Test requires %s mode, but running in %s mode", requiredMode, tc.Mode)
		return true
	}
	return false
}

// TestingT interface for compatibility with testing.T
type TestingT interface {
	Skipf(format string, args ...interface{})
	Logf(format string, args ...interface{})
	Errorf(format string, args ...interface{})
	Fatalf(format string, args ...interface{})
	FailNow()
}

// WaitForLocalStack waits for LocalStack to be available
func (tc *TestConfig) WaitForLocalStack(ctx context.Context) error {
	if !tc.IsLocalStack() {
		return nil // No need to wait for real AWS
	}

	endpointURL := os.Getenv("AWS_ENDPOINT_URL")
	if endpointURL == "" {
		return errors.New("AWS_ENDPOINT_URL not set for LocalStack mode, cannot wait for LocalStack")
	}

	// Log which endpoint we're using
	// Waiting for LocalStack

	// Use HTTP health check for LocalStack readiness
	client := &http.Client{Timeout: 2 * time.Second}
	maxRetries := 30
	for i := 0; i < maxRetries; i++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpointURL+"/_localstack/health", http.NoBody)
		if err != nil {
			log.Printf("Error creating LocalStack health check request: %v", err)
			time.Sleep(1 * time.Second)
			continue
		}
		resp, err := client.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			_ = resp.Body.Close()
			log.Printf("LocalStack is ready at %s", endpointURL)
			return nil // LocalStack is ready
		}
		if resp != nil {
			_ = resp.Body.Close()
		}
		log.Printf("Waiting for LocalStack at %s (attempt %d/%d)", endpointURL, i+1, maxRetries)
		time.Sleep(1 * time.Second)
	}

	return fmt.Errorf("%w at %s after %d retries", ErrLocalStackNotAvailable, endpointURL, maxRetries)
}

// getEnvOrDefault returns environment variable value or default
func getEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// contains checks if a string contains a substring (helper function)
func contains(s, substr string) bool {
	return s != "" && substr != "" &&
		(s == substr ||
			s[:len(substr)+1] == substr+"," ||
			s[len(s)-len(substr)-1:] == ","+substr ||
			len(s) > len(substr)+1 && s[len(s)-len(substr):] == substr)
}
