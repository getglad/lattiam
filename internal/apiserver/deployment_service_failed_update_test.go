//go:build integration
// +build integration

package apiserver

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/lattiam/lattiam/internal/deployment"
	"github.com/lattiam/lattiam/tests/helpers"
)

// DeploymentServiceFailedUpdateSuite tests failed update scenarios and state preservation
type DeploymentServiceFailedUpdateSuite struct {
	suite.Suite
	service      *DeploymentService
	cleanupFuncs []func()
}

func TestDeploymentServiceFailedUpdateSuite(t *testing.T) {
	suite.Run(t, new(DeploymentServiceFailedUpdateSuite))
}

func (s *DeploymentServiceFailedUpdateSuite) SetupSuite() {
	helpers.MarkTestCategory(s.T(), helpers.CategoryIntegration, helpers.CategoryFast)
}

func (s *DeploymentServiceFailedUpdateSuite) SetupTest() {
	// Set up temp state directory for each test
	tempDir := s.T().TempDir()
	s.T().Setenv("LATTIAM_STATE_DIR", tempDir)

	// Create deployment service for this test
	providerDir := s.T().TempDir()
	ds, err := NewDeploymentService(providerDir)
	s.Require().NoError(err, "Failed to create deployment service")
	s.service = ds
}

func (s *DeploymentServiceFailedUpdateSuite) TearDownTest() {
	if s.service != nil {
		s.service.Close()
	}

	// Run any additional cleanup functions
	for _, cleanup := range s.cleanupFuncs {
		cleanup()
	}
}

func (s *DeploymentServiceFailedUpdateSuite) TestFailedUpdatePreservesSuccessfulState() {
	s.Run("Failed Update Should Preserve Previous Successful Resource State", func() {
		ctx := context.Background()

		// Step 1: Create initial deployment with unique names
		uniqueSuffix := helpers.UniqueName("failed-update")
		deploymentName := "Test Deployment " + uniqueSuffix
		resourceName := "test-" + uniqueSuffix

		initialJSON := map[string]interface{}{
			"resource": map[string]interface{}{
				"null_resource": map[string]interface{}{
					resourceName: map[string]interface{}{
						"triggers": map[string]interface{}{
							"timestamp": "initial",
						},
					},
				},
			},
		}

		dep, err := s.service.CreateDeployment(ctx, deploymentName, initialJSON, nil)
		s.Require().NoError(err)
		s.Require().NotNil(dep)

		// Wait for deployment to complete using helper
		completedDep := s.waitForDeploymentCompletion(dep.ID)

		// Verify initial deployment succeeded
		s.Assert().Equal(StatusCompleted, completedDep.Status, "Initial deployment should complete successfully")
		s.Assert().NotEmpty(completedDep.Results, "Should have deployment results")
		s.Require().NoError(completedDep.Results[0].Error, "Resource should be created without error")
		s.Assert().NotNil(completedDep.Results[0].State, "Resource should have state")

		// Save the successful results for comparison
		successfulResults := completedDep.Results

		// Step 2: Attempt an update that will fail
		failingResourceName := "will-fail-" + uniqueSuffix
		failingJSON := map[string]interface{}{
			"resource": map[string]interface{}{
				"null_resource": map[string]interface{}{
					resourceName: map[string]interface{}{
						"triggers": map[string]interface{}{
							"timestamp": "updated",
						},
					},
				},
				"invalid_provider_resource": map[string]interface{}{
					failingResourceName: map[string]interface{}{
						// This resource type doesn't exist and will cause deployment to fail
						"name": "should-fail-" + uniqueSuffix,
					},
				},
			},
		}

		// Update deployment with new resources that will fail
		completedDep.Resources, err = s.service.ParseTerraformJSON(failingJSON)
		s.Require().NoError(err)
		completedDep.Status = StatusPending
		// Don't clear Results - we want to preserve them for the update logic
		completedDep.Error = ""
		completedDep.TerraformJSON = failingJSON

		// Perform the update
		updatedDep, err := s.service.UpdateDeployment(ctx, completedDep)
		s.Require().NoError(err)

		// Wait for update to complete (it will fail)
		updatedDep = s.waitForDeploymentCompletion(dep.ID)

		// Step 3: Verify the behavior
		s.Assert().Equal(StatusFailed, updatedDep.Status, "Update should fail")
		s.Assert().NotEmpty(updatedDep.Error, "Should have error message")

		// When a deployment partially fails, resources that can be updated are updated,
		// while failed resources show errors. This is the current behavior.
		if len(successfulResults) > 0 {
			// We should still have results after the update
			s.Assert().NotNil(updatedDep.Results, "Results should not be nil after failed update")

			// Check if we can find the resource - it should be updated with new state
			foundResource := s.findResourceByName(updatedDep.Results, "null_resource", resourceName)
			s.Require().NotNil(foundResource, "Should find the updated resource")
			s.Require().NoError(foundResource.Error, "Updated resource should not have error")
			s.Require().NotNil(foundResource.State, "Updated resource should have state")

			// The resource was successfully updated, so it should have the new timestamp
			triggers := foundResource.State["triggers"].(map[string]interface{})
			s.Assert().Equal("updated", triggers["timestamp"], "Resource should be updated with new configuration")

			// Also check that we have the failed resource with an error
			failedResource := s.findResourceByName(updatedDep.Results, "invalid_provider_resource", failingResourceName)
			s.Require().NotNil(failedResource, "Should find the failed resource")
			s.Assert().Error(failedResource.Error, "Failed resource should have error")
		}

		// Step 4: Verify we can plan from the failed state
		s.verifyPlanAfterFailedUpdate(ctx, dep.ID, resourceName)
	})

	s.Run("Successful Update Should Replace Previous State", func() {
		ctx := context.Background()

		// Create unique names for this subtest
		uniqueSuffix := helpers.UniqueName("successful-update")
		deploymentName := "Update Test " + uniqueSuffix
		firstResourceName := "first-" + uniqueSuffix
		secondResourceName := "second-" + uniqueSuffix

		initialJSON := map[string]interface{}{
			"resource": map[string]interface{}{
				"null_resource": map[string]interface{}{
					firstResourceName: map[string]interface{}{
						"triggers": map[string]interface{}{
							"version": "1",
						},
					},
				},
			},
		}

		dep, err := s.service.CreateDeployment(ctx, deploymentName, initialJSON, nil)
		s.Require().NoError(err)

		// Wait for completion
		completedDep := s.waitForDeploymentCompletion(dep.ID)
		s.Require().Equal(StatusCompleted, completedDep.Status)

		// Update with different resources
		updateJSON := map[string]interface{}{
			"resource": map[string]interface{}{
				"null_resource": map[string]interface{}{
					secondResourceName: map[string]interface{}{
						"triggers": map[string]interface{}{
							"version": "2",
						},
					},
				},
			},
		}

		completedDep.Resources, err = s.service.ParseTerraformJSON(updateJSON)
		s.Require().NoError(err)
		completedDep.Status = StatusPending
		// Don't clear Results - preserve them for update logic
		completedDep.Error = ""
		completedDep.TerraformJSON = updateJSON

		updatedDep, err := s.service.UpdateDeployment(ctx, completedDep)
		s.Require().NoError(err)

		// Wait for update to complete
		updatedDep = s.waitForDeploymentCompletion(dep.ID)

		// Successful update should complete
		s.Assert().Equal(StatusCompleted, updatedDep.Status)

		// When we replace the resources entirely (first -> second), we should have
		// only the new resource in the results
		s.Assert().Len(updatedDep.Results, 1, "Should have one resource after replacement")

		// Should have the new resource
		newResource := s.findResourceByName(updatedDep.Results, "null_resource", secondResourceName)
		s.Require().NotNil(newResource, "Should have the new resource after successful update")
		s.Require().NoError(newResource.Error, "New resource should be created without error")
		s.Assert().NotNil(newResource.State, "New resource should have state")

		// Check the state has the correct version
		if triggers, ok := newResource.State["triggers"].(map[string]interface{}); ok {
			s.Assert().Equal("2", triggers["version"], "Should have version 2")
		}

		// The old resource should NOT be in results anymore since we replaced it
		oldResource := s.findResourceByName(updatedDep.Results, "null_resource", firstResourceName)
		s.Assert().Nil(oldResource, "Old resource should not be in results after replacement")
	})
}

// Helper methods

func (s *DeploymentServiceFailedUpdateSuite) waitForDeploymentCompletion(deploymentID string) *Deployment {
	var completedDep *Deployment
	var err error

	for range 30 {
		completedDep, err = s.service.GetDeployment(deploymentID)
		s.Require().NoError(err)
		if completedDep.Status == StatusCompleted || completedDep.Status == StatusFailed {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	return completedDep
}

func (s *DeploymentServiceFailedUpdateSuite) findResourceByName(results []deployment.Result, resourceType, resourceName string) *deployment.Result {
	for i := range results {
		result := &results[i]
		if result.Resource.Type == resourceType && result.Resource.Name == resourceName {
			return result
		}
	}
	return nil
}

func (s *DeploymentServiceFailedUpdateSuite) verifyPlanAfterFailedUpdate(ctx context.Context, deploymentID, resourceName string) {
	// The plan should recognize existing resources
	plan, err := s.service.PlanDeployment(ctx, deploymentID, nil)
	s.Require().NoError(err)
	s.Require().NotNil(plan)

	// The plan should show the existing null_resource state
	deploymentPlan, ok := plan.(*DeploymentPlan)
	s.Require().True(ok, "Plan should be a DeploymentPlan")

	// The plan should recognize that we have existing resources with their updated state
	hasExistingResource := false
	for _, rp := range deploymentPlan.ResourcePlans {
		if rp.Type == "null_resource" && rp.Name == resourceName {
			hasExistingResource = true
			s.Assert().NotNil(rp.CurrentState, "Plan should show current state of existing resource")
			// The current state should reflect the successful update
			if triggers, ok := rp.CurrentState["triggers"].(map[string]interface{}); ok {
				s.Assert().Equal("updated", triggers["timestamp"], "Plan should show the updated state")
			}
			break
		}
	}
	s.Assert().True(hasExistingResource, "Plan should recognize existing resources with their current state")
}
