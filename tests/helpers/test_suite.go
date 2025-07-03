package helpers

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/stretchr/testify/suite"
)

// BaseTestSuite provides common functionality for all test suites
type BaseTestSuite struct {
	suite.Suite
	APIClient        *APIClient
	DeploymentIDs    []string
	CleanupFunctions []func()
}

// SetupSuite runs before all tests in the suite
func (s *BaseTestSuite) SetupSuite() {
	// Initialize TestConfig
	tc := NewTestConfig()

	// Configure LocalStack environment if needed
	if tc.IsLocalStack() {
		tc.ApplyLocalStackEnvConfig(s.T())
	}

	// Start a new API server for the test suite
	testServer := StartTestServer(s.T())
	s.APIClient = NewAPIClient(s.T(), testServer.URL)
	s.AddCleanupFunc(func() {
		testServer.Stop(s.T())
	})

	// Ensure providers are available
	if err := EnsureAWSProvider(s.T()); err != nil {
		s.T().Fatalf("Failed to ensure AWS provider: %v", err)
	}
}

// TearDownSuite runs after all tests in the suite
func (s *BaseTestSuite) TearDownSuite() {
	// Clean up all tracked deployments
	s.CleanupDeployments()

	// Run any additional cleanup functions
	for _, cleanup := range s.CleanupFunctions {
		cleanup()
	}

	// Clean up state files
	s.CleanupStateFiles()
}

// CleanupStateFiles removes the terraform state directory
func (s *BaseTestSuite) CleanupStateFiles() {
	s.T().Log("Cleaning up state files...")
	homeDir, err := os.UserHomeDir()
	if err != nil {
		s.T().Logf("Failed to get home directory: %v", err)
		return
	}
	stateDir := filepath.Join(homeDir, ".lattiam", "state", "terraform")
	if err := os.RemoveAll(stateDir); err != nil {
		s.T().Logf("Failed to remove state directory %s: %v", stateDir, err)
	}
}

// SetupTest runs before each test
func (s *BaseTestSuite) SetupTest() {
	// Reset deployment tracking
	s.DeploymentIDs = []string{}
}

// TearDownTest runs after each test
func (s *BaseTestSuite) TearDownTest() {
	// Clean up test-specific deployments
	s.CleanupDeployments()
}

// TrackDeployment adds a deployment ID to be cleaned up
func (s *BaseTestSuite) TrackDeployment(deploymentID string) {
	s.DeploymentIDs = append(s.DeploymentIDs, deploymentID)
}

// CleanupDeployments deletes all tracked deployments
func (s *BaseTestSuite) CleanupDeployments() {
	for _, id := range s.DeploymentIDs {
		s.T().Logf("Cleaning up deployment: %s", id)
		s.APIClient.DeleteDeployment(id)
	}
	s.DeploymentIDs = []string{}
}

// AddCleanupFunc adds a cleanup function to be run during teardown
func (s *BaseTestSuite) AddCleanupFunc(fn func()) {
	s.CleanupFunctions = append(s.CleanupFunctions, fn)
}

// CreateAndTrackDeployment creates a deployment and tracks it for cleanup
func (s *BaseTestSuite) CreateAndTrackDeployment(req DeploymentRequest) map[string]interface{} {
	deployment := s.APIClient.CreateDeployment(req)

	// Extract deployment ID
	if id, ok := deployment["id"].(string); ok {
		s.TrackDeployment(id)
	} else {
		s.T().Fatal("Deployment response missing ID")
	}

	return deployment
}

// RequireDeploymentSuccess waits for a deployment to complete successfully
func (s *BaseTestSuite) RequireDeploymentSuccess(deploymentID string) {
	s.APIClient.WaitForDeploymentStatus(deploymentID, "completed", DefaultAPITimeout)
}

// RequireDeploymentFailure waits for a deployment to fail
func (s *BaseTestSuite) RequireDeploymentFailure(deploymentID string) {
	s.APIClient.WaitForDeploymentStatus(deploymentID, "failed", DefaultAPITimeout)
}

// GetResourceOutput extracts a resource output from deployment state
func (s *BaseTestSuite) GetResourceOutput(deployment map[string]interface{}, resourceType, resourceName, outputKey string) interface{} {
	state, ok := deployment["state"].(map[string]interface{})
	s.Require().True(ok, "Deployment missing state")

	resourceKey := fmt.Sprintf("%s.%s", resourceType, resourceName)
	resource, ok := state[resourceKey].(map[string]interface{})
	s.Require().True(ok, "Resource %s not found in state", resourceKey)

	value, ok := resource[outputKey]
	s.Require().True(ok, "Output %s not found in resource %s", outputKey, resourceKey)

	return value
}

// ParallelTestSuite extends BaseTestSuite with parallel test support
type ParallelTestSuite struct {
	BaseTestSuite
}

// SetupTest for parallel tests ensures unique resource names
func (p *ParallelTestSuite) SetupTest() {
	p.BaseTestSuite.SetupTest()
}

// GenerateUniquePrefix creates a unique prefix for parallel test resources
func (p *ParallelTestSuite) GenerateUniquePrefix() string {
	return fmt.Sprintf("parallel-test-%d", time.Now().UnixNano())
}
