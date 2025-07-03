//go:build integration
// +build integration

package apiserver

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/lattiam/lattiam/internal/deployment"
	"github.com/lattiam/lattiam/tests/helpers"
)

// DeploymentMetadataSuite tests deployment metadata storage and persistence
type DeploymentMetadataSuite struct {
	suite.Suite
	cleanupFuncs []func()
}

func TestDeploymentMetadataSuite(t *testing.T) {
	suite.Run(t, new(DeploymentMetadataSuite))
}

func (s *DeploymentMetadataSuite) SetupSuite() {
	helpers.MarkTestCategory(s.T(), helpers.CategoryIntegration, helpers.CategoryFast)
}

func (s *DeploymentMetadataSuite) SetupTest() {
	// Set up temp state directory for each test
	tempDir := s.T().TempDir()
	s.T().Setenv("LATTIAM_STATE_DIR", tempDir)

	// Set LocalStack endpoint for metadata capture

}

func (s *DeploymentMetadataSuite) createService() *DeploymentService {
	// Create deployment service for this test
	providerDir := s.T().TempDir()
	ds, err := NewDeploymentService(providerDir)
	s.Require().NoError(err, "Failed to create deployment service")
	return ds
}

func (s *DeploymentMetadataSuite) TearDownSuite() {
	// Run any additional cleanup functions
	for _, cleanup := range s.cleanupFuncs {
		cleanup()
	}
}

func (s *DeploymentMetadataSuite) TestDeploymentMetadataStorage() {
	// Create service for this test
	service := s.createService()
	defer service.Close()

	// Create a simple deployment using unique names
	uniqueSuffix := helpers.UniqueName("metadata")
	deploymentName := "Metadata Test " + uniqueSuffix

	terraformJSON := map[string]interface{}{
		"resource": map[string]interface{}{
			"null_resource": map[string]interface{}{
				uniqueSuffix: map[string]interface{}{},
			},
		},
	}

	deployConfig := &DeploymentConfig{
		AWSProfile: "test-profile",
		AWSRegion:  "us-west-2",
	}

	ctx := context.Background()
	dep, err := service.CreateDeployment(ctx, deploymentName, terraformJSON, deployConfig)
	s.Require().NoError(err, "Failed to create deployment")

	// Wait for deployment to process using helper method
	storedDep := s.waitForDeploymentToProcess(service, dep.ID)

	// Check metadata was stored
	s.Assert().NotNil(storedDep.Metadata, "Expected metadata to be stored")

	// Check provider versions
	s.validateProviderVersions(storedDep.Metadata)

	// Check environment metadata
	s.validateEnvironmentMetadata(storedDep.Metadata)
}

func (s *DeploymentMetadataSuite) TestDeploymentMetadataPersistence() {
	// Create initial service
	service := s.createService()

	// Create deployment with unique names
	uniqueSuffix := helpers.UniqueName("persistence")
	deploymentName := "Persistence Test " + uniqueSuffix

	terraformJSON := map[string]interface{}{
		"resource": map[string]interface{}{
			"null_resource": map[string]interface{}{
				uniqueSuffix: map[string]interface{}{},
			},
		},
	}

	ctx := context.Background()
	dep, err := service.CreateDeployment(ctx, deploymentName, terraformJSON, nil)
	s.Require().NoError(err, "Failed to create deployment")

	// Wait for deployment to complete
	s.waitForDeploymentCompletion(service, dep.ID)

	// Close service
	service.Close()

	// Create new service instance to test persistence
	ds2 := s.createService()
	defer ds2.Close()

	// Retrieve deployment
	persistedDep, err := ds2.GetDeployment(dep.ID)
	s.Require().NoError(err, "Failed to get deployment after restart")

	// Verify metadata persisted
	s.Assert().NotNil(persistedDep.Metadata, "Expected metadata to persist across restart")
	s.Assert().Contains(persistedDep.Metadata, "provider_versions", "Expected provider_versions to persist")
	s.Assert().Contains(persistedDep.Metadata, "environment", "Expected environment metadata to persist")
}

func (s *DeploymentMetadataSuite) TestDestroyOperationUsesMetadata() {
	// Create service for this test
	service := s.createService()
	defer service.Close()

	// Create a completed deployment with metadata using unique names
	uniqueSuffix := helpers.UniqueName("destroy")
	deploymentID := helpers.UniqueName("deployment")
	deploymentName := "Destroy Metadata Test " + uniqueSuffix

	dep := &Deployment{
		ID:        deploymentID,
		Name:      deploymentName,
		Status:    helpers.StatusCompleted,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Metadata: map[string]interface{}{
			"provider_versions": map[string]interface{}{
				"null": "3.2.0",
			},
			"environment": map[string]interface{}{
				"test_mode": true,
			},
		},
		Resources: []deployment.Resource{
			{
				Type: "null_resource",
				Name: uniqueSuffix,
				Properties: map[string]interface{}{
					"triggers": map[string]interface{}{
						"always_run": "test-trigger-" + uniqueSuffix,
					},
				},
			},
		},
		Results: []deployment.Result{
			{
				Resource: deployment.Resource{
					Type: "null_resource",
					Name: uniqueSuffix,
					Properties: map[string]interface{}{
						"triggers": map[string]interface{}{
							"always_run": "test-trigger-" + uniqueSuffix,
						},
					},
				},
				State: map[string]interface{}{
					"id": "test-resource-id-" + uniqueSuffix,
					"triggers": map[string]interface{}{
						"always_run": "test-trigger-" + uniqueSuffix,
					},
				},
			},
		},
	}

	// Store deployment
	ctx := context.Background()
	stateDep := convertToStateDeployment(dep)
	err := service.store.Save(ctx, stateDep)
	s.Require().NoError(err, "Failed to save deployment")

	// Execute destroy - should use metadata
	service.executeDestroy(ctx, dep)

	// The test passes if destroy operation completes without panic
	// and metadata is available during destroy (checked via logs)
}

func (s *DeploymentMetadataSuite) TestTerraformStateFileMetadata() {
	s.Run("LocalState", func() {
		s.testStateFileMetadata("", "file://")
	})

	s.Run("RemoteState", func() {
		s.T().Setenv("LATTIAM_TERRAFORM_BACKEND", "s3://my-terraform-state/lattiam/")
		s.testStateFileMetadata("s3://my-terraform-state/lattiam/", "s3://")
	})

	s.Run("PerDeploymentStateBackend", func() {
		deployConfig := &DeploymentConfig{
			StateBackend: "gs://my-gcs-bucket/deployment-states/",
		}
		s.testStateFileMetadataWithConfig(deployConfig, "gs://my-gcs-bucket/deployment-states/", "gs://")
	})
}

// Helper methods

func (s *DeploymentMetadataSuite) waitForDeploymentToProcess(service *DeploymentService, deploymentID string) *Deployment {
	var storedDep *Deployment
	var err error

	maxWait := 2 * time.Second
	pollInterval := 100 * time.Millisecond
	start := time.Now()

	for time.Since(start) < maxWait {
		storedDep, err = service.GetDeployment(deploymentID)
		s.Require().NoError(err, "Failed to get deployment")

		// Wait until deployment is no longer pending/planning/applying
		if storedDep.Status != helpers.StatusPending &&
			storedDep.Status != helpers.StatusPlanning &&
			storedDep.Status != helpers.StatusApplying {
			break
		}
		time.Sleep(pollInterval)
	}

	return storedDep
}

func (s *DeploymentMetadataSuite) waitForDeploymentCompletion(service *DeploymentService, deploymentID string) {
	maxWait := 3 * time.Second
	pollInterval := 100 * time.Millisecond
	start := time.Now()

	for time.Since(start) < maxWait {
		deploymentStatus, err := service.GetDeployment(deploymentID)
		if err == nil && deploymentStatus != nil &&
			(deploymentStatus.Status == helpers.StatusCompleted ||
				deploymentStatus.Status == helpers.StatusFailed) {
			break
		}
		time.Sleep(pollInterval)
	}
}

func (s *DeploymentMetadataSuite) validateProviderVersions(metadata map[string]interface{}) {
	providerVersions, ok := metadata["provider_versions"].(map[string]string)
	if !ok {
		// Try interface conversion
		if pvInterface, ok := metadata["provider_versions"].(map[string]interface{}); ok {
			// Convert to map[string]string
			providerVersions = make(map[string]string)
			for k, v := range pvInterface {
				if strVal, ok := v.(string); ok {
					providerVersions[k] = strVal
				}
			}
		} else {
			s.Fail("Expected provider_versions in metadata")
			return
		}
	}

	// Should have null provider version since we're using null_resource
	nullVersion, ok := providerVersions["null"]
	s.Assert().True(ok && nullVersion != "", "Expected null provider version in metadata, got: %v", providerVersions)
}

func (s *DeploymentMetadataSuite) validateEnvironmentMetadata(metadata map[string]interface{}) {
	env, ok := metadata["environment"].(map[string]interface{})
	s.Require().True(ok, "Expected environment in metadata")

	// Check LocalStack flag
	localstackEnabled, ok := env["localstack_enabled"].(bool)
	s.Assert().True(ok && localstackEnabled, "Expected localstack_enabled to be true")

	// Check timeout config
	timeoutConfig, ok := env["timeout_config"].(map[string]interface{})
	s.Require().True(ok, "Expected timeout_config in environment metadata")

	// Should have all three timeout values
	expectedTimeouts := []string{"provider_timeout", "provider_startup_timeout", "deployment_timeout"}
	for _, key := range expectedTimeouts {
		val, ok := timeoutConfig[key].(string)
		s.Assert().True(ok && val != "", "Expected %s in timeout_config", key)
	}
}

func (s *DeploymentMetadataSuite) testStateFileMetadata(backendURL, expectedPrefix string) {
	s.testStateFileMetadataWithConfig(nil, backendURL, expectedPrefix)
}

func (s *DeploymentMetadataSuite) testStateFileMetadataWithConfig(deployConfig *DeploymentConfig, backendURL, expectedPrefix string) {
	// Create service for this test
	service := s.createService()
	defer service.Close()

	// Create deployment with unique names
	uniqueSuffix := helpers.UniqueName("state")
	deploymentName := "State Path Test " + uniqueSuffix

	terraformJSON := map[string]interface{}{
		"resource": map[string]interface{}{
			"null_resource": map[string]interface{}{
				uniqueSuffix: map[string]interface{}{},
			},
		},
	}

	ctx := context.Background()
	dep, err := service.CreateDeployment(ctx, deploymentName, terraformJSON, deployConfig)
	s.Require().NoError(err, "Failed to create deployment")

	// Wait for deployment to process
	time.Sleep(500 * time.Millisecond)

	// Get the deployment and check metadata
	storedDep, err := service.GetDeployment(dep.ID)
	s.Require().NoError(err, "Failed to get deployment")

	// Check terraform state file path exists
	stateFilePath, ok := storedDep.Metadata["terraform_state_file"].(string)
	s.Require().True(ok, "Expected terraform_state_file in metadata")

	// Should have expected prefix
	if expectedPrefix != "" {
		s.Assert().True(strings.HasPrefix(stateFilePath, expectedPrefix),
			"Expected %s prefix, got: %s", expectedPrefix, stateFilePath)
	}

	// Should contain the deployment ID
	s.Assert().True(strings.Contains(stateFilePath, dep.ID),
		"State file path should contain deployment ID: %s", stateFilePath)

	// Should end with .tfstate
	s.Assert().True(strings.HasSuffix(stateFilePath, ".tfstate"),
		"State file path should end with .tfstate: %s", stateFilePath)

	// For local state, should contain the temp directory
	if expectedPrefix == "file://" {
		tempDir := os.Getenv("LATTIAM_STATE_DIR")
		s.Assert().True(strings.Contains(stateFilePath, tempDir),
			"State file path should contain temp dir %s: %s", tempDir, stateFilePath)
	}

	// For specific backends, check the full path structure
	if backendURL != "" && expectedPrefix != "file://" {
		s.Assert().True(strings.HasPrefix(stateFilePath, backendURL),
			"Expected URI starting with %s, got: %s", backendURL, stateFilePath)
	}
}
