//go:build integration && !localstack
// +build integration,!localstack

package protocol

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/config"
)

// TestProviderWrapper_RandomStringLifecycle tests random_string resource lifecycle
// This is an integration test as it interacts with a real provider process via go-plugin.
func TestProviderWrapper_RandomStringLifecycle(t *testing.T) {
	ctx := context.Background()

	// Create provider manager
	manager, err := NewProviderManagerWithOptions(
		WithBaseDir("/tmp/wrapper-lifecycle-test"),
		WithConfigSource(config.NewDefaultConfigSource()),
	)
	require.NoError(t, err, "Failed to create provider manager")
	defer manager.Close()

	// Get a provider through the manager
	provider, err := manager.GetProvider(ctx, "test-deployment-1", "random", "3.7.2", nil, "random_string")
	require.NoError(t, err, "Failed to get random provider")

	// Configure the provider
	err = provider.Configure(ctx, map[string]interface{}{})
	require.NoError(t, err, "Failed to configure provider")

	// 1. Create a resource
	createConfig := map[string]interface{}{
		"length":  int64(16),
		"special": false,
		"upper":   true,
		"lower":   true,
		"number":  true,
	}

	createdState, err := provider.CreateResource(ctx, "random_string", createConfig)
	require.NoError(t, err, "CreateResource should succeed")
	require.NotNil(t, createdState, "Should have created state")

	// Verify the result contains expected fields
	assert.Contains(t, createdState, "result", "Should have result field")
	assert.Contains(t, createdState, "length", "Should have length field")
	assert.Equal(t, int64(16), createdState["length"], "Length should be 16")

	resultStr, ok := createdState["result"].(string)
	assert.True(t, ok, "Result should be a string")
	assert.Len(t, resultStr, 16, "Result string should be 16 characters")

	t.Logf("✅ Created random_string with result: %s", resultStr)

	// 2. Update the resource (change length)
	updateConfig := map[string]interface{}{
		"length":  int64(24), // Changed from 16 to 24
		"special": false,
		"upper":   true,
		"lower":   true,
		"number":  true,
	}

	updatedState, err := provider.UpdateResource(ctx, "random_string", createdState, updateConfig)
	require.NoError(t, err, "UpdateResource should succeed")
	require.NotNil(t, updatedState, "Should have updated state")

	// Verify the update caused a replacement (random_string replaces on any change)
	assert.Equal(t, int64(24), updatedState["length"], "Length should be updated to 24")
	newResultStr, ok := updatedState["result"].(string)
	assert.True(t, ok, "Result should be a string")
	assert.Len(t, newResultStr, 24, "New result string should be 24 characters")
	assert.NotEqual(t, resultStr, newResultStr, "Result should have changed (replacement)")

	t.Logf("✅ Updated random_string with new result: %s", newResultStr)

	// 3. Delete the resource
	err = provider.DeleteResource(ctx, "random_string", updatedState)
	require.NoError(t, err, "DeleteResource should succeed")

	t.Log("✅ Successfully deleted random_string")

	// Verify UnifiedProviderClient was used
	wrapper, ok := provider.(*providerWrapper)
	require.True(t, ok, "Provider should be providerWrapper")
	assert.NotNil(t, wrapper.unifiedClient, "UnifiedClient should be initialized")

	t.Log("✅ Full resource lifecycle completed using UnifiedProviderClient")
}
