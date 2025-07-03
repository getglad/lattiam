package protocol

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/zclconf/go-cty/cty"
	ctymsgpack "github.com/zclconf/go-cty/cty/msgpack"

	tfplugin5 "github.com/lattiam/lattiam/internal/proto/tfplugin5"
	tfplugin6 "github.com/lattiam/lattiam/internal/proto/tfplugin6"
)

// Note: Using static errors from terraform_client.go for consistency

// TerraformCompatibleClient wraps the provider client to handle resource operations
// exactly like Terraform CLI does, avoiding provider crashes
type TerraformCompatibleClient struct {
	*TerraformProviderClient
}

// PlanResult holds the result of a plan operation
type PlanResult struct {
	PlannedState    []byte
	PlannedPrivate  []byte
	RequiresReplace []string // Attribute paths that require resource replacement
}

// ErrRequiresReplace indicates that a resource update requires replacement
type ErrRequiresReplace struct {
	Paths []string
}

func (e *ErrRequiresReplace) Error() string {
	return "resource update requires replacement due to changes in: " + strings.Join(e.Paths, ", ")
}

// NewTerraformCompatibleClient creates a client that mimics Terraform's behavior
func NewTerraformCompatibleClient(
	ctx context.Context, instance *ProviderInstance, typeName string,
) (*TerraformCompatibleClient, error) {
	client, err := NewTerraformProviderClient(ctx, instance, typeName)
	if err != nil {
		return nil, err
	}

	return &TerraformCompatibleClient{
		TerraformProviderClient: client,
	}, nil
}

// Create implements resource creation following Terraform's exact pattern
func (c *TerraformCompatibleClient) Create(
	ctx context.Context, config map[string]interface{},
) (map[string]interface{}, error) {
	// Get resource schema
	resourceSchema, ok := c.schema.ResourceSchemas[c.typeName]
	if !ok {
		return nil, fmt.Errorf("%w for %s", ErrResourceSchemaNotFound, c.typeName)
	}

	// 1. Create EmptyValue for the resource (like Terraform does)
	emptyVal := c.createEmptyValue(resourceSchema.Block)

	// 2. Convert config to cty.Value
	configVal, err := c.mapToCtyValue(config, resourceSchema.Block)
	if err != nil {
		return nil, fmt.Errorf("failed to convert config: %w", err)
	}

	// 3. Create ProposedNewState by merging empty value with config
	// This mimics Terraform's objchange.ProposedNew() behavior
	proposedNewVal := c.mergeWithDefaults(emptyVal, configVal, resourceSchema.Block)

	// 4. Marshal values for the provider
	resourceImpliedType := c.getImpliedType(resourceSchema.Block)

	// PriorState is null for creation (but must be a proper msgpack-encoded null)
	priorStateNull := cty.NullVal(resourceImpliedType)
	priorStateBytes, err := ctymsgpack.Marshal(priorStateNull, resourceImpliedType)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal null prior state: %w", err)
	}

	// ProposedNewState with all fields properly initialized
	proposedBytes, err := ctymsgpack.Marshal(proposedNewVal, resourceImpliedType)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal proposed state: %w", err)
	}

	// Config value
	configBytes, err := ctymsgpack.Marshal(configVal, resourceImpliedType)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal config: %w", err)
	}

	// 5. Call PlanResourceChange with proper request structure
	planResult, err := c.planResourceChangeWithProperRequest(ctx, priorStateBytes, configBytes, proposedBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to plan resource change: %w", err)
	}

	// Terraform ignores RequiresReplace during initial resource creation.
	// RequiresReplace is only meaningful during updates to indicate which field
	// changes require resource replacement. During create, we're already creating
	// a new resource, so the concept of replacement doesn't apply.
	// Some providers (like AWS v6) may still return RequiresReplace during create
	// to indicate which fields will be immutable after creation.
	if len(planResult.RequiresReplace) > 0 {
		log.Printf("Provider indicated fields that will require replacement on future updates for %s: %v", c.typeName, planResult.RequiresReplace)
	}

	// 6. Apply the change
	newStateBytes, err := c.applyResourceChangeWithPrivate(ctx, priorStateBytes, planResult.PlannedState, configBytes, planResult.PlannedPrivate)
	if err != nil {
		return nil, fmt.Errorf("failed to apply resource change: %w", err)
	}

	// 7. Unmarshal result
	newStateVal, err := ctymsgpack.Unmarshal(newStateBytes, resourceImpliedType)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal new state: %w", err)
	}

	return c.ctyValueToMap(newStateVal)
}

// createEmptyValue creates an empty value matching Terraform's configschema.Block.EmptyValue()
func (c *TerraformCompatibleClient) createEmptyValue(schema *tfprotov6.SchemaBlock) cty.Value {
	vals := make(map[string]cty.Value)

	// All attributes start as null
	for _, attr := range schema.Attributes {
		vals[attr.Name] = cty.NullVal(TftypesToCtyType(attr.Type))
	}

	// Blocks get empty collections based on nesting
	for _, block := range schema.BlockTypes {
		blockImpliedType := c.getImpliedType(block.Block)

		switch block.Nesting {
		case tfprotov6.SchemaNestedBlockNestingModeSingle:
			vals[block.TypeName] = cty.NullVal(blockImpliedType)
		case tfprotov6.SchemaNestedBlockNestingModeList:
			vals[block.TypeName] = cty.ListValEmpty(blockImpliedType)
		case tfprotov6.SchemaNestedBlockNestingModeSet:
			vals[block.TypeName] = cty.SetValEmpty(blockImpliedType)
		case tfprotov6.SchemaNestedBlockNestingModeMap:
			vals[block.TypeName] = cty.MapValEmpty(blockImpliedType)
		case tfprotov6.SchemaNestedBlockNestingModeInvalid:
			vals[block.TypeName] = cty.ListValEmpty(blockImpliedType) // Fallback for invalid
		case tfprotov6.SchemaNestedBlockNestingModeGroup:
			vals[block.TypeName] = cty.ListValEmpty(blockImpliedType) // Fallback for group
		default:
			vals[block.TypeName] = cty.ListValEmpty(blockImpliedType)
		}
	}

	return cty.ObjectVal(vals)
}

// mergeWithDefaults merges config values into empty schema, setting defaults for computed values
func (c *TerraformCompatibleClient) mergeWithDefaults(
	emptyVal, configVal cty.Value, schema *tfprotov6.SchemaBlock,
) cty.Value {
	if !emptyVal.Type().IsObjectType() || !configVal.Type().IsObjectType() {
		return configVal
	}

	result := make(map[string]cty.Value)

	// Start with empty value structure
	for name, val := range emptyVal.AsValueMap() {
		result[name] = val
	}

	// Override with config values
	for name, val := range configVal.AsValueMap() {
		if !val.IsNull() {
			result[name] = val
		}
	}

	// For computed attributes that aren't set, mark as unknown (like Terraform does)
	for _, attr := range schema.Attributes {
		if attr.Computed && !attr.Optional {
			if val, ok := result[attr.Name]; ok && val.IsNull() {
				result[attr.Name] = cty.UnknownVal(TftypesToCtyType(attr.Type))
			}
		}
	}

	return cty.ObjectVal(result)
}

// planResourceChangeWithProperRequest ensures all request fields are properly initialized
func (c *TerraformCompatibleClient) planResourceChangeWithProperRequest(
	ctx context.Context, priorStateBytes, configBytes, proposedBytes []byte,
) (*PlanResult, error) {
	// Add a timeout to the context for this specific gRPC call
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	switch c.protocolVersion {
	case 5:
		return c.planResourceChangeV5(ctx, priorStateBytes, configBytes, proposedBytes)
	case 6:
		return c.planResourceChangeV6(ctx, priorStateBytes, configBytes, proposedBytes)
	default:
		return nil, fmt.Errorf("%w: %d", ErrUnsupportedProtocolVersion, c.protocolVersion)
	}
}

// Update implements resource update following Terraform's pattern
func (c *TerraformCompatibleClient) Update(
	ctx context.Context, currentState map[string]interface{}, newConfig map[string]interface{},
) (map[string]interface{}, error) {
	// Get resource schema
	resourceSchema, ok := c.schema.ResourceSchemas[c.typeName]
	if !ok {
		return nil, fmt.Errorf("%w for %s", ErrResourceSchemaNotFound, c.typeName)
	}

	resourceImpliedType := c.getImpliedType(resourceSchema.Block)

	// 1. Convert current state to cty.Value
	currentStateVal, err := c.mapToCtyValue(currentState, resourceSchema.Block)
	if err != nil {
		return nil, fmt.Errorf("failed to convert current state: %w", err)
	}

	// 2. Convert new config to cty.Value
	newConfigVal, err := c.mapToCtyValue(newConfig, resourceSchema.Block)
	if err != nil {
		return nil, fmt.Errorf("failed to convert new config: %w", err)
	}

	// 3. Create proposed new state by merging current state with new config
	// This preserves computed values from current state while applying config changes
	proposedNewVal := c.mergeWithDefaults(currentStateVal, newConfigVal, resourceSchema.Block)

	// 4. Marshal values for the provider
	currentStateBytes, err := ctymsgpack.Marshal(currentStateVal, resourceImpliedType)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal current state: %w", err)
	}

	proposedBytes, err := ctymsgpack.Marshal(proposedNewVal, resourceImpliedType)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal proposed state: %w", err)
	}

	configBytes, err := ctymsgpack.Marshal(newConfigVal, resourceImpliedType)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal config: %w", err)
	}

	// 5. Plan the update
	planResult, err := c.planResourceChangeWithProperRequest(ctx, currentStateBytes, configBytes, proposedBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to plan resource update: %w", err)
	}

	// Check if replacement is required
	if len(planResult.RequiresReplace) > 0 {
		// Need to check if any of the RequiresReplace paths have actually changed
		needsReplace := false
		for _, path := range planResult.RequiresReplace {
			if c.hasAttributeChanged(currentStateVal, newConfigVal, path) {
				needsReplace = true
				break
			}
		}

		if needsReplace {
			return nil, &ErrRequiresReplace{
				Paths: planResult.RequiresReplace,
			}
		}
	}

	// 6. Apply the update
	newStateBytes, err := c.applyResourceChangeWithPrivate(ctx, currentStateBytes, planResult.PlannedState, configBytes, planResult.PlannedPrivate)
	if err != nil {
		return nil, fmt.Errorf("failed to apply resource update: %w", err)
	}

	// 7. Unmarshal result
	newStateVal, err := ctymsgpack.Unmarshal(newStateBytes, resourceImpliedType)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal new state: %w", err)
	}

	return c.ctyValueToMap(newStateVal)
}

// Delete implements resource deletion following Terraform's pattern
func (c *TerraformCompatibleClient) Delete(
	ctx context.Context, _ string, currentState map[string]interface{},
) error {
	// Get resource schema
	resourceSchema, ok := c.schema.ResourceSchemas[c.typeName]
	if !ok {
		return fmt.Errorf("%w for %s", ErrResourceSchemaNotFound, c.typeName)
	}

	resourceImpliedType := c.getImpliedType(resourceSchema.Block)

	// 1. Convert current state to cty.Value
	currentStateVal, err := c.mapToCtyValue(currentState, resourceSchema.Block)
	if err != nil {
		return fmt.Errorf("failed to convert current state: %w", err)
	}

	// 2. Marshal current state
	currentStateBytes, err := ctymsgpack.Marshal(currentStateVal, resourceImpliedType)
	if err != nil {
		return fmt.Errorf("failed to marshal current state: %w", err)
	}

	// 3. For deletion, proposed new state is null
	proposedNewNull := cty.NullVal(resourceImpliedType)
	proposedNewBytes, err := ctymsgpack.Marshal(proposedNewNull, resourceImpliedType)
	if err != nil {
		return fmt.Errorf("failed to marshal null proposed state: %w", err)
	}

	// 4. Config is also null for deletion
	configNull := cty.NullVal(resourceImpliedType)
	configBytes, err := ctymsgpack.Marshal(configNull, resourceImpliedType)
	if err != nil {
		return fmt.Errorf("failed to marshal null config: %w", err)
	}

	// 5. Plan the deletion
	planResult, err := c.planResourceChangeWithProperRequest(ctx, currentStateBytes, configBytes, proposedNewBytes)
	if err != nil {
		return fmt.Errorf("failed to plan resource deletion: %w", err)
	}

	// 6. Apply the deletion
	_, err = c.applyResourceChangeWithPrivate(ctx, currentStateBytes, planResult.PlannedState, configBytes, planResult.PlannedPrivate)
	if err != nil {
		return fmt.Errorf("failed to apply resource deletion: %w", err)
	}

	return nil
}

// attributePathToStringV5 converts a v5 AttributePath to a string representation
func attributePathToStringV5(path *tfplugin5.AttributePath) string {
	if path == nil {
		return ""
	}

	var parts []string
	for _, step := range path.GetSteps() {
		switch s := step.GetSelector().(type) {
		case *tfplugin5.AttributePath_Step_AttributeName:
			parts = append(parts, s.AttributeName)
		case *tfplugin5.AttributePath_Step_ElementKeyString:
			parts = append(parts, fmt.Sprintf("[%q]", s.ElementKeyString))
		case *tfplugin5.AttributePath_Step_ElementKeyInt:
			parts = append(parts, fmt.Sprintf("[%d]", s.ElementKeyInt))
		}
	}

	return strings.Join(parts, ".")
}

// attributePathToStringV6 converts a v6 AttributePath to a string representation
func attributePathToStringV6(path *tfplugin6.AttributePath) string {
	if path == nil {
		return ""
	}

	var parts []string
	for _, step := range path.GetSteps() {
		switch s := step.GetSelector().(type) {
		case *tfplugin6.AttributePath_Step_AttributeName:
			parts = append(parts, s.AttributeName)
		case *tfplugin6.AttributePath_Step_ElementKeyString:
			parts = append(parts, fmt.Sprintf("[%q]", s.ElementKeyString))
		case *tfplugin6.AttributePath_Step_ElementKeyInt:
			parts = append(parts, fmt.Sprintf("[%d]", s.ElementKeyInt))
		}
	}

	return strings.Join(parts, ".")
}

// hasAttributeChanged checks if a specific attribute has changed between two values
func (c *TerraformCompatibleClient) hasAttributeChanged(oldVal, newVal cty.Value, path string) bool {
	// Parse the path
	parts := strings.Split(path, ".")

	// Navigate through both values
	oldCurrent := oldVal
	newCurrent := newVal

	for _, part := range parts {
		// Handle attribute access
		if oldCurrent.Type().IsObjectType() && newCurrent.Type().IsObjectType() {
			oldAttrs := oldCurrent.AsValueMap()
			newAttrs := newCurrent.AsValueMap()

			oldCurrent = oldAttrs[part]
			newCurrent = newAttrs[part]
		} else {
			// If we can't navigate further, assume changed
			return true
		}
	}

	// Compare the final values
	return !oldCurrent.Equals(newCurrent).True()
}

// ReadDataSource implements data source reading following Terraform's pattern
func (c *TerraformCompatibleClient) ReadDataSource(
	ctx context.Context, config map[string]interface{},
) (map[string]interface{}, error) {
	// Get data source schema
	dataSourceSchema, ok := c.schema.DataSourceSchemas[c.typeName]
	if !ok {
		return nil, fmt.Errorf("data source schema not found for %s", c.typeName)
	}

	// Convert config to cty.Value
	configVal, err := c.mapToCtyValue(config, dataSourceSchema.Block)
	if err != nil {
		return nil, fmt.Errorf("failed to convert config: %w", err)
	}

	// Marshal config for the provider
	dataSourceImpliedType := c.getImpliedType(dataSourceSchema.Block)
	configBytes, err := ctymsgpack.Marshal(configVal, dataSourceImpliedType)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal config: %w", err)
	}

	// Call ReadDataSource RPC
	switch c.protocolVersion {
	case 5:
		return c.readDataSourceV5(ctx, configBytes, dataSourceImpliedType)
	case 6:
		return c.readDataSourceV6(ctx, configBytes, dataSourceImpliedType)
	default:
		return nil, fmt.Errorf("%w: %d", ErrUnsupportedProtocolVersion, c.protocolVersion)
	}
}

// readDataSourceV5 handles protocol v5 data source reading
func (c *TerraformCompatibleClient) readDataSourceV5(
	ctx context.Context, configBytes []byte, dataSourceImpliedType cty.Type,
) (map[string]interface{}, error) {
	req := &tfplugin5.ReadDataSource_Request{
		TypeName: c.typeName,
		Config: &tfplugin5.DynamicValue{
			Msgpack: configBytes,
		},
	}

	resp, err := c.grpcClientV5.ReadDataSource(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("read data source error: %w", err)
	}

	if len(resp.GetDiagnostics()) > 0 {
		return nil, fmt.Errorf("read data source error: %s", diagnosticsToErrorV5(resp.GetDiagnostics()))
	}

	if resp.GetState() == nil || resp.State.Msgpack == nil {
		return nil, errors.New("read data source returned no state")
	}

	return c.unmarshalDataSourceState(resp.GetState().GetMsgpack(), dataSourceImpliedType)
}

// readDataSourceV6 handles protocol v6 data source reading
func (c *TerraformCompatibleClient) readDataSourceV6(
	ctx context.Context, configBytes []byte, dataSourceImpliedType cty.Type,
) (map[string]interface{}, error) {
	req := &tfplugin6.ReadDataSource_Request{
		TypeName: c.typeName,
		Config: &tfplugin6.DynamicValue{
			Msgpack: configBytes,
		},
	}

	resp, err := c.grpcClientV6.ReadDataSource(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("read data source error: %w", err)
	}

	if len(resp.GetDiagnostics()) > 0 {
		return nil, fmt.Errorf("read data source error: %s", diagnosticsToError(resp.GetDiagnostics()))
	}

	if resp.GetState() == nil || resp.State.Msgpack == nil {
		return nil, errors.New("read data source returned no state")
	}

	return c.unmarshalDataSourceState(resp.GetState().GetMsgpack(), dataSourceImpliedType)
}

// unmarshalDataSourceState is a helper function to unmarshal data source state
func (c *TerraformCompatibleClient) unmarshalDataSourceState(
	msgpack []byte, dataSourceImpliedType cty.Type,
) (map[string]interface{}, error) {
	stateVal, err := ctymsgpack.Unmarshal(msgpack, dataSourceImpliedType)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal state: %w", err)
	}

	return c.ctyValueToMap(stateVal)
}

// planResourceChangeV5 handles protocol v5 plan resource change
//
//nolint:dupl // Protocol-specific types require separate implementations
func (c *TerraformCompatibleClient) planResourceChangeV5(
	ctx context.Context, priorStateBytes, configBytes, proposedBytes []byte,
) (*PlanResult, error) {
	req := &tfplugin5.PlanResourceChange_Request{
		TypeName: c.typeName,
		PriorState: &tfplugin5.DynamicValue{
			Msgpack: priorStateBytes,
		},
		Config: &tfplugin5.DynamicValue{
			Msgpack: configBytes,
		},
		ProposedNewState: &tfplugin5.DynamicValue{
			Msgpack: proposedBytes,
		},
		// PriorPrivate can be nil for resources being created
		PriorPrivate: []byte{},
	}

	resp, err := c.grpcClientV5.PlanResourceChange(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("plan error: %w", err)
	}

	if len(resp.GetDiagnostics()) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrPlanError, diagnosticsToErrorV5(resp.GetDiagnostics()))
	}

	if resp.GetPlannedState() == nil || resp.PlannedState.Msgpack == nil {
		return nil, ErrPlanReturnedNoState
	}

	// Convert attribute paths to strings
	requiresReplace := make([]string, 0, len(resp.GetRequiresReplace()))
	for _, path := range resp.GetRequiresReplace() {
		requiresReplace = append(requiresReplace, attributePathToStringV5(path))
	}

	return &PlanResult{
		PlannedState:    resp.GetPlannedState().GetMsgpack(),
		PlannedPrivate:  resp.GetPlannedPrivate(),
		RequiresReplace: requiresReplace,
	}, nil
}

// planResourceChangeV6 handles protocol v6 plan resource change
//
//nolint:dupl // Protocol-specific types require separate implementations
func (c *TerraformCompatibleClient) planResourceChangeV6(
	ctx context.Context, priorStateBytes, configBytes, proposedBytes []byte,
) (*PlanResult, error) {
	req := &tfplugin6.PlanResourceChange_Request{
		TypeName: c.typeName,
		PriorState: &tfplugin6.DynamicValue{
			Msgpack: priorStateBytes,
		},
		Config: &tfplugin6.DynamicValue{
			Msgpack: configBytes,
		},
		ProposedNewState: &tfplugin6.DynamicValue{
			Msgpack: proposedBytes,
		},
		PriorPrivate: []byte{},
	}

	resp, err := c.grpcClientV6.PlanResourceChange(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("plan error: %w", err)
	}

	if len(resp.GetDiagnostics()) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrPlanError, diagnosticsToError(resp.GetDiagnostics()))
	}

	if resp.GetPlannedState() == nil || resp.PlannedState.Msgpack == nil {
		return nil, ErrPlanReturnedNoState
	}

	// Convert attribute paths to strings
	requiresReplace := make([]string, 0, len(resp.GetRequiresReplace()))
	for _, path := range resp.GetRequiresReplace() {
		requiresReplace = append(requiresReplace, attributePathToStringV6(path))
	}

	return &PlanResult{
		PlannedState:    resp.GetPlannedState().GetMsgpack(),
		PlannedPrivate:  resp.GetPlannedPrivate(),
		RequiresReplace: requiresReplace,
	}, nil
}
