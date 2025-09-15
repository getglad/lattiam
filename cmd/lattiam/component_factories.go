package main

import (
	"fmt"

	"github.com/lattiam/lattiam/internal/config"
	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/internal/utils/components"
)

// createStateStore creates a state store based on configuration
func createStateStore(cfg *config.ServerConfig) (interfaces.StateStore, error) {
	stateStore, err := components.CreateStateStore(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create state store: %w", err)
	}
	return stateStore, nil
}

// createProviderManager creates a provider manager based on configuration
func createProviderManager(cfg *config.ServerConfig) (interfaces.ProviderLifecycleManager, error) {
	manager, err := components.CreateProviderManager(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create provider manager: %w", err)
	}
	return manager, nil
}

// createInterpolator creates an interpolation resolver based on configuration
func createInterpolator(cfg *config.ServerConfig) (interfaces.InterpolationResolver, error) {
	interpolator, err := components.CreateInterpolator(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create interpolator: %w", err)
	}
	return interpolator, nil
}
