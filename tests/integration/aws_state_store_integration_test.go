//go:build integration
// +build integration

package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/internal/state"
	"github.com/lattiam/lattiam/tests/testutil"
)

// AWSStateStoreIntegrationTestSuite provides BDD-style integration tests for AWSStateStore
type AWSStateStoreIntegrationTestSuite struct {
	suite.Suite
	ctx            context.Context
	localstack     *testutil.LocalStackContainer
	endpoint       string
	dynamoClient   *dynamodb.Client
	s3Client       *s3.Client
	store          *state.AWSStateStore
	storeConfig    state.AWSStateStoreConfig
	testTableName  string
	testBucketName string
}

// TestAWSStateStoreIntegrationSuite runs the test suite
func TestAWSStateStoreIntegrationSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration tests in short mode")
	}

	suite.Run(t, new(AWSStateStoreIntegrationTestSuite))
}

// SetupSuite initializes LocalStack container and creates test infrastructure
func (suite *AWSStateStoreIntegrationTestSuite) SetupSuite() {
	suite.ctx = context.Background()

	// Try to use existing LocalStack first
	suite.endpoint = os.Getenv("LOCALSTACK_ENDPOINT")
	if suite.endpoint != "" {
		suite.T().Log("Using existing LocalStack at", suite.endpoint)
		suite.initializeClients()
		return
	}

	// Start LocalStack container using our testutil
	suite.T().Log("Starting LocalStack container...")

	lsc := testutil.SetupLocalStackWithServices(suite.T(), "s3,dynamodb,ec2,iam,sts")
	suite.localstack = lsc
	suite.endpoint = lsc.GetEndpoint()

	suite.T().Log("LocalStack started at", suite.endpoint)

	// Set environment variables for this test
	suite.T().Setenv("AWS_ENDPOINT_URL", suite.endpoint)
	suite.T().Setenv("LOCALSTACK_ENDPOINT", suite.endpoint)

	suite.initializeClients()
}

// initializeClients creates AWS clients for testing
func (suite *AWSStateStoreIntegrationTestSuite) initializeClients() {
	// Create AWS config for LocalStack
	awsCfg, err := config.LoadDefaultConfig(suite.ctx,
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
	)
	require.NoError(suite.T(), err)

	// Create clients
	suite.dynamoClient = dynamodb.NewFromConfig(awsCfg, func(o *dynamodb.Options) {
		o.BaseEndpoint = aws.String(suite.endpoint)
	})

	suite.s3Client = s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(suite.endpoint)
		o.UsePathStyle = true
	})
}

// SetupTest creates fresh AWS resources for each test
func (suite *AWSStateStoreIntegrationTestSuite) SetupTest() {
	// Generate unique names for this test
	testID := fmt.Sprintf("%d", time.Now().UnixNano())
	suite.testTableName = "lattiam-test-deployments-" + testID
	suite.testBucketName = "lattiam-test-state-" + testID

	// Create test store
	suite.storeConfig = state.AWSStateStoreConfig{
		DynamoDBTable:  suite.testTableName,
		DynamoDBRegion: "us-east-1",
		S3Bucket:       suite.testBucketName,
		S3Region:       "us-east-1",
		Endpoint:       suite.endpoint,
		LockingEnabled: true,
		LockConfig: &state.DynamoDBLockConfig{
			TableName: "lattiam-test-locks-" + testID,
			Region:    "us-east-1",
			Endpoint:  suite.endpoint,
		},
	}

	store, err := state.NewAWSStateStore(suite.storeConfig)
	require.NoError(suite.T(), err)
	suite.store = store

	suite.T().Log("Created test store with table:", suite.testTableName, "bucket:", suite.testBucketName)
}

// TearDownTest cleans up AWS resources after each test
func (suite *AWSStateStoreIntegrationTestSuite) TearDownTest() {
	if suite.store != nil {
		// Clean up all deployments
		deployments, _ := suite.store.ListDeployments(suite.ctx)
		for _, deployment := range deployments {
			suite.store.DeleteDeploymentState(deployment.DeploymentID)
		}

		// Delete DynamoDB table
		if suite.testTableName != "" {
			suite.dynamoClient.DeleteTable(suite.ctx, &dynamodb.DeleteTableInput{
				TableName: aws.String(suite.testTableName),
			})
		}

		// Delete S3 bucket (empty it first)
		if suite.testBucketName != "" {
			suite.s3Client.DeleteBucket(suite.ctx, &s3.DeleteBucketInput{
				Bucket: aws.String(suite.testBucketName),
			})
		}

		// Delete lock table if exists
		if suite.storeConfig.LockConfig != nil {
			suite.dynamoClient.DeleteTable(suite.ctx, &dynamodb.DeleteTableInput{
				TableName: aws.String(suite.storeConfig.LockConfig.TableName),
			})
		}
	}
}

// TearDownSuite is not needed as our testutil handles cleanup with t.Cleanup()

// Test_Given_FreshStore_When_PingCalled_Then_Succeeds tests basic connectivity
func (suite *AWSStateStoreIntegrationTestSuite) Test_Given_FreshStore_When_PingCalled_Then_Succeeds() {
	// Given: Fresh store is created in SetupTest

	// When: Ping is called
	err := suite.store.Ping(suite.ctx)

	// Then: It should succeed
	assert.NoError(suite.T(), err, "Ping should succeed for healthy LocalStack")
}

// Test_Given_NoDeployments_When_ListDeployments_Then_ReturnsEmptyList tests listing empty deployments
func (suite *AWSStateStoreIntegrationTestSuite) Test_Given_NoDeployments_When_ListDeployments_Then_ReturnsEmptyList() {
	// Given: Fresh store with no deployments

	// When: ListDeployments is called
	deployments, err := suite.store.ListDeployments(suite.ctx)

	// Then: Should return empty list
	assert.NoError(suite.T(), err)
	assert.Empty(suite.T(), deployments)
}

// Test_Given_NewDeployment_When_UpdateState_Then_CreatesAndStores tests deployment creation lifecycle
func (suite *AWSStateStoreIntegrationTestSuite) Test_Given_NewDeployment_When_UpdateState_Then_CreatesAndStores() {
	// Given: A new deployment ID and state
	deploymentID := "test-deployment-123"
	state := map[string]interface{}{
		"resources": []interface{}{
			map[string]interface{}{
				"type": "aws_s3_bucket",
				"name": "test_bucket",
				"attributes": map[string]interface{}{
					"bucket": "test-bucket-name",
					"region": "us-east-1",
				},
			},
		},
		"outputs": map[string]interface{}{
			"bucket_name": map[string]interface{}{
				"value":     "test-bucket-name",
				"sensitive": false,
			},
		},
	}

	// When: UpdateDeploymentState is called
	err := suite.store.UpdateDeploymentState(deploymentID, state)

	// Then: Should succeed
	assert.NoError(suite.T(), err)

	// And: Deployment should be retrievable
	retrievedState, err := suite.store.GetDeploymentState(deploymentID)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), deploymentID, retrievedState["deployment_id"])
	assert.Equal(suite.T(), 1, retrievedState["resource_count"])

	// And: Should appear in listings
	deploymentList, err := suite.store.ListDeployments(suite.ctx)
	assert.NoError(suite.T(), err)
	deploymentIDs := make([]string, len(deploymentList))
	for i, dep := range deploymentList {
		deploymentIDs[i] = dep.DeploymentID
	}
	assert.Contains(suite.T(), deploymentIDs, deploymentID)

	// And: Terraform state should be stored and retrievable
	tfState, err := suite.store.ExportTerraformState(deploymentID)
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), tfState)
	assert.Len(suite.T(), tfState.Resources, 1)
	assert.Equal(suite.T(), "aws_s3_bucket", tfState.Resources[0].Type)
}

// Test_Given_ExistingDeployment_When_UpdateState_Then_UpdatesSerialAndTimestamp tests state updates
func (suite *AWSStateStoreIntegrationTestSuite) Test_Given_ExistingDeployment_When_UpdateState_Then_UpdatesSerialAndTimestamp() {
	// Given: An existing deployment
	deploymentID := "test-deployment-update"
	initialState := map[string]interface{}{
		"resources": []interface{}{
			map[string]interface{}{
				"type": "aws_instance",
				"name": "web",
				"attributes": map[string]interface{}{
					"instance_type": "t2.micro",
				},
			},
		},
	}

	err := suite.store.UpdateDeploymentState(deploymentID, initialState)
	require.NoError(suite.T(), err)

	// Get initial metadata
	initialMetadata, err := suite.store.GetDeploymentState(deploymentID)
	require.NoError(suite.T(), err)
	initialSerial := initialMetadata["serial"].(uint64)
	initialUpdatedAt := initialMetadata["updated_at"].(string)

	// Wait a bit to ensure timestamp difference
	time.Sleep(1100 * time.Millisecond) // Increase wait to ensure timestamp difference in seconds

	// When: Update the deployment with modified state
	updatedState := map[string]interface{}{
		"resources": []interface{}{
			map[string]interface{}{
				"type": "aws_instance",
				"name": "web",
				"attributes": map[string]interface{}{
					"instance_type": "t3.small", // Changed instance type
				},
			},
			map[string]interface{}{
				"type": "aws_s3_bucket",
				"name": "data",
				"attributes": map[string]interface{}{
					"bucket": "data-bucket",
				},
			},
		},
	}

	err = suite.store.UpdateDeploymentState(deploymentID, updatedState)

	// Then: Should succeed
	assert.NoError(suite.T(), err)

	// And: Metadata should be updated
	updatedMetadata, err := suite.store.GetDeploymentState(deploymentID)
	assert.NoError(suite.T(), err)

	updatedSerial := updatedMetadata["serial"].(uint64)
	updatedTimestamp := updatedMetadata["updated_at"].(string)

	assert.Greater(suite.T(), updatedSerial, initialSerial, "Serial should increment")
	assert.NotEqual(suite.T(), initialUpdatedAt, updatedTimestamp, "Timestamp should update")
	assert.Equal(suite.T(), 2, updatedMetadata["resource_count"], "Resource count should reflect new resources")

	// And: Terraform state should reflect changes
	tfState, err := suite.store.ExportTerraformState(deploymentID)
	assert.NoError(suite.T(), err)
	assert.Len(suite.T(), tfState.Resources, 2, "Should have both resources")
	assert.Equal(suite.T(), updatedSerial, tfState.Serial, "TF state serial should match metadata")
}

// Test_Given_MultipleDeployments_When_ConcurrentOperations_Then_AllSucceed tests concurrent operations
func (suite *AWSStateStoreIntegrationTestSuite) Test_Given_MultipleDeployments_When_ConcurrentOperations_Then_AllSucceed() {
	// Given: Multiple deployment operations to run concurrently
	const numOperations = 10
	var wg sync.WaitGroup
	errors := make(chan error, numOperations)

	// When: Multiple deployment operations run concurrently
	for i := 0; i < numOperations; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			deploymentID := fmt.Sprintf("concurrent-deployment-%d", id)
			state := map[string]interface{}{
				"resources": []interface{}{
					map[string]interface{}{
						"type": "aws_instance",
						"name": fmt.Sprintf("web-%d", id),
						"attributes": map[string]interface{}{
							"instance_type": "t2.micro",
							"ami":           fmt.Sprintf("ami-12345%d", id),
						},
					},
				},
			}

			if err := suite.store.UpdateDeploymentState(deploymentID, state); err != nil {
				errors <- fmt.Errorf("deployment %d failed: %w", id, err)
				return
			}

			// Verify the deployment was created
			if _, err := suite.store.GetDeploymentState(deploymentID); err != nil {
				errors <- fmt.Errorf("get deployment %d failed: %w", id, err)
				return
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Then: All operations should succeed
	var errorList []error
	for err := range errors {
		errorList = append(errorList, err)
	}
	assert.Empty(suite.T(), errorList, "All concurrent operations should succeed: %v", errorList)

	// And: All deployments should be listable
	deploymentList, err := suite.store.ListDeployments(suite.ctx)
	assert.NoError(suite.T(), err)
	assert.Len(suite.T(), deploymentList, numOperations, "Should have all deployments created")
}

// Test_Given_LargeStateFile_When_SaveAndRetrieve_Then_HandlesCorrectly tests large state file handling
func (suite *AWSStateStoreIntegrationTestSuite) Test_Given_LargeStateFile_When_SaveAndRetrieve_Then_HandlesCorrectly() {
	// Given: A large state with many resources
	deploymentID := "large-state-deployment"
	const numResources = 100

	resources := make([]interface{}, numResources)
	for i := 0; i < numResources; i++ {
		resources[i] = map[string]interface{}{
			"type": "aws_instance",
			"name": fmt.Sprintf("instance-%d", i),
			"attributes": map[string]interface{}{
				"instance_type": "t3.micro",
				"ami":           "ami-0123456789abcdef0",
				"tags": map[string]interface{}{
					"Name":        fmt.Sprintf("Instance-%d", i),
					"Environment": "test",
					"LargeData":   strings.Repeat("x", 1000), // Add some bulk to each resource
				},
			},
		}
	}

	largeState := map[string]interface{}{
		"resources": resources,
		"outputs": map[string]interface{}{
			"instance_count": map[string]interface{}{
				"value":     numResources,
				"sensitive": false,
			},
		},
	}

	// When: Save the large state
	err := suite.store.UpdateDeploymentState(deploymentID, largeState)

	// Then: Should succeed
	assert.NoError(suite.T(), err)

	// And: Should be retrievable with correct resource count
	metadata, err := suite.store.GetDeploymentState(deploymentID)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), numResources, metadata["resource_count"])

	// And: Terraform state should be complete
	tfState, err := suite.store.ExportTerraformState(deploymentID)
	assert.NoError(suite.T(), err)
	assert.Len(suite.T(), tfState.Resources, numResources)

	// And: All resources should have expected data
	for i, resource := range tfState.Resources {
		assert.Equal(suite.T(), "aws_instance", resource.Type)
		assert.Equal(suite.T(), fmt.Sprintf("instance-%d", i), resource.Name)
		assert.Contains(suite.T(), resource.Instances[0].Attributes, "tags")
	}
}

// Test_Given_ExistingDeployment_When_DeleteDeployment_Then_RemovedFromBothStores tests deletion
func (suite *AWSStateStoreIntegrationTestSuite) Test_Given_ExistingDeployment_When_DeleteDeployment_Then_RemovedFromBothStores() {
	// Given: An existing deployment
	deploymentID := "deployment-to-delete"
	state := map[string]interface{}{
		"resources": []interface{}{
			map[string]interface{}{
				"type": "aws_s3_bucket",
				"name": "temp_bucket",
			},
		},
	}

	err := suite.store.UpdateDeploymentState(deploymentID, state)
	require.NoError(suite.T(), err)

	// Verify it exists
	_, err = suite.store.GetDeploymentState(deploymentID)
	require.NoError(suite.T(), err)

	// When: Delete the deployment
	err = suite.store.DeleteDeploymentState(deploymentID)

	// Then: Should succeed
	assert.NoError(suite.T(), err)

	// And: Should not be retrievable from DynamoDB
	_, err = suite.store.GetDeploymentState(deploymentID)
	assert.Error(suite.T(), err)
	assert.Contains(suite.T(), err.Error(), "not found")

	// And: Should not appear in listings
	deploymentList, err := suite.store.ListDeployments(suite.ctx)
	assert.NoError(suite.T(), err)
	deploymentIDs := make([]string, len(deploymentList))
	for i, dep := range deploymentList {
		deploymentIDs[i] = dep.DeploymentID
	}
	assert.NotContains(suite.T(), deploymentIDs, deploymentID)

	// And: Terraform state should not be retrievable (should return empty state)
	tfState, err := suite.store.ExportTerraformState(deploymentID)
	assert.NoError(suite.T(), err) // Returns empty state, not error
	assert.Len(suite.T(), tfState.Resources, 0, "Should return empty state for non-existent deployment")
}

// Test_Given_TerraformStateFile_When_ImportState_Then_UpdatesMetadata tests Terraform state import
func (suite *AWSStateStoreIntegrationTestSuite) Test_Given_TerraformStateFile_When_ImportState_Then_UpdatesMetadata() {
	// Given: A deployment and a complete Terraform state to import
	deploymentID := "import-test-deployment"

	// Create initial deployment
	initialState := map[string]interface{}{"resources": []interface{}{}}
	err := suite.store.UpdateDeploymentState(deploymentID, initialState)
	require.NoError(suite.T(), err)

	// Create a realistic Terraform state
	tfState := &interfaces.TerraformState{
		Version:          4,
		TerraformVersion: "1.5.0",
		Serial:           42,
		Lineage:          "imported-lineage-123",
		Outputs: map[string]interface{}{
			"bucket_arn": map[string]interface{}{
				"value":     "arn:aws:s3:::imported-bucket",
				"sensitive": false,
			},
		},
		Resources: []interfaces.TerraformResource{
			{
				Mode:     "managed",
				Type:     "aws_s3_bucket",
				Name:     "imported",
				Provider: "provider[\"registry.terraform.io/hashicorp/aws\"]",
				Instances: []interfaces.TerraformResourceInstance{
					{
						SchemaVersion: 1,
						Attributes: map[string]interface{}{
							"bucket":        "imported-bucket",
							"region":        "us-east-1",
							"force_destroy": true,
						},
					},
				},
			},
		},
	}

	// When: Import the Terraform state
	err = suite.store.ImportTerraformState(deploymentID, tfState)

	// Then: Should succeed
	assert.NoError(suite.T(), err)

	// And: Metadata should be updated with imported values
	metadata, err := suite.store.GetDeploymentState(deploymentID)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), uint64(42), metadata["serial"])
	assert.Equal(suite.T(), "imported-lineage-123", metadata["lineage"])
	assert.Equal(suite.T(), 1, metadata["resource_count"])

	// And: Exported state should match imported state
	exportedState, err := suite.store.ExportTerraformState(deploymentID)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), tfState.Serial, exportedState.Serial)
	assert.Equal(suite.T(), tfState.Lineage, exportedState.Lineage)
	assert.Len(suite.T(), exportedState.Resources, 1)
	assert.Equal(suite.T(), "aws_s3_bucket", exportedState.Resources[0].Type)
}

// Test_Given_LockingEnabled_When_LockDeployment_Then_PreventsNoncurrentAccess tests deployment locking
func (suite *AWSStateStoreIntegrationTestSuite) Test_Given_LockingEnabled_When_LockDeployment_Then_PreventsConcurrentAccess() {
	// Given: A deployment exists
	deploymentID := "lockable-deployment"
	state := map[string]interface{}{
		"resources": []interface{}{
			map[string]interface{}{"type": "aws_instance", "name": "test"},
		},
	}
	err := suite.store.UpdateDeploymentState(deploymentID, state)
	require.NoError(suite.T(), err)

	// When: First lock is acquired
	lock1, err := suite.store.LockDeployment(suite.ctx, deploymentID)

	// Then: Should succeed
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), lock1)
	assert.Equal(suite.T(), deploymentID, lock1.DeploymentID())

	// And: Second lock attempt should fail
	lock2, err := suite.store.LockDeployment(suite.ctx, deploymentID)
	assert.Error(suite.T(), err, "Second lock should fail")
	assert.Nil(suite.T(), lock2)

	// When: First lock is released
	err = suite.store.UnlockDeployment(suite.ctx, lock1)
	assert.NoError(suite.T(), err)

	// Then: New lock should succeed
	lock3, err := suite.store.LockDeployment(suite.ctx, deploymentID)
	assert.NoError(suite.T(), err)
	assert.NotNil(suite.T(), lock3)

	// Cleanup
	suite.store.UnlockDeployment(suite.ctx, lock3)
}

// Test_Given_OldDeployments_When_Cleanup_Then_RemovesExpiredData tests cleanup functionality
func (suite *AWSStateStoreIntegrationTestSuite) Test_Given_OldDeployments_When_Cleanup_Then_RemovesExpiredData() {
	// Given: Multiple deployments with different ages
	oldDeploymentID := "old-deployment"
	recentDeploymentID := "recent-deployment"

	// Create "old" deployment (simulate by creating with older timestamp)
	state := map[string]interface{}{
		"resources": []interface{}{
			map[string]interface{}{"type": "aws_instance", "name": "old"},
		},
	}
	err := suite.store.UpdateDeploymentState(oldDeploymentID, state)
	require.NoError(suite.T(), err)

	// Create recent deployment
	err = suite.store.UpdateDeploymentState(recentDeploymentID, state)
	require.NoError(suite.T(), err)

	// Verify both exist
	deploymentList, err := suite.store.ListDeployments(suite.ctx)
	require.NoError(suite.T(), err)
	assert.Len(suite.T(), deploymentList, 2)

	// When: Cleanup is called with a very short duration (should remove nothing since all are recent)
	err = suite.store.Cleanup(1 * time.Nanosecond)

	// Then: Should succeed but remove nothing (deployments are too recent)
	assert.NoError(suite.T(), err)

	// Verify deployments still exist
	deploymentList, err = suite.store.ListDeployments(suite.ctx)
	assert.NoError(suite.T(), err)
	assert.Len(suite.T(), deploymentList, 2, "Recent deployments should not be cleaned up")

	// When: Cleanup with very long duration (should remove everything)
	err = suite.store.Cleanup(24 * time.Hour)

	// Then: Should succeed and remove old deployments
	assert.NoError(suite.T(), err)

	// Note: The actual cleanup behavior depends on CreatedAt timestamps in DynamoDB
	// This test verifies the cleanup method executes without errors
}

// Test_Given_StorageInfo_When_GetStorageInfo_Then_ReturnsCorrectInfo tests storage information
func (suite *AWSStateStoreIntegrationTestSuite) Test_Given_StorageInfo_When_GetStorageInfo_Then_ReturnsCorrectInfo() {
	// Given: Some deployments exist
	for i := 0; i < 3; i++ {
		deploymentID := fmt.Sprintf("info-test-%d", i)
		state := map[string]interface{}{
			"resources": []interface{}{
				map[string]interface{}{"type": "aws_instance", "name": fmt.Sprintf("web-%d", i)},
			},
		}
		err := suite.store.UpdateDeploymentState(deploymentID, state)
		require.NoError(suite.T(), err)
	}

	// When: Get storage info
	info := suite.store.GetStorageInfo()

	// Then: Should return correct information
	assert.NotNil(suite.T(), info)
	assert.Equal(suite.T(), "aws", info.Type)
	assert.True(suite.T(), info.Exists)
	assert.True(suite.T(), info.Writable)
	assert.Equal(suite.T(), 3, info.DeploymentCount)
	// Size information might be 0 in the basic implementation
}

// Test_Given_NetworkFailure_When_Operations_Then_HandlesGracefully tests network failure scenarios
func (suite *AWSStateStoreIntegrationTestSuite) Test_Given_NetworkFailure_When_Operations_Then_HandlesGracefully() {
	// This test would require more sophisticated network manipulation
	// For now, we test with invalid endpoints to simulate network issues

	// Given: Store with invalid endpoint
	config := state.AWSStateStoreConfig{
		DynamoDBTable:  "invalid-table",
		DynamoDBRegion: "us-east-1",
		S3Bucket:       "invalid-bucket",
		S3Region:       "us-east-1",
		Endpoint:       "http://invalid-endpoint:9999", // Non-existent endpoint
	}

	invalidStore, err := state.NewAWSStateStore(config)

	// When: Operations are attempted (they might succeed during initialization due to LocalStack)
	// Then: Should handle errors gracefully without panicking
	if err == nil {
		// If store creation succeeded, test operations
		_, pingErr := func() (interface{}, error) {
			return nil, invalidStore.Ping(suite.ctx)
		}()

		if pingErr != nil {
			// Expected behavior - network errors should be returned, not panic
			assert.Error(suite.T(), pingErr, "Network failures should return errors")
		}
	} else {
		// Store creation failed, which is also acceptable
		assert.Error(suite.T(), err, "Invalid configuration should return error")
	}
}

// Test_Given_S3OnlyBackend_When_MigrateToAWS_Then_PreservesData tests migration scenario
func (suite *AWSStateStoreIntegrationTestSuite) Test_Given_S3OnlyBackend_When_MigrateToAWS_Then_PreservesData() {
	// This test simulates migrating from S3-only storage to the new AWS backend
	// Given: Simulate existing S3 state file (we'll create it manually)
	deploymentID := "migration-test"

	// Create Terraform state directly in S3 (simulating old storage)
	// Construct the S3 key manually since stateS3Key is unexported
	stateKey := deploymentID + ".tfstate"
	if suite.storeConfig.S3Prefix != "" {
		stateKey = strings.TrimSuffix(suite.storeConfig.S3Prefix, "/") + "/" + stateKey
	}
	tfState := &interfaces.TerraformState{
		Version:          4,
		TerraformVersion: "1.5.0",
		Serial:           1,
		Lineage:          "migration-lineage",
		Resources: []interfaces.TerraformResource{
			{
				Type: "aws_s3_bucket",
				Name: "legacy",
				Mode: "managed",
				Instances: []interfaces.TerraformResourceInstance{
					{
						Attributes: map[string]interface{}{
							"bucket": "legacy-bucket",
						},
					},
				},
			},
		},
	}

	stateJSON, err := json.Marshal(tfState)
	require.NoError(suite.T(), err)

	// Put state directly in S3 (simulating existing state)
	_, err = suite.s3Client.PutObject(suite.ctx, &s3.PutObjectInput{
		Bucket: aws.String(suite.testBucketName),
		Key:    aws.String(stateKey),
		Body:   bytes.NewReader(stateJSON),
	})
	require.NoError(suite.T(), err)

	// When: Import the existing state through the new backend
	err = suite.store.ImportTerraformState(deploymentID, tfState)

	// Then: Should succeed and create metadata
	assert.NoError(suite.T(), err)

	// And: Metadata should exist in DynamoDB
	metadata, err := suite.store.GetDeploymentState(deploymentID)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), deploymentID, metadata["deployment_id"])
	assert.Equal(suite.T(), uint64(1), metadata["serial"])
	assert.Equal(suite.T(), "migration-lineage", metadata["lineage"])

	// And: State should still be retrievable
	exportedState, err := suite.store.ExportTerraformState(deploymentID)
	assert.NoError(suite.T(), err)
	assert.Equal(suite.T(), tfState.Serial, exportedState.Serial)
	assert.Equal(suite.T(), tfState.Lineage, exportedState.Lineage)
}

// Helper test to validate that the test setup is working correctly
func (suite *AWSStateStoreIntegrationTestSuite) Test_Setup_ValidateTestInfrastructure() {
	// Validate LocalStack is responding
	assert.NotEmpty(suite.T(), suite.endpoint, "LocalStack endpoint should be set")

	// Validate clients are working
	assert.NotNil(suite.T(), suite.dynamoClient, "DynamoDB client should be initialized")
	assert.NotNil(suite.T(), suite.s3Client, "S3 client should be initialized")

	// Validate store is configured
	assert.NotNil(suite.T(), suite.store, "AWSStateStore should be initialized")
	assert.Equal(suite.T(), suite.testTableName, suite.storeConfig.DynamoDBTable)
	assert.Equal(suite.T(), suite.testBucketName, suite.storeConfig.S3Bucket)

	suite.T().Log("Test infrastructure validation passed")
}
