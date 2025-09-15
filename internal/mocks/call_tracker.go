package mocks

import (
	"sync"
	"time"
)

// CallTracker provides generic call tracking functionality for mocks
type CallTracker[T any] struct {
	calls []T
	mutex sync.RWMutex
}

// NewCallTracker creates a new call tracker for the specified call type
func NewCallTracker[T any]() *CallTracker[T] {
	return &CallTracker[T]{
		calls: make([]T, 0),
	}
}

// RecordCall records a method call
func (ct *CallTracker[T]) RecordCall(call T) {
	ct.mutex.Lock()
	defer ct.mutex.Unlock()
	ct.calls = append(ct.calls, call)
}

// GetCalls returns all recorded calls
func (ct *CallTracker[T]) GetCalls() []T {
	ct.mutex.RLock()
	defer ct.mutex.RUnlock()
	calls := make([]T, len(ct.calls))
	copy(calls, ct.calls)
	return calls
}

// ClearCalls clears the recorded call history
func (ct *CallTracker[T]) ClearCalls() {
	ct.mutex.Lock()
	defer ct.mutex.Unlock()
	ct.calls = ct.calls[:0] // Keep underlying array capacity
}

// GetCallCount returns the number of recorded calls
func (ct *CallTracker[T]) GetCallCount() int {
	ct.mutex.RLock()
	defer ct.mutex.RUnlock()
	return len(ct.calls)
}

// GetLastCall returns the most recent call, or nil if no calls recorded
func (ct *CallTracker[T]) GetLastCall() *T {
	ct.mutex.RLock()
	defer ct.mutex.RUnlock()
	if len(ct.calls) == 0 {
		return nil
	}
	return &ct.calls[len(ct.calls)-1]
}

// FilterCalls returns calls that match the provided predicate function
func (ct *CallTracker[T]) FilterCalls(predicate func(T) bool) []T {
	ct.mutex.RLock()
	defer ct.mutex.RUnlock()

	var filtered []T
	for _, call := range ct.calls {
		if predicate(call) {
			filtered = append(filtered, call)
		}
	}
	return filtered
}

// CommonCall provides common fields that most call types will have
type CommonCall struct {
	Method    string
	Timestamp time.Time
	Error     error
}

// NewCommonCall creates a CommonCall with current timestamp
func NewCommonCall(method string, err error) CommonCall {
	return CommonCall{
		Method:    method,
		Timestamp: time.Now(),
		Error:     err,
	}
}

// CallWithDeploymentID extends CommonCall with deployment context
type CallWithDeploymentID struct {
	CommonCall
	DeploymentID string
}

// NewCallWithDeploymentID creates a call with deployment context
func NewCallWithDeploymentID(method, deploymentID string, err error) CallWithDeploymentID {
	return CallWithDeploymentID{
		CommonCall:   NewCommonCall(method, err),
		DeploymentID: deploymentID,
	}
}

// CallWithResourceContext extends CommonCall with resource context
type CallWithResourceContext struct {
	CommonCall
	DeploymentID string
	ResourceKey  string
}

// NewCallWithResourceContext creates a call with resource context
func NewCallWithResourceContext(method, deploymentID, resourceKey string, err error) CallWithResourceContext {
	return CallWithResourceContext{
		CommonCall:   NewCommonCall(method, err),
		DeploymentID: deploymentID,
		ResourceKey:  resourceKey,
	}
}

// CallWithProviderContext extends CommonCall with provider context
type CallWithProviderContext struct {
	CommonCall
	ProviderType string
}

// NewCallWithProviderContext creates a call with provider context
func NewCallWithProviderContext(method, providerType string, err error) CallWithProviderContext {
	return CallWithProviderContext{
		CommonCall:   NewCommonCall(method, err),
		ProviderType: providerType,
	}
}

// Call represents a basic method call with optional parameters
type Call struct {
	CommonCall
	Parameters map[string]interface{}
}

// NewCall creates a basic call with optional parameters
func NewCall(method string, parameters map[string]interface{}) Call {
	return Call{
		CommonCall: NewCommonCall(method, nil),
		Parameters: parameters,
	}
}
