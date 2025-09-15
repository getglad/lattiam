package interfaces

// DeploymentService defines the business logic operations for deployments
type DeploymentService interface {
	// ListDeployments returns deployments matching the filter criteria
	ListDeployments(filter DeploymentFilter) ([]*QueuedDeployment, error)

	// CreateDeployment creates a new deployment from the request
	CreateDeployment(request *DeploymentRequest) (*QueuedDeployment, error)

	// GetDeploymentByID retrieves a deployment by its ID
	GetDeploymentByID(deploymentID string) (*QueuedDeployment, error)

	// CancelDeployment cancels an in-progress deployment
	CancelDeployment(deploymentID string) error

	// DeleteDeployment deletes a deployment, potentially destroying its resources
	DeleteDeployment(deploymentID string) error

	// UpdateDeployment updates an existing deployment with new configuration
	UpdateDeployment(deploymentID string, request *UpdateRequest) (*QueuedUpdate, error)

	// GetDeploymentStatus returns the current status of a deployment
	GetDeploymentStatus(deploymentID string) (*DeploymentStatus, error)

	// GetDeploymentState returns the detailed state of a deployment
	GetDeploymentState(deploymentID string) (map[string]interface{}, error)

	// GenerateUpdatePlan creates a plan for updating a deployment
	GenerateUpdatePlan(deploymentID string, newTerraformJSON map[string]interface{}) (*UpdatePlan, error)

	// DeploymentNeedsDestruction checks if a deployment has resources that need to be destroyed
	DeploymentNeedsDestruction(deploymentID string) (bool, error)

	// GetQueueMetrics returns current queue metrics
	GetQueueMetrics() QueueMetrics
}
