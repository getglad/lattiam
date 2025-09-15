package deployment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lattiam/lattiam/internal/apiserver/types"
	"github.com/lattiam/lattiam/internal/dependency"
	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/internal/logging"
	"github.com/lattiam/lattiam/internal/utils"
)

// Service implements the DeploymentService interface using the new clean interfaces
type Service struct {
	queue              interfaces.DeploymentQueue
	tracker            interfaces.DeploymentTracker
	stateStore         interfaces.StateStore
	providerManager    interfaces.ProviderLifecycleManager
	interpolator       interfaces.InterpolationResolver
	dependencyResolver interfaces.DependencyResolver
	txCoordinator      *TransactionCoordinator
	converter          *types.RequestConverter
	logger             *logging.Logger
}

// ServiceConfig holds all dependencies needed by the deployment service
type ServiceConfig struct {
	Queue              interfaces.DeploymentQueue
	Tracker            interfaces.DeploymentTracker
	StateStore         interfaces.StateStore
	DependencyResolver interfaces.DependencyResolver
	ProviderManager    interfaces.ProviderLifecycleManager
	Interpolator       interfaces.InterpolationResolver
}

// NewServiceWithConfig creates a new deployment service with full configuration
func NewServiceWithConfig(cfg ServiceConfig) (interfaces.DeploymentService, error) {
	if cfg.Queue == nil {
		return nil, errors.New("deployment queue is required")
	}
	if cfg.Tracker == nil {
		return nil, errors.New("deployment tracker is required")
	}
	// StateStore, ProviderManager, Interpolator, and DependencyResolver are optional for backward compatibility
	// but required for update functionality
	return &Service{
		queue:              cfg.Queue,
		tracker:            cfg.Tracker,
		stateStore:         cfg.StateStore,
		providerManager:    cfg.ProviderManager,
		interpolator:       cfg.Interpolator,
		dependencyResolver: cfg.DependencyResolver,
		txCoordinator:      NewTransactionCoordinator(),
		converter:          types.NewRequestConverterWithDefaults(),
		logger:             logging.NewLogger("deployment-service"),
	}, nil
}

// ListDeployments returns deployments matching the filter criteria
func (s *Service) ListDeployments(filter interfaces.DeploymentFilter) ([]*interfaces.QueuedDeployment, error) {
	deployments, err := s.tracker.List(filter)
	if err != nil {
		return nil, fmt.Errorf("failed to list deployments: %w", err)
	}
	return deployments, nil
}

// CreateDeployment creates a new deployment from the request
func (s *Service) CreateDeployment(request *interfaces.DeploymentRequest) (*interfaces.QueuedDeployment, error) { //nolint:gocyclo // Complex deployment creation logic
	if request == nil {
		return nil, errors.New("deployment request is required")
	}

	// Create a new queued deployment
	deployment := &interfaces.QueuedDeployment{
		ID:        generateDeploymentID(),
		Request:   request,
		Status:    interfaces.DeploymentStatusQueued,
		CreatedAt: time.Now(),
	}

	// Extract request ID from metadata if present
	if request.Metadata != nil {
		if requestID, ok := request.Metadata[interfaces.MetadataKeyRequestID].(string); ok && requestID != "" {
			deployment.RequestID = requestID
		}
	}

	// Use transaction coordinator for atomic operations
	err := CreateDeploymentTransaction(s.txCoordinator, s.tracker, s.queue, deployment)
	if err != nil {
		return nil, fmt.Errorf("failed to create deployment: %w", err)
	}

	return deployment, nil
}

// GetDeploymentByID retrieves a deployment by its ID
func (s *Service) GetDeploymentByID(deploymentID string) (*interfaces.QueuedDeployment, error) {
	if deploymentID == "" {
		return nil, errors.New("deployment ID is required")
	}

	// Get from tracker using efficient GetByID method
	deployment, err := s.tracker.GetByID(deploymentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get deployment: %w", err)
	}
	return deployment, nil
}

// GetDeploymentStatus returns the current status of a deployment
func (s *Service) GetDeploymentStatus(deploymentID string) (*interfaces.DeploymentStatus, error) {
	if deploymentID == "" {
		return nil, errors.New("deployment ID is required")
	}

	status, err := s.tracker.GetStatus(deploymentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get deployment status: %w", err)
	}
	return status, nil
}

// GetDeploymentState returns the detailed state of a deployment
func (s *Service) GetDeploymentState(deploymentID string) (map[string]interface{}, error) {
	if deploymentID == "" {
		return nil, errors.New("deployment ID is required")
	}

	// Get result from tracker
	result, err := s.tracker.GetResult(deploymentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get deployment result: %w", err)
	}

	if result == nil {
		return nil, nil
	}

	// Return resources as state
	return result.Resources, nil
}

// CancelDeployment cancels an in-progress deployment
func (s *Service) CancelDeployment(deploymentID string) error {
	if deploymentID == "" {
		return errors.New("deployment ID is required")
	}

	ctx := context.Background()
	if err := s.queue.Cancel(ctx, deploymentID); err != nil {
		return fmt.Errorf("failed to cancel deployment: %w", err)
	}
	return nil
}

// DeleteDeployment deletes a deployment, potentially destroying its resources
//
//nolint:gocognit,funlen,gocyclo // Complex deletion logic with multiple resource types
func (s *Service) DeleteDeployment(deploymentID string) error {
	if deploymentID == "" {
		return errors.New("deployment ID is required")
	}

	// Get current status
	status, err := s.GetDeploymentStatus(deploymentID)
	if err != nil {
		return err
	}

	switch *status {
	case interfaces.DeploymentStatusCompleted, interfaces.DeploymentStatusFailed:
		// Check if needs destruction
		needsDestruction, err := s.DeploymentNeedsDestruction(deploymentID)
		if err != nil {
			return fmt.Errorf("failed to check destruction needs: %w", err)
		}

		if needsDestruction {
			// Queue destruction job
			return s.queueDestructionJob(deploymentID)
		}

		// No destruction needed - clean up state store and tracker
		if s.stateStore != nil {
			if err := s.stateStore.DeleteDeployment(context.Background(), deploymentID); err != nil {
				return fmt.Errorf("failed to clean up state store: %w", err)
			}
		}
		if err := s.tracker.Remove(deploymentID); err != nil {
			return fmt.Errorf("failed to remove deployment: %w", err)
		}
		return nil

	case interfaces.DeploymentStatusProcessing:
		// Mark as canceling first
		if err := s.tracker.SetStatus(deploymentID, interfaces.DeploymentStatusCanceling); err != nil {
			return fmt.Errorf("failed to set deployment status: %w", err)
		}
		return nil

	case interfaces.DeploymentStatusQueued:
		// Cancel and remove
		if err := s.CancelDeployment(deploymentID); err != nil {
			return err
		}
		// Clean up state store and tracker
		if s.stateStore != nil {
			if err := s.stateStore.DeleteDeployment(context.Background(), deploymentID); err != nil {
				s.logger.Warnf("Failed to clean up state store for deployment %s: %v", deploymentID, err)
			}
		}
		if err := s.tracker.Remove(deploymentID); err != nil {
			return fmt.Errorf("failed to remove deployment: %w", err)
		}
		return nil

	case interfaces.DeploymentStatusCanceled:
		// Already canceled, just check if needs destruction
		needsDestruction, err := s.DeploymentNeedsDestruction(deploymentID)
		if err != nil {
			return fmt.Errorf("failed to check destruction needs: %w", err)
		}
		if needsDestruction {
			return s.queueDestructionJob(deploymentID)
		}
		// No destruction needed - clean up state store and tracker
		if s.stateStore != nil {
			if err := s.stateStore.DeleteDeployment(context.Background(), deploymentID); err != nil {
				return fmt.Errorf("failed to clean up state store: %w", err)
			}
		}
		if err := s.tracker.Remove(deploymentID); err != nil {
			return fmt.Errorf("failed to remove deployment: %w", err)
		}
		return nil

	case interfaces.DeploymentStatusCanceling:
		// Already canceling, can't delete until canceled
		return fmt.Errorf("cannot delete deployment while cancellation is in progress")

	case interfaces.DeploymentStatusDestroying:
		// Already destroying, can't delete until destroyed
		return fmt.Errorf("cannot delete deployment while destruction is in progress")

	case interfaces.DeploymentStatusDestroyed, interfaces.DeploymentStatusDeleted:
		// Already destroyed/deleted - clean up state store and tracker
		if s.stateStore != nil {
			if err := s.stateStore.DeleteDeployment(context.Background(), deploymentID); err != nil {
				s.logger.Warnf("Failed to clean up state store for deployment %s: %v", deploymentID, err)
			}
		}
		if err := s.tracker.Remove(deploymentID); err != nil {
			s.logger.Warnf("Failed to remove deployment %s from tracker: %v", deploymentID, err)
		}
		return nil

	default:
		return fmt.Errorf("cannot delete deployment in status: %s", *status)
	}
}

// UpdateDeployment updates an existing deployment with new configuration
func (s *Service) UpdateDeployment(deploymentID string, request *interfaces.UpdateRequest) (*interfaces.QueuedUpdate, error) {
	// Validate inputs
	if err := s.validateUpdateInputs(deploymentID, request); err != nil {
		return nil, err
	}

	// Check if deployment can be updated
	if err := s.checkDeploymentUpdateEligibility(deploymentID); err != nil {
		return nil, err
	}

	// Generate update plan
	plan, err := s.GenerateUpdatePlan(deploymentID, request.NewTerraformJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to generate update plan: %w", err)
	}

	// Create and queue the update
	return s.createAndQueueUpdate(deploymentID, request, plan)
}

// validateUpdateInputs validates the inputs for an update request
func (s *Service) validateUpdateInputs(deploymentID string, request *interfaces.UpdateRequest) error {
	if deploymentID == "" {
		return errors.New("deployment ID is required")
	}
	if request == nil {
		return errors.New("update request is required")
	}
	return nil
}

// checkDeploymentUpdateEligibility checks if a deployment can be updated
func (s *Service) checkDeploymentUpdateEligibility(deploymentID string) error {
	status, err := s.GetDeploymentStatus(deploymentID)
	if err != nil {
		return err
	}
	return s.validateDeploymentForUpdate(*status)
}

// createAndQueueUpdate creates an update deployment and queues it
func (s *Service) createAndQueueUpdate(deploymentID string, request *interfaces.UpdateRequest, plan *interfaces.UpdatePlan) (*interfaces.QueuedUpdate, error) {
	// Generate update ID
	updateID := generateUpdateID()

	// Build deployment request
	deploymentRequest, err := s.buildUpdateDeploymentRequest(deploymentID, updateID, request, plan)
	if err != nil {
		return nil, err
	}

	// Create queued deployment
	queuedDeployment := s.buildQueuedUpdateDeployment(deploymentID, updateID, deploymentRequest)

	// Queue the update atomically
	if err := CreateDeploymentTransaction(s.txCoordinator, s.tracker, s.queue, queuedDeployment); err != nil {
		return nil, fmt.Errorf("failed to create update deployment: %w", err)
	}

	// Return queued update
	return &interfaces.QueuedUpdate{
		ID:           updateID,
		DeploymentID: deploymentID,
		Request:      request,
		Status:       interfaces.UpdateStatusPending,
		CreatedAt:    time.Now(),
		Plan:         plan,
	}, nil
}

// buildUpdateDeploymentRequest builds a deployment request for an update
func (s *Service) buildUpdateDeploymentRequest(deploymentID, updateID string, request *interfaces.UpdateRequest, plan *interfaces.UpdatePlan) (*interfaces.DeploymentRequest, error) {
	// Convert terraform JSON to deployment request
	convertedRequest, err := s.convertTerraformToRequest(request.NewTerraformJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to convert terraform JSON: %w", err)
	}

	// Build deployment request with update metadata
	return &interfaces.DeploymentRequest{
		Resources:   convertedRequest.Resources,
		DataSources: convertedRequest.DataSources,
		Options:     convertedRequest.Options,
		Metadata: map[string]interface{}{
			interfaces.MetadataKeyOperation:     interfaces.OperationUpdate,
			interfaces.MetadataKeyDeploymentID:  deploymentID,
			interfaces.MetadataKeyUpdateID:      updateID,
			interfaces.MetadataKeyUpdatePlan:    plan,
			interfaces.MetadataKeyTerraformJSON: request.NewTerraformJSON,
		},
	}, nil
}

// buildQueuedUpdateDeployment builds a queued deployment for an update
func (s *Service) buildQueuedUpdateDeployment(deploymentID, updateID string, request *interfaces.DeploymentRequest) *interfaces.QueuedDeployment {
	return &interfaces.QueuedDeployment{
		ID:        fmt.Sprintf("update-%s-%s", deploymentID, updateID),
		Request:   request,
		Status:    interfaces.DeploymentStatusQueued,
		CreatedAt: time.Now(),
	}
}

// GenerateUpdatePlan creates a plan for updating a deployment
func (s *Service) GenerateUpdatePlan(deploymentID string, newTerraformJSON map[string]interface{}) (*interfaces.UpdatePlan, error) {
	if deploymentID == "" {
		return nil, errors.New("deployment ID is required")
	}
	if newTerraformJSON == nil {
		return nil, errors.New("new terraform JSON is required")
	}

	// Check if we have required components for planning
	if s.stateStore == nil || s.providerManager == nil {
		return nil, errors.New("update planning requires state store and provider manager")
	}

	ctx := context.Background()

	// Extract current state
	currentState, err := s.extractCurrentState(deploymentID)
	if err != nil {
		return nil, fmt.Errorf("failed to extract current state: %w", err)
	}

	// Extract provider configurations and versions
	providerConfigs := s.extractProviderConfigs(newTerraformJSON)
	providerVersions := s.extractProviderVersions(newTerraformJSON, currentState)

	// Get deployed resources for interpolation
	deployedResources, err := s.getDeployedResourcesForInterpolation(deploymentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get deployed resources: %w", err)
	}

	// Parse configurations
	newResources, currentResources, err := s.parseConfigurations(newTerraformJSON, currentState)
	if err != nil {
		return nil, fmt.Errorf("failed to parse configurations: %w", err)
	}

	// Generate changes for all resources
	changes, err := s.generateResourceChanges(ctx, deploymentID, newResources, currentResources,
		providerConfigs, providerVersions, deployedResources)
	if err != nil {
		return nil, fmt.Errorf("failed to generate resource changes: %w", err)
	}

	// Build update plan
	return s.buildUpdatePlan(deploymentID, changes, providerConfigs, providerVersions), nil
}

// extractCurrentState retrieves the current terraform configuration from deployment metadata
func (s *Service) extractCurrentState(deploymentID string) (map[string]interface{}, error) {
	deployment, err := s.GetDeploymentByID(deploymentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get deployment: %w", err)
	}

	var currentState map[string]interface{}
	if deployment.Request != nil && deployment.Request.Metadata != nil {
		if terraformJSON, ok := deployment.Request.Metadata[interfaces.MetadataKeyTerraformJSON].(map[string]interface{}); ok {
			currentState = map[string]interface{}{
				"terraform_json": terraformJSON,
			}
		}
	}

	return currentState, nil
}

// extractProviderConfigs extracts provider configurations from terraform JSON
func (s *Service) extractProviderConfigs(terraformJSON map[string]interface{}) map[string]map[string]interface{} {
	providerConfigs := make(map[string]map[string]interface{})
	if providers, ok := terraformJSON["provider"].(map[string]interface{}); ok {
		for providerName, configRaw := range providers {
			if providerConfig, ok := configRaw.(map[string]interface{}); ok {
				providerConfigs[providerName] = providerConfig
			}
		}
	}
	return providerConfigs
}

// extractProviderVersions extracts provider versions from both new and current terraform JSON
func (s *Service) extractProviderVersions(newTerraformJSON, currentState map[string]interface{}) map[string]string {
	providerVersions := utils.ExtractProviderVersions(newTerraformJSON)

	// Also check current terraform JSON for any missing versions
	if currentState != nil && currentState["terraform_json"] != nil {
		if currentJSON, ok := currentState["terraform_json"].(map[string]interface{}); ok {
			currentVersions := utils.ExtractProviderVersions(currentJSON)
			// Fill in any missing versions from current config
			for provider, version := range currentVersions {
				if _, exists := providerVersions[provider]; !exists {
					providerVersions[provider] = version
				}
			}
		}
	}

	return providerVersions
}

// getDeployedResourcesForInterpolation retrieves and formats deployed resources for interpolation
func (s *Service) getDeployedResourcesForInterpolation(deploymentID string) (map[string]map[string]interface{}, error) {
	// Load Terraform state to get resource information
	ctx := context.Background()
	stateData, err := s.stateStore.LoadTerraformState(ctx, deploymentID)
	if err != nil {
		return nil, fmt.Errorf("failed to load terraform state: %w", err)
	}

	if stateData == nil {
		return make(map[string]map[string]interface{}), nil
	}

	// Parse Terraform state
	var tfState interfaces.TerraformState
	if err := json.Unmarshal(stateData, &tfState); err != nil {
		return nil, fmt.Errorf("failed to parse terraform state: %w", err)
	}

	// Convert Terraform resources to the expected format
	allStates := make(map[string]map[string]interface{})
	for _, resource := range tfState.Resources {
		for _, instance := range resource.Instances {
			resourceKey := fmt.Sprintf("%s.%s", resource.Type, resource.Name)
			allStates[resourceKey] = instance.Attributes
		}
	}

	// Convert state format for interpolation
	deployedResources := make(map[string]map[string]interface{})
	for stateKey, state := range allStates {
		var resourceType, resourceName string

		// Handle multiple key formats
		if strings.HasPrefix(stateKey, deploymentID+"_") {
			// Remove deployment ID prefix
			keyWithoutDeployment := strings.TrimPrefix(stateKey, deploymentID+"_")
			// Parse using ParseResourceKey which handles slash notation
			resourceType, resourceName = interfaces.ParseResourceKey(interfaces.ResourceKey(keyWithoutDeployment))
		} else {
			// Parse key directly
			resourceType, resourceName = interfaces.ParseResourceKey(interfaces.ResourceKey(stateKey))
		}

		if resourceType != "" && resourceName != "" {
			if _, ok := deployedResources[resourceType]; !ok {
				deployedResources[resourceType] = make(map[string]interface{})
			}
			deployedResources[resourceType][resourceName] = state
		}
	}

	return deployedResources, nil
}

// parseConfigurations parses new and current terraform configurations
func (s *Service) parseConfigurations(newTerraformJSON, currentState map[string]interface{}) (newResources []interfaces.Resource, currentResources []interfaces.Resource, err error) {
	converter := types.NewRequestConverterWithDefaults()
	newResources, _, err = converter.ParseTerraformJSON(newTerraformJSON)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse new terraform JSON: %w", err)
	}

	// Parse current configuration (if exists)
	if currentState != nil && currentState["terraform_json"] != nil {
		if currentJSON, ok := currentState["terraform_json"].(map[string]interface{}); ok {
			currentResources, _, _ = converter.ParseTerraformJSON(currentJSON)
		}
	}

	return newResources, currentResources, nil
}

// generateResourceChanges generates changes for all resources including creates, updates, and deletes
func (s *Service) generateResourceChanges(ctx context.Context, deploymentID string,
	newResources, currentResources []interfaces.Resource,
	providerConfigs map[string]map[string]interface{},
	providerVersions map[string]string,
	deployedResources map[string]map[string]interface{},
) ([]interfaces.ResourceChange, error) {
	// Load Terraform state to get resource information
	stateData, err := s.stateStore.LoadTerraformState(ctx, deploymentID)
	if err != nil {
		return nil, fmt.Errorf("failed to load terraform state: %w", err)
	}

	// Convert to a map for quick lookups
	statesByResourceKey := make(map[string]map[string]interface{})

	if stateData != nil {
		// Parse Terraform state
		var tfState interfaces.TerraformState
		if err := json.Unmarshal(stateData, &tfState); err != nil {
			return nil, fmt.Errorf("failed to parse terraform state: %w", err)
		}

		// Convert Terraform resources to the expected format
		for _, resource := range tfState.Resources {
			for _, instance := range resource.Instances {
				resourceKey := fmt.Sprintf("%s.%s", resource.Type, resource.Name)
				statesByResourceKey[resourceKey] = instance.Attributes
			}
		}
	}

	changes := make([]interfaces.ResourceChange, 0)

	// Process new and updated resources
	for _, newResource := range newResources {
		change, err := s.planResourceChangeWithState(ctx, deploymentID, newResource,
			providerConfigs, providerVersions, deployedResources, statesByResourceKey)
		if err != nil {
			return nil, err
		}
		changes = append(changes, change)
	}

	// Handle deleted resources
	deleteChanges := s.findDeletedResources(newResources, currentResources)
	changes = append(changes, deleteChanges...)

	return changes, nil
}

// planResourceChangeWithState plans a single resource change with pre-fetched state
func (s *Service) planResourceChangeWithState(ctx context.Context, deploymentID string,
	newResource interfaces.Resource,
	providerConfigs map[string]map[string]interface{},
	providerVersions map[string]string,
	deployedResources map[string]map[string]interface{},
	statesByResourceKey map[string]map[string]interface{},
) (interfaces.ResourceChange, error) {
	// Get current state from pre-fetched map
	resourceKey := string(interfaces.MakeResourceKey(newResource.Type, newResource.Name))
	currentResourceState := statesByResourceKey[resourceKey]

	// Get provider for this resource
	providerType := utils.ExtractProviderName(newResource.Type)
	providerVersion := providerVersions[providerType]
	if providerVersion == "" {
		return interfaces.ResourceChange{}, fmt.Errorf("provider version not specified for provider %s", providerType)
	}

	// Get provider instance
	providerConfig := interfaces.ProviderConfig(providerConfigs)
	provider, err := s.providerManager.GetProvider(ctx, deploymentID, providerType, providerVersion, providerConfig, newResource.Type)
	if err != nil {
		return interfaces.ResourceChange{}, fmt.Errorf("failed to get provider %s for resource %s: %w", providerType, newResource.Type, err)
	}

	// Resolve interpolations if needed
	userConfig := newResource.Properties
	if s.interpolator != nil {
		resolvedConfig, err := s.interpolator.ResolveInterpolations(userConfig, deployedResources)
		if err == nil {
			userConfig = resolvedConfig
		}
	}

	// Call provider planning
	plan, err := provider.PlanResourceChange(ctx, newResource.Type, currentResourceState, userConfig)
	if err != nil {
		return interfaces.ResourceChange{}, fmt.Errorf("failed to plan resource %s.%s: %w", newResource.Type, newResource.Name, err)
	}

	// Convert to ResourceChange
	changeKey := interfaces.MakeResourceKey(newResource.Type, newResource.Name)
	return interfaces.ResourceChange{
		ResourceKey:   changeKey,
		Action:        convertPlanActionToChangeAction(plan.Action),
		Before:        currentResourceState,
		After:         plan.PlannedState,
		ProposedState: userConfig,
		ReplacePaths:  plan.RequiresReplace,
	}, nil
}

// findDeletedResources identifies resources that exist in current but not in new configuration
func (s *Service) findDeletedResources(newResources, currentResources []interfaces.Resource) []interfaces.ResourceChange {
	var deleteChanges []interfaces.ResourceChange

	for _, currentResource := range currentResources {
		found := false
		for _, newResource := range newResources {
			if currentResource.Type == newResource.Type && currentResource.Name == newResource.Name {
				found = true
				break
			}
		}
		if !found {
			resourceKey := interfaces.MakeResourceKey(currentResource.Type, currentResource.Name)
			change := interfaces.ResourceChange{
				ResourceKey: resourceKey,
				Action:      interfaces.ActionDelete,
				Before:      currentResource.Properties,
				After:       nil,
			}
			deleteChanges = append(deleteChanges, change)
		}
	}

	return deleteChanges
}

// buildUpdatePlan assembles the final update plan with summary and dangerous changes
func (s *Service) buildUpdatePlan(deploymentID string, changes []interfaces.ResourceChange,
	providerConfigs map[string]map[string]interface{}, providerVersions map[string]string,
) *interfaces.UpdatePlan {
	summary := interfaces.UpdateSummary{}
	dangerousChanges := []interfaces.DangerousChange{}

	// Calculate execution order using centralized logic
	var executionOrder []interfaces.ResourceChange
	if s.dependencyResolver != nil && len(changes) > 0 {
		calculator := dependency.NewChangeOrderCalculator(s.dependencyResolver)
		orderedChanges, err := calculator.CalculateExecutionOrder(changes)
		if err != nil {
			// Log the error but don't fail - executor will fall back to its own analysis
			s.logger.Warnf("Failed to calculate execution order during plan: %v", err)
			executionOrder = nil
		} else {
			executionOrder = orderedChanges
		}
	}

	// Calculate summary and identify dangerous changes
	for _, change := range changes {
		switch change.Action {
		case interfaces.ActionCreate:
			summary.ToCreate++
		case interfaces.ActionUpdate:
			summary.ToUpdate++
		case interfaces.ActionDelete:
			summary.ToDelete++
			// Deletes are always dangerous
			dangerousChanges = append(dangerousChanges, interfaces.DangerousChange{
				ResourceKey: change.ResourceKey,
				Reason:      "Resource deletion",
				Impact:      "Data loss - resource will be permanently deleted",
			})
		case interfaces.ActionNoOp:
			// No-op changes don't affect counts
		case interfaces.ActionReplace:
			summary.ToReplace++
			// Replaces are dangerous
			dangerousChanges = append(dangerousChanges, interfaces.DangerousChange{
				ResourceKey: change.ResourceKey,
				Reason:      "Resource replacement",
				Impact:      "Resource will be deleted and recreated, potential downtime",
			})
		}
	}

	planID := fmt.Sprintf("plan-%s-%d", deploymentID, time.Now().Unix())

	return &interfaces.UpdatePlan{
		DeploymentID:     deploymentID,
		PlanID:           planID,
		Changes:          changes,
		ExecutionOrder:   executionOrder,
		Summary:          summary,
		DangerousChanges: dangerousChanges,
		CreatedAt:        time.Now(),
		RequiresApproval: len(dangerousChanges) > 0,
		ProviderConfigs:  providerConfigs,
		ProviderVersions: providerVersions,
	}
}

// DeploymentNeedsDestruction checks if a deployment has resources that need to be destroyed
func (s *Service) DeploymentNeedsDestruction(deploymentID string) (bool, error) {
	if deploymentID == "" {
		return false, errors.New("deployment ID is required")
	}

	// Check deployment result for resources
	result, err := s.tracker.GetResult(deploymentID)
	if err == nil && result != nil && len(result.Resources) > 0 {
		return true, nil
	}

	// Also check the deployment request
	deployment, err := s.GetDeploymentByID(deploymentID)
	if err == nil && deployment != nil && len(deployment.Request.Resources) > 0 {
		return true, nil
	}

	return false, nil
}

// validateDeploymentForUpdate checks if a deployment can be updated
func (s *Service) validateDeploymentForUpdate(status interfaces.DeploymentStatus) error {
	switch status {
	case interfaces.DeploymentStatusProcessing:
		return ErrDeploymentInProgress
	case interfaces.DeploymentStatusQueued:
		return ErrDeploymentQueued
	case interfaces.DeploymentStatusCanceled:
		return ErrDeploymentCanceled
	case interfaces.DeploymentStatusCanceling:
		return &Error{
			Code:       "DEPLOYMENT_CANCELING",
			Message:    "cannot update a deployment that is being canceled",
			HTTPStatus: 409,
		}
	case interfaces.DeploymentStatusDestroying:
		return ErrDeploymentDestroying
	case interfaces.DeploymentStatusDestroyed:
		return ErrDeploymentDestroyed
	case interfaces.DeploymentStatusDeleted:
		return &Error{
			Code:       "DEPLOYMENT_DELETED",
			Message:    "cannot update a deleted deployment",
			HTTPStatus: 410,
		}
	case interfaces.DeploymentStatusCompleted, interfaces.DeploymentStatusFailed:
		return nil // These can be updated
	default:
		return &Error{
			Code:       "INVALID_STATUS",
			Message:    fmt.Sprintf("invalid deployment status for update: %s", status),
			HTTPStatus: 400,
		}
	}
}

// queueDestructionJob creates and queues a destruction job for a deployment
func (s *Service) queueDestructionJob(deploymentID string) error {
	// Find the deployment
	deployment, err := s.GetDeploymentByID(deploymentID)
	if err != nil {
		return fmt.Errorf("failed to find deployment: %w", err)
	}

	// Create destruction deployment
	destructionDeployment := &interfaces.QueuedDeployment{
		ID:        fmt.Sprintf("destroy-%s-%d", deploymentID, time.Now().UnixNano()),
		Status:    interfaces.DeploymentStatusQueued,
		CreatedAt: time.Now(),
		Request: &interfaces.DeploymentRequest{
			Resources:   deployment.Request.Resources,
			DataSources: deployment.Request.DataSources,
			Options:     deployment.Request.Options,
			Metadata:    make(map[string]interface{}),
		},
	}

	// Copy metadata and mark for destruction
	for k, v := range deployment.Request.Metadata {
		destructionDeployment.Request.Metadata[k] = v
	}
	destructionDeployment.Request.Metadata[interfaces.MetadataKeyOperation] = interfaces.OperationDestroy
	destructionDeployment.Request.Metadata[interfaces.MetadataKeyOriginalDeploymentID] = deploymentID

	// Use transaction coordinator for atomic operations
	err = CreateDeploymentTransaction(s.txCoordinator, s.tracker, s.queue, destructionDeployment)
	if err != nil {
		return fmt.Errorf("failed to create destruction deployment: %w", err)
	}

	// Update original deployment status
	// This is a best-effort operation - if it fails, the destruction job is already queued
	// and will still execute. We log the error but don't fail the operation.
	if err := s.tracker.SetStatus(deploymentID, interfaces.DeploymentStatusDestroying); err != nil {
		// Log as warning since this is non-fatal - destruction will proceed regardless
		s.logger.Warnf("Failed to update deployment %s status to destroying (non-fatal): %v. Destruction job %s is already queued and will proceed.",
			deploymentID, err, destructionDeployment.ID)
	}

	return nil
}

// GetQueueMetrics returns current queue metrics
func (s *Service) GetQueueMetrics() interfaces.QueueMetrics {
	return s.queue.GetMetrics()
}

// Helper functions
func generateDeploymentID() string {
	return fmt.Sprintf("dep-%d", time.Now().UnixNano())
}

func generateUpdateID() string {
	return fmt.Sprintf("upd-%d", time.Now().UnixNano())
}

// convertPlanActionToChangeAction converts provider plan action to our change action
func convertPlanActionToChangeAction(action interfaces.PlanAction) interfaces.ChangeAction {
	switch action {
	case interfaces.PlanActionCreate:
		return interfaces.ActionCreate
	case interfaces.PlanActionUpdate:
		return interfaces.ActionUpdate
	case interfaces.PlanActionDelete:
		return interfaces.ActionDelete
	case interfaces.PlanActionReplace:
		return interfaces.ActionReplace
	case interfaces.PlanActionNoop:
		return interfaces.ActionNoOp
	default:
		return interfaces.ActionNoOp
	}
}

// convertTerraformToRequest converts terraform JSON to deployment request
func (s *Service) convertTerraformToRequest(terraformJSON map[string]interface{}) (*interfaces.DeploymentRequest, error) {
	// Use the centralized converter to parse terraform JSON
	resources, dataSources, err := s.converter.ParseTerraformJSON(terraformJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to parse terraform JSON: %w", err)
	}

	return &interfaces.DeploymentRequest{
		Resources:   resources,
		DataSources: dataSources,
		Options:     interfaces.DeploymentOptions{},
		Metadata:    make(map[string]interface{}),
	}, nil
}
