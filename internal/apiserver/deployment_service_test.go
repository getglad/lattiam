//go:build integration
// +build integration

package apiserver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/lattiam/lattiam/tests/helpers"
)

// DeploymentServiceSuite tests core deployment service functionality
type DeploymentServiceSuite struct {
	suite.Suite
	service      *DeploymentService
	cleanupFuncs []func()
}

func TestDeploymentServiceSuite(t *testing.T) {
	suite.Run(t, new(DeploymentServiceSuite))
}

func (s *DeploymentServiceSuite) SetupSuite() {
	helpers.MarkTestCategory(s.T(), helpers.CategoryIntegration, helpers.CategoryFast)
}

func (s *DeploymentServiceSuite) SetupTest() {
	// Set up temp state directory for each test
	tempDir := s.T().TempDir()
	s.T().Setenv("LATTIAM_STATE_DIR", tempDir)

	// Use same provider dir as app for integration tests
	homeDir, _ := os.UserHomeDir()
	providerDir := filepath.Join(homeDir, ".lattiam", "providers")
	ds, err := NewDeploymentService(providerDir)
	s.Require().NoError(err, "Failed to create deployment service")
	s.service = ds
}

func (s *DeploymentServiceSuite) TearDownTest() {
	if s.service != nil {
		s.service.Close()
	}

	// Run any additional cleanup functions
	for _, cleanup := range s.cleanupFuncs {
		cleanup()
	}
}

func (s *DeploymentServiceSuite) TestMultipleDeploymentsConcurrency() {
	// Tests that multiple deployments can run successfully
	// This test catches the provider instance caching issue where subsequent deployments
	// get stuck because they're trying to use a stopped provider instance

	ctx := context.Background()
	uniqueSuffix := helpers.UniqueName("concurrency")

	// Create first deployment with unique names
	firstRoleName := fmt.Sprintf("test-role-deployment-1-%s", uniqueSuffix)
	deployment1, err := s.service.CreateDeployment(ctx, "test-role-1-"+uniqueSuffix, map[string]interface{}{
		"resource": map[string]interface{}{
			"aws_iam_role": map[string]interface{}{
				"test1-" + uniqueSuffix: map[string]interface{}{
					"name": firstRoleName,
					"assume_role_policy": `{"Version":"2012-10-17","Statement":[` +
						`{"Effect":"Allow","Principal":{"Service":"lambda.amazonaws.com"},"Action":"sts:AssumeRole"}]}`,
					"path": "/",
				},
			},
		},
	}, nil)
	s.Require().NoError(err, "Failed to create first deployment")
	s.Assert().Equal(StatusPending, deployment1.Status)

	// Wait for first deployment to complete or fail
	completedDep1 := s.waitForDeploymentCompletion(deployment1.ID, 5*time.Second)
	s.T().Logf("First deployment finished with status: %s", completedDep1.Status)
	if completedDep1.Error != "" {
		s.T().Logf("First deployment error: %s", completedDep1.Error)
	}

	// Create second deployment immediately after first completes
	secondRoleName := fmt.Sprintf("test-role-deployment-2-%s", uniqueSuffix)
	deployment2, err := s.service.CreateDeployment(ctx, "test-role-2-"+uniqueSuffix, map[string]interface{}{
		"resource": map[string]interface{}{
			"aws_iam_role": map[string]interface{}{
				"test2-" + uniqueSuffix: map[string]interface{}{
					"name": secondRoleName,
					"assume_role_policy": `{"Version":"2012-10-17","Statement":[` +
						`{"Effect":"Allow","Principal":{"Service":"ec2.amazonaws.com"},"Action":"sts:AssumeRole"}]}`,
					"path": "/",
				},
			},
		},
	}, nil)
	s.Require().NoError(err, "Failed to create second deployment")

	// Wait for second deployment to progress beyond "planning"
	// If it gets stuck in planning, it's likely due to provider caching issue
	stuck := s.checkDeploymentProgress(deployment2.ID, 15*time.Second)

	// This assertion will fail if provider caching issue exists
	s.Assert().False(stuck, "Second deployment appears stuck in planning phase - likely provider caching issue")

	// Verify both deployments eventually complete or fail (not stuck)
	allDeployments := s.service.ListDeployments()
	for _, d := range allDeployments {
		if d.ID == deployment1.ID || d.ID == deployment2.ID {
			// Neither deployment should be stuck in pending/planning after reasonable time
			s.Assert().NotEqual(StatusPending, d.Status, "Deployment %s stuck in pending", d.ID)
			s.Assert().NotEqual(StatusPlanning, d.Status, "Deployment %s stuck in planning", d.ID)
		}
	}
}

func (s *DeploymentServiceSuite) TestProviderInstanceReuse() {
	// Tests that provider instances are properly managed
	// when multiple deployments use the same provider

	ctx := context.Background()
	uniqueSuffix := helpers.UniqueName("provider-reuse")

	// Create three deployments in rapid succession
	// They should all complete successfully, not get stuck waiting for dead provider instances
	deploymentConfigs := []struct {
		name string
		role string
	}{
		{"deployment-1-" + uniqueSuffix, fmt.Sprintf("test-role-alpha-%s", uniqueSuffix)},
		{"deployment-2-" + uniqueSuffix, fmt.Sprintf("test-role-beta-%s", uniqueSuffix)},
		{"deployment-3-" + uniqueSuffix, fmt.Sprintf("test-role-gamma-%s", uniqueSuffix)},
	}

	deploymentIDs := make([]string, 0, len(deploymentConfigs))
	deployments := make([]*Deployment, 0, len(deploymentConfigs))

	for i, config := range deploymentConfigs {
		s.T().Logf("Creating deployment %s with role %s", config.name, config.role)
		deployment, err := s.service.CreateDeployment(ctx, config.name, map[string]interface{}{
			"resource": map[string]interface{}{
				"aws_iam_role": map[string]interface{}{
					fmt.Sprintf("test-%d-%s", i+1, uniqueSuffix): map[string]interface{}{
						"name": config.role,
						"assume_role_policy": `{"Version":"2012-10-17","Statement":[` +
							`{"Effect":"Allow","Principal":{"Service":"lambda.amazonaws.com"},"Action":"sts:AssumeRole"}]}`,
						"path": "/",
					},
				},
			},
		}, nil)
		s.Require().NoError(err, "Failed to create deployment %s", config.name)
		s.T().Logf("Created deployment ID %s for %s", deployment.ID, config.name)
		deploymentIDs = append(deploymentIDs, deployment.ID)
		deployments = append(deployments, deployment)

		// Small delay between deployments to simulate real usage
		time.Sleep(100 * time.Millisecond)
	}

	// Poll for deployments to progress beyond planning
	stuckCount := s.waitForMultipleDeploymentProgress(deploymentIDs, 5*time.Second)

	for i, id := range deploymentIDs {
		d, err := s.service.GetDeployment(id)
		s.Require().NoError(err)

		if d.Status == StatusPending || d.Status == StatusPlanning {
			s.T().Logf("Deployment %d (%s) is stuck in %s status", i+1, deployments[i].Name, d.Status)
		} else {
			s.T().Logf("Deployment %d (%s) progressed to %s status", i+1, deployments[i].Name, d.Status)
		}
	}

	// If provider caching is broken, we expect deployments 2 and 3 to be stuck
	s.Assert().Equal(0, stuckCount,
		"Expected 0 stuck deployments, but found %d - provider instance reuse issue", stuckCount)
}

// Helper methods

func (s *DeploymentServiceSuite) waitForDeploymentCompletion(deploymentID string, maxWait time.Duration) *Deployment {
	pollInterval := 100 * time.Millisecond
	start := time.Now()

	for time.Since(start) < maxWait {
		d, err := s.service.GetDeployment(deploymentID)
		s.Require().NoError(err)

		if d.Status == StatusCompleted || d.Status == StatusFailed {
			return d
		}
		time.Sleep(pollInterval)
	}

	// Return final state even if not completed
	d, err := s.service.GetDeployment(deploymentID)
	s.Require().NoError(err)
	return d
}

func (s *DeploymentServiceSuite) checkDeploymentProgress(deploymentID string, timeout time.Duration) bool {
	// Returns true if deployment is stuck (didn't progress beyond planning)
	progressCheckStart := time.Now()

	for time.Since(progressCheckStart) < timeout {
		d, err := s.service.GetDeployment(deploymentID)
		s.Require().NoError(err)

		// If status progresses beyond planning, it's not stuck
		if d.Status != StatusPending && d.Status != StatusPlanning {
			s.T().Logf("Deployment progressed to: %s", d.Status)
			return false // Not stuck
		}
		time.Sleep(500 * time.Millisecond)
	}

	return true // Stuck
}

func (s *DeploymentServiceSuite) waitForMultipleDeploymentProgress(deploymentIDs []string, maxWait time.Duration) int {
	// Returns count of stuck deployments
	pollInterval := 100 * time.Millisecond
	start := time.Now()

	for time.Since(start) < maxWait {
		allProgressed := true
		for _, id := range deploymentIDs {
			d, err := s.service.GetDeployment(id)
			s.Require().NoError(err)
			if d.Status == StatusPending || d.Status == StatusPlanning {
				allProgressed = false
				break
			}
		}
		if allProgressed {
			break
		}
		time.Sleep(pollInterval)
	}

	// Count stuck deployments
	stuckCount := 0
	for _, id := range deploymentIDs {
		d, err := s.service.GetDeployment(id)
		s.Require().NoError(err)

		if d.Status == StatusPending || d.Status == StatusPlanning {
			stuckCount++
		}
	}

	return stuckCount
}
