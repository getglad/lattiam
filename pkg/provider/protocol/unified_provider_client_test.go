//go:build integration
// +build integration

package protocol

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/config"
	"github.com/lattiam/lattiam/tests/helpers/localstack"
	"github.com/lattiam/lattiam/tests/testutil"
)

// TestUnifiedProviderClient_GetProviderSchema tests the GetProviderSchema method
func TestUnifiedProviderClient_GetProviderSchema(t *testing.T) {
	ctx := context.Background()

	// Create a unique temporary directory for this test
	tempDir, err := os.MkdirTemp("", "unified-client-schema-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create provider manager
	manager, err := NewProviderManagerWithOptions(
		WithBaseDir(tempDir),
		WithConfigSource(config.NewDefaultConfigSource()),
	)
	require.NoError(t, err, "Failed to create provider manager")
	defer manager.Close()

	t.Run("V5_Provider_Random", func(t *testing.T) {
		// Get a v5 provider instance
		provider, err := manager.GetProvider(ctx, "test-deployment-1", "random", "3.7.2", nil, "random_string")
		require.NoError(t, err, "Failed to get random provider")

		// Extract the plugin instance
		wrapper, ok := provider.(*providerWrapper)
		require.True(t, ok, "Provider should be providerWrapper")

		instance, ok := wrapper.provInstance.(*PluginProviderInstance)
		require.True(t, ok, "Should be PluginProviderInstance")

		// Verify protocol version
		protocolVersion := instance.GetProtocolVersion()
		t.Logf("Random provider 3.7.2 uses protocol version: %d", protocolVersion)

		// Create UnifiedProviderClient
		client, err := NewUnifiedProviderClient(ctx, instance)
		require.NoError(t, err, "Failed to create UnifiedProviderClient")
		defer client.Close()

		// Test GetProviderSchema
		resp, err := client.GetProviderSchema(ctx)
		require.NoError(t, err, "GetProviderSchema should succeed")

		// Validate response
		assert.NotNil(t, resp)
		assert.NotNil(t, resp.Provider)
		assert.Contains(t, resp.ResourceSchemas, "random_string")
		assert.Contains(t, resp.ResourceSchemas, "random_id")
		assert.Contains(t, resp.ResourceSchemas, "random_integer")
		assert.Contains(t, resp.ResourceSchemas, "random_password")
		assert.Contains(t, resp.ResourceSchemas, "random_pet")
		assert.Contains(t, resp.ResourceSchemas, "random_shuffle")
		assert.Contains(t, resp.ResourceSchemas, "random_uuid")

		t.Logf("✅ UnifiedProviderClient successfully retrieved schema from v5 provider")
		t.Logf("   Found %d resource schemas", len(resp.ResourceSchemas))
		t.Logf("   Found %d data source schemas", len(resp.DataSourceSchemas))
	})

	t.Run("V5_Provider_AWS", func(t *testing.T) {
		// Get AWS provider v6.2.0 (which is protocol v5)
		provider, err := manager.GetProvider(ctx, "test-deployment-2", "aws", "6.2.0", nil, "aws_s3_bucket")
		if err != nil {
			t.Skip("AWS provider not available")
		}

		// Extract the plugin instance
		wrapper, ok := provider.(*providerWrapper)
		require.True(t, ok, "Provider should be providerWrapper")

		instance, ok := wrapper.provInstance.(*PluginProviderInstance)
		require.True(t, ok, "Should be PluginProviderInstance")

		// Verify it's a v5 provider
		assert.Equal(t, 5, instance.GetProtocolVersion())

		// Create UnifiedProviderClient
		client, err := NewUnifiedProviderClient(ctx, instance)
		require.NoError(t, err, "Failed to create UnifiedProviderClient")
		defer client.Close()

		// Test GetProviderSchema
		resp, err := client.GetProviderSchema(ctx)
		require.NoError(t, err, "GetProviderSchema should succeed")

		// Validate response
		assert.NotNil(t, resp)
		assert.NotNil(t, resp.Provider)

		// Check for key AWS resources
		assert.Contains(t, resp.ResourceSchemas, "aws_s3_bucket")
		assert.Contains(t, resp.ResourceSchemas, "aws_instance")
		assert.Contains(t, resp.ResourceSchemas, "aws_iam_role")

		// Check for data sources
		assert.Contains(t, resp.DataSourceSchemas, "aws_caller_identity")
		assert.Contains(t, resp.DataSourceSchemas, "aws_region")

		t.Logf("✅ UnifiedProviderClient successfully retrieved schema from AWS v5 provider")
		t.Logf("   Found %d resource schemas", len(resp.ResourceSchemas))
		t.Logf("   Found %d data source schemas", len(resp.DataSourceSchemas))
	})

	t.Run("HealthCheck", func(t *testing.T) {
		// Create a unique temporary directory for this test instance
		tempDir, err := os.MkdirTemp("", "unified-client-health-test-*")
		require.NoError(t, err)
		defer os.RemoveAll(tempDir)

		// Create a fresh provider manager for this test
		healthManager, err := NewProviderManagerWithOptions(
			WithBaseDir(tempDir),
			WithConfigSource(config.NewDefaultConfigSource()),
		)
		require.NoError(t, err)
		defer healthManager.Close()

		// Get a provider instance
		provider, err := healthManager.GetProvider(ctx, "test-deployment-3", "random", "3.7.2", nil, "random_string")
		require.NoError(t, err)

		wrapper, ok := provider.(*providerWrapper)
		require.True(t, ok)

		instance, ok := wrapper.provInstance.(*PluginProviderInstance)
		require.True(t, ok)

		// Create UnifiedProviderClient
		client, err := NewUnifiedProviderClient(ctx, instance)
		require.NoError(t, err)

		// Check health
		assert.True(t, client.IsHealthy(), "Client should be healthy")

		// Close and check health again
		err = client.Close()
		assert.NoError(t, err, "Close should succeed")

		// After close, should not be healthy
		assert.False(t, client.IsHealthy(), "Client should not be healthy after close")
	})
}

// TestUnifiedProviderClient_ConfigureProvider tests provider configuration
func TestUnifiedProviderClient_ConfigureProvider(t *testing.T) {
	ctx := context.Background()

	// Skip if we can't download providers
	if testing.Short() {
		t.Skip("Skipping UnifiedProviderClient test in short mode")
	}

	// Create a unique temporary directory for this test
	tempDir, err := os.MkdirTemp("", "unified-client-config-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create provider manager
	manager, err := NewProviderManagerWithOptions(
		WithBaseDir(tempDir),
		WithConfigSource(config.NewDefaultConfigSource()),
	)
	require.NoError(t, err)
	defer manager.Close()

	t.Run("ConfigureRandomProvider", func(t *testing.T) {
		// Get random provider
		provider, err := manager.GetProvider(ctx, "test-deployment-4", "random", "3.7.2", nil, "random_string")
		require.NoError(t, err)

		wrapper, ok := provider.(*providerWrapper)
		require.True(t, ok)

		instance, ok := wrapper.provInstance.(*PluginProviderInstance)
		require.True(t, ok)

		// Create UnifiedProviderClient
		client, err := NewUnifiedProviderClient(ctx, instance)
		require.NoError(t, err)
		defer client.Close()

		// Random provider doesn't require configuration
		// But we can still call ConfigureProvider with empty config
		configReq := &tfprotov6.ConfigureProviderRequest{
			Config: &tfprotov6.DynamicValue{
				MsgPack: []byte{0x80}, // Empty msgpack map
			},
		}

		resp, err := client.ConfigureProvider(ctx, configReq)
		assert.NoError(t, err, "ConfigureProvider should succeed with empty config")
		assert.NotNil(t, resp, "Response should not be nil")

		// Check for diagnostics
		if len(resp.Diagnostics) > 0 {
			for _, diag := range resp.Diagnostics {
				t.Logf("Diagnostic: %s - %s", diag.Severity, diag.Summary)
			}
		}

		t.Log("✅ UnifiedProviderClient ConfigureProvider succeeded")
	})
}

// TestUnifiedProviderClient_PlanAndApply tests the full resource lifecycle
func TestUnifiedProviderClient_PlanAndApply(t *testing.T) {
	ctx := context.Background()

	// Skip if we can't download providers
	if testing.Short() {
		t.Skip("Skipping UnifiedProviderClient Plan/Apply test in short mode")
	}

	// Don't automatically set LocalStack env vars for unit tests
	// These should only be set when actually running integration tests

	// Create a unique temporary directory for this test
	tempDir, err := os.MkdirTemp("", "unified-client-plan-apply-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Create provider manager
	manager, err := NewProviderManagerWithOptions(
		WithBaseDir(tempDir),
		WithConfigSource(config.NewDefaultConfigSource()),
	)
	require.NoError(t, err)
	defer manager.Close()

	t.Run("Random_Provider_PlanAndApply", func(t *testing.T) {
		// Get random provider
		provider, err := manager.GetProvider(ctx, "test-deployment-5", "random", "3.7.2", nil, "random_string")
		require.NoError(t, err)

		wrapper, ok := provider.(*providerWrapper)
		require.True(t, ok)

		instance, ok := wrapper.provInstance.(*PluginProviderInstance)
		require.True(t, ok)

		// Create UnifiedProviderClient
		client, err := NewUnifiedProviderClient(ctx, instance)
		require.NoError(t, err)
		defer client.Close()

		// Configure the provider (random doesn't need config but we do it anyway)
		configReq := &tfprotov6.ConfigureProviderRequest{
			Config: &tfprotov6.DynamicValue{
				MsgPack: []byte{0x80}, // Empty msgpack map
			},
		}

		configResp, err := client.ConfigureProvider(ctx, configReq)
		require.NoError(t, err)
		require.Empty(t, configResp.Diagnostics, "Should have no diagnostics")

		// Get the schema first to understand what attributes are needed
		schemaResp, err := client.GetProviderSchema(ctx)
		require.NoError(t, err)

		// Get the random_string resource schema
		randomStringSchema, ok := schemaResp.ResourceSchemas["random_string"]
		require.True(t, ok, "Should have random_string resource schema")

		// Create a random_string resource configuration with only the attributes we're setting
		// This is the correct way - only send what we're configuring
		randomConfig := map[string]interface{}{
			"length":  int64(8),
			"special": false,
		}

		// Create the DynamicValue using the schema
		configValue, err := CreateDynamicValueWithSchema(randomConfig, randomStringSchema.Block)
		require.NoError(t, err)

		// Plan the resource creation
		planReq := &tfprotov6.PlanResourceChangeRequest{
			TypeName:         "random_string",
			Config:           configValue,
			ProposedNewState: configValue,
			PriorState: &tfprotov6.DynamicValue{
				MsgPack: []byte{0xc0}, // msgpack nil
			},
		}

		planResp, err := client.PlanResourceChange(ctx, planReq)
		require.NoError(t, err, "PlanResourceChange should succeed")
		require.NotNil(t, planResp)
		require.Empty(t, planResp.Diagnostics, "Plan should have no diagnostics")
		require.NotNil(t, planResp.PlannedState, "Should have planned state")

		t.Log("✅ Plan succeeded for random_string")

		// Apply the resource creation
		applyReq := &tfprotov6.ApplyResourceChangeRequest{
			TypeName:     "random_string",
			Config:       configValue,
			PlannedState: planResp.PlannedState,
			PriorState:   planReq.PriorState,
		}

		applyResp, err := client.ApplyResourceChange(ctx, applyReq)
		require.NoError(t, err, "ApplyResourceChange should succeed")
		require.NotNil(t, applyResp)
		require.Empty(t, applyResp.Diagnostics, "Apply should have no diagnostics")
		require.NotNil(t, applyResp.NewState, "Should have new state after apply")

		t.Log("✅ Apply succeeded for random_string")

		// Verify the resource was created by reading it
		readReq := &tfprotov6.ReadResourceRequest{
			TypeName:     "random_string",
			CurrentState: applyResp.NewState,
		}

		readResp, err := client.ReadResource(ctx, readReq)
		require.NoError(t, err, "ReadResource should succeed")
		require.NotNil(t, readResp)
		require.Empty(t, readResp.Diagnostics, "Read should have no diagnostics")
		require.NotNil(t, readResp.NewState, "Should have state after read")

		t.Log("✅ Read succeeded for random_string")
		t.Log("✅ Full lifecycle test (Plan->Apply->Read) passed for Random provider")
	})

	t.Run("AWS_Provider_PlanAndApply", func(t *testing.T) {
		// Setup LocalStack container and get dynamic endpoint
		lsc := testutil.SetupLocalStack(t)
		endpointURL := lsc.GetEndpoint()

		// Set environment variables for AWS SDK
		t.Setenv("AWS_ENDPOINT_URL", endpointURL)
		t.Setenv("LOCALSTACK_ENDPOINT", endpointURL)

		// Create a unique temporary directory for LocalStack test
		lsTempDir, err := os.MkdirTemp("", "unified-client-localstack-test-*")
		require.NoError(t, err)
		defer os.RemoveAll(lsTempDir)

		// Create LocalStack-configured provider manager
		lsManager, err := NewProviderManagerWithOptions(
			WithConfigSource(localstack.NewConfigSource()),
			WithBaseDir(lsTempDir),
		)
		require.NoError(t, err, "Failed to create LocalStack provider manager")
		defer lsManager.Close()

		// Get AWS provider with LocalStack configuration
		provider, err := lsManager.GetProvider(ctx, "test-deployment-6", "aws", "6.2.0", nil, "aws_s3_bucket")
		if err != nil {
			t.Skip("AWS provider not available")
		}

		wrapper, ok := provider.(*providerWrapper)
		require.True(t, ok)

		instance, ok := wrapper.provInstance.(*PluginProviderInstance)
		require.True(t, ok)

		// Create UnifiedProviderClient
		client, err := NewUnifiedProviderClient(ctx, instance)
		require.NoError(t, err)
		defer client.Close()

		// The provider is already configured for LocalStack via the config source
		// We don't need to configure it again

		// Get the provider schema to verify it's working
		schemaResp, err := client.GetProviderSchema(ctx)
		require.NoError(t, err)
		require.NotNil(t, schemaResp)

		t.Log("✅ AWS provider is configured and ready")

		// Get the s3_bucket resource schema
		s3BucketSchema, ok := schemaResp.ResourceSchemas["aws_s3_bucket"]
		require.True(t, ok, "Should have aws_s3_bucket resource schema")

		// Create an S3 bucket resource configuration
		bucketConfig := map[string]interface{}{
			"bucket": "test-bucket-unified-client",
			"tags": map[string]interface{}{
				"Environment": "test",
				"ManagedBy":   "unified-client",
			},
		}

		bucketConfigValue, err := CreateDynamicValueWithSchema(bucketConfig, s3BucketSchema.Block)
		require.NoError(t, err)

		// Plan the bucket creation
		planReq := &tfprotov6.PlanResourceChangeRequest{
			TypeName:         "aws_s3_bucket",
			Config:           bucketConfigValue,
			ProposedNewState: bucketConfigValue,
			PriorState: &tfprotov6.DynamicValue{
				MsgPack: []byte{0xc0}, // msgpack nil
			},
		}

		planResp, err := client.PlanResourceChange(ctx, planReq)
		require.NoError(t, err, "PlanResourceChange should succeed")
		require.NotNil(t, planResp)

		// Check for errors
		for _, diag := range planResp.Diagnostics {
			if diag.Severity == tfprotov6.DiagnosticSeverityError {
				t.Fatalf("Error planning S3 bucket: %s - %s", diag.Summary, diag.Detail)
			}
		}

		require.NotNil(t, planResp.PlannedState, "Should have planned state")

		t.Log("✅ Plan succeeded for aws_s3_bucket")

		// Apply the bucket creation (LocalStack is configured, so this should succeed)
		applyReq := &tfprotov6.ApplyResourceChangeRequest{
			TypeName:     "aws_s3_bucket",
			Config:       bucketConfigValue,
			PlannedState: planResp.PlannedState,
			PriorState:   planReq.PriorState,
		}

		applyResp, err := client.ApplyResourceChange(ctx, applyReq)
		require.NoError(t, err, "ApplyResourceChange should succeed")
		require.NotNil(t, applyResp)

		// Check for LocalStack permission errors
		for _, diag := range applyResp.Diagnostics {
			if diag.Severity == tfprotov6.DiagnosticSeverityError {
				// LocalStack permission errors are common in test environments
				if strings.Contains(diag.Summary, "403") || strings.Contains(diag.Summary, "Forbidden") {
					t.Skip("Skipping test due to LocalStack permission error: " + diag.Summary)
				}
				t.Fatalf("Apply error: %s - %s", diag.Summary, diag.Detail)
			}
		}

		require.NotNil(t, applyResp.NewState, "Should have new state after apply")

		t.Log("✅ Apply succeeded for aws_s3_bucket")

		// Clean up: Plan and apply a destroy operation
		t.Log("Planning destroy operation...")

		// For destroy, ProposedNewState is null
		destroyPlanReq := &tfprotov6.PlanResourceChangeRequest{
			TypeName: "aws_s3_bucket",
			Config: &tfprotov6.DynamicValue{
				MsgPack: []byte{0xc0}, // msgpack nil
			},
			ProposedNewState: &tfprotov6.DynamicValue{
				MsgPack: []byte{0xc0}, // msgpack nil
			},
			PriorState: applyResp.NewState,
		}

		destroyPlanResp, err := client.PlanResourceChange(ctx, destroyPlanReq)
		require.NoError(t, err, "PlanResourceChange for destroy should succeed")
		require.NotNil(t, destroyPlanResp)
		require.Empty(t, destroyPlanResp.Diagnostics, "Destroy plan should have no diagnostics")

		t.Log("✅ Destroy plan succeeded")

		// Apply the destroy
		destroyApplyReq := &tfprotov6.ApplyResourceChangeRequest{
			TypeName:     "aws_s3_bucket",
			Config:       destroyPlanReq.Config,
			PlannedState: destroyPlanResp.PlannedState,
			PriorState:   destroyPlanReq.PriorState,
		}

		destroyApplyResp, err := client.ApplyResourceChange(ctx, destroyApplyReq)
		require.NoError(t, err, "ApplyResourceChange for destroy should succeed")
		require.NotNil(t, destroyApplyResp)
		require.Empty(t, destroyApplyResp.Diagnostics, "Destroy apply should have no diagnostics")

		// After destroy, NewState should be null/empty
		if destroyApplyResp.NewState != nil && len(destroyApplyResp.NewState.MsgPack) > 0 {
			// Check if it's actually null (msgpack nil is 0xc0)
			require.Equal(t, []byte{0xc0}, destroyApplyResp.NewState.MsgPack, "State after destroy should be null")
		}

		t.Log("✅ Destroy succeeded for aws_s3_bucket")
		t.Log("✅ Full lifecycle test passed for AWS provider (Plan->Apply->Destroy)")
	})
}
