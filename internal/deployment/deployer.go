package deployment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"github.com/lattiam/lattiam/internal/provider"
	"github.com/lattiam/lattiam/pkg/provider/protocol"
)

// Static errors for err113 compliance
var (
	ErrResourceTypeNotFoundInSchema = errors.New("resource type not found in provider schema")
	ErrNoResourcesFoundInJSON       = errors.New("no resources found in Terraform JSON")
)

// Resource represents a single infrastructure resource
type Resource struct {
	Type       string
	Name       string
	Properties map[string]interface{}
}

// DataSource represents a single data source
type DataSource struct {
	Type       string
	Name       string
	Properties map[string]interface{}
}

// Result contains the result of deploying a resource
type Result struct {
	Resource Resource
	State    map[string]interface{}
	Error    error
	// TerraformState contains the raw state data from the provider
	TerraformState *protocol.TerraformRawState
}

// DataSourceResult contains the result of reading a data source
type DataSourceResult struct {
	DataSource DataSource
	State      map[string]interface{}
	Error      error
}

// Options configures deployment behavior
type Options struct {
	DryRun          bool
	ProviderVersion string
	ProviderConfig  protocol.ProviderConfig
	Logger          Logger
	TerraformJSON   map[string]interface{} // For provider version resolution
}

// Logger interface for deployment logging
type Logger interface {
	Infof(format string, args ...interface{})
	Errorf(format string, args ...interface{})
	Debugf(format string, args ...interface{})
	ResourceDeploymentStart(resourceType, resourceName string, current, total int)
	ResourceDeploymentSuccess(resourceType, resourceName string)
	ResourceDeploymentFailed(resourceType, resourceName string, err error)
	DeploymentSummary(successful, total int)
	// ErrorWithAnalysis logs an error with detailed analysis
	ErrorWithAnalysis(err error)
}

// Deployer handles resource deployments
type Deployer struct {
	manager          *protocol.ProviderManager
	logger           Logger
	terraformPlanner *TerraformLibraryPlanner // Use Terraform's internal libraries for planning
}

// NewDeployer creates a deployer instance
func NewDeployer(providerDir string, logger Logger) (*Deployer, error) {
	manager, err := protocol.NewProviderManager(providerDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create provider manager: %w", err)
	}

	// Create Terraform library planner for sophisticated dependency analysis
	terraformPlanner := NewTerraformLibraryPlanner(manager, logger)

	return &Deployer{
		manager:          manager,
		logger:           logger,
		terraformPlanner: terraformPlanner,
	}, nil
}

// GetManager returns the provider manager for advanced operations
func (d *Deployer) GetManager() *protocol.ProviderManager {
	return d.manager
}

// AnalyzeResourceDependencies analyzes resources and returns them in dependency order
// This implements a standard topological sort using depth-first search (DFS), which is
// a well-known graph algorithm used by many tools (Make, npm, apt, etc.) for dependency
// resolution. The algorithm ensures resources are deployed only after their dependencies.
func AnalyzeResourceDependencies(resources []Resource) []Resource {
	// Build dependency graph
	dependencies := make(map[string][]string) // resource -> list of dependencies
	resourceMap := make(map[string]*Resource)

	for i := range resources {
		resource := &resources[i]
		key := fmt.Sprintf("%s/%s", resource.Type, resource.Name)
		resourceMap[key] = resource
		dependencies[key] = []string{}

		// Find all interpolations in this resource
		deps := FindInterpolationDependencies(resource.Properties)
		dependencies[key] = deps
	}

	// Topological sort using DFS
	// Unlike Terraform's full DAG implementation, we use a simplified approach:
	// - No parallel execution planning (we deploy sequentially)
	// - No graph transformations or optimizations
	// - Simple cycle detection with clear error messages
	ordered := []Resource{}
	visited := make(map[string]bool)
	visiting := make(map[string]bool)

	var visit func(string) error
	visit = func(key string) error {
		if visited[key] {
			return nil
		}
		if visiting[key] {
			return fmt.Errorf("circular dependency detected involving %s", key)
		}

		visiting[key] = true

		// Visit dependencies first
		for _, dep := range dependencies[key] {
			if err := visit(dep); err != nil {
				return err
			}
		}

		visiting[key] = false
		visited[key] = true

		if resource, ok := resourceMap[key]; ok {
			ordered = append(ordered, *resource)
		}

		return nil
	}

	// Visit all resources
	for key := range resourceMap {
		if err := visit(key); err != nil {
			// Log warning and return original order if circular dependency detected
			log.Printf("Warning: %v, using original order", err)
			return resources
		}
	}

	return ordered
}

// FindInterpolationDependencies finds all resource dependencies in properties
func FindInterpolationDependencies(properties map[string]interface{}) []string {
	deps := make(map[string]bool)
	findDeps(properties, deps)

	result := []string{}
	for dep := range deps {
		result = append(result, dep)
	}
	return result
}

const dataPrefix = "data"

func findDeps(value interface{}, deps map[string]bool) {
	switch v := value.(type) {
	case string:
		matches := interpolationPattern.FindAllStringSubmatch(v, -1)
		for _, match := range matches {
			if len(match) > 1 {
				parts := strings.Split(match[1], ".")
				// Handle data source references (data.type.name.attribute)
				if parts[0] == dataPrefix && len(parts) >= 3 {
					deps[fmt.Sprintf("%s.%s.%s", dataPrefix, parts[1], parts[2])] = true
				} else if len(parts) >= 2 {
					// Handle regular resource references (type.name.attribute)
					deps[fmt.Sprintf("%s/%s", parts[0], parts[1])] = true
				}
			}
		}
	case map[string]interface{}:
		for _, val := range v {
			findDeps(val, deps)
		}
	case []interface{}:
		for _, val := range v {
			findDeps(val, deps)
		}
	}
}

// DeployResourcesWithPlan uses interpolation resolution for cross-resource references
func (d *Deployer) DeployResourcesWithPlan(
	ctx context.Context,
	_ string, // deploymentID - not used
	terraformJSON map[string]interface{},
	_ string, // stateFile - not used
	opts Options,
) ([]Result, error) {
	// Parse configuration
	config, err := d.parseConfiguration(terraformJSON)
	if err != nil {
		return nil, err
	}

	// Create state store for interpolations
	deployedStates := make(map[string]map[string]interface{})

	// Read data sources first
	if err := d.readDataSources(ctx, config.DataSources, deployedStates, opts); err != nil {
		d.logger.Infof("Warning: Some data sources failed to read: %v", err)
	}

	// Get ordered resources with dependency analysis
	orderedResources := d.getOrderedResources(ctx, config.Resources)

	// Deploy resources
	return d.deployOrderedResources(ctx, orderedResources, deployedStates, terraformJSON, opts)
}

// parseConfiguration parses the Terraform JSON configuration
func (d *Deployer) parseConfiguration(terraformJSON map[string]interface{}) (*TerraformConfiguration, error) {
	jsonData, _ := json.Marshal(terraformJSON)
	config, err := ParseTerraformJSONComplete(jsonData)
	if err != nil {
		return nil, err
	}

	d.logger.Debugf("Parsed configuration: %d resources, %d data sources", len(config.Resources), len(config.DataSources))
	d.logger.Infof("Found %d resources to deploy", len(config.Resources))

	return config, nil
}

// readDataSources reads all data sources and stores their state
func (d *Deployer) readDataSources(ctx context.Context, dataSources []DataSource, deployedStates map[string]map[string]interface{}, opts Options) error {
	if len(dataSources) == 0 {
		return nil
	}

	d.logger.Infof("Reading %d data sources", len(dataSources))

	// Group by provider for efficiency
	dataSourceGroups := d.groupDataSourcesByProvider(dataSources)
	d.logger.Debugf("Grouped data sources into %d providers", len(dataSourceGroups))

	var lastErr error
	for providerName, providerDataSources := range dataSourceGroups {
		if err := d.readProviderDataSources(ctx, providerName, providerDataSources, deployedStates, opts); err != nil {
			lastErr = err
		}
	}

	return lastErr
}

// readProviderDataSources reads data sources for a specific provider
func (d *Deployer) readProviderDataSources(
	ctx context.Context, providerName string, dataSources []DataSource,
	deployedStates map[string]map[string]interface{}, opts Options,
) error {
	d.logger.Debugf("Reading %d data sources for provider %s", len(dataSources), providerName)

	var lastErr error
	for _, dataSource := range dataSources {
		if err := d.readSingleDataSource(ctx, dataSource, deployedStates, opts); err != nil {
			lastErr = err
		}
	}

	return lastErr
}

// readSingleDataSource reads a single data source
func (d *Deployer) readSingleDataSource(
	ctx context.Context, dataSource DataSource,
	deployedStates map[string]map[string]interface{}, opts Options,
) error {
	// Resolve interpolations
	resolvedProps, err := ResolveInterpolations(dataSource.Properties, deployedStates)
	if err != nil {
		return fmt.Errorf("failed to resolve interpolations for data source %s/%s: %w", dataSource.Type, dataSource.Name, err)
	}
	dataSource.Properties = resolvedProps

	result, err := d.ReadDataSource(ctx, dataSource, opts)
	if err != nil {
		d.logger.Errorf("Failed to read data source %s/%s: %v", dataSource.Type, dataSource.Name, err)
		return err
	}

	// Store state for interpolation
	dataKey := fmt.Sprintf("data.%s.%s", dataSource.Type, dataSource.Name)
	deployedStates[dataKey] = result.State
	d.logger.Infof("Successfully read data source %s with state available for interpolation", dataKey)
	d.logger.Debugf("Data source %s state: %+v", dataKey, result.State)

	return nil
}

// getOrderedResources returns resources in dependency order
func (d *Deployer) getOrderedResources(ctx context.Context, resources []Resource) []Resource {
	orderedResources, err := d.terraformPlanner.AnalyzeResourceDependenciesWithTerraform(ctx, resources)
	if err != nil {
		d.logger.Infof("Terraform library planner failed (%v), falling back to simple analysis", err)
		orderedResources = AnalyzeResourceDependencies(resources)
	}
	d.logger.Infof("Deploying %d resources with interpolation support and dependency ordering", len(orderedResources))
	return orderedResources
}

// deployOrderedResources deploys resources in order with retry support
func (d *Deployer) deployOrderedResources(
	ctx context.Context, orderedResources []Resource,
	deployedStates map[string]map[string]interface{},
	_ map[string]interface{}, opts Options,
) ([]Result, error) {
	// Initialize results
	results := make([]Result, len(orderedResources))
	for i, resource := range orderedResources {
		results[i].Resource = resource
	}

	// Build a map of all resources that exist in this deployment
	allResourceKeys := make(map[string]bool)
	for _, resource := range orderedResources {
		resourceKey := fmt.Sprintf("%s/%s", resource.Type, resource.Name)
		allResourceKeys[resourceKey] = true
	}

	// Initialize retry queue for dependency management
	retryQueue := NewDependencyQueueWithKnownResources(allResourceKeys)

	// Copy initial deployed states to retry queue
	for key, state := range deployedStates {
		retryQueue.MarkResourceDeployed(key, state)
	}

	// Deploy resources sequentially in dependency order (don't group by provider)
	// This ensures cross-provider dependencies are respected
	for i, resource := range orderedResources {
		d.logger.Infof("Deploying resource %d/%d: %s.%s", i+1, len(orderedResources), resource.Type, resource.Name)
		ir := resourceWithIndex{resource: resource, index: i}
		providerName := ExtractProviderName(resource.Type)
		version := d.getProviderVersionForGroup(providerName, opts)

		// Log detailed information about deployment attempt
		d.logger.Debugf("Attempting deployment of %s/%s with provider %s version %s",
			resource.Type, resource.Name, providerName, version)

		err := d.deploySingleResourceWithRetry(ctx, ir, providerName, version, deployedStates, results, retryQueue, opts)

		// If resource succeeded, mark it as deployed in retry queue
		if err == nil {
			resourceKey := fmt.Sprintf("%s/%s", resource.Type, resource.Name)
			retryQueue.MarkResourceDeployed(resourceKey, results[i].State)
			d.logger.Infof("Resource %s deployed successfully", resourceKey)
		} else {
			d.logger.Infof("Resource %s/%s was not deployed successfully in initial pass: %v", resource.Type, resource.Name, err)
		}
	}

	// Process retry queue until empty or max retries exceeded
	d.processRetryQueue(ctx, retryQueue, results, opts)

	// Mark any remaining pending resources as failed
	if retryQueue.HasPendingResources() {
		d.markPendingResourcesAsFailed(retryQueue, results)
	}

	// Log summary
	d.logDeploymentSummary(results)
	return results, nil
}

// getProviderVersionForGroup determines the provider version
func (d *Deployer) getProviderVersionForGroup(providerName string, opts Options) string {
	if opts.TerraformJSON != nil {
		resolver := NewProviderVersionResolver(opts.TerraformJSON)
		return resolver.GetProviderVersion(providerName, opts.ProviderVersion)
	}

	if opts.ProviderVersion != "" {
		return opts.ProviderVersion
	}

	return getDefaultProviderVersion(providerName)
}

// deploySingleResourceWithRetry deploys a single resource with retry support
func (d *Deployer) deploySingleResourceWithRetry(
	ctx context.Context, ir resourceWithIndex, providerName, version string,
	deployedStates map[string]map[string]interface{}, results []Result,
	retryQueue *DependencyQueue, opts Options,
) error {
	resource := ir.resource

	// Check if interpolations can be resolved
	resolvedProperties, err := ResolveInterpolationsStrict(resource.Properties, deployedStates)
	if err != nil {
		// Add to retry queue - dependencies not ready
		d.logger.Infof("Adding %s/%s to retry queue: %v", resource.Type, resource.Name, err)

		// Get the current pending count before adding
		beforeCount := retryQueue.GetPendingCount()
		retryQueue.AddFailedResource(resource.Type, resource.Name, resource.Properties, err)
		afterCount := retryQueue.GetPendingCount()

		// If the resource wasn't added to the queue (permanent failure), mark it as failed
		if afterCount == beforeCount {
			results[ir.index].Error = fmt.Errorf("dependency resolution failed: %w", err)
			d.logger.Errorf("Resource %s/%s has permanently unresolvable dependencies: %v", resource.Type, resource.Name, err)
			return results[ir.index].Error
		}

		return nil
	}

	// Set resolved properties
	resource.Properties = resolvedProperties
	d.logger.Debugf("After interpolation for %s.%s: %+v", resource.Type, resource.Name, resource.Properties)

	// Get provider schema
	providerSchema, err := d.fetchProviderSchema(ctx, providerName, version, resource.Type, opts.ProviderConfig)
	if err != nil {
		results[ir.index].Error = err
		d.logger.Errorf("Failed to get schema for %s/%s: %v", resource.Type, resource.Name, err)
		return err
	}

	// Deploy the resource
	state, err := d.deployResourceWithProvider(ctx, resource, providerName, version, providerSchema, opts.ProviderConfig)
	// Store result
	if err != nil {
		// Check if this is a retryable error
		if isRetryableError(err) {
			d.logger.Infof("Adding %s/%s to retry queue due to retryable error: %v", resource.Type, resource.Name, err)
			retryQueue.AddFailedResource(resource.Type, resource.Name, resource.Properties, err)
		} else {
			results[ir.index].Error = err
			d.logger.Errorf("Failed to deploy %s/%s with non-retryable error: %v", resource.Type, resource.Name, err)
		}
		return err
	}

	results[ir.index].State = state
	d.storeDeployedState(resource, state, deployedStates)
	d.logger.Infof("Successfully deployed %s/%s", resource.Type, resource.Name)
	return nil
}

// processRetryQueue processes resources in the retry queue
func (d *Deployer) processRetryQueue(ctx context.Context, retryQueue *DependencyQueue, results []Result, opts Options) {
	d.logger.Infof("Processing retry queue...")

	maxRetryRounds := 10 // Prevent infinite loops
	retryRound := 0

	for retryQueue.HasPendingResources() && retryRound < maxRetryRounds {
		retryRound++
		d.logger.Infof("Retry round %d: %d resources pending", retryRound, retryQueue.GetPendingCount())

		readyResources := retryQueue.GetReadyResources(ctx)
		if len(readyResources) == 0 {
			d.logger.Infof("No resources ready for retry, waiting...")
			// Wait longer before checking again to respect exponential backoff
			select {
			case <-ctx.Done():
				d.logger.Infof("Context canceled during retry processing")
				return
			case <-time.After(5 * time.Second):
				continue
			}
		}

		progressMade := false
		for _, retryResource := range readyResources {
			d.logger.Infof("Retrying %s/%s (attempt %d)", retryResource.ResourceType, retryResource.ResourceName, retryResource.RetryCount+1)

			// Find result index for this resource
			resultIndex := d.findResultIndex(results, retryResource.ResourceType, retryResource.ResourceName)
			if resultIndex == -1 {
				d.logger.Errorf("Could not find result index for %s/%s", retryResource.ResourceType, retryResource.ResourceName)
				continue
			}

			// Create resource and indexed resource for retry
			resource := Resource{
				Type:       retryResource.ResourceType,
				Name:       retryResource.ResourceName,
				Properties: retryResource.Properties,
			}
			ir := resourceWithIndex{resource: resource, index: resultIndex}

			providerName := ExtractProviderName(resource.Type)
			version := d.getProviderVersionForGroup(providerName, opts)

			// Try to deploy with current deployed states from retry queue
			deployedStates := retryQueue.GetDeployedStates()
			err := d.deploySingleResourceWithRetry(ctx, ir, providerName, version, deployedStates, results, retryQueue, opts)

			if err == nil {
				progressMade = true
				resourceKey := fmt.Sprintf("%s/%s", resource.Type, resource.Name)
				retryQueue.MarkResourceDeployed(resourceKey, results[resultIndex].State)
				d.logger.Infof("Successfully deployed %s/%s on retry", resource.Type, resource.Name)
			}
		}

		// If no progress was made, break to avoid infinite loop
		if !progressMade {
			d.logger.Infof("No progress made in retry round %d, stopping", retryRound)
			break
		}
	}

	if retryQueue.HasPendingResources() {
		d.logger.Infof("Retry processing complete. %d resources still pending after %d rounds", retryQueue.GetPendingCount(), retryRound)
	} else {
		d.logger.Infof("All resources successfully deployed after %d retry rounds", retryRound)
	}
}

// findResultIndex finds the index of a resource in the results array
func (d *Deployer) findResultIndex(results []Result, resourceType, resourceName string) int {
	for i, result := range results {
		if result.Resource.Type == resourceType && result.Resource.Name == resourceName {
			return i
		}
	}
	return -1
}

// storeDeployedState stores the deployed state for interpolation
func (d *Deployer) storeDeployedState(resource Resource, state map[string]interface{}, deployedStates map[string]map[string]interface{}) {
	resourceKey := fmt.Sprintf("%s/%s", resource.Type, resource.Name)
	deployedStates[resourceKey] = state
	d.logger.Infof("Successfully deployed %s with state available for interpolation", resourceKey)
}

// isRetryableError determines if an error is retryable
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}

	errStr := err.Error()

	// Common retryable error patterns
	retryablePatterns := []string{
		"no valid credential sources found",
		"connection timeout",
		"request timeout",
		"rate limited",
		"throttled",
		"service unavailable",
		"internal server error",
		"temporary failure",
	}

	for _, pattern := range retryablePatterns {
		if strings.Contains(strings.ToLower(errStr), pattern) {
			return true
		}
	}

	return false
}

// markPendingResourcesAsFailed marks resources still in retry queue as failed
func (d *Deployer) markPendingResourcesAsFailed(retryQueue *DependencyQueue, results []Result) {
	pendingResources := retryQueue.GetAllPendingResources()
	for _, pending := range pendingResources {
		// Find the resource in results and mark as failed
		for i, result := range results {
			if result.Resource.Type == pending.ResourceType && result.Resource.Name == pending.ResourceName {
				if results[i].Error == nil {
					results[i].Error = fmt.Errorf("dependency resolution failed after maximum retries: %w", pending.LastError)
					d.logger.Errorf("Resource %s/%s failed: unresolved dependencies", pending.ResourceType, pending.ResourceName)
				}
				break
			}
		}
	}
}

// logDeploymentSummary logs the deployment results summary
func (d *Deployer) logDeploymentSummary(results []Result) {
	successCount := 0
	for _, result := range results {
		if result.Error == nil {
			successCount++
		}
	}
	d.logger.DeploymentSummary(successCount, len(results))
}

// groupDataSourcesByProvider groups data sources by their provider
func (d *Deployer) groupDataSourcesByProvider(dataSources []DataSource) map[string][]DataSource {
	groups := make(map[string][]DataSource)
	for _, ds := range dataSources {
		providerName := ExtractProviderName(ds.Type)
		groups[providerName] = append(groups[providerName], ds)
	}
	return groups
}

// Close cleans up deployer resources
func (d *Deployer) Close() error {
	if d.manager != nil {
		if err := d.manager.Close(); err != nil {
			return fmt.Errorf("failed to close provider manager: %w", err)
		}
	}
	return nil
}

// resourceWithIndex tracks a resource with its original index
type resourceWithIndex struct {
	resource Resource
	index    int
}

// DeployResources deploys multiple resources
func (d *Deployer) DeployResources(ctx context.Context, resources []Resource, opts Options) []Result {
	results := make([]Result, len(resources))

	// Initialize results with resources
	for i, resource := range resources {
		results[i].Resource = resource
	}

	d.logger.Infof("Starting deployment of %d resources", len(resources))

	// Analyze dependencies and get ordered resources
	orderedResources := AnalyzeResourceDependencies(resources)
	d.logger.Infof("Deploying %d resources with dependency ordering", len(orderedResources))

	// Create state store for interpolations
	deployedStates := make(map[string]map[string]interface{})

	// Deploy resources sequentially in dependency order
	for i, resource := range orderedResources {
		d.logger.Infof("Deploying resource %d/%d: %s.%s", i+1, len(orderedResources), resource.Type, resource.Name)

		// Find the original index for this resource
		originalIndex := -1
		for j, originalResource := range resources {
			if originalResource.Type == resource.Type && originalResource.Name == resource.Name {
				originalIndex = j
				break
			}
		}

		if originalIndex == -1 {
			d.logger.Errorf("Could not find original index for resource %s.%s", resource.Type, resource.Name)
			continue
		}

		// Resolve interpolations
		d.logger.Debugf("Before interpolation for %s.%s: %+v", resource.Type, resource.Name, resource.Properties)
		d.logger.Debugf("Available deployed states: %+v", deployedStates)
		var err error
		resource.Properties, err = ResolveInterpolations(resource.Properties, deployedStates)
		if err != nil {
			results[originalIndex].Error = err
			d.logger.Errorf("Failed to resolve interpolations for %s/%s: %v", resource.Type, resource.Name, err)
			continue
		}
		d.logger.Debugf("After interpolation for %s.%s: %+v", resource.Type, resource.Name, resource.Properties)

		// Deploy the resource
		result, err := d.DeployResource(ctx, resource, opts)
		if err != nil {
			results[originalIndex].Error = err
			d.logger.Errorf("Failed to deploy %s/%s: %v", resource.Type, resource.Name, err)
		} else {
			results[originalIndex] = *result
			// Store state for interpolation
			resourceKey := fmt.Sprintf("%s/%s", resource.Type, resource.Name)
			deployedStates[resourceKey] = result.State
			d.logger.Infof("Successfully deployed %s with state available for interpolation", resourceKey)
		}
	}

	// Log deployment summary
	successCount := 0
	for _, result := range results {
		if result.Error == nil {
			successCount++
		}
	}
	d.logger.DeploymentSummary(successCount, len(results))

	return results
}

// DeployResource deploys a single resource
func (d *Deployer) DeployResource(ctx context.Context, resource Resource, opts Options) (*Result, error) {
	providerName := ExtractProviderName(resource.Type)
	// Create version resolver if terraform JSON is provided
	var version string
	if opts.TerraformJSON != nil {
		resolver := NewProviderVersionResolver(opts.TerraformJSON)
		version = resolver.GetProviderVersion(providerName, opts.ProviderVersion)
	} else {
		version = d.getProviderVersion(providerName, opts.ProviderVersion)
	}

	// Check for cached schema
	providerKey := fmt.Sprintf("%s-%s", providerName, version)
	cachedSchema, hasCached := d.manager.GetCachedSchema(providerKey)
	var providerSchema *tfprotov6.GetProviderSchemaResponse

	d.logger.Infof("Processing resource %s (type: %s) with provider %s v%s, cache key: %s, cached: %v",
		resource.Name, resource.Type, providerName, version, providerKey, hasCached)

	if hasCached {
		d.logger.Infof("Using cached schema for %s v%s", providerName, version)
		providerSchema = cachedSchema
		// Debug: Check if cached schema has the expected resource type
		if _, exists := providerSchema.ResourceSchemas[resource.Type]; !exists {
			d.logger.Errorf("SCHEMA MISMATCH: Cached schema for %s does not contain resource type %s. Available types: %v",
				providerKey, resource.Type, d.getResourceTypesList(providerSchema))
		}
	} else {
		d.logger.Infof("Fetching new schema for %s v%s", providerName, version)
		// Get schema from provider
		schema, err := d.fetchProviderSchema(ctx, providerName, version, resource.Type, opts.ProviderConfig)
		if err != nil {
			return nil, err
		}
		providerSchema = schema
		d.manager.SetCachedSchema(providerKey, providerSchema)
		d.logger.Infof("Cached new schema for %s v%s with %d resource types",
			providerName, version, len(providerSchema.ResourceSchemas))
	}

	// Verify resource type exists
	if _, exists := providerSchema.ResourceSchemas[resource.Type]; !exists {
		return nil, fmt.Errorf("%w: %s", ErrResourceTypeNotFoundInSchema, resource.Type)
	}

	if opts.DryRun {
		d.logger.Infof("Dry run: Resource %s/%s would be created", resource.Type, resource.Name)
		return &Result{
			Resource: resource,
			State:    resource.Properties,
		}, nil
	}

	// Deploy the resource
	state, err := d.deployResourceWithProvider(ctx, resource, providerName, version, providerSchema, opts.ProviderConfig)
	if err != nil {
		return nil, err
	}

	return &Result{
		Resource: resource,
		State:    state,
	}, nil
}

// fetchProviderSchema gets the provider schema
func (d *Deployer) fetchProviderSchema(
	ctx context.Context, providerName, version, resourceType string, providerConfig protocol.ProviderConfig,
) (*tfprotov6.GetProviderSchemaResponse, error) {
	instance, err := d.manager.GetProviderWithConfig(ctx, providerName, version, providerConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to get provider: %w", err)
	}
	defer func() {
		if err := instance.Stop(); err != nil {
			d.logger.Errorf("Failed to stop provider instance: %v", err)
		}
	}()

	client, err := protocol.NewTerraformCompatibleClient(ctx, instance, resourceType)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}
	defer func() {
		_ = client.Close()
	}()

	schema, err := client.GetProviderSchema(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get provider schema: %w", err)
	}
	return schema, nil
}

// deployResourceWithProvider deploys a resource with a provider instance
func (d *Deployer) deployResourceWithProvider(
	ctx context.Context, resource Resource, providerName, version string,
	providerSchema *tfprotov6.GetProviderSchemaResponse, providerConfig protocol.ProviderConfig,
) (map[string]interface{}, error) {
	instance, err := d.manager.GetProviderWithConfig(ctx, providerName, version, providerConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to get provider: %w", err)
	}
	defer func() {
		if err := instance.Stop(); err != nil {
			d.logger.Errorf("Failed to stop provider instance: %v", err)
		}
	}()

	client, err := protocol.NewTerraformCompatibleClient(ctx, instance, resource.Type)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}
	defer func() {
		_ = client.Close()
	}()

	// Configure provider
	if err := d.configureProvider(ctx, client, providerName, providerSchema, providerConfig); err != nil {
		return nil, err
	}

	// Ensure optional fields for certain resource types
	if resource.Type == "aws_iam_role" && resource.Properties["tags"] == nil {
		resource.Properties["tags"] = make(map[string]interface{})
	}

	// AWS provider v6 requires timeouts block for S3 buckets
	if resource.Type == "aws_s3_bucket" && resource.Properties["timeouts"] == nil {
		resource.Properties["timeouts"] = make(map[string]interface{})
	}

	// Create the resource
	result, err := client.Create(ctx, resource.Properties)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}
	return result, nil
}

// configureProvider configures the provider with appropriate settings
func (d *Deployer) configureProvider(
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
		if currentProviderConfig, ok := providerConfig[providerName]; ok {
			for key, value := range currentProviderConfig {
				envConfig.AttributeOverrides[key] = value
			}
		}
	}

	// Apply endpoint-specific configuration
	endpointStrategy := provider.GetEndpointStrategy()
	endpointStrategy.ConfigureEndpoint(envConfig)

	configValues := make(map[string]interface{})
	if providerConfig != nil {
		if currentProviderConfig, ok := providerConfig[providerName]; ok {
			for key, value := range currentProviderConfig {
				configValues[key] = value
			}
		}
	}

	configData, err := configurator.ConfigureProvider(
		ctx,
		providerName,
		providerSchema.Provider.Block,
		configValues,
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
func (d *Deployer) getProviderVersion(providerName, override string) string {
	if override != "" {
		return override
	}

	switch providerName {
	case "aws":
		return "5.99.1" // Use existing version for now
	case "azurerm":
		return "3.85.0"
	case "google":
		return "5.10.0"
	default:
		return "latest"
	}
}

// ExtractProviderName extracts provider name from resource type
func ExtractProviderName(resourceType string) string {
	// Handle data sources like "data/aws_s3_bucket"
	if strings.Contains(resourceType, "/") {
		parts := strings.SplitN(resourceType, "/", 2)
		if len(parts) > 1 {
			// The actual provider is part of the second element, e.g., "aws_s3_bucket"
			subParts := strings.SplitN(parts[1], "_", 2)
			if len(subParts) > 0 {
				return subParts[0]
			}
		}
	}
	// Original logic for standard resources
	parts := strings.SplitN(resourceType, "_", 2)
	if len(parts) > 0 {
		return parts[0]
	}
	return resourceType
}

// getResourceTypesList returns a list of resource types in a schema for debugging
func (d *Deployer) getResourceTypesList(schema *tfprotov6.GetProviderSchemaResponse) []string {
	if schema == nil || schema.ResourceSchemas == nil {
		return []string{}
	}
	types := make([]string, 0, len(schema.ResourceSchemas))
	for resourceType := range schema.ResourceSchemas {
		types = append(types, resourceType)
	}
	return types
}

// ReadDataSource reads a single data source
func (d *Deployer) ReadDataSource(ctx context.Context, dataSource DataSource, opts Options) (*DataSourceResult, error) {
	providerName := ExtractProviderName(dataSource.Type)

	// Get provider version
	var version string
	if opts.TerraformJSON != nil {
		resolver := NewProviderVersionResolver(opts.TerraformJSON)
		version = resolver.GetProviderVersion(providerName, opts.ProviderVersion)
	} else {
		version = d.getProviderVersion(providerName, opts.ProviderVersion)
	}

	// Get provider instance
	instance, err := d.manager.GetProviderWithConfig(ctx, providerName, version, opts.ProviderConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to get provider: %w", err)
	}
	defer func() {
		if err := instance.Stop(); err != nil {
			d.logger.Errorf("Failed to stop provider instance: %v", err)
		}
	}()

	// Create client for data source
	client, err := protocol.NewTerraformCompatibleClient(ctx, instance, dataSource.Type)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}
	defer func() {
		_ = client.Close()
	}()

	// Get provider schema
	providerSchema, err := client.GetProviderSchema(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get provider schema: %w", err)
	}

	// Configure provider
	if err := d.configureProvider(ctx, client, providerName, providerSchema, opts.ProviderConfig); err != nil {
		return nil, err
	}

	// Read the data source
	d.logger.Infof("Reading data source %s/%s", dataSource.Type, dataSource.Name)
	state, err := client.ReadDataSource(ctx, dataSource.Properties)
	if err != nil {
		return nil, fmt.Errorf("failed to read data source: %w", err)
	}

	return &DataSourceResult{
		DataSource: dataSource,
		State:      state,
	}, nil
}

// TerraformConfiguration holds parsed Terraform JSON configuration
type TerraformConfiguration struct {
	Resources   []Resource
	DataSources []DataSource
}

// ParseTerraformJSON parses Terraform JSON and returns resources
func ParseTerraformJSON(data []byte) ([]Resource, error) {
	config, err := ParseTerraformJSONComplete(data)
	if err != nil {
		return nil, err
	}
	return config.Resources, nil
}

// ParseTerraformJSONComplete parses Terraform JSON and returns both resources and data sources
func ParseTerraformJSONComplete(data []byte) (*TerraformConfiguration, error) {
	var tfJSON struct {
		Resource map[string]map[string]map[string]interface{} `json:"resource"`
		Data     map[string]map[string]map[string]interface{} `json:"data"`
	}

	if err := json.Unmarshal(data, &tfJSON); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	config := &TerraformConfiguration{}

	// Parse resources
	for resourceType, instances := range tfJSON.Resource {
		for instanceName, properties := range instances {
			config.Resources = append(config.Resources, Resource{
				Type:       resourceType,
				Name:       instanceName,
				Properties: properties,
			})
		}
	}

	// Parse data sources
	for dataType, instances := range tfJSON.Data {
		for instanceName, properties := range instances {
			config.DataSources = append(config.DataSources, DataSource{
				Type:       dataType,
				Name:       instanceName,
				Properties: properties,
			})
		}
	}

	if len(config.Resources) == 0 && len(config.DataSources) == 0 {
		return nil, ErrNoResourcesFoundInJSON
	}

	return config, nil
}
