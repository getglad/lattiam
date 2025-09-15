//go:build integration
// +build integration

package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/lattiam/lattiam/internal/state"
	"github.com/lattiam/lattiam/tests/testutil"
)

// TestAWSStateStore_BasicOperations tests basic operations with LocalStack
func TestAWSStateStore_BasicOperations(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping AWS test in short mode")
	}

	// Setup LocalStack for this test
	lsc := testutil.SetupLocalStack(t)
	endpoint := lsc.GetEndpoint()

	// Set environment variables for AWS SDK
	t.Setenv("AWS_ENDPOINT_URL", endpoint)
	t.Setenv("LOCALSTACK_ENDPOINT", endpoint)
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_REGION", "us-east-1")

	config := state.AWSStateStoreConfig{
		DynamoDBTable:  "lattiam-test-deployments",
		DynamoDBRegion: "us-east-1",
		S3Bucket:       "lattiam-test-state",
		S3Region:       "us-east-1",
		Endpoint:       endpoint,
	}

	store, err := state.NewAWSStateStore(config)
	if err != nil {
		t.Fatalf("Failed to create AWS state store: %v", err)
	}

	// Test Ping
	ctx := context.Background()
	if err := store.Ping(ctx); err != nil {
		t.Skipf("Skipping AWS test - ping failed: %v", err)
	}

	// Test basic deployment operations
	deploymentID := "test-deployment-" + fmt.Sprintf("%d", time.Now().UnixNano())

	// Test UpdateDeploymentState (should create deployment)
	state := map[string]interface{}{
		"resources": []interface{}{
			map[string]interface{}{
				"type": "aws_instance",
				"name": "test",
				"attributes": map[string]interface{}{
					"id":            "i-1234567890abcdef0",
					"instance_type": "t3.micro",
				},
			},
		},
	}

	err = store.UpdateDeploymentState(deploymentID, state)
	if err != nil {
		t.Fatalf("Failed to update deployment state: %v", err)
	}

	// Test GetDeploymentState
	retrievedState, err := store.GetDeploymentState(deploymentID)
	if err != nil {
		t.Fatalf("Failed to get deployment state: %v", err)
	}

	if retrievedState["deployment_id"] != deploymentID {
		t.Errorf("Retrieved wrong deployment ID: got %v, want %s", retrievedState["deployment_id"], deploymentID)
	}

	// Test ListDeployments
	deployments, err := store.ListDeployments(ctx)
	if err != nil {
		t.Fatalf("Failed to list deployments: %v", err)
	}

	found := false
	for _, deployment := range deployments {
		if deployment.DeploymentID == deploymentID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Deployment %s not found in list: %v", deploymentID, deployments)
	}

	// Test ExportTerraformState
	tfState, err := store.ExportTerraformState(deploymentID)
	if err != nil {
		t.Fatalf("Failed to export Terraform state: %v", err)
	}

	if tfState.Serial == 0 {
		t.Error("Exported state has zero serial number")
	}

	// Clean up
	err = store.DeleteDeploymentState(deploymentID)
	if err != nil {
		t.Errorf("Failed to delete deployment state: %v", err)
	}
}
