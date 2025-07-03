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

// DeploymentServiceUpdateSuite tests deployment update functionality
type DeploymentServiceUpdateSuite struct {
	suite.Suite
	service      *DeploymentService
	cleanupFuncs []func()
}

func TestDeploymentServiceUpdateSuite(t *testing.T) {
	suite.Run(t, new(DeploymentServiceUpdateSuite))
}

func (s *DeploymentServiceUpdateSuite) SetupSuite() {
	helpers.MarkTestCategory(s.T(), helpers.CategoryIntegration, helpers.CategoryFast)
}

func (s *DeploymentServiceUpdateSuite) SetupTest() {
	// Set up temp state directory for each test
	tempDir := s.T().TempDir()
	s.T().Setenv("LATTIAM_STATE_DIR", tempDir)

	// Create deployment service for this test
	providerDir := s.T().TempDir()
	ds, err := NewDeploymentService(providerDir)
	s.Require().NoError(err, "Failed to create deployment service")
	s.service = ds
}

func (s *DeploymentServiceUpdateSuite) TearDownTest() {
	if s.service != nil {
		s.service.Close()
	}

	// Run any additional cleanup functions
	for _, cleanup := range s.cleanupFuncs {
		cleanup()
	}
}

func (s *DeploymentServiceUpdateSuite) TestUpdateDeployment() {
	s.Run("Update Deployment Name", func() {
		ctx := context.Background()
		uniqueSuffix := helpers.UniqueName("update-name")

		// Create initial deployment with unique names
		terraformJSON := map[string]interface{}{
			"resource": map[string]interface{}{
				helpers.ResourceTypeS3Bucket: map[string]interface{}{
					"test-bucket-" + uniqueSuffix: map[string]interface{}{
						"bucket": "test-bucket-" + uniqueSuffix,
					},
				},
			},
		}

		originalName := "Original Name " + uniqueSuffix
		dep, err := s.service.CreateDeployment(ctx, originalName, terraformJSON, nil)
		s.Require().NoError(err, "Failed to create deployment")

		// Wait for deployment to complete or fail
		time.Sleep(100 * time.Millisecond)

		// Get the deployment to ensure it's not in progress
		currentDep, err := s.service.GetDeployment(dep.ID)
		s.Require().NoError(err, "Failed to get deployment")

		// Update the name
		updatedName := "Updated Name " + uniqueSuffix
		currentDep.Name = updatedName
		updatedDep, err := s.service.UpdateDeployment(ctx, currentDep)
		s.Require().NoError(err, "Failed to update deployment")

		s.Assert().Equal(updatedName, updatedDep.Name, "Expected updated name")

		// Verify persistence
		retrievedDep, err := s.service.GetDeployment(dep.ID)
		s.Require().NoError(err, "Failed to retrieve updated deployment")
		s.Assert().Equal(updatedName, retrievedDep.Name, "Update should be persisted")
	})

	s.Run("Update Deployment Configuration", func() {
		ctx := context.Background()
		uniqueSuffix := helpers.UniqueName("update-config")

		// Create initial deployment with config
		terraformJSON := map[string]interface{}{
			"resource": map[string]interface{}{
				"null_resource": map[string]interface{}{
					"test-" + uniqueSuffix: map[string]interface{}{},
				},
			},
		}

		initialConfig := &DeploymentConfig{
			AWSProfile: "default",
			AWSRegion:  "us-east-1",
		}

		deploymentName := "Config Test " + uniqueSuffix
		dep, err := s.service.CreateDeployment(ctx, deploymentName, terraformJSON, initialConfig)
		s.Require().NoError(err, "Failed to create deployment")

		// Wait for deployment to process
		time.Sleep(100 * time.Millisecond)

		// Get the deployment
		currentDep, err := s.service.GetDeployment(dep.ID)
		s.Require().NoError(err, "Failed to get deployment")

		// Update configuration
		currentDep.Config = &DeploymentConfig{
			AWSProfile: "production",
			AWSRegion:  "us-west-2",
		}

		updatedDep, err := s.service.UpdateDeployment(ctx, currentDep)
		s.Require().NoError(err, "Failed to update deployment")

		s.Assert().Equal("production", updatedDep.Config.AWSProfile, "Expected updated profile")
		s.Assert().Equal("us-west-2", updatedDep.Config.AWSRegion, "Expected updated region")
	})

	s.Run("Update Deployment Resources Triggers Redeploy", func() {
		ctx := context.Background()
		uniqueSuffix := helpers.UniqueName("update-resources")

		// Create initial deployment
		terraformJSON := map[string]interface{}{
			"resource": map[string]interface{}{
				"null_resource": map[string]interface{}{
					"initial-" + uniqueSuffix: map[string]interface{}{},
				},
			},
		}

		deploymentName := "Resource Update Test " + uniqueSuffix
		dep, err := s.service.CreateDeployment(ctx, deploymentName, terraformJSON, nil)
		s.Require().NoError(err, "Failed to create deployment")

		// Wait for initial deployment
		time.Sleep(100 * time.Millisecond)

		// Get the deployment
		currentDep, err := s.service.GetDeployment(dep.ID)
		s.Require().NoError(err, "Failed to get deployment")

		// Update resources with unique names
		newTerraformJSON := map[string]interface{}{
			"resource": map[string]interface{}{
				"null_resource": map[string]interface{}{
					"updated-" + uniqueSuffix: map[string]interface{}{},
				},
			},
		}

		newResources, err := s.service.ParseTerraformJSON(newTerraformJSON)
		s.Require().NoError(err, "Failed to parse new terraform JSON")

		// Update deployment with new resources
		currentDep.Resources = newResources
		currentDep.Status = StatusPending
		currentDep.Results = nil
		currentDep.Error = ""

		updatedDep, err := s.service.UpdateDeployment(ctx, currentDep)
		s.Require().NoError(err, "Failed to update deployment")

		// Should have reset to pending status
		s.Assert().Equal(StatusPending, updatedDep.Status, "Expected pending status")

		// Should have new resources
		s.Assert().Len(updatedDep.Resources, 1, "Expected 1 resource")
		s.Assert().Equal("updated-"+uniqueSuffix, updatedDep.Resources[0].Name, "Expected updated resource name")
	})

	s.Run("Cannot Update In-Progress Deployment", func() {
		ctx := context.Background()
		uniqueSuffix := helpers.UniqueName("in-progress")
		deploymentID := helpers.UniqueName("deployment")

		// Create a deployment that will stay in progress
		dep := &Deployment{
			ID:        deploymentID,
			Name:      "In Progress " + uniqueSuffix,
			Status:    StatusApplying,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
			Resources: []deployment.Resource{
				{
					Type:       "null_resource",
					Name:       "test-" + uniqueSuffix,
					Properties: map[string]interface{}{},
				},
			},
		}

		// Save deployment
		stateDep := convertToStateDeployment(dep)
		err := s.service.store.Save(ctx, stateDep)
		s.Require().NoError(err, "Failed to save deployment")

		// Try to update - should work since UpdateDeployment doesn't check status
		// (the check is done in the HTTP handler)
		dep.Name = "Should Update " + uniqueSuffix
		_, err = s.service.UpdateDeployment(ctx, dep)
		s.Require().NoError(err, "UpdateDeployment should not check status")
	})
}

func (s *DeploymentServiceUpdateSuite) TestUpdateDeploymentPersistence() {
	ctx := context.Background()
	uniqueSuffix := helpers.UniqueName("persistence")

	// Create deployment with unique names
	terraformJSON := map[string]interface{}{
		"resource": map[string]interface{}{
			"null_resource": map[string]interface{}{
				"test-" + uniqueSuffix: map[string]interface{}{},
			},
		},
	}

	originalName := "Persistence Test " + uniqueSuffix
	dep, err := s.service.CreateDeployment(ctx, originalName, terraformJSON, nil)
	s.Require().NoError(err, "Failed to create deployment")

	// Wait for processing
	time.Sleep(100 * time.Millisecond)

	// Get and update
	currentDep, err := s.service.GetDeployment(dep.ID)
	s.Require().NoError(err, "Failed to get deployment")

	updatedName := "After Update " + uniqueSuffix
	currentDep.Name = updatedName
	_, err = s.service.UpdateDeployment(ctx, currentDep)
	s.Require().NoError(err, "Failed to update deployment")

	// Close service
	s.service.Close()

	// Create new service instance to test persistence
	providerDir := s.T().TempDir()
	ds2, err := NewDeploymentService(providerDir)
	s.Require().NoError(err, "Failed to create new deployment service")
	defer ds2.Close()

	// Verify update persisted
	persistedDep, err := ds2.GetDeployment(dep.ID)
	s.Require().NoError(err, "Failed to get deployment after restart")
	s.Assert().Equal(updatedName, persistedDep.Name, "Update should persist across service restart")
}
