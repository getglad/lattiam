package helpers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/lattiam/lattiam/internal/state"
)

// LocalStackHelper provides utilities for LocalStack testing
type LocalStackHelper struct {
	t            *testing.T
	awsConfig    aws.Config
	s3Client     *s3.Client
	dynamoClient *dynamodb.Client
	Endpoint     string
	region       string
}

// NewLocalStackHelper creates a new LocalStack helper for testing
func NewLocalStackHelper(t *testing.T) *LocalStackHelper {
	t.Helper()

	// Use existing LocalStack endpoint if available, otherwise default to localhost
	endpoint := getLocalStackEndpoint()

	helper := &LocalStackHelper{
		t:        t,
		Endpoint: endpoint,
		region:   "us-east-1",
	}

	// Initialize AWS config
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	awsConfig, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(helper.region),
		config.WithCredentialsProvider(&staticCredentials{}),
	)
	if err != nil {
		t.Fatalf("Failed to load AWS config for LocalStack: %v", err)
	}

	helper.awsConfig = awsConfig

	// Create S3 client
	helper.s3Client = s3.NewFromConfig(awsConfig, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(helper.Endpoint)
		o.UsePathStyle = true
	})

	// Create DynamoDB client
	helper.dynamoClient = dynamodb.NewFromConfig(awsConfig, func(o *dynamodb.Options) {
		o.BaseEndpoint = aws.String(helper.Endpoint)
	})

	return helper
}

// staticCredentials provides static test credentials for LocalStack
type staticCredentials struct{}

func (c *staticCredentials) Retrieve(_ context.Context) (aws.Credentials, error) {
	return aws.Credentials{
		AccessKeyID:     "test",
		SecretAccessKey: "test",
		SessionToken:    "",
	}, nil
}

// getLocalStackEndpoint determines the correct LocalStack endpoint
func getLocalStackEndpoint() string {
	// Check if running inside a container (Docker network)
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return "http://localstack:4566"
	}

	// Check environment override
	if endpoint := os.Getenv("LOCALSTACK_ENDPOINT"); endpoint != "" {
		return endpoint
	}

	// Default to localhost
	return "http://localhost:4566"
}

// IsLocalStackAvailable checks if LocalStack is running and accessible
func (h *LocalStackHelper) IsLocalStackAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Try a simple S3 operation
	_, err := h.s3Client.ListBuckets(ctx, &s3.ListBucketsInput{})
	return err == nil
}

// WaitForLocalStack waits for LocalStack to be ready
func (h *LocalStackHelper) WaitForLocalStack(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for LocalStack to be ready")
		case <-ticker.C:
			if h.IsLocalStackAvailable() {
				return nil
			}
		}
	}
}

// CreateTestBucket creates a test S3 bucket with a unique name
func (h *LocalStackHelper) CreateTestBucket(prefix string) (bucketName string, cleanup func()) {
	h.t.Helper()

	bucketName = h.GenerateTestBucketName(prefix)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create bucket
	_, err := h.s3Client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucketName),
	})
	if err != nil {
		h.t.Fatalf("Failed to create test bucket %s: %v", bucketName, err)
	}

	// Return cleanup function
	cleanup = func() {
		h.CleanupTestBucket(bucketName)
	}

	return bucketName, cleanup
}

// GenerateTestBucketName generates a unique test bucket name
func (h *LocalStackHelper) GenerateTestBucketName(prefix string) string {
	if prefix == "" {
		prefix = "lattiam-test"
	}

	// Generate random suffix
	randomBytes := make([]byte, 4)
	if _, err := rand.Read(randomBytes); err != nil {
		h.t.Fatalf("Failed to generate random bytes: %v", err)
	}

	suffix := hex.EncodeToString(randomBytes)
	timestamp := time.Now().Format("20060102-150405")

	return fmt.Sprintf("%s-%s-%s", prefix, timestamp, suffix)
}

// CleanupTestBucket removes a test bucket and all its contents
func (h *LocalStackHelper) CleanupTestBucket(bucketName string) {
	h.t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// List and delete all objects
	paginator := s3.NewListObjectsV2Paginator(h.s3Client, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucketName),
	})

	var objectsToDelete []s3types.ObjectIdentifier

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			h.t.Logf("Warning: Failed to list objects in bucket %s: %v", bucketName, err)
			return
		}

		for _, obj := range page.Contents {
			objectsToDelete = append(objectsToDelete, s3types.ObjectIdentifier{
				Key: obj.Key,
			})
		}
	}

	// Delete objects in batches
	if len(objectsToDelete) > 0 {
		const batchSize = 1000
		for i := 0; i < len(objectsToDelete); i += batchSize {
			end := i + batchSize
			if end > len(objectsToDelete) {
				end = len(objectsToDelete)
			}

			_, err := h.s3Client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
				Bucket: aws.String(bucketName),
				Delete: &s3types.Delete{
					Objects: objectsToDelete[i:end],
					Quiet:   aws.Bool(true),
				},
			})
			if err != nil {
				h.t.Logf("Warning: Failed to delete objects from bucket %s: %v", bucketName, err)
			}
		}
	}

	// Delete bucket
	_, err := h.s3Client.DeleteBucket(ctx, &s3.DeleteBucketInput{
		Bucket: aws.String(bucketName),
	})
	if err != nil {
		h.t.Logf("Warning: Failed to delete bucket %s: %v", bucketName, err)
	}
}

// CreateTestDynamoDBTable creates a test DynamoDB table for locking
func (h *LocalStackHelper) CreateTestDynamoDBTable(tableName string) func() {
	h.t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create table
	_, err := h.dynamoClient.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName: aws.String(tableName),
		KeySchema: []types.KeySchemaElement{
			{
				AttributeName: aws.String("LockID"),
				KeyType:       types.KeyTypeHash,
			},
		},
		AttributeDefinitions: []types.AttributeDefinition{
			{
				AttributeName: aws.String("LockID"),
				AttributeType: types.ScalarAttributeTypeS,
			},
		},
		BillingMode: types.BillingModePayPerRequest,
	})
	if err != nil {
		h.t.Fatalf("Failed to create test DynamoDB table %s: %v", tableName, err)
	}

	// Wait for table to be active
	waiter := dynamodb.NewTableExistsWaiter(h.dynamoClient)
	if err := waiter.Wait(ctx, &dynamodb.DescribeTableInput{
		TableName: aws.String(tableName),
	}, 2*time.Minute); err != nil {
		h.t.Fatalf("Timeout waiting for table %s to become active: %v", tableName, err)
	}

	// Return cleanup function
	cleanup := func() {
		h.CleanupTestDynamoDBTable(tableName)
	}

	return cleanup
}

// CleanupTestDynamoDBTable removes a test DynamoDB table
func (h *LocalStackHelper) CleanupTestDynamoDBTable(tableName string) {
	h.t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	_, err := h.dynamoClient.DeleteTable(ctx, &dynamodb.DeleteTableInput{
		TableName: aws.String(tableName),
	})
	if err != nil {
		h.t.Logf("Warning: Failed to delete DynamoDB table %s: %v", tableName, err)
	}
}

// CreateS3StateFileStore creates an S3 state file store configured for LocalStack
// Note: This only handles Terraform state files, not full state management
func (h *LocalStackHelper) CreateS3StateFileStore(bucketName string, cfg *state.S3StateFileStoreConfig) (*state.S3StateFileStore, error) {
	storeConfig := state.S3StateFileStoreConfig{
		Bucket:   bucketName,
		Region:   h.region,
		Endpoint: h.Endpoint,
	}

	// Override with provided config if any
	if cfg != nil {
		if cfg.Prefix != "" {
			storeConfig.Prefix = cfg.Prefix
		}
	}

	store, err := state.NewS3StateFileStore(storeConfig)
	if err != nil {
		return nil, err //nolint:wrapcheck // test helper function
	}
	return store, nil
}

// CreateDynamoDBLockConfig creates a DynamoDB lock config for LocalStack
func (h *LocalStackHelper) CreateDynamoDBLockConfig(tableName string) *state.DynamoDBLockConfig {
	return &state.DynamoDBLockConfig{
		TableName:        tableName,
		Region:           h.region,
		Endpoint:         h.Endpoint,
		TTL:              15 * time.Minute,
		RefreshInterval:  3 * time.Minute,
		TimeoutOnAcquire: 10 * time.Second,
	}
}

// GenerateTestData creates test deployment and resource data
func (h *LocalStackHelper) GenerateTestData() TestDataSet {
	return TestDataSet{
		DeploymentID: fmt.Sprintf("test-deployment-%d", time.Now().Unix()),
		ResourceData: map[string]map[string]interface{}{
			"aws_s3_bucket.example": {
				"id":     "test-bucket-12345",
				"bucket": "test-bucket-12345",
				"region": "us-east-1",
				"arn":    "arn:aws:s3:::test-bucket-12345",
			},
			"aws_security_group.web": {
				"id":          "sg-12345",
				"name":        "web-sg",
				"description": "Web security group",
				"vpc_id":      "vpc-12345",
			},
		},
		DeploymentState: map[string]interface{}{
			"status":      "deployed",
			"created_at":  time.Now().Add(-1 * time.Hour).Format(time.RFC3339),
			"updated_at":  time.Now().Format(time.RFC3339),
			"version":     1,
			"description": "Test deployment for S3 backend testing",
		},
	}
}

// TestDataSet holds test data for deployments and resources
type TestDataSet struct {
	DeploymentID    string
	ResourceData    map[string]map[string]interface{}
	DeploymentState map[string]interface{}
}

// CreateMultipleTestData creates multiple test data sets for batch testing
func (h *LocalStackHelper) CreateMultipleTestData(count int) []TestDataSet {
	var dataSets []TestDataSet

	for i := 0; i < count; i++ {
		baseData := h.GenerateTestData()
		baseData.DeploymentID = fmt.Sprintf("test-deployment-%d-%d", i, time.Now().Unix())

		// Vary the data slightly for each deployment
		for _, resourceData := range baseData.ResourceData {
			if bucket, ok := resourceData["bucket"].(string); ok {
				resourceData["bucket"] = fmt.Sprintf("%s-%d", bucket, i)
				resourceData["id"] = fmt.Sprintf("%s-%d", resourceData["id"], i)
				if arn, ok := resourceData["arn"].(string); ok {
					resourceData["arn"] = strings.Replace(arn, "test-bucket-12345", fmt.Sprintf("test-bucket-12345-%d", i), 1)
				}
			}
		}

		dataSets = append(dataSets, baseData)
	}

	return dataSets
}

// SkipIfLocalStackUnavailable skips the test if LocalStack is not available
func (h *LocalStackHelper) SkipIfLocalStackUnavailable() {
	if !h.IsLocalStackAvailable() {
		h.t.Skip("LocalStack is not available - skipping integration test")
	}
}

// AssertS3ObjectExists checks if an S3 object exists
func (h *LocalStackHelper) AssertS3ObjectExists(bucketName, key string) {
	h.t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := h.s3Client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(key),
	})
	if err != nil {
		h.t.Errorf("Expected S3 object %s/%s to exist, but got error: %v", bucketName, key, err)
	}
}

// AssertS3ObjectNotExists checks if an S3 object does not exist
func (h *LocalStackHelper) AssertS3ObjectNotExists(bucketName, key string) {
	h.t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := h.s3Client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(key),
	})
	if err == nil {
		h.t.Errorf("Expected S3 object %s/%s to not exist, but it does", bucketName, key)
	}
}

// CountS3Objects counts the number of objects in a bucket with an optional prefix
func (h *LocalStackHelper) CountS3Objects(bucketName, prefix string) int {
	h.t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(bucketName),
	}
	if prefix != "" {
		input.Prefix = aws.String(prefix)
	}

	paginator := s3.NewListObjectsV2Paginator(h.s3Client, input)
	count := 0

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			h.t.Fatalf("Failed to list S3 objects: %v", err)
		}
		count += len(page.Contents)
	}

	return count
}

// GetDynamoDBItemCount gets the number of items in a DynamoDB table
func (h *LocalStackHelper) GetDynamoDBItemCount(tableName string) int {
	h.t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := h.dynamoClient.Scan(ctx, &dynamodb.ScanInput{
		TableName: aws.String(tableName),
		Select:    types.SelectCount,
	})
	if err != nil {
		h.t.Fatalf("Failed to scan DynamoDB table %s: %v", tableName, err)
	}

	return int(result.Count)
}
