package errors

import (
	"context"
	"errors"
	"strings"
)

// ErrorType represents the category of error
type ErrorType int

const (
	// ErrorTypeUnknown is the default error type
	ErrorTypeUnknown ErrorType = iota
	// ErrorTypeTimeout indicates a timeout occurred
	ErrorTypeTimeout
	// ErrorTypeAuthentication indicates an authentication failure
	ErrorTypeAuthentication
	// ErrorTypeAuthorization indicates an authorization failure
	ErrorTypeAuthorization
	// ErrorTypeNetwork indicates a network connectivity issue
	ErrorTypeNetwork
	// ErrorTypeProvider indicates a provider-specific error
	ErrorTypeProvider
	// ErrorTypeConfiguration indicates a configuration error
	ErrorTypeConfiguration
)

// ErrorAnalysis contains the analysis result of an error
type ErrorAnalysis struct {
	Type       ErrorType
	Original   error
	Message    string
	Suggestion string
}

// AnalyzeError analyzes an error and returns categorized information
func AnalyzeError(err error) ErrorAnalysis {
	if err == nil {
		return ErrorAnalysis{
			Type:    ErrorTypeUnknown,
			Message: "No error",
		}
	}

	analysis := ErrorAnalysis{
		Type:     ErrorTypeUnknown,
		Original: err,
		Message:  err.Error(),
	}

	errStr := strings.ToLower(err.Error())

	// Check error types in order of priority
	if result := checkTimeoutError(err, errStr); result != nil {
		return *result
	}

	if result := checkAuthenticationError(err, errStr); result != nil {
		return *result
	}

	if result := checkAuthorizationError(err, errStr); result != nil {
		return *result
	}

	if result := checkNetworkError(err, errStr); result != nil {
		return *result
	}

	if result := checkProviderError(err, errStr); result != nil {
		return *result
	}

	if result := checkConfigurationError(err, errStr); result != nil {
		return *result
	}

	return analysis
}

// checkTimeoutError checks if the error is a timeout error
func checkTimeoutError(err error, errStr string) *ErrorAnalysis {
	if errors.Is(err, context.DeadlineExceeded) ||
		strings.Contains(errStr, "deadline exceeded") ||
		strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "context canceled") {
		return &ErrorAnalysis{
			Type:       ErrorTypeTimeout,
			Original:   err,
			Message:    "Operation timed out",
			Suggestion: "Consider increasing timeout values using environment variables: LATTIAM_PROVIDER_TIMEOUT, LATTIAM_DEPLOYMENT_TIMEOUT",
		}
	}
	return nil
}

// checkAuthenticationError checks if the error is an authentication error
func checkAuthenticationError(err error, errStr string) *ErrorAnalysis {
	authPatterns := []string{
		"invaliduserid.notfound",
		"invalidsignature",
		"invalidclienttokenid",
		"tokenerror",
		"unable to locate credentials",
		"no valid credential",
		"credential",
		"authentication failed",
		"unauthorized",
		"invalid credentials",
		"token expired",
		"session token",
		"shared config profile",
		"profile not found",
	}

	if containsAnyPattern(errStr, authPatterns) {
		return &ErrorAnalysis{
			Type:       ErrorTypeAuthentication,
			Original:   err,
			Message:    "Authentication failed",
			Suggestion: "Check your AWS credentials, profile configuration, or session tokens",
		}
	}
	return nil
}

// checkAuthorizationError checks if the error is an authorization error
func checkAuthorizationError(err error, errStr string) *ErrorAnalysis {
	authzPatterns := []string{
		"unauthorizedoperation",
		"accessdenied",
		"access denied",
		"forbidden",
		"not authorized",
		"permission denied",
		"insufficient privileges",
	}

	if containsAnyPattern(errStr, authzPatterns) {
		return &ErrorAnalysis{
			Type:       ErrorTypeAuthorization,
			Original:   err,
			Message:    "Authorization failed",
			Suggestion: "Ensure your IAM user/role has the necessary permissions for this operation",
		}
	}
	return nil
}

// checkNetworkError checks if the error is a network error
func checkNetworkError(err error, errStr string) *ErrorAnalysis {
	networkPatterns := []string{
		"connection refused",
		"no such host",
		"network unreachable",
		"connection reset",
		"broken pipe",
		"cannot connect",
		"dial tcp",
		"i/o timeout",
		"tls handshake",
	}

	if containsAnyPattern(errStr, networkPatterns) {
		return &ErrorAnalysis{
			Type:       ErrorTypeNetwork,
			Original:   err,
			Message:    "Network connectivity issue",
			Suggestion: "Check your network connection and ensure the endpoint is reachable",
		}
	}
	return nil
}

// checkProviderError checks if the error is a provider error
func checkProviderError(err error, errStr string) *ErrorAnalysis {
	providerPatterns := []string{
		"provider produced",
		"provider error",
		"failed to get provider",
		"provider crash",
		"provider panic",
		"provider schema",
	}

	if containsAnyPattern(errStr, providerPatterns) {
		return &ErrorAnalysis{
			Type:       ErrorTypeProvider,
			Original:   err,
			Message:    "Provider error",
			Suggestion: "This may be a provider bug or incompatibility. Check provider logs for details",
		}
	}
	return nil
}

// checkConfigurationError checks if the error is a configuration error
func checkConfigurationError(err error, errStr string) *ErrorAnalysis {
	configPatterns := []string{
		"invalid configuration",
		"missing required",
		"required attribute",
		"validation error",
		"invalid value",
		"prepare config error",
		"configure error",
	}

	if containsAnyPattern(errStr, configPatterns) {
		return &ErrorAnalysis{
			Type:       ErrorTypeConfiguration,
			Original:   err,
			Message:    "Configuration error",
			Suggestion: "Review your resource configuration and ensure all required fields are provided",
		}
	}
	return nil
}

// containsAnyPattern checks if the string contains any of the patterns
func containsAnyPattern(str string, patterns []string) bool {
	for _, pattern := range patterns {
		if strings.Contains(str, pattern) {
			return true
		}
	}
	return false
}

// String returns the error type as a string
func (et ErrorType) String() string {
	switch et {
	case ErrorTypeTimeout:
		return "Timeout"
	case ErrorTypeAuthentication:
		return "Authentication"
	case ErrorTypeAuthorization:
		return "Authorization"
	case ErrorTypeNetwork:
		return "Network"
	case ErrorTypeProvider:
		return "Provider"
	case ErrorTypeConfiguration:
		return "Configuration"
	default:
		return "Unknown"
	}
}

// FormatError formats an error analysis for user display
func FormatError(analysis ErrorAnalysis) string {
	var sb strings.Builder

	sb.WriteString("Error Type: ")
	sb.WriteString(analysis.Type.String())
	sb.WriteString("\n")

	sb.WriteString("Message: ")
	sb.WriteString(analysis.Message)
	sb.WriteString("\n")

	if analysis.Suggestion != "" {
		sb.WriteString("Suggestion: ")
		sb.WriteString(analysis.Suggestion)
		sb.WriteString("\n")
	}

	if analysis.Original != nil {
		sb.WriteString("Details: ")
		sb.WriteString(analysis.Original.Error())
		sb.WriteString("\n")
	}

	return sb.String()
}
