//go:build integration
// +build integration

package integration

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"

	"github.com/lattiam/lattiam/tests/helpers"
)

type DeploymentDependenciesTestSuite struct {
	helpers.BaseTestSuite
}

func TestDeploymentDependenciesTestSuite(t *testing.T) {
	suite.Run(t, new(DeploymentDependenciesTestSuite))
}

// TestDeploymentDependencies tests sophisticated dependency resolution
//
//nolint:paralleltest // integration tests use shared resources
func (s *DeploymentDependenciesTestSuite) TestDeploymentDependencies() {
	s.T().Run("complex dependency graph", func(t *testing.T) {
		// Load the sophisticated dependencies fixture
		var data map[string]interface{}
		helpers.LoadFixtureJSON(s.T(), "deployments/dependencies/complex-deps.json", &data)

		// Create deployment
		deployment := s.APIClient.CreateDeployment(data)

		deploymentID := deployment["id"].(string)
		s.Require().NotEmpty(deploymentID)

		// Wait for deployment to finish
		s.APIClient.WaitForDeploymentStatus(deploymentID, "completed", 2*time.Minute)

		// Get deployment details to verify resources were created
		result := s.APIClient.GetDeployment(deploymentID)

		// Verify all resources were created
		resources := result["resources"].([]interface{})
		assert.Len(s.T(), resources, 8) // Should have 8 resources from the fixture

		// Verify dependency chain worked
		// The bucket names should contain the prefix from random_string
		var prefixValue string
		var bucketNames []string

		for _, res := range resources {
			resource := res.(map[string]interface{})
			resourceType := resource["type"].(string)

			if resourceType == "random_string" && resource["name"] == "prefix" {
				state := resource["state"].(map[string]interface{})
				prefixValue = state["result"].(string)
			}

			if resourceType == "aws_s3_bucket" {
				state := resource["state"].(map[string]interface{})
				bucketName := state["bucket"].(string)
				bucketNames = append(bucketNames, bucketName)
			}
		}

		// Verify prefix was used in bucket names
		assert.NotEmpty(s.T(), prefixValue)
		for _, bucketName := range bucketNames {
			assert.Contains(s.T(), bucketName, prefixValue, "Bucket name should contain the random prefix")
		}

		// Clean up
		s.APIClient.DeleteDeployment(deploymentID)
	})

	s.T().Run("basic multi-resource dependencies", func(t *testing.T) {
		// Load the basic dependencies fixture
		var data map[string]interface{}
		helpers.LoadFixtureJSON(s.T(), "deployments/dependencies/basic-deps.json", &data)

		deployment := s.APIClient.CreateDeployment(data)

		deploymentID := deployment["id"].(string)
		s.APIClient.WaitForDeploymentStatus(deploymentID, "completed", 60*time.Second)

		// Verify resources were created with proper interpolation
		result := s.APIClient.GetDeployment(deploymentID)

		resources := result["resources"].([]interface{})

		// Find the bucket and verify it has the random suffix
		var randomSuffix string
		var bucketName string

		for _, res := range resources {
			resource := res.(map[string]interface{})
			if resource["type"] == "random_string" {
				state := resource["state"].(map[string]interface{})
				randomSuffix = state["result"].(string)
			}
			if resource["type"] == "aws_s3_bucket" {
				state := resource["state"].(map[string]interface{})
				bucketName = state["bucket"].(string)
			}
		}

		assert.NotEmpty(s.T(), randomSuffix)
		assert.NotEmpty(s.T(), bucketName)
		assert.True(s.T(), strings.HasSuffix(bucketName, randomSuffix),
			"Bucket name should end with the random suffix")

		s.APIClient.DeleteDeployment(deploymentID)
	})

}
