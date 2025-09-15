// Package utils provides utility functions for the Lattiam system
package utils

import (
	"fmt"
	"regexp"
	"strings"
)

const (
	redactedPlaceholder = "<REDACTED>"
)

// Common patterns for sensitive data
var sensitivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(password|passwd|pwd|secret|token|key|apikey|api_key|access_key|private_key|client_secret)["':=]\s*["']?([^"',}\s]+)`),
	regexp.MustCompile(`(?i)(bearer|authorization|auth-token|x-api-key)["':=]\s*["']?([^"',}\s]+)`),
	regexp.MustCompile(`(?i)ssh-rsa\s+[A-Za-z0-9+/=]+`),
	regexp.MustCompile(`(?i)-----BEGIN\s+(RSA\s+)?PRIVATE\s+KEY-----[\s\S]+?-----END\s+(RSA\s+)?PRIVATE\s+KEY-----`),
}

// Common sensitive field names in Terraform state
var sensitiveFields = map[string]bool{
	"password":          true,
	"secret":            true,
	"token":             true,
	"private_key":       true,
	"client_secret":     true,
	"access_key":        true,
	"secret_key":        true,
	"api_key":           true,
	"auth_token":        true,
	"refresh_token":     true,
	"database_password": true,
	"master_password":   true,
	"private_key_pem":   true,
	"certificate_pem":   true,
	"certificate_chain": true,
	"signing_key":       true,
	"encryption_key":    true,
}

// RedactSensitiveString redacts sensitive information from a string representation
func RedactSensitiveString(input string) string {
	if input == "" {
		return input
	}

	redacted := input

	// Apply pattern-based redaction
	for _, pattern := range sensitivePatterns {
		redacted = pattern.ReplaceAllStringFunc(redacted, func(match string) string {
			// Preserve the key but redact the value
			parts := pattern.FindStringSubmatch(match)
			if len(parts) > 1 {
				key := parts[1]
				return fmt.Sprintf("%s=%s", key, redactedPlaceholder)
			}
			return redactedPlaceholder
		})
	}

	return redacted
}

// RedactSensitiveMap recursively redacts sensitive fields from a map
func RedactSensitiveMap(input map[string]interface{}) map[string]interface{} {
	if input == nil {
		return nil
	}

	result := make(map[string]interface{})

	for key, value := range input {
		// Check if this key is sensitive
		if isSensitiveField(key) {
			result[key] = redactedPlaceholder
			continue
		}

		// Recursively process nested structures
		switch v := value.(type) {
		case map[string]interface{}:
			result[key] = RedactSensitiveMap(v)
		case []interface{}:
			result[key] = redactSensitiveSlice(v)
		case string:
			// Check if the string value contains sensitive patterns
			if containsSensitivePattern(v) {
				result[key] = redactedPlaceholder
			} else {
				result[key] = v
			}
		default:
			result[key] = value
		}
	}

	return result
}

// redactSensitiveSlice processes slices for sensitive data
func redactSensitiveSlice(input []interface{}) []interface{} {
	if input == nil {
		return nil
	}

	result := make([]interface{}, len(input))

	for i, item := range input {
		switch v := item.(type) {
		case map[string]interface{}:
			result[i] = RedactSensitiveMap(v)
		case []interface{}:
			result[i] = redactSensitiveSlice(v)
		case string:
			if containsSensitivePattern(v) {
				result[i] = redactedPlaceholder
			} else {
				result[i] = v
			}
		default:
			result[i] = item
		}
	}

	return result
}

// isSensitiveField checks if a field name is sensitive
func isSensitiveField(key string) bool {
	lowerKey := strings.ToLower(key)

	// Direct match
	if sensitiveFields[lowerKey] {
		return true
	}

	// Partial match for common patterns
	sensitiveSubstrings := []string{
		"password", "secret", "token", "key", "credential",
		"private", "auth", "bearer", "api_key", "access_key",
	}

	for _, substr := range sensitiveSubstrings {
		if strings.Contains(lowerKey, substr) {
			return true
		}
	}

	return false
}

// containsSensitivePattern checks if a string contains sensitive patterns
func containsSensitivePattern(value string) bool {
	// Check for common secret formats
	if len(value) > 20 {
		// Check for base64-like strings that might be secrets
		if regexp.MustCompile(`^[A-Za-z0-9+/]{20,}={0,2}$`).MatchString(value) {
			return true
		}

		// Check for hex strings that might be secrets
		if regexp.MustCompile(`^[a-fA-F0-9]{32,}$`).MatchString(value) {
			return true
		}
	}

	// Check for JWT tokens (must have specific JWT format with base64url encoding)
	if strings.Count(value, ".") == 2 {
		parts := strings.Split(value, ".")
		if len(parts) == 3 {
			// JWT parts should be base64url encoded (no spaces, specific chars)
			isJWT := true
			for _, part := range parts {
				if !regexp.MustCompile(`^[A-Za-z0-9_-]+$`).MatchString(part) || len(part) < 10 {
					isJWT = false
					break
				}
			}
			if isJWT {
				return true
			}
		}
	}

	return false
}

// RedactStateForLogging prepares state data for safe logging
func RedactStateForLogging(state interface{}) interface{} {
	switch v := state.(type) {
	case map[string]interface{}:
		return RedactSensitiveMap(v)
	case string:
		return RedactSensitiveString(v)
	case []interface{}:
		return redactSensitiveSlice(v)
	default:
		// For other types, convert to string and redact
		str := fmt.Sprintf("%+v", state)
		return RedactSensitiveString(str)
	}
}
