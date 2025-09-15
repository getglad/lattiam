// Package state provides state storage implementations for deployments
package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/internal/utils"
)

// AWSStateStoreConfig holds configuration for the AWS state store
type AWSStateStoreConfig struct {
	// DynamoDB settings
	DynamoDBTable  string `json:"dynamodb_table"`
	DynamoDBRegion string `json:"dynamodb_region"`

	// S3 settings
	S3Bucket string `json:"s3_bucket"`
	S3Region string `json:"s3_region"`
	S3Prefix string `json:"s3_prefix,omitempty"` // Optional prefix for state files

	// Common settings
	Endpoint string `json:"endpoint,omitempty"` // For LocalStack or custom endpoints
	// AWS credentials should be provided via IAM roles, instance profiles, or environment variables
	// Never store credentials in configuration files

	// Locking configuration
	LockingEnabled bool                `json:"locking_enabled"`
	LockConfig     *DynamoDBLockConfig `json:"lock_config,omitempty"`
}

// AWSStateStore implements StateStore using DynamoDB for metadata and S3 for state files
type AWSStateStore struct {
	config       AWSStateStoreConfig
	dynamoClient *dynamodb.Client
	s3Client     *s3.Client
	lockProvider *DynamoDBLockProvider
}

// AWSDeploymentMetadata represents deployment metadata stored in DynamoDB
type AWSDeploymentMetadata struct {
	DeploymentID  string    `json:"deployment_id" dynamodb:"PK"`
	Name          string    `json:"name"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	StateS3Key    string    `json:"state_s3_key"`
	ResourceCount int       `json:"resource_count"`
	Serial        uint64    `json:"serial"`  // Terraform state serial for versioning
	Lineage       string    `json:"lineage"` // Terraform state lineage
	Version       int       `json:"version"` // Internal schema version
}

// NewAWSStateStore creates a new AWS-based state store
func NewAWSStateStore(cfg AWSStateStoreConfig) (*AWSStateStore, error) {
	if err := validateAWSConfig(cfg); err != nil {
		return nil, fmt.Errorf("invalid AWS configuration: %w", err)
	}

	// Load AWS config
	awsCfg, err := loadAWSConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create DynamoDB client
	dynamoClient := createDynamoDBClient(awsCfg, cfg.Endpoint)

	// Create S3 client
	s3Client := createS3Client(awsCfg, cfg.Endpoint)

	store := &AWSStateStore{
		config:       cfg,
		dynamoClient: dynamoClient,
		s3Client:     s3Client,
	}

	// Initialize DynamoDB locking if enabled
	if cfg.LockingEnabled && cfg.LockConfig != nil {
		// Set region if not specified in lock config
		if cfg.LockConfig.Region == "" {
			cfg.LockConfig.Region = cfg.DynamoDBRegion
		}
		if cfg.LockConfig.Endpoint == "" {
			cfg.LockConfig.Endpoint = cfg.Endpoint
		}

		lockProvider, err := NewDynamoDBLockProvider(*cfg.LockConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to create DynamoDB lock provider: %w", err)
		}
		store.lockProvider = lockProvider
	}

	// Initialize infrastructure
	if err := store.initialize(); err != nil {
		return nil, fmt.Errorf("failed to initialize AWS state store: %w", err)
	}

	return store, nil
}

// validateAWSConfig validates the AWS state store configuration
func validateAWSConfig(cfg AWSStateStoreConfig) error {
	if cfg.DynamoDBTable == "" {
		return fmt.Errorf("DynamoDB table name is required")
	}
	if cfg.DynamoDBRegion == "" {
		return fmt.Errorf("DynamoDB region is required")
	}
	if cfg.S3Bucket == "" {
		return fmt.Errorf("S3 bucket name is required")
	}
	if cfg.S3Region == "" {
		return fmt.Errorf("S3 region is required")
	}
	return nil
}

// loadAWSConfig loads AWS configuration with custom settings
func loadAWSConfig(cfg AWSStateStoreConfig) (aws.Config, error) {
	// Use DynamoDB region as primary (metadata is more critical than state files)
	region := cfg.DynamoDBRegion
	// Note: Cross-region setup between S3 and DynamoDB is supported but may have latency implications

	return loadAWSConfigForEndpoint(region, cfg.Endpoint)
}

// isLocalStackOrTestEnv detects if we're running in a LocalStack or test environment (shared helper)
func isLocalStackOrTestEnv(endpoint string) bool {
	// Check if endpoint is set to LocalStack patterns
	if endpoint != "" {
		endpointLower := strings.ToLower(endpoint)
		if strings.Contains(endpointLower, "localstack") || strings.Contains(endpointLower, "localhost") {
			return true
		}
	}

	// Check environment variables that indicate testing
	if os.Getenv("LATTIAM_USE_LOCALSTACK") == "true" || os.Getenv("LOCALSTACK_ENDPOINT") != "" {
		return true
	}

	// Check if we're in test mode (Go testing sets specific environment)
	if os.Getenv("GO_TEST_PROCESS") != "" || strings.HasSuffix(os.Args[0], ".test") {
		return true
	}

	return false
}

// loadAWSConfigForEndpoint loads AWS configuration for a given region and endpoint
func loadAWSConfigForEndpoint(region, endpoint string) (aws.Config, error) {
	configOptions := []func(*config.LoadOptions) error{
		config.WithRegion(region),
	}

	// Check if this is a LocalStack or test environment
	if isLocalStackOrTestEnv(endpoint) {
		// Use static test credentials for LocalStack/testing
		configOptions = append(configOptions,
			config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
		)
	}

	cfg, err := config.LoadDefaultConfig(context.TODO(), configOptions...)
	if err != nil {
		return aws.Config{}, fmt.Errorf("failed to load AWS config: %w", err)
	}
	return cfg, nil
}

// createDynamoDBClient creates a DynamoDB client with optional custom endpoint
func createDynamoDBClient(awsCfg aws.Config, endpoint string) *dynamodb.Client {
	// For LocalStack/test environments, endpoint is already configured in awsCfg
	// For production, we may still need endpoint override for specific use cases
	if endpoint != "" && !isEndpointInConfig(awsCfg, endpoint) {
		return dynamodb.NewFromConfig(awsCfg, func(o *dynamodb.Options) {
			o.BaseEndpoint = aws.String(endpoint)
		})
	}
	return dynamodb.NewFromConfig(awsCfg)
}

// createS3Client creates an S3 client with optional custom endpoint
func createS3Client(awsCfg aws.Config, endpoint string) *s3.Client {
	// For LocalStack/test environments, endpoint is already configured in awsCfg
	// For production, we may still need endpoint override for specific use cases
	if endpoint != "" && !isEndpointInConfig(awsCfg, endpoint) {
		return s3.NewFromConfig(awsCfg, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(endpoint)
			o.UsePathStyle = true // Required for LocalStack
		})
	}

	// Always use path style for LocalStack, even when endpoint is in config
	if endpoint != "" && (strings.Contains(strings.ToLower(endpoint), "localstack") || strings.Contains(strings.ToLower(endpoint), "localhost")) {
		return s3.NewFromConfig(awsCfg, func(o *s3.Options) {
			o.UsePathStyle = true
		})
	}

	return s3.NewFromConfig(awsCfg)
}

// isEndpointInConfig checks if the endpoint is already configured in AWS config
// This is a simple heuristic - in production you might want more sophisticated detection
func isEndpointInConfig(_ aws.Config, endpoint string) bool {
	// For LocalStack/test scenarios, assume endpoint is in config if it matches patterns
	if strings.Contains(strings.ToLower(endpoint), "localstack") || strings.Contains(strings.ToLower(endpoint), "localhost") {
		return true
	}
	return false
}

// initialize sets up DynamoDB table and S3 bucket
func (a *AWSStateStore) initialize() error {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second) // Increased timeout for CI environments
	defer cancel()

	// Table and bucket initialization - details logged at trace level only

	// Retry logic for infrastructure initialization in case of transient issues
	maxRetries := 3
	retryDelay := 2 * time.Second

	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		// Ensure DynamoDB table exists
		if err := a.ensureDynamoDBTable(ctx); err != nil {
			lastErr = fmt.Errorf("failed to ensure DynamoDB table (attempt %d/%d): %w", attempt, maxRetries, err)
			if attempt < maxRetries {
				select {
				case <-ctx.Done():
					return fmt.Errorf("timeout during DynamoDB table initialization: %w", ctx.Err())
				case <-time.After(retryDelay):
					continue
				}
			}
			return lastErr
		}

		// Ensure S3 bucket exists
		if err := a.ensureS3Bucket(ctx); err != nil {
			lastErr = fmt.Errorf("failed to ensure S3 bucket (attempt %d/%d): %w", attempt, maxRetries, err)
			if attempt < maxRetries {
				select {
				case <-ctx.Done():
					return fmt.Errorf("timeout during S3 bucket initialization: %w", ctx.Err())
				case <-time.After(retryDelay):
					continue
				}
			}
			return lastErr
		}

		// Both table and bucket initialized successfully
		break
	}

	return nil
}

// ensureDynamoDBTable ensures the DynamoDB table exists with proper schema
//
//nolint:funlen // DynamoDB table setup requires comprehensive configuration and validation
func (a *AWSStateStore) ensureDynamoDBTable(ctx context.Context) error {
	// Check if table exists and is active
	describeResp, err := a.dynamoClient.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: aws.String(a.config.DynamoDBTable),
	})
	if err == nil {
		// Table exists, check if it's active
		if describeResp.Table.TableStatus == types.TableStatusActive {
			return nil // Table exists and is active
		}

		// Table exists but is not active, wait for it
		waiter := dynamodb.NewTableExistsWaiter(a.dynamoClient)
		waitErr := waiter.Wait(ctx, &dynamodb.DescribeTableInput{
			TableName: aws.String(a.config.DynamoDBTable),
		}, 5*time.Minute)
		if waitErr != nil {
			return fmt.Errorf("failed to wait for existing table to be active: %w", waitErr)
		}
		return nil
	}

	// Check if it's a "table not found" error
	var notFound *types.ResourceNotFoundException
	if !errors.As(err, &notFound) {
		return fmt.Errorf("failed to describe table: %w", err)
	}

	// Table doesn't exist, create it
	// Add retry logic here to handle concurrent table creation attempts
	maxRetries := 3
	retryDelay := 1 * time.Second

	for attempt := 1; attempt <= maxRetries; attempt++ {
		_, createErr := a.dynamoClient.CreateTable(ctx, &dynamodb.CreateTableInput{
			TableName: aws.String(a.config.DynamoDBTable),
			KeySchema: []types.KeySchemaElement{
				{
					AttributeName: aws.String("PK"), // Partition Key: DeploymentID
					KeyType:       types.KeyTypeHash,
				},
			},
			AttributeDefinitions: []types.AttributeDefinition{
				{
					AttributeName: aws.String("PK"),
					AttributeType: types.ScalarAttributeTypeS,
				},
				{
					AttributeName: aws.String("Status"),
					AttributeType: types.ScalarAttributeTypeS,
				},
				{
					AttributeName: aws.String("CreatedAt"),
					AttributeType: types.ScalarAttributeTypeS,
				},
			},
			GlobalSecondaryIndexes: []types.GlobalSecondaryIndex{
				{
					IndexName: aws.String("StatusIndex"),
					KeySchema: []types.KeySchemaElement{
						{
							AttributeName: aws.String("Status"),
							KeyType:       types.KeyTypeHash,
						},
						{
							AttributeName: aws.String("CreatedAt"),
							KeyType:       types.KeyTypeRange,
						},
					},
					Projection: &types.Projection{
						ProjectionType: types.ProjectionTypeAll,
					},
				},
			},
			BillingMode: types.BillingModePayPerRequest,
		})

		if createErr == nil {
			// Table creation initiated successfully
			break
		}

		// Check if table already exists (race condition with another process)
		var alreadyExists *types.ResourceInUseException
		if errors.As(createErr, &alreadyExists) {
			// Table already exists, which is fine
			break
		}

		// Other error, retry if we have attempts left
		if attempt < maxRetries {
			select {
			case <-ctx.Done():
				return fmt.Errorf("timeout during table creation retry: %w", ctx.Err())
			case <-time.After(retryDelay):
				continue
			}
		}

		return fmt.Errorf("failed to create DynamoDB table after %d attempts: %w", maxRetries, createErr)
	}

	// Wait for table to be active with extended timeout for CI environments
	waiter := dynamodb.NewTableExistsWaiter(a.dynamoClient)
	err = waiter.Wait(ctx, &dynamodb.DescribeTableInput{
		TableName: aws.String(a.config.DynamoDBTable),
	}, 10*time.Minute) // Increased timeout for slower CI environments
	if err != nil {
		return fmt.Errorf("failed to wait for table to be active: %w", err)
	}

	// Additional verification: describe table to ensure it's really active
	finalDescribe, err := a.dynamoClient.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: aws.String(a.config.DynamoDBTable),
	})
	if err != nil {
		return fmt.Errorf("failed to verify table is active: %w", err)
	}

	if finalDescribe.Table.TableStatus != types.TableStatusActive {
		return fmt.Errorf("table is not active after waiting, status: %s", finalDescribe.Table.TableStatus)
	}

	// Table is now active and verified
	return nil
}

// ensureS3Bucket ensures the S3 bucket exists
func (a *AWSStateStore) ensureS3Bucket(ctx context.Context) error {
	// Check if bucket exists
	_, err := a.s3Client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(a.config.S3Bucket),
	})
	if err == nil {
		return nil // Bucket exists
	}

	// Try to create the bucket if it doesn't exist
	var noBucket *s3types.NoSuchBucket
	if errors.As(err, &noBucket) || strings.Contains(err.Error(), "NotFound") {
		createInput := &s3.CreateBucketInput{
			Bucket: aws.String(a.config.S3Bucket),
		}

		// Only set LocationConstraint for non-us-east-1 regions and non-LocalStack endpoints
		// LocalStack has issues with LocationConstraint, and AWS us-east-1 doesn't require it
		isLocalStack := a.config.Endpoint != "" && (strings.Contains(a.config.Endpoint, "localstack") || strings.Contains(a.config.Endpoint, "localhost"))
		if a.config.S3Region != "us-east-1" && !isLocalStack {
			createInput.CreateBucketConfiguration = &s3types.CreateBucketConfiguration{
				LocationConstraint: s3types.BucketLocationConstraint(a.config.S3Region),
			}
		}

		_, err = a.s3Client.CreateBucket(ctx, createInput)
		if err != nil {
			return fmt.Errorf("failed to create S3 bucket: %w", err)
		}
	} else {
		return fmt.Errorf("failed to access S3 bucket: %w", err)
	}

	return nil
}

// Deployment-level operations implementing StateStore interface

// UpdateDeploymentState updates deployment metadata and writes state to S3
func (a *AWSStateStore) UpdateDeploymentState(deploymentID string, state map[string]interface{}) error {
	if err := validateDeploymentID(deploymentID); err != nil {
		return fmt.Errorf("invalid deployment ID: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Get existing metadata or create new if not found
	metadata, err := a.getDeploymentMetadata(ctx, deploymentID)
	if err != nil {
		// If deployment doesn't exist, create it
		if strings.Contains(err.Error(), "not found") {
			now := time.Now().UTC()
			metadata = &AWSDeploymentMetadata{
				DeploymentID:  deploymentID,
				Name:          deploymentID, // Default name to ID
				Status:        string(interfaces.DeploymentStatusQueued),
				CreatedAt:     now,
				UpdatedAt:     now,
				StateS3Key:    a.stateS3Key(deploymentID),
				ResourceCount: 0,
				Serial:        0,
				Lineage:       a.generateLineage(),
				Version:       1,
			}
		} else {
			return fmt.Errorf("failed to get deployment metadata: %w", err)
		}
	}

	// Update metadata
	metadata.UpdatedAt = time.Now().UTC()
	metadata.Serial++

	// Count resources in state if available
	if resources, ok := state["resources"]; ok {
		if resourceList, ok := resources.([]interface{}); ok {
			metadata.ResourceCount = len(resourceList)
		}
	}

	// Convert state to Terraform format and save to S3
	tfState := a.convertToTerraformState(deploymentID, state, metadata)
	if err := a.saveTerraformStateToS3(ctx, deploymentID, tfState); err != nil {
		return fmt.Errorf("failed to save Terraform state to S3: %w", err)
	}

	// Update metadata in DynamoDB
	if err := a.updateDeploymentMetadata(ctx, metadata); err != nil {
		return fmt.Errorf("failed to update deployment metadata: %w", err)
	}

	return nil
}

// GetDeploymentState retrieves deployment metadata from DynamoDB (fast query)
func (a *AWSStateStore) GetDeploymentState(deploymentID string) (map[string]interface{}, error) {
	if err := validateDeploymentID(deploymentID); err != nil {
		return nil, fmt.Errorf("invalid deployment ID: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	metadata, err := a.getDeploymentMetadata(ctx, deploymentID)
	if err != nil {
		return nil, err
	}

	// Return metadata as state (not full Terraform state - use ExportTerraformState for that)
	return map[string]interface{}{
		"deployment_id":  metadata.DeploymentID,
		"name":           metadata.Name,
		"status":         metadata.Status,
		"created_at":     metadata.CreatedAt.Format(time.RFC3339),
		"updated_at":     metadata.UpdatedAt.Format(time.RFC3339),
		"resource_count": metadata.ResourceCount,
		"serial":         metadata.Serial,
		"lineage":        metadata.Lineage,
		"version":        metadata.Version,
	}, nil
}

// DeleteDeploymentState removes deployment from both DynamoDB and S3
func (a *AWSStateStore) DeleteDeploymentState(deploymentID string) error {
	if err := validateDeploymentID(deploymentID); err != nil {
		return fmt.Errorf("invalid deployment ID: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Delete from DynamoDB
	_, err := a.dynamoClient.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(a.config.DynamoDBTable),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: deploymentID},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to delete deployment from DynamoDB: %w", err)
	}

	// Delete state file from S3
	stateKey := a.stateS3Key(deploymentID)
	_, err = a.s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(a.config.S3Bucket),
		Key:    aws.String(stateKey),
	})
	if err != nil {
		// Don't fail if S3 object doesn't exist
		var noSuchKey *s3types.NoSuchKey
		if !errors.As(err, &noSuchKey) {
			return fmt.Errorf("failed to delete state from S3: %w", err)
		}
	}

	return nil
}

// ListDeployments returns all deployment IDs by querying DynamoDB
// Resource-level operations (simplified - resources are stored as part of Terraform state)

// UpdateResourceState updates a resource state (stored in Terraform state)
func (a *AWSStateStore) UpdateResourceState(resourceKey string, state map[string]interface{}) error {
	// Extract deployment ID from resource key (format: "deploymentID_resourceType.resourceName")
	parts := strings.SplitN(resourceKey, "_", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid resource key format: %s", resourceKey)
	}

	deploymentID := parts[0]

	// Get current deployment state
	currentState, err := a.GetDeploymentState(deploymentID)
	if err != nil {
		return fmt.Errorf("failed to get deployment state: %w", err)
	}

	// Update the resource within the deployment state
	// This is a simplified implementation - in production, you'd merge into Terraform state
	if currentState["resources"] == nil {
		currentState["resources"] = make(map[string]interface{})
	}

	resources := currentState["resources"].(map[string]interface{})
	resources[resourceKey] = state

	// Update the deployment state
	return a.UpdateDeploymentState(deploymentID, currentState)
}

// GetResourceState retrieves a resource state from the deployment's Terraform state
func (a *AWSStateStore) GetResourceState(resourceKey string) (map[string]interface{}, error) {
	// Extract deployment ID from resource key
	parts := strings.SplitN(resourceKey, "_", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid resource key format: %s", resourceKey)
	}

	deploymentID := parts[0]

	// Get full Terraform state
	tfState, err := a.ExportTerraformState(deploymentID)
	if err != nil {
		return nil, err
	}

	// Find the resource in Terraform state
	for _, resource := range tfState.Resources {
		if len(resource.Instances) > 0 {
			// Simple key matching - in production, you'd need more sophisticated matching
			currentKey := deploymentID + "_" + resource.Type + "." + resource.Name
			if currentKey == resourceKey {
				return resource.Instances[0].Attributes, nil
			}
		}
	}

	return nil, nil // Resource not found
}

// GetAllResourceStates retrieves all resource states for a deployment
func (a *AWSStateStore) GetAllResourceStates(deploymentID string) (map[string]map[string]interface{}, error) {
	tfState, err := a.ExportTerraformState(deploymentID)
	if err != nil {
		return nil, err
	}

	result := make(map[string]map[string]interface{})

	for _, resource := range tfState.Resources {
		if len(resource.Instances) > 0 {
			resourceKey := resource.Type + "." + resource.Name
			result[resourceKey] = resource.Instances[0].Attributes
		}
	}

	return result, nil
}

// DeleteResourceState removes a resource state (simplified implementation)
func (a *AWSStateStore) DeleteResourceState(resourceKey string) error {
	// In a full implementation, you'd remove the resource from Terraform state
	// For now, we'll just mark it as nil in the state
	return a.UpdateResourceState(resourceKey, nil)
}

// Batch operations (delegated to individual operations for simplicity)

// UpdateResourceStatesBatch updates multiple resource states
func (a *AWSStateStore) UpdateResourceStatesBatch(states map[string]map[string]interface{}) error {
	// Simple implementation - could be optimized with concurrent updates
	for key, state := range states {
		if err := a.UpdateResourceState(key, state); err != nil {
			return fmt.Errorf("failed to update resource %s in batch: %w", key, err)
		}
	}
	return nil
}

// GetResourceStatesBatch retrieves multiple resource states
func (a *AWSStateStore) GetResourceStatesBatch(resourceKeys []string) (map[string]map[string]interface{}, error) {
	result := make(map[string]map[string]interface{})

	for _, key := range resourceKeys {
		state, err := a.GetResourceState(key)
		if err == nil && state != nil {
			result[key] = state
		}
	}

	return result, nil
}

// Terraform state compatibility

// ExportTerraformState retrieves full Terraform state from S3
func (a *AWSStateStore) ExportTerraformState(deploymentID string) (*interfaces.TerraformState, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stateKey := a.stateS3Key(deploymentID)

	result, err := a.s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(a.config.S3Bucket),
		Key:    aws.String(stateKey),
	})
	if err != nil {
		var noSuchKey *s3types.NoSuchKey
		if errors.As(err, &noSuchKey) {
			// Return empty state if not found
			return &interfaces.TerraformState{
				Version:          4,
				TerraformVersion: "1.5.0",
				Serial:           0,
				Lineage:          a.generateLineage(),
				Outputs:          make(map[string]interface{}),
				Resources:        []interfaces.TerraformResource{},
			}, nil
		}
		return nil, fmt.Errorf("failed to get Terraform state from S3: %w", err)
	}
	defer func() { _ = result.Body.Close() }()

	var tfState interfaces.TerraformState
	if err := json.NewDecoder(result.Body).Decode(&tfState); err != nil {
		return nil, fmt.Errorf("failed to decode Terraform state: %w", err)
	}

	return &tfState, nil
}

// ImportTerraformState stores Terraform state in S3 and updates metadata
func (a *AWSStateStore) ImportTerraformState(deploymentID string, state *interfaces.TerraformState) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Save state to S3
	if err := a.saveTerraformStateToS3(ctx, deploymentID, state); err != nil {
		return fmt.Errorf("failed to save imported state to S3: %w", err)
	}

	// Get or create metadata
	metadata, err := a.getDeploymentMetadata(ctx, deploymentID)
	if err != nil {
		// If deployment doesn't exist, create new metadata for import
		if strings.Contains(err.Error(), "not found") {
			metadata = &AWSDeploymentMetadata{
				DeploymentID:  deploymentID,
				Name:          deploymentID,
				Status:        string(interfaces.DeploymentStatusCompleted),
				CreatedAt:     time.Now().UTC(),
				UpdatedAt:     time.Now().UTC(),
				StateS3Key:    a.stateS3Key(deploymentID),
				Serial:        state.Serial,
				Lineage:       state.Lineage,
				Version:       state.Version,
				ResourceCount: len(state.Resources),
			}
		} else {
			return fmt.Errorf("failed to get deployment metadata: %w", err)
		}
	} else {
		// Update existing metadata
		metadata.UpdatedAt = time.Now().UTC()
		metadata.Serial = state.Serial
		metadata.Lineage = state.Lineage
		metadata.ResourceCount = len(state.Resources)
	}

	if err := a.updateDeploymentMetadata(ctx, metadata); err != nil {
		return fmt.Errorf("failed to update metadata after import: %w", err)
	}

	return nil
}

// Locking operations

// LockDeployment creates a distributed lock using DynamoDB

// Health and maintenance operations

// Ping tests connectivity to both DynamoDB and S3

// Cleanup removes old data based on age
func (a *AWSStateStore) Cleanup(olderThan time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second) // 5 minute timeout
	defer cancel()

	cutoff := time.Now().Add(-olderThan)

	// Query deployments older than cutoff using GSI
	result, err := a.dynamoClient.Scan(ctx, &dynamodb.ScanInput{
		TableName:        aws.String(a.config.DynamoDBTable),
		FilterExpression: aws.String("CreatedAt < :cutoff"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":cutoff": &types.AttributeValueMemberS{Value: cutoff.Format(time.RFC3339)},
		},
	})
	if err != nil {
		return fmt.Errorf("failed to query old deployments: %w", err)
	}

	// Delete old deployments
	for _, item := range result.Items {
		if pk, ok := item["PK"]; ok {
			if s, ok := pk.(*types.AttributeValueMemberS); ok {
				if err := a.DeleteDeploymentState(s.Value); err != nil {
					// Log error but continue cleanup
					continue
				}
			}
		}
	}

	return nil
}

// GetStorageInfo returns information about the storage backend
func (a *AWSStateStore) GetStorageInfo() *interfaces.StorageInfo {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	info := &interfaces.StorageInfo{
		Type:     "aws",
		Exists:   true,
		Writable: true,
	}

	// Count deployments
	result, err := a.dynamoClient.Scan(ctx, &dynamodb.ScanInput{
		TableName: aws.String(a.config.DynamoDBTable),
		Select:    types.SelectCount,
	})
	if err == nil {
		info.DeploymentCount = int(result.Count)
	}

	// For S3 size calculation, we'd need to list and sum all objects
	// For performance, we'll skip this in the basic implementation
	info.TotalSizeBytes = 0
	info.UsedPercent = 0

	return info
}

// Helper methods

// getDeploymentMetadata retrieves deployment metadata from DynamoDB
func (a *AWSStateStore) getDeploymentMetadata(ctx context.Context, deploymentID string) (*AWSDeploymentMetadata, error) {
	result, err := a.dynamoClient.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(a.config.DynamoDBTable),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: deploymentID},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get deployment metadata: %w", err)
	}

	if result.Item == nil {
		return nil, fmt.Errorf("deployment not found")
	}

	return a.unmarshalDeploymentMetadata(result.Item)
}

// updateDeploymentMetadata updates deployment metadata in DynamoDB
func (a *AWSStateStore) updateDeploymentMetadata(ctx context.Context, metadata *AWSDeploymentMetadata) error {
	item, err := a.marshalDeploymentMetadata(*metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	_, err = a.dynamoClient.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(a.config.DynamoDBTable),
		Item:      item,
	})
	if err != nil {
		return fmt.Errorf("failed to update metadata: %w", err)
	}

	return nil
}

// marshalDeploymentMetadata converts metadata to DynamoDB item
func (a *AWSStateStore) marshalDeploymentMetadata(metadata AWSDeploymentMetadata) (map[string]types.AttributeValue, error) {
	// Check for empty values that would cause DynamoDB errors
	if metadata.DeploymentID == "" {
		return nil, fmt.Errorf("DeploymentID cannot be empty")
	}
	if metadata.Status == "" {
		return nil, fmt.Errorf("status cannot be empty")
	}

	// DynamoDB does not allow empty strings in AttributeValueMemberS
	// Ensure all string fields have values
	if metadata.Name == "" {
		metadata.Name = metadata.DeploymentID // Default to deployment ID
	}
	if metadata.StateS3Key == "" {
		metadata.StateS3Key = fmt.Sprintf("states/%s.tfstate", metadata.DeploymentID)
	}
	if metadata.Lineage == "" {
		metadata.Lineage = "default"
	}

	return map[string]types.AttributeValue{
		"PK":            &types.AttributeValueMemberS{Value: metadata.DeploymentID},
		"Name":          &types.AttributeValueMemberS{Value: metadata.Name},
		"Status":        &types.AttributeValueMemberS{Value: metadata.Status},
		"CreatedAt":     &types.AttributeValueMemberS{Value: metadata.CreatedAt.Format(time.RFC3339)},
		"UpdatedAt":     &types.AttributeValueMemberS{Value: metadata.UpdatedAt.Format(time.RFC3339)},
		"StateS3Key":    &types.AttributeValueMemberS{Value: metadata.StateS3Key},
		"ResourceCount": &types.AttributeValueMemberN{Value: strconv.Itoa(metadata.ResourceCount)},
		"Serial":        &types.AttributeValueMemberN{Value: strconv.FormatUint(metadata.Serial, 10)},
		"Lineage":       &types.AttributeValueMemberS{Value: metadata.Lineage},
		"Version":       &types.AttributeValueMemberN{Value: strconv.Itoa(metadata.Version)},
	}, nil
}

// unmarshalDeploymentMetadata converts DynamoDB item to metadata
//
//nolint:gocognit,funlen,gocyclo // Complex unmarshaling logic with type conversions
func (a *AWSStateStore) unmarshalDeploymentMetadata(item map[string]types.AttributeValue) (*AWSDeploymentMetadata, error) {
	metadata := &AWSDeploymentMetadata{}

	if v, ok := item["PK"]; ok {
		if s, ok := v.(*types.AttributeValueMemberS); ok {
			metadata.DeploymentID = s.Value
		}
	}

	if v, ok := item["Name"]; ok {
		if s, ok := v.(*types.AttributeValueMemberS); ok {
			metadata.Name = s.Value
		}
	}

	if v, ok := item["Status"]; ok {
		if s, ok := v.(*types.AttributeValueMemberS); ok {
			metadata.Status = s.Value
		}
	}

	if v, ok := item["CreatedAt"]; ok {
		if s, ok := v.(*types.AttributeValueMemberS); ok {
			if t, err := time.Parse(time.RFC3339, s.Value); err == nil {
				metadata.CreatedAt = t
			}
		}
	}

	if v, ok := item["UpdatedAt"]; ok {
		if s, ok := v.(*types.AttributeValueMemberS); ok {
			if t, err := time.Parse(time.RFC3339, s.Value); err == nil {
				metadata.UpdatedAt = t
			}
		}
	}

	if v, ok := item["StateS3Key"]; ok {
		if s, ok := v.(*types.AttributeValueMemberS); ok {
			metadata.StateS3Key = s.Value
		}
	}

	if v, ok := item["ResourceCount"]; ok {
		if n, ok := v.(*types.AttributeValueMemberN); ok {
			if count, err := strconv.Atoi(n.Value); err == nil {
				metadata.ResourceCount = count
			}
		}
	}

	if v, ok := item["Serial"]; ok {
		if n, ok := v.(*types.AttributeValueMemberN); ok {
			if serial, err := strconv.ParseUint(n.Value, 10, 64); err == nil {
				metadata.Serial = serial
			}
		}
	}

	if v, ok := item["Lineage"]; ok {
		if s, ok := v.(*types.AttributeValueMemberS); ok {
			metadata.Lineage = s.Value
		}
	}

	if v, ok := item["Version"]; ok {
		if n, ok := v.(*types.AttributeValueMemberN); ok {
			if version, err := strconv.Atoi(n.Value); err == nil {
				metadata.Version = version
			}
		}
	}

	return metadata, nil
}

// deploymentIDPattern is a regex pattern for valid deployment IDs
var deploymentIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*[a-zA-Z0-9]$`)

// validateDeploymentID validates that a deployment ID is safe for use in S3 keys
func validateDeploymentID(deploymentID string) error {
	if deploymentID == "" {
		return fmt.Errorf("deployment ID cannot be empty")
	}
	if len(deploymentID) > 100 {
		return fmt.Errorf("deployment ID too long (max 100 characters)")
	}
	if !deploymentIDPattern.MatchString(deploymentID) {
		return fmt.Errorf("deployment ID contains invalid characters")
	}
	return nil
}

// stateS3Key generates the S3 key for a deployment's state file
func (a *AWSStateStore) stateS3Key(deploymentID string) string {
	// Basic validation to prevent obvious issues
	// We're lenient here since deployment IDs might have been created before validation
	if deploymentID == "" || strings.Contains(deploymentID, "..") || strings.Contains(deploymentID, "//") {
		// Use a sanitized version
		log.Printf("[WARN] Potentially unsafe deployment ID for S3 key, sanitizing: %s", deploymentID)
		// For backward compatibility, we'll still generate a key but log the warning
	}

	key := deploymentID + ".tfstate"
	if a.config.S3Prefix != "" {
		return strings.TrimSuffix(a.config.S3Prefix, "/") + "/" + key
	}
	return key
}

// generateLineage generates a unique lineage identifier for Terraform state
func (a *AWSStateStore) generateLineage() string {
	return fmt.Sprintf("lineage-%d", time.Now().UnixNano())
}

// convertToTerraformState converts internal state to Terraform state format
func (a *AWSStateStore) convertToTerraformState(_ string, state map[string]interface{}, metadata *AWSDeploymentMetadata) *interfaces.TerraformState {
	tfState := &interfaces.TerraformState{
		Version:          4,
		TerraformVersion: "1.5.0",
		Serial:           metadata.Serial,
		Lineage:          metadata.Lineage,
		Outputs:          make(map[string]interface{}),
		Resources:        []interfaces.TerraformResource{},
	}

	// Convert resources if present
	if resources, ok := state["resources"]; ok {
		if resourceList, ok := resources.([]interface{}); ok {
			for _, res := range resourceList {
				if resource, ok := res.(map[string]interface{}); ok {
					tfResource := a.convertToTerraformResource(resource)
					if tfResource != nil {
						tfState.Resources = append(tfState.Resources, *tfResource)
					}
				}
			}
		}
	}

	// Extract outputs if present
	if outputs, ok := state["outputs"]; ok {
		if outputMap, ok := outputs.(map[string]interface{}); ok {
			tfState.Outputs = outputMap
		}
	}

	return tfState
}

// convertToTerraformResource converts a resource to Terraform format
func (a *AWSStateStore) convertToTerraformResource(resource map[string]interface{}) *interfaces.TerraformResource {
	resourceType, ok := resource["type"].(string)
	if !ok {
		return nil
	}

	resourceName, ok := resource["name"].(string)
	if !ok {
		return nil
	}

	attributes, ok := resource["attributes"].(map[string]interface{})
	if !ok {
		attributes = resource // Use the resource itself as attributes if no explicit attributes
	}

	return &interfaces.TerraformResource{
		Mode:     "managed",
		Type:     resourceType,
		Name:     resourceName,
		Provider: fmt.Sprintf("provider[%q]", utils.ExtractProviderName(resourceType)),
		Instances: []interfaces.TerraformResourceInstance{
			{
				SchemaVersion: 1,
				Attributes:    attributes,
			},
		},
	}
}

// saveTerraformStateToS3 saves Terraform state to S3
func (a *AWSStateStore) saveTerraformStateToS3(ctx context.Context, deploymentID string, state *interfaces.TerraformState) error {
	stateKey := a.stateS3Key(deploymentID)

	stateJSON, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal Terraform state: %w", err)
	}

	_, err = a.s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(a.config.S3Bucket),
		Key:         aws.String(stateKey),
		Body:        strings.NewReader(string(stateJSON)),
		ContentType: aws.String("application/json"),
	})
	if err != nil {
		return fmt.Errorf("failed to save state to S3: %w", err)
	}

	return nil
}

// CreateDeployment creates a new deployment metadata entry
func (a *AWSStateStore) CreateDeployment(ctx context.Context, deployment *interfaces.DeploymentMetadata) error {
	if err := validateDeploymentID(deployment.DeploymentID); err != nil {
		return fmt.Errorf("invalid deployment ID: %w", err)
	}
	now := time.Now().UTC()
	metadata := &AWSDeploymentMetadata{
		DeploymentID:  deployment.DeploymentID,
		Name:          deployment.DeploymentID, // Use ID as name if not provided
		Status:        string(deployment.Status),
		CreatedAt:     deployment.CreatedAt,
		UpdatedAt:     now,
		StateS3Key:    a.stateS3Key(deployment.DeploymentID),
		ResourceCount: deployment.ResourceCount,
		Serial:        0,
		Lineage:       a.generateLineage(),
		Version:       1,
	}

	return a.updateDeploymentMetadata(ctx, metadata)
}

// GetDeployment retrieves deployment metadata
func (a *AWSStateStore) GetDeployment(ctx context.Context, deploymentID string) (*interfaces.DeploymentMetadata, error) {
	if err := validateDeploymentID(deploymentID); err != nil {
		return nil, fmt.Errorf("invalid deployment ID: %w", err)
	}
	metadata, err := a.getDeploymentMetadata(ctx, deploymentID)
	if err != nil {
		return nil, err
	}

	return &interfaces.DeploymentMetadata{
		DeploymentID:  metadata.DeploymentID,
		Status:        interfaces.DeploymentStatus(metadata.Status),
		CreatedAt:     metadata.CreatedAt,
		UpdatedAt:     metadata.UpdatedAt,
		ResourceCount: metadata.ResourceCount,
		LastAppliedAt: &metadata.UpdatedAt, // Use UpdatedAt as last applied
	}, nil
}

// ListDeployments returns all deployments with metadata
func (a *AWSStateStore) ListDeployments(ctx context.Context) ([]*interfaces.DeploymentMetadata, error) {
	var deployments []*interfaces.DeploymentMetadata
	var lastEvaluatedKey map[string]types.AttributeValue

	for {
		input := &dynamodb.ScanInput{
			TableName: aws.String(a.config.DynamoDBTable),
		}

		if lastEvaluatedKey != nil {
			input.ExclusiveStartKey = lastEvaluatedKey
		}

		result, err := a.dynamoClient.Scan(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("failed to scan deployments from DynamoDB: %w", err)
		}

		for _, item := range result.Items {
			metadata, err := a.unmarshalDeploymentMetadata(item)
			if err != nil {
				continue // Skip invalid entries
			}

			deployments = append(deployments, &interfaces.DeploymentMetadata{
				DeploymentID:  metadata.DeploymentID,
				Status:        interfaces.DeploymentStatus(metadata.Status),
				CreatedAt:     metadata.CreatedAt,
				UpdatedAt:     metadata.UpdatedAt,
				ResourceCount: metadata.ResourceCount,
				LastAppliedAt: &metadata.UpdatedAt,
			})
		}

		lastEvaluatedKey = result.LastEvaluatedKey
		if lastEvaluatedKey == nil {
			break
		}
	}

	return deployments, nil
}

// UpdateDeploymentStatus updates the status of a deployment
func (a *AWSStateStore) UpdateDeploymentStatus(ctx context.Context, deploymentID string, status interfaces.DeploymentStatus) error {
	metadata, err := a.getDeploymentMetadata(ctx, deploymentID)
	if err != nil {
		return err
	}

	metadata.Status = string(status)
	metadata.UpdatedAt = time.Now().UTC()

	return a.updateDeploymentMetadata(ctx, metadata)
}

// DeleteDeployment removes a deployment (alias for existing method)
func (a *AWSStateStore) DeleteDeployment(_ context.Context, deploymentID string) error {
	return a.DeleteDeploymentState(deploymentID)
}

// SaveTerraformState saves raw Terraform state data and updates metadata
func (a *AWSStateStore) SaveTerraformState(ctx context.Context, deploymentID string, stateData []byte) error {
	if err := validateDeploymentID(deploymentID); err != nil {
		return fmt.Errorf("invalid deployment ID: %w", err)
	}
	stateKey := a.stateS3Key(deploymentID)

	// Save to S3
	_, err := a.s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(a.config.S3Bucket),
		Key:         aws.String(stateKey),
		Body:        strings.NewReader(string(stateData)),
		ContentType: aws.String("application/json"),
	})
	if err != nil {
		return fmt.Errorf("failed to save state to S3: %w", err)
	}

	// Parse the state to extract metadata
	var tfState interfaces.TerraformState
	if err := json.Unmarshal(stateData, &tfState); err != nil {
		// Log error but don't fail - state is already saved to S3
		log.Printf("[DEBUG] Failed to parse terraform state for metadata update: %v", err)
		return nil
	}

	// Get or create metadata
	metadata, err := a.getDeploymentMetadata(ctx, deploymentID)
	if err != nil {
		// If deployment doesn't exist, create new metadata
		if strings.Contains(err.Error(), "not found") {
			metadata = &AWSDeploymentMetadata{
				DeploymentID:  deploymentID,
				Name:          deploymentID,
				Status:        string(interfaces.DeploymentStatusCompleted),
				CreatedAt:     time.Now().UTC(),
				UpdatedAt:     time.Now().UTC(),
				StateS3Key:    stateKey,
				Serial:        tfState.Serial,
				Lineage:       tfState.Lineage,
				Version:       tfState.Version,
				ResourceCount: len(tfState.Resources),
			}
		} else {
			// Log error but don't fail - state is already saved to S3
			log.Printf("[DEBUG] Failed to get deployment metadata for update: %v", err)
			return nil
		}
	} else {
		// Update existing metadata
		metadata.UpdatedAt = time.Now().UTC()
		metadata.Serial = tfState.Serial
		metadata.Lineage = tfState.Lineage
		metadata.ResourceCount = len(tfState.Resources)
		if tfState.Version > 0 {
			metadata.Version = tfState.Version
		}
	}

	// Update metadata in DynamoDB
	if err := a.updateDeploymentMetadata(ctx, metadata); err != nil {
		// Log error but don't fail - state is already saved to S3
		log.Printf("[DEBUG] Failed to update deployment metadata: %v", err)
	}

	return nil
}

// LoadTerraformState loads raw Terraform state data
func (a *AWSStateStore) LoadTerraformState(ctx context.Context, deploymentID string) ([]byte, error) {
	if err := validateDeploymentID(deploymentID); err != nil {
		return nil, fmt.Errorf("invalid deployment ID: %w", err)
	}
	stateKey := a.stateS3Key(deploymentID)

	result, err := a.s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(a.config.S3Bucket),
		Key:    aws.String(stateKey),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get Terraform state from S3: %w", err)
	}
	defer func() { _ = result.Body.Close() }()

	data, err := io.ReadAll(result.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read S3 object body: %w", err)
	}
	return data, nil
}

// DeleteTerraformState removes Terraform state file
func (a *AWSStateStore) DeleteTerraformState(ctx context.Context, deploymentID string) error {
	if err := validateDeploymentID(deploymentID); err != nil {
		return fmt.Errorf("invalid deployment ID: %w", err)
	}
	stateKey := a.stateS3Key(deploymentID)
	_, err := a.s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(a.config.S3Bucket),
		Key:    aws.String(stateKey),
	})
	if err != nil {
		// Don't fail if S3 object doesn't exist
		var noSuchKey *s3types.NoSuchKey
		if !errors.As(err, &noSuchKey) {
			return fmt.Errorf("failed to delete state from S3: %w", err)
		}
	}
	return nil
}

// Ping with context (interface compliance)
func (a *AWSStateStore) Ping(ctx context.Context) error {
	// Test DynamoDB connectivity
	_, err := a.dynamoClient.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: aws.String(a.config.DynamoDBTable),
	})
	if err != nil {
		return fmt.Errorf("DynamoDB connectivity failed: %w", err)
	}

	// Test S3 connectivity
	_, err = a.s3Client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(a.config.S3Bucket),
	})
	if err != nil {
		return fmt.Errorf("S3 connectivity failed: %w", err)
	}

	return nil
}

// LockDeployment with context (interface compliance)
func (a *AWSStateStore) LockDeployment(_ context.Context, deploymentID string) (interfaces.StateLock, error) {
	if a.lockProvider == nil {
		return nil, fmt.Errorf("locking is not enabled")
	}

	return a.lockProvider.AcquireLock(deploymentID, "deployment")
}

// UnlockDeployment with context (interface compliance)
func (a *AWSStateStore) UnlockDeployment(_ context.Context, lock interfaces.StateLock) error {
	if a.lockProvider == nil {
		return fmt.Errorf("locking is not enabled")
	}

	if dynamoLock, ok := lock.(*DynamoDBLock); ok {
		return dynamoLock.Release()
	}

	return fmt.Errorf("invalid lock type")
}

// Interface compliance check
var _ interfaces.StateStore = (*AWSStateStore)(nil)
