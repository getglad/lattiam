package protocol

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/lattiam/lattiam/pkg/logging"
)

// RetryConfig defines retry behavior for provider operations
type RetryConfig struct {
	MaxAttempts     int
	InitialDelay    time.Duration
	MaxDelay        time.Duration
	BackoffFactor   float64
	RetryableErrors []error
}

// DefaultRetryConfig returns sensible defaults for provider operations
func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxAttempts:   3,
		InitialDelay:  100 * time.Millisecond,
		MaxDelay:      5 * time.Second,
		BackoffFactor: 2.0,
		RetryableErrors: []error{
			errors.New("i/o timeout"),
			errors.New("connection reset by peer"),
			errors.New("broken pipe"),
		},
	}
}

// RetryHandler provides retry logic for provider operations
type RetryHandler struct {
	config *RetryConfig
}

// NewRetryHandler creates a new retry handler
func NewRetryHandler(config *RetryConfig) *RetryHandler {
	if config == nil {
		config = DefaultRetryConfig()
	}
	return &RetryHandler{config: config}
}

// ExecuteWithRetry runs an operation with retry logic
func (r *RetryHandler) ExecuteWithRetry(
	ctx context.Context,
	operation string,
	fn func() error,
) error {
	var lastErr error
	delay := r.config.InitialDelay

	for attempt := 1; attempt <= r.config.MaxAttempts; attempt++ {
		// Check context before each attempt
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("operation canceled: %w", err)
		}

		// Execute the operation
		err := fn()
		if err == nil {
			if attempt > 1 {
				logging.ProtocolSuccess("RetrySuccess", operation,
					fmt.Sprintf("succeeded after %d attempts", attempt))
			}
			return nil
		}

		lastErr = err

		// Check if error is retryable
		if !r.isRetryable(err) || attempt == r.config.MaxAttempts {
			return err
		}

		// Log retry attempt
		logging.ProtocolWarning("RetryAttempt", operation,
			fmt.Sprintf("attempt %d/%d failed: %v, retrying in %v",
				attempt, r.config.MaxAttempts, err, delay))

		// Wait before retry
		select {
		case <-time.After(delay):
			// Calculate next delay with backoff
			delay = time.Duration(float64(delay) * r.config.BackoffFactor)
			if delay > r.config.MaxDelay {
				delay = r.config.MaxDelay
			}
		case <-ctx.Done():
			return fmt.Errorf("retry canceled: %w", ctx.Err())
		}
	}

	return fmt.Errorf("operation failed after %d attempts: %w",
		r.config.MaxAttempts, lastErr)
}

// isRetryable checks if an error should trigger a retry
func (r *RetryHandler) isRetryable(err error) bool {
	if err == nil {
		return false
	}

	// Check against known retryable errors
	errStr := err.Error()
	for _, retryableErr := range r.config.RetryableErrors {
		if errors.Is(err, retryableErr) ||
			contains(errStr, retryableErr.Error()) {
			return true
		}
	}

	// Check for gRPC errors that are retryable
	if contains(errStr, "unavailable") ||
		contains(errStr, "deadline exceeded") ||
		contains(errStr, "internal error") {
		return true
	}

	return false
}

// contains checks if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr || s != "" && substr != "" &&
			findSubstring(s, substr))
}

// findSubstring is a simple substring search
func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
