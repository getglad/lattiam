// Package testconfig provides test configuration utilities without importing application code.
// This avoids import cycles that would occur if these utilities were in tests/helpers.
package testconfig

import (
	"context"
	"os"
	"testing"
	"time"
)

// GetLocalStackEndpoint returns the appropriate LocalStack endpoint based on environment
func GetLocalStackEndpoint() string {
	// First check if explicitly set (by testcontainers or other setup)
	if endpoint := os.Getenv("LOCALSTACK_ENDPOINT"); endpoint != "" {
		return endpoint
	}
	if endpoint := os.Getenv("AWS_ENDPOINT_URL"); endpoint != "" {
		return endpoint
	}

	// Default to localhost (testcontainers will set the actual endpoint)
	return "http://localhost:4566"
}

const (
	// DefaultLocalStackURL is the default endpoint for LocalStack (dynamically determined)
	// Use GetLocalStackEndpoint() for dynamic resolution
	DefaultLocalStackURL = "http://localstack:4566"

	// LocalStackReadyTimeout is how long to wait for LocalStack to be ready
	LocalStackReadyTimeout = 30 * time.Second
)

const trueValue = "true"

// ShouldUseLocalStack returns true if tests should use LocalStack
func ShouldUseLocalStack() bool {
	return os.Getenv("LATTIAM_USE_LOCALSTACK") != "false" &&
		(os.Getenv("LATTIAM_TEST_MODE") == trueValue ||
			os.Getenv("LATTIAM_TEST_MODE") == "localstack")
}

// ShouldWaitForLocalStack returns true if tests should wait for LocalStack to be ready
func ShouldWaitForLocalStack() bool {
	// Only wait if we're in a CI environment or explicitly told to
	return os.Getenv("CI") == trueValue || os.Getenv("LATTIAM_WAIT_FOR_LOCALSTACK") == trueValue
}

// WaitForLocalStack waits for LocalStack to be available
func WaitForLocalStack(_ context.Context) error {
	// For now, just return nil
	// In a real implementation, this would check LocalStack health endpoint
	return nil
}

// SkipIfNoLocalStack skips the test if LocalStack is not available
func SkipIfNoLocalStack(tb testing.TB) {
	tb.Helper()

	if !ShouldUseLocalStack() {
		tb.Skip("Skipping LocalStack test - LATTIAM_USE_LOCALSTACK not set")
	}

	// Check if endpoint is set
	if os.Getenv("AWS_ENDPOINT_URL") == "" {
		tb.Skip("Skipping LocalStack test - AWS_ENDPOINT_URL not set")
	}
}
