//go:build !integration
// +build !integration

package protocol

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRetryHandlerActuallyRetries(t *testing.T) {
	t.Parallel()
	config := &RetryConfig{
		MaxAttempts:   3,
		InitialDelay:  10 * time.Millisecond,
		MaxDelay:      100 * time.Millisecond,
		BackoffFactor: 2.0,
		RetryableErrors: []error{
			errors.New("throttled"),
		},
	}

	handler := NewRetryHandler(config)
	ctx := context.Background()

	t.Run("Retries on retryable error", func(t *testing.T) {
		t.Parallel()
		attemptCount := 0
		err := handler.ExecuteWithRetry(ctx, "TestOperation", func() error {
			attemptCount++
			if attemptCount < 3 {
				return errors.New("throttled")
			}
			return nil
		})

		require.NoError(t, err)
		assert.Equal(t, 3, attemptCount, "Should have retried 3 times")
	})

	t.Run("Does not retry on non-retryable error", func(t *testing.T) {
		t.Parallel()
		attemptCount := 0
		err := handler.ExecuteWithRetry(ctx, "TestOperation", func() error {
			attemptCount++
			return errors.New("permanent error")
		})

		require.Error(t, err)
		assert.Equal(t, 1, attemptCount, "Should not retry on non-retryable error")
	})

	t.Run("Respects max attempts", func(t *testing.T) {
		t.Parallel()
		attemptCount := 0
		err := handler.ExecuteWithRetry(ctx, "TestOperation", func() error {
			attemptCount++
			return errors.New("throttled")
		})

		require.Error(t, err)
		assert.Equal(t, 3, attemptCount, "Should stop after max attempts")
	})
}

func TestRetryDelayCalculation(t *testing.T) {
	t.Parallel()
	config := &RetryConfig{
		MaxAttempts:   5,
		InitialDelay:  100 * time.Millisecond,
		MaxDelay:      1 * time.Second,
		BackoffFactor: 2.0,
		RetryableErrors: []error{
			errors.New("retry me"),
		},
	}

	handler := NewRetryHandler(config)
	ctx := context.Background()

	var delays []time.Duration
	lastAttemptTime := time.Now()

	attemptCount := 0
	err := handler.ExecuteWithRetry(ctx, "TestDelay", func() error {
		attemptCount++
		if attemptCount > 1 {
			delay := time.Since(lastAttemptTime)
			delays = append(delays, delay)
		}
		lastAttemptTime = time.Now()

		if attemptCount < 4 {
			return errors.New("retry me")
		}
		return nil
	})

	require.NoError(t, err)
	require.Len(t, delays, 3)

	// Verify delays increase with backoff
	// First retry: ~100ms
	assert.InDelta(t, 100, delays[0].Milliseconds(), 20)

	// Second retry: ~200ms (100ms * 2.0)
	assert.InDelta(t, 200, delays[1].Milliseconds(), 40)

	// Third retry: ~400ms (200ms * 2.0)
	assert.InDelta(t, 400, delays[2].Milliseconds(), 80)
}
