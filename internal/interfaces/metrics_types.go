// Package interfaces defines metrics and health status types for monitoring
package interfaces

import "time"

// SystemMetrics provides metrics about the overall system
type SystemMetrics struct {
	DeploymentsProcessed  int64
	DeploymentsSucceeded  int64
	DeploymentsFailed     int64
	AverageDeploymentTime time.Duration
	CurrentQueueDepth     int
	ActiveWorkers         int
	SystemUptime          time.Duration
}

// HealthStatus represents the overall health status
type HealthStatus string

const (
	// HealthStatusHealthy indicates system is operating normally
	HealthStatusHealthy HealthStatus = "healthy"
	// HealthStatusDegraded indicates system has issues but is functional
	HealthStatusDegraded HealthStatus = "degraded"
	// HealthStatusUnhealthy indicates system is not functioning properly
	HealthStatusUnhealthy HealthStatus = "unhealthy"
)
