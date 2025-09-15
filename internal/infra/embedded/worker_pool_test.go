package embedded

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/interfaces"
)

// TestWorkerPool_Start verifies the worker pool starts correctly
func TestWorkerPool_Start(t *testing.T) {
	t.Parallel()
	queue := NewQueue(10)
	tracker := NewTracker()
	executor := func(_ context.Context, _ *interfaces.QueuedDeployment) error {
		return nil
	}

	pool, err := NewWorkerPool(WorkerPoolConfig{
		Queue:      queue,
		Tracker:    tracker,
		Executor:   executor,
		MinWorkers: 1,
		MaxWorkers: 2,
	})
	require.NoError(t, err)

	// Should be able to start
	pool.Start()

	// Starting again should be safe (idempotent)
	pool.Start()

	// Stop the pool
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err = pool.Stop(ctx)
	assert.NoError(t, err)
}

// TestWorkerPool_PanicRecovery verifies the pool recovers from panics
//
//nolint:funlen // Comprehensive panic recovery test requiring multiple scenarios
func TestWorkerPool_PanicRecovery(t *testing.T) {
	t.Parallel()
	queue := NewQueue(10)
	tracker := NewTracker()

	panicCount := int32(0)
	normalCount := int32(0)

	executor := func(_ context.Context, d *interfaces.QueuedDeployment) error {
		if d.ID == "panic-deployment" {
			atomic.AddInt32(&panicCount, 1)
			panic("executor panic!")
		}
		atomic.AddInt32(&normalCount, 1)
		return nil
	}

	pool, err := NewWorkerPool(WorkerPoolConfig{
		Queue:      queue,
		Tracker:    tracker,
		Executor:   executor,
		MinWorkers: 2,
		MaxWorkers: 2,
	})
	require.NoError(t, err)

	pool.Start()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		err := pool.Stop(ctx)
		assert.NoError(t, err)
	}()

	// Register and enqueue a deployment that will panic
	panicDeployment := &interfaces.QueuedDeployment{
		ID:        "panic-deployment",
		Status:    interfaces.DeploymentStatusQueued,
		CreatedAt: time.Now(),
		Request:   &interfaces.DeploymentRequest{},
	}
	err = tracker.Register(panicDeployment)
	require.NoError(t, err)
	err = queue.Enqueue(context.Background(), panicDeployment)
	require.NoError(t, err)

	// Register and enqueue a normal deployment after the panic
	normalDeployment := &interfaces.QueuedDeployment{
		ID:        "normal-deployment",
		Status:    interfaces.DeploymentStatusQueued,
		CreatedAt: time.Now(),
		Request:   &interfaces.DeploymentRequest{},
	}
	err = tracker.Register(normalDeployment)
	require.NoError(t, err)
	err = queue.Enqueue(context.Background(), normalDeployment)
	require.NoError(t, err)

	// Wait for processing
	time.Sleep(200 * time.Millisecond)

	// Verify the panic deployment was attempted
	assert.Equal(t, int32(1), atomic.LoadInt32(&panicCount))

	// Verify the normal deployment was still processed (pool didn't die)
	assert.Equal(t, int32(1), atomic.LoadInt32(&normalCount))

	// Check status of panic deployment - should be failed
	panicStatus, err := tracker.GetStatus("panic-deployment")
	require.NoError(t, err)
	assert.Equal(t, interfaces.DeploymentStatusFailed, *panicStatus)

	// Check status of normal deployment - should be completed
	normalStatus, err := tracker.GetStatus("normal-deployment")
	require.NoError(t, err)
	assert.Equal(t, interfaces.DeploymentStatusCompleted, *normalStatus)
}

// TestWorkerPool_ErrorHandling verifies proper error handling
func TestWorkerPool_ErrorHandling(t *testing.T) {
	t.Parallel()
	queue := NewQueue(10)
	tracker := NewTracker()

	testError := errors.New("test execution error")
	executor := func(_ context.Context, d *interfaces.QueuedDeployment) error {
		if d.ID == "error-deployment" {
			return testError
		}
		return nil
	}

	pool, err := NewWorkerPool(WorkerPoolConfig{
		Queue:      queue,
		Tracker:    tracker,
		Executor:   executor,
		MinWorkers: 1,
		MaxWorkers: 1,
	})
	require.NoError(t, err)

	pool.Start()
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		err := pool.Stop(ctx)
		assert.NoError(t, err)
	}()

	// Create deployment that will error
	deployment := &interfaces.QueuedDeployment{
		ID:        "error-deployment",
		Status:    interfaces.DeploymentStatusQueued,
		CreatedAt: time.Now(),
		Request:   &interfaces.DeploymentRequest{},
	}

	err = tracker.Register(deployment)
	require.NoError(t, err)
	err = queue.Enqueue(context.Background(), deployment)
	require.NoError(t, err)

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Verify deployment failed
	status, err := tracker.GetStatus("error-deployment")
	require.NoError(t, err)
	assert.Equal(t, interfaces.DeploymentStatusFailed, *status)

	// Note: The current tracker implementation doesn't preserve LastError
	// This would require enhancing the tracker to store error information
	// For now, we just verify the status changed to failed
}

// TestWorkerPool_GracefulShutdown verifies clean shutdown
func TestWorkerPool_GracefulShutdown(t *testing.T) {
	t.Parallel()
	queue := NewQueue(10)
	tracker := NewTracker()

	processingStarted := make(chan struct{})
	processingBlock := make(chan struct{})

	executor := func(ctx context.Context, _ *interfaces.QueuedDeployment) error {
		close(processingStarted)
		select {
		case <-processingBlock:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	pool, err := NewWorkerPool(WorkerPoolConfig{
		Queue:      queue,
		Tracker:    tracker,
		Executor:   executor,
		MinWorkers: 1,
		MaxWorkers: 1,
	})
	require.NoError(t, err)

	pool.Start()

	// Enqueue a deployment
	deployment := &interfaces.QueuedDeployment{
		ID:        "shutdown-test",
		Status:    interfaces.DeploymentStatusQueued,
		CreatedAt: time.Now(),
		Request:   &interfaces.DeploymentRequest{},
	}

	err = tracker.Register(deployment)
	require.NoError(t, err)
	err = queue.Enqueue(context.Background(), deployment)
	require.NoError(t, err)

	// Wait for processing to start
	<-processingStarted

	// Start shutdown in background
	shutdownDone := make(chan error)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		shutdownDone <- pool.Stop(ctx)
	}()

	// Let shutdown start
	time.Sleep(50 * time.Millisecond)

	// Unblock the executor
	close(processingBlock)

	// Verify shutdown completes
	err = <-shutdownDone
	assert.NoError(t, err)
}

// TestWorkerPool_ConcurrentStop verifies multiple stop calls are safe
func TestWorkerPool_ConcurrentStop(t *testing.T) {
	t.Parallel()
	queue := NewQueue(10)
	tracker := NewTracker()
	executor := func(_ context.Context, _ *interfaces.QueuedDeployment) error {
		return nil
	}

	pool, err := NewWorkerPool(WorkerPoolConfig{
		Queue:      queue,
		Tracker:    tracker,
		Executor:   executor,
		MinWorkers: 1,
		MaxWorkers: 1,
	})
	require.NoError(t, err)

	pool.Start()

	// Call stop concurrently
	stopErrors := make(chan error, 3)
	for i := 0; i < 3; i++ {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			stopErrors <- pool.Stop(ctx)
		}()
	}

	// All should complete without error
	for i := 0; i < 3; i++ {
		err := <-stopErrors
		assert.NoError(t, err)
	}
}

// TestWorkerPool_InvalidConfig verifies configuration validation
func TestWorkerPool_InvalidConfig(t *testing.T) {
	t.Parallel()
	queue := NewQueue(10)
	tracker := NewTracker()
	executor := func(_ context.Context, _ *interfaces.QueuedDeployment) error {
		return nil
	}

	tests := []struct {
		name   string
		config WorkerPoolConfig
		errMsg string
	}{
		{
			name: "nil queue",
			config: WorkerPoolConfig{
				Queue:    nil,
				Tracker:  tracker,
				Executor: executor,
			},
			errMsg: "queue is required",
		},
		{
			name: "nil tracker",
			config: WorkerPoolConfig{
				Queue:    queue,
				Tracker:  nil,
				Executor: executor,
			},
			errMsg: "tracker is required",
		},
		{
			name: "nil executor",
			config: WorkerPoolConfig{
				Queue:    queue,
				Tracker:  tracker,
				Executor: nil,
			},
			errMsg: "executor is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := NewWorkerPool(tt.config)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errMsg)
		})
	}
}

// TestWorkerPool_WorkerCount verifies worker count management
func TestWorkerPool_WorkerCount(t *testing.T) {
	t.Parallel()
	queue := NewQueue(10)
	tracker := NewTracker()

	blockExecution := make(chan struct{})
	executor := func(_ context.Context, _ *interfaces.QueuedDeployment) error {
		<-blockExecution
		return nil
	}

	pool, err := NewWorkerPool(WorkerPoolConfig{
		Queue:      queue,
		Tracker:    tracker,
		Executor:   executor,
		MinWorkers: 2,
		MaxWorkers: 5,
	})
	require.NoError(t, err)

	pool.Start()
	defer func() {
		close(blockExecution)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		err := pool.Stop(ctx)
		assert.NoError(t, err)
	}()

	// Worker pool should have workers available
	time.Sleep(50 * time.Millisecond)
	workerCount := pool.GetWorkerCount()
	assert.Positive(t, workerCount, "Should have at least one worker")
	assert.LessOrEqual(t, workerCount, 5, "Should not exceed max workers")
}
