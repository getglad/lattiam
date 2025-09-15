// Package utils provides utility functions for the Lattiam system
package utils

import (
	"strings"
)

// SanitizeResponse recursively removes sensitive keys from response data.
// It removes any keys that might contain filesystem paths or other sensitive information.
func SanitizeResponse(data interface{}) interface{} {
	// Define sensitive key patterns that should be removed
	// These are checked as whole words or with underscores/dashes
	sensitivePatterns := []string{
		"_path", "_dir", "_directory", "_location",
		"base_dir", "deployments_path", "file_path",
		"_root", "_folder",
		// Also check if key ends with these
		"path", "directory", "location", "folder",
	}

	return sanitizeValue(data, sensitivePatterns)
}

// sanitizeValue recursively processes the value based on its type
func sanitizeValue(data interface{}, sensitivePatterns []string) interface{} {
	switch v := data.(type) {
	case map[string]interface{}:
		// Process map by checking each key
		result := make(map[string]interface{})
		for key, value := range v {
			if !isSensitiveKey(key, sensitivePatterns) {
				// Recursively sanitize the value
				result[key] = sanitizeValue(value, sensitivePatterns)
			}
			// Skip sensitive keys entirely
		}
		return result

	case []interface{}:
		// Process slice by sanitizing each element
		result := make([]interface{}, len(v))
		for i, item := range v {
			result[i] = sanitizeValue(item, sensitivePatterns)
		}
		return result

	case string:
		// Check if string value looks like a path
		if looksLikePath(v) {
			return "[REDACTED]"
		}
		return v

	default:
		// Return other types as-is (numbers, bools, nil, etc.)
		return v
	}
}

// isSensitiveKey checks if a key matches any sensitive pattern
func isSensitiveKey(key string, patterns []string) bool {
	lowerKey := strings.ToLower(key)

	// Check exact matches for common sensitive keys
	exactMatches := []string{"path", "dir", "directory", "location", "root", "folder"}
	for _, exact := range exactMatches {
		if lowerKey == exact {
			return true
		}
	}

	// Check if key contains pattern (for compound keys like base_dir)
	for _, pattern := range patterns {
		if strings.Contains(lowerKey, pattern) {
			return true
		}
	}

	// Check if key ends with path-related suffix
	suffixes := []string{"_path", "_dir", "_directory", "_location", "_folder"}
	for _, suffix := range suffixes {
		if strings.HasSuffix(lowerKey, suffix) {
			return true
		}
	}

	return false
}

// looksLikePath checks if a string value appears to be a filesystem path
func looksLikePath(s string) bool {
	// Check for common path indicators
	pathIndicators := []string{
		"/home/", "/usr/", "/var/", "/etc/", "/opt/",
		"/tmp/", "/mnt/", "/root/", "C:\\", "D:\\",
		"~/", "../", "./",
	}

	for _, indicator := range pathIndicators {
		if strings.Contains(s, indicator) {
			return true
		}
	}

	// Check if it starts with / (absolute Unix path)
	if strings.HasPrefix(s, "/") && len(s) > 1 {
		return true
	}

	// Special case for just "/"
	if s == "/" {
		return true
	}

	return false
}
