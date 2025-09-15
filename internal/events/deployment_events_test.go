package events

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/interfaces"
)

func TestEventBus(t *testing.T) { //nolint:funlen // Test function with comprehensive test cases
	t.Parallel()

	t.Run("StatusChangeEvent", func(t *testing.T) {
		t.Parallel()
		eb := NewEventBus()

		var received DeploymentEvent
		var wg sync.WaitGroup
		wg.Add(1)

		eb.Subscribe(EventStatusChanged, func(event DeploymentEvent) {
			received = event
			wg.Done()
		})

		// Publish status change
		eb.PublishStatusChange("test-deployment", interfaces.DeploymentStatusCompleted)

		// Wait for event to be received
		wg.Wait()

		// Verify event
		assert.Equal(t, EventStatusChanged, received.Type)
		assert.Equal(t, "test-deployment", received.DeploymentID)
		assert.NotNil(t, received.Status)
		assert.Equal(t, interfaces.DeploymentStatusCompleted, *received.Status)
	})

	t.Run("ResultEvent", func(t *testing.T) {
		t.Parallel()
		eb := NewEventBus()

		var received DeploymentEvent
		var wg sync.WaitGroup
		wg.Add(1)

		eb.Subscribe(EventResultReady, func(event DeploymentEvent) {
			received = event
			wg.Done()
		})

		// Publish result
		result := &interfaces.DeploymentResult{
			DeploymentID: "test-deployment",
			Success:      true,
			CompletedAt:  time.Now(),
		}
		eb.PublishResult("test-deployment", result)

		// Wait for event to be received
		wg.Wait()

		// Verify event
		assert.Equal(t, EventResultReady, received.Type)
		assert.Equal(t, "test-deployment", received.DeploymentID)
		assert.NotNil(t, received.Result)
		assert.Equal(t, result, received.Result)
	})

	t.Run("MultipleSubscribers", func(t *testing.T) {
		t.Parallel()
		eb := NewEventBus()

		var count int
		var mu sync.Mutex
		var wg sync.WaitGroup
		wg.Add(3)

		// Subscribe 3 handlers
		for i := 0; i < 3; i++ {
			eb.Subscribe(EventStatusChanged, func(_ DeploymentEvent) {
				mu.Lock()
				count++
				mu.Unlock()
				wg.Done()
			})
		}

		// Publish once
		eb.PublishStatusChange("test-deployment", interfaces.DeploymentStatusCompleted)

		// Wait for all handlers
		wg.Wait()

		// Verify all handlers were called
		assert.Equal(t, 3, count)
	})
}

func TestTrackerAdapter(t *testing.T) { // Test function with comprehensive test cases
	t.Parallel()

	// Mock tracker
	tracker := &mockTracker{
		statuses: make(map[string]interfaces.DeploymentStatus),
		results:  make(map[string]*interfaces.DeploymentResult),
	}

	eb := NewEventBus()
	ConnectTrackerToEventBus(eb, tracker)

	//nolint:paralleltest // subtests share tracker state
	t.Run("StatusUpdate", func(t *testing.T) {
		// Publish status change
		eb.PublishStatusChange("test-deployment", interfaces.DeploymentStatusCompleted)

		// Give async handler time to process
		time.Sleep(50 * time.Millisecond)

		// Verify tracker was updated
		statusPtr, err := tracker.GetStatus("test-deployment")
		require.NoError(t, err)
		require.NotNil(t, statusPtr)
		assert.Equal(t, interfaces.DeploymentStatusCompleted, *statusPtr)
	})

	//nolint:paralleltest // subtests share tracker state
	t.Run("ResultUpdate", func(t *testing.T) {
		// Publish result
		result := &interfaces.DeploymentResult{
			DeploymentID: "test-deployment",
			Success:      true,
			CompletedAt:  time.Now(),
		}
		eb.PublishResult("test-deployment", result)

		// Give async handler time to process
		time.Sleep(50 * time.Millisecond)

		// Verify tracker was updated
		storedResult, err := tracker.GetResult("test-deployment")
		require.NoError(t, err)
		require.NotNil(t, storedResult)
		assert.Equal(t, result, storedResult)
	})
}

// mockTracker implements a simple in-memory tracker for testing
type mockTracker struct {
	mu          sync.Mutex
	statuses    map[string]interfaces.DeploymentStatus
	results     map[string]*interfaces.DeploymentResult
	deployments map[string]*interfaces.QueuedDeployment
}

func (m *mockTracker) Register(deployment *interfaces.QueuedDeployment) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deployments == nil {
		m.deployments = make(map[string]*interfaces.QueuedDeployment)
	}
	m.deployments[deployment.ID] = deployment
	m.statuses[deployment.ID] = deployment.Status
	return nil
}

func (m *mockTracker) GetByID(deploymentID string) (*interfaces.QueuedDeployment, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.deployments == nil {
		return nil, fmt.Errorf("deployment %s not found", deploymentID)
	}
	deployment, exists := m.deployments[deploymentID]
	if !exists {
		return nil, fmt.Errorf("deployment %s not found", deploymentID)
	}
	return deployment, nil
}

func (m *mockTracker) GetStatus(deploymentID string) (*interfaces.DeploymentStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	status, exists := m.statuses[deploymentID]
	if !exists {
		return nil, nil
	}
	return &status, nil
}

func (m *mockTracker) SetStatus(deploymentID string, status interfaces.DeploymentStatus) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.statuses[deploymentID] = status
	return nil
}

func (m *mockTracker) GetResult(deploymentID string) (*interfaces.DeploymentResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.results[deploymentID], nil
}

func (m *mockTracker) SetResult(deploymentID string, result *interfaces.DeploymentResult) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.results[deploymentID] = result
	return nil
}

func (m *mockTracker) List(_ interfaces.DeploymentFilter) ([]*interfaces.QueuedDeployment, error) {
	return nil, nil
}

func (m *mockTracker) Remove(deploymentID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.statuses, deploymentID)
	delete(m.results, deploymentID)
	delete(m.deployments, deploymentID)
	return nil
}

func (m *mockTracker) Load(_ interfaces.StateStore) error {
	// No-op for test mock
	return nil
}
