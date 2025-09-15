//go:build integration && localstack
// +build integration,localstack

package protocol

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/tests/helpers/localstack"
	"github.com/lattiam/lattiam/tests/testutil"
)

// TestProviderWrapper_AWSLifecycleWithLocalStack tests AWS resource lifecycle with LocalStack
func TestProviderWrapper_AWSLifecycleWithLocalStack(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	ctx := context.Background()

	// Start LocalStack container and get endpoint
	lsc := testutil.SetupLocalStack(t)
	endpointURL := lsc.GetEndpoint()

	// Set environment variables for AWS SDK
	t.Setenv("AWS_ENDPOINT_URL", endpointURL)
	t.Setenv("LOCALSTACK_ENDPOINT", endpointURL)

	// Create provider manager with LocalStack configuration
	manager, err := NewProviderManagerWithOptions(
		WithConfigSource(localstack.NewConfigSource()),
		WithBaseDir("/tmp/wrapper-lifecycle-test"),
	)
	require.NoError(t, err, "Failed to create LocalStack provider manager")
	defer manager.Close()

	// Get AWS provider - LocalStack configuration is automatically applied
	provider, err := manager.GetProvider(ctx, "test-lifecycle-deployment", "aws", "6.2.0", nil, "aws_s3_bucket")
	require.NoError(t, err, "Failed to get AWS provider instance")

	// 1. Create an S3 bucket
	// Generate unique bucket name to avoid conflicts
	timestamp := time.Now().Unix()
	bucketName := fmt.Sprintf("test-lifecycle-bucket-%d", timestamp)
	bucketConfig := map[string]interface{}{
		"bucket": bucketName,
		"tags": map[string]interface{}{
			"Environment": "test",
			"Purpose":     "lifecycle-test",
		},
	}
	t.Logf("Creating bucket: %s", bucketName)

	createdState, err := provider.CreateResource(ctx, "aws_s3_bucket", bucketConfig)

	// Check for LocalStack permission errors
	if err != nil && (strings.Contains(err.Error(), "403") || strings.Contains(err.Error(), "Forbidden")) {
		t.Skip("Skipping test due to LocalStack permission error: " + err.Error())
	}

	require.NoError(t, err, "CreateResource should succeed")
	require.NotNil(t, createdState, "Should have created state")

	// Verify bucket was created
	assert.Contains(t, createdState, "bucket", "Should have bucket name")
	assert.Contains(t, createdState, "id", "Should have bucket ID")
	assert.Contains(t, createdState, "tags", "Should have tags")

	t.Logf("✅ Created S3 bucket: %s", createdState["bucket"])

	// 2. Update the bucket (add/change tags)
	updateConfig := map[string]interface{}{
		"bucket": bucketName, // Bucket name can't change
		"tags": map[string]interface{}{
			"Environment": "test",
			"Purpose":     "updated-lifecycle-test",
			"UpdatedBy":   "UnifiedProviderClient",
		},
	}

	updatedState, err := provider.UpdateResource(ctx, "aws_s3_bucket", createdState, updateConfig)
	require.NoError(t, err, "UpdateResource should succeed")
	require.NotNil(t, updatedState, "Should have updated state")

	// Verify tags were updated
	tags, ok := updatedState["tags"].(map[string]interface{})
	assert.True(t, ok, "Tags should be a map")
	assert.Equal(t, "updated-lifecycle-test", tags["Purpose"], "Purpose tag should be updated")
	assert.Equal(t, "UnifiedProviderClient", tags["UpdatedBy"], "UpdatedBy tag should be added")

	t.Log("✅ Updated S3 bucket tags")

	// 3. Delete the bucket
	err = provider.DeleteResource(ctx, "aws_s3_bucket", updatedState)
	require.NoError(t, err, "DeleteResource should succeed")

	t.Log("✅ Successfully deleted S3 bucket")

	// Verify UnifiedProviderClient was used
	wrapper, ok := provider.(*providerWrapper)
	require.True(t, ok, "Provider should be providerWrapper")
	assert.NotNil(t, wrapper.unifiedClient, "UnifiedClient should be initialized")

	t.Log("✅ Full AWS resource lifecycle completed using UnifiedProviderClient")
}
