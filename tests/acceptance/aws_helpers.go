//go:build demo
// +build demo

package acceptance

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// awsHelpers provides AWS resource verification methods
type awsHelpers struct {
	s3Client  *s3.Client
	iamClient *iam.Client
}

// newAWSHelpers creates AWS clients configured for LocalStack
func newAWSHelpers() (*awsHelpers, error) {
	// Configure for LocalStack
	customResolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
		localstackEndpoint := os.Getenv("AWS_ENDPOINT_URL")
		if localstackEndpoint == "" {
			localstackEndpoint = "http://localstack:4566"
		}
		return aws.Endpoint{
			URL:               localstackEndpoint,
			HostnameImmutable: true,
			Source:            aws.EndpointSourceCustom,
		}, nil
	})

	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithRegion("us-east-1"),
		config.WithEndpointResolverWithOptions(customResolver),
		config.WithCredentialsProvider(aws.AnonymousCredentials{}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return &awsHelpers{
		s3Client:  s3.NewFromConfig(cfg),
		iamClient: iam.NewFromConfig(cfg),
	}, nil
}

// verifyS3BucketExists checks if an S3 bucket exists
func (h *awsHelpers) verifyS3BucketExists(bucketName string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := h.s3Client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(bucketName),
	})
	return err == nil
}

// verifyS3BucketTags checks if an S3 bucket has the expected tags
func (h *awsHelpers) verifyS3BucketTags(bucketName string, expectedTags map[string]string) (bool, map[string]string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := h.s3Client.GetBucketTagging(ctx, &s3.GetBucketTaggingInput{
		Bucket: aws.String(bucketName),
	})
	if err != nil {
		return false, nil
	}

	actualTags := make(map[string]string)
	for _, tag := range result.TagSet {
		if tag.Key != nil && tag.Value != nil {
			actualTags[*tag.Key] = *tag.Value
		}
	}

	// Check if all expected tags are present
	for key, expectedValue := range expectedTags {
		if actualValue, ok := actualTags[key]; !ok || actualValue != expectedValue {
			return false, actualTags
		}
	}

	return true, actualTags
}

// waitForS3BucketDeleted waits for a bucket to be deleted
func (h *awsHelpers) waitForS3BucketDeleted(bucketName string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		if !h.verifyS3BucketExists(bucketName) {
			return true
		}
		time.Sleep(1 * time.Second)
	}

	return false
}

// verifyIAMRoleExists checks if an IAM role exists
func (h *awsHelpers) verifyIAMRoleExists(roleName string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := h.iamClient.GetRole(ctx, &iam.GetRoleInput{
		RoleName: aws.String(roleName),
	})
	return err == nil
}

// listS3BucketsWithPrefix lists all S3 buckets with a given prefix
func (h *awsHelpers) listS3BucketsWithPrefix(prefix string) ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	result, err := h.s3Client.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		return nil, err
	}

	var buckets []string
	for _, bucket := range result.Buckets {
		if bucket.Name != nil && strings.HasPrefix(*bucket.Name, prefix) {
			buckets = append(buckets, *bucket.Name)
		}
	}

	return buckets, nil
}

// cleanupS3BucketsWithPrefix deletes all S3 buckets with a given prefix
func (h *awsHelpers) cleanupS3BucketsWithPrefix(prefix string) error {
	buckets, err := h.listS3BucketsWithPrefix(prefix)
	if err != nil {
		return err
	}

	for _, bucket := range buckets {
		// First, delete all objects in the bucket
		ctx := context.Background()

		// List all objects
		listResp, err := h.s3Client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket: aws.String(bucket),
		})
		if err != nil {
			continue // Skip this bucket
		}

		// Delete each object
		for _, obj := range listResp.Contents {
			_, _ = h.s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
				Bucket: aws.String(bucket),
				Key:    obj.Key,
			})
		}

		// Delete the bucket
		_, _ = h.s3Client.DeleteBucket(ctx, &s3.DeleteBucketInput{
			Bucket: aws.String(bucket),
		})
	}

	return nil
}
