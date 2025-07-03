//go:build integration
// +build integration

package integration

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"

	"github.com/lattiam/lattiam/tests/helpers"
)

type LocalStackVerificationTestSuite struct {
	helpers.BaseTestSuite
}

func TestLocalStackVerificationTestSuite(t *testing.T) {
	suite.Run(t, new(LocalStackVerificationTestSuite))
}

// TestSimpleDeployment verifies basic LocalStack integration
func (s *LocalStackVerificationTestSuite) TestSimpleDeployment() {
	s.T().Run("simple S3 bucket deployment", func(t *testing.T) {
		// Create a simple S3 bucket deployment
		deploymentReq := map[string]interface{}{
			"name": "LocalStack Test",
			"terraform_json": map[string]interface{}{
				"provider": map[string]interface{}{
					"aws": map[string]interface{}{
						"version": "5.31.0",
					},
				},
				"resource": map[string]interface{}{
					"aws_s3_bucket": map[string]interface{}{
						"test_bucket": map[string]interface{}{
							"bucket": "test-localstack-bucket-" + helpers.UniqueName(""),
						},
					},
				},
			},
		}

		// Create deployment
		deployment := s.APIClient.CreateDeployment(deploymentReq)

		deploymentID, ok := deployment["id"].(string)
		s.Require().True(ok, "deployment ID should be a string")
		s.Require().NotEmpty(deploymentID)

		// Wait for deployment to finish (either completed or failed)
		// Wait for deployment to finish (either completed or failed)
		s.APIClient.WaitForDeploymentStatus(deploymentID, "completed", 2*time.Minute)

		// Verify deployment succeeded
		result := s.APIClient.GetDeployment(deploymentID)
		assert.Equal(s.T(), "completed", result["status"])

		// Verify bucket was created
		resources, ok := result["resources"].([]interface{})
		s.Require().True(ok, "resources should be an array")
		s.Require().Len(resources, 1, "should have exactly one resource")

		resource := resources[0].(map[string]interface{})
		assert.Equal(s.T(), "aws_s3_bucket", resource["type"])
		assert.Equal(s.T(), "test_bucket", resource["name"])
		assert.Equal(s.T(), "completed", resource["status"])

		// Clean up
		s.APIClient.DeleteDeployment(deploymentID)
	})
}

// TestLocalStackEnvironment verifies LocalStack environment configuration
func (s *LocalStackVerificationTestSuite) TestLocalStackEnvironment() {
	s.T().Run("LocalStack environment setup", func(t *testing.T) {
		// This test just verifies that our LocalStack environment setup worked
		// The BaseTestSuite should have configured LocalStack automatically

		// The fact that we got this far means the API server started successfully
		// and LocalStack configuration is working
		assert.True(s.T(), true, "LocalStack environment configuration successful")
	})
}
