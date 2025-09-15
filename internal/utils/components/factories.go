// Package components provides factory functions for creating system components and utilities.
package components

import (
	"fmt"

	"github.com/lattiam/lattiam/internal/config"
	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/internal/system"
)

// CreateStateStore creates a state store based on configuration
func CreateStateStore(cfg *config.ServerConfig) (interfaces.StateStore, error) {
	factory := system.NewDefaultComponentFactory()
	stateConfig := interfaces.StateStoreConfig{
		Type: cfg.StateStore.Type,
		Options: map[string]interface{}{
			"path":          cfg.StateStore.File.Path,
			"server_config": cfg, // Pass the full server config for S3 configuration extraction
		},
	}
	stateStore, err := factory.CreateStateStore(stateConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create state store: %w", err)
	}
	return stateStore, nil
}

// CreateProviderManager creates a provider manager based on configuration
func CreateProviderManager(cfg *config.ServerConfig) (interfaces.ProviderLifecycleManager, error) {
	factory := system.NewDefaultComponentFactory()
	providerConfig := interfaces.ProviderManagerConfig{
		Options: map[string]interface{}{
			"provider_dir": cfg.ProviderDir,
		},
	}
	manager, err := factory.CreateProviderLifecycleManager(providerConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create provider manager: %w", err)
	}
	return manager, nil
}

// CreateInterpolator creates an interpolation resolver based on configuration
func CreateInterpolator(_ *config.ServerConfig) (interfaces.InterpolationResolver, error) {
	factory := system.NewDefaultComponentFactory()
	interpolatorConfig := interfaces.InterpolationResolverConfig{
		// Add any configuration options if needed
		Options: map[string]interface{}{},
	}
	resolver, err := factory.CreateInterpolationResolver(interpolatorConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create interpolation resolver: %w", err)
	}
	return resolver, nil
}
