package errors

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestAnalyzeError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name            string
		err             error
		expectedType    ErrorType
		expectedMsg     string
		checkSuggestion bool
	}{
		{
			name:         "nil error",
			err:          nil,
			expectedType: ErrorTypeUnknown,
			expectedMsg:  "No error",
		},
		{
			name:            "context deadline exceeded",
			err:             context.DeadlineExceeded,
			expectedType:    ErrorTypeTimeout,
			expectedMsg:     "Operation timed out",
			checkSuggestion: true,
		},
		{
			name:            "timeout in error message",
			err:             errors.New("operation timeout after 30s"),
			expectedType:    ErrorTypeTimeout,
			expectedMsg:     "Operation timed out",
			checkSuggestion: true,
		},
		{
			name:            "AWS invalid token",
			err:             errors.New("InvalidClientTokenId: The security token included in the request is invalid"),
			expectedType:    ErrorTypeAuthentication,
			expectedMsg:     "Authentication failed",
			checkSuggestion: true,
		},
		{
			name:            "AWS access denied",
			err:             errors.New("AccessDenied: User is not authorized to perform: iam:CreateRole"),
			expectedType:    ErrorTypeAuthorization,
			expectedMsg:     "Authorization failed",
			checkSuggestion: true,
		},
		{
			name:            "Network connection refused",
			err:             errors.New("dial tcp 127.0.0.1:8080: connection refused"),
			expectedType:    ErrorTypeNetwork,
			expectedMsg:     "Network connectivity issue",
			checkSuggestion: true,
		},
		{
			name:            "Provider crash",
			err:             errors.New("provider produced unexpected panic: runtime error"),
			expectedType:    ErrorTypeProvider,
			expectedMsg:     "Provider error",
			checkSuggestion: true,
		},
		{
			name:            "Missing required attribute",
			err:             errors.New("required attribute 'name' not provided"),
			expectedType:    ErrorTypeConfiguration,
			expectedMsg:     "Configuration error",
			checkSuggestion: true,
		},
		{
			name:         "Unknown error",
			err:          errors.New("something went wrong"),
			expectedType: ErrorTypeUnknown,
			expectedMsg:  "something went wrong",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			analysis := AnalyzeError(tt.err)

			if analysis.Type != tt.expectedType {
				t.Errorf("Expected error type %v, got %v", tt.expectedType, analysis.Type)
			}

			if analysis.Message != tt.expectedMsg {
				t.Errorf("Expected message %q, got %q", tt.expectedMsg, analysis.Message)
			}

			if tt.checkSuggestion && analysis.Suggestion == "" {
				t.Error("Expected a suggestion but got none")
			}

			if tt.err != nil && !errors.Is(analysis.Original, tt.err) {
				t.Error("Original error not preserved")
			}
		})
	}
}

func TestErrorTypeString(t *testing.T) {
	t.Parallel()
	tests := []struct {
		errorType ErrorType
		expected  string
	}{
		{ErrorTypeUnknown, "Unknown"},
		{ErrorTypeTimeout, "Timeout"},
		{ErrorTypeAuthentication, "Authentication"},
		{ErrorTypeAuthorization, "Authorization"},
		{ErrorTypeNetwork, "Network"},
		{ErrorTypeProvider, "Provider"},
		{ErrorTypeConfiguration, "Configuration"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			t.Parallel()
			if got := tt.errorType.String(); got != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestFormatError(t *testing.T) {
	t.Parallel()
	// Create a timeout error
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	time.Sleep(10 * time.Millisecond)
	err := ctx.Err()

	analysis := AnalyzeError(err)
	formatted := FormatError(analysis)

	// Check that formatted output contains key elements
	expectedStrings := []string{
		"Error Type: Timeout",
		"Message: Operation timed out",
		"Suggestion:",
		"Details: context deadline exceeded",
	}

	for _, expected := range expectedStrings {
		if !contains(formatted, expected) {
			t.Errorf("Expected formatted error to contain %q, but it didn't.\nFull output:\n%s", expected, formatted)
		}
	}
}

func TestAuthenticationErrorVariants(t *testing.T) {
	t.Parallel()
	authErrors := []string{
		"InvalidUserid.NotFound: The user ID 'AKIA123' does not exist",
		"InvalidSignature: The request signature we calculated does not match",
		"TokenError: The provided token is malformed",
		"Unable to locate credentials",
		"No valid credential sources found",
		"Authentication failed: invalid username or password",
		"Unauthorized: Session token has expired",
	}

	for _, errMsg := range authErrors {
		t.Run(errMsg, func(t *testing.T) {
			t.Parallel()
			analysis := AnalyzeError(errors.New(errMsg))
			if analysis.Type != ErrorTypeAuthentication {
				t.Errorf("Expected authentication error for %q, got %v", errMsg, analysis.Type)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && (stringContains(s, substr)))
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
