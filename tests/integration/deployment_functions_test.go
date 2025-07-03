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

type DeploymentFunctionsTestSuite struct {
	helpers.BaseTestSuite
}

func TestDeploymentFunctionsTestSuite(t *testing.T) {
	suite.Run(t, new(DeploymentFunctionsTestSuite))
}

// TestDeploymentWithFunctions tests that Terraform functions are evaluated correctly during deployment
//
//nolint:paralleltest // integration tests use shared resources
func (s *DeploymentFunctionsTestSuite) TestDeploymentWithFunctions() {
	s.T().Run("all function categories", func(t *testing.T) {
		// Load the comprehensive function test fixture
		var data map[string]interface{}
		helpers.LoadFixtureJSON(t, "deployments/functions/all-functions.json", &data)

		// Create deployment
		deployment := s.APIClient.CreateDeployment(data)

		deploymentID, ok := deployment["id"].(string)
		s.Require().True(ok, "deployment ID should be a string")
		s.Require().NotEmpty(deploymentID)

		// Wait for deployment to complete
		s.APIClient.WaitForDeploymentStatus(deploymentID, "completed", 60*time.Second)

		// Get deployment details to verify functions were evaluated
		result := s.APIClient.GetDeployment(deploymentID)

		// Verify functions were evaluated
		resources := result["resources"].([]interface{})

		// Check UUID function
		for _, res := range resources {
			resource := res.(map[string]interface{})
			if resource["name"] == "uuid_example" {
				state, ok := resource["state"].(map[string]interface{})
				if !ok || state == nil {
					t.Logf("Warning: resource %s has no state", resource["name"])
					continue
				}
				keepers, ok := state["keepers"].(map[string]interface{})
				if !ok || keepers == nil {
					t.Logf("Warning: resource %s has no keepers in state", resource["name"])
					continue
				}
				uuid, ok := keepers["unique_id"].(string)
				if !ok {
					t.Logf("Warning: resource %s has no unique_id in keepers", resource["name"])
					continue
				}
				// UUID should be 36 characters with hyphens
				assert.Len(t, uuid, 36)
				assert.Contains(t, uuid, "-")
			}
		}

		// Check timestamp function
		for _, res := range resources {
			resource := res.(map[string]interface{})
			if resource["name"] == "timestamp_example" {
				state, ok := resource["state"].(map[string]interface{})
				if !ok || state == nil {
					continue
				}
				keepers, ok := state["keepers"].(map[string]interface{})
				if !ok || keepers == nil {
					continue
				}
				timestamp, ok := keepers["created_at"].(string)
				if !ok {
					continue
				}
				// Timestamp should be RFC3339 format
				assert.Contains(t, timestamp, "T")
				assert.Contains(t, timestamp, "Z")
			}
		}

		// Check string functions
		for _, res := range resources {
			resource := res.(map[string]interface{})
			if resource["name"] == "string_functions" {
				state, ok := resource["state"].(map[string]interface{})
				if !ok || state == nil {
					continue
				}
				keepers, ok := state["keepers"].(map[string]interface{})
				if !ok || keepers == nil {
					continue
				}

				// upper() function
				if upperDemo, ok := keepers["upper_demo"].(string); ok {
					assert.Equal(t, "LATTIAM ROCKS", upperDemo)
				}

				// format() function
				if formatted, ok := keepers["formatted"].(string); ok {
					assert.Equal(t, "Hello World from Lattiam!", formatted)
				}

				// base64encode() function
				if base64, ok := keepers["base64_encoded"].(string); ok {
					assert.Equal(t, "TGF0dGlhbTogQVBJLWRyaXZlbiBpbmZyYXN0cnVjdHVyZQ==", base64)
				}
			}
		}

		// Check math functions
		for _, res := range resources {
			resource := res.(map[string]interface{})
			if resource["name"] == "math_functions" {
				state, ok := resource["state"].(map[string]interface{})
				if !ok || state == nil {
					continue
				}
				keepers, ok := state["keepers"].(map[string]interface{})
				if !ok || keepers == nil {
					continue
				}

				// max() function
				if maxVal, ok := keepers["max_value"].(string); ok {
					assert.Equal(t, "99", maxVal)
				}

				// min() function
				if minVal, ok := keepers["min_value"].(string); ok {
					assert.Equal(t, "3", minVal)
				}

				// abs() function
				if absVal, ok := keepers["abs_negative"].(string); ok {
					assert.Equal(t, "100", absVal)
				}
			}
		}

		// Clean up
		s.APIClient.DeleteDeployment(deploymentID)
	})

	s.T().Run("function evaluation in resource names", func(t *testing.T) {
		// Create a deployment with functions similar to the fixture structure
		deploymentReq := map[string]interface{}{
			"name": "test-function-evaluation",
			"terraform_json": map[string]interface{}{
				"provider": map[string]interface{}{
					"random": map[string]interface{}{
						"version": "3.6.0",
					},
				},
				"resource": map[string]interface{}{
					"random_string": map[string]interface{}{
						"nested_function_test": map[string]interface{}{
							"length":  16,
							"special": false,
							"upper":   true,
							"keepers": map[string]interface{}{
								"function_test": "${upper(\"test-${uuid()}\")}",
								"description":   "Nested function evaluation test",
							},
						},
					},
				},
			},
		}

		deployment := s.APIClient.CreateDeployment(deploymentReq)

		deploymentID, ok := deployment["id"].(string)
		s.Require().True(ok, "deployment ID should be a string")
		s.Require().NotEmpty(deploymentID)
		s.APIClient.WaitForDeploymentStatus(deploymentID, "completed", 30*time.Second)

		// Verify the function was evaluated
		result := s.APIClient.GetDeployment(deploymentID)

		var resource map[string]interface{}
		var state map[string]interface{}
		var keepers map[string]interface{}
		var functionTest string

		s.Require().Eventually(func() bool {
			result = s.APIClient.GetDeployment(deploymentID)
			resourcesRaw, ok := result["resources"].([]interface{})
			if !ok || len(resourcesRaw) == 0 {
				return false
			}
			resource, ok = resourcesRaw[0].(map[string]interface{})
			if !ok {
				return false
			}
			state, ok = resource["state"].(map[string]interface{})
			if !ok || state == nil {
				return false
			}
			keepers, ok = state["keepers"].(map[string]interface{})
			if !ok || keepers == nil {
				return false
			}
			functionTest, ok = keepers["function_test"].(string)
			return ok
		}, 30*time.Second, 1*time.Second, "Timed out waiting for function_test in keepers")

		// Should start with "TEST-" and contain a UUID
		assert.True(t, strings.HasPrefix(functionTest, "TEST-"))
		assert.Len(t, functionTest, 41) // "TEST-" (5) + UUID (36)

		s.APIClient.DeleteDeployment(deploymentID)
	})
}
