package protocol

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/lattiam/lattiam/internal/proto/tfplugin5"
	"github.com/lattiam/lattiam/internal/proto/tfplugin6"
)

// PluginProviderInstance represents a provider plugin instance (unified version without terraformClient)
type PluginProviderInstance struct {
	name            string
	version         string
	path            string
	client          *plugin.Client
	grpcClientV5    tfplugin5.ProviderClient // Set if V5 provider
	grpcClientV6    tfplugin6.ProviderClient // Set if V6 provider
	protocolVersion int                      // Keep for logging/debugging
}

// NewPluginProviderInstance creates a new provider instance using go-plugin
func NewPluginProviderInstance(ctx context.Context, providerPath, name, version string) (*PluginProviderInstance, error) {
	return NewPluginProviderInstanceWithEnv(ctx, providerPath, name, version, nil)
}

// NewPluginProviderInstanceWithEnv creates a new provider instance with custom environment
//
//nolint:funlen,gocognit // Complex provider instance setup with environment configuration and process management
func NewPluginProviderInstanceWithEnv(_ context.Context, providerPath, name, version string, env []string) (*PluginProviderInstance, error) {
	// Set up the command with proper environment
	cmd := exec.Command(providerPath)

	// Use provided environment or default
	if env == nil {
		env = os.Environ()
		env = append(env, "TF_PLUGIN_MAGIC_COOKIE=d602bf8f470bc67ca7faa0386276bbdd4330efaf76d1a219cb4d6991ca9872b2")
	}

	// Add any AWS-specific environment variables if this is the AWS provider
	if name == "aws" {
		// Ensure AWS environment variables are passed through
		for _, envVar := range []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_DEFAULT_REGION", "AWS_ENDPOINT_URL"} {
			if val := os.Getenv(envVar); val != "" {
				found := false
				for _, e := range env {
					if strings.HasPrefix(e, envVar+"=") {
						found = true
						break
					}
				}
				if !found {
					env = append(env, envVar+"="+val)
				}
			}
		}
	}
	cmd.Env = env

	// Configure the plugin client
	client := plugin.NewClient(&plugin.ClientConfig{
		HandshakeConfig: plugin.HandshakeConfig{
			// Don't specify a protocol version here - let go-plugin negotiate
			MagicCookieKey:   "TF_PLUGIN_MAGIC_COOKIE",
			MagicCookieValue: "d602bf8f470bc67ca7faa0386276bbdd4330efaf76d1a219cb4d6991ca9872b2",
		},
		VersionedPlugins: map[int]plugin.PluginSet{
			5: {"provider": &TerraformPluginV5{}},
			6: {"provider": &TerraformPlugin{}},
		},
		Cmd:              cmd,
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolGRPC},
		GRPCDialOptions:  []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())},
		Managed:          true,
		Logger: hclog.New(&hclog.LoggerOptions{
			Level:      hclog.Error, // Only log errors to avoid noise
			Output:     os.Stderr,
			JSONFormat: false,
		}),
		// CRITICAL: This is what fixes the EOF errors!
		// Redirect stdout/stderr to avoid buffer blocking
		SyncStdout: io.Discard, // Discard provider stdout
		SyncStderr: io.Discard, // Discard provider stderr
	})

	// Connect via RPC
	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, fmt.Errorf("failed to connect to provider: %w", err)
	}

	// Request the plugin
	raw, err := rpcClient.Dispense("provider")
	if err != nil {
		client.Kill()
		return nil, fmt.Errorf("failed to dispense provider plugin: %w", err)
	}

	// The dispensed plugin could be either v5 or v6
	instance := &PluginProviderInstance{
		name:    name,
		version: version,
		path:    providerPath,
		client:  client,
	}

	// Check if it's v6
	if grpcClientV6, ok := raw.(tfplugin6.ProviderClient); ok {
		instance.grpcClientV6 = grpcClientV6
		instance.protocolVersion = 6
		GetDebugLogger().Logf("plugin", "Provider %s using protocol v6", name)
		return instance, nil
	}

	// Check if it's v5
	if grpcClientV5, ok := raw.(tfplugin5.ProviderClient); ok {
		instance.grpcClientV5 = grpcClientV5
		instance.protocolVersion = 5
		GetDebugLogger().Logf("plugin", "Provider %s using protocol v5", name)
		return instance, nil
	}

	client.Kill()
	return nil, fmt.Errorf("unexpected client type: %T", raw)
}

// Stop stops the provider process gracefully
func (p *PluginProviderInstance) Stop() error {
	// First, try to call the provider's Stop method if available
	// This allows the provider to gracefully shut down any in-flight operations
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	// For V5 providers
	if p.grpcClientV5 != nil {
		stopReq := &tfplugin5.Stop_Request{}
		if _, err := p.grpcClientV5.Stop(ctx, stopReq); err != nil {
			// Log but don't fail - provider might not support Stop
			GetDebugLogger().Logf("plugin", "Provider %s Stop RPC failed (this is normal for some providers): %v", p.name, err)
		} else {
			GetDebugLogger().Logf("plugin", "Provider %s Stop RPC succeeded", p.name)
		}
	}

	// For V6 providers
	if p.grpcClientV6 != nil {
		stopReq := &tfplugin6.StopProvider_Request{}
		if _, err := p.grpcClientV6.StopProvider(ctx, stopReq); err != nil {
			// Log but don't fail - provider might not support StopProvider
			GetDebugLogger().Logf("plugin", "Provider %s StopProvider RPC failed (this is normal for some providers): %v", p.name, err)
		} else {
			GetDebugLogger().Logf("plugin", "Provider %s StopProvider RPC succeeded", p.name)
		}
	}

	// Give the provider a moment to clean up after Stop
	time.Sleep(100 * time.Millisecond)

	// Now kill the process
	p.client.Kill()

	// Wait for the process to actually exit
	// The go-plugin library doesn't provide a direct wait method,
	// but checking Exited() in a loop with a timeout ensures cleanup
	for i := 0; i < 50; i++ { // 5 second timeout (50 * 100ms)
		if p.client.Exited() {
			return nil
		}
		// Small delay before checking again
		time.Sleep(100 * time.Millisecond)
	}

	// If still not exited after timeout, return error but process should be killed
	return fmt.Errorf("provider process did not exit cleanly within timeout")
}

// IsHealthy checks if the provider process is still running
func (p *PluginProviderInstance) IsHealthy() bool {
	return !p.client.Exited()
}

// GetProtocolVersion returns the protocol version used by this provider
func (p *PluginProviderInstance) GetProtocolVersion() int {
	return p.protocolVersion
}

// Name returns the provider name
func (p *PluginProviderInstance) Name() string {
	return p.name
}

// Version returns the provider version
func (p *PluginProviderInstance) Version() string {
	return p.version
}

// Path returns the provider binary path
func (p *PluginProviderInstance) Path() string {
	return p.path
}
