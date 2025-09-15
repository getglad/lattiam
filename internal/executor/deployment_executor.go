// Package executor provides deployment execution logic and resource management for infrastructure deployments.
package executor

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/lattiam/lattiam/internal/dependency"
	"github.com/lattiam/lattiam/internal/deployment"
	"github.com/lattiam/lattiam/internal/events"
	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/internal/interpolation"
	"github.com/lattiam/lattiam/internal/logging"
	"github.com/lattiam/lattiam/internal/utils"
)

const (
	// awsS3BucketType is the resource type for AWS S3 buckets
	awsS3BucketType = "aws_s3_bucket"
)

// DeploymentExecutor handles the execution of deployments
type DeploymentExecutor struct {
	providerManager    interfaces.ProviderLifecycleManager
	eventBus           *events.EventBus
	stateStore         interfaces.StateStore
	timeout            time.Duration
	interpolator       interfaces.InterpolationResolver
	dependencyResolver interfaces.DependencyResolver
	logger             *logging.Logger
}

// executionContext tracks state during deployment execution
type executionContext struct {
	deploymentID      string
	deployedResources map[string]interface{}
	providerConfigs   map[string]map[string]interface{}
}

// New creates a new deployment executor
func New(
	providerManager interfaces.ProviderLifecycleManager,
	stateStore interfaces.StateStore,
	timeout time.Duration,
) *DeploymentExecutor {
	opts := []Option{}
	if timeout > 0 {
		opts = append(opts, WithTimeout(timeout))
	}
	return NewWithOptions(providerManager, stateStore, opts...)
}

// NewWithEventBus creates a new deployment executor with a custom event bus
func NewWithEventBus(
	providerManager interfaces.ProviderLifecycleManager,
	stateStore interfaces.StateStore,
	eventBus *events.EventBus,
	timeout time.Duration,
) *DeploymentExecutor {
	opts := []Option{
		WithEventBus(eventBus),
	}
	if timeout > 0 {
		opts = append(opts, WithTimeout(timeout))
	}
	return NewWithOptions(providerManager, stateStore, opts...)
}

// GetEventBus returns the event bus for subscribing to events
func (e *DeploymentExecutor) GetEventBus() *events.EventBus {
	return e.eventBus
}

// Option is a functional option for configuring a DeploymentExecutor
type Option func(*DeploymentExecutor)

// WithTimeout sets a custom timeout for the executor
func WithTimeout(timeout time.Duration) Option {
	return func(e *DeploymentExecutor) {
		e.timeout = timeout
	}
}

// WithEventBus sets a custom event bus for the executor
func WithEventBus(eventBus *events.EventBus) Option {
	return func(e *DeploymentExecutor) {
		e.eventBus = eventBus
	}
}

// WithInterpolator sets a custom interpolation resolver
func WithInterpolator(interpolator interfaces.InterpolationResolver) Option {
	return func(e *DeploymentExecutor) {
		e.interpolator = interpolator
	}
}

// WithDependencyResolver sets a custom dependency resolver
func WithDependencyResolver(resolver interfaces.DependencyResolver) Option {
	return func(e *DeploymentExecutor) {
		e.dependencyResolver = resolver
	}
}

// WithLogger sets a custom logger
func WithLogger(logger *logging.Logger) Option {
	return func(e *DeploymentExecutor) {
		e.logger = logger
	}
}

// NewWithOptions creates a new deployment executor with functional options
func NewWithOptions(
	providerManager interfaces.ProviderLifecycleManager,
	stateStore interfaces.StateStore,
	opts ...Option,
) *DeploymentExecutor {
	// Create executor with defaults
	executor := &DeploymentExecutor{
		providerManager:    providerManager,
		stateStore:         stateStore,
		eventBus:           events.NewEventBus(),
		timeout:            10 * time.Minute,
		interpolator:       interpolation.NewHCLInterpolationResolver(false),
		dependencyResolver: dependency.NewProductionDependencyResolver(false),
		logger:             logging.NewLogger("executor"),
	}

	// Apply options
	for _, opt := range opts {
		opt(executor)
	}

	return executor
}

// sortResourcesByDependencies sorts resources using the DependencyResolver for proper topological ordering
func (e *DeploymentExecutor) sortResourcesByDependencies(resources []interfaces.Resource) ([]interfaces.Resource, error) {
	if len(resources) == 0 {
		return resources, nil
	}

	// Build dependency graph using dependency resolver
	graph, err := e.dependencyResolver.BuildDependencyGraph(resources, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build dependency graph: %w", err)
	}

	// Get proper execution order
	executionOrder, err := e.dependencyResolver.GetExecutionOrder(graph)
	if err != nil {
		return nil, fmt.Errorf("failed to determine execution order: %w", err)
	}

	// Create a map for quick resource lookup
	resourceMap := make(map[string]*interfaces.Resource)
	for i := range resources {
		key := fmt.Sprintf("%s.%s", resources[i].Type, resources[i].Name)
		resourceMap[key] = &resources[i]
	}

	// Build sorted result based on execution order
	sortedResources := make([]interfaces.Resource, 0, len(resources))
	for _, key := range executionOrder {
		if resource, ok := resourceMap[key]; ok {
			sortedResources = append(sortedResources, *resource)
		}
	}

	return sortedResources, nil
}

// analyzeUpdateDependencies analyzes resource changes and determines the proper execution order
// considering dependencies between resources. This ensures that:
// - Resources are created before resources that depend on them
// - Resources that depend on others are deleted before their dependencies
// - Updates and replaces respect dependency ordering
// - Operations are interleaved based on actual dependencies, not just action type
func (e *DeploymentExecutor) analyzeUpdateDependencies(changes []interfaces.ResourceChange) ([]interfaces.ResourceChange, error) {
	// Use the centralized change order calculator
	calculator := dependency.NewChangeOrderCalculator(e.dependencyResolver)
	orderedChanges, err := calculator.CalculateExecutionOrder(changes)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate execution order: %w", err)
	}
	return orderedChanges, nil
}

// sortResourcesForDestruction sorts resources in reverse dependency order for destruction
// If A depends on B during creation, then B depends on A during destruction
func (e *DeploymentExecutor) sortResourcesForDestruction(resources []interfaces.Resource) ([]interfaces.Resource, error) {
	if len(resources) == 0 {
		return resources, nil
	}

	// Build dependency graph using dependency resolver
	graph, err := e.dependencyResolver.BuildDependencyGraph(resources, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build dependency graph for destruction: %w", err)
	}

	// Get execution order for creation
	executionOrder, err := e.dependencyResolver.GetExecutionOrder(graph)
	if err != nil {
		return nil, fmt.Errorf("failed to determine execution order for destruction: %w", err)
	}

	// Create a map for quick resource lookup
	resourceMap := make(map[string]*interfaces.Resource)
	for i := range resources {
		key := fmt.Sprintf("%s.%s", resources[i].Type, resources[i].Name)
		resourceMap[key] = &resources[i]
	}

	// Reverse the execution order for destruction
	// If A depends on B during creation, then B must be destroyed before A
	sortedResources := make([]interfaces.Resource, 0, len(resources))
	for i := len(executionOrder) - 1; i >= 0; i-- {
		if resource, ok := resourceMap[executionOrder[i]]; ok {
			sortedResources = append(sortedResources, *resource)
		}
	}

	return sortedResources, nil
}

// Execute processes a deployment request
func (e *DeploymentExecutor) Execute(ctx context.Context, deploy *interfaces.QueuedDeployment) error {
	e.logger.Infof("Starting execution of deployment %s", deploy.ID)

	// Create execution context with timeout
	execCtx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()

	// Validate deployment request
	if deploy.Request == nil {
		return fmt.Errorf("deployment request is nil")
	}

	// Handle special operations (update/destroy)
	if handled, err := e.handleSpecialOperations(execCtx, deploy); handled {
		return err
	}

	// Execute regular deployment
	return e.executeRegularDeployment(execCtx, deploy)
}

// handleSpecialOperations checks for update/destroy operations
func (e *DeploymentExecutor) handleSpecialOperations(ctx context.Context, deploy *interfaces.QueuedDeployment) (bool, error) {
	if deploy.Request.Metadata == nil {
		return false, nil
	}

	op, ok := deploy.Request.Metadata[interfaces.MetadataKeyOperation].(string)
	if !ok {
		return false, nil
	}

	switch op {
	case interfaces.OperationUpdate:
		return true, e.ExecuteUpdate(ctx, deploy)
	case interfaces.OperationDestroy:
		return true, e.ExecuteDestroy(ctx, deploy)
	default:
		return false, nil
	}
}

// executeRegularDeployment handles normal deployment operations
func (e *DeploymentExecutor) executeRegularDeployment(ctx context.Context, deploy *interfaces.QueuedDeployment) error {
	// Extract provider versions
	providerVersions := e.extractProviderVersions(deploy)

	// Extract provider configurations
	providerConfigs := e.extractProviderConfigs(deploy)

	// Create execution context
	execContext := e.createExecutionContext(deploy)
	execContext.providerConfigs = providerConfigs

	// Initialize result
	result := &interfaces.DeploymentResult{
		DeploymentID: deploy.ID,
		Success:      false,
		Resources:    make(map[string]interface{}),
		Outputs:      make(map[string]interface{}),
	}

	// Process data sources
	if err := e.processAllDataSources(ctx, deploy, result, providerVersions, execContext); err != nil {
		return err
	}

	// Sort resources by dependencies
	sortedResources, err := e.sortResourcesByDependencies(deploy.Request.Resources)
	if err != nil {
		result.Error = fmt.Errorf("failed to sort resources by dependencies: %w", err)
		e.storeResult(deploy.ID, result)
		return result.Error
	}

	// Process resources in dependency order
	for _, resource := range sortedResources {
		if err := e.processResource(ctx, resource, result, deploy.Request.Options, providerVersions, execContext); err != nil {
			result.Error = fmt.Errorf("resource %s.%s failed: %w", resource.Type, resource.Name, err)
			e.storeResult(deploy.ID, result)
			return result.Error
		}
	}

	// Mark as successful
	result.Success = true
	result.CompletedAt = time.Now()
	e.storeResult(deploy.ID, result)

	e.logger.Infof("Successfully completed deployment %s", deploy.ID)
	return nil
}

// extractProviderVersions extracts provider versions from terraform JSON
func (e *DeploymentExecutor) extractProviderVersions(deploy *interfaces.QueuedDeployment) map[string]string {
	if deploy.Request.Metadata == nil {
		return nil
	}

	terraformJSON, ok := deploy.Request.Metadata[interfaces.MetadataKeyTerraformJSON].(map[string]interface{})
	if !ok {
		return nil
	}

	return utils.ExtractProviderVersions(terraformJSON)
}

// extractProviderConfigs extracts provider configurations from terraform JSON
func (e *DeploymentExecutor) extractProviderConfigs(deploy *interfaces.QueuedDeployment) map[string]map[string]interface{} {
	if deploy.Request.Metadata == nil {
		return nil
	}

	terraformJSON, ok := deploy.Request.Metadata[interfaces.MetadataKeyTerraformJSON].(map[string]interface{})
	if !ok {
		return nil
	}

	// Extract provider configurations from terraform JSON
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

// createExecutionContext creates execution context with deployed resources
func (e *DeploymentExecutor) createExecutionContext(deploy *interfaces.QueuedDeployment) *executionContext {
	execContext := &executionContext{
		deploymentID:      deploy.ID,
		deployedResources: make(map[string]interface{}),
	}

	// Populate deployed resources from state store
	if e.stateStore != nil {
		ctx := context.Background()
		tfState, err := e.loadTerraformState(ctx, deploy.ID)
		if err == nil {
			// Extract resources from Terraform state
			resources := e.extractResourcesFromState(tfState)
			for resourceKey, state := range resources {
				execContext.deployedResources[resourceKey] = state
			}
		}
		// If error loading state, just continue with empty deployed resources map
		// This is expected for new deployments that don't have state yet
	}

	return execContext
}

// processAllDataSources processes all data sources for the deployment
func (e *DeploymentExecutor) processAllDataSources(ctx context.Context, deploy *interfaces.QueuedDeployment, result *interfaces.DeploymentResult, providerVersions map[string]string, execContext *executionContext) error {
	for _, dataSource := range deploy.Request.DataSources {
		if err := e.processDataSource(ctx, dataSource, result, providerVersions, execContext); err != nil {
			result.Error = fmt.Errorf("data source %s.%s failed: %w", dataSource.Type, dataSource.Name, err)
			e.storeResult(deploy.ID, result)
			return result.Error
		}
	}
	return nil
}

// ExecuteUpdate processes an update deployment request
func (e *DeploymentExecutor) ExecuteUpdate(ctx context.Context, dep *interfaces.QueuedDeployment) error {
	e.logger.Infof("Starting execution of update deployment %s", dep.ID)

	// Extract and validate update plan
	plan, err := deployment.ExtractUpdatePlan(dep)
	if err != nil {
		return fmt.Errorf("failed to extract update plan: %w", err)
	}
	e.logUpdatePlan(plan)

	// Initialize result
	result := e.initializeUpdateResult(dep.ID)

	// Determine execution order: use pre-calculated order if available, otherwise analyze dependencies
	var changeOrder []interfaces.ResourceChange
	if len(plan.ExecutionOrder) > 0 {
		// Use the pre-calculated dependency-aware execution order
		changeOrder = plan.ExecutionOrder
		e.logger.Debugf("Using pre-calculated execution order with %d changes", len(changeOrder))
	} else {
		// Fall back to analyzing dependencies (for backward compatibility)
		changeOrder, err = e.analyzeUpdateDependencies(plan.Changes)
		if err != nil {
			result.Error = fmt.Errorf("failed to analyze update dependencies: %w", err)
			e.storeResult(dep.ID, result)
			return result.Error
		}
		e.logger.Debugf("Calculated execution order with %d changes", len(changeOrder))
	}

	// Process changes
	if err := e.processUpdateChanges(ctx, changeOrder, result, dep, plan); err != nil {
		return err
	}

	// Mark as successful
	result.Success = true
	result.CompletedAt = time.Now()
	e.storeResult(dep.ID, result)

	// Update original deployment if applicable
	e.updateOriginalDeployment(dep, result)

	e.logger.Infof("Successfully completed update deployment %s", dep.ID)
	return nil
}

// logUpdatePlan logs details about the update plan
func (e *DeploymentExecutor) logUpdatePlan(plan *interfaces.UpdatePlan) {
	e.logger.Debugf("Update plan has %d changes", len(plan.Changes))
	for i, change := range plan.Changes {
		e.logger.Debugf("Change %d: %s action=%s", i, change.ResourceKey, change.Action)
	}
}

// initializeUpdateResult creates a new deployment result for updates
func (e *DeploymentExecutor) initializeUpdateResult(deploymentID string) *interfaces.DeploymentResult {
	return &interfaces.DeploymentResult{
		DeploymentID: deploymentID,
		Success:      false,
		Resources:    make(map[string]interface{}),
		Outputs:      make(map[string]interface{}),
	}
}

// processUpdateChanges processes all changes in the update plan
func (e *DeploymentExecutor) processUpdateChanges(ctx context.Context, changeOrder []interfaces.ResourceChange, result *interfaces.DeploymentResult, dep *interfaces.QueuedDeployment, plan *interfaces.UpdatePlan) error {
	for _, change := range changeOrder {
		if err := e.processSingleChange(ctx, change, result, dep, plan); err != nil {
			return err
		}
	}
	return nil
}

// processSingleChange processes a single resource change
func (e *DeploymentExecutor) processSingleChange(ctx context.Context, change interfaces.ResourceChange, result *interfaces.DeploymentResult, dep *interfaces.QueuedDeployment, plan *interfaces.UpdatePlan) error {
	switch change.Action {
	case interfaces.ActionDelete:
		if err := e.processDelete(ctx, change, result, plan); err != nil {
			result.Error = fmt.Errorf("failed to delete %s: %w", change.ResourceKey, err)
			e.storeResult(dep.ID, result)
			return result.Error
		}
	case interfaces.ActionUpdate, interfaces.ActionReplace:
		if err := e.processUpdate(ctx, change, result, dep.Request.Options, plan); err != nil {
			result.Error = fmt.Errorf("failed to update %s: %w", change.ResourceKey, err)
			e.storeResult(dep.ID, result)
			return result.Error
		}
	case interfaces.ActionCreate:
		if err := e.processCreate(ctx, change, result, dep.Request.Options, plan); err != nil {
			result.Error = fmt.Errorf("failed to create %s: %w", change.ResourceKey, err)
			e.storeResult(dep.ID, result)
			return result.Error
		}
	case interfaces.ActionNoOp:
		// Skip no-op changes
		e.logger.Debugf("Skipping no-op change for %s", change.ResourceKey)
	}
	return nil
}

// updateOriginalDeployment updates the original deployment with update results
func (e *DeploymentExecutor) updateOriginalDeployment(dep *interfaces.QueuedDeployment, result *interfaces.DeploymentResult) {
	originalID, ok := dep.Request.Metadata[interfaces.MetadataKeyDeploymentID].(string)
	if !ok || originalID == "" {
		e.logger.Debugf("No original deployment ID found in metadata for update deployment %s", dep.ID)
		return
	}

	e.logger.Debugf("Updating original deployment %s with result from update deployment %s", originalID, dep.ID)

	// Update the original deployment's result
	originalResult := &interfaces.DeploymentResult{
		DeploymentID: originalID,
		Success:      true,
		Resources:    result.Resources,
		Outputs:      result.Outputs,
		CompletedAt:  time.Now(),
	}

	e.logger.Debugf("Setting result for original deployment %s with %d resources", originalID, len(result.Resources))
	e.logS3BucketResultTags(result.Resources)

	// Publish result event for original deployment
	e.eventBus.PublishResult(originalID, originalResult)
	e.logger.Debugf("Published result event for original deployment %s", originalID)
}

// logS3BucketResultTags logs S3 bucket tags from result for debugging
func (e *DeploymentExecutor) logS3BucketResultTags(resources map[string]interface{}) {
	for key, res := range resources {
		if strings.Contains(key, awsS3BucketType) {
			if resMap, ok := res.(map[string]interface{}); ok {
				if tags, ok := resMap["tags"]; ok {
					e.logger.Debugf("S3 bucket %s in result has tags: %+v", key, utils.RedactStateForLogging(tags))
				} else {
					e.logger.Debugf("S3 bucket %s in result has NO tags", key)
				}
			}
		}
	}
}

// processCreate handles resource creation with proper error handling and state tracking.
// Creates resources through the provider and updates deployment state, ensuring
// partial failures don't leave the system in an inconsistent state.
//
//nolint:funlen // Resource creation involves multiple validation and state management steps
func (e *DeploymentExecutor) processCreate(
	ctx context.Context,
	change interfaces.ResourceChange,
	result *interfaces.DeploymentResult,
	options interfaces.DeploymentOptions,
	plan *interfaces.UpdatePlan,
) error {
	e.logger.Infof("Creating resource %s", change.ResourceKey)

	// Skip in dry-run mode
	if options.DryRun {
		e.logger.Infof("Dry-run mode: skipping create of %s", change.ResourceKey)
		result.Resources[string(change.ResourceKey)] = map[string]interface{}{
			"status": "dry-run-create",
		}
		return nil
	}

	// Extract resource type and provider name from resource key
	resourceType, _ := interfaces.ParseResourceKey(change.ResourceKey)
	providerName := utils.ExtractProviderName(resourceType)

	// Get provider config from plan
	var providerConfig interfaces.ProviderConfig
	if plan.ProviderConfigs != nil {
		if cfg, ok := plan.ProviderConfigs[providerName]; ok {
			providerConfig = interfaces.ProviderConfig{providerName: cfg}
		}
	}

	// Get provider version from plan
	providerVersion := ""
	if plan.ProviderVersions != nil {
		providerVersion = plan.ProviderVersions[providerName]
	}

	// Get provider instance
	provider, err := e.providerManager.GetProvider(
		ctx,
		plan.DeploymentID,
		providerName,
		providerVersion,
		providerConfig,
		resourceType,
	)
	if err != nil {
		return fmt.Errorf("failed to get provider %s: %w", providerName, err)
	}
	defer func() {
		if releaseErr := e.providerManager.ReleaseProvider(plan.DeploymentID, providerName); releaseErr != nil {
			e.logger.Warnf("Failed to release provider %s: %v", providerName, releaseErr)
		}
	}()

	// Create resource plan for apply
	resourcePlan := &interfaces.ResourcePlan{
		ResourceType:  resourceType,
		Action:        interfaces.PlanActionCreate,
		ProposedState: change.After,
		PlannedState:  change.After,
	}

	// Apply the resource
	state, err := provider.ApplyResourceChange(ctx, resourceType, resourcePlan)
	if err != nil {
		return fmt.Errorf("failed to apply resource: %w", err)
	}

	// Store result
	result.Resources[string(change.ResourceKey)] = state

	return nil
}

// processUpdate updates an existing resource based on the change
func (e *DeploymentExecutor) processUpdate(
	ctx context.Context,
	change interfaces.ResourceChange,
	result *interfaces.DeploymentResult,
	options interfaces.DeploymentOptions,
	plan *interfaces.UpdatePlan,
) error {
	action := "Updating"
	if change.Action == interfaces.ActionReplace {
		action = "Replacing"
	}
	e.logger.Infof("%s resource %s", action, change.ResourceKey)

	// Skip in dry-run mode
	if options.DryRun {
		e.logger.Infof("Dry-run mode: skipping %s of %s", strings.ToLower(action), change.ResourceKey)
		result.Resources[string(change.ResourceKey)] = map[string]interface{}{
			"status": fmt.Sprintf("dry-run-%s", strings.ToLower(action)),
		}
		return nil
	}

	// Extract resource info
	resourceType, _ := interfaces.ParseResourceKey(change.ResourceKey)
	providerName := utils.ExtractProviderName(resourceType)

	// Get provider instance
	provider, err := e.getProviderForUpdate(ctx, providerName, resourceType, plan)
	if err != nil {
		return err
	}
	defer func() {
		if releaseErr := e.providerManager.ReleaseProvider(plan.DeploymentID, providerName); releaseErr != nil {
			e.logger.Warnf("Failed to release provider %s: %v", providerName, releaseErr)
		}
	}()

	// Apply the update
	state, err := e.applyUpdateChange(ctx, provider, change, resourceType, plan)
	if err != nil {
		return err
	}

	// Store result
	result.Resources[string(change.ResourceKey)] = state

	// Update state store
	e.updateStateStoreAfterUpdate(change.ResourceKey, state, plan)

	return nil
}

// getProviderForUpdate gets and configures provider for update operation
func (e *DeploymentExecutor) getProviderForUpdate(ctx context.Context, providerName, resourceType string, plan *interfaces.UpdatePlan) (interfaces.UnifiedProvider, error) {
	// Get provider config from plan
	var providerConfig interfaces.ProviderConfig
	if plan.ProviderConfigs != nil {
		if cfg, ok := plan.ProviderConfigs[providerName]; ok {
			providerConfig = interfaces.ProviderConfig{providerName: cfg}
		}
	}

	// Get provider version from plan
	providerVersion := ""
	if plan.ProviderVersions != nil {
		providerVersion = plan.ProviderVersions[providerName]
	}

	// Get provider instance (GetProvider already configures the provider internally)
	provider, err := e.providerManager.GetProvider(
		ctx,
		plan.DeploymentID,
		providerName,
		providerVersion,
		providerConfig,
		resourceType,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get provider %s: %w", providerName, err)
	}

	return provider, nil
}

// applyUpdateChange applies the resource change for update
func (e *DeploymentExecutor) applyUpdateChange(ctx context.Context, provider interfaces.UnifiedProvider, change interfaces.ResourceChange, resourceType string, _ *interfaces.UpdatePlan) (map[string]interface{}, error) {
	// For updates, use ProposedState (user config) not After (provider planned state)
	proposedState := change.After
	if change.ProposedState != nil {
		proposedState = change.ProposedState
	}

	resourcePlan := &interfaces.ResourcePlan{
		ResourceType:  resourceType,
		Action:        convertChangeActionToPlanAction(change.Action),
		CurrentState:  change.Before,
		ProposedState: proposedState,
		PlannedState:  proposedState,
	}

	// Debug logging for S3 bucket updates
	if resourceType == awsS3BucketType {
		e.logS3BucketUpdate(change, proposedState)
	}

	// Apply the resource change
	e.logger.Debugf("Applying resource change for %s: action=%s", resourceType, resourcePlan.Action)
	state, err := provider.ApplyResourceChange(ctx, resourceType, resourcePlan)
	if err != nil {
		e.logger.Errorf("ApplyResourceChange failed for %s: %v", resourceType, err)
		return nil, fmt.Errorf("failed to apply resource change: %w", err)
	}
	e.logger.Debugf("ApplyResourceChange succeeded for %s, state keys: %v", resourceType, getMapKeys(state))

	// Debug logging for S3 bucket state after update
	if resourceType == awsS3BucketType {
		e.logS3BucketStateAfterUpdate(state)
	}

	return state, nil
}

// updateStateStoreAfterUpdate updates the state store after a successful update
func (e *DeploymentExecutor) updateStateStoreAfterUpdate(resourceKey interfaces.ResourceKey, state map[string]interface{}, plan *interfaces.UpdatePlan) {
	if e.stateStore != nil && plan.DeploymentID != "" {
		ctx := context.Background()
		resourceType, resourceName := interfaces.ParseResourceKey(resourceKey)

		// Load current Terraform state
		tfState, err := e.loadTerraformState(ctx, plan.DeploymentID)
		if err != nil {
			// If state doesn't exist, create new empty state
			tfState = e.createEmptyTerraformState()
		}

		// Update the resource in state
		e.updateResourceInState(tfState, resourceType, resourceName, state)

		// Save updated state
		if err := e.saveTerraformState(ctx, plan.DeploymentID, tfState); err != nil {
			e.logger.Warnf("Failed to save Terraform state for %s.%s: %v", resourceType, resourceName, err)
		}
	}
}

// logS3BucketUpdate logs debug info for S3 bucket updates
func (e *DeploymentExecutor) logS3BucketUpdate(change interfaces.ResourceChange, proposedState map[string]interface{}) {
	e.logger.Debugf("S3 bucket update - Before state: %+v", utils.RedactStateForLogging(change.Before))
	e.logger.Debugf("S3 bucket update - After state: %+v", utils.RedactStateForLogging(change.After))
	e.logger.Debugf("S3 bucket update - ProposedState: %+v", utils.RedactStateForLogging(change.ProposedState))
	e.logger.Debugf("S3 bucket update - Using proposedState: %+v", utils.RedactStateForLogging(proposedState))
	if tags, ok := proposedState["tags"]; ok {
		e.logger.Debugf("S3 bucket update - Tags in proposedState: %+v", utils.RedactStateForLogging(tags))
	} else {
		e.logger.Debugf("S3 bucket update - No tags found in proposedState")
	}
}

// logS3BucketStateAfterUpdate logs debug info for S3 bucket state after update
func (e *DeploymentExecutor) logS3BucketStateAfterUpdate(state map[string]interface{}) {
	if tags, ok := state["tags"]; ok {
		e.logger.Debugf("S3 bucket state after update - Tags: %+v", utils.RedactStateForLogging(tags))
	} else {
		e.logger.Debugf("S3 bucket state after update - No tags in returned state")
	}
}

// processDelete deletes an existing resource based on the change
func (e *DeploymentExecutor) processDelete(
	ctx context.Context,
	change interfaces.ResourceChange,
	result *interfaces.DeploymentResult,
	plan *interfaces.UpdatePlan,
) error {
	e.logger.Infof("Deleting resource %s", change.ResourceKey)

	// Extract resource type and provider name from resource key
	resourceType, _ := interfaces.ParseResourceKey(change.ResourceKey)
	providerName := utils.ExtractProviderName(resourceType)

	// Get provider config from plan
	var providerConfig interfaces.ProviderConfig
	if plan.ProviderConfigs != nil {
		if cfg, ok := plan.ProviderConfigs[providerName]; ok {
			providerConfig = interfaces.ProviderConfig{providerName: cfg}
		}
	}

	// Get provider version from plan
	providerVersion := ""
	if plan.ProviderVersions != nil {
		providerVersion = plan.ProviderVersions[providerName]
	}

	// Get provider instance
	provider, err := e.providerManager.GetProvider(
		ctx,
		plan.DeploymentID,
		providerName,
		providerVersion,
		providerConfig,
		resourceType,
	)
	if err != nil {
		return fmt.Errorf("failed to get provider %s: %w", providerName, err)
	}
	defer func() {
		if releaseErr := e.providerManager.ReleaseProvider(plan.DeploymentID, providerName); releaseErr != nil {
			e.logger.Warnf("Failed to release provider %s: %v", providerName, releaseErr)
		}
	}()

	// Create resource plan for deletion
	resourcePlan := &interfaces.ResourcePlan{
		ResourceType:  resourceType,
		Action:        interfaces.PlanActionDelete,
		CurrentState:  change.Before,
		ProposedState: nil, // nil indicates deletion
		PlannedState:  nil,
	}

	// Apply the deletion
	_, err = provider.ApplyResourceChange(ctx, resourceType, resourcePlan)
	if err != nil {
		return fmt.Errorf("failed to delete resource: %w", err)
	}

	// Mark as deleted in result
	result.Resources[string(change.ResourceKey)] = map[string]interface{}{
		"status": "deleted",
	}

	return nil
}

// processDataSource reads a single data source
func (e *DeploymentExecutor) processDataSource(
	ctx context.Context,
	dataSource interfaces.DataSource,
	result *interfaces.DeploymentResult,
	providerVersions map[string]string,
	execContext *executionContext,
) error {
	e.logger.Infof("Reading data source %s.%s", dataSource.Type, dataSource.Name)

	// Extract provider name from type (e.g., "aws_instance" -> "aws")
	providerName := utils.ExtractProviderName(dataSource.Type)

	// Get provider version
	providerVersion := ""
	if providerVersions != nil {
		providerVersion = providerVersions[providerName]
	}

	// Get provider configuration from execution context
	var providerConfig interfaces.ProviderConfig
	if execContext.providerConfigs != nil {
		if cfg, ok := execContext.providerConfigs[providerName]; ok {
			providerConfig = interfaces.ProviderConfig{providerName: cfg}
		}
	}

	// Get provider instance (GetProvider already configures the provider internally)
	provider, err := e.providerManager.GetProvider(
		ctx,
		execContext.deploymentID,
		providerName,
		providerVersion,
		providerConfig,
		dataSource.Type,
	)
	if err != nil {
		return fmt.Errorf("failed to get provider %s: %w", providerName, err)
	}
	defer func() {
		if releaseErr := e.providerManager.ReleaseProvider(execContext.deploymentID, providerName); releaseErr != nil {
			e.logger.Warnf("Failed to release provider %s: %v", providerName, releaseErr)
		}
	}()

	// Interpolate data source properties
	interpolatedProps, err := e.interpolateProperties(dataSource.Properties, execContext.deployedResources)
	if err != nil {
		return fmt.Errorf("failed to interpolate data source properties: %w", err)
	}

	// Read data source
	data, err := provider.ReadDataSource(ctx, dataSource.Type, interpolatedProps)
	if err != nil {
		return fmt.Errorf("failed to read data source: %w", err)
	}

	// Store result
	key := fmt.Sprintf("data.%s.%s", dataSource.Type, dataSource.Name)
	result.Outputs[key] = data

	// Store in execution context for interpolation
	execContext.deployedResources[key] = data

	return nil
}

// processResource creates or updates a single resource
//
//nolint:funlen,gocognit,gocyclo // Complex resource processing logic with multiple deployment paths
func (e *DeploymentExecutor) processResource(
	ctx context.Context,
	resource interfaces.Resource,
	result *interfaces.DeploymentResult,
	options interfaces.DeploymentOptions,
	providerVersions map[string]string,
	execContext *executionContext,
) error {
	e.logger.Infof("Processing resource %s.%s", resource.Type, resource.Name)

	// Skip in dry-run mode
	if options.DryRun {
		e.logger.Infof("Dry-run mode: skipping resource %s.%s", resource.Type, resource.Name)
		resourceKey := fmt.Sprintf("%s.%s", resource.Type, resource.Name)
		result.Resources[resourceKey] = map[string]interface{}{
			"status": "dry-run",
			"type":   resource.Type,
		}
		return nil
	}

	// Extract provider name from type
	providerName := utils.ExtractProviderName(resource.Type)

	// Get provider version
	providerVersion := ""
	if providerVersions != nil {
		providerVersion = providerVersions[providerName]
	}

	// Get provider configuration from execution context
	var providerConfig interfaces.ProviderConfig
	if execContext.providerConfigs != nil {
		if cfg, ok := execContext.providerConfigs[providerName]; ok {
			providerConfig = interfaces.ProviderConfig{providerName: cfg}
		}
	}

	// Get provider instance (GetProvider already configures the provider internally)
	provider, err := e.providerManager.GetProvider(
		ctx,
		execContext.deploymentID,
		providerName,
		providerVersion,
		providerConfig,
		resource.Type,
	)
	if err != nil {
		return fmt.Errorf("failed to get provider %s: %w", providerName, err)
	}
	defer func() {
		if releaseErr := e.providerManager.ReleaseProvider(execContext.deploymentID, providerName); releaseErr != nil {
			e.logger.Warnf("Failed to release provider %s: %v", providerName, releaseErr)
		}
	}()

	// Interpolate resource properties
	interpolatedProps, err := e.interpolateProperties(resource.Properties, execContext.deployedResources)
	if err != nil {
		return fmt.Errorf("failed to interpolate resource properties: %w", err)
	}

	// Load current Terraform state to check if resource exists
	var existing map[string]interface{}
	var resourceExists bool
	var tfState *interfaces.TerraformState

	if e.stateStore != nil {
		var err error
		tfState, err = e.loadTerraformState(ctx, execContext.deploymentID)
		if err == nil {
			// Check if resource exists in state
			tfResource, _ := e.findResourceInState(tfState, resource.Type, resource.Name)
			if tfResource != nil && len(tfResource.Instances) > 0 {
				existing = tfResource.Instances[0].Attributes
				resourceExists = true
			}
		} else {
			// No state exists yet, create new empty state
			tfState = e.createEmptyTerraformState()
		}
	}

	var resourceAttributes map[string]interface{}

	if !resourceExists {
		// Resource doesn't exist, create it
		e.logger.Infof("Creating resource %s.%s", resource.Type, resource.Name)
		created, err := provider.CreateResource(ctx, resource.Type, interpolatedProps)
		if err != nil {
			return fmt.Errorf("failed to create resource: %w", err)
		}
		resourceAttributes = created
		resourceKey := string(interfaces.MakeResourceKey(resource.Type, resource.Name))
		result.Resources[resourceKey] = created
		// Store in execution context for interpolation
		execContext.deployedResources[resourceKey] = created
	} else {
		// Resource exists, update it
		e.logger.Infof("Updating resource %s.%s", resource.Type, resource.Name)
		// Debug log for random_string
		if resource.Type == "random_string" {
			e.logger.Debugf("Existing random_string state: %+v", utils.RedactStateForLogging(existing))
		}
		updated, err := provider.UpdateResource(ctx, resource.Type, existing, interpolatedProps)
		if err != nil {
			return fmt.Errorf("failed to update resource: %w", err)
		}
		// Debug log for random_string
		if resource.Type == "random_string" {
			e.logger.Debugf("Updated random_string state: %+v", utils.RedactStateForLogging(updated))
		}
		resourceAttributes = updated
		resourceKey := string(interfaces.MakeResourceKey(resource.Type, resource.Name))
		result.Resources[resourceKey] = updated
		// Store in execution context for interpolation
		execContext.deployedResources[resourceKey] = updated
	}

	// Update Terraform state and save it
	if e.stateStore != nil && tfState != nil {
		e.updateResourceInState(tfState, resource.Type, resource.Name, resourceAttributes)
		if err := e.saveTerraformState(ctx, execContext.deploymentID, tfState); err != nil {
			e.logger.Warnf("Failed to save Terraform state for %s.%s: %v", resource.Type, resource.Name, err)
		}
	}

	return nil
}

// storeResult saves the deployment result
func (e *DeploymentExecutor) storeResult(deploymentID string, result *interfaces.DeploymentResult) {
	// Log what we're storing
	e.logger.Debugf("Storing result for deployment %s: Success=%v, Resources=%d items",
		deploymentID, result.Success, len(result.Resources))

	// Store the full result
	e.logger.Debugf("About to store result - Resources map contents:")
	for k, v := range result.Resources {
		e.logger.Debugf("  %s: %+v", k, utils.RedactStateForLogging(v))
	}
	// Publish result event
	e.eventBus.PublishResult(deploymentID, result)

	// Also update the status
	if result.Success {
		e.eventBus.PublishStatusChange(deploymentID, interfaces.DeploymentStatusCompleted)
	} else {
		e.eventBus.PublishStatusChange(deploymentID, interfaces.DeploymentStatusFailed)
	}
}

// convertChangeActionToPlanAction converts our change action to provider plan action
func convertChangeActionToPlanAction(action interfaces.ChangeAction) interfaces.PlanAction {
	switch action {
	case interfaces.ActionCreate:
		return interfaces.PlanActionCreate
	case interfaces.ActionUpdate:
		return interfaces.PlanActionUpdate
	case interfaces.ActionDelete:
		return interfaces.PlanActionDelete
	case interfaces.ActionReplace:
		return interfaces.PlanActionReplace
	case interfaces.ActionNoOp:
		return interfaces.PlanActionNoop
	default:
		return interfaces.PlanActionNoop
	}
}

// interpolateProperties resolves variable references in properties
func (e *DeploymentExecutor) interpolateProperties(properties map[string]interface{}, deployedResources map[string]interface{}) (map[string]interface{}, error) { //nolint:gocognit // Complex property interpolation
	// Convert deployed resources to the format expected by interpolation resolver
	// Format should be: resourceType.resourceName -> resource attributes
	deployedResourcesFormatted := make(map[string]map[string]interface{})

	for key, value := range deployedResources {
		// Check if this is a data source key (starts with "data.")
		if strings.HasPrefix(key, "data.") {
			// Parse data source key: "data.type.name"
			parts := strings.SplitN(key, ".", 3)
			if len(parts) != 3 {
				continue // Skip malformed keys
			}

			dataSourceType := parts[1]
			dataSourceName := parts[2]

			// Ensure the data namespace exists
			if _, exists := deployedResourcesFormatted["data"]; !exists {
				deployedResourcesFormatted["data"] = make(map[string]interface{})
			}

			// Ensure the data source type namespace exists within the data namespace
			if _, exists := deployedResourcesFormatted["data"][dataSourceType]; !exists {
				deployedResourcesFormatted["data"][dataSourceType] = make(map[string]interface{})
			}

			// Store the data source data under data.type.name
			if dataSourceData, ok := value.(map[string]interface{}); ok {
				deployedResourcesFormatted["data"][dataSourceType].(map[string]interface{})[dataSourceName] = dataSourceData
			} else {
				// Convert to map format for simple values
				deployedResourcesFormatted["data"][dataSourceType].(map[string]interface{})[dataSourceName] = map[string]interface{}{
					"id": value, // Default to id for simple values
				}
			}
		} else {
			// This is a regular resource key: "type.name"
			resourceType, resourceName := interfaces.ParseResourceKey(interfaces.ResourceKey(key))
			if resourceType == "" || resourceName == "" {
				continue // Skip malformed keys
			}

			// Ensure the resource type namespace exists
			if _, exists := deployedResourcesFormatted[resourceType]; !exists {
				deployedResourcesFormatted[resourceType] = make(map[string]interface{})
			}

			// Store the resource data under resourceType.resourceName
			if resourceData, ok := value.(map[string]interface{}); ok {
				deployedResourcesFormatted[resourceType][resourceName] = resourceData
			} else {
				// Convert to map format for simple values
				deployedResourcesFormatted[resourceType][resourceName] = map[string]interface{}{
					"id": value, // Default to id for simple values
				}
			}
		}
	}

	// Use the interpolation resolver to resolve references
	result, err := e.interpolator.ResolveInterpolations(properties, deployedResourcesFormatted)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve interpolations: %w", err)
	}
	e.logger.Debugf("Interpolated properties: original=%+v, resolved=%+v",
		utils.RedactStateForLogging(properties),
		utils.RedactStateForLogging(result))
	return result, nil
}

// ExecuteDestroy processes a deployment destruction request
func (e *DeploymentExecutor) ExecuteDestroy(ctx context.Context, deploy *interfaces.QueuedDeployment) error {
	e.logger.Infof("Starting destruction of deployment %s", deploy.ID)

	// Get the original deployment ID
	originalDeploymentID, ok := deploy.Request.Metadata[interfaces.MetadataKeyOriginalDeploymentID].(string)
	if !ok {
		return fmt.Errorf("original_deployment_id not found in destroy deployment metadata")
	}

	// Extract provider versions from terraform JSON in metadata
	var providerVersions map[string]string
	if deploy.Request.Metadata != nil {
		if terraformJSON, ok := deploy.Request.Metadata[interfaces.MetadataKeyTerraformJSON].(map[string]interface{}); ok {
			providerVersions = utils.ExtractProviderVersions(terraformJSON)
		}
	}

	// Store results
	result := &interfaces.DeploymentResult{
		DeploymentID: deploy.ID,
		Success:      false,
		Resources:    make(map[string]interface{}),
		Outputs:      make(map[string]interface{}),
	}

	// Sort resources for destruction (reverse dependency order)
	resources, err := e.sortResourcesForDestruction(deploy.Request.Resources)
	if err != nil {
		result.Error = fmt.Errorf("failed to sort resources for destruction: %w", err)
		e.storeResult(deploy.ID, result)
		return result.Error
	}

	// Process resources in computed destruction order
	for _, resource := range resources {
		if err := e.destroyResource(ctx, resource, result, providerVersions, originalDeploymentID); err != nil {
			// Log the error but continue with other resources
			e.logger.Errorf("Failed to destroy resource %s.%s: %v", resource.Type, resource.Name, err)
			result.Error = fmt.Errorf("failed to destroy resource %s.%s: %w", resource.Type, resource.Name, err)

			// Mark as failed but continue with other resources
			resourceKey := string(interfaces.MakeResourceKey(resource.Type, resource.Name))
			result.Resources[resourceKey] = map[string]interface{}{
				"status": "failed",
				"error":  err.Error(),
			}
		}
	}

	// Mark as successful
	result.Success = true
	result.CompletedAt = time.Now()
	e.storeResult(deploy.ID, result)

	// Update the original deployment status to destroyed
	// Publish status change event for original deployment
	e.eventBus.PublishStatusChange(originalDeploymentID, interfaces.DeploymentStatusDestroyed)

	e.logger.Infof("Successfully completed destruction of deployment %s", deploy.ID)
	return nil
}

// destroyResource destroys a single resource
//
//nolint:funlen // Resource destruction involves multiple validation and cleanup steps
func (e *DeploymentExecutor) destroyResource(
	ctx context.Context,
	resource interfaces.Resource,
	result *interfaces.DeploymentResult,
	providerVersions map[string]string,
	originalDeploymentID string,
) error {
	e.logger.Infof("Destroying resource %s.%s", resource.Type, resource.Name)

	// Extract provider name from type
	providerName := utils.ExtractProviderName(resource.Type)

	// Get provider version
	providerVersion := ""
	if providerVersions != nil {
		providerVersion = providerVersions[providerName]
	}

	// Get provider instance (GetProvider already configures the provider internally)
	// For destroy operation, we don't have provider configs, just use nil
	provider, err := e.providerManager.GetProvider(
		ctx,
		originalDeploymentID,
		providerName,
		providerVersion,
		nil, // Destroy operations don't have provider config
		resource.Type,
	)
	if err != nil {
		return fmt.Errorf("failed to get provider %s: %w", providerName, err)
	}
	defer func() {
		if releaseErr := e.providerManager.ReleaseProvider(originalDeploymentID, providerName); releaseErr != nil {
			e.logger.Warnf("Failed to release provider %s: %v", providerName, releaseErr)
		}
	}()

	// Load current Terraform state to get resource state
	var currentState map[string]interface{}
	var tfState *interfaces.TerraformState
	var resourceExists bool

	if e.stateStore != nil {
		var err error
		tfState, err = e.loadTerraformState(ctx, originalDeploymentID)
		if err == nil {
			// Check if resource exists in state
			tfResource, _ := e.findResourceInState(tfState, resource.Type, resource.Name)
			if tfResource != nil && len(tfResource.Instances) > 0 {
				currentState = tfResource.Instances[0].Attributes
				resourceExists = true
			}
		}
		// If error loading state or resource not found, nothing to destroy
	}

	// If we have state, delete the resource
	if resourceExists && currentState != nil {
		// Call provider to delete resource
		e.logger.Infof("Calling provider to delete resource %s.%s", resource.Type, resource.Name)
		if err := provider.DeleteResource(ctx, resource.Type, currentState); err != nil {
			return fmt.Errorf("failed to delete resource: %w", err)
		}

		// Remove from Terraform state
		if tfState != nil {
			e.removeResourceFromState(tfState, resource.Type, resource.Name)
			if err := e.saveTerraformState(ctx, originalDeploymentID, tfState); err != nil {
				e.logger.Warnf("Failed to save Terraform state after deleting %s.%s: %v", resource.Type, resource.Name, err)
			}
		}
	}

	// Mark as destroyed in result
	resourceKey := string(interfaces.MakeResourceKey(resource.Type, resource.Name))
	result.Resources[resourceKey] = map[string]interface{}{
		"status": "destroyed",
	}

	return nil
}

// getMapKeys returns the keys of a map for debugging
func getMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// Terraform state management functions

// loadTerraformState loads the complete Terraform state for a deployment
func (e *DeploymentExecutor) loadTerraformState(ctx context.Context, deploymentID string) (*interfaces.TerraformState, error) {
	stateData, err := e.stateStore.LoadTerraformState(ctx, deploymentID)
	if err != nil {
		return nil, fmt.Errorf("failed to load terraform state: %w", err)
	}

	var tfState interfaces.TerraformState
	if err := json.Unmarshal(stateData, &tfState); err != nil {
		return nil, fmt.Errorf("failed to parse terraform state: %w", err)
	}

	return &tfState, nil
}

// saveTerraformState saves the complete Terraform state for a deployment
func (e *DeploymentExecutor) saveTerraformState(ctx context.Context, deploymentID string, tfState *interfaces.TerraformState) error {
	stateData, err := json.MarshalIndent(tfState, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal terraform state: %w", err)
	}

	if err := e.stateStore.SaveTerraformState(ctx, deploymentID, stateData); err != nil {
		return fmt.Errorf("failed to save terraform state: %w", err)
	}
	return nil
}

// createEmptyTerraformState creates a new empty Terraform state
func (e *DeploymentExecutor) createEmptyTerraformState() *interfaces.TerraformState {
	return &interfaces.TerraformState{
		Version:          4,
		TerraformVersion: "1.5.0",
		Serial:           1,
		Lineage:          e.generateStateLineage(),
		Outputs:          make(map[string]interface{}),
		Resources:        []interfaces.TerraformResource{},
	}
}

// generateStateLineage creates a unique lineage ID for Terraform state
func (e *DeploymentExecutor) generateStateLineage() string {
	now := time.Now()
	return fmt.Sprintf("%d-%d", now.UnixNano(), now.Unix())
}

// extractResourcesFromState extracts all resources from Terraform state as a map
func (e *DeploymentExecutor) extractResourcesFromState(tfState *interfaces.TerraformState) map[string]map[string]interface{} {
	result := make(map[string]map[string]interface{})

	for _, resource := range tfState.Resources {
		for i, instance := range resource.Instances {
			// Create resource key in the format the executor expects
			var resourceKey string
			if len(resource.Instances) == 1 {
				resourceKey = fmt.Sprintf("%s.%s", resource.Type, resource.Name)
			} else {
				resourceKey = fmt.Sprintf("%s.%s[%d]", resource.Type, resource.Name, i)
			}

			result[resourceKey] = instance.Attributes
		}
	}

	return result
}

// findResourceInState finds a specific resource in Terraform state
func (e *DeploymentExecutor) findResourceInState(tfState *interfaces.TerraformState, resourceType, resourceName string) (resource *interfaces.TerraformResource, index int) {
	for i := range tfState.Resources {
		if tfState.Resources[i].Type == resourceType && tfState.Resources[i].Name == resourceName {
			return &tfState.Resources[i], i
		}
	}
	return nil, -1
}

// updateResourceInState updates or adds a resource in Terraform state
func (e *DeploymentExecutor) updateResourceInState(tfState *interfaces.TerraformState, resourceType, resourceName string, attributes map[string]interface{}) {
	resource, _ := e.findResourceInState(tfState, resourceType, resourceName)

	if resource == nil {
		// Create new resource
		newResource := interfaces.TerraformResource{
			Mode:     "managed",
			Type:     resourceType,
			Name:     resourceName,
			Provider: fmt.Sprintf("provider[%q]", utils.ExtractProviderName(resourceType)),
			Instances: []interfaces.TerraformResourceInstance{
				{
					SchemaVersion: 1,
					Attributes:    attributes,
				},
			},
		}
		tfState.Resources = append(tfState.Resources, newResource)
	} else {
		// Update existing resource
		instance := interfaces.TerraformResourceInstance{
			SchemaVersion: 1,
			Attributes:    attributes,
		}

		if len(resource.Instances) == 0 {
			resource.Instances = []interfaces.TerraformResourceInstance{instance}
		} else {
			resource.Instances[0] = instance
		}
	}

	// Increment serial number
	tfState.Serial++
}

// removeResourceFromState removes a resource from Terraform state
func (e *DeploymentExecutor) removeResourceFromState(tfState *interfaces.TerraformState, resourceType, resourceName string) bool {
	_, index := e.findResourceInState(tfState, resourceType, resourceName)
	if index == -1 {
		return false // Resource not found
	}

	// Remove the resource from state
	tfState.Resources = append(tfState.Resources[:index], tfState.Resources[index+1:]...)
	tfState.Serial++
	return true
}

// CreateExecutorFunc creates an executor function for use with the background system
func CreateExecutorFunc(executor *DeploymentExecutor) func(context.Context, *interfaces.QueuedDeployment) error {
	return executor.Execute
}
