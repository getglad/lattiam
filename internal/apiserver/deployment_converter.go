package apiserver

import (
	"github.com/lattiam/lattiam/internal/deployment"
	"github.com/lattiam/lattiam/internal/state"
)

// convertToStateDeployment converts an apiserver Deployment to state DeploymentState
func convertToStateDeployment(dep *Deployment) *state.DeploymentState {
	if dep == nil {
		return nil
	}

	// Convert resources
	resources := make([]state.ResourceState, len(dep.Resources))
	for i, res := range dep.Resources {
		resources[i] = state.ResourceState{
			Type:       res.Type,
			Name:       res.Name,
			Properties: res.Properties,
		}
	}

	// Convert results
	results := make([]state.DeploymentResult, len(dep.Results))
	for i, res := range dep.Results {
		var errStr string
		if res.Error != nil {
			errStr = res.Error.Error()
		}
		results[i] = state.DeploymentResult{
			Resource: state.ResourceState{
				Type:       res.Resource.Type,
				Name:       res.Resource.Name,
				Properties: res.Resource.Properties,
			},
			State: res.State,
			Error: errStr,
		}
	}

	// Convert config
	var config map[string]interface{}
	if dep.Config != nil {
		config = map[string]interface{}{
			"aws_profile":   dep.Config.AWSProfile,
			"aws_region":    dep.Config.AWSRegion,
			"state_backend": dep.Config.StateBackend,
		}
	}

	// Include ResourceErrors in metadata if present
	metadata := dep.Metadata
	if metadata == nil {
		metadata = make(map[string]interface{})
	}
	if len(dep.ResourceErrors) > 0 {
		metadata["resource_errors"] = dep.ResourceErrors
	}

	return &state.DeploymentState{
		ID:            dep.ID,
		Name:          dep.Name,
		Status:        string(dep.Status),
		Resources:     resources,
		Results:       results,
		Config:        config,
		CreatedAt:     dep.CreatedAt,
		UpdatedAt:     dep.UpdatedAt,
		Metadata:      metadata,
		TerraformJSON: dep.TerraformJSON,
	}
}

// convertFromStateDeployment converts a state DeploymentState to apiserver Deployment
func convertFromStateDeployment(stateDep *state.DeploymentState) *Deployment {
	if stateDep == nil {
		return nil
	}

	return &Deployment{
		ID:             stateDep.ID,
		Name:           stateDep.Name,
		Status:         DeploymentStatus(stateDep.Status),
		Resources:      convertStateResources(stateDep.Resources),
		Results:        convertStateResults(stateDep.Results),
		Config:         convertStateConfig(stateDep.Config),
		CreatedAt:      stateDep.CreatedAt,
		UpdatedAt:      stateDep.UpdatedAt,
		Metadata:       stateDep.Metadata,
		TerraformJSON:  stateDep.TerraformJSON,
		ResourceErrors: extractResourceErrors(stateDep.Metadata),
	}
}

// convertStateResources converts state resources to deployment resources
func convertStateResources(stateResources []state.ResourceState) []deployment.Resource {
	resources := make([]deployment.Resource, len(stateResources))
	for i, res := range stateResources {
		resources[i] = deployment.Resource{
			Type:       res.Type,
			Name:       res.Name,
			Properties: res.Properties,
		}
	}
	return resources
}

// convertStateResults converts state results to deployment results
func convertStateResults(stateResults []state.DeploymentResult) []deployment.Result {
	results := make([]deployment.Result, len(stateResults))
	for i, res := range stateResults {
		var err error
		if res.Error != "" {
			err = deployment.ErrResourceTypeNotFoundInSchema // Generic error for now
		}
		results[i] = deployment.Result{
			Resource: deployment.Resource{
				Type:       res.Resource.Type,
				Name:       res.Resource.Name,
				Properties: res.Resource.Properties,
			},
			State: res.State,
			Error: err,
		}
	}
	return results
}

// convertStateConfig converts state config to deployment config
func convertStateConfig(stateConfig map[string]interface{}) *DeploymentConfig {
	if stateConfig == nil {
		return nil
	}

	deployConfig := &DeploymentConfig{}
	if profile, ok := stateConfig["aws_profile"].(string); ok {
		deployConfig.AWSProfile = profile
	}
	if region, ok := stateConfig["aws_region"].(string); ok {
		deployConfig.AWSRegion = region
	}
	if backend, ok := stateConfig["state_backend"].(string); ok {
		deployConfig.StateBackend = backend
	}
	return deployConfig
}

// extractResourceErrors extracts resource errors from metadata
func extractResourceErrors(metadata map[string]interface{}) []ResourceError {
	if metadata == nil {
		return nil
	}

	errors, ok := metadata["resource_errors"].([]interface{})
	if !ok {
		return nil
	}

	var resourceErrors []ResourceError
	for _, e := range errors {
		if resourceErr := parseResourceError(e); resourceErr != nil {
			resourceErrors = append(resourceErrors, *resourceErr)
		}
	}
	return resourceErrors
}

// parseResourceError parses a single resource error from interface{}
func parseResourceError(e interface{}) *ResourceError {
	errMap, ok := e.(map[string]interface{})
	if !ok {
		return nil
	}

	resourceErr := &ResourceError{}
	if resource, ok := errMap["resource"].(string); ok {
		resourceErr.Resource = resource
	}
	if resType, ok := errMap["type"].(string); ok {
		resourceErr.Type = resType
	}
	if name, ok := errMap["name"].(string); ok {
		resourceErr.Name = name
	}
	if errStr, ok := errMap["error"].(string); ok {
		resourceErr.Error = errStr
	}
	if phase, ok := errMap["phase"].(string); ok {
		resourceErr.Phase = phase
	}
	return resourceErr
}
