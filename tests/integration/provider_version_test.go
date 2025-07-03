package integration

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/lattiam/lattiam/tests/helpers"
)

type ProviderVersionTestSuite struct {
	helpers.BaseTestSuite
}

// TestProviderVersionTestSuite runs the provider version test suite
//
//nolint:paralleltest // Integration tests use shared LocalStack and API server resources
func TestProviderVersionTestSuite(t *testing.T) {
	suite.Run(t, new(ProviderVersionTestSuite))
}

func (s *ProviderVersionTestSuite) SetupTest() {
	s.BaseTestSuite.SetupTest()
}

func (s *ProviderVersionTestSuite) TearDownTest() {
	s.BaseTestSuite.TearDownTest()
	helpers.AssertNoTerraformProviderProcesses(s.T())
}

// TestTerraformRequiredProviders tests provider version configuration via terraform.required_providers
func (s *ProviderVersionTestSuite) TestTerraformRequiredProviders() {
	// Create deployment with terraform.required_providers
	payload := map[string]interface{}{
		"name": "Provider Version Test",
		"terraform_json": map[string]interface{}{
			"terraform": map[string]interface{}{
				"required_providers": map[string]interface{}{
					"aws": map[string]interface{}{
						"source":  "hashicorp/aws",
						"version": "6.0.0",
					},
				},
			},
			"resource": map[string]interface{}{
				"aws_s3_bucket": map[string]interface{}{
					"test": map[string]interface{}{
						"bucket": "test-provider-version-bucket-" + helpers.UniqueName(""),
					},
				},
			},
		},
	}

	result := s.APIClient.CreateDeployment(payload)
	deploymentID := result["id"].(string)

	// Wait a bit for deployment to process
	s.APIClient.WaitForDeploymentStatus(deploymentID, "completed", 2*time.Minute)

	// Check deployment metadata for provider version
	deployment := s.APIClient.GetDeployment(deploymentID)

	// Verify provider version in metadata
	if metadata, ok := deployment["metadata"].(map[string]interface{}); ok {
		if providerVersions, ok := metadata["provider_versions"].(map[string]interface{}); ok {
			s.Equal("6.0.0", providerVersions["aws"], "AWS provider version should be from required_providers")
		} else {
			s.T().Error("No provider_versions in metadata")
		}
	} else {
		s.T().Error("No metadata in deployment")
	}
	s.APIClient.DeleteDeployment(deploymentID)
}

// TestEnvironmentVariable tests provider version configuration via environment variable
func (s *ProviderVersionTestSuite) TestEnvironmentVariable() {
	// Set environment variable
	os.Setenv("AWS_PROVIDER_VERSION", "6.0.0")
	s.T().Cleanup(func() {
		os.Unsetenv("AWS_PROVIDER_VERSION")
	})

	// Create deployment without terraform.required_providers but with provider config
	payload := map[string]interface{}{
		"name": "Env Version Test",
		"terraform_json": map[string]interface{}{
			"provider": map[string]interface{}{
				"aws": map[string]interface{}{
					"version": "6.0.0",
				},
			},
			"resource": map[string]interface{}{
				"aws_s3_bucket": map[string]interface{}{
					"test": map[string]interface{}{
						"bucket": "test-env-version-bucket-" + helpers.UniqueName(""),
					},
				},
			},
		},
	}

	result := s.APIClient.CreateDeployment(payload)
	deploymentID := result["id"].(string)

	// Wait a bit for deployment to process
	s.APIClient.WaitForDeploymentStatus(deploymentID, "completed", 2*time.Minute)

	// Check deployment metadata for provider version
	deployment := s.APIClient.GetDeployment(deploymentID)

	// Verify provider version in metadata
	if metadata, ok := deployment["metadata"].(map[string]interface{}); ok {
		if providerVersions, ok := metadata["provider_versions"].(map[string]interface{}); ok {
			s.Equal("6.0.0", providerVersions["aws"], "AWS provider version should be from environment variable")
		} else {
			s.T().Error("No provider_versions in metadata")
		}
	} else {
		s.T().Error("No metadata in deployment")
	}
	s.APIClient.DeleteDeployment(deploymentID)
}
