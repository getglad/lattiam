package helpers

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/tests/testconfig"
)

// TestUniqueName verifies unique name generation
func TestUniqueName(t *testing.T) {
	t.Parallel()

	// Generate multiple names to ensure uniqueness without relying on time
	names := make(map[string]bool)
	const numNames = 100

	for i := 0; i < numNames; i++ {
		name := UniqueName("test")
		if names[name] {
			t.Errorf("Duplicate name generated: %s", name)
		}
		names[name] = true
		assert.Contains(t, name, "test-", "Name should contain prefix")
	}

	// Verify we got the expected number of unique names
	assert.Len(t, names, numNames, "Should generate unique names")
}

// TestNewDeploymentRequest verifies deployment request creation
func TestNewDeploymentRequest(t *testing.T) {
	t.Parallel()
	req := NewDeploymentRequest("test-deployment",
		NewS3BucketResource("bucket1"),
		NewIAMRoleResource("role1"),
	)

	assert.Equal(t, "test-deployment", req.Name)
	assert.Equal(t, "aws", req.Provider)
	assert.Equal(t, "5.31.0", req.ProviderVersion)

	// Check resources
	resources := req.TerraformJSON["resource"].(map[string]interface{})
	assert.Contains(t, resources, "aws_s3_bucket")
	assert.Contains(t, resources, "aws_iam_role")
}

// Constants for expected prefixes
const (
	iamRoleNamePrefix  = "test-role-test-role-"
	s3BucketNamePrefix = "test-bucket-test-bucket-"
)

// TestTestDataBuilders verifies resource builders
func TestTestDataBuilders(t *testing.T) {
	t.Parallel()
	t.Run("IAMRoleBuilder", func(t *testing.T) {
		t.Parallel()
		role := NewIAMRoleResource("test-role")
		iamRole := role["aws_iam_role"].(map[string]interface{})
		roleConfig := iamRole["test-role"].(map[string]interface{})

		roleName, ok := roleConfig["name"].(string)
		require.True(t, ok, "Role name should be a string")
		assert.True(t, strings.HasPrefix(roleName, iamRoleNamePrefix),
			"IAM role name should start with '%s', got '%s'", iamRoleNamePrefix, roleName)
		assert.Contains(t, roleConfig["assume_role_policy"], "ec2.amazonaws.com")
	})

	t.Run("S3BucketBuilder", func(t *testing.T) {
		t.Parallel()
		bucket := NewS3BucketResource("test-bucket")
		s3Bucket := bucket["aws_s3_bucket"].(map[string]interface{})
		bucketConfig := s3Bucket["test-bucket"].(map[string]interface{})

		bucketName, ok := bucketConfig["bucket"].(string)
		require.True(t, ok, "Bucket name should be a string")
		assert.True(t, strings.HasPrefix(bucketName, s3BucketNamePrefix),
			"S3 bucket name should start with '%s', got '%s'", s3BucketNamePrefix, bucketName)
	})

	t.Run("VPCWithSubnet", func(t *testing.T) {
		t.Parallel()
		vpc := NewVPCResource("vpc")
		subnet := NewSubnetResource("subnet", "vpc")

		// Check VPC
		vpcResource := vpc["aws_vpc"].(map[string]interface{})
		vpcConfig := vpcResource["vpc"].(map[string]interface{})
		assert.Equal(t, "10.0.0.0/16", vpcConfig["cidr_block"])

		// Check subnet references VPC
		subnetResource := subnet["aws_subnet"].(map[string]interface{})
		subnetConfig := subnetResource["subnet"].(map[string]interface{})
		assert.Equal(t, "${aws_vpc.vpc.id}", subnetConfig["vpc_id"])
	})
}

// TestMergeResources verifies resource merging
func TestMergeResources(t *testing.T) {
	t.Parallel()
	res1 := NewS3BucketResource("bucket1")
	res2 := NewIAMRoleResource("role1")
	res3 := NewSecurityGroupResource("sg1")

	merged := MergeResources(res1, res2, res3)

	assert.Contains(t, merged, "aws_s3_bucket")
	assert.Contains(t, merged, "aws_iam_role")
	assert.Contains(t, merged, "aws_security_group")
}

// TestAPIHelperConstants verifies API constants
func TestAPIHelperConstants(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 2*time.Minute, DefaultAPITimeout)
	assert.Equal(t, 5*time.Second, DefaultPollInterval)
	assert.Equal(t, "http://localhost:8084", DefaultAPIBaseURL)
	assert.Equal(t, "/api/v1/system/health", DefaultHealthEndpoint)
	assert.Equal(t, testconfig.DefaultLocalStackURL, LocalStackEndpoint)
}

// TestGenerateTestResourceName verifies resource name generation
func TestGenerateTestResourceName(t *testing.T) {
	t.Parallel()
	// GenerateTestResourceName is in test_config.go
	name1 := GenerateTestResourceName("test")
	name2 := GenerateTestResourceName("test")

	require.NotEqual(t, name1, name2, "Names should be unique")
	assert.Contains(t, name1, "test-")
	assert.Contains(t, name2, "test-")
}
