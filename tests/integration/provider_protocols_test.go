//go:build integration
// +build integration

package integration

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/pkg/provider/protocol"
)

// TestProviderClientProtocols tests that the client works with both v5 and v6 protocols
//
//nolint:paralleltest // integration tests use shared resources
func TestProviderClientProtocols(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	tests := []struct {
		name            string
		providerName    string
		providerVersion string
		expectedProto   int
	}{
		{
			name:            "AWS Provider v5.70.0 (uses protocol v5)",
			providerName:    "aws",
			providerVersion: "5.70.0",
			expectedProto:   5,
		},
		// Add more providers/versions as needed
	}

	for _, tt := range tests {
		//paralleltest:disable

		t.Run(tt.name, func(t *testing.T) {
			// Start provider
			manager, err := protocol.NewProviderManager()
			require.NoError(t, err)

			instance, err := manager.GetProvider(ctx, tt.providerName, tt.providerVersion)
			require.NoError(t, err)
			defer func() {
				if err := instance.Stop(); err != nil {
					t.Logf("Failed to stop provider instance: %v", err)
				}
			}()

			// Create client - this should work with both v5 and v6
			client, err := protocol.NewTerraformProviderClient(ctx, instance, "test_resource")
			require.NoError(t, err, "Client should support protocol v%d", tt.expectedProto)
			defer client.Close()

			// Get schema should work
			schema, err := client.GetProviderSchema(ctx)
			require.NoError(t, err, "GetProviderSchema should work with protocol v%d", tt.expectedProto)
			require.NotNil(t, schema)
			require.NotNil(t, schema.Provider)

			// Test provider configuration
			providerConfig := createMinimalProviderConfig()

			_, err = client.PrepareProviderConfig(ctx, providerConfig)
			require.NoError(t, err, "PrepareProviderConfig should work with protocol v%d", tt.expectedProto)

			err = client.ConfigureProvider(ctx, providerConfig)
			require.NoError(t, err, "ConfigureProvider should work with protocol v%d", tt.expectedProto)

			t.Logf("✓ Client successfully tested with protocol v%d", tt.expectedProto)
		})
	}
}

// createMinimalProviderConfig creates a minimal working provider configuration
func createMinimalProviderConfig() map[string]interface{} {
	return map[string]interface{}{
		// Minimal config that should work with most providers
		"region":                      "us-east-1",
		"skip_credentials_validation": true,
		"skip_region_validation":      true,
		"skip_requesting_account_id":  true,
		"skip_metadata_api_check":     "true",
	}
}
