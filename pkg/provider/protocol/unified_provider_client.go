//nolint:wrapcheck // These are adapter methods that pass through client errors transparently
package protocol

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-go/tfprotov5"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-mux/tf5to6server"

	"github.com/lattiam/lattiam/internal/proto/tfplugin5"
	tfplugin6 "github.com/lattiam/lattiam/internal/proto/tfplugin6"
)

// UnifiedProviderClient provides a unified interface to providers using official HashiCorp libraries
// It automatically handles v5 and v6 providers through a single v6 interface
//
// Architecture notes:
// - go-plugin gives us gRPC clients (tfplugin5.ProviderClient or tfplugin6.ProviderClient)
// - Our internal code and tf5to6server expect server interfaces (tfprotov5/6.ProviderServer)
// - The client-to-server adapters bridge this gap with minimal conversion
// - For v5 providers: client → v5 adapter → tf5to6server → v6 interface
// - For v6 providers: client → v6 adapter → v6 interface

// UnifiedProviderClient provides a unified interface for interacting with Terraform providers
type UnifiedProviderClient struct {
	providerName    string
	providerVersion string
	v6Server        tfprotov6.ProviderServer // Unified v6 interface
	instance        *PluginProviderInstance  // Keep reference to the plugin instance
}

// NewUnifiedProviderClient creates a new client using the official libraries
func NewUnifiedProviderClient(ctx context.Context, instance *PluginProviderInstance) (*UnifiedProviderClient, error) {
	client := &UnifiedProviderClient{
		providerName:    instance.Name(),
		providerVersion: instance.Version(),
		instance:        instance,
	}

	// Detect protocol version and create appropriate server
	switch instance.GetProtocolVersion() {
	case 5:
		// V5 provider - create adapter and wrap with tf5to6server
		v5ClientInterface := instance.GetGRPCClientV5()
		if v5ClientInterface == nil {
			return nil, fmt.Errorf("v5 client is nil for provider %s", instance.Name())
		}

		// Type assert to get the actual client
		v5Client, ok := v5ClientInterface.(tfplugin5.ProviderClient)
		if !ok {
			return nil, fmt.Errorf("v5 client is not of expected type for provider %s", instance.Name())
		}

		// Create client-to-server adapter
		v5ServerAdapter := &v5ClientToServerAdapterComplete{
			client: v5Client,
		}

		// Wrap with tf5to6server
		v6Server, err := tf5to6server.UpgradeServer(ctx, func() tfprotov5.ProviderServer {
			return v5ServerAdapter
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create tf5to6server adapter: %w", err)
		}

		client.v6Server = v6Server
	case 6:
		// V6 provider - create server adapter directly
		v6ClientInterface := instance.GetGRPCClientV6()
		if v6ClientInterface == nil {
			return nil, fmt.Errorf("v6 client is nil for provider %s", instance.Name())
		}

		// Type assert to get the actual client
		v6Client, ok := v6ClientInterface.(tfplugin6.ProviderClient)
		if !ok {
			return nil, fmt.Errorf("v6 client is not of expected type for provider %s", instance.Name())
		}

		// Create v6 client-to-server adapter
		client.v6Server = &v6ClientToServerAdapter{
			client: v6Client,
		}
	default:
		return nil, fmt.Errorf("unsupported protocol version %d for provider %s",
			instance.GetProtocolVersion(), instance.Name())
	}

	return client, nil
}

// GetProviderSchema returns the provider schema
func (c *UnifiedProviderClient) GetProviderSchema(ctx context.Context) (*tfprotov6.GetProviderSchemaResponse, error) {
	return c.v6Server.GetProviderSchema(ctx, &tfprotov6.GetProviderSchemaRequest{})
}

// ValidateProviderConfig validates the provider configuration
func (c *UnifiedProviderClient) ValidateProviderConfig(ctx context.Context, req *tfprotov6.ValidateProviderConfigRequest) (*tfprotov6.ValidateProviderConfigResponse, error) {
	return c.v6Server.ValidateProviderConfig(ctx, req)
}

// ConfigureProvider configures the provider
func (c *UnifiedProviderClient) ConfigureProvider(ctx context.Context, req *tfprotov6.ConfigureProviderRequest) (*tfprotov6.ConfigureProviderResponse, error) {
	return c.v6Server.ConfigureProvider(ctx, req)
}

// ValidateResourceConfig validates a resource configuration
func (c *UnifiedProviderClient) ValidateResourceConfig(ctx context.Context, req *tfprotov6.ValidateResourceConfigRequest) (*tfprotov6.ValidateResourceConfigResponse, error) {
	return c.v6Server.ValidateResourceConfig(ctx, req)
}

// UpgradeResourceState upgrades resource state
func (c *UnifiedProviderClient) UpgradeResourceState(ctx context.Context, req *tfprotov6.UpgradeResourceStateRequest) (*tfprotov6.UpgradeResourceStateResponse, error) {
	return c.v6Server.UpgradeResourceState(ctx, req)
}

// ReadResource reads a resource
func (c *UnifiedProviderClient) ReadResource(ctx context.Context, req *tfprotov6.ReadResourceRequest) (*tfprotov6.ReadResourceResponse, error) {
	return c.v6Server.ReadResource(ctx, req)
}

// PlanResourceChange plans changes to a resource
func (c *UnifiedProviderClient) PlanResourceChange(ctx context.Context, req *tfprotov6.PlanResourceChangeRequest) (*tfprotov6.PlanResourceChangeResponse, error) {
	return c.v6Server.PlanResourceChange(ctx, req)
}

// ApplyResourceChange applies changes to a resource
func (c *UnifiedProviderClient) ApplyResourceChange(ctx context.Context, req *tfprotov6.ApplyResourceChangeRequest) (*tfprotov6.ApplyResourceChangeResponse, error) {
	return c.v6Server.ApplyResourceChange(ctx, req)
}

// ImportResourceState imports resource state
func (c *UnifiedProviderClient) ImportResourceState(ctx context.Context, req *tfprotov6.ImportResourceStateRequest) (*tfprotov6.ImportResourceStateResponse, error) {
	return c.v6Server.ImportResourceState(ctx, req)
}

// MoveResourceState moves resource state
func (c *UnifiedProviderClient) MoveResourceState(ctx context.Context, req *tfprotov6.MoveResourceStateRequest) (*tfprotov6.MoveResourceStateResponse, error) {
	return c.v6Server.MoveResourceState(ctx, req)
}

// ValidateDataResourceConfig validates a data source configuration
func (c *UnifiedProviderClient) ValidateDataResourceConfig(ctx context.Context, req *tfprotov6.ValidateDataResourceConfigRequest) (*tfprotov6.ValidateDataResourceConfigResponse, error) {
	return c.v6Server.ValidateDataResourceConfig(ctx, req)
}

// ReadDataSource reads a data source
func (c *UnifiedProviderClient) ReadDataSource(ctx context.Context, req *tfprotov6.ReadDataSourceRequest) (*tfprotov6.ReadDataSourceResponse, error) {
	return c.v6Server.ReadDataSource(ctx, req)
}

// GetFunctions returns provider functions
func (c *UnifiedProviderClient) GetFunctions(ctx context.Context, req *tfprotov6.GetFunctionsRequest) (*tfprotov6.GetFunctionsResponse, error) {
	return c.v6Server.GetFunctions(ctx, req)
}

// CallFunction calls a provider function
func (c *UnifiedProviderClient) CallFunction(ctx context.Context, req *tfprotov6.CallFunctionRequest) (*tfprotov6.CallFunctionResponse, error) {
	return c.v6Server.CallFunction(ctx, req)
}

// GetMetadata returns provider metadata
func (c *UnifiedProviderClient) GetMetadata(ctx context.Context, req *tfprotov6.GetMetadataRequest) (*tfprotov6.GetMetadataResponse, error) {
	return c.v6Server.GetMetadata(ctx, req)
}

// StopProvider stops the provider
func (c *UnifiedProviderClient) StopProvider(ctx context.Context, req *tfprotov6.StopProviderRequest) (*tfprotov6.StopProviderResponse, error) {
	return c.v6Server.StopProvider(ctx, req)
}

// Close closes the client connection
func (c *UnifiedProviderClient) Close() error {
	// The server doesn't need explicit closing, but we should stop the provider instance
	if c.instance != nil {
		return c.instance.Stop()
	}
	return nil
}

// IsHealthy checks if the provider is healthy
func (c *UnifiedProviderClient) IsHealthy() bool {
	if c.instance == nil {
		return false
	}
	return c.instance.IsHealthy()
}

// GetProviderName returns the provider name
func (c *UnifiedProviderClient) GetProviderName() string {
	return c.providerName
}

// GetProviderVersion returns the provider version
func (c *UnifiedProviderClient) GetProviderVersion() string {
	return c.providerVersion
}
