//go:build integration
// +build integration

package protocol

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/config"
)

// TestProviderProtocolVersions checks which protocol versions specific providers use
func TestProviderProtocolVersions(t *testing.T) {
	// Skip if we can't download providers
	if testing.Short() {
		t.Skip("Skipping provider protocol version test in short mode")
	}

	ctx := context.Background()

	tests := []struct {
		provider string
		version  string
	}{
		{"aws", "6.2.0"},
		{"aws", "5.31.0"},
		{"aws", "4.67.0"},
		{"random", "3.4.3"},
		{"random", "3.6.0"},
		{"random", "3.6.3"},
		{"tls", "4.0.0"},
		{"tls", "4.0.4"},
		{"tls", "4.0.5"},
		{"tls", "4.0.6"},
		{"http", "3.0.0"},
		{"http", "3.4.0"},
		{"http", "3.4.5"},
	}

	manager, err := NewProviderManagerWithOptions(
		WithBaseDir("/tmp/provider-version-test"),
		WithConfigSource(config.NewDefaultConfigSource()),
	)
	require.NoError(t, err)
	defer manager.Close()

	for _, tt := range tests {
		t.Run(tt.provider+"_"+tt.version, func(t *testing.T) {
			// Try to get the provider
			provider, err := manager.GetProvider(ctx, "test-deployment", tt.provider, tt.version, nil, "test_resource")
			if err != nil {
				t.Logf("Failed to get %s provider %s: %v", tt.provider, tt.version, err)
				t.Skip("Provider not available")
			}

			// Get the plugin instance to check protocol version
			wrapper, ok := provider.(*providerWrapper)
			require.True(t, ok, "Provider should be providerWrapper")

			instance, ok := wrapper.provInstance.(*PluginProviderInstance)
			require.True(t, ok, "Should be PluginProviderInstance")

			t.Logf("%s provider %s uses protocol v%d", tt.provider, tt.version, instance.GetProtocolVersion())
		})
	}
}
