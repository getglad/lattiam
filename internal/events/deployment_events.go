// Package events provides event handling and tracking for deployment lifecycle events.
package events

import (
	"sync"
	"time"

	"github.com/lattiam/lattiam/internal/interfaces"
)

// EventType represents the type of deployment event
type EventType string

const (
	// EventStatusChanged is emitted when deployment status changes
	EventStatusChanged EventType = "status_changed"
	// EventResultReady is emitted when deployment result is ready
	EventResultReady EventType = "result_ready"
	// EventError is emitted when an error occurs
	EventError EventType = "error"
)

// DeploymentEvent represents an event in the deployment lifecycle
type DeploymentEvent struct {
	Type         EventType
	DeploymentID string
	Timestamp    time.Time

	// Event-specific data
	Status *interfaces.DeploymentStatus
	Result *interfaces.DeploymentResult
	Error  error
}

// EventHandler is a function that handles deployment events
type EventHandler func(event DeploymentEvent)

// EventBus manages deployment event subscriptions and dispatching
type EventBus struct {
	mu          sync.RWMutex
	handlers    map[EventType][]EventHandler
	synchronous bool // When true, handlers are called synchronously (for testing)
}

// NewEventBus creates a new event bus
func NewEventBus() *EventBus {
	return &EventBus{
		handlers: make(map[EventType][]EventHandler),
	}
}

// NewSynchronousEventBus creates a new event bus that calls handlers synchronously (for testing)
func NewSynchronousEventBus() *EventBus {
	return &EventBus{
		handlers:    make(map[EventType][]EventHandler),
		synchronous: true,
	}
}

// Subscribe registers a handler for specific event types
func (eb *EventBus) Subscribe(eventType EventType, handler EventHandler) {
	eb.mu.Lock()
	defer eb.mu.Unlock()

	eb.handlers[eventType] = append(eb.handlers[eventType], handler)
}

// Publish sends an event to all registered handlers
func (eb *EventBus) Publish(event DeploymentEvent) {
	eb.mu.RLock()
	handlers := eb.handlers[event.Type]
	synchronous := eb.synchronous
	eb.mu.RUnlock()

	if synchronous {
		// Call handlers synchronously for testing
		for _, handler := range handlers {
			handler(event)
		}
	} else {
		// Call handlers asynchronously to avoid blocking
		for _, handler := range handlers {
			go handler(event)
		}
	}
}

// PublishStatusChange is a convenience method for status change events
func (eb *EventBus) PublishStatusChange(deploymentID string, status interfaces.DeploymentStatus) {
	eb.Publish(DeploymentEvent{
		Type:         EventStatusChanged,
		DeploymentID: deploymentID,
		Timestamp:    time.Now(),
		Status:       &status,
	})
}

// PublishResult is a convenience method for result events
func (eb *EventBus) PublishResult(deploymentID string, result *interfaces.DeploymentResult) {
	eb.Publish(DeploymentEvent{
		Type:         EventResultReady,
		DeploymentID: deploymentID,
		Timestamp:    time.Now(),
		Result:       result,
	})
}

// PublishError is a convenience method for error events
func (eb *EventBus) PublishError(deploymentID string, err error) {
	eb.Publish(DeploymentEvent{
		Type:         EventError,
		DeploymentID: deploymentID,
		Timestamp:    time.Now(),
		Error:        err,
	})
}
