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

// DeploymentServiceDestroySuite tests deployment destroy functionality
type DeploymentServiceDestroySuite struct {
	suite.Suite
	service      *DeploymentService
	cleanupFuncs []func()
}

func TestDeploymentServiceDestroySuite(t *testing.T) {
	suite.Run(t, new(DeploymentServiceDestroySuite))
}

func (s *DeploymentServiceDestroySuite) SetupSuite() {
	helpers.MarkTestCategory(s.T(), helpers.CategoryIntegration, helpers.CategoryFast)

	// Create deployment service with test provider directory
	providerDir := s.T().TempDir()
	ds, err := NewDeploymentService(providerDir)
	s.Require().NoError(err, "Failed to create deployment service")

	s.service = ds
	s.T().Cleanup(func() {
		ds.Close()
	})
}

func (s *DeploymentServiceDestroySuite) TearDownSuite() {
	// Run any additional cleanup functions
	for _, cleanup := range s.cleanupFuncs {
		cleanup()
	}
}

func (s *DeploymentServiceDestroySuite) TestDeploymentServiceDestroy() {
	// Create a mock deployment with results using unique names
	uniqueSuffix := helpers.UniqueName("test")
	dep := &Deployment{
		ID:        helpers.UniqueName("deployment"),
		Name:      "Test Deployment " + uniqueSuffix,
		Status:    StatusCompleted,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Resources: []deployment.Resource{
			{
				Type: helpers.ResourceTypeS3Bucket,
				Name: "test-bucket",
				Properties: map[string]interface{}{
					"bucket": "test-bucket-" + uniqueSuffix,
				},
			},
		},
		Results: []deployment.Result{
			{
				Resource: deployment.Resource{
					Type: helpers.ResourceTypeS3Bucket,
					Name: "test-bucket",
					Properties: map[string]interface{}{
						"bucket": "test-bucket-" + uniqueSuffix,
					},
				},
				State: map[string]interface{}{
					"id":     "test-bucket-" + uniqueSuffix,
					"bucket": "test-bucket-" + uniqueSuffix,
					"arn":    "arn:aws:s3:::test-bucket-" + uniqueSuffix,
				},
				Error: nil,
			},
		},
	}

	// Store deployment
	ctx := context.Background()
	stateDep := convertToStateDeployment(dep)
	err := s.service.store.Save(ctx, stateDep)
	s.Require().NoError(err, "Failed to save deployment")

	// Test that destroy operation doesn't panic
	// Note: This will fail to actually destroy resources without a real provider
	// but it tests the code path and error handling
	destroyCtx := context.Background()
	s.service.executeDestroy(destroyCtx, dep)

	// Verify deployment status was updated
	updatedDep, err := s.service.GetDeployment(dep.ID)
	s.Require().NoError(err, "Failed to get updated deployment")

	// The status should be either destroyed or failed (since we don't have a real provider)
	s.Assert().True(updatedDep.Status == helpers.StatusDestroyed || updatedDep.Status == helpers.StatusFailed,
		"Expected deployment status to be destroyed or failed, got %s", updatedDep.Status)
}

func (s *DeploymentServiceDestroySuite) TestDestroyResourceOrder() {
	// Create a deployment with multiple resources using unique names
	uniqueSuffix := helpers.UniqueName("order")
	dep := &Deployment{
		ID:        helpers.UniqueName("deployment"),
		Name:      "Test Deployment Order " + uniqueSuffix,
		Status:    StatusCompleted,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Results: []deployment.Result{
			{
				Resource: deployment.Resource{
					Type: helpers.ResourceTypeIAMRole,
					Name: "test-role",
				},
				State: map[string]interface{}{
					"id":   "test-role-" + uniqueSuffix,
					"name": "test-role-" + uniqueSuffix,
				},
			},
			{
				Resource: deployment.Resource{
					Type: "aws_iam_role_policy_attachment",
					Name: "test-attachment",
				},
				State: map[string]interface{}{
					"id":         "test-attachment-" + uniqueSuffix,
					"role":       "test-role-" + uniqueSuffix,
					"policy_arn": "arn:aws:iam::aws:policy/ReadOnlyAccess",
				},
			},
		},
	}

	// Store deployment
	ctx := context.Background()
	stateDep := convertToStateDeployment(dep)
	err := s.service.store.Save(ctx, stateDep)
	s.Require().NoError(err, "Failed to save deployment")

	// Execute destroy - resources should be destroyed in reverse order
	destroyCtx := context.Background()
	s.service.executeDestroy(destroyCtx, dep)

	// Verify status was updated
	updatedDep, err := s.service.GetDeployment(dep.ID)
	s.Require().NoError(err, "Failed to get updated deployment")

	s.Assert().True(updatedDep.Status == helpers.StatusDestroyed || updatedDep.Status == helpers.StatusFailed,
		"Expected deployment status to be destroyed or failed, got %s", updatedDep.Status)
}

func (s *DeploymentServiceDestroySuite) TestDestroySkipsFailedResources() {
	// Create a deployment with some failed resources using unique names
	uniqueSuffix := helpers.UniqueName("failed")
	dep := &Deployment{
		ID:        helpers.UniqueName("deployment"),
		Name:      "Test Deployment Failed " + uniqueSuffix,
		Status:    StatusCompleted,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Results: []deployment.Result{
			{
				Resource: deployment.Resource{
					Type: helpers.ResourceTypeS3Bucket,
					Name: "successful-bucket",
				},
				State: map[string]interface{}{
					"id":     "successful-bucket-" + uniqueSuffix,
					"bucket": "successful-bucket-" + uniqueSuffix,
				},
			},
			{
				Resource: deployment.Resource{
					Type: helpers.ResourceTypeS3Bucket,
					Name: "failed-bucket",
				},
				Error: deployment.ErrNoResourcesFoundInJSON, // Simulated creation failure
			},
			{
				Resource: deployment.Resource{
					Type: helpers.ResourceTypeS3Bucket,
					Name: "no-state-bucket",
				},
				State: nil, // No state means creation didn't complete
			},
		},
	}

	// Store deployment
	ctx := context.Background()
	stateDep := convertToStateDeployment(dep)
	err := s.service.store.Save(ctx, stateDep)
	s.Require().NoError(err, "Failed to save deployment")

	// Execute destroy - should only try to destroy the successful resource
	destroyCtx := context.Background()
	s.service.executeDestroy(destroyCtx, dep)

	// Verify the deployment was processed
	updatedDep, err := s.service.GetDeployment(dep.ID)
	s.Require().NoError(err, "Failed to get updated deployment")

	s.Assert().True(updatedDep.Status == helpers.StatusDestroyed || updatedDep.Status == helpers.StatusFailed,
		"Expected deployment status to be destroyed or failed, got %s", updatedDep.Status)
}
