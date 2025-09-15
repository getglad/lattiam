package events

import (
	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/pkg/logging"
)

var logger = logging.NewLogger("tracker-adapter")

// ConnectTrackerToEventBus subscribes a tracker to deployment events
func ConnectTrackerToEventBus(eventBus *EventBus, tracker interfaces.DeploymentTracker) {
	// Subscribe to status change events
	eventBus.Subscribe(EventStatusChanged, func(event DeploymentEvent) {
		if event.Status != nil {
			if err := tracker.SetStatus(event.DeploymentID, *event.Status); err != nil {
				logger.Error("Failed to update deployment %s status: %v", event.DeploymentID, err)
			}
		}
	})

	// Subscribe to result events
	eventBus.Subscribe(EventResultReady, func(event DeploymentEvent) {
		if event.Result != nil {
			if err := tracker.SetResult(event.DeploymentID, event.Result); err != nil {
				logger.Error("Failed to set deployment %s result: %v", event.DeploymentID, err)
			}
		}
	})

	// Subscribe to error events
	eventBus.Subscribe(EventError, func(event DeploymentEvent) {
		logger.Error("Deployment %s error: %v", event.DeploymentID, event.Error)
	})
}

// ConnectDeploymentServiceToEventBus subscribes a deployment service to handle state store cleanup
//
// Event Flow:
// 1. API DELETE /deployments/{id} → DeploymentService.DeleteDeployment()
// 2. DeploymentService queues destruction job if deployment exists
// 3. Executor processes destruction job → destroys resources
// 4. Executor publishes DeploymentStatusDestroyed event
// 5. This handler receives event → calls DeleteDeployment() again
// 6. DeleteDeployment() sees status is already "destroyed" → only cleans up state store
//
// This design ensures state cleanup happens AFTER successful resource destruction.
// The double-call to DeleteDeployment is intentional and safe:
// - First call: Initiates destruction workflow
// - Second call (via event): Performs final cleanup
//
// Safety: DeleteDeployment checks deployment status to prevent duplicate operations
func ConnectDeploymentServiceToEventBus(eventBus *EventBus, deploymentService interfaces.DeploymentService) {
	// Subscribe to status change events for state store cleanup
	eventBus.Subscribe(EventStatusChanged, func(event DeploymentEvent) {
		if event.Status != nil && *event.Status == interfaces.DeploymentStatusDestroyed {
			// When a deployment is marked as destroyed, clean up the state store
			// This is safe to call even if it's already cleaned up
			// DeleteDeployment will check the status and only perform cleanup
			if err := deploymentService.DeleteDeployment(event.DeploymentID); err != nil {
				// Use Warn level as cleanup failure is non-critical
				// The resources are already destroyed at this point
				logger.Warn("Failed to clean up state store for destroyed deployment %s: %v", event.DeploymentID, err)
			} else {
				logger.Info("Successfully cleaned up state store for destroyed deployment %s", event.DeploymentID)
			}
		}
	})
}
