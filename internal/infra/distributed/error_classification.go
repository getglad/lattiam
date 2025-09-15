package distributed

import (
	"errors"
	"strings"
)

// ErrorType represents different categories of errors
type ErrorType int

const (
	// ErrorTypeUnknown indicates an unclassified error type
	ErrorTypeUnknown ErrorType = iota
	// ErrorTypeTransient indicates a temporary error that may resolve on retry
	ErrorTypeTransient
	// ErrorTypePermanent indicates a persistent error that won't resolve on retry
	ErrorTypePermanent
	// ErrorTypeMemoryPressure indicates memory-related resource constraints
	ErrorTypeMemoryPressure
	// ErrorTypeConnectionRefused indicates network connectivity issues
	ErrorTypeConnectionRefused
	// ErrorTypeTimeout indicates operations that exceeded time limits
	ErrorTypeTimeout
	// ErrorTypeResourceExhausted indicates resource limits have been reached
	ErrorTypeResourceExhausted
	// ErrorTypeAuthentication indicates authentication or credential failures
	ErrorTypeAuthentication
	// ErrorTypePermission indicates authorization or permission failures
	ErrorTypePermission
)

// String returns the string representation of ErrorType
func (et ErrorType) String() string {
	switch et {
	case ErrorTypeTransient:
		return "transient"
	case ErrorTypePermanent:
		return "permanent"
	case ErrorTypeMemoryPressure:
		return "memory_pressure"
	case ErrorTypeConnectionRefused:
		return "connection_refused"
	case ErrorTypeTimeout:
		return "timeout"
	case ErrorTypeResourceExhausted:
		return "resource_exhausted"
	case ErrorTypeAuthentication:
		return "authentication"
	case ErrorTypePermission:
		return "permission"
	case ErrorTypeUnknown:
		return "unknown"
	default:
		return "unknown"
	}
}

// ErrorClassifier provides error classification and handling recommendations
type ErrorClassifier struct{}

// NewErrorClassifier creates a new error classifier
func NewErrorClassifier() *ErrorClassifier {
	return &ErrorClassifier{}
}

// Classify categorizes an error and provides handling recommendations
//
//nolint:funlen // Error classification requires comprehensive pattern matching
func (ec *ErrorClassifier) Classify(err error) *ErrorInfo {
	if err == nil {
		return &ErrorInfo{Type: ErrorTypeUnknown, Retryable: false}
	}

	// Check for specific error types first
	var memErr *MemoryPressureError
	if errors.As(err, &memErr) {
		return &ErrorInfo{
			Type:             ErrorTypeMemoryPressure,
			Retryable:        false, // Not immediately retryable
			RequiresBackoff:  true,
			SuggestedBackoff: "5s",
			UseFallback:      true,
			Description:      "Redis memory pressure detected",
		}
	}

	// Classify based on error message
	errStr := strings.ToLower(err.Error())

	// Connection-related errors
	if ec.containsAny(errStr, []string{
		"connection refused",
		"connection reset by peer",
		"connection closed",
		"connection pool exhausted",
		"no route to host",
		"network unreachable",
	}) {
		return &ErrorInfo{
			Type:             ErrorTypeConnectionRefused,
			Retryable:        true,
			RequiresBackoff:  true,
			SuggestedBackoff: "1s",
			UseFallback:      false, // Don't use fallback for connection failures - they should fail
			Description:      "Connection failure",
		}
	}

	// Timeout errors
	if ec.containsAny(errStr, []string{
		"timeout",
		"deadline exceeded",
		"context deadline exceeded",
		"i/o timeout",
		"read timeout",
		"write timeout",
	}) {
		return &ErrorInfo{
			Type:             ErrorTypeTimeout,
			Retryable:        true,
			RequiresBackoff:  false,
			SuggestedBackoff: "100ms",
			UseFallback:      false,
			Description:      "Operation timeout",
		}
	}

	// Resource exhaustion
	if ec.containsAny(errStr, []string{
		"out of memory",
		"oom",
		"oom command not allowed",
		"maxmemory",
		"memory limit exceeded",
		"redis: connection pool exhausted",
		"too many connections",
		"resource temporarily unavailable",
	}) {
		return &ErrorInfo{
			Type:             ErrorTypeResourceExhausted,
			Retryable:        false, // Don't retry memory pressure immediately
			RequiresBackoff:  true,
			SuggestedBackoff: "5s",
			UseFallback:      true,
			Description:      "Resource exhaustion (memory/connection limits)",
		}
	}

	// Authentication and permission errors
	if ec.containsAny(errStr, []string{
		"authentication failed",
		"invalid username or password",
		"noauth",
		"permission denied",
		"access denied",
		"unauthorized",
	}) {
		return &ErrorInfo{
			Type:             ErrorTypeAuthentication,
			Retryable:        false,
			RequiresBackoff:  false,
			SuggestedBackoff: "",
			UseFallback:      false,
			Description:      "Authentication or permission error",
		}
	}

	// General transient errors
	if ec.containsAny(errStr, []string{
		"temporary",
		"temporarily",
		"unavailable",
		"try again",
		"retry",
		"redis:",
		"broken pipe",
		"connection aborted",
	}) {
		return &ErrorInfo{
			Type:             ErrorTypeTransient,
			Retryable:        true,
			RequiresBackoff:  true,
			SuggestedBackoff: "500ms",
			UseFallback:      true,
			Description:      "Transient error",
		}
	}

	// Unknown errors - conservative approach
	return &ErrorInfo{
		Type:             ErrorTypeUnknown,
		Retryable:        false,
		RequiresBackoff:  false,
		SuggestedBackoff: "",
		UseFallback:      false,
		Description:      "Unknown error type",
	}
}

// containsAny checks if the string contains any of the given substrings
func (ec *ErrorClassifier) containsAny(s string, patterns []string) bool {
	for _, pattern := range patterns {
		if strings.Contains(s, pattern) {
			return true
		}
	}
	return false
}

// IsRetryable is a convenience method to check if an error is retryable
func (ec *ErrorClassifier) IsRetryable(err error) bool {
	info := ec.Classify(err)
	return info.Retryable
}

// IsTransient is a convenience method to check if an error is transient
func (ec *ErrorClassifier) IsTransient(err error) bool {
	info := ec.Classify(err)
	return info.Type == ErrorTypeTransient ||
		info.Type == ErrorTypeConnectionRefused ||
		info.Type == ErrorTypeTimeout ||
		info.Type == ErrorTypeResourceExhausted ||
		info.Type == ErrorTypeMemoryPressure
}

// ShouldUseFallback determines if fallback mechanisms should be used
func (ec *ErrorClassifier) ShouldUseFallback(err error) bool {
	info := ec.Classify(err)
	return info.UseFallback
}

// ErrorInfo contains detailed information about an error
type ErrorInfo struct {
	Type             ErrorType
	Retryable        bool
	RequiresBackoff  bool
	SuggestedBackoff string
	UseFallback      bool
	Description      string
}
