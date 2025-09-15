//nolint:wrapcheck // Mock implementations pass through errors without wrapping
package mocks

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/lattiam/lattiam/internal/interfaces"
)

// MockResourceExecutor implements worker.ResourceExecutor for testing
type MockResourceExecutor struct {
	mu         sync.RWMutex
	called     bool
	shouldFail bool
	delay      time.Duration
	executions []ExecutorCall
}

// ExecutorCall represents a call to the ResourceExecutor for tracking
type ExecutorCall struct {
	Method       string
	DeploymentID string
	ResourceType string
	Timestamp    time.Time
	Error        error
}

// NewMockResourceExecutor creates a new mock resource executor
func NewMockResourceExecutor() *MockResourceExecutor {
	return &MockResourceExecutor{
		executions: make([]ExecutorCall, 0),
	}
}

// SetShouldFail configures the mock to fail operations
func (m *MockResourceExecutor) SetShouldFail(shouldFail bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shouldFail = shouldFail
}

// SetDelay configures the mock to introduce delays
func (m *MockResourceExecutor) SetDelay(delay time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.delay = delay
}

// Execute processes a deployment and all its resources
func (m *MockResourceExecutor) Execute(ctx context.Context, deployment *interfaces.QueuedDeployment) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.called = true

	// Add delay if configured
	if m.delay > 0 {
		time.Sleep(m.delay)
	}

	// Check for cancellation
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Record the call
	call := ExecutorCall{
		Method:       "Execute",
		DeploymentID: deployment.ID,
		Timestamp:    time.Now(),
	}

	if m.shouldFail {
		call.Error = fmt.Errorf("mock executor failure")
		m.executions = append(m.executions, call)
		return call.Error
	}

	// Simulate some work
	time.Sleep(10 * time.Millisecond)

	call.Error = nil
	m.executions = append(m.executions, call)
	return nil
}

// ExecuteResource processes a single resource
func (m *MockResourceExecutor) ExecuteResource(ctx context.Context, resource *interfaces.Resource, deployment *interfaces.QueuedDeployment) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Record the call
	call := ExecutorCall{
		Method:       "ExecuteResource",
		DeploymentID: deployment.ID,
		ResourceType: resource.Type,
		Timestamp:    time.Now(),
	}

	if m.shouldFail {
		call.Error = fmt.Errorf("mock executor failure")
		m.executions = append(m.executions, call)
		return call.Error
	}

	// Check for cancellation
	if ctx.Err() != nil {
		call.Error = ctx.Err()
		m.executions = append(m.executions, call)
		return call.Error
	}

	// Simulate some work
	time.Sleep(5 * time.Millisecond)

	call.Error = nil
	m.executions = append(m.executions, call)
	return nil
}

// ValidateDeployment validates a deployment before execution
func (m *MockResourceExecutor) ValidateDeployment(deployment *interfaces.QueuedDeployment) error {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.shouldFail {
		return fmt.Errorf("mock validation failure")
	}

	if deployment == nil {
		return fmt.Errorf("deployment is nil")
	}

	if deployment.ID == "" {
		return fmt.Errorf("deployment ID is empty")
	}

	if deployment.Request == nil {
		return fmt.Errorf("deployment request is nil")
	}

	if len(deployment.Request.Resources) == 0 {
		return fmt.Errorf("deployment has no resources")
	}

	return nil
}

// WasCalled returns whether the executor was called
func (m *MockResourceExecutor) WasCalled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.called
}

// GetExecutions returns all recorded executions
func (m *MockResourceExecutor) GetExecutions() []ExecutorCall {
	m.mu.RLock()
	defer m.mu.RUnlock()

	executions := make([]ExecutorCall, len(m.executions))
	copy(executions, m.executions)
	return executions
}

// GetExecutionCount returns the number of executions
func (m *MockResourceExecutor) GetExecutionCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.executions)
}

// Reset clears all recorded state
func (m *MockResourceExecutor) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.called = false
	m.shouldFail = false
	m.delay = 0
	m.executions = make([]ExecutorCall, 0)
}
