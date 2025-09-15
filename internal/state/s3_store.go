package state

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/lattiam/lattiam/internal/interfaces"
)

// S3StateFileStore implements TerraformStateFileStore using AWS S3 for Terraform state file storage only.
// This store is focused solely on storing, retrieving, and managing Terraform state files.
// Deployment metadata operations are handled by separate stores (e.g., DynamoDB).
type S3StateFileStore struct {
	client *s3.Client
	bucket string
	region string
	prefix string // Optional prefix for organizing state files
}

// S3StateFileStoreConfig holds the configuration for S3StateFileStore
type S3StateFileStoreConfig struct {
	Bucket          string              `json:"bucket"`
	Region          string              `json:"region"`
	Prefix          string              `json:"prefix,omitempty"`           // Optional prefix for state files
	Endpoint        string              `json:"endpoint,omitempty"`         // For LocalStack or custom endpoints
	DynamoDBLocking *DynamoDBLockConfig `json:"dynamodb_locking,omitempty"` // Legacy field for backward compatibility
	// AWS credentials should be provided via IAM roles, instance profiles, or environment variables
}

// NewS3StateFileStore creates a new S3-based Terraform state file store
func NewS3StateFileStore(cfg S3StateFileStoreConfig) (*S3StateFileStore, error) {
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("bucket name is required")
	}
	if cfg.Region == "" {
		return nil, fmt.Errorf("region is required")
	}

	// Load AWS config
	awsCfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithRegion(cfg.Region),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create S3 client with custom endpoint if provided (for LocalStack)
	var s3Client *s3.Client
	if cfg.Endpoint != "" {
		s3Client = s3.NewFromConfig(awsCfg, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
			o.UsePathStyle = true // Required for LocalStack
		})
	} else {
		s3Client = s3.NewFromConfig(awsCfg)
	}

	store := &S3StateFileStore{
		client: s3Client,
		bucket: cfg.Bucket,
		region: cfg.Region,
		prefix: cfg.Prefix,
	}

	// NOTE: DynamoDBLocking field is ignored in this focused implementation
	// Locking operations have been moved to DynamoDB-based deployment metadata stores

	// Initialize bucket if needed
	if err := store.initializeBucket(context.Background()); err != nil {
		return nil, fmt.Errorf("failed to initialize S3 bucket: %w", err)
	}

	return store, nil
}

// initializeBucket sets up the S3 bucket
func (s *S3StateFileStore) initializeBucket(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Check if bucket exists and is accessible
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(s.bucket),
	})
	if err != nil {
		// Try to create the bucket if it doesn't exist
		var noBucket *types.NoSuchBucket
		if errors.As(err, &noBucket) || strings.Contains(err.Error(), "NotFound") {
			_, err = s.client.CreateBucket(ctx, &s3.CreateBucketInput{
				Bucket: aws.String(s.bucket),
				CreateBucketConfiguration: &types.CreateBucketConfiguration{
					LocationConstraint: types.BucketLocationConstraint(s.region),
				},
			})
			if err != nil {
				return fmt.Errorf("failed to create S3 bucket %s: %w", s.bucket, err)
			}
		} else {
			return fmt.Errorf("failed to access S3 bucket %s: %w", s.bucket, err)
		}
	}

	return nil
}

// objectKey constructs the full S3 object key with prefix for state files
func (s *S3StateFileStore) objectKey(key string) string {
	if s.prefix == "" {
		return "terraform-state/" + key
	}
	return strings.TrimSuffix(s.prefix, "/") + "/terraform-state/" + key
}

// SaveTerraformState saves a Terraform state file for a deployment
func (s *S3StateFileStore) SaveTerraformState(ctx context.Context, deploymentID string, stateData []byte) error {
	if deploymentID == "" {
		return fmt.Errorf("deployment ID cannot be empty")
	}
	if len(stateData) == 0 {
		return fmt.Errorf("state data cannot be empty")
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Save current state with versioning
	key := s.objectKey(deploymentID + ".tfstate")

	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        strings.NewReader(string(stateData)),
		ContentType: aws.String("application/json"),
		Metadata: map[string]string{
			"deployment-id": deploymentID,
			"saved-at":      time.Now().UTC().Format(time.RFC3339),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to save Terraform state to S3: %w", err)
	}

	return nil
}

// LoadTerraformState retrieves a Terraform state file for a deployment
func (s *S3StateFileStore) LoadTerraformState(ctx context.Context, deploymentID string) ([]byte, error) {
	if deploymentID == "" {
		return nil, fmt.Errorf("deployment ID cannot be empty")
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	key := s.objectKey(deploymentID + ".tfstate")

	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		var noSuchKey *types.NoSuchKey
		if errors.As(err, &noSuchKey) {
			return nil, fmt.Errorf("terraform state not found")
		}
		return nil, fmt.Errorf("failed to get Terraform state from S3: %w", err)
	}
	defer func() { _ = result.Body.Close() }()

	// Read with size limit for safety (10MB limit for state files)
	// This prevents memory exhaustion from concurrent requests
	limitReader := &io.LimitedReader{R: result.Body, N: 10 * 1024 * 1024}
	data, err := io.ReadAll(limitReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read Terraform state: %w", err)
	}

	return data, nil
}

// DeleteTerraformState removes a Terraform state file for a deployment
func (s *S3StateFileStore) DeleteTerraformState(ctx context.Context, deploymentID string) error {
	if deploymentID == "" {
		return fmt.Errorf("deployment ID cannot be empty")
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	key := s.objectKey(deploymentID + ".tfstate")

	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("failed to delete Terraform state from S3: %w", err)
	}

	// Also delete any versioned state files
	_ = s.deleteStateVersions(ctx, deploymentID)

	return nil
}

// GetStateVersions retrieves available versions of a state file
func (s *S3StateFileStore) GetStateVersions(ctx context.Context, deploymentID string) ([]int, error) {
	if deploymentID == "" {
		return nil, fmt.Errorf("deployment ID cannot be empty")
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	prefix := s.objectKey(deploymentID + ".tfstate.v")

	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(prefix),
	})

	var versions []int

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list state versions: %w", err)
		}

		for _, obj := range page.Contents {
			key := *obj.Key
			// Extract version number from key like "deployment.tfstate.v1"
			suffix := strings.TrimPrefix(key, prefix)
			if version, err := strconv.Atoi(suffix); err == nil {
				versions = append(versions, version)
			}
		}
	}

	return versions, nil
}

// GetStateVersion retrieves a specific version of a state file
func (s *S3StateFileStore) GetStateVersion(ctx context.Context, deploymentID string, version int) ([]byte, error) {
	if deploymentID == "" {
		return nil, fmt.Errorf("deployment ID cannot be empty")
	}
	if version < 1 {
		return nil, fmt.Errorf("version must be >= 1")
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	key := s.objectKey(fmt.Sprintf("%s.tfstate.v%d", deploymentID, version))

	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		var noSuchKey *types.NoSuchKey
		if errors.As(err, &noSuchKey) {
			return nil, fmt.Errorf("terraform state version not found")
		}
		return nil, fmt.Errorf("failed to get Terraform state version from S3: %w", err)
	}
	defer func() { _ = result.Body.Close() }()

	// Read with size limit for safety (10MB limit)
	// This prevents memory exhaustion from concurrent requests
	limitReader := &io.LimitedReader{R: result.Body, N: 10 * 1024 * 1024}
	data, err := io.ReadAll(limitReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read Terraform state version: %w", err)
	}

	return data, nil
}

// deleteStateVersions removes all versioned state files for a deployment
func (s *S3StateFileStore) deleteStateVersions(ctx context.Context, deploymentID string) error {
	versions, err := s.GetStateVersions(ctx, deploymentID)
	if err != nil {
		return err // Don't fail the main delete if we can't list versions
	}

	for _, version := range versions {
		key := s.objectKey(fmt.Sprintf("%s.tfstate.v%d", deploymentID, version))
		_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(s.bucket),
			Key:    aws.String(key),
		})
		if err != nil {
			// Log but don't fail - this is cleanup
			continue
		}
	}

	return nil
}

// Ping tests connectivity to S3 (implements TerraformStateFileStore interface)
func (s *S3StateFileStore) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// Test connectivity by performing a head bucket operation
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(s.bucket),
	})
	if err != nil {
		return fmt.Errorf("S3 state file store not accessible: %w", err)
	}

	return nil
}

// PingWithContext is an alias for Ping (kept for backward compatibility)
func (s *S3StateFileStore) PingWithContext(ctx context.Context) error {
	return s.Ping(ctx)
}

// GetS3Client returns the underlying S3 client for advanced operations
func (s *S3StateFileStore) GetS3Client() *s3.Client {
	return s.client
}

// GetBucket returns the bucket name
func (s *S3StateFileStore) GetBucket() string {
	return s.bucket
}

// GetPrefix returns the prefix used for state files
func (s *S3StateFileStore) GetPrefix() string {
	return s.prefix
}

// Ensure S3StateFileStore implements TerraformStateFileStore interface
var _ interfaces.TerraformStateFileStore = (*S3StateFileStore)(nil)
