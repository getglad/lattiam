//go:build integration
// +build integration

package system

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/interfaces"
)

func TestProviderManagerAdapter(t *testing.T) {
	t.Run("CreateProviderLifecycleManagerWithAdapter", func(t *testing.T) {
		// Create factory
		factory := NewDefaultComponentFactory()

		// Create provider manager config
		config := interfaces.ProviderManagerConfig{
			Options: map[string]interface{}{
				"provider_dir": "./test-providers",
			},
		}

		// Create provider lifecycle manager
		lifecycleManager, err := factory.CreateProviderLifecycleManager(config)
		require.NoError(t, err)
		require.NotNil(t, lifecycleManager)

		// Verify it's the real adapter, not a mock
		_, isAdapter := lifecycleManager.(*ProviderManagerAdapter)
		assert.True(t, isAdapter, "Expected ProviderManagerAdapter, got %T", lifecycleManager)

		// Initialize should work (ProviderManagerAdapter implements Initialize)
		if initializable, ok := lifecycleManager.(interface{ Initialize() error }); ok {
			err = initializable.Initialize()
			assert.NoError(t, err)
		}

		// IsHealthy should return true (ProviderManagerAdapter implements IsHealthy)
		if healthChecker, ok := lifecycleManager.(interface{ IsHealthy() bool }); ok {
			assert.True(t, healthChecker.IsHealthy())
		}

		// Shutdown should work (ProviderManagerAdapter implements Shutdown)
		if shutdownable, ok := lifecycleManager.(interface{ Shutdown() error }); ok {
			err = shutdownable.Shutdown()
			assert.NoError(t, err)
		}
	})

	t.Run("CreateProviderLifecycleManagerWithMock", func(t *testing.T) {
		// Create factory
		factory := NewDefaultComponentFactory()

		// Create provider manager with mock option
		config := interfaces.ProviderManagerConfig{
			Options: map[string]interface{}{
				"use_mock": true,
			},
		}

		// Create provider lifecycle manager with mock config
		lifecycleManager, err := factory.CreateProviderLifecycleManager(config)
		require.NoError(t, err)
		require.NotNil(t, lifecycleManager)

		// Verify it's a mock (not adapter)
		_, isAdapter := lifecycleManager.(*ProviderManagerAdapter)
		assert.False(t, isAdapter, "Expected mock, got ProviderManagerAdapter")
	})
}
