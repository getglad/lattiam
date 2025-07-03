package expressions

import (
	"strings"
	"testing"
)

func TestEvaluateExpression(t *testing.T) {
	t.Parallel()
	evaluator := NewEvaluator()

	tests := []struct {
		name     string
		input    string
		wantType string // "uuid", "timestamp", "static", etc.
	}{
		{
			name:     "uuid function",
			input:    "${uuid()}",
			wantType: "uuid",
		},
		{
			name:     "timestamp function",
			input:    "${timestamp()}",
			wantType: "timestamp",
		},
		{
			name:     "string functions",
			input:    "${upper(\"hello\")}",
			wantType: "static",
		},
		{
			name:     "base64 encode",
			input:    "${base64encode(\"hello world\")}",
			wantType: "static",
		},
		{
			name:     "math functions",
			input:    "${max(5, 10, 3)}",
			wantType: "static",
		},
		{
			name:     "string interpolation with function",
			input:    "prefix-${uuid()}-suffix",
			wantType: "uuid_interpolated",
		},
		{
			name:     "multiple functions",
			input:    "${upper(\"test\")}-${lower(\"TEST\")}",
			wantType: "static",
		},
		{
			name:     "no interpolation",
			input:    "just a plain string",
			wantType: "plain",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := evaluator.EvaluateExpression(tt.input)
			if err != nil {
				t.Errorf("EvaluateExpression() error = %v", err)
				return
			}

			// Verify the result based on type
			verifyResult(t, tt.wantType, tt.input, result)

			t.Logf("Input: %s => Output: %s", tt.input, result)
		})
	}
}

// verifyResult validates the result based on the expected type
func verifyResult(t *testing.T, wantType, input, result string) {
	t.Helper()

	switch wantType {
	case "uuid":
		verifyUUID(t, result)
	case "timestamp":
		verifyTimestamp(t, result)
	case "uuid_interpolated":
		verifyUUIDInterpolated(t, result)
	case "plain":
		verifyPlain(t, input, result)
	case "static":
		verifyStatic(t, input, result)
	}
}

func verifyUUID(t *testing.T, result string) {
	t.Helper()
	// UUID should be 36 characters (8-4-4-4-12)
	if len(result) != 36 || !strings.Contains(result, "-") {
		t.Errorf("Expected UUID format, got %s", result)
	}
}

func verifyTimestamp(t *testing.T, result string) {
	t.Helper()
	// Timestamp should contain T and Z (RFC3339)
	if !strings.Contains(result, "T") || !strings.Contains(result, "Z") {
		t.Errorf("Expected RFC3339 timestamp, got %s", result)
	}
}

func verifyUUIDInterpolated(t *testing.T, result string) {
	t.Helper()
	// Should have prefix and suffix with UUID in between
	if !strings.HasPrefix(result, "prefix-") || !strings.HasSuffix(result, "-suffix") {
		t.Errorf("Expected prefix-UUID-suffix format, got %s", result)
	}
	// Extract UUID part and verify
	parts := strings.Split(result, "-")
	if len(parts) < 7 { // prefix + 5 UUID parts + suffix
		t.Errorf("Expected UUID in interpolation, got %s", result)
	}
}

func verifyPlain(t *testing.T, input, result string) {
	t.Helper()
	if result != input {
		t.Errorf("Expected %s, got %s", input, result)
	}
}

func verifyStatic(t *testing.T, input, result string) {
	t.Helper()
	// Map of input to expected output for static results
	expectedResults := map[string]string{
		"${upper(\"hello\")}":                   "HELLO",
		"${base64encode(\"hello world\")}":      "aGVsbG8gd29ybGQ=",
		"${max(5, 10, 3)}":                      "10",
		"${upper(\"test\")}-${lower(\"TEST\")}": "TEST-test",
	}

	if expected, ok := expectedResults[input]; ok {
		if result != expected {
			t.Errorf("Expected %s, got %s", expected, result)
		}
	}
}

func TestEvaluateWithVariables(t *testing.T) {
	t.Parallel()
	evaluator := NewEvaluator()

	tests := []struct {
		name      string
		input     string
		variables map[string]interface{}
		want      string
	}{
		{
			name:  "variable reference",
			input: "${var.name}",
			variables: map[string]interface{}{
				"var": map[string]interface{}{
					"name": "test-value",
				},
			},
			want: "test-value",
		},
		{
			name:  "function with variable",
			input: "${upper(var.name)}",
			variables: map[string]interface{}{
				"var": map[string]interface{}{
					"name": "hello",
				},
			},
			want: "HELLO",
		},
		{
			name:  "mixed content",
			input: "prefix-${var.env}-${uuid()}-suffix",
			variables: map[string]interface{}{
				"var": map[string]interface{}{
					"env": "production",
				},
			},
			want: "prefix-production-*-suffix", // * will be UUID
		},
		{
			name:      "nested functions",
			input:     `${trimspace(upper("  hello  "))}`,
			variables: map[string]interface{}{},
			want:      "HELLO",
		},
		{
			name:  "variable with mixed types in map",
			input: "${var.name}",
			variables: map[string]interface{}{
				"var": map[string]interface{}{
					"name":    "test-server",
					"port":    8080,
					"enabled": true,
				},
			},
			want: "test-server",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := evaluator.EvaluateWithVariables(tt.input, tt.variables)
			if err != nil {
				t.Errorf("EvaluateWithVariables() error = %v", err)
				return
			}

			// For cases with UUID, do partial matching
			if strings.Contains(tt.want, "*") {
				prefix := strings.Split(tt.want, "*")[0]
				suffix := strings.Split(tt.want, "*")[1]
				if !strings.HasPrefix(result, prefix) || !strings.HasSuffix(result, suffix) {
					t.Errorf("Expected pattern %s, got %s", tt.want, result)
				}
			} else if result != tt.want {
				t.Errorf("Expected %s, got %s", tt.want, result)
			}

			t.Logf("Input: %s => Output: %s", tt.input, result)
		})
	}
}

func TestHashFunctions(t *testing.T) {
	t.Parallel()
	evaluator := NewEvaluator()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "md5",
			input: "${md5(\"hello world\")}",
			want:  "5eb63bbbe01eeed093cb22bb8f5acdc3",
		},
		{
			name:  "sha1",
			input: "${sha1(\"hello world\")}",
			want:  "2aae6c35c94fcfb415dbe95f408b9ce91ee846ed",
		},
		{
			name:  "sha256",
			input: "${sha256(\"hello world\")}",
			want:  "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := evaluator.EvaluateExpression(tt.input)
			if err != nil {
				t.Errorf("EvaluateExpression() error = %v", err)
				return
			}

			if result != tt.want {
				t.Errorf("Expected %s, got %s", tt.want, result)
			}
		})
	}
}
