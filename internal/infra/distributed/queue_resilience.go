package distributed

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"

	"github.com/lattiam/lattiam/pkg/logging"
)

// ResilienceConfig configures resilience patterns for queue operations
type ResilienceConfig struct {
	// Retry configuration
	MaxRetries    int
	InitialDelay  time.Duration
	MaxDelay      time.Duration
	BackoffFactor float64

	// Circuit breaker configuration
	CircuitBreaker *CircuitBreakerConfig

	// Memory pressure thresholds
	MemoryWarningThreshold int64 // Redis memory usage in bytes
	MemoryDisableThreshold int64 // When to completely disable new enqueues

	// Fallback configuration
	EnableFallback bool
	FallbackTTL    time.Duration
}

// DefaultResilienceConfig returns sensible defaults
func DefaultResilienceConfig() *ResilienceConfig {
	return &ResilienceConfig{
		MaxRetries:             3,
		InitialDelay:           100 * time.Millisecond,
		MaxDelay:               5 * time.Second,
		BackoffFactor:          2.0,
		CircuitBreaker:         DefaultCircuitBreakerConfig(),
		MemoryWarningThreshold: 100 * 1024 * 1024, // 100MB
		MemoryDisableThreshold: 500 * 1024 * 1024, // 500MB
		EnableFallback:         true,
		FallbackTTL:            5 * time.Minute,
	}
}

// QueueResilientExecutor provides resilient execution for queue operations
type QueueResilientExecutor struct {
	config         *ResilienceConfig
	circuitBreaker *CircuitBreaker
	redisClient    redis.UniversalClient
	fallbackQueue  *FallbackQueue
	classifier     *ErrorClassifier
	logger         *logging.Logger
}

// NewQueueResilientExecutor creates a new resilient executor
func NewQueueResilientExecutor(redisOpt asynq.RedisConnOpt, config *ResilienceConfig) (*QueueResilientExecutor, error) {
	if config == nil {
		config = DefaultResilienceConfig()
	}

	// Create Redis client for health checks
	var redisClient redis.UniversalClient
	switch opt := redisOpt.(type) {
	case *asynq.RedisClientOpt:
		redisClient = redis.NewClient(&redis.Options{
			Addr:         opt.Addr,
			Password:     opt.Password,
			DB:           opt.DB,
			ReadTimeout:  1 * time.Second,
			WriteTimeout: 1 * time.Second,
		})
	case *asynq.RedisClusterClientOpt:
		redisClient = redis.NewClusterClient(&redis.ClusterOptions{
			Addrs:        opt.Addrs,
			Password:     opt.Password,
			ReadTimeout:  1 * time.Second,
			WriteTimeout: 1 * time.Second,
		})
	default:
		// If we can't create a Redis client for health checks, we can still provide
		// resilience patterns without memory pressure detection
		redisClient = nil
	}

	executor := &QueueResilientExecutor{
		config:         config,
		circuitBreaker: NewCircuitBreaker("queue-enqueue", config.CircuitBreaker),
		redisClient:    redisClient,
		classifier:     NewErrorClassifier(),
		logger:         logging.NewLogger("queue-resilience"),
	}

	// Initialize fallback queue if enabled
	if config.EnableFallback {
		executor.fallbackQueue = NewFallbackQueue(config.FallbackTTL)
	}

	return executor, nil
}

// ExecuteWithResilience executes a queue operation with full resilience patterns
func (re *QueueResilientExecutor) ExecuteWithResilience(
	ctx context.Context,
	operation string,
	fn func() error,
) error {
	return re.circuitBreaker.Execute(ctx, func() error {
		return re.executeWithRetry(ctx, operation, fn)
	})
}

// executeWithRetry implements retry logic with exponential backoff
func (re *QueueResilientExecutor) executeWithRetry( //nolint:gocognit // Complex retry logic with exponential backoff
	ctx context.Context,
	operation string,
	fn func() error,
) error {
	var lastErr error
	delay := re.config.InitialDelay

	for attempt := 1; attempt <= re.config.MaxRetries; attempt++ {
		// Check context cancellation
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("operation canceled: %w", err)
		}

		// Check Redis memory pressure before attempting
		if err := re.checkMemoryPressure(ctx); err != nil {
			return fmt.Errorf("memory pressure detected: %w", err)
		}

		// Execute the operation
		err := fn()
		if err == nil {
			if attempt > 1 {
				re.logger.Info("Operation '%s' succeeded after %d attempts", operation, attempt)
			}
			return nil
		}

		lastErr = err

		// Classify the error to determine handling strategy
		errorInfo := re.classifier.Classify(err)

		// Check if error is retryable
		if !errorInfo.Retryable || attempt == re.config.MaxRetries {
			// Try fallback if recommended by error classification
			if re.fallbackQueue != nil && errorInfo.UseFallback {
				re.logger.Warn("Primary queue failed (%s), attempting fallback for operation '%s': %v",
					errorInfo.Description, operation, err)
				return re.executeFallback(ctx, operation, fn)
			}
			return fmt.Errorf("operation failed after %d attempts (%s): %w",
				attempt, errorInfo.Description, err)
		}

		// Log retry attempt
		re.logger.Warn("Operation '%s' attempt %d/%d failed: %v, retrying in %v",
			operation, attempt, re.config.MaxRetries, err, delay)

		// Wait with exponential backoff
		select {
		case <-time.After(delay):
			delay = time.Duration(float64(delay) * re.config.BackoffFactor)
			if delay > re.config.MaxDelay {
				delay = re.config.MaxDelay
			}
		case <-ctx.Done():
			return fmt.Errorf("retry canceled: %w", ctx.Err())
		}
	}

	return fmt.Errorf("operation failed after %d attempts: %w", re.config.MaxRetries, lastErr)
}

// checkMemoryPressure checks Redis memory usage and returns error if too high
func (re *QueueResilientExecutor) checkMemoryPressure(ctx context.Context) error {
	// Skip memory pressure check if we don't have a Redis client
	if re.redisClient == nil {
		return nil
	}

	// Get Redis INFO memory
	info, err := re.redisClient.Info(ctx, "memory").Result()
	if err != nil {
		// If we can't check memory, allow the operation (connection issues are handled by retry)
		return nil //nolint:nilerr // Intentionally returning nil to allow operation when Redis info unavailable
	}

	usedMemory, err := re.parseMemoryUsage(info)
	if err != nil {
		re.logger.Warn("Failed to parse Redis memory info: %v", err)
		return nil
	}

	if usedMemory >= re.config.MemoryDisableThreshold {
		return &MemoryPressureError{
			CurrentUsage: usedMemory,
			Threshold:    re.config.MemoryDisableThreshold,
			Severity:     "critical",
		}
	}

	if usedMemory >= re.config.MemoryWarningThreshold {
		re.logger.Warn("Redis memory pressure detected: %d bytes (threshold: %d bytes)",
			usedMemory, re.config.MemoryWarningThreshold)
	}

	return nil
}

// parseMemoryUsage parses used_memory from Redis INFO output
func (re *QueueResilientExecutor) parseMemoryUsage(info string) (int64, error) {
	lines := strings.Split(info, "\n")
	for _, line := range lines {
		if !strings.HasPrefix(line, "used_memory:") {
			continue
		}
		parts := strings.Split(line, ":")
		if len(parts) != 2 {
			continue
		}
		value := strings.TrimSpace(parts[1])
		value = strings.TrimRight(value, "\r") // Remove potential \r
		bytesUsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("failed to parse used_memory value '%s': %w", value, err)
		}
		return bytesUsed, nil
	}
	return 0, fmt.Errorf("used_memory not found in Redis INFO output")
}

// executeFallback attempts to use the fallback queue
func (re *QueueResilientExecutor) executeFallback(ctx context.Context, operation string, fn func() error) error {
	if re.fallbackQueue == nil {
		return fmt.Errorf("fallback queue not available")
	}

	// For memory pressure situations, we'll accept the operation into fallback
	// and return success to prevent cascade failures
	re.logger.Warn("Using fallback queue for operation '%s' due to Redis memory pressure", operation)

	// Check context cancellation
	select {
	case <-ctx.Done():
		return ctx.Err() //nolint:wrapcheck // context.Err() doesn't need wrapping
	default:
		// Store in fallback for later processing and return success
		// This allows the system to gracefully degrade under memory pressure
		err := re.fallbackQueue.Store(ctx, operation, fn)
		if err != nil {
			re.logger.Error("Failed to store operation in fallback queue: %v", err)
			return err
		}

		// Return success - the operation is now in the fallback queue
		// This prevents the calling code from seeing failures during memory pressure
		return nil
	}
}

// Close closes the resilient executor and its resources
func (re *QueueResilientExecutor) Close() error {
	if re.redisClient != nil {
		if err := re.redisClient.Close(); err != nil {
			re.logger.Error("Failed to close Redis client: %v", err)
		}
	}

	if re.fallbackQueue != nil {
		re.fallbackQueue.Close()
	}

	return nil
}

// MemoryPressureError represents a Redis memory pressure condition
type MemoryPressureError struct {
	CurrentUsage int64
	Threshold    int64
	Severity     string
}

func (e *MemoryPressureError) Error() string {
	return fmt.Sprintf("Redis memory pressure (%s): %d bytes used, threshold: %d bytes",
		e.Severity, e.CurrentUsage, e.Threshold)
}
