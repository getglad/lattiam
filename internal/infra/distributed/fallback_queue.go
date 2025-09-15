package distributed

import (
	"context"
	"sync"
	"time"

	"github.com/lattiam/lattiam/pkg/logging"
)

// FallbackOperation represents an operation stored in the fallback queue
type FallbackOperation struct {
	ID        string
	Operation string
	Timestamp time.Time
	ExpiresAt time.Time
	Attempts  int
	Fn        func() error
}

// FallbackQueue provides a temporary in-memory queue for operations that fail due to Redis issues
type FallbackQueue struct {
	operations map[string]*FallbackOperation
	mutex      sync.RWMutex
	ttl        time.Duration
	logger     *logging.Logger
	stopCh     chan struct{}
	wg         sync.WaitGroup
}

// NewFallbackQueue creates a new fallback queue
func NewFallbackQueue(ttl time.Duration) *FallbackQueue {
	fq := &FallbackQueue{
		operations: make(map[string]*FallbackOperation),
		ttl:        ttl,
		logger:     logging.NewLogger("fallback-queue"),
		stopCh:     make(chan struct{}),
	}

	// Start cleanup goroutine
	fq.wg.Add(1)
	go fq.cleanupLoop()

	return fq
}

// Store stores an operation in the fallback queue
func (fq *FallbackQueue) Store(_ context.Context, operation string, fn func() error) error {
	fq.mutex.Lock()
	defer fq.mutex.Unlock()

	// Generate a unique ID for this operation
	id := generateOperationID(operation)
	now := time.Now()

	op := &FallbackOperation{
		ID:        id,
		Operation: operation,
		Timestamp: now,
		ExpiresAt: now.Add(fq.ttl),
		Attempts:  0,
		Fn:        fn,
	}

	fq.operations[id] = op
	fq.logger.Info("Stored operation '%s' in fallback queue (ID: %s)", operation, id)

	return nil
}

// Retry attempts to retry all stored operations
func (fq *FallbackQueue) Retry() int {
	fq.mutex.Lock()
	operations := make([]*FallbackOperation, 0, len(fq.operations))
	for _, op := range fq.operations {
		if !op.ExpiresAt.Before(time.Now()) {
			operations = append(operations, op)
		}
	}
	fq.mutex.Unlock()

	successCount := 0
	for _, op := range operations {
		if fq.retryOperation(op) {
			successCount++
			fq.remove(op.ID)
		}
	}

	if successCount > 0 {
		fq.logger.Info("Successfully retried %d/%d fallback operations", successCount, len(operations))
	}

	return successCount
}

// retryOperation attempts to retry a single operation
func (fq *FallbackQueue) retryOperation(op *FallbackOperation) bool {
	op.Attempts++

	err := op.Fn()
	if err != nil {
		fq.logger.Warn("Fallback operation '%s' failed (attempt %d): %v", op.Operation, op.Attempts, err)
		return false
	}

	fq.logger.Info("Fallback operation '%s' succeeded after %d attempts", op.Operation, op.Attempts)
	return true
}

// remove removes an operation from the fallback queue
func (fq *FallbackQueue) remove(id string) {
	fq.mutex.Lock()
	defer fq.mutex.Unlock()
	delete(fq.operations, id)
}

// cleanupLoop periodically removes expired operations
func (fq *FallbackQueue) cleanupLoop() {
	defer fq.wg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			fq.cleanup()
		case <-fq.stopCh:
			return
		}
	}
}

// cleanup removes expired operations
func (fq *FallbackQueue) cleanup() {
	fq.mutex.Lock()
	defer fq.mutex.Unlock()

	now := time.Now()
	expiredCount := 0

	for id, op := range fq.operations {
		if op.ExpiresAt.Before(now) {
			delete(fq.operations, id)
			expiredCount++
		}
	}

	if expiredCount > 0 {
		fq.logger.Info("Cleaned up %d expired fallback operations", expiredCount)
	}
}

// Size returns the current number of operations in the fallback queue
func (fq *FallbackQueue) Size() int {
	fq.mutex.RLock()
	defer fq.mutex.RUnlock()
	return len(fq.operations)
}

// Close closes the fallback queue and stops background processes
func (fq *FallbackQueue) Close() {
	close(fq.stopCh)
	fq.wg.Wait()

	fq.mutex.Lock()
	defer fq.mutex.Unlock()

	operationCount := len(fq.operations)
	if operationCount > 0 {
		fq.logger.Warn("Closing fallback queue with %d pending operations", operationCount)
	}

	// Clear all operations
	fq.operations = make(map[string]*FallbackOperation)
}

// generateOperationID generates a unique ID for an operation
func generateOperationID(operation string) string {
	return operation + "-" + time.Now().Format("20060102-150405.000")
}
