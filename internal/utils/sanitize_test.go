package utils

import (
	"reflect"
	"testing"
)

//nolint:funlen // Comprehensive response sanitization test covering multiple data structures
func TestSanitizeResponse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		input    interface{}
		expected interface{}
	}{
		{
			name: "removes path keys from map",
			input: map[string]interface{}{
				"type":             "file",
				"path":             "/home/user/data",
				"base_dir":         "/var/lib/app",
				"deployments_path": "/etc/app/deployments",
				"exists":           true,
				"count":            42,
			},
			expected: map[string]interface{}{
				"type":   "file",
				"exists": true,
				"count":  42,
			},
		},
		{
			name: "sanitizes nested maps",
			input: map[string]interface{}{
				"storage": map[string]interface{}{
					"type":     "local",
					"path":     "/data/storage",
					"writable": true,
				},
				"metrics": map[string]interface{}{
					"directory": "/var/log/metrics",
					"enabled":   true,
				},
			},
			expected: map[string]interface{}{
				"storage": map[string]interface{}{
					"type":     "local",
					"writable": true,
				},
				"metrics": map[string]interface{}{
					"enabled": true,
				},
			},
		},
		{
			name: "sanitizes arrays",
			input: []interface{}{
				map[string]interface{}{
					"name": "config1",
					"path": "/etc/config1",
				},
				map[string]interface{}{
					"name":     "config2",
					"location": "/opt/config2",
				},
			},
			expected: []interface{}{
				map[string]interface{}{
					"name": "config1",
				},
				map[string]interface{}{
					"name": "config2",
				},
			},
		},
		{
			name: "redacts path-like strings",
			input: map[string]interface{}{
				"message":     "Error in /var/log/app.log",
				"description": "Check the file",
				"home_path":   "/home/user",
			},
			expected: map[string]interface{}{
				"message":     "[REDACTED]",
				"description": "Check the file",
			},
		},
		{
			name: "handles mixed nested structures",
			input: map[string]interface{}{
				"system": map[string]interface{}{
					"paths": []interface{}{
						"/usr/bin",
						"/usr/local/bin",
					},
					"env": map[string]interface{}{
						"HOME":     "/home/user",
						"LANG":     "en_US.UTF-8",
						"base_dir": "/app",
					},
				},
			},
			expected: map[string]interface{}{
				"system": map[string]interface{}{
					"env": map[string]interface{}{
						"HOME": "[REDACTED]",
						"LANG": "en_US.UTF-8",
						// base_dir is removed as it contains _dir
					},
				},
			},
		},
		{
			name: "preserves non-sensitive data",
			input: map[string]interface{}{
				"port":    8080,
				"enabled": true,
				"version": "1.2.3",
				"name":    "myapp",
			},
			expected: map[string]interface{}{
				"port":    8080,
				"enabled": true,
				"version": "1.2.3",
				"name":    "myapp",
			},
		},
		{
			name:     "handles nil input",
			input:    nil,
			expected: nil,
		},
		{
			name: "handles empty structures",
			input: map[string]interface{}{
				"empty_map":   map[string]interface{}{},
				"empty_array": []interface{}{},
			},
			expected: map[string]interface{}{
				"empty_map":   map[string]interface{}{},
				"empty_array": []interface{}{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := SanitizeResponse(tt.input)
			if !reflect.DeepEqual(result, tt.expected) {
				t.Errorf("SanitizeResponse() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestLooksLikePath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input    string
		expected bool
	}{
		{"/home/user", true},
		{"/var/log/app.log", true},
		{"C:\\Windows\\System32", true},
		{"~/documents", true},
		{"../config", true},
		{"./local", true},
		{"simple string", false},
		{"port:8080", false},
		{"https://example.com", false},
		{"", false},
		{"justtext", false},
		{"/", true},
		{"value/with/slashes", false}, // Not an absolute path
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			result := looksLikePath(tt.input)
			if result != tt.expected {
				t.Errorf("looksLikePath(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}
