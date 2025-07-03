package integration

import (
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/lattiam/lattiam/tests/helpers"
)

type ProviderLifecycleTestSuite struct {
	helpers.BaseTestSuite
}

// TestProviderLifecycleTestSuite runs the provider lifecycle test suite
//
//nolint:paralleltest // Integration tests use shared LocalStack and API server resources
func TestProviderLifecycleTestSuite(t *testing.T) {
	suite.Run(t, new(ProviderLifecycleTestSuite))
}

func (s *ProviderLifecycleTestSuite) SetupTest() {
	s.BaseTestSuite.SetupTest()
}

func (s *ProviderLifecycleTestSuite) TearDownTest() {
	s.BaseTestSuite.TearDownTest()
	// Final check to ensure no processes are lingering after the test.
	helpers.AssertNoTerraformProviderProcesses(s.T())
}

// TestProviderProcessCleanupAfterDeployment creates and immediately deletes a deployment
// to ensure the provider process is cleaned up correctly.
func (s *ProviderLifecycleTestSuite) TestProviderProcessCleanupAfterDeployment() {
	// Define a simple deployment that requires the AWS provider.
	payload := map[string]interface{}{
		"name": "Provider Lifecycle Test",
		"terraform_json": map[string]interface{}{
			"terraform": map[string]interface{}{
				"required_providers": map[string]interface{}{
					"aws": map[string]interface{}{
						"source":  "hashicorp/aws",
						"version": "5.31.0", // Use a known stable version
					},
				},
			},
			"provider": map[string]interface{}{
				"aws": map[string]interface{}{
					"region": "us-east-1",
				},
			},
			"resource": map[string]interface{}{
				"aws_iam_role": map[string]interface{}{
					"test": map[string]interface{}{
						"name": "test-provider-lifecycle-role-" + helpers.UniqueName(""),
						"assume_role_policy": `{
							"Version": "2012-10-17",
							"Statement": [
								{
									"Effect": "Allow",
									"Principal": {
										"Service": "ec2.amazonaws.com"
									},
									"Action": "sts:AssumeRole"
								}
							]
						}`,
						"tags": map[string]interface{}{
							"Environment": "test",
							"Purpose":     "provider-lifecycle-test",
						},
					},
				},
			},
		},
	}

	// Create the deployment.
	result := s.APIClient.CreateDeployment(payload)
	deploymentID := result["id"].(string)
	s.TrackDeployment(deploymentID) // Ensure it gets cleaned up even if the test fails early.

	// Wait for the deployment to complete to ensure the provider has been started.
	s.APIClient.WaitForDeploymentStatus(deploymentID, "completed", 60*time.Second)

	// Immediately delete the deployment.
	s.APIClient.DeleteDeployment(deploymentID)

	// The TearDownTest method will now run and assert that no provider processes are left.
}
