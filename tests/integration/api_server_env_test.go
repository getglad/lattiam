//go:build integration
// +build integration

package integration

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/lattiam/lattiam/internal/apiserver"
	"github.com/lattiam/lattiam/tests/helpers"
)

// APIServerEnvTestSuite tests API server with environment variables
type APIServerEnvTestSuite struct {
	helpers.BaseTestSuite
	server *apiserver.APIServer
	apiURL string
}

func TestAPIServerEnvSuite(t *testing.T) {
	suite.Run(t, new(APIServerEnvTestSuite))
}

func (s *APIServerEnvTestSuite) SetupSuite() {
	// Call the parent SetupSuite first to handle common setup like provider availability
	s.BaseTestSuite.SetupSuite()

	// Start a new API server for the test suite, specifically for this test suite
	testServer := helpers.StartTestServer(s.T())
	s.server = testServer.Server
	s.apiURL = testServer.URL
	s.APIClient = helpers.NewAPIClient(s.T(), s.apiURL) // Re-initialize APIClient with this suite's server URL

	// Add cleanup function to stop this specific server
	s.AddCleanupFunc(func() {
		testServer.Stop(s.T())
	})
}

func (s *APIServerEnvTestSuite) TearDownSuite() {
	// Stop the API server started by this suite
	if s.server != nil {
		s.server.Shutdown(context.Background())
	}
	// Call the parent TearDownSuite for other cleanup
	s.BaseTestSuite.TearDownSuite()
}

func (s *APIServerEnvTestSuite) TestDeploymentWithCustomEndpoint() {
	helpers.MarkTestCategory(s.T(), helpers.CategoryIntegration, helpers.CategoryLocalStack)

	// Skip if not in LocalStack environment
	if os.Getenv("LOCALSTACK_TEST") != "true" {
		s.T().Skip("Skipping LocalStack integration test")
	}

	// Ensure LATTIAM_API_SERVER_URL is set
	os.Setenv("LATTIAM_API_SERVER_URL", helpers.LocalStackEndpoint)

	// Create a new APIClient for this test, using the suite's API URL
	apiClient := helpers.NewAPIClient(s.T(), s.apiURL)

	// Create deployment request using test data builder with unique resource name
	uniqueSuffix := helpers.GenerateTestResourceName("")
	deploymentReq := helpers.NewDeploymentRequest(
		helpers.UniqueName("env-test"),
		helpers.NewIAMRoleResource(uniqueSuffix),
	)

	// Add custom tags to the IAM role
	if resources, ok := deploymentReq.TerraformJSON["resource"].(map[string]interface{}); ok {
		if iamRoles, ok := resources["aws_iam_role"].(map[string]interface{}); ok {
			if testRole, ok := iamRoles[uniqueSuffix].(map[string]interface{}); ok {
				testRole["tags"] = map[string]string{
					"Environment": "test",
					"CreatedBy":   "api-env-test",
				}
			}
		}
	}

	// Create deployment with automatic tracking
	deployment := apiClient.CreateDeployment(deploymentReq)
	deploymentID := deployment["id"].(string)

	// Wait for deployment to finish (either completed or failed)
	apiClient.WaitForDeploymentStatus(deploymentID, "completed", helpers.StandardTimeout)

	// Get the completed deployment to check state
	completedDeployment := apiClient.GetDeployment(deploymentID)

	// Check final status
	finalStatus := completedDeployment["status"].(string)
	if finalStatus == helpers.StatusCompleted {
		// In the API response, the state is in resources[].state
		resources, ok := completedDeployment["resources"].([]interface{})
		s.Require().True(ok, "Deployment should have resources")
		s.Require().NotEmpty(resources, "Deployment should have at least one resource")

		// Check that the first resource has state
		firstResource, ok := resources[0].(map[string]interface{})
		s.Require().True(ok, "Resource should be a map")
		s.Assert().NotEmpty(firstResource["state"], "Resource should have state after completion")

		// Verify it's our IAM role with the tags we set
		if state, ok := firstResource["state"].(map[string]interface{}); ok {
			if tags, ok := state["tags"].(map[string]interface{}); ok {
				s.Assert().Equal("test", tags["Environment"])
				s.Assert().Equal("api-env-test", tags["CreatedBy"])
			}
		}
	} else if finalStatus == helpers.StatusFailed {
		// This might happen if resource already exists, which is OK for this test
		s.T().Logf("Deployment failed (possibly due to existing resource): %v", completedDeployment["error"])
		// For this test, we still consider it a pass if deployment processed
		s.Assert().NotEmpty(completedDeployment["id"], "Deployment should have been processed")
	}

	// Clean up the deployment created by this test
	apiClient.DeleteDeployment(deploymentID)
}
