package protocol

import (
	"context"
	"fmt"

	"github.com/zclconf/go-cty/cty"
	ctymsgpack "github.com/zclconf/go-cty/cty/msgpack"

	tfplugin5 "github.com/lattiam/lattiam/internal/proto/tfplugin5"
	tfplugin6 "github.com/lattiam/lattiam/internal/proto/tfplugin6"
)

// TerraformRawState stores the raw provider state data
type TerraformRawState struct {
	StateData     []byte // Raw msgpack state from provider
	PrivateData   []byte // Provider's private data for refresh/destroy
	SchemaVersion int    // Schema version of the resource
}

// ResourceState contains both the processed state and raw Terraform state data
type ResourceState struct {
	State          map[string]interface{}
	TerraformState *TerraformRawState
}

// CreateWithState creates a resource and returns both processed and raw state
func (c *TerraformProviderClient) CreateWithState(
	ctx context.Context, config map[string]interface{},
) (*ResourceState, error) {
	// Get resource schema
	resourceSchema, ok := c.schema.ResourceSchemas[c.typeName]
	if !ok {
		return nil, fmt.Errorf("%w for %s", ErrResourceSchemaNotFound, c.typeName)
	}

	// Convert to cty.Value using schema
	configVal, err := c.mapToCtyValue(config, resourceSchema.Block)
	if err != nil {
		return nil, fmt.Errorf("failed to map config to cty.Value: %w", err)
	}

	// Get the implied type from schema
	resourceImpliedType := c.getImpliedType(resourceSchema.Block)

	// Marshal with type information
	configBytes, err := ctymsgpack.Marshal(configVal, resourceImpliedType)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal config: %w", err)
	}

	// Plan the change
	plannedBytes, privateData, err := c.planResourceChangeWithPrivate(ctx, nil, configBytes, configBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to plan resource change: %w", err)
	}

	// Apply the change
	newStateBytes, err := c.applyResourceChange(ctx, nil, plannedBytes, configBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to apply resource change: %w", err)
	}

	// Unmarshal the result for the processed state
	newStateVal, err := ctymsgpack.Unmarshal(newStateBytes, resourceImpliedType)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal new state: %w", err)
	}

	// Convert to map
	stateMap, err := c.ctyValueToMap(newStateVal)
	if err != nil {
		return nil, fmt.Errorf("failed to convert state to map: %w", err)
	}

	return &ResourceState{
		State: stateMap,
		TerraformState: &TerraformRawState{
			StateData:     newStateBytes,
			PrivateData:   privateData,
			SchemaVersion: int(resourceSchema.Version),
		},
	}, nil
}

// planResourceChangeWithPrivate plans a resource change and returns both planned state and private data
//
//nolint:gocognit // protocol version handling
func (c *TerraformProviderClient) planResourceChangeWithPrivate(
	ctx context.Context, priorState, proposedState, config []byte,
) (plannedState []byte, privateData []byte, err error) {
	switch c.protocolVersion {
	case 5: //nolint:dupl // Protocol-specific implementations
		req := &tfplugin5.PlanResourceChange_Request{
			TypeName: c.typeName,
			Config: &tfplugin5.DynamicValue{
				Msgpack: config,
			},
			ProposedNewState: &tfplugin5.DynamicValue{
				Msgpack: proposedState,
			},
		}
		if priorState != nil {
			req.PriorState = &tfplugin5.DynamicValue{
				Msgpack: priorState,
			}
		}

		resp, err := c.grpcClientV5.PlanResourceChange(ctx, req)
		if err != nil {
			return nil, nil, fmt.Errorf("plan error: %w", err)
		}

		// Check diagnostics
		if len(resp.GetDiagnostics()) > 0 {
			return nil, nil, fmt.Errorf("%w: %s", ErrPlanError, diagnosticsToErrorV5(resp.GetDiagnostics()))
		}

		if resp.GetPlannedState() == nil || resp.PlannedState.Msgpack == nil {
			return nil, nil, ErrPlanReturnedNoState
		}

		// Extract private data
		var privateData []byte
		if resp.PlannedPrivate != nil {
			privateData = resp.GetPlannedPrivate()
		}

		return resp.GetPlannedState().GetMsgpack(), privateData, nil

	case 6: //nolint:dupl // Protocol-specific implementations
		req := &tfplugin6.PlanResourceChange_Request{
			TypeName: c.typeName,
			Config: &tfplugin6.DynamicValue{
				Msgpack: config,
			},
			ProposedNewState: &tfplugin6.DynamicValue{
				Msgpack: proposedState,
			},
		}
		if priorState != nil {
			req.PriorState = &tfplugin6.DynamicValue{
				Msgpack: priorState,
			}
		}

		resp, err := c.grpcClientV6.PlanResourceChange(ctx, req)
		if err != nil {
			return nil, nil, fmt.Errorf("plan error: %w", err)
		}

		// Check diagnostics
		if len(resp.GetDiagnostics()) > 0 {
			return nil, nil, fmt.Errorf("%w: %s", ErrPlanError, diagnosticsToError(resp.GetDiagnostics()))
		}

		if resp.GetPlannedState() == nil || resp.PlannedState.Msgpack == nil {
			return nil, nil, ErrPlanReturnedNoState
		}

		// Extract private data
		var privateData []byte
		if resp.PlannedPrivate != nil {
			privateData = resp.GetPlannedPrivate()
		}

		return resp.GetPlannedState().GetMsgpack(), privateData, nil

	default:
		return nil, nil, fmt.Errorf("%w: %d", ErrUnsupportedProtocolVersion, c.protocolVersion)
	}
}

// DeleteWithState deletes a resource using the full state including private data
func (c *TerraformProviderClient) DeleteWithState(
	ctx context.Context,
	_ string,
	currentState map[string]interface{},
	terraformState *TerraformRawState,
) error {
	// Get resource schema
	resourceSchema, ok := c.schema.ResourceSchemas[c.typeName]
	if !ok {
		return fmt.Errorf("%w for %s", ErrResourceSchemaNotFound, c.typeName)
	}

	// If we have raw state data, use it directly
	var priorStateBytes []byte
	if terraformState != nil && len(terraformState.StateData) > 0 {
		priorStateBytes = terraformState.StateData
	} else {
		// Fallback to converting from map
		stateVal, err := c.mapToCtyValue(currentState, resourceSchema.Block)
		if err != nil {
			return fmt.Errorf("failed to map state to cty.Value: %w", err)
		}

		resourceImpliedType := c.getImpliedType(resourceSchema.Block)
		priorStateBytes, err = ctymsgpack.Marshal(stateVal, resourceImpliedType)
		if err != nil {
			return fmt.Errorf("failed to marshal state: %w", err)
		}
	}

	// Create empty proposed state (for deletion)
	emptyVal := cty.NullVal(c.getImpliedType(resourceSchema.Block))
	emptyBytes, err := ctymsgpack.Marshal(emptyVal, c.getImpliedType(resourceSchema.Block))
	if err != nil {
		return fmt.Errorf("failed to marshal empty state: %w", err)
	}

	// Plan the deletion with private data if available
	var plannedBytes []byte
	var privateData []byte
	if terraformState != nil && len(terraformState.PrivateData) > 0 {
		privateData = terraformState.PrivateData
	}

	plannedBytes, _, err = c.planResourceChangeWithPrivateData(
		ctx, priorStateBytes, emptyBytes, emptyBytes, privateData,
	)
	if err != nil {
		return fmt.Errorf("failed to plan resource deletion: %w", err)
	}

	// Apply the deletion
	_, err = c.applyResourceChangeWithPrivate(ctx, priorStateBytes, plannedBytes, emptyBytes, privateData)
	if err != nil {
		return fmt.Errorf("failed to apply resource deletion: %w", err)
	}

	return nil
}

// planResourceChangeWithPrivateData includes existing private data in the plan request
func (c *TerraformProviderClient) planResourceChangeWithPrivateData(
	ctx context.Context, priorState, proposedState, config, privateData []byte,
) (plannedState []byte, plannedPrivate []byte, err error) {
	switch c.protocolVersion {
	case 5:
		req := &tfplugin5.PlanResourceChange_Request{
			TypeName: c.typeName,
			Config: &tfplugin5.DynamicValue{
				Msgpack: config,
			},
			ProposedNewState: &tfplugin5.DynamicValue{
				Msgpack: proposedState,
			},
			PriorPrivate: privateData,
		}
		if priorState != nil {
			req.PriorState = &tfplugin5.DynamicValue{
				Msgpack: priorState,
			}
		}

		resp, err := c.grpcClientV5.PlanResourceChange(ctx, req)
		if err != nil {
			return nil, nil, fmt.Errorf("plan error: %w", err)
		}

		if len(resp.GetDiagnostics()) > 0 {
			return nil, nil, fmt.Errorf("%w: %s", ErrPlanError, diagnosticsToErrorV5(resp.GetDiagnostics()))
		}

		if resp.GetPlannedState() == nil || resp.PlannedState.Msgpack == nil {
			return nil, nil, ErrPlanReturnedNoState
		}

		return resp.GetPlannedState().GetMsgpack(), resp.GetPlannedPrivate(), nil

	case 6:
		req := &tfplugin6.PlanResourceChange_Request{
			TypeName: c.typeName,
			Config: &tfplugin6.DynamicValue{
				Msgpack: config,
			},
			ProposedNewState: &tfplugin6.DynamicValue{
				Msgpack: proposedState,
			},
			PriorPrivate: privateData,
		}
		if priorState != nil {
			req.PriorState = &tfplugin6.DynamicValue{
				Msgpack: priorState,
			}
		}

		resp, err := c.grpcClientV6.PlanResourceChange(ctx, req)
		if err != nil {
			return nil, nil, fmt.Errorf("plan error: %w", err)
		}

		if len(resp.GetDiagnostics()) > 0 {
			return nil, nil, fmt.Errorf("%w: %s", ErrPlanError, diagnosticsToError(resp.GetDiagnostics()))
		}

		if resp.GetPlannedState() == nil || resp.PlannedState.Msgpack == nil {
			return nil, nil, ErrPlanReturnedNoState
		}

		return resp.GetPlannedState().GetMsgpack(), resp.GetPlannedPrivate(), nil

	default:
		return nil, nil, fmt.Errorf("%w: %d", ErrUnsupportedProtocolVersion, c.protocolVersion)
	}
}

// applyResourceChangeWithPrivate includes private data in the apply request
func (c *TerraformProviderClient) applyResourceChangeWithPrivate(
	ctx context.Context, priorState, plannedState, config, privateData []byte,
) ([]byte, error) {
	switch c.protocolVersion {
	case 5:
		req := &tfplugin5.ApplyResourceChange_Request{
			TypeName: c.typeName,
			Config: &tfplugin5.DynamicValue{
				Msgpack: config,
			},
			PlannedState: &tfplugin5.DynamicValue{
				Msgpack: plannedState,
			},
			PlannedPrivate: privateData,
		}
		if priorState != nil {
			req.PriorState = &tfplugin5.DynamicValue{
				Msgpack: priorState,
			}
		}

		resp, err := c.grpcClientV5.ApplyResourceChange(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("apply error: %w", err)
		}

		if len(resp.GetDiagnostics()) > 0 {
			return nil, fmt.Errorf("%w: %s", ErrApplyError, diagnosticsToErrorV5(resp.GetDiagnostics()))
		}

		if resp.GetNewState() == nil || resp.NewState.Msgpack == nil {
			return nil, ErrApplyReturnedNoState
		}

		return resp.GetNewState().GetMsgpack(), nil

	case 6:
		req := &tfplugin6.ApplyResourceChange_Request{
			TypeName: c.typeName,
			Config: &tfplugin6.DynamicValue{
				Msgpack: config,
			},
			PlannedState: &tfplugin6.DynamicValue{
				Msgpack: plannedState,
			},
			PlannedPrivate: privateData,
		}
		if priorState != nil {
			req.PriorState = &tfplugin6.DynamicValue{
				Msgpack: priorState,
			}
		}

		resp, err := c.grpcClientV6.ApplyResourceChange(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("apply error: %w", err)
		}

		if len(resp.GetDiagnostics()) > 0 {
			return nil, fmt.Errorf("%w: %s", ErrApplyError, diagnosticsToError(resp.GetDiagnostics()))
		}

		if resp.GetNewState() == nil || resp.NewState.Msgpack == nil {
			return nil, ErrApplyReturnedNoState
		}

		return resp.GetNewState().GetMsgpack(), nil

	default:
		return nil, fmt.Errorf("%w: %d", ErrUnsupportedProtocolVersion, c.protocolVersion)
	}
}
