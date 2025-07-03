package helpers

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// DeleteS3Bucket deletes an S3 bucket using AWS SDK, ignoring errors if it doesn't exist
func DeleteS3Bucket(t *testing.T, bucketName string) {
	t.Helper()

	tc := NewTestConfig()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cfg, err := tc.GetAWSConfig(ctx)
	if err != nil {
		t.Logf("Warning: Failed to get AWS config: %v", err)
		return
	}

	s3Client := s3.NewFromConfig(cfg)

	// First, try to empty the bucket
	listResp, err := s3Client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucketName),
	})
	if err != nil {
		var noBucket *s3types.NoSuchBucket
		if errors.As(err, &noBucket) {
			t.Logf("S3 bucket %s does not exist, nothing to delete", bucketName)
			return
		}
		t.Logf("Warning: Failed to list objects in S3 bucket %s: %v", bucketName, err)
		return
	}

	// Delete all objects if any exist
	if len(listResp.Contents) > 0 {
		var objectsToDelete []s3types.ObjectIdentifier
		for _, obj := range listResp.Contents {
			objectsToDelete = append(objectsToDelete, s3types.ObjectIdentifier{
				Key: obj.Key,
			})
		}

		_, err = s3Client.DeleteObjects(ctx, &s3.DeleteObjectsInput{
			Bucket: aws.String(bucketName),
			Delete: &s3types.Delete{
				Objects: objectsToDelete,
			},
		})
		if err != nil {
			t.Logf("Warning: Failed to delete objects from S3 bucket %s: %v", bucketName, err)
		}
	}

	// Now delete the bucket
	_, err = s3Client.DeleteBucket(ctx, &s3.DeleteBucketInput{
		Bucket: aws.String(bucketName),
	})
	if err != nil {
		var noBucket *s3types.NoSuchBucket
		if errors.As(err, &noBucket) {
			t.Logf("S3 bucket %s does not exist, nothing to delete", bucketName)
			return
		}
		t.Logf("Warning: Failed to delete S3 bucket %s: %v", bucketName, err)
		return
	}

	t.Logf("Successfully deleted S3 bucket: %s", bucketName)
}

// GetEC2InstanceIDFromName gets the ID of an EC2 instance from its "Name" tag using AWS SDK
func GetEC2InstanceIDFromName(t *testing.T, instanceName string) string {
	t.Helper()

	tc := NewTestConfig()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cfg, err := tc.GetAWSConfig(ctx)
	if err != nil {
		t.Logf("Warning: Failed to get AWS config: %v", err)
		return ""
	}

	ec2Client := ec2.NewFromConfig(cfg)

	input := &ec2.DescribeInstancesInput{
		Filters: []ec2types.Filter{
			{
				Name:   aws.String("tag:Name"),
				Values: []string{instanceName},
			},
		},
	}

	result, err := ec2Client.DescribeInstances(ctx, input)
	if err != nil {
		t.Logf("Warning: Failed to describe EC2 instances: %v", err)
		return ""
	}

	for _, reservation := range result.Reservations {
		for _, instance := range reservation.Instances {
			if instance.InstanceId != nil {
				return *instance.InstanceId
			}
		}
	}

	return ""
}

// DeleteSecurityGroup deletes a security group using AWS SDK, ignoring errors if it doesn't exist
func DeleteSecurityGroup(t *testing.T, groupName string) {
	t.Helper()

	tc := NewTestConfig()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cfg, err := tc.GetAWSConfig(ctx)
	if err != nil {
		t.Logf("Warning: Failed to get AWS config: %v", err)
		return
	}

	ec2Client := ec2.NewFromConfig(cfg)

	_, err = ec2Client.DeleteSecurityGroup(ctx, &ec2.DeleteSecurityGroupInput{
		GroupName: aws.String(groupName),
	})
	if err != nil {
		// Check for common "doesn't exist" errors
		if strings.Contains(err.Error(), "does not exist") ||
			strings.Contains(err.Error(), "InvalidGroup.NotFound") {
			t.Logf("Security group %s does not exist, nothing to delete", groupName)
			return
		}
		t.Logf("Warning: Failed to delete security group %s: %v", groupName, err)
		return
	}

	t.Logf("Successfully deleted security group: %s", groupName)
}
