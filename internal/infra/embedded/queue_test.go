package embedded

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/interfaces"
)

func TestQueue_Enqueue(t *testing.T) { //nolint:funlen // Test function with comprehensive test cases
	t.Parallel()

	t.Run("SuccessfulEnqueue", func(t *testing.T) {
		t.Parallel()
		queue := NewQueue(10)
		deployment := &interfaces.QueuedDeployment{
			ID:     "dep-123",
			Status: interfaces.DeploymentStatusQueued,
		}

		err := queue.Enqueue(context.Background(), deployment)
		require.NoError(t, err)
		assert.Equal(t, 1, queue.Size())
	})

	t.Run("NilDeployment", func(t *testing.T) {
		t.Parallel()
		queue := NewQueue(10)
		err := queue.Enqueue(context.Background(), nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "deployment is nil")
	})

	t.Run("EmptyID", func(t *testing.T) {
		t.Parallel()
		queue := NewQueue(10)
		deployment := &interfaces.QueuedDeployment{
			Status: interfaces.DeploymentStatusQueued,
		}
		err := queue.Enqueue(context.Background(), deployment)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "deployment ID is empty")
	})

	t.Run("QueueFull", func(t *testing.T) {
		t.Parallel()
		queue := NewQueue(1) // Small capacity
		deployment1 := &interfaces.QueuedDeployment{
			ID:     "dep-1",
			Status: interfaces.DeploymentStatusQueued,
		}
		deployment2 := &interfaces.QueuedDeployment{
			ID:     "dep-2",
			Status: interfaces.DeploymentStatusQueued,
		}

		// First should succeed
		err := queue.Enqueue(context.Background(), deployment1)
		require.NoError(t, err)

		// Second should fail (queue full)
		err = queue.Enqueue(context.Background(), deployment2)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "queue is full")
	})

	t.Run("ContextCanceled", func(t *testing.T) {
		t.Parallel()
		queue := NewQueue(10)
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		deployment := &interfaces.QueuedDeployment{
			ID:     "dep-123",
			Status: interfaces.DeploymentStatusQueued,
		}

		err := queue.Enqueue(ctx, deployment)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "canceled")
	})

	t.Run("QueueClosed", func(t *testing.T) {
		t.Parallel()
		queue := NewQueue(10)
		queue.Close()

		deployment := &interfaces.QueuedDeployment{
			ID:     "dep-123",
			Status: interfaces.DeploymentStatusQueued,
		}

		err := queue.Enqueue(context.Background(), deployment)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "queue is closed")
	})
}

func TestQueue_Dequeue(t *testing.T) { // Test function with comprehensive test cases
	t.Parallel()

	t.Run("SuccessfulDequeue", func(t *testing.T) {
		t.Parallel()
		queue := NewQueue(10)
		deployment := &interfaces.QueuedDeployment{
			ID:     "dep-123",
			Status: interfaces.DeploymentStatusQueued,
		}

		// Enqueue
		err := queue.Enqueue(context.Background(), deployment)
		require.NoError(t, err)

		// Dequeue
		got, err := queue.Dequeue(context.Background())
		require.NoError(t, err)
		assert.NotNil(t, got)
		assert.Equal(t, deployment.ID, got.ID)
		assert.Equal(t, 0, queue.Size())
	})

	t.Run("EmptyQueue", func(t *testing.T) {
		t.Parallel()
		queue := NewQueue(10)
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		// Should timeout
		got, err := queue.Dequeue(ctx)
		require.Error(t, err)
		assert.Nil(t, got)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
	})

	t.Run("ContextCanceled", func(t *testing.T) {
		t.Parallel()
		queue := NewQueue(10)
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		got, err := queue.Dequeue(ctx)
		require.Error(t, err)
		assert.Nil(t, got)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("QueueClosed", func(t *testing.T) {
		t.Parallel()
		queue := NewQueue(10)

		// Close queue while dequeue is waiting
		go func() {
			time.Sleep(100 * time.Millisecond)
			queue.Close()
		}()

		got, err := queue.Dequeue(context.Background())
		require.Error(t, err)
		assert.Nil(t, got)
		assert.Contains(t, err.Error(), "queue is closed")
	})
}

func TestQueue_Cancel(t *testing.T) { // Test function with comprehensive test cases
	t.Parallel()

	t.Run("SuccessfulCancel", func(t *testing.T) {
		t.Parallel()
		queue := NewQueue(10)
		deployment := &interfaces.QueuedDeployment{
			ID:     "dep-123",
			Status: interfaces.DeploymentStatusQueued,
		}

		// Enqueue
		err := queue.Enqueue(context.Background(), deployment)
		require.NoError(t, err)

		// Cancel
		err = queue.Cancel(context.Background(), deployment.ID)
		require.NoError(t, err)
	})

	t.Run("NonExistentDeployment", func(t *testing.T) {
		t.Parallel()
		queue := NewQueue(10)
		err := queue.Cancel(context.Background(), "non-existent")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("EmptyID", func(t *testing.T) {
		t.Parallel()
		queue := NewQueue(10)
		err := queue.Cancel(context.Background(), "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "deployment ID is empty")
	})
}

func TestQueue_Close(t *testing.T) { // Test function with comprehensive test cases
	t.Parallel()

	t.Run("CloseQueue", func(t *testing.T) {
		t.Parallel()
		queue := NewQueue(10)

		// Enqueue some items
		for i := 0; i < 3; i++ {
			deployment := &interfaces.QueuedDeployment{
				ID:     fmt.Sprintf("dep-%d", i),
				Status: interfaces.DeploymentStatusQueued,
			}
			err := queue.Enqueue(context.Background(), deployment)
			require.NoError(t, err)
		}

		// Close
		queue.Close()

		// Can't enqueue after close
		deployment := &interfaces.QueuedDeployment{
			ID:     "dep-new",
			Status: interfaces.DeploymentStatusQueued,
		}
		err := queue.Enqueue(context.Background(), deployment)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "queue is closed")
	})

	t.Run("MultipleClosesSafe", func(t *testing.T) {
		t.Parallel()
		queue := NewQueue(10)

		// Multiple closes should be safe
		queue.Close()
		queue.Close()
		queue.Close()

		// Should still be closed
		deployment := &interfaces.QueuedDeployment{
			ID:     "dep-123",
			Status: interfaces.DeploymentStatusQueued,
		}
		err := queue.Enqueue(context.Background(), deployment)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "queue is closed")
	})
}

func TestQueue_Properties(t *testing.T) { // Test function with comprehensive test cases
	t.Parallel()

	t.Run("DefaultCapacity", func(t *testing.T) {
		t.Parallel()
		queue := NewQueue(0) // Should use default
		assert.Equal(t, 100, queue.Capacity())
	})

	t.Run("CustomCapacity", func(t *testing.T) {
		t.Parallel()
		queue := NewQueue(50)
		assert.Equal(t, 50, queue.Capacity())
	})

	t.Run("SizeTracking", func(t *testing.T) {
		t.Parallel()
		queue := NewQueue(10)
		assert.Equal(t, 0, queue.Size())

		// Add items
		for i := 0; i < 3; i++ {
			deployment := &interfaces.QueuedDeployment{
				ID:     fmt.Sprintf("dep-%d", i),
				Status: interfaces.DeploymentStatusQueued,
			}
			err := queue.Enqueue(context.Background(), deployment)
			require.NoError(t, err)
		}
		assert.Equal(t, 3, queue.Size())

		// Remove one
		_, err := queue.Dequeue(context.Background())
		require.NoError(t, err)
		assert.Equal(t, 2, queue.Size())
	})
}
