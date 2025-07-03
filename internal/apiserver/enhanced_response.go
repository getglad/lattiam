package apiserver

import "github.com/lattiam/lattiam/internal/deployment"

// EnhancedDeploymentResponse provides detailed deployment information
type EnhancedDeploymentResponse struct {
	ID             string                 `json:"id"`
	Name           string                 `json:"name"`
	Status         string                 `json:"status"`
	ResourceErrors []ResourceError        `json:"resource_errors,omitempty"`
	Resources      []ResourceStatus       `json:"resources,omitempty"`
	Summary        *DeploymentSummary     `json:"summary,omitempty"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
	CreatedAt      string                 `json:"created_at"`
	UpdatedAt      string                 `json:"updated_at"`
}

// ResourceStatus shows the status of each resource
type ResourceStatus struct {
	Type   string                 `json:"type"`
	Name   string                 `json:"name"`
	Status string                 `json:"status"`
	Error  string                 `json:"error,omitempty"`
	State  map[string]interface{} `json:"state,omitempty"`
}

// DeploymentSummary provides aggregate information
type DeploymentSummary struct {
	TotalResources      int `json:"total_resources"`
	SuccessfulResources int `json:"successful_resources"`
	FailedResources     int `json:"failed_resources"`
	PendingResources    int `json:"pending_resources"`
}

// BuildEnhancedResponse creates a detailed response from deployment
func BuildEnhancedResponse(dep *Deployment) EnhancedDeploymentResponse {
	resp := EnhancedDeploymentResponse{
		ID:             dep.ID,
		Name:           dep.Name,
		Status:         string(dep.Status),
		ResourceErrors: dep.ResourceErrors,
		Metadata:       dep.Metadata,
		CreatedAt:      dep.CreatedAt.Format("2006-01-02T15:04:05Z"),
		UpdatedAt:      dep.UpdatedAt.Format("2006-01-02T15:04:05Z"),
	}

	// Add resource details if available
	if len(dep.Resources) > 0 {
		resp.Resources, resp.Summary = buildResourceDetails(dep.Resources, dep.Results)
	}

	return resp
}

// buildResourceDetails creates resource status details and summary
func buildResourceDetails(
	resources []deployment.Resource, results []deployment.Result,
) ([]ResourceStatus, *DeploymentSummary) {
	resourceStatuses := make([]ResourceStatus, len(resources))
	summary := &DeploymentSummary{
		TotalResources: len(resources),
	}

	// Create a map to match results by resource type and name
	resultMap := make(map[string]*deployment.Result)
	for i := range results {
		result := &results[i]
		key := result.Resource.Type + "/" + result.Resource.Name
		resultMap[key] = result
	}

	for i, resource := range resources {
		status := "pending"
		var resourceError string
		var resourceState map[string]interface{}

		// Find matching result by resource type and name
		key := resource.Type + "/" + resource.Name
		if result, found := resultMap[key]; found {
			if result.Error != nil {
				status = "failed"
				resourceError = result.Error.Error()
				summary.FailedResources++
			} else if result.State != nil {
				status = "completed"
				resourceState = result.State
				summary.SuccessfulResources++
			}
		} else {
			summary.PendingResources++
		}

		resourceStatuses[i] = ResourceStatus{
			Type:   resource.Type,
			Name:   resource.Name,
			Status: status,
			Error:  resourceError,
			State:  resourceState,
		}
	}

	return resourceStatuses, summary
}
