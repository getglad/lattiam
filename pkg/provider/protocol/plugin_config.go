package protocol

import (
	"context"

	"github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"

	"github.com/lattiam/lattiam/internal/proto/tfplugin5"
	"github.com/lattiam/lattiam/internal/proto/tfplugin6"
)

// HandshakeConfig is the handshake configuration for Terraform plugins
var HandshakeConfig = plugin.HandshakeConfig{
	ProtocolVersion:  5, // Support both v5 and v6
	MagicCookieKey:   "TF_PLUGIN_MAGIC_COOKIE",
	MagicCookieValue: "d602bf8f470bc67ca7faa0386276bbdd4330efaf76d1a219cb4d6991ca9872b2",
}

// TerraformPluginV5 implements plugin.GRPCPlugin interface for Terraform v5 providers
type TerraformPluginV5 struct {
	plugin.Plugin
}

// GRPCServer is not needed for client-only usage
func (p *TerraformPluginV5) GRPCServer(_ *plugin.GRPCBroker, _ *grpc.Server) error {
	return nil
}

// GRPCClient returns the interface implementation for the plugin
func (p *TerraformPluginV5) GRPCClient(_ context.Context, _ *plugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return tfplugin5.NewProviderClient(c), nil
}

// TerraformPlugin implements plugin.GRPCPlugin interface for Terraform v6 providers
type TerraformPlugin struct {
	plugin.Plugin
}

// GRPCServer is not needed for client-only usage
func (p *TerraformPlugin) GRPCServer(_ *plugin.GRPCBroker, _ *grpc.Server) error {
	return nil
}

// GRPCClient returns the interface implementation for the plugin
func (p *TerraformPlugin) GRPCClient(_ context.Context, _ *plugin.GRPCBroker, c *grpc.ClientConn) (interface{}, error) {
	return tfplugin6.NewProviderClient(c), nil
}
