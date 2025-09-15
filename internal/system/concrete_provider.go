package system

import (
	"context"
	"fmt"

	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/internal/provider"
)

// ConcreteProvider implements the provider interfaces, wrapping the underlying provider client.
// It's a concrete struct that implements all interfaces by delegating
// to the wrapped provider.Provider implementation.
//
// This allows ProviderLifecycleManager.GetProvider() to return a concrete type
// instead of an interface, eliminating the need for the fat Provider interface.
type ConcreteProvider struct {
	// The actual provider implementation (protocol-specific)
	wrapped provider.Provider

	// Metadata
	name    string
	version string
}

// NewConcreteProvider creates a new ConcreteProvider wrapping the actual implementation
func NewConcreteProvider(p provider.Provider, name, version string) *ConcreteProvider {
	return &ConcreteProvider{
		wrapped: p,
		name:    name,
		version: version,
	}
}

// --- ResourceOperations implementation ---

// CreateResource creates a new resource with the given properties
func (p *ConcreteProvider) CreateResource(ctx context.Context, resourceType string, properties map[string]interface{}) (map[string]interface{}, error) {
	result, err := p.wrapped.CreateResource(ctx, resourceType, properties)
	if err != nil {
		return nil, fmt.Errorf("create resource %s: %w", resourceType, err)
	}
	return result, nil
}

// ReadResource reads the current state of a resource
func (p *ConcreteProvider) ReadResource(ctx context.Context, resourceType string, properties map[string]interface{}) (map[string]interface{}, error) {
	result, err := p.wrapped.ReadResource(ctx, resourceType, properties)
	if err != nil {
		return nil, fmt.Errorf("read resource %s: %w", resourceType, err)
	}
	return result, nil
}

// UpdateResource updates an existing resource with new properties
func (p *ConcreteProvider) UpdateResource(ctx context.Context, resourceType string, properties map[string]interface{}, currentState map[string]interface{}) (map[string]interface{}, error) {
	// Note: provider.Provider has (priorState, config) order, we need to swap
	result, err := p.wrapped.UpdateResource(ctx, resourceType, currentState, properties)
	if err != nil {
		return nil, fmt.Errorf("update resource %s: %w", resourceType, err)
	}
	return result, nil
}

// DeleteResource deletes an existing resource
func (p *ConcreteProvider) DeleteResource(ctx context.Context, resourceType string, currentState map[string]interface{}) error {
	if err := p.wrapped.DeleteResource(ctx, resourceType, currentState); err != nil {
		return fmt.Errorf("delete resource %s: %w", resourceType, err)
	}
	return nil
}

// --- DataSourceReader implementation ---

// ReadDataSource reads data from a data source
func (p *ConcreteProvider) ReadDataSource(ctx context.Context, dataSourceType string, config map[string]interface{}) (map[string]interface{}, error) {
	result, err := p.wrapped.ReadDataSource(ctx, dataSourceType, config)
	if err != nil {
		return nil, fmt.Errorf("read data source %s: %w", dataSourceType, err)
	}
	return result, nil
}

// --- ResourcePlanner implementation ---

// PlanResourceChange plans changes to a resource
func (p *ConcreteProvider) PlanResourceChange(ctx context.Context, resourceType string, currentState, proposedState map[string]interface{}) (*interfaces.ResourcePlan, error) {
	// Use the wrapped provider's planning
	resp, err := p.wrapped.PlanResourceChange(ctx, resourceType, currentState, proposedState)
	if err != nil {
		return nil, fmt.Errorf("plan resource change for %s: %w", resourceType, err)
	}

	// Determine the action based on state and RequiresReplace
	var action interfaces.PlanAction
	switch {
	case len(currentState) == 0:
		action = interfaces.PlanActionCreate
	case len(proposedState) == 0:
		action = interfaces.PlanActionDelete
	case resp != nil && len(resp.RequiresReplace) > 0:
		// Replacement is required
		action = interfaces.PlanActionReplace
	default:
		action = interfaces.PlanActionUpdate
	}

	// Convert RequiresReplace attribute paths to strings
	var requiresReplace []string
	if resp != nil && len(resp.RequiresReplace) > 0 {
		requiresReplace = make([]string, 0, len(resp.RequiresReplace))
		for _, path := range resp.RequiresReplace {
			if path != nil {
				// Convert attribute path to string representation
				// For now, just use a simple string representation
				pathStr := path.String()
				requiresReplace = append(requiresReplace, pathStr)
			}
		}
	}

	return &interfaces.ResourcePlan{
		ResourceType:    resourceType,
		Action:          action,
		CurrentState:    currentState,
		ProposedState:   proposedState,
		PlannedState:    proposedState, // Simplified - should convert from resp.PlannedState
		RequiresReplace: requiresReplace,
	}, nil
}

// ApplyResourceChange applies planned changes to a resource
func (p *ConcreteProvider) ApplyResourceChange(ctx context.Context, resourceType string, plan *interfaces.ResourcePlan) (map[string]interface{}, error) {
	// The wrapped provider doesn't have ApplyResourceChange method
	// We need to implement the apply logic using the basic CRUD operations

	switch plan.Action {
	case interfaces.PlanActionCreate:
		result, err := p.wrapped.CreateResource(ctx, resourceType, plan.ProposedState)
		if err != nil {
			return nil, fmt.Errorf("apply create for %s: %w", resourceType, err)
		}
		return result, nil
	case interfaces.PlanActionUpdate:
		result, err := p.wrapped.UpdateResource(ctx, resourceType, plan.CurrentState, plan.ProposedState)
		if err != nil {
			return nil, fmt.Errorf("apply update for %s: %w", resourceType, err)
		}
		return result, nil
	case interfaces.PlanActionDelete:
		err := p.wrapped.DeleteResource(ctx, resourceType, plan.CurrentState)
		if err != nil {
			return nil, fmt.Errorf("apply delete for %s: %w", resourceType, err)
		}
		return nil, nil
	case interfaces.PlanActionReplace:
		// Delete then create
		if err := p.wrapped.DeleteResource(ctx, resourceType, plan.CurrentState); err != nil {
			return nil, fmt.Errorf("replace delete for %s: %w", resourceType, err)
		}
		result, err := p.wrapped.CreateResource(ctx, resourceType, plan.ProposedState)
		if err != nil {
			return nil, fmt.Errorf("replace create for %s: %w", resourceType, err)
		}
		return result, nil
	case interfaces.PlanActionNoop:
		// No operation needed
		return plan.CurrentState, nil
	default:
		// Unknown action - treat as no-op
		return plan.CurrentState, nil
	}
}

// --- UnifiedProvider interface implementation ---

// Configure configures the provider with the given configuration
func (p *ConcreteProvider) Configure(ctx context.Context, config map[string]interface{}) error {
	if err := p.wrapped.Configure(ctx, config); err != nil {
		return fmt.Errorf("configure provider: %w", err)
	}
	return nil
}

// Close closes the provider and releases resources
func (p *ConcreteProvider) Close() error {
	if err := p.wrapped.Close(); err != nil {
		return fmt.Errorf("close provider: %w", err)
	}
	return nil
}

// --- Concrete type methods ---
// These methods are available on the concrete type but not through the interface

// Name returns the provider name
func (p *ConcreteProvider) Name() string {
	return p.name
}

// Version returns the provider version
func (p *ConcreteProvider) Version() string {
	return p.version
}
