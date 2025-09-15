// Package dependency provides dependency resolution and change ordering for resource deployment
package dependency

import (
	"fmt"
	"strings"

	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/internal/logging"
)

// ChangeOrderCalculator provides centralized logic for calculating resource change execution order
type ChangeOrderCalculator struct {
	resolver interfaces.DependencyResolver
	logger   *logging.Logger
}

// NewChangeOrderCalculator creates a new instance of ChangeOrderCalculator
func NewChangeOrderCalculator(resolver interfaces.DependencyResolver) *ChangeOrderCalculator {
	return &ChangeOrderCalculator{
		resolver: resolver,
		logger:   logging.NewLogger("change-order-calculator"),
	}
}

// CalculateExecutionOrder determines the dependency-aware execution order for resource changes.
// This centralizes the logic that was previously duplicated in DeploymentService and DeploymentExecutor.
func (c *ChangeOrderCalculator) CalculateExecutionOrder(changes []interfaces.ResourceChange) ([]interfaces.ResourceChange, error) {
	if len(changes) == 0 {
		return changes, nil
	}

	// Convert ResourceChange objects to Resources for dependency analysis
	resources := c.convertChangesToResources(changes)

	// Build dependency graph using the dependency resolver
	graph, err := c.resolver.BuildDependencyGraph(resources, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build dependency graph for changes: %w", err)
	}

	// Validate no cycles exist
	if err := c.resolver.ValidateNoCycles(graph); err != nil {
		return nil, fmt.Errorf("circular dependency detected in update plan: %w", err)
	}

	// Get topological execution order
	executionOrder, err := c.resolver.GetExecutionOrder(graph)
	if err != nil {
		return nil, fmt.Errorf("failed to determine execution order: %w", err)
	}

	// Build a map of changes by resource key for quick lookup
	changeMap := make(map[string]*interfaces.ResourceChange)
	for i := range changes {
		key := string(changes[i].ResourceKey)
		changeMap[key] = &changes[i]
	}

	// Order changes based on dependency graph, handling different action types
	orderedChanges := make([]interfaces.ResourceChange, 0, len(changes))

	// Process non-delete changes in dependency order
	for _, key := range executionOrder {
		if change, ok := changeMap[key]; ok {
			if change.Action != interfaces.ActionDelete {
				orderedChanges = append(orderedChanges, *change)
			}
		}
	}

	// Process deletes in reverse dependency order (reverse the execution order)
	for i := len(executionOrder) - 1; i >= 0; i-- {
		key := executionOrder[i]
		if change, ok := changeMap[key]; ok {
			if change.Action == interfaces.ActionDelete {
				orderedChanges = append(orderedChanges, *change)
			}
		}
	}

	return orderedChanges, nil
}

// convertChangesToResources converts ResourceChange objects to Resource objects for dependency analysis
func (c *ChangeOrderCalculator) convertChangesToResources(changes []interfaces.ResourceChange) []interfaces.Resource {
	resources := make([]interfaces.Resource, 0, len(changes))

	for _, change := range changes {
		// Extract type and name from ResourceKey (format: "type.name")
		parts := strings.SplitN(string(change.ResourceKey), ".", 2)
		if len(parts) != 2 {
			c.logger.Warnf("Invalid resource key format: %s", change.ResourceKey)
			continue
		}

		// Determine which properties to use based on action
		var properties map[string]interface{}
		switch change.Action {
		case interfaces.ActionCreate, interfaces.ActionUpdate, interfaces.ActionReplace:
			// For creates and updates, use the "after" state
			properties = change.After
		case interfaces.ActionDelete:
			// For deletes, use the "before" state to maintain dependencies
			properties = change.Before
		case interfaces.ActionNoOp:
			// For no-ops, use either (they should be the same)
			properties = change.After
			if properties == nil {
				properties = change.Before
			}
		}

		resource := interfaces.Resource{
			Type:       parts[0],
			Name:       parts[1],
			Properties: properties,
		}
		resources = append(resources, resource)
	}

	return resources
}
