package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/zclconf/go-cty/cty"
	ctyjson "github.com/zclconf/go-cty/cty/json"
	ctymsgpack "github.com/zclconf/go-cty/cty/msgpack"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/lattiam/lattiam/internal/proto/tfplugin5"
	"github.com/lattiam/lattiam/internal/proto/tfplugin6"
)

// Static errors for err113 compliance
var (
	ErrUnsupportedProtocolVersion   = errors.New("unsupported protocol version")
	ErrProviderSchemaNotAvailable   = errors.New("provider schema not available")
	ErrPrepareConfigError           = errors.New("prepare config error")
	ErrConfigureError               = errors.New("configure error")
	ErrResourceSchemaNotFound       = errors.New("resource schema not found")
	ErrPlanError                    = errors.New("plan error")
	ErrApplyError                   = errors.New("apply error")
	ErrPlanReturnedNoState          = errors.New("plan returned no planned state")
	ErrApplyReturnedNoState         = errors.New("apply returned no new state")
	ErrRequiredAttributeNotProvided = errors.New("required attribute not provided")
	ErrFailedToConvertAttribute     = errors.New("failed to convert attribute")
	ErrFailedToProcessBlock         = errors.New("failed to process block")
	ErrCannotConvertMapToNonMap     = errors.New("cannot convert map to non-map type")
	ErrUnableToParseProviderAddress = errors.New("unable to parse provider address")
)

// TerraformProviderClient handles communication with Terraform providers using proper type encoding
type TerraformProviderClient struct {
	conn            *grpc.ClientConn
	protocolVersion int
	grpcClientV5    tfplugin5.ProviderClient
	grpcClientV6    tfplugin6.ProviderClient
	name            string
	typeName        string
	schema          *tfprotov6.GetProviderSchemaResponse // Cached schema
}

// NewTerraformProviderClient creates a client with proper type handling for Terraform providers
func NewTerraformProviderClient(
	ctx context.Context, instance *ProviderInstance, typeName string,
) (*TerraformProviderClient, error) {
	debugLogger := GetDebugLogger()
	debugLogger.Logf("client", "Creating client for provider %s, resource type %s", instance.Name, typeName)
	// Parse the provider's address output with version
	protocolVersion, network, address, err := parseProviderAddressWithVersion(instance.address)
	if err != nil {
		return nil, fmt.Errorf("failed to parse provider address: %w", err)
	}

	// Build the dial address
	var dialAddr string
	if network == UnixNetwork {
		dialAddr = "unix://" + address
	} else {
		dialAddr = address
	}

	log.Printf("Connecting to provider at %s using protocol v%d for resource type %s", dialAddr, protocolVersion, typeName)

	// Connect to the provider
	conn, err := grpc.NewClient(
		dialAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(MaxGRPCRecvMsgSize)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to provider: %w", err)
	}

	client := &TerraformProviderClient{
		conn:            conn,
		protocolVersion: protocolVersion,
		name:            instance.Name,
		typeName:        typeName,
	}

	// Create appropriate client based on protocol version
	switch protocolVersion {
	case 5:
		client.grpcClientV5 = tfplugin5.NewProviderClient(conn)
	case 6:
		client.grpcClientV6 = tfplugin6.NewProviderClient(conn)
	default:
		_ = conn.Close()
		return nil, fmt.Errorf("%w: %d", ErrUnsupportedProtocolVersion, protocolVersion)
	}

	// Get and cache schema
	schema, err := client.GetProviderSchema(ctx)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("failed to get provider schema: %w", err)
	}
	client.schema = schema

	return client, nil
}

// GetProviderSchema retrieves the provider's schema
func (c *TerraformProviderClient) GetProviderSchema(ctx context.Context) (*tfprotov6.GetProviderSchemaResponse, error) {
	if c.schema != nil {
		return c.schema, nil
	}

	switch c.protocolVersion {
	case 5:
		return c.getProviderSchemaV5(ctx)
	case 6:
		return c.getProviderSchemaV6(ctx)
	default:
		return nil, fmt.Errorf("%w: %d", ErrUnsupportedProtocolVersion, c.protocolVersion)
	}
}

// PrepareProviderConfig prepares the provider configuration with proper type encoding
func (c *TerraformProviderClient) PrepareProviderConfig(
	ctx context.Context, config map[string]interface{},
) (map[string]interface{}, error) {
	// Get provider schema
	if c.schema == nil || c.schema.Provider == nil {
		return nil, ErrProviderSchemaNotAvailable
	}

	// Convert to cty.Value using schema
	configVal, err := c.mapToCtyValue(config, c.schema.Provider.Block)
	if err != nil {
		return nil, fmt.Errorf("failed to convert config to cty value: %w", err)
	}

	// Marshal with type information
	impliedType := c.getImpliedType(c.schema.Provider.Block)
	configBytes, err := ctymsgpack.Marshal(configVal, impliedType)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal config: %w", err)
	}

	switch c.protocolVersion {
	case 5:
		req := &tfplugin5.PrepareProviderConfig_Request{
			Config: &tfplugin5.DynamicValue{
				Msgpack: configBytes,
			},
		}
		resp, err := c.grpcClientV5.PrepareProviderConfig(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("prepare config error: %w", err)
		}
		// Check diagnostics
		if len(resp.GetDiagnostics()) > 0 {
			return nil, fmt.Errorf("%w: %s", ErrPrepareConfigError, diagnosticsToErrorV5(resp.GetDiagnostics()))
		}

	case 6:
		req := &tfplugin6.ValidateProviderConfig_Request{
			Config: &tfplugin6.DynamicValue{
				Msgpack: configBytes,
			},
		}
		resp, err := c.grpcClientV6.ValidateProviderConfig(ctx, req)
		if err != nil {
			return nil, fmt.Errorf("prepare config error: %w", err)
		}
		// Check diagnostics
		if len(resp.GetDiagnostics()) > 0 {
			return nil, fmt.Errorf("%w: %s", ErrPrepareConfigError, diagnosticsToError(resp.GetDiagnostics()))
		}
	}

	return config, nil
}

// ConfigureProvider configures the provider with proper type encoding
func (c *TerraformProviderClient) ConfigureProvider(ctx context.Context, config map[string]interface{}) error {
	// Get provider schema
	if c.schema == nil || c.schema.Provider == nil {
		return ErrProviderSchemaNotAvailable
	}

	// Convert to cty.Value using schema
	configVal, err := c.mapToCtyValue(config, c.schema.Provider.Block)
	if err != nil {
		return fmt.Errorf("failed to convert config to cty value: %w", err)
	}

	// Marshal configuration
	configBytes, err := c.marshalProviderConfig(configVal, config)
	if err != nil {
		return err
	}

	// Save debug data
	c.saveDebugData("provider-config-msgpack", configBytes)

	// Send configuration to provider
	return c.sendConfigureRequest(ctx, configBytes)
}

// marshalProviderConfig marshals the provider configuration with type information
func (c *TerraformProviderClient) marshalProviderConfig(configVal cty.Value, config map[string]interface{}) ([]byte, error) {
	impliedType := c.getImpliedType(c.schema.Provider.Block)
	c.logConfigDetails(config, configVal, impliedType)

	configBytes, err := ctymsgpack.Marshal(configVal, impliedType)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal config: %w", err)
	}
	log.Printf("Client: ConfigureProvider - successfully marshaled %d bytes", len(configBytes))
	return configBytes, nil
}

// logConfigDetails logs configuration details for debugging
func (c *TerraformProviderClient) logConfigDetails(config map[string]interface{}, configVal cty.Value, impliedType cty.Type) {
	log.Printf("Client: ConfigureProvider - marshaling provider config with %d attributes", len(config))

	// Log specific attributes
	importantAttrs := []string{"profile", "region", "skip_requesting_account_id", "skip_credentials_validation", "allowed_account_ids", "access_key"}
	for k, v := range config {
		for _, attr := range importantAttrs {
			if k == attr {
				log.Printf("  %s: %v (type: %T)", k, v, v)
				break
			}
		}
	}

	// Debug: Print the actual cty.Value attributes
	c.validateConfigValue(configVal, impliedType)
}

// validateConfigValue validates and logs configuration value details
func (c *TerraformProviderClient) validateConfigValue(configVal cty.Value, impliedType cty.Type) {
	if !configVal.Type().IsObjectType() {
		log.Printf("Client: ERROR - configVal is not an object type: %s", configVal.Type().FriendlyName())
		return
	}

	valType := configVal.Type()
	log.Printf("Client: configVal has %d attributes", len(valType.AttributeTypes()))
	impliedAttrs := impliedType.AttributeTypes()
	log.Printf("Client: impliedType has %d attributes", len(impliedAttrs))

	// Check for missing attributes
	for name := range impliedAttrs {
		if !valType.HasAttribute(name) {
			log.Printf("Client: ERROR - missing attribute in configVal: %s", name)
		}
	}
}

// saveDebugData saves data for debugging if debug mode is enabled
func (c *TerraformProviderClient) saveDebugData(name string, data []byte) {
	debugLogger := GetDebugLogger()
	if err := debugLogger.SaveData(name, data); err != nil {
		log.Printf("Failed to save debug data: %v", err)
	}
}

// sendConfigureRequest sends the configuration request to the provider
func (c *TerraformProviderClient) sendConfigureRequest(ctx context.Context, configBytes []byte) error {
	switch c.protocolVersion {
	case 5:
		return c.sendConfigureRequestV5(ctx, configBytes)
	case 6:
		return c.sendConfigureRequestV6(ctx, configBytes)
	default:
		return fmt.Errorf("%w: %d", ErrUnsupportedProtocolVersion, c.protocolVersion)
	}
}

// sendConfigureRequestV5 handles protocol version 5 configure request
func (c *TerraformProviderClient) sendConfigureRequestV5(ctx context.Context, configBytes []byte) error {
	req := &tfplugin5.Configure_Request{
		Config: &tfplugin5.DynamicValue{
			Msgpack: configBytes,
		},
	}

	resp, err := c.grpcClientV5.Configure(ctx, req)
	if err != nil {
		return fmt.Errorf("configure error: %w", err)
	}

	return c.handleConfigureDiagnosticsV5(resp.GetDiagnostics())
}

// sendConfigureRequestV6 handles protocol version 6 configure request
func (c *TerraformProviderClient) sendConfigureRequestV6(ctx context.Context, configBytes []byte) error {
	req := &tfplugin6.ConfigureProvider_Request{
		Config: &tfplugin6.DynamicValue{
			Msgpack: configBytes,
		},
	}

	resp, err := c.grpcClientV6.ConfigureProvider(ctx, req)
	if err != nil {
		return fmt.Errorf("configure error: %w", err)
	}

	return c.handleConfigureDiagnosticsV6(resp.GetDiagnostics())
}

// handleConfigureDiagnosticsV5 processes diagnostics for protocol v5
func (c *TerraformProviderClient) handleConfigureDiagnosticsV5(diags []*tfplugin5.Diagnostic) error {
	if len(diags) == 0 {
		return nil
	}

	for _, diag := range diags {
		if diag.GetSeverity() == tfplugin5.Diagnostic_ERROR {
			return fmt.Errorf("%w: %s", ErrConfigureError, diagnosticsToErrorV5(diags))
		}
	}

	// Only warnings, log them but don't fail
	log.Printf("Provider configuration warnings: %s", diagnosticsToErrorV5(diags))
	return nil
}

// handleConfigureDiagnosticsV6 processes diagnostics for protocol v6
func (c *TerraformProviderClient) handleConfigureDiagnosticsV6(diags []*tfplugin6.Diagnostic) error {
	if len(diags) == 0 {
		return nil
	}

	for _, diag := range diags {
		if diag.GetSeverity() == tfplugin6.Diagnostic_ERROR {
			return fmt.Errorf("%w: %s", ErrConfigureError, diagnosticsToError(diags))
		}
	}

	// Only warnings, log them but don't fail
	log.Printf("Provider configuration warnings: %s", diagnosticsToError(diags))
	return nil
}

// Create creates a resource with proper type encoding
func (c *TerraformProviderClient) Create(
	ctx context.Context, config map[string]interface{},
) (map[string]interface{}, error) {
	// Get resource schema
	resourceSchema, ok := c.schema.ResourceSchemas[c.typeName]
	if !ok {
		return nil, fmt.Errorf("%w for %s", ErrResourceSchemaNotFound, c.typeName)
	}

	// Log schema attributes for debugging
	log.Printf("Client: Resource schema for %s has %d attributes", c.typeName, len(resourceSchema.Block.Attributes))
	for _, attr := range resourceSchema.Block.Attributes {
		typeStr := "unknown"
		if attr.Type != nil {
			typeStr = attr.Type.String()
		}
		log.Printf("  - %s: %s (required=%v, optional=%v)", attr.Name, typeStr, attr.Required, attr.Optional)
	}

	// Convert to cty.Value using schema
	log.Printf("Client: Converting config to cty.Value for resource %s", c.typeName)
	for k, v := range config {
		log.Printf("  %s: %T = %v", k, v, v)
	}
	configVal, err := c.mapToCtyValue(config, resourceSchema.Block)
	if err != nil {
		return nil, fmt.Errorf("failed to convert config to cty value: %w", err)
	}
	log.Printf("Client: Successfully converted to cty.Value")

	// Marshal with type information
	resourceImpliedType := c.getImpliedType(resourceSchema.Block)
	log.Printf("Client: Marshaling config with proper type information")
	log.Printf("Client: Config value type: %s", configVal.Type().FriendlyName())
	log.Printf("Client: Resource implied type: %s", resourceImpliedType.FriendlyName())

	// Debug: Check if all attributes are present
	objType := configVal.Type()
	log.Printf("Client: Config has %d attributes", len(objType.AttributeTypes()))
	for name := range objType.AttributeTypes() {
		val := configVal.GetAttr(name)
		log.Printf("  - %s: %s (null=%v, unknown=%v)", name, val.Type().FriendlyName(), val.IsNull(), !val.IsKnown())
	}

	configBytes, err := ctymsgpack.Marshal(configVal, resourceImpliedType)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal config: %w", err)
	}
	log.Printf("Client: Successfully marshaled %d bytes", len(configBytes))

	// Save debug data
	debugLogger := GetDebugLogger()
	if err := debugLogger.SaveData("resource-config-msgpack-"+c.typeName, configBytes); err != nil {
		log.Printf("Failed to save debug data: %v", err)
	}

	// For creation, proposedNewState is typically the same as config
	// The provider will fill in computed values during planning
	// Plan the change
	plannedBytes, err := c.planResourceChange(ctx, nil, configBytes, configBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to plan resource change: %w", err)
	}

	// Apply the change
	newStateBytes, err := c.applyResourceChange(ctx, nil, plannedBytes, configBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to apply resource change: %w", err)
	}

	// Unmarshal the result
	newStateVal, err := ctymsgpack.Unmarshal(newStateBytes, resourceImpliedType)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal new state: %w", err)
	}

	// Convert back to map
	return c.ctyValueToMap(newStateVal)
}

// planResourceChange plans a resource change with proper type encoding
func (c *TerraformProviderClient) planResourceChange(
	ctx context.Context, priorState, proposedState, config []byte,
) ([]byte, error) {
	log.Printf("Client: PlanResourceChange - starting with config size %d bytes", len(config))
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
		}
		if priorState != nil {
			req.PriorState = &tfplugin5.DynamicValue{
				Msgpack: priorState,
			}
		}

		log.Printf("Client: Sending PlanResourceChange request for %s", c.typeName)
		resp, err := c.grpcClientV5.PlanResourceChange(ctx, req)
		if err != nil {
			log.Printf("Client: PlanResourceChange failed: %v", err)
			return nil, fmt.Errorf("plan error: %w", err)
		}

		// Check diagnostics
		if len(resp.GetDiagnostics()) > 0 {
			return nil, fmt.Errorf("%w: %s", ErrPlanError, diagnosticsToErrorV5(resp.GetDiagnostics()))
		}

		if resp.GetPlannedState() == nil || resp.PlannedState.Msgpack == nil {
			return nil, ErrPlanReturnedNoState
		}

		return resp.GetPlannedState().GetMsgpack(), nil

	case 6:
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
			return nil, fmt.Errorf("plan error: %w", err)
		}

		// Check diagnostics
		if len(resp.GetDiagnostics()) > 0 {
			return nil, fmt.Errorf("%w: %s", ErrPlanError, diagnosticsToError(resp.GetDiagnostics()))
		}

		if resp.GetPlannedState() == nil || resp.PlannedState.Msgpack == nil {
			return nil, ErrPlanReturnedNoState
		}

		return resp.GetPlannedState().GetMsgpack(), nil

	default:
		return nil, fmt.Errorf("%w: %d", ErrUnsupportedProtocolVersion, c.protocolVersion)
	}
}

// applyResourceChange applies a resource change with proper type encoding
func (c *TerraformProviderClient) applyResourceChange(
	ctx context.Context, priorState, plannedState, config []byte,
) ([]byte, error) {
	// Add a timeout to the context for this specific gRPC call
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute) // Generous timeout for resource creation
	defer cancel()

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

		// Check diagnostics
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

		// Check diagnostics
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

// mapToCtyValue converts a map to a cty.Value based on schema
func (c *TerraformProviderClient) mapToCtyValue(
	data map[string]interface{}, schema *tfprotov6.SchemaBlock,
) (cty.Value, error) {
	attrs := make(map[string]cty.Value)
	log.Printf("Client: mapToCtyValue - processing %d schema attributes", len(schema.Attributes))

	// Process attributes
	if err := c.processAttributes(data, schema.Attributes, attrs); err != nil {
		return cty.NilVal, err
	}

	// Process block types
	log.Printf("Client: mapToCtyValue - processing %d block types", len(schema.BlockTypes))
	if err := c.processBlockTypes(data, schema.BlockTypes, attrs); err != nil {
		return cty.NilVal, err
	}

	return cty.ObjectVal(attrs), nil
}

// processAttributes handles schema attributes processing
func (c *TerraformProviderClient) processAttributes(
	data map[string]interface{}, attributes []*tfprotov6.SchemaAttribute, attrs map[string]cty.Value,
) error {
	for _, attr := range attributes {
		ctyType := TftypesToCtyType(attr.Type)

		val, exists := data[attr.Name]
		if !exists || val == nil {
			if attr.Required {
				return fmt.Errorf("%w: %q", ErrRequiredAttributeNotProvided, attr.Name)
			}
			attrs[attr.Name] = cty.NullVal(ctyType)
			continue
		}

		ctyVal, err := valueToCty(val, ctyType)
		if err != nil {
			return fmt.Errorf("%w %q: %w", ErrFailedToConvertAttribute, attr.Name, err)
		}
		attrs[attr.Name] = ctyVal
	}
	return nil
}

// processBlockTypes handles schema block types processing
func (c *TerraformProviderClient) processBlockTypes(
	data map[string]interface{}, blockTypes []*tfprotov6.SchemaNestedBlock, attrs map[string]cty.Value,
) error {
	for _, block := range blockTypes {
		// Skip the timeouts block if not provided - it's a meta-argument
		// that should be omitted rather than set to null
		if block.TypeName == "timeouts" {
			if _, exists := data[block.TypeName]; !exists {
				continue
			}
		}

		blockImpliedType := c.getImpliedType(block.Block)

		val, exists := data[block.TypeName]
		if !exists || val == nil {
			attrs[block.TypeName] = c.getEmptyValueForNesting(block.Nesting, blockImpliedType)
			continue
		}

		blockVal, err := c.processBlockValue(val, block, blockImpliedType)
		if err != nil {
			return fmt.Errorf("%w %q: %w", ErrFailedToProcessBlock, block.TypeName, err)
		}
		attrs[block.TypeName] = blockVal
	}
	return nil
}

// getEmptyValueForNesting returns appropriate empty/null value based on nesting mode
func (c *TerraformProviderClient) getEmptyValueForNesting(
	nesting tfprotov6.SchemaNestedBlockNestingMode, impliedType cty.Type,
) cty.Value {
	// For optional blocks that aren't provided, return null instead of empty collections
	// This matches Terraform's behavior and prevents provider errors
	switch nesting {
	case tfprotov6.SchemaNestedBlockNestingModeList:
		return cty.NullVal(cty.List(impliedType))
	case tfprotov6.SchemaNestedBlockNestingModeSet:
		return cty.NullVal(cty.Set(impliedType))
	case tfprotov6.SchemaNestedBlockNestingModeSingle:
		return cty.NullVal(impliedType)
	case tfprotov6.SchemaNestedBlockNestingModeMap:
		return cty.NullVal(cty.Map(impliedType))
	case tfprotov6.SchemaNestedBlockNestingModeInvalid:
		return cty.NullVal(cty.List(impliedType)) // Fallback for invalid
	case tfprotov6.SchemaNestedBlockNestingModeGroup:
		return cty.NullVal(cty.List(impliedType)) // Fallback for group
	default:
		return cty.NullVal(cty.List(impliedType))
	}
}

// processBlockValue handles the actual block value processing
func (c *TerraformProviderClient) processBlockValue(
	val interface{}, block *tfprotov6.SchemaNestedBlock, impliedType cty.Type,
) (cty.Value, error) {
	switch v := val.(type) {
	case []interface{}:
		return c.processSliceBlock(v, block, impliedType)
	case map[string]interface{}:
		return c.processMapBlock(v, block, impliedType)
	default:
		log.Printf("Client: Block %s has unexpected type %T", block.TypeName, v)
		return c.getEmptyValueForNesting(block.Nesting, impliedType), nil
	}
}

// processSliceBlock handles slice-type block values
func (c *TerraformProviderClient) processSliceBlock(
	v []interface{}, block *tfprotov6.SchemaNestedBlock, impliedType cty.Type,
) (cty.Value, error) {
	if len(v) == 0 {
		log.Printf("Client: Block %s is an empty collection", block.TypeName)
		return c.getEmptyValueForNesting(block.Nesting, impliedType), nil
	}

	// Re-enable endpoints block processing
	// if block.TypeName == "endpoints" {
	// 	log.Printf("Client: TEMPORARILY skipping endpoints block processing")
	// 	return c.getEmptyValueForNesting(block.Nesting, impliedType), nil
	// }

	// Handle non-empty blocks
	log.Printf("Client: Block %s has %d items", block.TypeName, len(v))

	// Convert each item in the slice
	elements := make([]cty.Value, len(v))
	for i, item := range v {
		// Each item should be a map
		itemMap, ok := item.(map[string]interface{})
		if !ok {
			return cty.NilVal, fmt.Errorf("block item %d is not a map", i)
		}

		// Convert the map to cty.Value
		itemVal, err := c.mapToCtyValue(itemMap, block.Block)
		if err != nil {
			return cty.NilVal, fmt.Errorf("failed to convert block item %d: %w", i, err)
		}
		elements[i] = itemVal
	}

	// Return the appropriate collection type based on nesting mode
	switch block.Nesting {
	case tfprotov6.SchemaNestedBlockNestingModeList:
		return cty.ListVal(elements), nil
	case tfprotov6.SchemaNestedBlockNestingModeSet:
		return cty.SetVal(elements), nil
	default:
		return cty.ListVal(elements), nil
	}
}

// processMapBlock handles map-type block values
func (c *TerraformProviderClient) processMapBlock(
	v map[string]interface{}, block *tfprotov6.SchemaNestedBlock, impliedType cty.Type,
) (cty.Value, error) {
	if len(v) == 0 {
		log.Printf("Client: Block %s is an empty single block", block.TypeName)
		if block.Nesting == tfprotov6.SchemaNestedBlockNestingModeSingle {
			return cty.NullVal(impliedType), nil
		}
		return cty.MapValEmpty(impliedType), nil
	}

	// Handle non-empty single blocks
	log.Printf("Client: Block %s has values", block.TypeName)

	if block.Nesting == tfprotov6.SchemaNestedBlockNestingModeSingle {
		// For single blocks, convert the map directly
		return c.mapToCtyValue(v, block.Block)
	}

	if block.TypeName == "endpoints" && c.name == "aws" && block.Nesting == tfprotov6.SchemaNestedBlockNestingModeSet {
		return c.processAWSEndpointsBlock(v, block)
	}

	// For map nesting mode, process each entry
	values := make(map[string]cty.Value)
	for key, val := range v {
		valMap, ok := val.(map[string]interface{})
		if !ok {
			return cty.NilVal, fmt.Errorf("map block value for key %s is not a map", key)
		}

		itemVal, err := c.mapToCtyValue(valMap, block.Block)
		if err != nil {
			return cty.NilVal, fmt.Errorf("failed to convert map block value for key %s: %w", key, err)
		}
		values[key] = itemVal
	}

	return cty.MapVal(values), nil
}

// processAWSEndpointsBlock handles AWS provider endpoints block special case
func (c *TerraformProviderClient) processAWSEndpointsBlock(
	v map[string]interface{}, block *tfprotov6.SchemaNestedBlock,
) (cty.Value, error) {
	// Create a single object with all the endpoint configurations
	// The schema expects ALL service attributes to be present
	endpointAttrs := make(map[string]cty.Value)

	// Initialize all service attributes with null values
	for _, attr := range block.Block.Attributes {
		endpointAttrs[attr.Name] = cty.NullVal(cty.String)
	}

	// Override with the actual endpoint URLs we have
	for service, val := range v {
		if urlStr, ok := val.(string); ok {
			// Only set if this service exists in the schema
			for _, attr := range block.Block.Attributes {
				if attr.Name == service {
					endpointAttrs[service] = cty.StringVal(urlStr)
					break
				}
			}
		}
	}

	// Create the object and wrap it in a set
	endpointObj := cty.ObjectVal(endpointAttrs)
	return cty.SetVal([]cty.Value{endpointObj}), nil
}

// tftypesToCtyType converts tftypes.Type to cty.Type
// Exported for use by TerraformCompatibleClient
func TftypesToCtyType(t tftypes.Type) cty.Type {
	if t == nil {
		return cty.String // Fallback for nil type
	}

	// Check primitive types first
	if primitiveType := convertPrimitiveType(t); primitiveType != cty.NilType {
		return primitiveType
	}

	// Check collection types
	if collectionType := convertCollectionType(t); collectionType != cty.NilType {
		return collectionType
	}

	// Fallback: check type string for v5 protocol compatibility
	return convertCtyTypeFromString(t.String())
}

// convertPrimitiveType converts primitive tftypes to cty types
func convertPrimitiveType(t tftypes.Type) cty.Type {
	switch {
	case t.Is(tftypes.String):
		return cty.String
	case t.Is(tftypes.Bool):
		return cty.Bool
	case t.Is(tftypes.Number):
		return cty.Number
	default:
		return cty.NilType
	}
}

// convertCollectionType converts collection tftypes to cty types
func convertCollectionType(t tftypes.Type) cty.Type {
	// Check lists
	if listType := convertListType(t); listType != cty.NilType {
		return listType
	}

	// Check sets
	if setType := convertSetType(t); setType != cty.NilType {
		return setType
	}

	// Check maps
	if mapType := convertMapType(t); mapType != cty.NilType {
		return mapType
	}

	return cty.NilType
}

// convertListType converts list tftypes to cty list types
func convertListType(t tftypes.Type) cty.Type {
	switch {
	case t.Is(tftypes.List{ElementType: tftypes.String}):
		return cty.List(cty.String)
	case t.Is(tftypes.List{ElementType: tftypes.DynamicPseudoType}):
		return cty.List(cty.String) // Default list element type
	default:
		return cty.NilType
	}
}

// convertSetType converts set tftypes to cty set types
func convertSetType(t tftypes.Type) cty.Type {
	switch {
	case t.Is(tftypes.Set{ElementType: tftypes.String}):
		return cty.Set(cty.String)
	case t.Is(tftypes.Set{ElementType: tftypes.DynamicPseudoType}):
		return cty.Set(cty.String) // Default set element type
	default:
		return cty.NilType
	}
}

// convertMapType converts map tftypes to cty map types
func convertMapType(t tftypes.Type) cty.Type {
	switch {
	case t.Is(tftypes.Map{ElementType: tftypes.String}):
		return cty.Map(cty.String)
	case t.Is(tftypes.Map{ElementType: tftypes.DynamicPseudoType}):
		return cty.Map(cty.String) // Default map element type
	default:
		return cty.NilType
	}
}

// convertCtyTypeFromString converts type based on string representation (v5 protocol fallback)
func convertCtyTypeFromString(typeStr string) cty.Type {
	switch {
	case typeStr == "tftypes.List[tftypes.String]" || typeStr == "list of string":
		return cty.List(cty.String)
	case typeStr == "tftypes.Set[tftypes.String]" || typeStr == "set of string":
		return cty.Set(cty.String)
	case typeStr == "tftypes.Map[tftypes.String]" || typeStr == "map of string":
		return cty.Map(cty.String)
	default:
		return cty.String // Default fallback
	}
}

// valueToCty converts a Go value to cty.Value
func valueToCty(val interface{}, targetType cty.Type) (cty.Value, error) {
	switch v := val.(type) {
	case string:
		return cty.StringVal(v), nil
	case bool:
		return cty.BoolVal(v), nil
	case int:
		return cty.NumberIntVal(int64(v)), nil
	case int64:
		return cty.NumberIntVal(v), nil
	case float64:
		return cty.NumberFloatVal(v), nil
	case []interface{}:
		return convertSliceToCty(v, targetType)
	case map[string]interface{}:
		return convertMapToCty(v, targetType)
	case nil:
		return cty.NullVal(targetType), nil
	default:
		return convertUnknownToCty(val, targetType)
	}
}

// convertSliceToCty handles slice to cty conversion
func convertSliceToCty(v []interface{}, targetType cty.Type) (cty.Value, error) {
	if !targetType.IsListType() && !targetType.IsSetType() {
		return cty.ListValEmpty(cty.String), nil
	}

	// Handle empty slices
	if len(v) == 0 {
		if targetType.IsListType() {
			return cty.ListValEmpty(targetType.ElementType()), nil
		}
		return cty.SetValEmpty(targetType.ElementType()), nil
	}

	// Handle non-empty slices
	elements := make([]cty.Value, len(v))
	elemType := targetType.ElementType()
	for i, elem := range v {
		elemVal, err := valueToCty(elem, elemType)
		if err != nil {
			return cty.NilVal, err
		}
		elements[i] = elemVal
	}

	if targetType.IsListType() {
		return cty.ListVal(elements), nil
	}
	return cty.SetVal(elements), nil
}

// convertMapToCty handles map to cty conversion
func convertMapToCty(v map[string]interface{}, targetType cty.Type) (cty.Value, error) {
	if !targetType.IsMapType() {
		return cty.NilVal, fmt.Errorf("%w: %s", ErrCannotConvertMapToNonMap, targetType.FriendlyName())
	}

	if len(v) == 0 {
		return cty.MapValEmpty(targetType.ElementType()), nil
	}

	vals := make(map[string]cty.Value)
	elemType := targetType.ElementType()
	for k, val := range v {
		elemVal, err := valueToCty(val, elemType)
		if err != nil {
			return cty.NilVal, err
		}
		vals[k] = elemVal
	}
	return cty.MapVal(vals), nil
}

// convertUnknownToCty handles unknown type conversion using JSON fallback
func convertUnknownToCty(val interface{}, targetType cty.Type) (cty.Value, error) {
	jsonBytes, err := ctyjson.Marshal(cty.StringVal(fmt.Sprintf("%v", val)), cty.String)
	if err != nil {
		return cty.NilVal, fmt.Errorf("failed to marshal cty value to JSON: %w", err)
	}
	result, err := ctyjson.Unmarshal(jsonBytes, targetType)
	if err != nil {
		return cty.NilVal, fmt.Errorf("failed to unmarshal JSON to cty value: %w", err)
	}
	return result, nil
}

// ctyValueToMap converts a cty.Value back to a map
func (c *TerraformProviderClient) ctyValueToMap(val cty.Value) (map[string]interface{}, error) {
	// Use JSON as intermediate format
	jsonBytes, err := ctyjson.Marshal(val, val.Type())
	if err != nil {
		return nil, fmt.Errorf("failed to marshal cty value to JSON: %w", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(jsonBytes, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	return result, nil
}

// Helper functions from v1 implementation
func (c *TerraformProviderClient) getProviderSchemaV5(
	ctx context.Context,
) (*tfprotov6.GetProviderSchemaResponse, error) {
	req := &tfplugin5.GetProviderSchema_Request{}

	resp, err := c.grpcClientV5.GetSchema(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get schema from provider v5: %w", err)
	}

	// Convert v5 response to v6 format
	result := &tfprotov6.GetProviderSchemaResponse{
		Provider:          convertSchemaV5ToV6(resp.GetProvider()),
		ResourceSchemas:   make(map[string]*tfprotov6.Schema),
		DataSourceSchemas: make(map[string]*tfprotov6.Schema),
	}

	for name, schema := range resp.GetResourceSchemas() {
		result.ResourceSchemas[name] = convertSchemaV5ToV6(schema)
	}

	for name, schema := range resp.GetDataSourceSchemas() {
		result.DataSourceSchemas[name] = convertSchemaV5ToV6(schema)
	}

	return result, nil
}

func (c *TerraformProviderClient) getProviderSchemaV6(
	ctx context.Context,
) (*tfprotov6.GetProviderSchemaResponse, error) {
	req := &tfplugin6.GetProviderSchema_Request{}

	resp, err := c.grpcClientV6.GetProviderSchema(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to get schema from provider v6: %w", err)
	}

	// Convert response (simplified - full implementation would convert all fields)
	result := &tfprotov6.GetProviderSchemaResponse{
		Provider:          convertSchemaV6(resp.GetProvider()),
		ResourceSchemas:   make(map[string]*tfprotov6.Schema),
		DataSourceSchemas: make(map[string]*tfprotov6.Schema),
	}

	for name, schema := range resp.GetResourceSchemas() {
		result.ResourceSchemas[name] = convertSchemaV6(schema)
	}

	return result, nil
}

// convertSchemaV6 converts proto schema to tfprotov6.Schema
func convertSchemaV6(protoSchema *tfplugin6.Schema) *tfprotov6.Schema {
	if protoSchema == nil {
		return nil
	}
	return &tfprotov6.Schema{
		Version: protoSchema.GetVersion(),
		Block:   convertBlockV6(protoSchema.GetBlock()),
	}
}

// convertBlockV6 converts proto block to tfprotov6.SchemaBlock
func convertBlockV6(protoBlock *tfplugin6.Schema_Block) *tfprotov6.SchemaBlock {
	if protoBlock == nil {
		return nil
	}

	block := &tfprotov6.SchemaBlock{
		Attributes: make([]*tfprotov6.SchemaAttribute, 0, len(protoBlock.GetAttributes())),
		BlockTypes: make([]*tfprotov6.SchemaNestedBlock, 0, len(protoBlock.GetBlockTypes())),
	}

	for _, attr := range protoBlock.GetAttributes() {
		block.Attributes = append(block.Attributes, &tfprotov6.SchemaAttribute{
			Name:        attr.GetName(),
			Type:        parseAttributeTypeV6(attr), // Parse actual type from v6 attribute
			Required:    attr.GetRequired(),
			Optional:    attr.GetOptional(),
			Computed:    attr.GetComputed(),
			Sensitive:   attr.GetSensitive(),
			Description: attr.GetDescription(),
		})
	}

	// FUTURE: Convert block types

	return block
}

// getImpliedType constructs the implied cty.Type from a schema block
func (c *TerraformProviderClient) getImpliedType(block *tfprotov6.SchemaBlock) cty.Type {
	if block == nil {
		return cty.EmptyObject
	}

	attrs := make(map[string]cty.Type)

	// Add attributes
	for _, attr := range block.Attributes {
		attrs[attr.Name] = TftypesToCtyType(attr.Type)
	}

	// Add block types with proper nesting
	for _, blockType := range block.BlockTypes {
		// Get the implied type of the nested block
		nestedType := c.getImpliedType(blockType.Block)

		// Determine collection type based on nesting mode
		switch blockType.Nesting {
		case tfprotov6.SchemaNestedBlockNestingModeList:
			attrs[blockType.TypeName] = cty.List(nestedType)
		case tfprotov6.SchemaNestedBlockNestingModeSet:
			attrs[blockType.TypeName] = cty.Set(nestedType)
		case tfprotov6.SchemaNestedBlockNestingModeSingle:
			attrs[blockType.TypeName] = nestedType
		case tfprotov6.SchemaNestedBlockNestingModeMap:
			attrs[blockType.TypeName] = cty.Map(nestedType)
		case tfprotov6.SchemaNestedBlockNestingModeInvalid:
			attrs[blockType.TypeName] = cty.List(nestedType) // Fallback for invalid
		case tfprotov6.SchemaNestedBlockNestingModeGroup:
			attrs[blockType.TypeName] = cty.List(nestedType) // Fallback for group
		default:
			// Default to list
			attrs[blockType.TypeName] = cty.List(nestedType)
		}
	}

	return cty.Object(attrs)
}

// diagnosticsToError converts diagnostics to error message
func diagnosticsToError(diags []*tfplugin6.Diagnostic) string {
	if len(diags) == 0 {
		return ""
	}
	// Return first error
	for _, diag := range diags {
		if diag.GetSeverity() == tfplugin6.Diagnostic_ERROR {
			return diag.GetSummary() + " - " + diag.GetDetail()
		}
	}
	return diags[0].GetSummary()
}

// diagnosticsToErrorV5 converts v5 diagnostics to error message
func diagnosticsToErrorV5(diags []*tfplugin5.Diagnostic) string {
	if len(diags) == 0 {
		return ""
	}
	// Return first error
	for _, diag := range diags {
		if diag.GetSeverity() == tfplugin5.Diagnostic_ERROR {
			return diag.GetSummary() + " - " + diag.GetDetail()
		}
	}
	return diags[0].GetSummary()
}

// convertSchemaV5ToV6 converts proto schema from v5 to v6 format
func convertSchemaV5ToV6(protoSchema *tfplugin5.Schema) *tfprotov6.Schema {
	if protoSchema == nil {
		return nil
	}
	return &tfprotov6.Schema{
		Version: protoSchema.GetVersion(),
		Block:   convertBlockV5ToV6(protoSchema.GetBlock()),
	}
}

// convertBlockV5ToV6 converts proto block from v5 to v6 format
func convertBlockV5ToV6(protoBlock *tfplugin5.Schema_Block) *tfprotov6.SchemaBlock {
	if protoBlock == nil {
		return nil
	}

	block := &tfprotov6.SchemaBlock{
		Attributes: make([]*tfprotov6.SchemaAttribute, 0, len(protoBlock.GetAttributes())),
		BlockTypes: make([]*tfprotov6.SchemaNestedBlock, 0, len(protoBlock.GetBlockTypes())),
	}

	for _, attr := range protoBlock.GetAttributes() {
		block.Attributes = append(block.Attributes, &tfprotov6.SchemaAttribute{
			Name:        attr.GetName(),
			Type:        parseAttributeTypeV5(attr), // Parse actual type from v5 attribute
			Required:    attr.GetRequired(),
			Optional:    attr.GetOptional(),
			Computed:    attr.GetComputed(),
			Sensitive:   attr.GetSensitive(),
			Description: attr.GetDescription(),
		})
	}

	for _, blockType := range protoBlock.GetBlockTypes() {
		block.BlockTypes = append(block.BlockTypes, &tfprotov6.SchemaNestedBlock{
			TypeName: blockType.GetTypeName(),
			Block:    convertBlockV5ToV6(blockType.GetBlock()),
			Nesting:  convertNestingModeV5ToV6(blockType.GetNesting()),
			MinItems: blockType.GetMinItems(),
			MaxItems: blockType.GetMaxItems(),
		})
	}

	return block
}

// convertNestingModeV5ToV6 converts nesting mode from v5 to v6
func convertNestingModeV5ToV6(mode tfplugin5.Schema_NestedBlock_NestingMode) tfprotov6.SchemaNestedBlockNestingMode {
	switch mode {
	case tfplugin5.Schema_NestedBlock_SINGLE:
		return tfprotov6.SchemaNestedBlockNestingModeSingle
	case tfplugin5.Schema_NestedBlock_LIST:
		return tfprotov6.SchemaNestedBlockNestingModeList
	case tfplugin5.Schema_NestedBlock_SET:
		return tfprotov6.SchemaNestedBlockNestingModeSet
	case tfplugin5.Schema_NestedBlock_MAP:
		return tfprotov6.SchemaNestedBlockNestingModeMap
	case tfplugin5.Schema_NestedBlock_INVALID:
		return tfprotov6.SchemaNestedBlockNestingModeInvalid
	case tfplugin5.Schema_NestedBlock_GROUP:
		return tfprotov6.SchemaNestedBlockNestingModeGroup
	default:
		return tfprotov6.SchemaNestedBlockNestingModeSingle
	}
}

// parseAttributeType parses the type information from attribute (common logic for v5 and v6)
func parseAttributeType(attrName string, typeBytes []byte) tftypes.Type {
	// No type info - make educated guess based on name
	if len(typeBytes) == 0 {
		return guessTypeFromName(attrName)
	}

	// Parse the JSON type info
	typeStr := string(typeBytes)
	return parseTypeFromString(typeStr)
}

// guessTypeFromName guesses the type based on attribute name patterns
func guessTypeFromName(attrName string) tftypes.Type {
	if isListAttributeName(attrName) {
		return tftypes.List{ElementType: tftypes.String}
	}
	if isMapAttributeName(attrName) {
		return tftypes.Map{ElementType: tftypes.String}
	}
	return tftypes.String
}

// isListAttributeName checks if the attribute name suggests a list type
func isListAttributeName(attrName string) bool {
	return strings.Contains(attrName, "_ids") ||
		strings.Contains(attrName, "_files") ||
		strings.Contains(attrName, "allowed_account_ids") ||
		strings.Contains(attrName, "forbidden_account_ids")
}

// isMapAttributeName checks if the attribute name suggests a map type
func isMapAttributeName(attrName string) bool {
	return attrName == "tags" || strings.HasSuffix(attrName, "_tags")
}

// parseTypeFromString parses type information from a JSON type string
func parseTypeFromString(typeStr string) tftypes.Type {
	switch {
	case isListType(typeStr):
		return parseListType(typeStr)
	case isSetType(typeStr):
		return parseSetType(typeStr)
	case isMapType(typeStr):
		return parseMapType(typeStr)
	case strings.Contains(typeStr, `"bool"`):
		return tftypes.Bool
	case strings.Contains(typeStr, `"number"`):
		return tftypes.Number
	default:
		return tftypes.String
	}
}

// isListType checks if the type string represents a list
func isListType(typeStr string) bool {
	return strings.Contains(typeStr, `"list"`) || strings.Contains(typeStr, `["list"`)
}

// isSetType checks if the type string represents a set
func isSetType(typeStr string) bool {
	return strings.Contains(typeStr, `"set"`) || strings.Contains(typeStr, `["set"`)
}

// isMapType checks if the type string represents a map
func isMapType(typeStr string) bool {
	return strings.Contains(typeStr, `"map"`) || strings.Contains(typeStr, `["map"`)
}

// parseListType parses a list type with its element type
func parseListType(typeStr string) tftypes.Type {
	if strings.Contains(typeStr, `"string"`) {
		return tftypes.List{ElementType: tftypes.String}
	}
	return tftypes.List{ElementType: tftypes.DynamicPseudoType}
}

// parseSetType parses a set type with its element type
func parseSetType(typeStr string) tftypes.Type {
	if strings.Contains(typeStr, `"string"`) {
		return tftypes.Set{ElementType: tftypes.String}
	}
	return tftypes.Set{ElementType: tftypes.DynamicPseudoType}
}

// parseMapType parses a map type with its element type
func parseMapType(typeStr string) tftypes.Type {
	if strings.Contains(typeStr, `"string"`) {
		return tftypes.Map{ElementType: tftypes.String}
	}
	return tftypes.Map{ElementType: tftypes.DynamicPseudoType}
}

// parseAttributeTypeV6 parses the type information from v6 attribute
func parseAttributeTypeV6(attr *tfplugin6.Schema_Attribute) tftypes.Type {
	return parseAttributeType(attr.GetName(), attr.GetType())
}

// parseAttributeTypeV5 parses the type information from v5 attribute
func parseAttributeTypeV5(attr *tfplugin5.Schema_Attribute) tftypes.Type {
	return parseAttributeType(attr.GetName(), attr.GetType())
}

// parseProviderAddressWithVersion extracts protocol version and address from provider output
func parseProviderAddressWithVersion(output string) (protocolVersion int, network string, address string, err error) {
	// Expected format:
	// 1|6|tcp|127.0.0.1:12345|grpc|  (protocol v6)
	// 1|5|tcp|127.0.0.1:12345|grpc|  (protocol v5)
	// 6|tcp|127.0.0.1:12345|grpc|    (protocol v6 old format)
	// 5|tcp|127.0.0.1:12345|grpc|    (protocol v5 old format)
	// unix:///path/to/socket|5       (unix socket with version)

	debugLogger := GetDebugLogger()
	debugLogger.Logf("address-parser", "Parsing provider address: %s", output)

	parts := strings.Split(strings.TrimSpace(output), "|")

	// Handle unix socket format
	if strings.HasPrefix(output, "unix://") {
		if len(parts) >= 2 {
			// unix:///path/to/socket|5
			version, err := strconv.Atoi(parts[1])
			if err != nil {
				return 5, "", "", fmt.Errorf("failed to parse unix socket version: %w", err)
			}
			return version, UnixNetwork, parts[0], nil
		}
		// Default to v5 for unix sockets without version
		return 5, UnixNetwork, parts[0], nil
	}

	// Handle standard format with version prefix
	if len(parts) >= 5 {
		// Try to parse first part as version (current format)
		if parts[0] == "1" {
			// Format: 1|VERSION|network|address|protocol|
			version, err := strconv.Atoi(parts[1])
			if err != nil {
				return 5, "", "", fmt.Errorf("failed to parse protocol version: %w", err)
			}
			return version, parts[2], parts[3], nil
		}
	}

	// Handle old format without "1|" prefix
	if len(parts) >= 4 {
		// Try to parse first part as version directly
		version, err := strconv.Atoi(parts[0])
		if err == nil && (version == 5 || version == 6) {
			// Format: VERSION|network|address|protocol|
			return version, parts[1], parts[2], nil
		}
	}

	// Fallback: assume v5 protocol
	if len(parts) >= 3 {
		debugLogger.Logf("address-parser", "Falling back to v5 protocol for address: %s", output)
		return 5, parts[0], parts[1], nil
	}

	return 5, "", "", fmt.Errorf("%w: %s", ErrUnableToParseProviderAddress, output)
}

// Close closes the connection
func (c *TerraformProviderClient) Close() error {
	if c.conn != nil {
		if err := c.conn.Close(); err != nil {
			return fmt.Errorf("failed to close gRPC connection: %w", err)
		}
	}
	return nil
}
