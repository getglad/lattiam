package distributed

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hibiken/asynq"

	"github.com/lattiam/lattiam/internal/interfaces"
)

// DLQManager provides operations for managing archived tasks (Dead Letter Queue)
// In asynq v0.25.1, failed tasks are automatically archived after max retries
type DLQManager struct {
	inspector *asynq.Inspector
	redisOpt  asynq.RedisConnOpt
}

// NewDLQManager creates a new DLQ manager
func NewDLQManager(redisURL string) (*DLQManager, error) {
	redisOpt, err := asynq.ParseRedisURI(redisURL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse redis URL: %w", err)
	}

	inspector := asynq.NewInspector(redisOpt)

	return &DLQManager{
		inspector: inspector,
		redisOpt:  redisOpt,
	}, nil
}

// DeadDeployment represents a deployment that failed all retries
type DeadDeployment struct {
	ID           string                       `json:"id"`
	Deployment   *interfaces.QueuedDeployment `json:"deployment"`
	Error        string                       `json:"error"`
	LastFailedAt time.Time                    `json:"last_failed_at"`
	RetryCount   int                          `json:"retry_count"`
	Queue        string                       `json:"queue"`
}

// ListDeadDeployments returns all archived deployments (equivalent to DLQ)
func (m *DLQManager) ListDeadDeployments(_ context.Context) ([]*DeadDeployment, error) {
	var deadDeployments []*DeadDeployment

	// Check archived tasks in each queue
	queues := []string{"deployments", "critical", "default"}
	for _, queue := range queues {
		archivedTasks, err := m.inspector.ListArchivedTasks(queue)
		if err != nil {
			continue // Queue might not exist yet
		}

		for _, task := range archivedTasks {
			if task.Type == TaskTypeDeployment {
				var deployment interfaces.QueuedDeployment
				if err := json.Unmarshal(task.Payload, &deployment); err != nil {
					continue
				}

				deadDeployments = append(deadDeployments, &DeadDeployment{
					ID:           task.ID,
					Deployment:   &deployment,
					Error:        task.LastErr,
					LastFailedAt: task.LastFailedAt,
					RetryCount:   task.Retried,
					Queue:        queue,
				})
			}
		}
	}

	return deadDeployments, nil
}

// GetDeadDeployment retrieves a specific deployment from the archived tasks
func (m *DLQManager) GetDeadDeployment(_ context.Context, deploymentID string) (*DeadDeployment, error) {
	// In asynq, archived tasks remain in their original queues
	queues := []string{"deployments", "critical", "default"}

	for _, queueName := range queues {
		// Get archived tasks from this queue
		archivedTasks, err := m.inspector.ListArchivedTasks(queueName)
		if err != nil {
			continue
		}

		// Search for the specific deployment
		for _, task := range archivedTasks {
			if task.Type == TaskTypeDeployment && task.ID == deploymentID {
				var deployment interfaces.QueuedDeployment
				if err := json.Unmarshal(task.Payload, &deployment); err != nil {
					return nil, fmt.Errorf("failed to unmarshal deployment: %w", err)
				}

				return &DeadDeployment{
					ID:           task.ID,
					Deployment:   &deployment,
					Error:        task.LastErr,
					LastFailedAt: task.LastFailedAt,
					RetryCount:   task.Retried,
					Queue:        queueName,
				}, nil
			}
		}
	}

	return nil, fmt.Errorf("deployment %s not found in archived tasks", deploymentID)
}

// RequeueDeadDeployment moves a deployment from archived state back to the active queue
func (m *DLQManager) RequeueDeadDeployment(ctx context.Context, deploymentID string) error {
	// Find the deployment in archived tasks
	deadDeployment, err := m.GetDeadDeployment(ctx, deploymentID)
	if err != nil {
		return err
	}

	// Remove from archived tasks
	if err := m.inspector.DeleteTask(deadDeployment.Queue, deploymentID); err != nil {
		return fmt.Errorf("failed to delete from archived tasks: %w", err)
	}

	// Re-enqueue using asynq client
	client := asynq.NewClient(m.redisOpt)
	defer func() { _ = client.Close() }()

	// Create new task with reset retry count
	payload, err := json.Marshal(deadDeployment.Deployment)
	if err != nil {
		return fmt.Errorf("failed to marshal deployment: %w", err)
	}

	task := asynq.NewTask(TaskTypeDeployment, payload,
		asynq.TaskID(deploymentID),
		asynq.Queue("deployments"),
		asynq.MaxRetry(3), // Reset retry count
	)

	_, err = client.EnqueueContext(ctx, task)
	if err != nil {
		return fmt.Errorf("failed to requeue deployment: %w", err)
	}

	return nil
}

// PurgeDeadDeployment permanently removes a deployment from archived tasks
func (m *DLQManager) PurgeDeadDeployment(ctx context.Context, deploymentID string) error {
	deadDeployment, err := m.GetDeadDeployment(ctx, deploymentID)
	if err != nil {
		return err
	}

	if err := m.inspector.DeleteTask(deadDeployment.Queue, deploymentID); err != nil {
		return fmt.Errorf("failed to delete from archived tasks: %w", err)
	}

	return nil
}

// GetDLQStats returns statistics about archived tasks (DLQ)
func (m *DLQManager) GetDLQStats(_ context.Context) (map[string]int, error) {
	stats := make(map[string]int)

	queues := []string{"deployments", "critical", "default"}

	for _, queueName := range queues {
		info, err := m.inspector.GetQueueInfo(queueName)
		if err != nil {
			continue // Queue might not exist yet
		}
		// In asynq, archived tasks are the equivalent of DLQ
		if info.Archived > 0 {
			stats[queueName] = info.Archived
		}
	}

	return stats, nil
}

// Close closes the DLQ manager
func (m *DLQManager) Close() error {
	err := m.inspector.Close()
	if err != nil {
		return fmt.Errorf("failed to close inspector: %w", err)
	}
	return nil
}
