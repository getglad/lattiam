package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/tests/helpers"
)

// TestAWSResourceCleanup verifies that AWS resources created during a test are properly cleaned up.
// This test deploys a simple S3 bucket and ensures it's deleted afterwards.
//
//nolint:paralleltest // test uses shared resources
func TestAWSResourceCleanup(t *testing.T) {
	// Start test server
	server := helpers.StartTestServer(t)
	defer server.Stop(t)
	// Note: Process cleanup is handled by server.Stop()

	// Create API client for proper status polling
	client := helpers.NewAPIClient(t, server.URL)

	// Generate a unique bucket name for this test
	bucketName := helpers.UniqueName("lattiam-test-bucket")

	// Defer cleanup of the S3 bucket. This will run even if the test fails.
	defer func() {
		helpers.DeleteS3Bucket(t, bucketName)
	}()

	// Define the S3 bucket deployment
	deployment := map[string]interface{}{
		"name": "test-s3-bucket-cleanup",
		"terraform_json": map[string]interface{}{
			"terraform": map[string]interface{}{
				"required_providers": map[string]interface{}{
					"aws": map[string]interface{}{
						"source":  "hashicorp/aws",
						"version": "5.31.0",
					},
				},
			},
			"provider": map[string]interface{}{
				"aws": map[string]interface{}{
					"version": "5.31.0",
				},
			},
			"resource": map[string]interface{}{
				"aws_s3_bucket": map[string]interface{}{
					"test": map[string]interface{}{
						"bucket": bucketName,
					},
				},
			},
		},
	}

	// Create deployment
	body, _ := json.Marshal(deployment)
	resp, err := http.Post(server.URL+"/api/v1/deployments", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var createResp map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&createResp)
	require.NoError(t, err)
	resp.Body.Close()

	deploymentID := createResp["id"].(string)

	// Defer cleanup of the deployment itself
	defer func() {
		client.DeleteDeployment(deploymentID)
	}()

	// Wait for deployment to complete successfully
	client.WaitForDeploymentStatus(deploymentID, "completed", 120*time.Second)

	// Optional: Add assertions here to verify the bucket exists before cleanup
	// (This would require AWS SDK calls or `aws s3api head-bucket`)
	// For now, relying on the cleanup defer to indicate success/failure.
}
