package deployment

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/lattiam/lattiam/internal/provider"
)

const (
	maxRetries = 3
	baseDelay  = 2 * time.Second
	maxDelay   = 30 * time.Second
)

// RetryableResource represents a resource that failed and should be retried
type RetryableResource struct {
	ResourceType  string
	ResourceName  string
	Properties    map[string]interface{}
	Dependencies  []string
	RetryCount    int
	LastError     error
	NextRetryTime time.Time
}

// DependencyQueue manages resources waiting for dependencies or retries
type DependencyQueue struct {
	pendingResources  map[string]*RetryableResource
	deployedResources map[string]map[string]interface{}
	knownResources    map[string]bool // All resources that exist in the deployment
}

// NewDependencyQueue creates a new dependency queue
func NewDependencyQueue() *DependencyQueue {
	return &DependencyQueue{
		pendingResources:  make(map[string]*RetryableResource),
		deployedResources: make(map[string]map[string]interface{}),
		knownResources:    make(map[string]bool),
	}
}

// NewDependencyQueueWithKnownResources creates a new dependency queue with known resources
func NewDependencyQueueWithKnownResources(knownResources map[string]bool) *DependencyQueue {
	return &DependencyQueue{
		pendingResources:  make(map[string]*RetryableResource),
		deployedResources: make(map[string]map[string]interface{}),
		knownResources:    knownResources,
	}
}

// AddFailedResource adds a resource to the retry queue
func (dq *DependencyQueue) AddFailedResource(resourceType, resourceName string, properties map[string]interface{}, err error) {
	resourceKey := fmt.Sprintf("%s/%s", resourceType, resourceName)

	// Extract dependencies from interpolations
	dependencies := dq.extractDependencies(properties)

	// Check if any dependencies are permanently unresolvable (don't exist in deployment)
	if len(dq.knownResources) > 0 { // Only check if we have the known resources list
		for _, dep := range dependencies {
			if !dq.knownResources[dep] && !strings.HasPrefix(dep, "data.") {
				// This dependency doesn't exist in the deployment and isn't a data source
				log.Printf("Resource %s has permanently unresolvable dependency: %s", resourceKey, dep)
				// Don't add to retry queue - it will never succeed
				return
			}
		}
	}

	// Calculate next retry time with exponential backoff
	retryCount := 0
	if existing, exists := dq.pendingResources[resourceKey]; exists {
		retryCount = existing.RetryCount + 1
	}

	delay := calculateBackoffDelay(retryCount)
	nextRetry := time.Now().Add(delay)

	dq.pendingResources[resourceKey] = &RetryableResource{
		ResourceType:  resourceType,
		ResourceName:  resourceName,
		Properties:    properties,
		Dependencies:  dependencies,
		RetryCount:    retryCount,
		LastError:     err,
		NextRetryTime: nextRetry,
	}

	log.Printf("Added %s to retry queue (attempt %d, next retry: %v, dependencies: %v)", resourceKey, retryCount+1, nextRetry, dependencies)
}

// MarkResourceDeployed marks a resource as successfully deployed
func (dq *DependencyQueue) MarkResourceDeployed(resourceKey string, result map[string]interface{}) {
	dq.deployedResources[resourceKey] = result

	// Remove from pending if it was there
	delete(dq.pendingResources, resourceKey)

	log.Printf("Marked %s as deployed, checking dependent resources", resourceKey)
}

// GetReadyResources returns resources that are ready to retry
func (dq *DependencyQueue) GetReadyResources(_ context.Context) []*RetryableResource {
	var ready []*RetryableResource
	now := time.Now()

	for _, resource := range dq.pendingResources {
		// Check if retry time has passed
		if now.Before(resource.NextRetryTime) {
			continue
		}

		// Check if max retries exceeded
		if resource.RetryCount >= maxRetries {
			log.Printf("Max retries exceeded for %s/%s: %v", resource.ResourceType, resource.ResourceName, resource.LastError)
			continue
		}

		// Check if dependencies are now available
		if dq.areDependenciesReady(resource.Dependencies) {
			ready = append(ready, resource)
		}
	}

	return ready
}

// RetryResource attempts to deploy a resource again
func (dq *DependencyQueue) RetryResource(ctx context.Context, resource *RetryableResource, prov provider.Provider) error {
	resourceKey := fmt.Sprintf("%s/%s", resource.ResourceType, resource.ResourceName)

	// Resolve interpolations with current deployed resources
	resolvedProperties, err := ResolveInterpolations(resource.Properties, dq.deployedResources)
	if err != nil {
		// If interpolation fails, dependencies not ready yet
		return fmt.Errorf("interpolation failed: %w", err)
	}

	// Validate that all interpolations were resolved
	unresolved := ValidateInterpolations(resolvedProperties, dq.deployedResources)
	if len(unresolved) > 0 {
		return fmt.Errorf("unresolved interpolations: %v", unresolved)
	}

	log.Printf("Retrying deployment of %s (attempt %d)", resourceKey, resource.RetryCount+1)

	// Attempt to deploy the resource
	result, err := prov.CreateResource(ctx, resource.ResourceType, resolvedProperties)
	if err != nil {
		// Check if this is a retryable error
		if isRetryableError(err) {
			dq.AddFailedResource(resource.ResourceType, resource.ResourceName, resource.Properties, err)
			return fmt.Errorf("retryable error, will retry: %w", err)
		}

		// Non-retryable error, remove from queue
		delete(dq.pendingResources, resourceKey)
		return fmt.Errorf("non-retryable error: %w", err)
	}

	// Success - mark as deployed
	dq.MarkResourceDeployed(resourceKey, result)
	return nil
}

// extractDependencies extracts resource dependencies from interpolations
func (dq *DependencyQueue) extractDependencies(properties map[string]interface{}) []string {
	var dependencies []string
	dq.extractDependenciesFromValue(properties, &dependencies)
	return dependencies
}

// extractDependenciesFromValue recursively extracts dependencies from a value
func (dq *DependencyQueue) extractDependenciesFromValue(value interface{}, dependencies *[]string) {
	switch v := value.(type) {
	case string:
		dq.extractDependenciesFromString(v, dependencies)
	case map[string]interface{}:
		dq.extractDependenciesFromMap(v, dependencies)
	case []interface{}:
		dq.extractDependenciesFromSlice(v, dependencies)
	}
}

// extractDependenciesFromString extracts dependencies from a string value
func (dq *DependencyQueue) extractDependenciesFromString(s string, dependencies *[]string) {
	matches := interpolationPattern.FindAllStringSubmatch(s, -1)
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		resourceKey := dq.parseResourceReference(match[1])
		if resourceKey != "" {
			dq.addUniqueDependency(resourceKey, dependencies)
		}
	}
}

// extractDependenciesFromMap extracts dependencies from a map
func (dq *DependencyQueue) extractDependenciesFromMap(m map[string]interface{}, dependencies *[]string) {
	for _, val := range m {
		dq.extractDependenciesFromValue(val, dependencies)
	}
}

// extractDependenciesFromSlice extracts dependencies from a slice
func (dq *DependencyQueue) extractDependenciesFromSlice(s []interface{}, dependencies *[]string) {
	for _, val := range s {
		dq.extractDependenciesFromValue(val, dependencies)
	}
}

// parseResourceReference parses a reference string and returns the resource key
func (dq *DependencyQueue) parseResourceReference(reference string) string {
	parts := strings.Split(reference, ".")

	switch {
	case parts[0] == "data" && len(parts) >= 3:
		return fmt.Sprintf("data.%s.%s", parts[1], parts[2])
	case len(parts) >= 2:
		return fmt.Sprintf("%s/%s", parts[0], parts[1])
	default:
		return ""
	}
}

// addUniqueDependency adds a dependency if it doesn't already exist
func (dq *DependencyQueue) addUniqueDependency(resourceKey string, dependencies *[]string) {
	for _, dep := range *dependencies {
		if dep == resourceKey {
			return
		}
	}
	*dependencies = append(*dependencies, resourceKey)
}

// areDependenciesReady checks if all dependencies are deployed
func (dq *DependencyQueue) areDependenciesReady(dependencies []string) bool {
	for _, dep := range dependencies {
		if _, exists := dq.deployedResources[dep]; !exists {
			log.Printf("Dependency %s not ready (available: %v)", dep, func() []string {
				keys := make([]string, 0, len(dq.deployedResources))
				for k := range dq.deployedResources {
					keys = append(keys, k)
				}
				return keys
			}())
			return false
		}
	}
	return true
}

// calculateBackoffDelay calculates exponential backoff delay
func calculateBackoffDelay(retryCount int) time.Duration {
	delay := baseDelay
	for i := 0; i < retryCount; i++ {
		delay *= 2
		if delay > maxDelay {
			delay = maxDelay
			break
		}
	}
	return delay
}

// HasPendingResources returns true if there are resources in the queue
func (dq *DependencyQueue) HasPendingResources() bool {
	return len(dq.pendingResources) > 0
}

// GetPendingCount returns the number of pending resources
func (dq *DependencyQueue) GetPendingCount() int {
	return len(dq.pendingResources)
}

// GetAllPendingResources returns all pending resources
func (dq *DependencyQueue) GetAllPendingResources() []*RetryableResource {
	resources := make([]*RetryableResource, 0, len(dq.pendingResources))
	for _, resource := range dq.pendingResources {
		resources = append(resources, resource)
	}
	return resources
}

// GetDeployedStates returns the map of currently deployed resources and their states.
func (dq *DependencyQueue) GetDeployedStates() map[string]map[string]interface{} {
	// Return a copy to prevent external modification
	states := make(map[string]map[string]interface{}, len(dq.deployedResources))
	for k, v := range dq.deployedResources {
		states[k] = v
	}
	return states
}
