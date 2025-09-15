package protocol

import (
	"context"
	"fmt"

	"github.com/lattiam/lattiam/internal/interfaces"
)

// createProviderClient creates a UnifiedProviderClient for the provider instance.
// This is used when the unified_client build tag is present.
func (pm *ProviderManager) createProviderClient(ctx context.Context, provInstance ProviderInstanceInterface, name string, config interfaces.ProviderConfig) (*providerWrapper, error) {
	// Type assert to get concrete *PluginProviderInstance
	pluginInstance, ok := provInstance.(*PluginProviderInstance)
	if !ok {
		return nil, fmt.Errorf("expected *PluginProviderInstance, got %T", provInstance)
	}

	// Create UnifiedProviderClient directly - no legacy client
	unifiedClient, err := NewUnifiedProviderClient(ctx, pluginInstance)
	if err != nil {
		return nil, fmt.Errorf("failed to create unified provider client: %w", err)
	}

	// Create wrapper using constructor (ignores legacy client parameter)
	wrapper := newProviderWrapper(nil, provInstance)
	wrapper.unifiedClient = unifiedClient

	// Configure the provider if configuration is provided
	if len(config) > 0 {
		if providerConfig, exists := config[name]; exists {
			if err := wrapper.Configure(ctx, providerConfig); err != nil {
				// If configuration fails, clean up the provider
				_ = unifiedClient.Close()
				return nil, fmt.Errorf("failed to configure provider: %w", err)
			}
		}
	}

	return wrapper, nil
}
