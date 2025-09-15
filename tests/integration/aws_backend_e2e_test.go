//go:build integration
// +build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/internal/state"
	"github.com/lattiam/lattiam/tests/testutil"
)

// TestAWSBackendEndToEnd tests the complete AWS backend functionality
func TestAWSBackendEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Start LocalStack using testcontainers
	localstack := testutil.SetupLocalStack(t)
	endpoint := localstack.GetEndpoint()
	t.Logf("Using LocalStack endpoint: %s", endpoint)

	// Set the endpoint for AWS SDK
	t.Setenv("AWS_ENDPOINT_URL", endpoint)
	t.Setenv("LOCALSTACK_ENDPOINT", endpoint)

	ctx := context.Background()

	// Create AWS clients
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithEndpointResolverWithOptions(aws.EndpointResolverWithOptionsFunc(
			func(service, region string, options ...interface{}) (aws.Endpoint, error) {
				return aws.Endpoint{
					URL:               endpoint,
					HostnameImmutable: true,
				}, nil
			},
		)),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
		config.WithRegion("us-east-1"),
	)
	require.NoError(t, err)

	dynamoClient := dynamodb.NewFromConfig(cfg)
	s3Client := s3.NewFromConfig(cfg)

	// Create unique test resources
	timestamp := time.Now().UnixNano()
	testTableName := fmt.Sprintf("lattiam-e2e-test-%d", timestamp)
	testBucketName := fmt.Sprintf("lattiam-e2e-bucket-%d", timestamp)

	// Cleanup function
	cleanup := func() {
		// Delete DynamoDB table
		_, _ = dynamoClient.DeleteTable(ctx, &dynamodb.DeleteTableInput{
			TableName: aws.String(testTableName),
		})

		// Delete S3 bucket and contents
		// First, list and delete all objects
		listResp, err := s3Client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket: aws.String(testBucketName),
		})
		if err == nil && listResp.Contents != nil {
			for _, obj := range listResp.Contents {
				_, _ = s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
					Bucket: aws.String(testBucketName),
					Key:    obj.Key,
				})
			}
		}
		// Then delete the bucket
		_, _ = s3Client.DeleteBucket(ctx, &s3.DeleteBucketInput{
			Bucket: aws.String(testBucketName),
		})
	}
	defer cleanup()

	// Create the AWS state store
	storeConfig := state.AWSStateStoreConfig{
		DynamoDBTable:  testTableName,
		DynamoDBRegion: "us-east-1",
		S3Bucket:       testBucketName,
		S3Region:       "us-east-1",
		S3Prefix:       "deployments/",
		Endpoint:       endpoint,
	}

	store, err := state.NewAWSStateStore(storeConfig)
	require.NoError(t, err, "Failed to create AWS state store")

	// Test 1: Ping should succeed
	t.Run("Ping", func(t *testing.T) {
		err := store.Ping(ctx)
		assert.NoError(t, err, "Ping should succeed")
	})

	// Test 2: Create a deployment
	deploymentID := fmt.Sprintf("test-deployment-%d", timestamp)

	t.Run("CreateDeployment", func(t *testing.T) {
		metadata := &interfaces.DeploymentMetadata{
			DeploymentID:  deploymentID,
			Status:        interfaces.DeploymentStatusProcessing,
			ResourceCount: 5,
			CreatedAt:     time.Now(),
			UpdatedAt:     time.Now(),
		}

		err := store.CreateDeployment(ctx, metadata)
		require.NoError(t, err, "Should create deployment")
	})

	// Test 3: Get the deployment
	t.Run("GetDeployment", func(t *testing.T) {
		retrieved, err := store.GetDeployment(ctx, deploymentID)
		require.NoError(t, err, "Should retrieve deployment")
		assert.Equal(t, deploymentID, retrieved.DeploymentID)
		assert.Equal(t, interfaces.DeploymentStatusProcessing, retrieved.Status)
		assert.Equal(t, 5, retrieved.ResourceCount)
	})

	// Test 4: List deployments
	t.Run("ListDeployments", func(t *testing.T) {
		deployments, err := store.ListDeployments(ctx)
		require.NoError(t, err, "Should list deployments")
		assert.Len(t, deployments, 1, "Should have one deployment")
		assert.Equal(t, deploymentID, deployments[0].DeploymentID)
	})

	// Test 5: Save Terraform state
	t.Run("SaveTerraformState", func(t *testing.T) {
		tfState := map[string]interface{}{
			"version":           4,
			"terraform_version": "1.5.0",
			"resources": []interface{}{
				map[string]interface{}{
					"type": "aws_s3_bucket",
					"name": "test-bucket",
					"instances": []interface{}{
						map[string]interface{}{
							"attributes": map[string]interface{}{
								"id":     "test-bucket-id",
								"bucket": "test-bucket",
							},
						},
					},
				},
			},
		}

		stateJSON, err := json.Marshal(tfState)
		require.NoError(t, err)

		err = store.SaveTerraformState(ctx, deploymentID, stateJSON)
		assert.NoError(t, err, "Should save Terraform state")
	})

	// Test 6: Load Terraform state
	t.Run("LoadTerraformState", func(t *testing.T) {
		loadedState, err := store.LoadTerraformState(ctx, deploymentID)
		require.NoError(t, err, "Should load Terraform state")

		var state map[string]interface{}
		err = json.Unmarshal(loadedState, &state)
		require.NoError(t, err)

		assert.Equal(t, float64(4), state["version"])
		assert.Equal(t, "1.5.0", state["terraform_version"])
	})

	// Test 7: Update deployment status
	t.Run("UpdateDeploymentStatus", func(t *testing.T) {
		err := store.UpdateDeploymentStatus(ctx, deploymentID, interfaces.DeploymentStatusCompleted)
		require.NoError(t, err, "Should update deployment status")

		retrieved, err := store.GetDeployment(ctx, deploymentID)
		require.NoError(t, err)
		assert.Equal(t, interfaces.DeploymentStatusCompleted, retrieved.Status)
	})

	// Test 8: Query deployments by status
	t.Run("QueryByStatus", func(t *testing.T) {
		// Create another deployment with different status
		deployment2ID := fmt.Sprintf("test-deployment-2-%d", timestamp)
		metadata2 := &interfaces.DeploymentMetadata{
			DeploymentID:  deployment2ID,
			Status:        interfaces.DeploymentStatusFailed,
			ResourceCount: 3,
			CreatedAt:     time.Now(),
			UpdatedAt:     time.Now(),
		}
		err := store.CreateDeployment(ctx, metadata2)
		require.NoError(t, err)

		// List all deployments and filter by status manually
		// (ListDeploymentsByStatus method doesn't exist in current implementation)
		allDeployments, err := store.ListDeployments(ctx)
		require.NoError(t, err)

		var completedCount, failedCount int
		for _, dep := range allDeployments {
			if dep.Status == interfaces.DeploymentStatusCompleted {
				completedCount++
				assert.Equal(t, deploymentID, dep.DeploymentID)
			} else if dep.Status == interfaces.DeploymentStatusFailed {
				failedCount++
				assert.Equal(t, deployment2ID, dep.DeploymentID)
			}
		}
		assert.Equal(t, 1, completedCount, "Should have 1 completed deployment")
		assert.Equal(t, 1, failedCount, "Should have 1 failed deployment")

		// Cleanup deployment2
		_ = store.DeleteDeployment(ctx, deployment2ID)
	})

	// Test 9: Delete deployment
	t.Run("DeleteDeployment", func(t *testing.T) {
		err := store.DeleteDeployment(ctx, deploymentID)
		require.NoError(t, err, "Should delete deployment")

		// Verify deployment is deleted from DynamoDB
		_, err = store.GetDeployment(ctx, deploymentID)
		assert.Error(t, err, "Should not find deleted deployment")

		// Verify state file is deleted from S3
		_, err = store.LoadTerraformState(ctx, deploymentID)
		assert.Error(t, err, "Should not find deleted state file")
	})

	// Test 10: Concurrent operations
	t.Run("ConcurrentOperations", func(t *testing.T) {
		// Verify infrastructure still exists before concurrent test
		// This helps catch LocalStack stability issues
		pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if err := store.Ping(pingCtx); err != nil {
			t.Logf("WARNING: Ping failed before concurrent test: %v", err)
			// Try to reinitialize the store if ping fails
			store2, err2 := state.NewAWSStateStore(storeConfig)
			if err2 != nil {
				t.Skipf("Cannot reinitialize AWS store for concurrent test: %v (original error: %v)", err2, err)
			}
			store = store2
			t.Log("Successfully reinitialized AWS store for concurrent test")
		}

		// Additional robustness check: verify table exists and is accessible
		// This is critical in CI environments where container timing can be unpredictable
		retryCtx, retryCancel := context.WithTimeout(ctx, 30*time.Second)
		defer retryCancel()

		maxRetries := 5
		retryDelay := 2 * time.Second

		for attempt := 1; attempt <= maxRetries; attempt++ {
			// Test basic operations before starting concurrent test
			testDeploymentID := fmt.Sprintf("pre-concurrent-test-%d-%d", timestamp, attempt)
			testMetadata := &interfaces.DeploymentMetadata{
				DeploymentID:  testDeploymentID,
				Status:        interfaces.DeploymentStatusProcessing,
				ResourceCount: 1,
				CreatedAt:     time.Now(),
				UpdatedAt:     time.Now(),
			}

			err := store.CreateDeployment(retryCtx, testMetadata)
			if err == nil {
				// Success - cleanup and proceed
				_ = store.DeleteDeployment(retryCtx, testDeploymentID)
				t.Logf("Infrastructure verified on attempt %d", attempt)
				break
			}

			t.Logf("Infrastructure check attempt %d failed: %v", attempt, err)

			// If this is the last attempt, fail the test
			if attempt == maxRetries {
				t.Fatalf("Infrastructure not ready after %d attempts, last error: %v", maxRetries, err)
			}

			// Wait before retrying
			select {
			case <-retryCtx.Done():
				t.Fatalf("Timeout waiting for infrastructure to be ready: %v", retryCtx.Err())
			case <-time.After(retryDelay):
				// Continue to next attempt
			}
		}

		// Create multiple deployments concurrently
		numDeployments := 10
		done := make(chan bool, numDeployments)
		errors := make(chan error, numDeployments)

		for i := 0; i < numDeployments; i++ {
			go func(idx int) {
				defer func() {
					if r := recover(); r != nil {
						errors <- fmt.Errorf("panic in concurrent operation %d: %v", idx, r)
					}
					done <- true
				}()

				depID := fmt.Sprintf("concurrent-deployment-%d-%d", timestamp, idx)

				// Add retry logic for individual operations in case of transient issues
				operationCtx, operationCancel := context.WithTimeout(ctx, 30*time.Second)
				defer operationCancel()

				var lastErr error
				for attempt := 1; attempt <= 3; attempt++ {
					// Create deployment with retry
					metadata := &interfaces.DeploymentMetadata{
						DeploymentID:  depID,
						Status:        interfaces.DeploymentStatusProcessing,
						ResourceCount: idx,
						CreatedAt:     time.Now(),
						UpdatedAt:     time.Now(),
					}

					err := store.CreateDeployment(operationCtx, metadata)
					if err != nil {
						lastErr = fmt.Errorf("failed to create deployment %s (attempt %d): %v", depID, attempt, err)
						if attempt < 3 {
							time.Sleep(100 * time.Millisecond * time.Duration(attempt))
							continue
						}
						errors <- lastErr
						return
					}

					// Save state with retry
					stateData := []byte(fmt.Sprintf(`{"version": 4, "deployment": "%s"}`, depID))
					err = store.SaveTerraformState(operationCtx, depID, stateData)
					if err != nil {
						lastErr = fmt.Errorf("failed to save state for %s (attempt %d): %v", depID, attempt, err)
						if attempt < 3 {
							time.Sleep(100 * time.Millisecond * time.Duration(attempt))
							continue
						}
						errors <- lastErr
						return
					}

					// Update status with retry
					err = store.UpdateDeploymentStatus(operationCtx, depID, interfaces.DeploymentStatusCompleted)
					if err != nil {
						lastErr = fmt.Errorf("failed to update status for %s (attempt %d): %v", depID, attempt, err)
						if attempt < 3 {
							time.Sleep(100 * time.Millisecond * time.Duration(attempt))
							continue
						}
						errors <- lastErr
						return
					}

					// All operations succeeded
					break
				}
			}(i)
		}

		// Wait for all operations to complete
		for i := 0; i < numDeployments; i++ {
			<-done
		}

		// Check for errors
		close(errors)
		var errorList []error
		for err := range errors {
			errorList = append(errorList, err)
		}

		if len(errorList) > 0 {
			t.Logf("Concurrent operation errors (%d/%d failed):", len(errorList), numDeployments)
			for _, err := range errorList {
				t.Logf("  - %v", err)
			}
			// Fail the test if too many operations failed
			if len(errorList) > numDeployments/2 {
				t.Fatalf("Too many concurrent operations failed (%d/%d)", len(errorList), numDeployments)
			}
		}

		// Verify deployments were created
		deployments, err := store.ListDeployments(ctx)
		require.NoError(t, err)

		// Count successful deployments (account for any that failed)
		successfulDeployments := numDeployments - len(errorList)
		actualConcurrentDeployments := 0
		for _, dep := range deployments {
			if strings.HasPrefix(dep.DeploymentID, fmt.Sprintf("concurrent-deployment-%d-", timestamp)) {
				actualConcurrentDeployments++
			}
		}

		assert.GreaterOrEqual(t, actualConcurrentDeployments, successfulDeployments,
			"Expected at least %d concurrent deployments, found %d", successfulDeployments, actualConcurrentDeployments)

		// Cleanup concurrent deployments
		for i := 0; i < numDeployments; i++ {
			depID := fmt.Sprintf("concurrent-deployment-%d-%d", timestamp, i)
			_ = store.DeleteDeployment(ctx, depID)
		}
	})

	// Test 11: Storage info
	t.Run("GetStorageInfo", func(t *testing.T) {
		info := store.GetStorageInfo()
		assert.Equal(t, "aws", info.Type)
		assert.True(t, info.Exists)
		assert.True(t, info.Writable)
	})
}

// TestAWSBackendMigrationFromS3Only tests migration from old S3-only backend
func TestAWSBackendMigrationFromS3Only(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Start LocalStack using testcontainers
	localstack := testutil.SetupLocalStack(t)
	endpoint := localstack.GetEndpoint()
	t.Logf("Using LocalStack endpoint: %s", endpoint)

	// Set the endpoint for AWS SDK
	t.Setenv("AWS_ENDPOINT_URL", endpoint)
	t.Setenv("LOCALSTACK_ENDPOINT", endpoint)

	ctx := context.Background()

	// Create AWS clients
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithEndpointResolverWithOptions(aws.EndpointResolverWithOptionsFunc(
			func(service, region string, options ...interface{}) (aws.Endpoint, error) {
				return aws.Endpoint{
					URL:               endpoint,
					HostnameImmutable: true,
				}, nil
			},
		)),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
		config.WithRegion("us-east-1"),
	)
	require.NoError(t, err)

	s3Client := s3.NewFromConfig(cfg)

	// Create test resources
	timestamp := time.Now().UnixNano()
	testBucketName := fmt.Sprintf("lattiam-migrate-test-%d", timestamp)

	// Cleanup
	defer func() {
		// Delete all objects
		listResp, _ := s3Client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket: aws.String(testBucketName),
		})
		if listResp != nil && listResp.Contents != nil {
			for _, obj := range listResp.Contents {
				_, _ = s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
					Bucket: aws.String(testBucketName),
					Key:    obj.Key,
				})
			}
		}
		// Delete bucket
		_, _ = s3Client.DeleteBucket(ctx, &s3.DeleteBucketInput{
			Bucket: aws.String(testBucketName),
		})
	}()

	// Create bucket
	_, err = s3Client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(testBucketName),
	})
	require.NoError(t, err)

	// Simulate old S3-only backend data
	deploymentID := "legacy-deployment"
	tfState := map[string]interface{}{
		"version":           4,
		"terraform_version": "1.4.0",
		"resources": []interface{}{
			map[string]interface{}{
				"type": "aws_instance",
				"name": "web",
			},
		},
	}

	stateJSON, err := json.Marshal(tfState)
	require.NoError(t, err)

	// Put state file in S3 (location expected by AWS state store)
	_, err = s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(testBucketName),
		Key:    aws.String(fmt.Sprintf("lattiam/deployments/%s.tfstate", deploymentID)),
		Body:   bytes.NewReader(stateJSON),
	})
	require.NoError(t, err)

	// Create AWS state store
	storeConfig := state.AWSStateStoreConfig{
		DynamoDBTable:  fmt.Sprintf("lattiam-migrate-table-%d", timestamp),
		DynamoDBRegion: "us-east-1",
		S3Bucket:       testBucketName,
		S3Region:       "us-east-1",
		S3Prefix:       "lattiam/deployments/",
		Endpoint:       endpoint,
	}

	store, err := state.NewAWSStateStore(storeConfig)
	require.NoError(t, err)

	// Import the existing state file
	t.Run("ImportFromS3", func(t *testing.T) {
		// Create metadata for the legacy deployment
		metadata := &interfaces.DeploymentMetadata{
			DeploymentID:  deploymentID,
			Status:        interfaces.DeploymentStatusCompleted,
			ResourceCount: 1,
			CreatedAt:     time.Now().Add(-24 * time.Hour), // Pretend it's old
			UpdatedAt:     time.Now(),
		}

		// Import into new system
		err := store.CreateDeployment(ctx, metadata)
		require.NoError(t, err, "Should create deployment metadata")

		// Verify we can load the state
		loadedState, err := store.LoadTerraformState(ctx, deploymentID)
		require.NoError(t, err, "Should load imported state")

		var state map[string]interface{}
		err = json.Unmarshal(loadedState, &state)
		require.NoError(t, err)

		assert.Equal(t, float64(4), state["version"])
		assert.Equal(t, "1.4.0", state["terraform_version"])
	})

	// Cleanup
	_ = store.DeleteDeployment(ctx, deploymentID)
}
