package apiserver

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"github.com/lattiam/lattiam/internal/config"
	"github.com/lattiam/lattiam/internal/deployment"
	"github.com/lattiam/lattiam/internal/provider"
	"github.com/lattiam/lattiam/internal/state"
	"github.com/lattiam/lattiam/pkg/provider/protocol"
)

// Static errors for err113 compliance
var (
	ErrDeploymentNotFound    = errors.New("deployment not found")
	ErrMissingResourceBlock  = errors.New("missing 'resource' block in Terraform JSON")
	ErrNoValidResourcesFound = errors.New("no valid resources found in Terraform JSON")
)

// DeploymentService manages the lifecycle of infrastructure deployments
type DeploymentService struct {
	deployer            *deployment.Deployer
	store               state.Store
	terraformStateStore *state.TerraformStateStore
	mu                  sync.RWMutex
	deploymentIndex     int
	config              *config.Config
}

// ResourceError represents a single resource failure with detailed information
type ResourceError struct {
	Resource string `json:"resource"`        // e.g., "aws_security_group_rule/web_http"
	Type     string `json:"type"`            // e.g., "aws_security_group_rule"
	Name     string `json:"name"`            // e.g., "web_http"
	Error    string `json:"error"`           // Full error message from provider
	Phase    string `json:"phase,omitempty"` // "plan", "apply", "dependency_resolution"
}

// Deployment represents a single infrastructure deployment
type Deployment struct {
	ID             string
	Name           string
	Status         DeploymentStatus
	Resources      []deployment.Resource
	Results        []deployment.Result
	CreatedAt      time.Time
	UpdatedAt      time.Time
	ResourceErrors []ResourceError        `json:"resource_errors,omitempty"` // Structured error information
	Config         *DeploymentConfig      `json:"config,omitempty"`          // AWS configuration
	Metadata       map[string]interface{} // Additional metadata for resource operations
	TerraformJSON  map[string]interface{} // Original Terraform JSON for provider version resolution
}

type DeploymentStatus string

const (
	StatusPending    DeploymentStatus = "pending"
	StatusPlanning   DeploymentStatus = "planning"
	StatusApplying   DeploymentStatus = "applying"
	StatusCompleted  DeploymentStatus = "completed"
	StatusFailed     DeploymentStatus = "failed"
	StatusDestroying DeploymentStatus = "destroying"
	StatusDestroyed  DeploymentStatus = "destroyed"
)

// DeploymentPlan represents a plan for deployment changes
type DeploymentPlan struct {
	DeploymentID  string                 `json:"deployment_id"`
	CurrentStatus string                 `json:"current_status"`
	ResourcePlans []ResourcePlan         `json:"resource_plans"`
	Summary       PlanSummary            `json:"summary"`
	TerraformJSON map[string]interface{} `json:"terraform_json,omitempty"`
}

// ResourcePlan represents the plan for a single resource
type ResourcePlan struct {
	Type          string                 `json:"type"`
	Name          string                 `json:"name"`
	Action        string                 `json:"action"` // create, update, destroy
	CurrentState  map[string]interface{} `json:"current_state,omitempty"`
	ProposedState map[string]interface{} `json:"proposed_state,omitempty"`
}

// PlanSummary provides a summary of planned changes
type PlanSummary struct {
	Create  int `json:"create"`
	Update  int `json:"update"`
	Destroy int `json:"destroy"`
	Total   int `json:"total"`
}

// NewDeploymentService creates a deployment service
func NewDeploymentService(providerDir string) (*DeploymentService, error) {
	logger := newAPILogger()
	deployer, err := deployment.NewDeployer(providerDir, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create deployer: %w", err)
	}

	cfg := config.LoadConfig()

	// Create state store
	stateDir := filepath.Join(os.Getenv("HOME"), ".lattiam", "state")
	if envDir := os.Getenv("LATTIAM_STATE_DIR"); envDir != "" {
		stateDir = envDir
	}

	store, err := state.NewFileStore(stateDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create state store: %w", err)
	}

	// Create Terraform state store
	tfStateStore, err := state.NewTerraformStateStore(stateDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create terraform state store: %w", err)
	}

	ds := &DeploymentService{
		deployer:            deployer,
		store:               store,
		terraformStateStore: tfStateStore,
		deploymentIndex:     0,
		config:              cfg,
	}

	// Recover deployments on startup
	ctx := context.Background()
	if err := ds.recoverDeployments(ctx); err != nil {
		log.Printf("Warning: failed to recover deployments: %v", err)
	}

	return ds, nil
}

// Close shuts down the deployment service and cleans up resources
func (ds *DeploymentService) Close() error {
	var errs []error

	if ds.deployer != nil {
		if err := ds.deployer.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close deployer: %w", err))
		}
	}

	if ds.store != nil {
		if err := ds.store.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close store: %w", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("close errors: %v", errs)
	}

	return nil
}

// getNextDeploymentIndex returns an incrementing counter for unique deployment IDs
func (ds *DeploymentService) getNextDeploymentIndex() int {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.deploymentIndex++
	return ds.deploymentIndex
}

// CreateDeployment creates a deployment from Terraform JSON
func (ds *DeploymentService) CreateDeployment(
	ctx context.Context, name string, terraformJSON map[string]interface{}, deployConfig *DeploymentConfig,
) (*Deployment, error) {
	// Parse all resources from Terraform JSON
	resources, err := ds.parseTerraformJSON(terraformJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to parse terraform JSON: %w", err)
	}

	// Create deployment record
	deployment := &Deployment{
		ID:            fmt.Sprintf("dep-%d-%d", time.Now().UnixNano(), ds.getNextDeploymentIndex()),
		Name:          name,
		Status:        StatusPending,
		Resources:     resources,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
		Config:        deployConfig,
		Metadata:      make(map[string]interface{}),
		TerraformJSON: terraformJSON,
	}

	// Convert to state and store
	stateDep := convertToStateDeployment(deployment)
	if err := ds.store.Save(ctx, stateDep); err != nil {
		return nil, fmt.Errorf("failed to save deployment: %w", err)
	}

	// Create a copy for return before starting goroutine
	deploymentCopy := *deployment

	// Start async deployment
	go func(deploymentID string) {
		// Use a new background context with timeout to prevent hanging
		// Don't use the request context as it gets canceled when the request completes
		ctx, cancel := context.WithTimeout(context.Background(), ds.config.DeploymentExecutionTimeout)
		defer cancel()
		ds.executeDeploymentByID(ctx, deploymentID)
	}(deployment.ID)

	return &deploymentCopy, nil
}

// GetDeployment retrieves a deployment by ID
func (ds *DeploymentService) GetDeployment(id string) (*Deployment, error) {
	ctx := context.Background()
	stateDep, err := ds.store.Load(ctx, id)
	if err != nil {
		if errors.Is(err, state.ErrDeploymentNotFound) {
			return nil, fmt.Errorf("%w: %s", ErrDeploymentNotFound, id)
		}
		return nil, fmt.Errorf("failed to load deployment: %w", err)
	}

	return convertFromStateDeployment(stateDep), nil
}

// ListDeployments returns all deployments
func (ds *DeploymentService) ListDeployments() []*Deployment {
	ctx := context.Background()
	stateDeployments, err := ds.store.List(ctx, "")
	if err != nil {
		log.Printf("Failed to list deployments: %v", err)
		return []*Deployment{}
	}

	deployments := make([]*Deployment, 0, len(stateDeployments))
	for _, stateDep := range stateDeployments {
		deployments = append(deployments, convertFromStateDeployment(stateDep))
	}
	return deployments
}

// UpdateDeployment updates an existing deployment
func (ds *DeploymentService) UpdateDeployment(ctx context.Context, dep *Deployment) (*Deployment, error) {
	// Update timestamp
	dep.UpdatedAt = time.Now()

	// Convert to state and update
	stateDep := convertToStateDeployment(dep)
	if err := ds.store.Update(ctx, stateDep); err != nil {
		return nil, fmt.Errorf("failed to update deployment: %w", err)
	}

	// If status is pending (meaning resources changed), start async deployment
	if dep.Status == StatusPending {
		// Create a copy for return before starting goroutine
		deploymentCopy := *dep

		// Start async deployment
		go func(deploymentID string) {
			// Use a new background context with timeout to prevent hanging
			// Don't use the request context as it gets canceled when the request completes
			ctx, cancel := context.WithTimeout(context.Background(), ds.config.DeploymentExecutionTimeout)
			defer cancel()
			ds.executeDeploymentByID(ctx, deploymentID)
		}(dep.ID)

		return &deploymentCopy, nil
	}

	return dep, nil
}

// PlanDeployment generates a plan for deployment changes
func (ds *DeploymentService) PlanDeployment(_ context.Context, id string, newTerraformJSON map[string]interface{}) (interface{}, error) {
	dep, err := ds.GetDeployment(id)
	if err != nil {
		return nil, err
	}

	// Determine what resources to plan
	resourcesToUse, terraformJSONToUse, err := ds.determineResourcesToPlan(dep, newTerraformJSON)
	if err != nil {
		return nil, err
	}

	// Create plan result structure
	planResult := &DeploymentPlan{
		DeploymentID:  id,
		CurrentStatus: string(dep.Status),
		ResourcePlans: []ResourcePlan{},
		TerraformJSON: terraformJSONToUse,
	}

	// Generate plans for each resource
	ds.generateResourcePlans(planResult, resourcesToUse, dep)

	// Calculate summary
	planResult.Summary = ds.calculatePlanSummary(planResult.ResourcePlans)

	return planResult, nil
}

// determineResourcesToPlan determines which resources to include in the plan
func (ds *DeploymentService) determineResourcesToPlan(dep *Deployment, newTerraformJSON map[string]interface{}) ([]deployment.Resource, map[string]interface{}, error) {
	if newTerraformJSON != nil {
		// Plan with new configuration
		newResources, err := ds.parseTerraformJSON(newTerraformJSON)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to parse new terraform JSON: %w", err)
		}
		return newResources, newTerraformJSON, nil
	}
	// Plan with existing configuration
	return dep.Resources, dep.TerraformJSON, nil
}

// generateResourcePlans generates plans for each resource
func (ds *DeploymentService) generateResourcePlans(planResult *DeploymentPlan, resourcesToUse []deployment.Resource, dep *Deployment) {
	for i, resource := range resourcesToUse {
		resourcePlan := ds.createResourcePlan(resource, i, dep, resourcesToUse)
		planResult.ResourcePlans = append(planResult.ResourcePlans, resourcePlan)
	}

	// Check for resources to destroy (in current but not in new)
	// Only check if we have a new terraform JSON (indicates an update)
	if len(planResult.TerraformJSON) > 0 && len(dep.TerraformJSON) > 0 {
		ds.addDestroyPlans(planResult, dep, resourcesToUse)
	}
}

// addDestroyPlans adds plans for resources that need to be destroyed
func (ds *DeploymentService) addDestroyPlans(planResult *DeploymentPlan, dep *Deployment, resourcesToUse []deployment.Resource) {
	for _, result := range dep.Results {
		if result.Error == nil && result.State != nil {
			if !resourceExistsInList(result.Resource, resourcesToUse) {
				resourcePlan := ResourcePlan{
					Type:          result.Resource.Type,
					Name:          result.Resource.Name,
					Action:        "destroy",
					CurrentState:  result.State,
					ProposedState: nil,
				}
				planResult.ResourcePlans = append(planResult.ResourcePlans, resourcePlan)
			}
		}
	}
}

// createResourcePlan creates a plan for a single resource
func (ds *DeploymentService) createResourcePlan(resource deployment.Resource, _ int, dep *Deployment, resourcesToUse []deployment.Resource) ResourcePlan {
	var currentState map[string]interface{}
	var action string

	// Look for the resource in Results by type and name, not by index
	for _, result := range dep.Results {
		if result.Resource.Type == resource.Type &&
			result.Resource.Name == resource.Name &&
			result.Error == nil &&
			result.State != nil {
			currentState = result.State
			action = "update"
			break
		}
	}

	// If we didn't find current state, it's a new resource
	if currentState == nil {
		action = "create"
	}

	// For destroy operations, check if resource is removed
	if currentState != nil && !resourceExistsInList(resource, resourcesToUse) {
		action = "destroy"
	}

	return ResourcePlan{
		Type:          resource.Type,
		Name:          resource.Name,
		Action:        action,
		CurrentState:  currentState,
		ProposedState: resource.Properties,
	}
}

// calculatePlanSummary calculates the summary of planned actions
func (ds *DeploymentService) calculatePlanSummary(resourcePlans []ResourcePlan) PlanSummary {
	return PlanSummary{
		Create:  countActions(resourcePlans, "create"),
		Update:  countActions(resourcePlans, "update"),
		Destroy: countActions(resourcePlans, "destroy"),
		Total:   len(resourcePlans),
	}
}

// DeleteDeployment destroys and removes a deployment
func (ds *DeploymentService) DeleteDeployment(_ context.Context, id string) error {
	dep, err := ds.GetDeployment(id)
	if err != nil {
		return err
	}

	// Update status
	ds.updateDeploymentStatus(dep, StatusDestroying)

	// Execute destruction
	go func(deploymentID string) {
		// Use a new background context with timeout
		// Don't use the request context as it gets canceled when the request completes
		ctx, cancel := context.WithTimeout(context.Background(), ds.config.DeploymentExecutionTimeout)
		defer cancel()
		ds.executeDestroyByID(ctx, deploymentID)
	}(id)

	return nil
}

// executeDeploymentByID performs the actual deployment by ID
func (ds *DeploymentService) executeDeploymentByID(ctx context.Context, deploymentID string) {
	// Get deployment from storage
	dep, err := ds.GetDeployment(deploymentID)
	if err != nil {
		log.Printf("Deployment %s not found: %v", deploymentID, err)
		return
	}

	ds.executeDeployment(ctx, dep)
}

// executeDeployment performs the actual deployment
func (ds *DeploymentService) executeDeployment(ctx context.Context, dep *Deployment) {
	// Update status to planning
	ds.updateDeploymentStatus(dep, StatusPlanning)

	log.Printf("Starting deployment %s with %d resources", dep.ID, len(dep.Resources))

	// Save the previous successful results before we start
	previousResults := dep.Results

	// Update status to applying
	ds.updateDeploymentStatus(dep, StatusApplying)

	// Create deployment options
	opts := ds.createDeploymentOptions(dep)

	// Build existing states map for updates
	existingStates := ds.buildExistingStatesMap(previousResults)

	// Execute the deployment
	results := ds.performDeployment(ctx, dep, opts, existingStates)

	// Update deployment with results and metadata
	ds.updateDeploymentResults(dep, results)
	ds.updateDeploymentMetadata(dep)

	// Process deployment results
	allSuccess, errors, resourceErrors := ds.processDeploymentResults(results)

	// Update final deployment status
	ds.updateFinalDeploymentStatus(dep, allSuccess, errors, resourceErrors, previousResults, results)

	// Save state and update store
	ds.saveDeploymentState(ctx, dep)
}

// createDeploymentOptions creates deployment options from the deployment config
func (ds *DeploymentService) createDeploymentOptions(dep *Deployment) deployment.Options {
	opts := deployment.Options{
		DryRun:        false,
		Logger:        newAPILogger(),
		TerraformJSON: dep.TerraformJSON,
	}

	// Extract provider configuration from Terraform JSON
	if providerCfg := ds.extractProviderConfigFromTerraformJSON(dep.TerraformJSON); providerCfg != nil {
		opts.ProviderConfig = providerCfg
	}

	return opts
}

// buildExistingStatesMap builds a map of existing resource states for updates
func (ds *DeploymentService) buildExistingStatesMap(previousResults []deployment.Result) map[string]map[string]interface{} {
	if len(previousResults) == 0 {
		return nil
	}

	existingStates := make(map[string]map[string]interface{})
	for _, result := range previousResults {
		if result.State != nil && result.Error == nil {
			key := fmt.Sprintf("%s/%s", result.Resource.Type, result.Resource.Name)
			existingStates[key] = result.State
		}
	}
	return existingStates
}

// performDeployment executes the deployment and returns results
func (ds *DeploymentService) performDeployment(ctx context.Context, dep *Deployment, opts deployment.Options, existingStates map[string]map[string]interface{}) []deployment.Result {
	stateLocation := ds.getTerraformStateLocation(dep.ID, dep.Config)

	// If we have existing states, skip planning path as it doesn't support updates
	if len(existingStates) > 0 {
		log.Printf("Deployment has existing resources, using update-aware deployment path")
		return ds.deployResourcesWithUpdateSupport(ctx, dep.Resources, opts, existingStates)
	}

	// Try planning-based deployment first for new deployments
	log.Printf("Attempting planning-based deployment for %s", dep.ID)
	planResults, err := ds.deployer.DeployResourcesWithPlan(
		ctx, dep.ID, dep.TerraformJSON, stateLocation, opts,
	)
	if err != nil {
		log.Printf("Failed to deploy with planning, falling back to direct deployment: %v", err)
		return ds.deployResourcesWithUpdateSupport(ctx, dep.Resources, opts, existingStates)
	}
	log.Printf("Planning-based deployment succeeded for %s", dep.ID)
	return planResults
}

// updateDeploymentResults updates the deployment with results
func (ds *DeploymentService) updateDeploymentResults(dep *Deployment, results []deployment.Result) {
	dep.Results = results
	dep.UpdatedAt = time.Now()
}

// updateDeploymentMetadata updates deployment metadata
func (ds *DeploymentService) updateDeploymentMetadata(dep *Deployment) {
	if dep.Metadata == nil {
		dep.Metadata = make(map[string]interface{})
	}

	// Store provider versions
	versionResolver := deployment.NewProviderVersionResolver(dep.TerraformJSON)
	dep.Metadata["provider_versions"] = versionResolver.GetProviderVersionsMap(dep.Resources)

	// Store environment info
	dep.Metadata["environment"] = map[string]interface{}{
		"localstack_enabled": os.Getenv("LOCALSTACK_ENDPOINT") != "",
		"state_dir":          os.Getenv("LATTIAM_STATE_DIR"),
		"timeout_config": map[string]interface{}{
			"provider_timeout":         ds.config.ProviderTimeout.String(),
			"provider_startup_timeout": ds.config.ProviderStartupTimeout.String(),
			"deployment_timeout":       ds.config.DeploymentExecutionTimeout.String(),
		},
	}

	// Store state file location
	dep.Metadata["terraform_state_file"] = ds.getTerraformStateLocation(dep.ID, dep.Config)
}

// processDeploymentResults processes results and returns success status and errors
func (ds *DeploymentService) processDeploymentResults(results []deployment.Result) (allSuccess bool, errors []string, resourceErrors []ResourceError) {
	allSuccess = true
	errors = make([]string, 0, len(results))
	resourceErrors = make([]ResourceError, 0)

	for _, result := range results {
		if result.Error == nil {
			continue
		}
		allSuccess = false
		errMsg := ds.formatDeploymentError(result)
		errors = append(errors, errMsg)

		// Create structured error
		phase := "apply"
		if strings.Contains(result.Error.Error(), "dependency resolution failed") {
			phase = "dependency_resolution"
		}

		resourceErr := ResourceError{
			Resource: fmt.Sprintf("%s/%s", result.Resource.Type, result.Resource.Name),
			Type:     result.Resource.Type,
			Name:     result.Resource.Name,
			Error:    result.Error.Error(),
			Phase:    phase,
		}
		resourceErrors = append(resourceErrors, resourceErr)
	}

	return allSuccess, errors, resourceErrors
}

// formatDeploymentError formats a deployment error with type analysis
func (ds *DeploymentService) formatDeploymentError(result deployment.Result) string {
	errMsg := fmt.Sprintf("%s/%s: %v", result.Resource.Type, result.Resource.Name, result.Error)

	// Check if this is an authentication error
	errStr := strings.ToLower(result.Error.Error())
	if strings.Contains(errStr, "credential") || strings.Contains(errStr, "profile") ||
		strings.Contains(errStr, "authentication") || strings.Contains(errStr, "unauthorized") {
		errMsg = fmt.Sprintf("%s/%s: authentication error - %v", result.Resource.Type, result.Resource.Name, result.Error)
	}

	return errMsg
}

// updateFinalDeploymentStatus updates the final deployment status based on results
func (ds *DeploymentService) updateFinalDeploymentStatus(dep *Deployment, allSuccess bool, _ []string, resourceErrors []ResourceError, previousResults, results []deployment.Result) {
	if allSuccess {
		ds.updateDeploymentStatus(dep, StatusCompleted)
		return
	}

	// If deployment failed, merge results intelligently
	if len(previousResults) > 0 && len(results) > 0 {
		mergedResults := ds.mergeDeploymentResults(previousResults, results)
		dep.Results = mergedResults
		log.Printf("Deployment %s failed, merged %d results preserving successful state", dep.ID, len(mergedResults))
	}

	// Set structured errors
	dep.ResourceErrors = resourceErrors
	ds.updateDeploymentStatus(dep, StatusFailed)
}

// saveDeploymentState saves Terraform state and updates the store
func (ds *DeploymentService) saveDeploymentState(ctx context.Context, dep *Deployment) {
	// Save Terraform state
	if err := ds.saveTerraformState(ctx, dep); err != nil {
		log.Printf("Failed to save Terraform state for deployment %s: %v", dep.ID, err)
	}

	// Save to store
	stateDep := convertToStateDeployment(dep)
	if err := ds.store.Update(ctx, stateDep); err != nil {
		log.Printf("Failed to update deployment %s in store: %v", dep.ID, err)
	}
}

// executeDestroyByID destroys the deployment resources by ID
func (ds *DeploymentService) executeDestroyByID(ctx context.Context, deploymentID string) {
	// Get deployment from storage
	dep, err := ds.GetDeployment(deploymentID)
	if err != nil {
		log.Printf("Deployment %s not found for destruction: %v", deploymentID, err)
		return
	}

	ds.executeDestroy(ctx, dep)
}

// executeDestroy destroys the deployment resources
func (ds *DeploymentService) executeDestroy(ctx context.Context, dep *Deployment) {
	log.Printf("Starting destruction of deployment %s with %d resources", dep.ID, len(dep.Results))

	// Create destruction options
	opts := ds.createDestroyOptions(dep)

	// Destroy resources and collect results
	successCount, errors, resourceErrors := ds.destroyResources(ctx, dep, opts)

	// Handle destruction results
	ds.handleDestroyResults(ctx, dep, successCount, errors, resourceErrors)
}

// createDestroyOptions creates options for resource destruction
func (ds *DeploymentService) createDestroyOptions(dep *Deployment) deployment.Options {
	opts := deployment.Options{
		DryRun: false,
		Logger: newAPILogger(),
	}

	// Log and use provider versions from metadata
	if providerVersion := ds.getProviderVersionFromMetadata(dep); providerVersion != "" {
		opts.ProviderVersion = providerVersion
	}

	// Set provider config if available
	if providerCfg := ds.extractProviderConfigFromTerraformJSON(dep.TerraformJSON); providerCfg != nil {
		opts.ProviderConfig = providerCfg
	}

	return opts
}

// getProviderVersionFromMetadata extracts provider version from deployment metadata
func (ds *DeploymentService) getProviderVersionFromMetadata(dep *Deployment) string {
	if dep.Metadata == nil {
		return ""
	}

	providerVersions, ok := dep.Metadata["provider_versions"].(map[string]string)
	if !ok {
		return ""
	}

	log.Printf("Deployment was created with provider versions: %v", providerVersions)

	// Return first version (single provider support for now)
	for _, version := range providerVersions {
		return version
	}

	return ""
}

// destroyResources destroys all resources in reverse order
func (ds *DeploymentService) destroyResources(ctx context.Context, dep *Deployment, opts deployment.Options) (successCount int, errors []string, resourceErrors []ResourceError) {
	successCount = 0
	resourceErrors = make([]ResourceError, 0)

	// Process resources in reverse order
	for i := len(dep.Results) - 1; i >= 0; i-- {
		result := dep.Results[i]
		if err := ds.destroySingleResult(ctx, result, opts); err != nil {
			errors = append(errors, err.Error())

			// Create structured error
			resourceErr := ResourceError{
				Resource: fmt.Sprintf("%s/%s", result.Resource.Type, result.Resource.Name),
				Type:     result.Resource.Type,
				Name:     result.Resource.Name,
				Error:    err.Error(),
				Phase:    "destroy",
			}
			resourceErrors = append(resourceErrors, resourceErr)
		} else {
			successCount++
		}
	}

	return successCount, errors, resourceErrors
}

// destroySingleResult destroys a single resource result
func (ds *DeploymentService) destroySingleResult(ctx context.Context, result deployment.Result, opts deployment.Options) error {
	// Skip if resource was not successfully created
	if result.Error != nil || result.State == nil {
		return nil //nolint:nilerr // intentionally returning nil for skipped resources
	}

	resource := result.Resource
	state := result.State

	log.Printf("Destroying resource %s/%s", resource.Type, resource.Name)

	// Destroy the resource
	if err := ds.destroyResource(ctx, resource, state, opts); err != nil {
		log.Printf("Failed to destroy %s/%s: %v", resource.Type, resource.Name, err)
		return fmt.Errorf("%s/%s: %w", resource.Type, resource.Name, err)
	}

	log.Printf("Successfully destroyed %s/%s", resource.Type, resource.Name)
	return nil
}

// handleDestroyResults updates status and cleanup based on destruction results
func (ds *DeploymentService) handleDestroyResults(ctx context.Context, dep *Deployment, _ int, errors []string, resourceErrors []ResourceError) {
	if len(errors) == 0 {
		ds.handleSuccessfulDestroy(ctx, dep)
	} else {
		ds.handleFailedDestroy(dep, errors, resourceErrors)
	}
}

// handleSuccessfulDestroy handles cleanup after successful destruction
func (ds *DeploymentService) handleSuccessfulDestroy(ctx context.Context, dep *Deployment) {
	ds.updateDeploymentStatus(dep, StatusDestroyed)
	log.Printf("Successfully destroyed all resources in deployment %s", dep.ID)

	// Clean up Terraform state
	if err := ds.terraformStateStore.DeleteState(ctx, dep.ID); err != nil {
		log.Printf("Failed to delete Terraform state for deployment %s: %v", dep.ID, err)
	}

	// Remove deployment from store
	if err := ds.store.Delete(ctx, dep.ID); err != nil {
		// Don't log errors if the store is closed (happens during shutdown)
		if !errors.Is(err, state.ErrStoreClosed) {
			log.Printf("Failed to delete deployment %s from store: %v", dep.ID, err)
		}
	}
}

// handleFailedDestroy handles partial destruction failures
func (ds *DeploymentService) handleFailedDestroy(dep *Deployment, errors []string, resourceErrors []ResourceError) {
	errorMsg := fmt.Sprintf("Failed to destroy %d resources: %s", len(errors), strings.Join(errors, "; "))
	dep.ResourceErrors = resourceErrors
	ds.updateDeploymentStatus(dep, StatusFailed)
	log.Printf("Failed to destroy deployment %s: %s", dep.ID, errorMsg)
}

// updatePartialDestructionState updates deployment state after partial destruction
//
//nolint:unused // kept for future partial destruction support
func (ds *DeploymentService) updatePartialDestructionState(dep *Deployment, errors []string) {
	// Build map of failed resources
	failedResources := make(map[string]bool)
	for _, errMsg := range errors {
		for _, result := range dep.Results {
			resourceKey := fmt.Sprintf("%s/%s:", result.Resource.Type, result.Resource.Name)
			if strings.Contains(errMsg, resourceKey) {
				failedResources[resourceKey] = true
			}
		}
	}

	// Keep only non-destroyed resources
	var remainingResults []deployment.Result
	for _, result := range dep.Results {
		resourceKey := fmt.Sprintf("%s/%s:", result.Resource.Type, result.Resource.Name)
		if failedResources[resourceKey] {
			remainingResults = append(remainingResults, result)
		}
	}

	dep.Results = remainingResults

	// Save updated Terraform state
	ctx := context.Background()
	if err := ds.saveTerraformState(ctx, dep); err != nil {
		log.Printf("Failed to update Terraform state after partial destroy for deployment %s: %v", dep.ID, err)
	}
}

// destroyResource destroys a single resource
// deployResourcesWithUpdateSupport deploys resources with update support
func (ds *DeploymentService) deployResourcesWithUpdateSupport(
	ctx context.Context, resources []deployment.Resource, opts deployment.Options,
	existingStates map[string]map[string]interface{},
) []deployment.Result {
	// If no existing states, use the deployer's multi-resource deployment for dependency ordering
	if len(existingStates) == 0 {
		log.Printf("Using fallback deployment with dependency ordering for %d resources", len(resources))
		return ds.deployer.DeployResources(ctx, resources, opts)
	}

	// For updates, we need to handle each resource individually but in dependency order
	log.Printf("Using update-aware deployment with dependency ordering for %d resources", len(resources))

	// Prepare deployment context
	orderedResources := deployment.AnalyzeResourceDependencies(resources)
	results := ds.initializeResults(resources)
	deployedStates := ds.copyExistingStates(existingStates)

	// Create resource index map for quick lookup
	resourceIndexMap := ds.createResourceIndexMap(resources)

	// Deploy resources in dependency order
	for _, resource := range orderedResources {
		originalIndex, found := resourceIndexMap[ds.resourceKey(resource)]
		if !found {
			log.Printf("Could not find original index for resource %s.%s", resource.Type, resource.Name)
			continue
		}

		ds.deployOrUpdateResource(ctx, resource, originalIndex, existingStates, deployedStates, results, opts)
	}

	return results
}

// initializeResults creates initial results array with resources
func (ds *DeploymentService) initializeResults(resources []deployment.Resource) []deployment.Result {
	results := make([]deployment.Result, len(resources))
	for i, resource := range resources {
		results[i].Resource = resource
	}
	return results
}

// copyExistingStates creates a copy of existing states for tracking deployments
func (ds *DeploymentService) copyExistingStates(existingStates map[string]map[string]interface{}) map[string]map[string]interface{} {
	deployedStates := make(map[string]map[string]interface{})
	for key, state := range existingStates {
		deployedStates[key] = state
	}
	return deployedStates
}

// createResourceIndexMap creates a map for quick resource index lookup
func (ds *DeploymentService) createResourceIndexMap(resources []deployment.Resource) map[string]int {
	indexMap := make(map[string]int)
	for i, resource := range resources {
		indexMap[ds.resourceKey(resource)] = i
	}
	return indexMap
}

// resourceKey generates a unique key for a resource
func (ds *DeploymentService) resourceKey(resource deployment.Resource) string {
	return fmt.Sprintf("%s/%s", resource.Type, resource.Name)
}

// deployOrUpdateResource handles deployment or update of a single resource
func (ds *DeploymentService) deployOrUpdateResource(
	ctx context.Context, resource deployment.Resource, originalIndex int,
	existingStates, deployedStates map[string]map[string]interface{},
	results []deployment.Result, opts deployment.Options,
) {
	key := ds.resourceKey(resource)
	existingState := existingStates[key]

	if existingState != nil {
		ds.handleResourceUpdate(ctx, resource, existingState, deployedStates, results, originalIndex, opts)
	} else {
		ds.handleResourceCreate(ctx, resource, deployedStates, results, originalIndex, opts)
	}
}

// handleResourceUpdate updates an existing resource
func (ds *DeploymentService) handleResourceUpdate(
	ctx context.Context, resource deployment.Resource, existingState map[string]interface{},
	deployedStates map[string]map[string]interface{}, results []deployment.Result,
	originalIndex int, opts deployment.Options,
) {
	key := ds.resourceKey(resource)
	log.Printf("Updating existing resource %s", key)

	state, err := ds.updateResource(ctx, resource, existingState, deployedStates, opts)
	if err != nil {
		results[originalIndex].Error = err
		log.Printf("Failed to update resource %s: %v", key, err)
		return
	}

	results[originalIndex].State = state
	deployedStates[key] = state
}

// handleResourceCreate creates a new resource
func (ds *DeploymentService) handleResourceCreate(
	ctx context.Context, resource deployment.Resource,
	deployedStates map[string]map[string]interface{}, results []deployment.Result,
	originalIndex int, opts deployment.Options,
) {
	key := ds.resourceKey(resource)
	log.Printf("Creating new resource %s", key)

	result, err := ds.deployer.DeployResource(ctx, resource, opts)
	if err != nil {
		results[originalIndex].Error = err
		log.Printf("Failed to create resource %s: %v", key, err)
		return
	}

	results[originalIndex] = *result
	if result.State != nil {
		deployedStates[key] = result.State
	}
}

// updateResource updates an existing resource
func (ds *DeploymentService) updateResource(
	ctx context.Context, resource deployment.Resource, currentState map[string]interface{},
	deployedStates map[string]map[string]interface{}, opts deployment.Options,
) (map[string]interface{}, error) {
	providerName := deployment.ExtractProviderName(resource.Type)

	// Use provider version from opts if set (comes from deployment metadata)
	version := opts.ProviderVersion
	if version == "" {
		version = ds.getProviderVersion(providerName, "")
	}

	// Get provider instance
	instance, err := ds.deployer.GetManager().GetProviderWithConfig(ctx, providerName, version, opts.ProviderConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to get provider %s: %w", providerName, err)
	}
	defer func() {
		if err := instance.Stop(); err != nil {
			log.Printf("Failed to stop provider instance: %v", err)
		}
	}()

	// Create client for this resource type
	client, err := protocol.NewTerraformCompatibleClient(ctx, instance, resource.Type)
	if err != nil {
		return nil, fmt.Errorf("failed to create client for %s: %w", resource.Type, err)
	}
	defer client.Close()

	// Get provider schema for configuration
	providerSchema, err := client.GetProviderSchema(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get provider schema: %w", err)
	}

	// Configure provider
	if err := ds.configureProviderForUpdate(ctx, client, providerName, providerSchema, opts.ProviderConfig); err != nil {
		return nil, fmt.Errorf("failed to configure provider: %w", err)
	}

	// Resolve interpolations in resource properties
	resolvedProperties, err := deployment.ResolveInterpolations(resource.Properties, deployedStates)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve interpolations: %w", err)
	}

	// Try to update the resource
	newState, err := client.Update(ctx, currentState, resolvedProperties)
	if err != nil {
		// Check if this is a requires replace error
		var requiresReplace *protocol.ErrRequiresReplace
		if errors.As(err, &requiresReplace) {
			log.Printf("Resource %s/%s requires replacement for paths: %v", resource.Type, resource.Name, requiresReplace.Paths)
			// Perform replacement: create new resource, then delete old one
			return ds.performResourceReplacement(ctx, client, resource, resolvedProperties, currentState, opts)
		}
		return nil, fmt.Errorf("failed to update resource: %w", err)
	}

	return newState, nil
}

// performResourceReplacement handles resource replacement (create new, delete old)
func (ds *DeploymentService) performResourceReplacement(
	ctx context.Context, client *protocol.TerraformCompatibleClient,
	resource deployment.Resource, resolvedProperties map[string]interface{}, currentState map[string]interface{}, _ deployment.Options,
) (map[string]interface{}, error) {
	log.Printf("Performing resource replacement for %s/%s", resource.Type, resource.Name)

	// Step 1: Create the new resource with the new configuration
	log.Printf("Creating replacement resource %s/%s", resource.Type, resource.Name)
	newState, err := client.Create(ctx, resolvedProperties)
	if err != nil {
		return nil, fmt.Errorf("failed to create replacement resource: %w", err)
	}

	// Step 2: Delete the old resource
	// Extract the resource ID (usually from 'id' field)
	resourceID, ok := currentState["id"].(string)
	if !ok || resourceID == "" {
		// Try to extract from state if id is missing
		if idVal, exists := currentState["arn"]; exists {
			resourceID = idVal.(string)
		} else if nameVal, exists := currentState["name"]; exists {
			resourceID = nameVal.(string)
		} else {
			// Use the first non-empty string value as fallback
			for key, val := range currentState {
				if strVal, ok := val.(string); ok && strVal != "" {
					resourceID = strVal
					log.Printf("Using %s=%s as resource ID for deletion", key, strVal)
					break
				}
			}
		}
	}

	log.Printf("Deleting old resource %s/%s (ID: %s)", resource.Type, resource.Name, resourceID)
	if err := client.Delete(ctx, resourceID, currentState); err != nil {
		// If deletion fails, we have a problem - the new resource exists but old one couldn't be deleted
		// Log the error but return the new state since the creation succeeded
		log.Printf("WARNING: Failed to delete old resource %s/%s: %v. Manual cleanup may be required.",
			resource.Type, resource.Name, err)
		// Still return the new state since creation succeeded
		return newState, nil
	}

	log.Printf("Successfully replaced resource %s/%s", resource.Type, resource.Name)
	return newState, nil
}

// configureProviderForUpdate configures the provider for resource update
func (ds *DeploymentService) configureProviderForUpdate(
	ctx context.Context, client *protocol.TerraformCompatibleClient, providerName string,
	providerSchema *tfprotov6.GetProviderSchemaResponse, providerConfig protocol.ProviderConfig,
) error {
	// Use the same configuration as for destroy
	return ds.configureProviderForDestroy(ctx, client, providerName, providerSchema, providerConfig)
}

func (ds *DeploymentService) destroyResource(
	ctx context.Context, resource deployment.Resource, state map[string]interface{}, opts deployment.Options,
) error {
	providerName := deployment.ExtractProviderName(resource.Type)
	version := ds.getProviderVersion(providerName, opts.ProviderVersion)

	// Get provider instance
	instance, err := ds.deployer.GetManager().GetProviderWithConfig(ctx, providerName, version, opts.ProviderConfig)
	if err != nil {
		return fmt.Errorf("failed to get provider %s: %w", providerName, err)
	}
	defer func() {
		if err := instance.Stop(); err != nil {
			log.Printf("Failed to stop provider instance: %v", err)
		}
	}()

	// Create client for this resource type
	client, err := protocol.NewTerraformCompatibleClient(ctx, instance, resource.Type)
	if err != nil {
		return fmt.Errorf("failed to create client for %s: %w", resource.Type, err)
	}
	defer client.Close()

	// Get provider schema for configuration
	providerSchema, err := client.GetProviderSchema(ctx)
	if err != nil {
		return fmt.Errorf("failed to get provider schema: %w", err)
	}

	// Configure provider
	if err := ds.configureProviderForDestroy(ctx, client, providerName, providerSchema, opts.ProviderConfig); err != nil {
		return fmt.Errorf("failed to configure provider: %w", err)
	}

	// Ensure state has resource ID
	resourceID, ok := state["id"].(string)
	if !ok || resourceID == "" {
		return fmt.Errorf("resource %s/%s has no ID in state", resource.Type, resource.Name)
	}

	// Delete the resource
	if err := client.Delete(ctx, resourceID, state); err != nil {
		return fmt.Errorf("failed to delete resource: %w", err)
	}

	return nil
}

// configureProviderForDestroy configures the provider for resource destruction
func (ds *DeploymentService) configureProviderForDestroy(
	ctx context.Context, client *protocol.TerraformCompatibleClient, providerName string,
	providerSchema *tfprotov6.GetProviderSchemaResponse, providerConfig protocol.ProviderConfig,
) error {
	configurator := provider.NewProviderConfigurator()

	// Build environment config
	envConfig := &provider.EnvironmentConfig{
		AttributeOverrides: make(map[string]interface{}),
	}

	// Apply provider-specific configuration
	if providerConfig != nil {
		if config, ok := providerConfig[providerName]; ok {
			for key, value := range config {
				envConfig.AttributeOverrides[key] = value
			}
		}
	}

	// Apply endpoint-specific configuration
	endpointStrategy := provider.GetEndpointStrategy()
	endpointStrategy.ConfigureEndpoint(envConfig)

	configData, err := configurator.ConfigureProvider(
		ctx,
		providerName,
		providerSchema.Provider.Block,
		map[string]interface{}{},
		envConfig,
	)
	if err != nil {
		return fmt.Errorf("failed to configure provider: %w", err)
	}

	if err := client.ConfigureProvider(ctx, configData); err != nil {
		return fmt.Errorf("failed to configure provider client: %w", err)
	}
	return nil
}

// getProviderVersion returns the version for a provider
func (ds *DeploymentService) getProviderVersion(providerName, override string) string {
	if override != "" {
		// Handle compatibility issues for problematic versions
		if providerName == "random" && override == "3.7.2" {
			log.Printf("WARNING: random provider 3.7.2 has ARM64 compatibility issues, using 3.6.0 instead")
			return "3.6.0"
		}
		return override
	}

	switch providerName {
	case "aws":
		return "6.0.0"
	case "azurerm":
		return "3.85.0"
	case "google":
		return "5.10.0"
	case "random":
		return "3.6.0"
	default:
		return "latest"
	}
}

// resourceExistsInList checks if a resource exists in a list of resources
func resourceExistsInList(resource deployment.Resource, resources []deployment.Resource) bool {
	for _, r := range resources {
		if r.Type == resource.Type && r.Name == resource.Name {
			return true
		}
	}
	return false
}

// countActions counts the number of resources with a specific action
func countActions(plans []ResourcePlan, action string) int {
	count := 0
	for _, plan := range plans {
		if plan.Action == action {
			count++
		}
	}
	return count
}

// ParseTerraformJSON extracts all resources from Terraform JSON (exported for server use)
func (ds *DeploymentService) ParseTerraformJSON(terraformJSON map[string]interface{}) ([]deployment.Resource, error) {
	return ds.parseTerraformJSON(terraformJSON)
}

// parseTerraformJSON extracts all resources from Terraform JSON
func (ds *DeploymentService) parseTerraformJSON(terraformJSON map[string]interface{}) ([]deployment.Resource, error) {
	resources, ok := terraformJSON["resource"].(map[string]interface{})
	if !ok {
		return nil, ErrMissingResourceBlock
	}

	var deploymentResources []deployment.Resource

	// Iterate through all resource types
	for resourceType, instances := range resources {
		instanceMap, ok := instances.(map[string]interface{})
		if !ok {
			continue
		}

		// Process each instance of this resource type
		for instanceName, config := range instanceMap {
			configMap, ok := config.(map[string]interface{})
			if !ok {
				continue
			}

			resource := deployment.Resource{
				Type:       resourceType,
				Name:       instanceName,
				Properties: configMap,
			}
			deploymentResources = append(deploymentResources, resource)
		}
	}

	if len(deploymentResources) == 0 {
		return nil, ErrNoValidResourcesFound
	}

	return deploymentResources, nil
}

// updateDeploymentStatus updates the deployment status
func (ds *DeploymentService) updateDeploymentStatus(dep *Deployment, status DeploymentStatus) {
	dep.Status = status
	dep.UpdatedAt = time.Now()

	// Save to store
	ctx := context.Background()
	stateDep := convertToStateDeployment(dep)
	if err := ds.store.Update(ctx, stateDep); err != nil {
		// Don't log errors if the store is closed (happens during shutdown) or if deployment not found (common in tests)
		if !errors.Is(err, state.ErrStoreClosed) && !errors.Is(err, state.ErrDeploymentNotFound) {
			log.Printf("Failed to update deployment status for %s: %v", dep.ID, err)
		}
	}
}

// recoverDeployments loads deployments from store on startup
// extractProviderConfigFromTerraformJSON extracts provider configuration from Terraform JSON
func (ds *DeploymentService) extractProviderConfigFromTerraformJSON(terraformJSON map[string]interface{}) protocol.ProviderConfig {
	providerBlock, ok := terraformJSON["provider"].(map[string]interface{})
	if !ok {
		return nil
	}

	config := make(protocol.ProviderConfig)

	for providerName, providerConfigI := range providerBlock {
		if providerConfig, ok := providerConfigI.(map[string]interface{}); ok {
			config[providerName] = providerConfig
		}
	}

	if len(config) > 0 {
		return config
	}

	return nil
}

func (ds *DeploymentService) recoverDeployments(ctx context.Context) error {
	stateDeployments, err := ds.store.List(ctx, "")
	if err != nil {
		return fmt.Errorf("failed to list deployments: %w", err)
	}

	now := time.Now()
	for _, stateDep := range stateDeployments {
		ds.handleStaleDeployment(ctx, stateDep, now)
		ds.updateDeploymentIndex(stateDep.ID)
	}

	log.Printf("Recovered deployments. Next deployment index: %d", ds.deploymentIndex+1)
	return nil
}

// handleStaleDeployment checks and handles stale in-progress deployments
func (ds *DeploymentService) handleStaleDeployment(ctx context.Context, stateDep *state.DeploymentState, now time.Time) {
	if !ds.isInProgressDeployment(stateDep.Status) {
		return
	}

	age := now.Sub(stateDep.UpdatedAt)
	if age > 10*time.Minute {
		ds.markDeploymentAsStale(ctx, stateDep, now, age)
	} else {
		log.Printf("Deployment %s is in-progress but not stale (age: %v)", stateDep.ID, age)
	}
}

// isInProgressDeployment checks if deployment status indicates in-progress
func (ds *DeploymentService) isInProgressDeployment(status string) bool {
	return status == string(StatusPlanning) || status == string(StatusApplying)
}

// markDeploymentAsStale marks a stale deployment as failed
func (ds *DeploymentService) markDeploymentAsStale(ctx context.Context, stateDep *state.DeploymentState, now time.Time, age time.Duration) {
	log.Printf("Marking stale deployment %s as failed (age: %v)", stateDep.ID, age)
	stateDep.Status = string(StatusFailed)
	stateDep.UpdatedAt = now
	if err := ds.store.Update(ctx, stateDep); err != nil {
		log.Printf("Failed to update stale deployment %s: %v", stateDep.ID, err)
	}
}

// updateDeploymentIndex updates the deployment index based on recovered deployment IDs
func (ds *DeploymentService) updateDeploymentIndex(deploymentID string) {
	if !strings.HasPrefix(deploymentID, "dep-") {
		return
	}

	parts := strings.Split(deploymentID, "-")
	if len(parts) < 3 {
		return
	}

	// Try to extract the index from the ID
	idxStr := parts[len(parts)-1]
	if idx, err := strconv.Atoi(idxStr); err == nil && idx > ds.deploymentIndex {
		ds.deploymentIndex = idx
	}
}

// mergeDeploymentResults intelligently merges deployment results when an update partially fails
// It keeps successful updates from the current deployment and preserves previous state
// for resources that failed or weren't included in the update
func (ds *DeploymentService) mergeDeploymentResults(previousResults, currentResults []deployment.Result) []deployment.Result {
	// Create a map to track results by resource key
	resultMap := make(map[string]deployment.Result)

	// First, add all previous results to the map
	for _, result := range previousResults {
		key := fmt.Sprintf("%s/%s", result.Resource.Type, result.Resource.Name)
		resultMap[key] = result
	}

	// Then, overlay current results, keeping successful updates
	for _, result := range currentResults {
		key := fmt.Sprintf("%s/%s", result.Resource.Type, result.Resource.Name)

		// If the current deployment succeeded for this resource, use the new result
		if result.Error == nil && result.State != nil {
			resultMap[key] = result
		}
		// If it failed but we don't have a previous result, still include it
		// so the error is visible
		if _, exists := resultMap[key]; !exists {
			resultMap[key] = result
		}
		// If it failed and we have a previous successful result, keep the previous
		// (which is already in the map)
	}

	// Convert map back to slice
	mergedResults := make([]deployment.Result, 0, len(resultMap))
	for _, result := range resultMap {
		mergedResults = append(mergedResults, result)
	}

	return mergedResults
}

// getTerraformStateLocation returns the location (path or URI) for a deployment's Terraform state
func (ds *DeploymentService) getTerraformStateLocation(deploymentID string, config *DeploymentConfig) string {
	// First check if deployment has a specific state backend configured
	if config != nil && config.StateBackend != "" {
		backend := config.StateBackend
		// Ensure backend ends with /
		if !strings.HasSuffix(backend, "/") {
			backend += "/"
		}
		return backend + deploymentID + ".tfstate"
	}

	// Fall back to environment variable configuration
	if backend := os.Getenv("LATTIAM_TERRAFORM_BACKEND"); backend != "" {
		// Examples:
		// LATTIAM_TERRAFORM_BACKEND=s3://my-bucket/terraform-state/
		// LATTIAM_TERRAFORM_BACKEND=gs://my-bucket/terraform-state/
		// LATTIAM_TERRAFORM_BACKEND=azurerm://mystorageaccount/terraform-state/
		if !strings.HasSuffix(backend, "/") {
			backend += "/"
		}
		return backend + deploymentID + ".tfstate"
	}

	// Default to local file storage
	stateDir := filepath.Join(os.Getenv("HOME"), ".lattiam", "state", "terraform")
	if envDir := os.Getenv("LATTIAM_STATE_DIR"); envDir != "" {
		stateDir = filepath.Join(envDir, "terraform")
	}

	// Return as file:// URI for consistency
	return "file://" + filepath.Join(stateDir, deploymentID+".tfstate")
}

// saveTerraformState saves the deployment results as a Terraform state file
func (ds *DeploymentService) saveTerraformState(ctx context.Context, dep *Deployment) error {
	// Load existing state or create new one
	tfState, err := ds.terraformStateStore.LoadState(ctx, dep.ID)
	if err != nil {
		log.Printf("Creating new Terraform state for deployment %s", dep.ID)
	}

	// Convert deployment results to Terraform state resources
	// Pre-allocate slice with capacity for successful results
	successfulCount := 0
	for _, result := range dep.Results {
		if result.Error == nil {
			successfulCount++
		}
	}
	tfResources := make([]state.TerraformStateResource, 0, successfulCount)

	for _, result := range dep.Results {
		// Skip failed resources
		if result.Error != nil {
			continue
		}

		// Extract provider from resource type (e.g., "aws_s3_bucket" -> "aws")
		providerName := deployment.ExtractProviderName(result.Resource.Type)

		tfResource := state.TerraformStateResource{
			Mode:     "managed",
			Type:     result.Resource.Type,
			Name:     result.Resource.Name,
			Provider: fmt.Sprintf("provider[%q]", providerName),
			Instances: []state.TerraformStateResourceInstance{
				{
					SchemaVersion: 0, // TODO: Get from provider schema
					Attributes:    result.State,
				},
			},
		}

		tfResources = append(tfResources, tfResource)
	}

	// Update state
	tfState.Resources = tfResources
	tfState.TerraformVersion = "1.5.0" // TODO: Make configurable

	// Save state
	if err := ds.terraformStateStore.SaveState(ctx, dep.ID, tfState); err != nil {
		return fmt.Errorf("failed to save terraform state: %w", err)
	}
	return nil
}

// loadTerraformState loads the Terraform state for a deployment
//
//nolint:unused // kept for future terraform state management
func (ds *DeploymentService) loadTerraformState(ctx context.Context, deploymentID string) (*state.TerraformState, error) {
	tfState, err := ds.terraformStateStore.LoadState(ctx, deploymentID)
	if err != nil {
		return nil, fmt.Errorf("failed to load terraform state: %w", err)
	}
	return tfState, nil
}
