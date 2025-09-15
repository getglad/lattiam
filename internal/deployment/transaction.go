package deployment

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/lattiam/lattiam/internal/interfaces"
)

// TransactionCoordinator ensures atomic operations across multiple components
type TransactionCoordinator struct {
	mu           sync.Mutex
	transactions map[string]*Transaction
}

// Transaction represents an atomic operation across multiple components
type Transaction struct {
	ID            string
	Operations    []Operation
	Compensations []Compensation
	State         TransactionState
	StartedAt     time.Time
	CompletedAt   *time.Time
}

// Operation represents a single operation in a transaction
type Operation struct {
	Name string
	Func func() error
}

// Compensation represents a rollback operation
type Compensation struct {
	Name string
	Func func() error
}

// TransactionState represents the state of a transaction
type TransactionState int

const (
	// TransactionPending indicates transaction is created but not yet started
	TransactionPending TransactionState = iota
	// TransactionInProgress indicates transaction is currently executing
	TransactionInProgress
	// TransactionCommitted indicates all operations completed successfully
	TransactionCommitted
	// TransactionRolledBack indicates transaction was rolled back due to failure
	TransactionRolledBack
	// TransactionFailed indicates rollback also failed
	TransactionFailed
)

// NewTransactionCoordinator creates a new transaction coordinator
func NewTransactionCoordinator() *TransactionCoordinator {
	return &TransactionCoordinator{
		transactions: make(map[string]*Transaction),
	}
}

// BeginTransaction starts a new transaction
func (tc *TransactionCoordinator) BeginTransaction(id string) *Transaction {
	tc.mu.Lock()
	defer tc.mu.Unlock()

	tx := &Transaction{
		ID:            id,
		State:         TransactionPending,
		StartedAt:     time.Now(),
		Operations:    []Operation{},
		Compensations: []Compensation{},
	}

	tc.transactions[id] = tx
	return tx
}

// AddOperation adds an operation to the transaction with its compensation
func (tx *Transaction) AddOperation(op Operation, comp Compensation) {
	tx.Operations = append(tx.Operations, op)
	tx.Compensations = append([]Compensation{comp}, tx.Compensations...) // Prepend for LIFO order
}

// Execute runs all operations in the transaction
func (tc *TransactionCoordinator) Execute(ctx context.Context, txID string) error {
	tc.mu.Lock()
	tx, exists := tc.transactions[txID]
	if !exists {
		tc.mu.Unlock()
		return fmt.Errorf("transaction %s not found", txID)
	}
	tx.State = TransactionInProgress
	tc.mu.Unlock()

	// Execute operations in order
	completedOps := 0
	for i, op := range tx.Operations {
		select {
		case <-ctx.Done():
			return tc.rollback(tx, completedOps, fmt.Errorf("transaction canceled: %w", ctx.Err()))
		default:
		}

		if err := op.Func(); err != nil {
			return tc.rollback(tx, completedOps, fmt.Errorf("operation %s failed: %w", op.Name, err))
		}
		completedOps = i + 1
	}

	// All operations succeeded
	tc.mu.Lock()
	tx.State = TransactionCommitted
	now := time.Now()
	tx.CompletedAt = &now
	tc.mu.Unlock()

	return nil
}

// rollback executes compensation functions for completed operations
func (tc *TransactionCoordinator) rollback(tx *Transaction, completedOps int, originalErr error) error {
	tc.mu.Lock()
	tx.State = TransactionRolledBack
	tc.mu.Unlock()

	// Execute compensations for completed operations (in reverse order)
	var rollbackErrors []error
	for i := 0; i < completedOps; i++ {
		comp := tx.Compensations[i]
		if err := comp.Func(); err != nil {
			rollbackErrors = append(rollbackErrors, fmt.Errorf("compensation %s failed: %w", comp.Name, err))
		}
	}

	tc.mu.Lock()
	if len(rollbackErrors) > 0 {
		tx.State = TransactionFailed
	}
	now := time.Now()
	tx.CompletedAt = &now
	tc.mu.Unlock()

	// Return original error with any rollback errors
	if len(rollbackErrors) > 0 {
		return fmt.Errorf("transaction failed: %w (rollback errors: %v)", originalErr, rollbackErrors)
	}
	return originalErr
}

// CreateDeploymentTransaction creates an atomic deployment creation transaction
func CreateDeploymentTransaction(
	tc *TransactionCoordinator,
	tracker interfaces.DeploymentTracker,
	queue interfaces.DeploymentQueue,
	deployment *interfaces.QueuedDeployment,
) error {
	tx := tc.BeginTransaction(deployment.ID)

	// Operation 1: Register with tracker
	tx.AddOperation(
		Operation{
			Name: "register_tracker",
			Func: func() error {
				return tracker.Register(deployment)
			},
		},
		Compensation{
			Name: "unregister_tracker",
			Func: func() error {
				return tracker.Remove(deployment.ID)
			},
		},
	)

	// Operation 2: Enqueue deployment
	tx.AddOperation(
		Operation{
			Name: "enqueue_deployment",
			Func: func() error {
				ctx := context.Background()
				return queue.Enqueue(ctx, deployment)
			},
		},
		Compensation{
			Name: "dequeue_deployment",
			Func: func() error {
				// Most queues don't support dequeue, so we mark it as canceled
				return tracker.SetStatus(deployment.ID, interfaces.DeploymentStatusCanceled)
			},
		},
	)

	// Execute the transaction
	ctx := context.Background()
	return tc.Execute(ctx, deployment.ID)
}
