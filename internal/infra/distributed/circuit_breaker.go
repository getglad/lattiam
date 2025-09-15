// Package distributed provides distributed computing components including circuit breakers, queues, and error handling.
package distributed

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/lattiam/lattiam/pkg/logging"
)

// CircuitState represents the current state of the circuit breaker
type CircuitState int32

const (
	// CircuitClosed indicates the circuit breaker is closed and allowing requests
	CircuitClosed CircuitState = iota
	// CircuitOpen indicates the circuit breaker is open and blocking requests
	CircuitOpen
	// CircuitHalfOpen indicates the circuit breaker is half-open and testing with limited requests
	CircuitHalfOpen
)

const (
	unknownState = "unknown"
)

// String returns the string representation of CircuitState
func (s CircuitState) String() string {
	switch s {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half-open"
	default:
		return unknownState
	}
}

// CircuitBreakerConfig configures circuit breaker behavior
type CircuitBreakerConfig struct {
	// MaxFailures is the maximum number of failures before opening
	MaxFailures int64
	// Timeout is how long to wait before attempting to close after opening
	Timeout time.Duration
	// ReadyToTrip is called whenever a request fails; it returns true if the circuit should trip
	ReadyToTrip func(counts Counts) bool
	// OnStateChange is called whenever the circuit breaker changes state
	OnStateChange func(name string, from CircuitState, to CircuitState)
}

// DefaultCircuitBreakerConfig returns sensible defaults
func DefaultCircuitBreakerConfig() *CircuitBreakerConfig {
	return &CircuitBreakerConfig{
		MaxFailures: 5,
		Timeout:     30 * time.Second,
		ReadyToTrip: func(counts Counts) bool {
			// Trip if failure rate > 50% with at least 10 requests
			return counts.Requests >= 10 && counts.TotalFailures > counts.Requests/2
		},
	}
}

// Counts represents circuit breaker statistics
type Counts struct {
	Requests             int64
	TotalSuccesses       int64
	TotalFailures        int64
	ConsecutiveSuccesses int64
	ConsecutiveFailures  int64
}

// CircuitBreaker implements the circuit breaker pattern
type CircuitBreaker struct {
	name   string
	config *CircuitBreakerConfig
	logger *logging.Logger
	mutex  sync.RWMutex
	state  CircuitState
	counts Counts
	expiry time.Time
}

// NewCircuitBreaker creates a new circuit breaker
func NewCircuitBreaker(name string, config *CircuitBreakerConfig) *CircuitBreaker {
	if config == nil {
		config = DefaultCircuitBreakerConfig()
	}

	return &CircuitBreaker{
		name:   name,
		config: config,
		logger: logging.NewLogger("circuit-breaker"),
		state:  CircuitClosed,
	}
}

// Execute runs a function through the circuit breaker
func (cb *CircuitBreaker) Execute(ctx context.Context, fn func() error) error {
	generation, err := cb.beforeRequest()
	if err != nil {
		return err
	}

	defer func() {
		if r := recover(); r != nil {
			cb.afterRequest(generation, false)
			panic(r)
		}
	}()

	// Check context before execution
	if err := ctx.Err(); err != nil {
		cb.afterRequest(generation, false)
		return err //nolint:wrapcheck // context.Err() doesn't need wrapping
	}

	// Execute the function
	err = fn()
	success := err == nil
	cb.afterRequest(generation, success)

	return err
}

// beforeRequest is called before a request is executed
func (cb *CircuitBreaker) beforeRequest() (int64, error) {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	now := time.Now()
	state, generation := cb.currentState(now)

	if state == CircuitOpen {
		return generation, errors.New("circuit breaker is open")
	} else if state == CircuitHalfOpen && cb.counts.Requests >= 1 {
		// Only allow one request in half-open state
		return generation, errors.New("circuit breaker is half-open, request limit reached")
	}

	cb.counts.Requests++
	return generation, nil
}

// afterRequest is called after a request is executed
func (cb *CircuitBreaker) afterRequest(before int64, success bool) {
	cb.mutex.Lock()
	defer cb.mutex.Unlock()

	now := time.Now()
	state, generation := cb.currentState(now)
	if generation != before {
		// Circuit state changed during execution, ignore this result
		return
	}

	if success {
		cb.onSuccess(state, now)
	} else {
		cb.onFailure(state, now)
	}
}

// onSuccess handles a successful request
func (cb *CircuitBreaker) onSuccess(state CircuitState, now time.Time) {
	switch state {
	case CircuitClosed:
		cb.counts.TotalSuccesses++
		cb.counts.ConsecutiveSuccesses++
		cb.counts.ConsecutiveFailures = 0
	case CircuitHalfOpen:
		cb.counts.TotalSuccesses++
		cb.counts.ConsecutiveSuccesses++
		cb.counts.ConsecutiveFailures = 0
		// After success in half-open, close the circuit
		cb.setState(CircuitClosed, now)
	case CircuitOpen:
		// Success shouldn't happen in open state, but handle gracefully
		// Do nothing - circuit remains open
	}
}

// onFailure handles a failed request
func (cb *CircuitBreaker) onFailure(state CircuitState, now time.Time) {
	switch state {
	case CircuitClosed:
		cb.counts.TotalFailures++
		cb.counts.ConsecutiveFailures++
		cb.counts.ConsecutiveSuccesses = 0
		if cb.config.ReadyToTrip != nil && cb.config.ReadyToTrip(cb.counts) {
			cb.setState(CircuitOpen, now)
		}
	case CircuitHalfOpen:
		cb.counts.TotalFailures++
		cb.counts.ConsecutiveFailures++
		cb.counts.ConsecutiveSuccesses = 0
		// Failure in half-open goes back to open
		cb.setState(CircuitOpen, now)
	case CircuitOpen:
		// Failure in open state is expected
		cb.counts.TotalFailures++
	}
}

// currentState returns the current state and generation
func (cb *CircuitBreaker) currentState(now time.Time) (state CircuitState, generation int64) {
	switch cb.state {
	case CircuitClosed:
		return CircuitClosed, 0
	case CircuitOpen:
		if cb.expiry.Before(now) {
			cb.setState(CircuitHalfOpen, now)
			return CircuitHalfOpen, time.Now().UnixNano()
		}
		return CircuitOpen, 0
	case CircuitHalfOpen:
		return CircuitHalfOpen, time.Now().UnixNano()
	default:
		return CircuitClosed, 0
	}
}

// setState changes the circuit breaker state
func (cb *CircuitBreaker) setState(state CircuitState, now time.Time) {
	if cb.state == state {
		return
	}

	prev := cb.state
	cb.state = state

	// Reset counts on state change
	cb.counts = Counts{}

	switch state {
	case CircuitClosed:
		cb.expiry = time.Time{}
	case CircuitOpen:
		cb.expiry = now.Add(cb.config.Timeout)
	case CircuitHalfOpen:
		cb.expiry = time.Time{}
	}

	// Log state change
	cb.logger.Info("Circuit breaker '%s' changed state from %s to %s", cb.name, prev.String(), state.String())

	// Call callback if configured
	if cb.config.OnStateChange != nil {
		cb.config.OnStateChange(cb.name, prev, state)
	}
}

// State returns the current circuit breaker state
func (cb *CircuitBreaker) State() CircuitState {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()
	return cb.state
}

// Counts returns the current circuit breaker counts
func (cb *CircuitBreaker) Counts() Counts {
	cb.mutex.RLock()
	defer cb.mutex.RUnlock()
	return cb.counts
}
