package utils

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRedactSensitiveString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "password in JSON",
			input:    `{"password": "supersecret123", "user": "admin"}`,
			expected: "password=<REDACTED>",
		},
		{
			name:     "API key",
			input:    `api_key: "sk-1234567890abcdef"`,
			expected: "api_key=<REDACTED>",
		},
		{
			name:     "multiple secrets",
			input:    `{"password": "secret1", "token": "secret2"}`,
			expected: "password=<REDACTED>",
		},
		{
			name:     "no sensitive data",
			input:    `{"name": "test", "count": 42}`,
			expected: `{"name": "test", "count": 42}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := RedactSensitiveString(tt.input)
			if tt.expected != tt.input {
				// If we expect redaction, check that it occurred
				assert.Contains(t, result, "<REDACTED>", "Expected redaction to occur")
			} else {
				// If no redaction expected, string should be unchanged
				assert.Equal(t, tt.expected, result)
			}
		})
	}
}

//nolint:funlen // Comprehensive sensitive data redaction test with multiple data types
func TestRedactSensitiveMap(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    map[string]interface{}
		validate func(t *testing.T, result map[string]interface{})
	}{
		{
			name: "simple password field",
			input: map[string]interface{}{
				"username": "admin",
				"password": "supersecret",
			},
			validate: func(t *testing.T, result map[string]interface{}) {
				t.Helper()
				assert.Equal(t, "admin", result["username"])
				assert.Equal(t, "<REDACTED>", result["password"])
			},
		},
		{
			name: "nested sensitive data",
			input: map[string]interface{}{
				"database": map[string]interface{}{
					"host":     "localhost",
					"password": "dbpass123",
				},
			},
			validate: func(t *testing.T, result map[string]interface{}) {
				t.Helper()
				db := result["database"].(map[string]interface{})
				assert.Equal(t, "localhost", db["host"])
				assert.Equal(t, "<REDACTED>", db["password"])
			},
		},
		{
			name: "JWT token detection",
			input: map[string]interface{}{
				"auth_token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U",
				"user_id":    "12345",
			},
			validate: func(t *testing.T, result map[string]interface{}) {
				t.Helper()
				assert.Equal(t, "<REDACTED>", result["auth_token"])
				assert.Equal(t, "12345", result["user_id"])
			},
		},
		{
			name: "AWS credentials",
			input: map[string]interface{}{
				"aws_access_key": "AKIAIOSFODNN7EXAMPLE",
				"aws_secret_key": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
				"region":         "us-west-2",
			},
			validate: func(t *testing.T, result map[string]interface{}) {
				t.Helper()
				assert.Equal(t, "<REDACTED>", result["aws_access_key"])
				assert.Equal(t, "<REDACTED>", result["aws_secret_key"])
				assert.Equal(t, "us-west-2", result["region"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := RedactSensitiveMap(tt.input)
			tt.validate(t, result)
		})
	}
}

func TestRedactStateForLogging(t *testing.T) {
	t.Parallel()
	// Test with a complex Terraform-like state structure
	state := map[string]interface{}{
		"resources": []interface{}{
			map[string]interface{}{
				"type": "aws_db_instance",
				"name": "main",
				"attributes": map[string]interface{}{
					"id":              "db-12345",
					"master_password": "supersecretdbpass",
					"endpoint":        "db.example.com",
					"database_name":   "myapp",
				},
			},
		},
		"outputs": map[string]interface{}{
			"db_url":  "db.example.com",
			"api_key": "sk-proj-1234567890",
		},
	}

	result := RedactStateForLogging(state)
	resultMap := result.(map[string]interface{})

	// Check that sensitive fields are redacted
	resources := resultMap["resources"].([]interface{})
	resource := resources[0].(map[string]interface{})
	attrs := resource["attributes"].(map[string]interface{})

	// master_password should definitely be redacted
	assert.Equal(t, "<REDACTED>", attrs["master_password"])
	// endpoint field is kept as is since "endpoint" itself isn't a sensitive field name
	assert.Equal(t, "db.example.com", attrs["endpoint"])

	outputs := resultMap["outputs"].(map[string]interface{})
	// api_key should be redacted because "key" is in the field name
	assert.Equal(t, "<REDACTED>", outputs["api_key"])
	// db_url should not be redacted
	assert.Equal(t, "db.example.com", outputs["db_url"])
}

func TestIsSensitiveField(t *testing.T) {
	t.Parallel()
	sensitiveFields := []string{
		"password",
		"Password",
		"PASSWORD",
		"master_password",
		"db_password",
		"secret_key",
		"api_key",
		"private_key",
		"auth_token",
		"client_secret",
	}

	nonSensitiveFields := []string{
		"username",
		"email",
		"id",
		"name",
		"description",
		"count",
		"enabled",
	}

	for _, field := range sensitiveFields {
		t.Run("sensitive_"+field, func(t *testing.T) {
			t.Parallel()
			assert.True(t, isSensitiveField(field), "%s should be detected as sensitive", field)
		})
	}

	for _, field := range nonSensitiveFields {
		t.Run("non_sensitive_"+field, func(t *testing.T) {
			t.Parallel()
			assert.False(t, isSensitiveField(field), "%s should not be detected as sensitive", field)
		})
	}
}

func TestContainsSensitivePattern(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		value     string
		sensitive bool
	}{
		{
			name:      "JWT token",
			value:     "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U",
			sensitive: true,
		},
		{
			name:      "base64 secret",
			value:     strings.Repeat("A", 40) + "==",
			sensitive: true,
		},
		{
			name:      "hex secret",
			value:     strings.Repeat("a1b2c3", 10),
			sensitive: true,
		},
		{
			name:      "normal string",
			value:     "hello world",
			sensitive: false,
		},
		{
			name:      "short base64",
			value:     "dGVzdA==",
			sensitive: false, // Too short to be considered a secret
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := containsSensitivePattern(tt.value)
			assert.Equal(t, tt.sensitive, result, "Pattern detection for %s", tt.name)
		})
	}
}
