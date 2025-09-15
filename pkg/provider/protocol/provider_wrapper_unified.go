package protocol

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"github.com/lattiam/lattiam/pkg/logging"
)

// unifiedLogger is a shared logger instance for unified client operations
var unifiedLogger = logging.NewLogger("unified-client")

// initUnifiedClient initializes the UnifiedProviderClient if the build tag is present
func (p *providerWrapper) initUnifiedClient(ctx context.Context) error {
	if p.provInstance == nil {
		return nil // Can't create unified client without provider instance
	}

	// Type assert to PluginProviderInstance
	pluginInstance, ok := p.provInstance.(*PluginProviderInstance)
	if !ok {
		unifiedLogger.Warn("Provider instance is not a PluginProviderInstance, cannot use UnifiedProviderClient")
		return nil
	}

	// Create UnifiedProviderClient
	unifiedClient, err := NewUnifiedProviderClient(ctx, pluginInstance)
	if err != nil {
		unifiedLogger.Warn("Failed to create UnifiedProviderClient: %v", err)
		return err
	}

	p.unifiedClient = unifiedClient
	unifiedLogger.Debug("Successfully initialized UnifiedProviderClient for provider %s", p.Name())
	return nil
}

// Configure configures the provider using UnifiedProviderClient only
func (p *providerWrapper) Configure(ctx context.Context, config map[string]interface{}) error {
	// Use the provided configuration directly
	finalConfig := config

	// Initialize UnifiedProviderClient if not already done
	if p.unifiedClient == nil && p.provInstance != nil {
		if err := p.initUnifiedClient(ctx); err != nil {
			return fmt.Errorf("failed to initialize unified client: %w", err)
		}
	}

	// Use UnifiedProviderClient (no fallback)
	if p.unifiedClient == nil {
		return fmt.Errorf("unified client not available")
	}

	unifiedClient, ok := p.unifiedClient.(*UnifiedProviderClient)
	if !ok {
		return fmt.Errorf("invalid unified client type")
	}

	// Get the schema to create properly typed config
	schema, err := unifiedClient.GetProviderSchema(ctx)
	if err != nil {
		return fmt.Errorf("failed to get provider schema: %w", err)
	}

	// Use DynamicSchemaConverter to complete the config with all schema attributes
	// This handles type conversions and ensures all required attributes are present
	converter := NewDynamicSchemaConverter(schema)
	completeConfig, err := converter.CompleteProviderConfig(finalConfig, p.Name())
	if err != nil {
		return fmt.Errorf("failed to complete provider config: %w", err)
	}

	// Create config dynamic value
	var configDV *tfprotov6.DynamicValue
	if len(completeConfig) > 0 && schema.Provider != nil {
		configDV, err = CreateDynamicValueWithSchema(completeConfig, schema.Provider.Block)
		if err != nil {
			return fmt.Errorf("failed to create config dynamic value: %w", err)
		}
	} else {
		// Empty config
		configDV = &tfprotov6.DynamicValue{MsgPack: []byte{0xc0}} // nil msgpack
	}

	// Create configure request
	req := &tfprotov6.ConfigureProviderRequest{
		Config: configDV,
	}

	resp, err := unifiedClient.ConfigureProvider(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to configure provider: %w", err)
	}

	// Check for errors in response
	for _, diag := range resp.Diagnostics {
		if diag.Severity == tfprotov6.DiagnosticSeverityError {
			return fmt.Errorf("provider configuration error: %s - %s", diag.Summary, diag.Detail)
		}
	}

	unifiedLogger.Debug("Successfully configured provider %s using UnifiedProviderClient", p.Name())
	return nil
}

// ConfigureWithResourceData configures the provider with a map of configuration values
func (p *providerWrapper) ConfigureWithResourceData(ctx context.Context, config map[string]interface{}) error {
	// For now, delegate to Configure which handles both unified and legacy
	return p.Configure(ctx, config)
}

// GetSchema uses UnifiedProviderClient only
func (p *providerWrapper) GetSchema(ctx context.Context) (*tfprotov6.GetProviderSchemaResponse, error) {
	// Initialize unified client on first use if not already done
	if p.unifiedClient == nil && p.provInstance != nil {
		if err := p.initUnifiedClient(ctx); err != nil {
			return nil, fmt.Errorf("failed to initialize unified client: %w", err)
		}
	}

	// Use UnifiedProviderClient (no fallback)
	if p.unifiedClient == nil {
		return nil, fmt.Errorf("unified client not available")
	}

	unifiedClient, ok := p.unifiedClient.(*UnifiedProviderClient)
	if !ok {
		return nil, fmt.Errorf("invalid unified client type")
	}

	return unifiedClient.GetProviderSchema(ctx)
}

// PlanResourceChange uses UnifiedProviderClient only
func (p *providerWrapper) PlanResourceChange(ctx context.Context, resourceType string, priorState, config map[string]interface{}) (*tfprotov6.PlanResourceChangeResponse, error) {
	// Initialize unified client on first use if not already done
	if p.unifiedClient == nil && p.provInstance != nil {
		if err := p.initUnifiedClient(ctx); err != nil {
			return nil, fmt.Errorf("failed to initialize unified client: %w", err)
		}
	}

	// Use UnifiedProviderClient (no fallback)
	if p.unifiedClient == nil {
		return nil, fmt.Errorf("unified client not available")
	}

	unifiedClient, ok := p.unifiedClient.(*UnifiedProviderClient)
	if !ok {
		return nil, fmt.Errorf("invalid unified client type")
	}

	// 1. Get the schema using the new client
	schema, err := unifiedClient.GetProviderSchema(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get schema: %w", err)
	}

	resourceSchema, ok := schema.ResourceSchemas[resourceType]
	if !ok {
		return nil, fmt.Errorf("resource type %s not found in schema", resourceType)
	}

	// 2. Use our new helpers to create correctly typed dynamic values
	var priorStateDV *tfprotov6.DynamicValue
	if len(priorState) > 0 {
		priorStateDV, err = CreateDynamicValueWithSchema(priorState, resourceSchema.Block)
		if err != nil {
			return nil, fmt.Errorf("failed to create prior state dynamic value: %w", err)
		}
	} else {
		// Null value for empty prior state
		priorStateDV = &tfprotov6.DynamicValue{
			MsgPack: []byte{0xc0}, // msgpack nil
		}
	}

	// For planning, config is what the user provided
	configDV, err := CreateDynamicValueWithSchema(config, resourceSchema.Block)
	if err != nil {
		return nil, fmt.Errorf("failed to create config dynamic value: %w", err)
	}

	// 3. Build the simple request struct
	req := &tfprotov6.PlanResourceChangeRequest{
		TypeName:         resourceType,
		PriorState:       priorStateDV,
		ProposedNewState: configDV, // Provider will compute the actual new state
		Config:           configDV,
	}

	// 4. Delegate the call to the new client. That's it.
	unifiedLogger.Debug("Delegating PlanResourceChange to UnifiedProviderClient for resource type %s", resourceType)
	return unifiedClient.PlanResourceChange(ctx, req)
}

// CreateResource uses UnifiedProviderClient only
//
//nolint:funlen,gocyclo // Complex resource creation logic with unified client initialization and validation
func (p *providerWrapper) CreateResource(ctx context.Context, resourceType string, config map[string]interface{}) (map[string]interface{}, error) {
	// Initialize unified client on first use if not already done
	if p.unifiedClient == nil && p.provInstance != nil {
		if err := p.initUnifiedClient(ctx); err != nil {
			return nil, fmt.Errorf("failed to initialize unified client: %w", err)
		}
	}

	// Use UnifiedProviderClient (no fallback)
	if p.unifiedClient == nil {
		return nil, fmt.Errorf("unified client not available")
	}

	unifiedClient, ok := p.unifiedClient.(*UnifiedProviderClient)
	if !ok {
		return nil, fmt.Errorf("invalid unified client type")
	}

	// 1. Get the schema
	schema, err := unifiedClient.GetProviderSchema(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get schema: %w", err)
	}

	resourceSchema, ok := schema.ResourceSchemas[resourceType]
	if !ok {
		return nil, fmt.Errorf("resource type %s not found in schema", resourceType)
	}

	// 2. Plan the resource creation (nil prior state for create)
	configDV, err := CreateDynamicValueWithSchema(config, resourceSchema.Block)
	if err != nil {
		return nil, fmt.Errorf("failed to create config dynamic value: %w", err)
	}

	planReq := &tfprotov6.PlanResourceChangeRequest{
		TypeName:         resourceType,
		PriorState:       &tfprotov6.DynamicValue{MsgPack: []byte{0xc0}}, // nil
		ProposedNewState: configDV,
		Config:           configDV,
	}

	planResp, err := unifiedClient.PlanResourceChange(ctx, planReq)
	if err != nil {
		return nil, fmt.Errorf("failed to plan resource creation: %w", err)
	}

	// Check for errors in plan
	for _, diag := range planResp.Diagnostics {
		if diag.Severity == tfprotov6.DiagnosticSeverityError {
			return nil, fmt.Errorf("plan error: %s - %s", diag.Summary, diag.Detail)
		}
	}

	// 3. Apply the resource creation
	applyReq := &tfprotov6.ApplyResourceChangeRequest{
		TypeName:     resourceType,
		Config:       configDV,
		PlannedState: planResp.PlannedState,
		PriorState:   planReq.PriorState,
	}

	applyResp, err := unifiedClient.ApplyResourceChange(ctx, applyReq)
	if err != nil {
		return nil, fmt.Errorf("failed to apply resource creation: %w", err)
	}

	// Check for errors in apply
	for _, diag := range applyResp.Diagnostics {
		if diag.Severity == tfprotov6.DiagnosticSeverityError {
			return nil, fmt.Errorf("apply error: %s - %s", diag.Summary, diag.Detail)
		}
	}

	// 4. Convert the result back to a map
	schemaType := resourceSchema.Block.ValueType()
	result, err := ConvertDynamicValueToMap(applyResp.NewState, schemaType)
	if err != nil {
		return nil, fmt.Errorf("failed to convert result to map: %w", err)
	}

	unifiedLogger.Debug("Successfully created resource %s using UnifiedProviderClient", resourceType)
	return result, nil
}

// ReadResource reads a resource using UnifiedProviderClient only
func (p *providerWrapper) ReadResource(ctx context.Context, resourceType string, state map[string]interface{}) (map[string]interface{}, error) {
	// Initialize unified client on first use if not already done
	if p.unifiedClient == nil && p.provInstance != nil {
		if err := p.initUnifiedClient(ctx); err != nil {
			return nil, fmt.Errorf("failed to initialize unified client: %w", err)
		}
	}

	// Use UnifiedProviderClient (no fallback)
	if p.unifiedClient == nil {
		return nil, fmt.Errorf("unified client not available")
	}

	unifiedClient, ok := p.unifiedClient.(*UnifiedProviderClient)
	if !ok {
		return nil, fmt.Errorf("invalid unified client type")
	}

	// Get the schema
	schema, err := unifiedClient.GetProviderSchema(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get schema: %w", err)
	}

	resourceSchema, ok := schema.ResourceSchemas[resourceType]
	if !ok {
		return nil, fmt.Errorf("resource type %s not found in schema", resourceType)
	}

	// Create dynamic value for current state
	stateDV, err := CreateDynamicValueWithSchema(state, resourceSchema.Block)
	if err != nil {
		return nil, fmt.Errorf("failed to create state dynamic value: %w", err)
	}

	// Create read request
	req := &tfprotov6.ReadResourceRequest{
		TypeName:     resourceType,
		CurrentState: stateDV,
	}

	// Read the resource
	resp, err := unifiedClient.ReadResource(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to read resource: %w", err)
	}

	// Check for errors
	for _, diag := range resp.Diagnostics {
		if diag.Severity == tfprotov6.DiagnosticSeverityError {
			return nil, fmt.Errorf("read error: %s - %s", diag.Summary, diag.Detail)
		}
	}

	// Convert the result back to a map
	schemaType := resourceSchema.Block.ValueType()
	result, err := ConvertDynamicValueToMap(resp.NewState, schemaType)
	if err != nil {
		return nil, fmt.Errorf("failed to convert result to map: %w", err)
	}

	unifiedLogger.Debug("Successfully read resource %s using UnifiedProviderClient", resourceType)
	return result, nil
}

// UpdateResource uses UnifiedProviderClient only
//
//nolint:gocognit,funlen,gocyclo // Complex resource update logic with state management
func (p *providerWrapper) UpdateResource(ctx context.Context, resourceType string, priorState, config map[string]interface{}) (map[string]interface{}, error) {
	// Initialize unified client on first use if not already done
	if p.unifiedClient == nil && p.provInstance != nil {
		if err := p.initUnifiedClient(ctx); err != nil {
			return nil, fmt.Errorf("failed to initialize unified client: %w", err)
		}
	}

	// Use UnifiedProviderClient (no fallback)
	if p.unifiedClient == nil {
		return nil, fmt.Errorf("unified client not available")
	}

	unifiedClient, ok := p.unifiedClient.(*UnifiedProviderClient)
	if !ok {
		return nil, fmt.Errorf("invalid unified client type")
	}

	// 1. Get the schema
	schema, err := unifiedClient.GetProviderSchema(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get schema: %w", err)
	}

	resourceSchema, ok := schema.ResourceSchemas[resourceType]
	if !ok {
		return nil, fmt.Errorf("resource type %s not found in schema", resourceType)
	}

	// 2. Create dynamic values for prior state and config
	priorStateDV, err := CreateDynamicValueWithSchema(priorState, resourceSchema.Block)
	if err != nil {
		return nil, fmt.Errorf("failed to create prior state dynamic value: %w", err)
	}

	configDV, err := CreateDynamicValueWithSchema(config, resourceSchema.Block)
	if err != nil {
		return nil, fmt.Errorf("failed to create config dynamic value: %w", err)
	}

	// 3. Plan the resource update
	planReq := &tfprotov6.PlanResourceChangeRequest{
		TypeName:         resourceType,
		PriorState:       priorStateDV,
		ProposedNewState: configDV,
		Config:           configDV,
	}

	unifiedLogger.Debug("UpdateResource: Planning update for %s", resourceType)
	planResp, err := unifiedClient.PlanResourceChange(ctx, planReq)
	if err != nil {
		return nil, fmt.Errorf("failed to plan resource update: %w", err)
	}

	// Log if this requires replacement
	if len(planResp.RequiresReplace) > 0 {
		unifiedLogger.Debug("UpdateResource: %s requires replacement for paths: %v", resourceType, planResp.RequiresReplace)
	}

	// Check for errors in plan
	for _, diag := range planResp.Diagnostics {
		if diag.Severity == tfprotov6.DiagnosticSeverityError {
			return nil, fmt.Errorf("plan error: %s - %s", diag.Summary, diag.Detail)
		}
	}

	// 4. Apply the resource update
	applyReq := &tfprotov6.ApplyResourceChangeRequest{
		TypeName:     resourceType,
		Config:       configDV,
		PlannedState: planResp.PlannedState,
		PriorState:   priorStateDV,
	}

	applyResp, err := unifiedClient.ApplyResourceChange(ctx, applyReq)
	if err != nil {
		return nil, fmt.Errorf("failed to apply resource update: %w", err)
	}

	// Check for errors in apply
	for _, diag := range applyResp.Diagnostics {
		if diag.Severity == tfprotov6.DiagnosticSeverityError {
			return nil, fmt.Errorf("apply error: %s - %s", diag.Summary, diag.Detail)
		}
	}

	// 5. Convert the result back to a map
	schemaType := resourceSchema.Block.ValueType()
	result, err := ConvertDynamicValueToMap(applyResp.NewState, schemaType)
	if err != nil {
		return nil, fmt.Errorf("failed to convert result to map: %w", err)
	}

	// Debug logging for random_string updates
	if resourceType == "random_string" {
		if priorLength, ok := priorState["length"]; ok {
			if newLength, ok := result["length"]; ok {
				unifiedLogger.Debug("UpdateResource: random_string length changed from %v to %v", priorLength, newLength)
			}
		}
		if priorResult, ok := priorState["result"]; ok {
			if newResult, ok := result["result"]; ok {
				unifiedLogger.Debug("UpdateResource: random_string result changed from %v to %v", priorResult, newResult)
			}
		}
	}

	unifiedLogger.Debug("Successfully updated resource %s using UnifiedProviderClient", resourceType)
	return result, nil
}

// DeleteResource uses UnifiedProviderClient only
//
//nolint:funlen // Complex resource deletion logic with unified client initialization and state management
func (p *providerWrapper) DeleteResource(ctx context.Context, resourceType string, state map[string]interface{}) error {
	// Initialize unified client on first use if not already done
	if p.unifiedClient == nil && p.provInstance != nil {
		if err := p.initUnifiedClient(ctx); err != nil {
			return fmt.Errorf("failed to initialize unified client: %w", err)
		}
	}

	// Use UnifiedProviderClient (no fallback)
	if p.unifiedClient == nil {
		return fmt.Errorf("unified client not available")
	}

	unifiedClient, ok := p.unifiedClient.(*UnifiedProviderClient)
	if !ok {
		return fmt.Errorf("invalid unified client type")
	}

	// 1. Get the schema
	schema, err := unifiedClient.GetProviderSchema(ctx)
	if err != nil {
		return fmt.Errorf("failed to get schema: %w", err)
	}

	resourceSchema, ok := schema.ResourceSchemas[resourceType]
	if !ok {
		return fmt.Errorf("resource type %s not found in schema", resourceType)
	}

	// 2. Create dynamic value for current state
	stateDV, err := CreateDynamicValueWithSchema(state, resourceSchema.Block)
	if err != nil {
		return fmt.Errorf("failed to create state dynamic value: %w", err)
	}

	// 3. Plan the resource deletion (nil proposed state for delete)
	planReq := &tfprotov6.PlanResourceChangeRequest{
		TypeName:         resourceType,
		PriorState:       stateDV,
		ProposedNewState: &tfprotov6.DynamicValue{MsgPack: []byte{0xc0}}, // nil for deletion
		Config:           &tfprotov6.DynamicValue{MsgPack: []byte{0xc0}}, // nil config for deletion
	}

	planResp, err := unifiedClient.PlanResourceChange(ctx, planReq)
	if err != nil {
		return fmt.Errorf("failed to plan resource deletion: %w", err)
	}

	// Check for errors in plan
	for _, diag := range planResp.Diagnostics {
		if diag.Severity == tfprotov6.DiagnosticSeverityError {
			return fmt.Errorf("plan error: %s - %s", diag.Summary, diag.Detail)
		}
	}

	// 4. Apply the resource deletion
	applyReq := &tfprotov6.ApplyResourceChangeRequest{
		TypeName:     resourceType,
		Config:       planReq.Config,
		PlannedState: planResp.PlannedState,
		PriorState:   stateDV,
	}

	applyResp, err := unifiedClient.ApplyResourceChange(ctx, applyReq)
	if err != nil {
		return fmt.Errorf("failed to apply resource deletion: %w", err)
	}

	// Check for errors in apply
	for _, diag := range applyResp.Diagnostics {
		if diag.Severity == tfprotov6.DiagnosticSeverityError {
			return fmt.Errorf("apply error: %s - %s", diag.Summary, diag.Detail)
		}
	}

	unifiedLogger.Debug("Successfully deleted resource %s using UnifiedProviderClient", resourceType)
	return nil
}

// ReadDataSource reads a data source using UnifiedProviderClient only
func (p *providerWrapper) ReadDataSource(ctx context.Context, dataSourceType string, config map[string]interface{}) (map[string]interface{}, error) {
	// Initialize unified client on first use if not already done
	if p.unifiedClient == nil && p.provInstance != nil {
		if err := p.initUnifiedClient(ctx); err != nil {
			return nil, fmt.Errorf("failed to initialize unified client: %w", err)
		}
	}

	// Use UnifiedProviderClient (no fallback)
	if p.unifiedClient == nil {
		return nil, fmt.Errorf("unified client not available")
	}

	unifiedClient, ok := p.unifiedClient.(*UnifiedProviderClient)
	if !ok {
		return nil, fmt.Errorf("invalid unified client type")
	}

	// Get schema
	schema, err := unifiedClient.GetProviderSchema(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get schema: %w", err)
	}

	dataSourceSchema, ok := schema.DataSourceSchemas[dataSourceType]
	if !ok {
		return nil, fmt.Errorf("data source type %s not found in schema", dataSourceType)
	}

	// Create dynamic value for config
	configDV, err := CreateDynamicValueWithSchema(config, dataSourceSchema.Block)
	if err != nil {
		return nil, fmt.Errorf("failed to create config dynamic value: %w", err)
	}

	// Create read request
	req := &tfprotov6.ReadDataSourceRequest{
		TypeName: dataSourceType,
		Config:   configDV,
	}

	// Read the data source
	resp, err := unifiedClient.ReadDataSource(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("failed to read data source: %w", err)
	}

	// Check for errors
	if resp != nil && len(resp.Diagnostics) > 0 {
		for _, diag := range resp.Diagnostics {
			if diag.Severity == tfprotov6.DiagnosticSeverityError {
				return nil, fmt.Errorf("data source read error: %s - %s", diag.Summary, diag.Detail)
			}
		}
	}

	// Convert the result back to a map
	schemaType := dataSourceSchema.Block.ValueType()
	result, err := ConvertDynamicValueToMap(resp.State, schemaType)
	if err != nil {
		return nil, fmt.Errorf("failed to convert result to map: %w", err)
	}

	return result, nil
}

// Close closes the provider connection and stops the provider instance (unified version)
func (p *providerWrapper) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	var firstErr error

	// Close the unified client connection if available
	if p.unifiedClient != nil {
		if uc, ok := p.unifiedClient.(*UnifiedProviderClient); ok {
			if err := uc.Close(); err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("failed to close unified client: %w", err)
				}
			}
		}
	}

	// Stop the provider instance to terminate the provider process
	if p.provInstance != nil {
		if err := p.provInstance.Stop(); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("failed to stop provider instance: %w", err)
			}
		}
	}

	return firstErr
}

// IsHealthy checks if the provider is healthy (unified version)
func (p *providerWrapper) IsHealthy() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	// If provider instance is nil, provider is not healthy
	if p.provInstance == nil {
		return false
	}

	// Check if the provider instance is healthy
	if !p.provInstance.IsHealthy() {
		return false
	}

	// Provider is healthy if it has active instance
	return true
}
