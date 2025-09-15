package testconfig

import (
	"testing"
)

// TestGetLocalStackEndpoint tests the LocalStack endpoint resolution
func TestGetLocalStackEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		envVars  map[string]string
		expected string
	}{
		{
			name:     "uses LOCALSTACK_ENDPOINT if set",
			envVars:  map[string]string{"LOCALSTACK_ENDPOINT": "http://custom:4566"},
			expected: "http://custom:4566",
		},
		{
			name:     "falls back to AWS_ENDPOINT_URL",
			envVars:  map[string]string{"AWS_ENDPOINT_URL": "http://aws:4566"},
			expected: "http://aws:4566",
		},
		{
			name:     "defaults to localhost",
			envVars:  map[string]string{},
			expected: "http://localhost:4566",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear env vars first, then set test values using t.Setenv
			t.Setenv("LOCALSTACK_ENDPOINT", "")
			t.Setenv("AWS_ENDPOINT_URL", "")

			// Set test env vars
			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			// Test
			got := GetLocalStackEndpoint()
			if got != tt.expected {
				t.Errorf("GetLocalStackEndpoint() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// TestShouldUseLocalStack tests the LocalStack usage detection
func TestShouldUseLocalStack(t *testing.T) {
	tests := []struct {
		name     string
		envVars  map[string]string
		expected bool
	}{
		{
			name:     "disabled when LATTIAM_USE_LOCALSTACK is false",
			envVars:  map[string]string{"LATTIAM_USE_LOCALSTACK": "false"},
			expected: false,
		},
		{
			name:     "enabled when LATTIAM_TEST_MODE is true",
			envVars:  map[string]string{"LATTIAM_TEST_MODE": "true"},
			expected: true,
		},
		{
			name:     "enabled when LATTIAM_TEST_MODE is localstack",
			envVars:  map[string]string{"LATTIAM_TEST_MODE": "localstack"},
			expected: true,
		},
		{
			name:     "disabled by default",
			envVars:  map[string]string{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear env vars first, then set test values using t.Setenv
			t.Setenv("LATTIAM_USE_LOCALSTACK", "")
			t.Setenv("LATTIAM_TEST_MODE", "")

			// Set test env vars
			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			// Test
			got := ShouldUseLocalStack()
			if got != tt.expected {
				t.Errorf("ShouldUseLocalStack() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// TestShouldWaitForLocalStack tests the LocalStack wait detection
func TestShouldWaitForLocalStack(t *testing.T) {
	tests := []struct {
		name     string
		envVars  map[string]string
		expected bool
	}{
		{
			name:     "waits in CI",
			envVars:  map[string]string{"CI": "true"},
			expected: true,
		},
		{
			name:     "waits when explicitly requested",
			envVars:  map[string]string{"LATTIAM_WAIT_FOR_LOCALSTACK": "true"},
			expected: true,
		},
		{
			name:     "doesn't wait by default",
			envVars:  map[string]string{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear env vars first, then set test values using t.Setenv
			t.Setenv("CI", "")
			t.Setenv("LATTIAM_WAIT_FOR_LOCALSTACK", "")

			// Set test env vars
			for k, v := range tt.envVars {
				t.Setenv(k, v)
			}

			// Test
			got := ShouldWaitForLocalStack()
			if got != tt.expected {
				t.Errorf("ShouldWaitForLocalStack() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// TestConstants verifies that constants are defined correctly
func TestConstants(t *testing.T) {
	t.Parallel()
	// Just verify the constants exist and have expected values
	if DefaultLocalStackURL != "http://localstack:4566" {
		t.Errorf("DefaultLocalStackURL = %v, want %v", DefaultLocalStackURL, "http://localstack:4566")
	}

	if LocalStackReadyTimeout.Seconds() != 30 {
		t.Errorf("LocalStackReadyTimeout = %v, want 30s", LocalStackReadyTimeout)
	}
}
