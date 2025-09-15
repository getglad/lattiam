package distributed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"

	"github.com/lattiam/lattiam/internal/interfaces"
)

// Tracker implements interfaces.DeploymentTracker using Redis
type Tracker struct {
	redis redis.UniversalClient
}

// NewTracker creates a new distributed deployment tracker
func NewTracker(redisOpt asynq.RedisConnOpt) (*Tracker, error) {
	// Create Redis client for custom operations
	var redisClient redis.UniversalClient
	switch opt := redisOpt.(type) {
	case *asynq.RedisClientOpt:
		redisClient = redis.NewClient(&redis.Options{
			Addr:     opt.Addr,
			Username: opt.Username,
			Password: opt.Password,
			DB:       opt.DB,
		})
	case *asynq.RedisClusterClientOpt:
		redisClient = redis.NewClusterClient(&redis.ClusterOptions{
			Addrs:    opt.Addrs,
			Username: opt.Username,
			Password: opt.Password,
		})
	default:
		return nil, fmt.Errorf("unsupported redis connection type")
	}

	return &Tracker{
		redis: redisClient,
	}, nil
}

// Register adds a new deployment to the tracker
func (t *Tracker) Register(deployment *interfaces.QueuedDeployment) error {
	if deployment == nil {
		return fmt.Errorf("deployment is nil")
	}
	if deployment.ID == "" {
		return fmt.Errorf("deployment ID is empty")
	}

	// Serialize deployment
	data, err := json.Marshal(deployment)
	if err != nil {
		return fmt.Errorf("failed to marshal deployment: %w", err)
	}

	// Store in Redis with expiration (7 days)
	key := deploymentKey(deployment.ID)
	ctx := context.Background()
	err = t.redis.Set(ctx, key, data, 7*24*time.Hour).Err()
	if err != nil {
		return fmt.Errorf("failed to store deployment: %w", err)
	}

	// Also store status separately for quick access
	statusKey := statusKey(deployment.ID)
	err = t.redis.Set(ctx, statusKey, string(deployment.Status), 7*24*time.Hour).Err()
	if err != nil {
		return fmt.Errorf("failed to store status: %w", err)
	}

	return nil
}

// GetByID returns a deployment by its ID
func (t *Tracker) GetByID(deploymentID string) (*interfaces.QueuedDeployment, error) {
	if deploymentID == "" {
		return nil, fmt.Errorf("deployment ID is empty")
	}

	ctx := context.Background()
	key := deploymentKey(deploymentID)

	data, err := t.redis.Get(ctx, key).Result()
	if errors.Is(err, redis.Nil) {
		return nil, fmt.Errorf("deployment %s not found", deploymentID)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get deployment: %w", err)
	}

	var deployment interfaces.QueuedDeployment
	if err := json.Unmarshal([]byte(data), &deployment); err != nil {
		return nil, fmt.Errorf("failed to unmarshal deployment: %w", err)
	}

	return &deployment, nil
}

// GetStatus returns the status of a deployment
func (t *Tracker) GetStatus(deploymentID string) (*interfaces.DeploymentStatus, error) {
	if deploymentID == "" {
		return nil, fmt.Errorf("deployment ID is empty")
	}

	ctx := context.Background()

	// Try to get from the status key first (faster)
	statusKey := statusKey(deploymentID)
	statusStr, err := t.redis.Get(ctx, statusKey).Result()
	if err == nil {
		status := interfaces.DeploymentStatus(statusStr)
		return &status, nil
	}

	// If status key not found, check if deployment exists at all
	key := deploymentKey(deploymentID)
	data, err := t.redis.Get(ctx, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, fmt.Errorf("deployment %s not found", deploymentID)
		}
		return nil, fmt.Errorf("failed to get deployment: %w", err)
	}

	// Unmarshal deployment to get status
	var deployment interfaces.QueuedDeployment
	if err := json.Unmarshal([]byte(data), &deployment); err != nil {
		return nil, fmt.Errorf("failed to unmarshal deployment: %w", err)
	}

	status := deployment.Status
	return &status, nil
}

// SetStatus updates the status of a deployment
func (t *Tracker) SetStatus(deploymentID string, status interfaces.DeploymentStatus) error {
	if deploymentID == "" {
		return fmt.Errorf("deployment ID is empty")
	}

	ctx := context.Background()

	// Check if deployment exists first
	key := deploymentKey(deploymentID)
	data, err := t.redis.Get(ctx, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return fmt.Errorf("deployment %s not found", deploymentID)
		}
		return fmt.Errorf("failed to get deployment: %w", err)
	}

	// Update status in Redis
	statusKey := statusKey(deploymentID)
	err = t.redis.Set(ctx, statusKey, string(status), 7*24*time.Hour).Err()
	if err != nil {
		return fmt.Errorf("failed to update status: %w", err)
	}

	// Also update the deployment record
	var deployment interfaces.QueuedDeployment
	if err := json.Unmarshal([]byte(data), &deployment); err == nil {
		deployment.Status = status

		// Update timestamps
		now := time.Now()
		switch status {
		case interfaces.DeploymentStatusQueued:
			// No timestamp update needed for queued
		case interfaces.DeploymentStatusProcessing:
			deployment.StartedAt = &now
		case interfaces.DeploymentStatusCompleted, interfaces.DeploymentStatusFailed:
			deployment.CompletedAt = &now
		case interfaces.DeploymentStatusCanceled:
			deployment.CompletedAt = &now
		case interfaces.DeploymentStatusCanceling:
			// No timestamp update - still processing
		case interfaces.DeploymentStatusDestroying:
			// No timestamp update - still processing destruction
		case interfaces.DeploymentStatusDestroyed:
			deployment.CompletedAt = &now
		case interfaces.DeploymentStatusDeleted:
			deployment.CompletedAt = &now
		default:
			// Other statuses don't need timestamp updates
		}

		// Save updated deployment
		if updatedData, err := json.Marshal(deployment); err == nil {
			_ = t.redis.Set(ctx, key, updatedData, 7*24*time.Hour).Err()
		}
	}

	return nil
}

// GetResult returns the result of a deployment
func (t *Tracker) GetResult(deploymentID string) (*interfaces.DeploymentResult, error) {
	if deploymentID == "" {
		return nil, fmt.Errorf("deployment ID is empty")
	}

	// Get result from Redis
	key := resultKey(deploymentID)
	ctx := context.Background()
	data, err := t.redis.Get(ctx, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, nil // No result yet
		}
		return nil, fmt.Errorf("failed to get result: %w", err)
	}

	var result interfaces.DeploymentResult
	if err := json.Unmarshal([]byte(data), &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal result: %w", err)
	}

	return &result, nil
}

// List returns deployments matching the filter
func (t *Tracker) List(filter interfaces.DeploymentFilter) ([]*interfaces.QueuedDeployment, error) {
	ctx := context.Background()

	// Use SCAN instead of KEYS for better performance
	pattern := "deployment:*:data"
	var results []*interfaces.QueuedDeployment

	// SCAN returns results in batches
	var cursor uint64
	for {
		var keys []string
		var err error
		keys, cursor, err = t.redis.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return nil, fmt.Errorf("failed to scan deployments: %w", err)
		}

		// Process this batch of keys
		for _, key := range keys {
			data, err := t.redis.Get(ctx, key).Result()
			if err != nil {
				continue // Skip if can't read
			}

			var deployment interfaces.QueuedDeployment
			if err := json.Unmarshal([]byte(data), &deployment); err != nil {
				continue // Skip if can't unmarshal
			}

			if matchesFilter(&deployment, filter) {
				results = append(results, &deployment)
			}
		}

		// If cursor is 0, we've scanned all keys
		if cursor == 0 {
			break
		}
	}

	return results, nil
}

// Remove deletes a deployment from the tracker
func (t *Tracker) Remove(deploymentID string) error {
	if deploymentID == "" {
		return fmt.Errorf("deployment ID is empty")
	}

	ctx := context.Background()

	// Remove all keys related to this deployment
	keys := []string{
		deploymentKey(deploymentID),
		statusKey(deploymentID),
		resultKey(deploymentID),
	}

	for _, key := range keys {
		_ = t.redis.Del(ctx, key).Err()
	}

	return nil
}

// SetResult stores the result of a deployment
func (t *Tracker) SetResult(deploymentID string, result *interfaces.DeploymentResult) error {
	if deploymentID == "" {
		return fmt.Errorf("deployment ID is empty")
	}
	if result == nil {
		return fmt.Errorf("result is nil")
	}

	// Serialize result
	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("failed to marshal result: %w", err)
	}

	// Store in Redis
	key := resultKey(deploymentID)
	ctx := context.Background()
	err = t.redis.Set(ctx, key, data, 7*24*time.Hour).Err()
	if err != nil {
		return fmt.Errorf("failed to store result: %w", err)
	}

	return nil
}

// Close closes the tracker connections
func (t *Tracker) Close() error {
	err := t.redis.Close()
	if err != nil {
		return fmt.Errorf("failed to close redis client: %w", err)
	}
	return nil
}

// Helper functions

func deploymentKey(deploymentID string) string {
	return fmt.Sprintf("deployment:%s:data", deploymentID)
}

func statusKey(deploymentID string) string {
	return fmt.Sprintf("deployment:%s:status", deploymentID)
}

func resultKey(deploymentID string) string {
	return fmt.Sprintf("deployment:%s:result", deploymentID)
}

func matchesFilter(deployment *interfaces.QueuedDeployment, filter interfaces.DeploymentFilter) bool {
	// Check status filter
	if len(filter.Status) > 0 {
		statusMatches := false
		for _, status := range filter.Status {
			if deployment.Status == status {
				statusMatches = true
				break
			}
		}
		if !statusMatches {
			return false
		}
	}

	// Check created after
	if !filter.CreatedAfter.IsZero() && deployment.CreatedAt.Before(filter.CreatedAfter) {
		return false
	}

	// Check created before
	if !filter.CreatedBefore.IsZero() && deployment.CreatedAt.After(filter.CreatedBefore) {
		return false
	}

	return true
}

// Load is a no-op for distributed tracker since Redis handles persistence automatically
func (t *Tracker) Load(_ interfaces.StateStore) error {
	// Redis-backed tracker doesn't need to load from StateStore since
	// all data is already persisted in Redis. The Load method is for
	// embedded trackers that use in-memory storage.
	return nil
}
