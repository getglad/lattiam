package interfaces

import (
	"time"
)

// Pool defines the interface for managing background workers
type Pool interface {
	// Lifecycle
	Start() error
	Stop() error
	IsRunning() bool

	// Worker management
	AddWorker() error
	RemoveWorker() error
	SetWorkerCount(count int) error

	// Status and health
	GetActiveWorkers() int
	GetIdleWorkers() int
	GetBusyWorkers() int
	GetQueueDepth() int
	IsHealthy() bool

	// Metrics and monitoring
	GetMetrics() PoolMetrics
	GetWorkerStatistics() []WorkerStatistics

	// Configuration
	SetConfiguration(config PoolConfiguration) error
	GetConfiguration() PoolConfiguration
}

// PoolMetrics provides metrics about the worker pool
type PoolMetrics struct {
	TotalJobs          int64
	CompletedJobs      int64
	FailedJobs         int64
	AverageJobDuration time.Duration
	WorkerUtilization  float64
	QueueWaitTime      time.Duration
}

// WorkerStatistics provides statistics about an individual worker
type WorkerStatistics struct {
	WorkerID      string
	Status        WorkerStatus
	CurrentJob    *QueuedDeployment
	JobsCompleted int64
	JobsFailed    int64
	StartTime     time.Time
	LastActivity  time.Time
}

// WorkerStatus represents the status of a worker
type WorkerStatus string

// WorkerStatus constants represent the various states of a worker
const (
	WorkerStatusIdle    WorkerStatus = "idle"
	WorkerStatusBusy    WorkerStatus = "busy"
	WorkerStatusStopped WorkerStatus = "stopped"
	WorkerStatusError   WorkerStatus = "error"
)

// PoolConfiguration provides configuration for the worker pool
type PoolConfiguration struct {
	MinWorkers      int
	MaxWorkers      int
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration
	QueueSize       int
}

// PoolCall represents a call to the Pool for tracking in mocks
type PoolCall struct {
	Method    string
	WorkerID  string
	Timestamp time.Time
	Error     error
}
