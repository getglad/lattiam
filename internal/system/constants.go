package system

import "time"

// DefaultWorkerPoolConfig contains default values for worker pool configuration
var DefaultWorkerPoolConfig = struct {
	MinWorkers      int
	MaxWorkers      int
	IdleTimeout     time.Duration
	QueueSize       int
	ShutdownTimeout time.Duration
}{
	MinWorkers:      2,
	MaxWorkers:      10,
	IdleTimeout:     30 * time.Second,
	QueueSize:       100,
	ShutdownTimeout: 30 * time.Second,
}

// DefaultQueueConfig contains default values for queue configuration
var DefaultQueueConfig = struct {
	Capacity         int
	DefaultStoreType string
}{
	Capacity:         1000,
	DefaultStoreType: "memory",
}

// DefaultSystemConfig contains default values for system configuration
var DefaultSystemConfig = struct {
	HealthCheckInterval  time.Duration
	MetricsRetentionTime time.Duration
	ShutdownTimeout      time.Duration
}{
	HealthCheckInterval:  30 * time.Second,
	MetricsRetentionTime: 24 * time.Hour,
	ShutdownTimeout:      30 * time.Second,
}

// DefaultProviderConfig contains default values for provider configuration
var DefaultProviderConfig = struct {
	ProviderDir string
}{
	ProviderDir: "./providers",
}
