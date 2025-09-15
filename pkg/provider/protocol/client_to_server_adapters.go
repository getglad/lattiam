//nolint:wrapcheck // These are adapter methods that pass through client errors transparently
package protocol

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-go/tfprotov5"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/lattiam/lattiam/internal/proto/tfplugin5"
	"github.com/lattiam/lattiam/internal/proto/tfplugin6"
)

// v5ClientToServerAdapterComplete adapts a tfplugin5.ProviderClient to tfprotov5.ProviderServer
// This adapter is necessary because:
// - go-plugin gives us a gRPC client (tfplugin5.ProviderClient) to talk to the provider
// - tf5to6server expects a server interface (tfprotov5.ProviderServer) as input
// - This adapter bridges the gap with minimal conversion between proto and tfprotov5 types
//
// The adapter is intentionally minimal - it only converts types, no business logic.
type v5ClientToServerAdapterComplete struct {
	tfprotov5.ProviderServer
	client tfplugin5.ProviderClient
}

// GetProviderSchema implements tfprotov5.ProviderServer
func (a *v5ClientToServerAdapterComplete) GetProviderSchema(ctx context.Context, _ *tfprotov5.GetProviderSchemaRequest) (*tfprotov5.GetProviderSchemaResponse, error) {
	// Call the client
	clientResp, err := a.client.GetSchema(ctx, &tfplugin5.GetProviderSchema_Request{})
	if err != nil {
		return nil, err
	}

	// Convert response
	resp := &tfprotov5.GetProviderSchemaResponse{
		ResourceSchemas:   make(map[string]*tfprotov5.Schema),
		DataSourceSchemas: make(map[string]*tfprotov5.Schema),
	}

	// Convert provider schema
	if clientResp.Provider != nil {
		resp.Provider = convertSchemaFromProtoV5(clientResp.Provider)
	}

	// Convert resource schemas
	for name, schema := range clientResp.ResourceSchemas {
		resp.ResourceSchemas[name] = convertSchemaFromProtoV5(schema)
	}

	// Convert data source schemas
	for name, schema := range clientResp.DataSourceSchemas {
		resp.DataSourceSchemas[name] = convertSchemaFromProtoV5(schema)
	}

	// Convert diagnostics
	resp.Diagnostics = convertDiagnosticsFromProtoV5(clientResp.Diagnostics)

	// Convert server capabilities
	if clientResp.ServerCapabilities != nil {
		resp.ServerCapabilities = &tfprotov5.ServerCapabilities{
			PlanDestroy:               clientResp.ServerCapabilities.PlanDestroy,
			GetProviderSchemaOptional: clientResp.ServerCapabilities.GetProviderSchemaOptional,
		}
	}

	// Convert functions
	if clientResp.Functions != nil {
		resp.Functions = make(map[string]*tfprotov5.Function)
		for name, fn := range clientResp.Functions {
			resp.Functions[name] = convertFunctionFromProtoV5(fn)
		}
	}

	return resp, nil
}

// PrepareProviderConfig implements tfprotov5.ProviderServer
func (a *v5ClientToServerAdapterComplete) PrepareProviderConfig(_ context.Context, req *tfprotov5.PrepareProviderConfigRequest) (*tfprotov5.PrepareProviderConfigResponse, error) {
	// V5 providers don't have PrepareProviderConfig, return the config as-is
	return &tfprotov5.PrepareProviderConfigResponse{
		PreparedConfig: req.Config,
	}, nil
}

// ConfigureProvider implements tfprotov5.ProviderServer
func (a *v5ClientToServerAdapterComplete) ConfigureProvider(ctx context.Context, req *tfprotov5.ConfigureProviderRequest) (*tfprotov5.ConfigureProviderResponse, error) {
	clientReq := &tfplugin5.Configure_Request{
		Config: &tfplugin5.DynamicValue{
			Msgpack: req.Config.MsgPack,
			Json:    req.Config.JSON,
		},
	}

	clientResp, err := a.client.Configure(ctx, clientReq)
	if err != nil {
		return nil, err
	}

	return &tfprotov5.ConfigureProviderResponse{
		Diagnostics: convertDiagnosticsFromProtoV5(clientResp.Diagnostics),
	}, nil
}

// StopProvider implements tfprotov5.ProviderServer
func (a *v5ClientToServerAdapterComplete) StopProvider(ctx context.Context, _ *tfprotov5.StopProviderRequest) (*tfprotov5.StopProviderResponse, error) {
	clientResp, err := a.client.Stop(ctx, &tfplugin5.Stop_Request{})
	if err != nil {
		// Stop errors are typically not critical
		//nolint:nilerr // Intentionally ignoring stop errors as they are not critical
		return &tfprotov5.StopProviderResponse{}, nil
	}

	return &tfprotov5.StopProviderResponse{
		Error: clientResp.Error,
	}, nil
}

// ValidateResourceTypeConfig implements tfprotov5.ProviderServer
func (a *v5ClientToServerAdapterComplete) ValidateResourceTypeConfig(ctx context.Context, req *tfprotov5.ValidateResourceTypeConfigRequest) (*tfprotov5.ValidateResourceTypeConfigResponse, error) {
	clientReq := &tfplugin5.ValidateResourceTypeConfig_Request{
		TypeName: req.TypeName,
		Config: &tfplugin5.DynamicValue{
			Msgpack: req.Config.MsgPack,
			Json:    req.Config.JSON,
		},
	}

	clientResp, err := a.client.ValidateResourceTypeConfig(ctx, clientReq)
	if err != nil {
		return nil, err
	}

	return &tfprotov5.ValidateResourceTypeConfigResponse{
		Diagnostics: convertDiagnosticsFromProtoV5(clientResp.Diagnostics),
	}, nil
}

// UpgradeResourceState implements tfprotov5.ProviderServer
func (a *v5ClientToServerAdapterComplete) UpgradeResourceState(ctx context.Context, req *tfprotov5.UpgradeResourceStateRequest) (*tfprotov5.UpgradeResourceStateResponse, error) {
	clientReq := &tfplugin5.UpgradeResourceState_Request{
		TypeName: req.TypeName,
		Version:  req.Version,
		RawState: &tfplugin5.RawState{
			Json:    req.RawState.JSON,
			Flatmap: req.RawState.Flatmap,
		},
	}

	clientResp, err := a.client.UpgradeResourceState(ctx, clientReq)
	if err != nil {
		return nil, err
	}

	resp := &tfprotov5.UpgradeResourceStateResponse{
		Diagnostics: convertDiagnosticsFromProtoV5(clientResp.Diagnostics),
	}

	if clientResp.UpgradedState != nil {
		resp.UpgradedState = &tfprotov5.DynamicValue{
			MsgPack: clientResp.UpgradedState.Msgpack,
			JSON:    clientResp.UpgradedState.Json,
		}
	}

	return resp, nil
}

// ReadResource implements tfprotov5.ProviderServer
func (a *v5ClientToServerAdapterComplete) ReadResource(ctx context.Context, req *tfprotov5.ReadResourceRequest) (*tfprotov5.ReadResourceResponse, error) {
	clientReq := &tfplugin5.ReadResource_Request{
		TypeName: req.TypeName,
		CurrentState: &tfplugin5.DynamicValue{
			Msgpack: req.CurrentState.MsgPack,
			Json:    req.CurrentState.JSON,
		},
		Private:      req.Private,
		ProviderMeta: convertDynamicValueToProtoV5(req.ProviderMeta),
	}

	clientResp, err := a.client.ReadResource(ctx, clientReq)
	if err != nil {
		return nil, err
	}

	resp := &tfprotov5.ReadResourceResponse{
		Diagnostics: convertDiagnosticsFromProtoV5(clientResp.Diagnostics),
		Private:     clientResp.Private,
	}

	if clientResp.NewState != nil {
		resp.NewState = &tfprotov5.DynamicValue{
			MsgPack: clientResp.NewState.Msgpack,
			JSON:    clientResp.NewState.Json,
		}
	}

	return resp, nil
}

// PlanResourceChange implements tfprotov5.ProviderServer
func (a *v5ClientToServerAdapterComplete) PlanResourceChange(ctx context.Context, req *tfprotov5.PlanResourceChangeRequest) (*tfprotov5.PlanResourceChangeResponse, error) {
	clientReq := &tfplugin5.PlanResourceChange_Request{
		TypeName:         req.TypeName,
		PriorState:       convertDynamicValueToProtoV5(req.PriorState),
		ProposedNewState: convertDynamicValueToProtoV5(req.ProposedNewState),
		Config:           convertDynamicValueToProtoV5(req.Config),
		PriorPrivate:     req.PriorPrivate,
		ProviderMeta:     convertDynamicValueToProtoV5(req.ProviderMeta),
	}

	clientResp, err := a.client.PlanResourceChange(ctx, clientReq)
	if err != nil {
		return nil, err
	}

	resp := &tfprotov5.PlanResourceChangeResponse{
		Diagnostics:    convertDiagnosticsFromProtoV5(clientResp.Diagnostics),
		PlannedPrivate: clientResp.PlannedPrivate,
	}

	if clientResp.PlannedState != nil {
		resp.PlannedState = &tfprotov5.DynamicValue{
			MsgPack: clientResp.PlannedState.Msgpack,
			JSON:    clientResp.PlannedState.Json,
		}
	}

	// Convert RequiresReplace attribute paths
	if len(clientResp.RequiresReplace) > 0 {
		resp.RequiresReplace = make([]*tftypes.AttributePath, len(clientResp.RequiresReplace))
		for i, path := range clientResp.RequiresReplace {
			resp.RequiresReplace[i] = convertAttributePathFromProtoV5(path)
		}
	}

	return resp, nil
}

// ApplyResourceChange implements tfprotov5.ProviderServer
func (a *v5ClientToServerAdapterComplete) ApplyResourceChange(ctx context.Context, req *tfprotov5.ApplyResourceChangeRequest) (*tfprotov5.ApplyResourceChangeResponse, error) {
	clientReq := &tfplugin5.ApplyResourceChange_Request{
		TypeName:       req.TypeName,
		PriorState:     convertDynamicValueToProtoV5(req.PriorState),
		PlannedState:   convertDynamicValueToProtoV5(req.PlannedState),
		Config:         convertDynamicValueToProtoV5(req.Config),
		PlannedPrivate: req.PlannedPrivate,
		ProviderMeta:   convertDynamicValueToProtoV5(req.ProviderMeta),
	}

	clientResp, err := a.client.ApplyResourceChange(ctx, clientReq)
	if err != nil {
		return nil, err
	}

	resp := &tfprotov5.ApplyResourceChangeResponse{
		Diagnostics: convertDiagnosticsFromProtoV5(clientResp.Diagnostics),
		Private:     clientResp.Private,
	}

	if clientResp.NewState != nil {
		resp.NewState = &tfprotov5.DynamicValue{
			MsgPack: clientResp.NewState.Msgpack,
			JSON:    clientResp.NewState.Json,
		}
	}

	return resp, nil
}

// ImportResourceState implements tfprotov5.ProviderServer
func (a *v5ClientToServerAdapterComplete) ImportResourceState(ctx context.Context, req *tfprotov5.ImportResourceStateRequest) (*tfprotov5.ImportResourceStateResponse, error) {
	clientReq := &tfplugin5.ImportResourceState_Request{
		TypeName: req.TypeName,
		Id:       req.ID,
	}

	clientResp, err := a.client.ImportResourceState(ctx, clientReq)
	if err != nil {
		return nil, err
	}

	resp := &tfprotov5.ImportResourceStateResponse{
		Diagnostics: convertDiagnosticsFromProtoV5(clientResp.Diagnostics),
	}

	// Convert imported resources
	if len(clientResp.ImportedResources) > 0 {
		resp.ImportedResources = make([]*tfprotov5.ImportedResource, len(clientResp.ImportedResources))
		for i, imported := range clientResp.ImportedResources {
			resp.ImportedResources[i] = &tfprotov5.ImportedResource{
				TypeName: imported.TypeName,
				Private:  imported.Private,
			}
			if imported.State != nil {
				resp.ImportedResources[i].State = &tfprotov5.DynamicValue{
					MsgPack: imported.State.Msgpack,
					JSON:    imported.State.Json,
				}
			}
		}
	}

	return resp, nil
}

// ValidateDataSourceConfig implements tfprotov5.ProviderServer
func (a *v5ClientToServerAdapterComplete) ValidateDataSourceConfig(ctx context.Context, req *tfprotov5.ValidateDataSourceConfigRequest) (*tfprotov5.ValidateDataSourceConfigResponse, error) {
	clientReq := &tfplugin5.ValidateDataSourceConfig_Request{
		TypeName: req.TypeName,
		Config: &tfplugin5.DynamicValue{
			Msgpack: req.Config.MsgPack,
			Json:    req.Config.JSON,
		},
	}

	clientResp, err := a.client.ValidateDataSourceConfig(ctx, clientReq)
	if err != nil {
		return nil, err
	}

	return &tfprotov5.ValidateDataSourceConfigResponse{
		Diagnostics: convertDiagnosticsFromProtoV5(clientResp.Diagnostics),
	}, nil
}

// ReadDataSource implements tfprotov5.ProviderServer
func (a *v5ClientToServerAdapterComplete) ReadDataSource(ctx context.Context, req *tfprotov5.ReadDataSourceRequest) (*tfprotov5.ReadDataSourceResponse, error) {
	clientReq := &tfplugin5.ReadDataSource_Request{
		TypeName:     req.TypeName,
		Config:       convertDynamicValueToProtoV5(req.Config),
		ProviderMeta: convertDynamicValueToProtoV5(req.ProviderMeta),
	}

	clientResp, err := a.client.ReadDataSource(ctx, clientReq)
	if err != nil {
		return nil, err
	}

	resp := &tfprotov5.ReadDataSourceResponse{
		Diagnostics: convertDiagnosticsFromProtoV5(clientResp.Diagnostics),
	}

	if clientResp.State != nil {
		resp.State = &tfprotov5.DynamicValue{
			MsgPack: clientResp.State.Msgpack,
			JSON:    clientResp.State.Json,
		}
	}

	return resp, nil
}

// GetFunctions implements tfprotov5.ProviderServer
func (a *v5ClientToServerAdapterComplete) GetFunctions(_ context.Context, _ *tfprotov5.GetFunctionsRequest) (*tfprotov5.GetFunctionsResponse, error) {
	// V5 providers don't support functions
	return &tfprotov5.GetFunctionsResponse{}, nil
}

// CallFunction implements tfprotov5.ProviderServer
func (a *v5ClientToServerAdapterComplete) CallFunction(_ context.Context, _ *tfprotov5.CallFunctionRequest) (*tfprotov5.CallFunctionResponse, error) {
	// V5 providers don't support functions
	return nil, fmt.Errorf("functions are not supported in protocol v5")
}

// v6ClientToServerAdapter adapts a tfplugin6.ProviderClient to tfprotov6.ProviderServer
// This adapter is necessary because:
// - go-plugin gives us a gRPC client (tfplugin6.ProviderClient) to talk to the provider
// - Our internal code expects a server interface (tfprotov6.ProviderServer)
// - This adapter bridges the gap with minimal conversion between proto and tfprotov6 types
//
// For v6 providers, we could potentially work directly with the client, but using
// the adapter keeps the architecture consistent with v5 providers and allows
// UnifiedProviderClient to work with a single interface type.
type v6ClientToServerAdapter struct {
	tfprotov6.ProviderServer
	client tfplugin6.ProviderClient
}

// GetProviderSchema implements tfprotov6.ProviderServer
func (a *v6ClientToServerAdapter) GetProviderSchema(ctx context.Context, _ *tfprotov6.GetProviderSchemaRequest) (*tfprotov6.GetProviderSchemaResponse, error) {
	clientResp, err := a.client.GetProviderSchema(ctx, &tfplugin6.GetProviderSchema_Request{})
	if err != nil {
		return nil, err
	}

	resp := &tfprotov6.GetProviderSchemaResponse{
		ResourceSchemas:   make(map[string]*tfprotov6.Schema),
		DataSourceSchemas: make(map[string]*tfprotov6.Schema),
	}

	// Convert provider schema
	if clientResp.Provider != nil {
		resp.Provider = convertSchemaFromProtoV6(clientResp.Provider)
	}

	// Convert resource schemas
	for name, schema := range clientResp.ResourceSchemas {
		resp.ResourceSchemas[name] = convertSchemaFromProtoV6(schema)
	}

	// Convert data source schemas
	for name, schema := range clientResp.DataSourceSchemas {
		resp.DataSourceSchemas[name] = convertSchemaFromProtoV6(schema)
	}

	// Convert diagnostics
	resp.Diagnostics = convertDiagnosticsFromProtoV6(clientResp.Diagnostics)

	// Convert server capabilities
	if clientResp.ServerCapabilities != nil {
		resp.ServerCapabilities = &tfprotov6.ServerCapabilities{
			PlanDestroy:               clientResp.ServerCapabilities.PlanDestroy,
			GetProviderSchemaOptional: clientResp.ServerCapabilities.GetProviderSchemaOptional,
		}
	}

	// Convert functions
	if clientResp.Functions != nil {
		resp.Functions = make(map[string]*tfprotov6.Function)
		for name, fn := range clientResp.Functions {
			resp.Functions[name] = convertFunctionFromProtoV6(fn)
		}
	}

	// Convert provider meta schema
	if clientResp.ProviderMeta != nil {
		resp.ProviderMeta = convertSchemaFromProtoV6(clientResp.ProviderMeta)
	}

	return resp, nil
}

// ValidateProviderConfig implements tfprotov6.ProviderServer
func (a *v6ClientToServerAdapter) ValidateProviderConfig(ctx context.Context, req *tfprotov6.ValidateProviderConfigRequest) (*tfprotov6.ValidateProviderConfigResponse, error) {
	clientReq := &tfplugin6.ValidateProviderConfig_Request{
		Config: &tfplugin6.DynamicValue{
			Msgpack: req.Config.MsgPack,
			Json:    req.Config.JSON,
		},
	}

	clientResp, err := a.client.ValidateProviderConfig(ctx, clientReq)
	if err != nil {
		return nil, err
	}

	return &tfprotov6.ValidateProviderConfigResponse{
		PreparedConfig: req.Config, // Echo back the config
		Diagnostics:    convertDiagnosticsFromProtoV6(clientResp.Diagnostics),
	}, nil
}

// ConfigureProvider implements tfprotov6.ProviderServer
func (a *v6ClientToServerAdapter) ConfigureProvider(ctx context.Context, req *tfprotov6.ConfigureProviderRequest) (*tfprotov6.ConfigureProviderResponse, error) {
	clientReq := &tfplugin6.ConfigureProvider_Request{
		Config: &tfplugin6.DynamicValue{
			Msgpack: req.Config.MsgPack,
			Json:    req.Config.JSON,
		},
	}

	clientResp, err := a.client.ConfigureProvider(ctx, clientReq)
	if err != nil {
		return nil, err
	}

	return &tfprotov6.ConfigureProviderResponse{
		Diagnostics: convertDiagnosticsFromProtoV6(clientResp.Diagnostics),
	}, nil
}

// StopProvider implements tfprotov6.ProviderServer
func (a *v6ClientToServerAdapter) StopProvider(ctx context.Context, _ *tfprotov6.StopProviderRequest) (*tfprotov6.StopProviderResponse, error) {
	clientResp, err := a.client.StopProvider(ctx, &tfplugin6.StopProvider_Request{})
	if err != nil {
		// Stop errors are typically not critical
		//nolint:nilerr // Intentionally ignoring stop errors as they are not critical
		return &tfprotov6.StopProviderResponse{}, nil
	}

	return &tfprotov6.StopProviderResponse{
		Error: clientResp.Error,
	}, nil
}

// ValidateResourceConfig implements tfprotov6.ProviderServer
func (a *v6ClientToServerAdapter) ValidateResourceConfig(ctx context.Context, req *tfprotov6.ValidateResourceConfigRequest) (*tfprotov6.ValidateResourceConfigResponse, error) {
	clientReq := &tfplugin6.ValidateResourceConfig_Request{
		TypeName: req.TypeName,
		Config: &tfplugin6.DynamicValue{
			Msgpack: req.Config.MsgPack,
			Json:    req.Config.JSON,
		},
	}

	clientResp, err := a.client.ValidateResourceConfig(ctx, clientReq)
	if err != nil {
		return nil, err
	}

	return &tfprotov6.ValidateResourceConfigResponse{
		Diagnostics: convertDiagnosticsFromProtoV6(clientResp.Diagnostics),
	}, nil
}

// UpgradeResourceState implements tfprotov6.ProviderServer
func (a *v6ClientToServerAdapter) UpgradeResourceState(ctx context.Context, req *tfprotov6.UpgradeResourceStateRequest) (*tfprotov6.UpgradeResourceStateResponse, error) {
	clientReq := &tfplugin6.UpgradeResourceState_Request{
		TypeName: req.TypeName,
		Version:  req.Version,
		RawState: &tfplugin6.RawState{
			Json:    req.RawState.JSON,
			Flatmap: req.RawState.Flatmap,
		},
	}

	clientResp, err := a.client.UpgradeResourceState(ctx, clientReq)
	if err != nil {
		return nil, err
	}

	resp := &tfprotov6.UpgradeResourceStateResponse{
		Diagnostics: convertDiagnosticsFromProtoV6(clientResp.Diagnostics),
	}

	if clientResp.UpgradedState != nil {
		resp.UpgradedState = &tfprotov6.DynamicValue{
			MsgPack: clientResp.UpgradedState.Msgpack,
			JSON:    clientResp.UpgradedState.Json,
		}
	}

	return resp, nil
}

// ReadResource implements tfprotov6.ProviderServer
func (a *v6ClientToServerAdapter) ReadResource(ctx context.Context, req *tfprotov6.ReadResourceRequest) (*tfprotov6.ReadResourceResponse, error) {
	clientReq := &tfplugin6.ReadResource_Request{
		TypeName: req.TypeName,
		CurrentState: &tfplugin6.DynamicValue{
			Msgpack: req.CurrentState.MsgPack,
			Json:    req.CurrentState.JSON,
		},
		Private: req.Private,
		ProviderMeta: &tfplugin6.DynamicValue{
			Msgpack: req.ProviderMeta.MsgPack,
			Json:    req.ProviderMeta.JSON,
		},
	}

	clientResp, err := a.client.ReadResource(ctx, clientReq)
	if err != nil {
		return nil, err
	}

	resp := &tfprotov6.ReadResourceResponse{
		Diagnostics: convertDiagnosticsFromProtoV6(clientResp.Diagnostics),
		Private:     clientResp.Private,
	}

	if clientResp.NewState != nil {
		resp.NewState = &tfprotov6.DynamicValue{
			MsgPack: clientResp.NewState.Msgpack,
			JSON:    clientResp.NewState.Json,
		}
	}

	return resp, nil
}

// PlanResourceChange implements tfprotov6.ProviderServer
func (a *v6ClientToServerAdapter) PlanResourceChange(ctx context.Context, req *tfprotov6.PlanResourceChangeRequest) (*tfprotov6.PlanResourceChangeResponse, error) {
	clientReq := &tfplugin6.PlanResourceChange_Request{
		TypeName: req.TypeName,
		Config: &tfplugin6.DynamicValue{
			Msgpack: req.Config.MsgPack,
			Json:    req.Config.JSON,
		},
		PriorState: &tfplugin6.DynamicValue{
			Msgpack: req.PriorState.MsgPack,
			Json:    req.PriorState.JSON,
		},
		ProposedNewState: &tfplugin6.DynamicValue{
			Msgpack: req.ProposedNewState.MsgPack,
			Json:    req.ProposedNewState.JSON,
		},
		PriorPrivate: req.PriorPrivate,
		ProviderMeta: &tfplugin6.DynamicValue{
			Msgpack: req.ProviderMeta.MsgPack,
			Json:    req.ProviderMeta.JSON,
		},
	}

	clientResp, err := a.client.PlanResourceChange(ctx, clientReq)
	if err != nil {
		return nil, err
	}

	resp := &tfprotov6.PlanResourceChangeResponse{
		Diagnostics:     convertDiagnosticsFromProtoV6(clientResp.Diagnostics),
		PlannedPrivate:  clientResp.PlannedPrivate,
		RequiresReplace: convertAttributePathsFromProtoV6(clientResp.RequiresReplace),
	}

	if clientResp.PlannedState != nil {
		resp.PlannedState = &tfprotov6.DynamicValue{
			MsgPack: clientResp.PlannedState.Msgpack,
			JSON:    clientResp.PlannedState.Json,
		}
	}

	return resp, nil
}

// ApplyResourceChange implements tfprotov6.ProviderServer
func (a *v6ClientToServerAdapter) ApplyResourceChange(ctx context.Context, req *tfprotov6.ApplyResourceChangeRequest) (*tfprotov6.ApplyResourceChangeResponse, error) {
	clientReq := &tfplugin6.ApplyResourceChange_Request{
		TypeName: req.TypeName,
		Config: &tfplugin6.DynamicValue{
			Msgpack: req.Config.MsgPack,
			Json:    req.Config.JSON,
		},
		PriorState: &tfplugin6.DynamicValue{
			Msgpack: req.PriorState.MsgPack,
			Json:    req.PriorState.JSON,
		},
		PlannedState: &tfplugin6.DynamicValue{
			Msgpack: req.PlannedState.MsgPack,
			Json:    req.PlannedState.JSON,
		},
		PlannedPrivate: req.PlannedPrivate,
		ProviderMeta: &tfplugin6.DynamicValue{
			Msgpack: req.ProviderMeta.MsgPack,
			Json:    req.ProviderMeta.JSON,
		},
	}

	clientResp, err := a.client.ApplyResourceChange(ctx, clientReq)
	if err != nil {
		return nil, err
	}

	resp := &tfprotov6.ApplyResourceChangeResponse{
		Diagnostics: convertDiagnosticsFromProtoV6(clientResp.Diagnostics),
		Private:     clientResp.Private,
	}

	if clientResp.NewState != nil {
		resp.NewState = &tfprotov6.DynamicValue{
			MsgPack: clientResp.NewState.Msgpack,
			JSON:    clientResp.NewState.Json,
		}
	}

	return resp, nil
}

// ImportResourceState implements tfprotov6.ProviderServer
func (a *v6ClientToServerAdapter) ImportResourceState(ctx context.Context, req *tfprotov6.ImportResourceStateRequest) (*tfprotov6.ImportResourceStateResponse, error) {
	clientReq := &tfplugin6.ImportResourceState_Request{
		TypeName: req.TypeName,
		Id:       req.ID,
	}

	clientResp, err := a.client.ImportResourceState(ctx, clientReq)
	if err != nil {
		return nil, err
	}

	resp := &tfprotov6.ImportResourceStateResponse{
		Diagnostics: convertDiagnosticsFromProtoV6(clientResp.Diagnostics),
	}

	// Convert imported resources
	if len(clientResp.ImportedResources) > 0 {
		resp.ImportedResources = make([]*tfprotov6.ImportedResource, len(clientResp.ImportedResources))
		for i, imported := range clientResp.ImportedResources {
			resp.ImportedResources[i] = &tfprotov6.ImportedResource{
				TypeName: imported.TypeName,
				Private:  imported.Private,
			}
			if imported.State != nil {
				resp.ImportedResources[i].State = &tfprotov6.DynamicValue{
					MsgPack: imported.State.Msgpack,
					JSON:    imported.State.Json,
				}
			}
		}
	}

	// Convert deferrals
	if clientResp.Deferred != nil {
		resp.Deferred = &tfprotov6.Deferred{
			Reason: tfprotov6.DeferredReason(clientResp.Deferred.Reason),
		}
	}

	return resp, nil
}

// MoveResourceState implements tfprotov6.ProviderServer
func (a *v6ClientToServerAdapter) MoveResourceState(ctx context.Context, req *tfprotov6.MoveResourceStateRequest) (*tfprotov6.MoveResourceStateResponse, error) {
	clientReq := &tfplugin6.MoveResourceState_Request{
		SourceTypeName:      req.SourceTypeName,
		SourceSchemaVersion: req.SourceSchemaVersion,
		SourceState: &tfplugin6.RawState{
			Json:    req.SourceState.JSON,
			Flatmap: req.SourceState.Flatmap,
		},
		TargetTypeName: req.TargetTypeName,
		SourcePrivate:  req.SourcePrivate,
	}

	clientResp, err := a.client.MoveResourceState(ctx, clientReq)
	if err != nil {
		return nil, err
	}

	resp := &tfprotov6.MoveResourceStateResponse{
		Diagnostics:   convertDiagnosticsFromProtoV6(clientResp.Diagnostics),
		TargetPrivate: clientResp.TargetPrivate,
	}

	if clientResp.TargetState != nil {
		resp.TargetState = &tfprotov6.DynamicValue{
			MsgPack: clientResp.TargetState.Msgpack,
			JSON:    clientResp.TargetState.Json,
		}
	}

	return resp, nil
}

// ValidateDataResourceConfig implements tfprotov6.ProviderServer
func (a *v6ClientToServerAdapter) ValidateDataResourceConfig(ctx context.Context, req *tfprotov6.ValidateDataResourceConfigRequest) (*tfprotov6.ValidateDataResourceConfigResponse, error) {
	clientReq := &tfplugin6.ValidateDataResourceConfig_Request{
		TypeName: req.TypeName,
		Config: &tfplugin6.DynamicValue{
			Msgpack: req.Config.MsgPack,
			Json:    req.Config.JSON,
		},
	}

	clientResp, err := a.client.ValidateDataResourceConfig(ctx, clientReq)
	if err != nil {
		return nil, err
	}

	return &tfprotov6.ValidateDataResourceConfigResponse{
		Diagnostics: convertDiagnosticsFromProtoV6(clientResp.Diagnostics),
	}, nil
}

// ReadDataSource implements tfprotov6.ProviderServer
func (a *v6ClientToServerAdapter) ReadDataSource(ctx context.Context, req *tfprotov6.ReadDataSourceRequest) (*tfprotov6.ReadDataSourceResponse, error) {
	clientReq := &tfplugin6.ReadDataSource_Request{
		TypeName: req.TypeName,
		Config: &tfplugin6.DynamicValue{
			Msgpack: req.Config.MsgPack,
			Json:    req.Config.JSON,
		},
		ProviderMeta: &tfplugin6.DynamicValue{
			Msgpack: req.ProviderMeta.MsgPack,
			Json:    req.ProviderMeta.JSON,
		},
	}

	// ClientCapabilities support may be added in future versions

	clientResp, err := a.client.ReadDataSource(ctx, clientReq)
	if err != nil {
		return nil, err
	}

	resp := &tfprotov6.ReadDataSourceResponse{
		Diagnostics: convertDiagnosticsFromProtoV6(clientResp.Diagnostics),
	}

	if clientResp.State != nil {
		resp.State = &tfprotov6.DynamicValue{
			MsgPack: clientResp.State.Msgpack,
			JSON:    clientResp.State.Json,
		}
	}

	// Convert deferrals
	if clientResp.Deferred != nil {
		resp.Deferred = &tfprotov6.Deferred{
			Reason: tfprotov6.DeferredReason(clientResp.Deferred.Reason),
		}
	}

	return resp, nil
}

// GetFunctions implements tfprotov6.ProviderServer
func (a *v6ClientToServerAdapter) GetFunctions(_ context.Context, _ *tfprotov6.GetFunctionsRequest) (*tfprotov6.GetFunctionsResponse, error) {
	// Functions are already retrieved as part of GetProviderSchema in v6
	// This is a separate method for future extensibility
	return &tfprotov6.GetFunctionsResponse{}, nil
}

// CallFunction implements tfprotov6.ProviderServer
func (a *v6ClientToServerAdapter) CallFunction(ctx context.Context, req *tfprotov6.CallFunctionRequest) (*tfprotov6.CallFunctionResponse, error) {
	clientReq := &tfplugin6.CallFunction_Request{
		Name:      req.Name,
		Arguments: make([]*tfplugin6.DynamicValue, len(req.Arguments)),
	}

	for i, arg := range req.Arguments {
		clientReq.Arguments[i] = &tfplugin6.DynamicValue{
			Msgpack: arg.MsgPack,
			Json:    arg.JSON,
		}
	}

	clientResp, err := a.client.CallFunction(ctx, clientReq)
	if err != nil {
		return nil, err
	}

	resp := &tfprotov6.CallFunctionResponse{}

	if clientResp.Result != nil {
		resp.Result = &tfprotov6.DynamicValue{
			MsgPack: clientResp.Result.Msgpack,
			JSON:    clientResp.Result.Json,
		}
	}

	return resp, nil
}

// GetMetadata implements tfprotov6.ProviderServer
func (a *v6ClientToServerAdapter) GetMetadata(ctx context.Context, _ *tfprotov6.GetMetadataRequest) (*tfprotov6.GetMetadataResponse, error) {
	clientResp, err := a.client.GetMetadata(ctx, &tfplugin6.GetMetadata_Request{})
	if err != nil {
		return nil, err
	}

	resp := &tfprotov6.GetMetadataResponse{
		ServerCapabilities: &tfprotov6.ServerCapabilities{
			PlanDestroy:               false, // Default to false unless provider specifies
			GetProviderSchemaOptional: false,
		},
		Diagnostics: convertDiagnosticsFromProtoV6(clientResp.Diagnostics),
		Resources:   make([]tfprotov6.ResourceMetadata, 0),
		DataSources: make([]tfprotov6.DataSourceMetadata, 0),
		Functions:   make([]tfprotov6.FunctionMetadata, 0),
	}

	// Convert server capabilities
	if clientResp.ServerCapabilities != nil {
		resp.ServerCapabilities.PlanDestroy = clientResp.ServerCapabilities.PlanDestroy
		resp.ServerCapabilities.GetProviderSchemaOptional = clientResp.ServerCapabilities.GetProviderSchemaOptional
	}

	// Convert resources
	for _, res := range clientResp.Resources {
		resp.Resources = append(resp.Resources, tfprotov6.ResourceMetadata{
			TypeName: res.TypeName,
		})
	}

	// Convert data sources
	for _, ds := range clientResp.DataSources {
		resp.DataSources = append(resp.DataSources, tfprotov6.DataSourceMetadata{
			TypeName: ds.TypeName,
		})
	}

	// Convert functions
	for _, fn := range clientResp.Functions {
		resp.Functions = append(resp.Functions, tfprotov6.FunctionMetadata{
			Name: fn.Name,
		})
	}

	return resp, nil
}
