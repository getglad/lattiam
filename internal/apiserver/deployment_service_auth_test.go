//go:build integration
// +build integration

package apiserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/lattiam/lattiam/tests/helpers"
)

// DeploymentServiceAuthSuite tests deployment authentication and configuration
type DeploymentServiceAuthSuite struct {
	suite.Suite
	server       *APIServer
	service      *DeploymentService
	cleanupFuncs []func()
}

func TestDeploymentServiceAuthSuite(t *testing.T) {
	suite.Run(t, new(DeploymentServiceAuthSuite))
}

func (s *DeploymentServiceAuthSuite) SetupSuite() {
	helpers.MarkTestCategory(s.T(), helpers.CategoryIntegration, helpers.CategoryFast)

	// Create test server
	server, err := NewAPIServer(8087) // Use different port to avoid conflicts
	s.Require().NoError(err, "Failed to create API server")
	s.server = server

	// Create deployment service
	providerDir := s.T().TempDir()
	ds, err := NewDeploymentService(providerDir)
	s.Require().NoError(err, "Failed to create deployment service")
	s.service = ds

	s.T().Cleanup(func() {
		ds.Close()
	})
}

func (s *DeploymentServiceAuthSuite) TearDownSuite() {
	// Run any additional cleanup functions
	for _, cleanup := range s.cleanupFuncs {
		cleanup()
	}
}

func (s *DeploymentServiceAuthSuite) TestDeploymentWithAWSConfig() {
	tests := []struct {
		name           string
		deploymentJSON string
		wantStatus     int
		checkResponse  func(s *DeploymentServiceAuthSuite, resp *http.Response)
	}{
		{
			name: "deployment with AWS config",
			deploymentJSON: s.createDeploymentJSON(
				helpers.UniqueName("test-with-config"),
				&DeploymentConfig{
					AWSProfile: "test-profile",
					AWSRegion:  "us-west-2",
				},
				helpers.UniqueName("bucket"),
			),
			wantStatus: http.StatusCreated,
			checkResponse: func(s *DeploymentServiceAuthSuite, resp *http.Response) {
				var deployment DeploymentResponse
				err := json.NewDecoder(resp.Body).Decode(&deployment)
				s.Require().NoError(err)
				s.Assert().Contains(deployment.Name, "test-with-config")
				s.Assert().Equal(helpers.StatusPending, deployment.Status)
			},
		},
		{
			name: "deployment without config",
			deploymentJSON: s.createDeploymentJSON(
				helpers.UniqueName("test-no-config"),
				nil,
				helpers.UniqueName("bucket"),
			),
			wantStatus: http.StatusCreated,
			checkResponse: func(s *DeploymentServiceAuthSuite, resp *http.Response) {
				var deployment DeploymentResponse
				err := json.NewDecoder(resp.Body).Decode(&deployment)
				s.Require().NoError(err)
				s.Assert().Contains(deployment.Name, "test-no-config")
				s.Assert().Equal(helpers.StatusPending, deployment.Status)
			},
		},
		{
			name: "deployment with invalid JSON",
			deploymentJSON: `{
				"name": "test-invalid",
				"terraform_json": {invalid json}
			}`,
			wantStatus: http.StatusBadRequest,
			checkResponse: func(s *DeploymentServiceAuthSuite, resp *http.Response) {
				var errResp ErrorResponse
				err := json.NewDecoder(resp.Body).Decode(&errResp)
				s.Require().NoError(err)
				s.Assert().Equal("invalid_json", errResp.Error)
			},
		},
		{
			name:           "deployment missing name",
			deploymentJSON: s.createDeploymentJSONWithoutName(helpers.UniqueName("bucket")),
			wantStatus:     http.StatusBadRequest,
			checkResponse: func(s *DeploymentServiceAuthSuite, resp *http.Response) {
				var errResp ErrorResponse
				err := json.NewDecoder(resp.Body).Decode(&errResp)
				s.Require().NoError(err)
				s.Assert().Equal("missing_name", errResp.Error)
			},
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			resp := s.makeHTTPRequest(http.MethodPost, "/api/v1/deployments", tt.deploymentJSON)
			defer resp.Body.Close()

			s.Assert().Equal(tt.wantStatus, resp.StatusCode)
			if tt.checkResponse != nil {
				tt.checkResponse(s, resp)
			}
		})
	}
}

func (s *DeploymentServiceAuthSuite) TestDeploymentConfigPropagation() {
	// Create unique test data
	uniqueSuffix := helpers.UniqueName("config")
	deploymentName := "test-config-propagation-" + uniqueSuffix
	bucketName := "test-config-bucket-" + uniqueSuffix

	config := &DeploymentConfig{
		AWSProfile: "test-profile",
		AWSRegion:  "eu-west-1",
	}

	deployment, err := s.service.CreateDeployment(
		context.Background(),
		deploymentName,
		map[string]interface{}{
			"resource": map[string]interface{}{
				helpers.ResourceTypeS3Bucket: map[string]interface{}{
					"test": map[string]interface{}{
						"bucket": bucketName,
					},
				},
			},
		},
		config,
	)
	s.Require().NoError(err)

	// Verify config was stored with deployment
	s.Assert().NotNil(deployment.Config)
	s.Assert().Equal("test-profile", deployment.Config.AWSProfile)
	s.Assert().Equal("eu-west-1", deployment.Config.AWSRegion)

	// Verify config persists when retrieving deployment
	retrieved, err := s.service.GetDeployment(deployment.ID)
	s.Require().NoError(err)
	s.Assert().NotNil(retrieved.Config)
	s.Assert().Equal("test-profile", retrieved.Config.AWSProfile)
	s.Assert().Equal("eu-west-1", retrieved.Config.AWSRegion)
}

func (s *DeploymentServiceAuthSuite) TestMultipleDeploymentsWithDifferentConfigs() {
	// Create unique test data
	uniqueSuffix := helpers.UniqueName("multi")

	// Create first deployment with config
	deployment1, err := s.service.CreateDeployment(
		context.Background(),
		"deployment-1-"+uniqueSuffix,
		map[string]interface{}{
			"resource": map[string]interface{}{
				helpers.ResourceTypeS3Bucket: map[string]interface{}{
					"bucket1": map[string]interface{}{
						"bucket": "test-bucket-1-" + uniqueSuffix,
					},
				},
			},
		},
		&DeploymentConfig{
			AWSProfile: "profile-1",
			AWSRegion:  "us-east-1",
		},
	)
	s.Require().NoError(err)

	// Create second deployment with different config
	deployment2, err := s.service.CreateDeployment(
		context.Background(),
		"deployment-2-"+uniqueSuffix,
		map[string]interface{}{
			"resource": map[string]interface{}{
				helpers.ResourceTypeS3Bucket: map[string]interface{}{
					"bucket2": map[string]interface{}{
						"bucket": "test-bucket-2-" + uniqueSuffix,
					},
				},
			},
		},
		&DeploymentConfig{
			AWSProfile: "profile-2",
			AWSRegion:  "us-west-2",
		},
	)
	s.Require().NoError(err)

	// Verify each deployment has its own config
	retrieved1, err := s.service.GetDeployment(deployment1.ID)
	s.Require().NoError(err)
	s.Assert().Equal("profile-1", retrieved1.Config.AWSProfile)
	s.Assert().Equal("us-east-1", retrieved1.Config.AWSRegion)

	retrieved2, err := s.service.GetDeployment(deployment2.ID)
	s.Require().NoError(err)
	s.Assert().Equal("profile-2", retrieved2.Config.AWSProfile)
	s.Assert().Equal("us-west-2", retrieved2.Config.AWSRegion)
}

func (s *DeploymentServiceAuthSuite) TestAuthenticationErrorDetection() {
	// Test the contains function for authentication error detection
	authErrors := []string{
		"no valid credential sources found",
		"Invalid security token",
		"Access denied",
		"Authentication failed",
		"credentials not found",
		"AUTHENTICATION ERROR",
	}

	for _, errMsg := range authErrors {
		s.Assert().True(contains(errMsg, "credential") ||
			contains(errMsg, "authentication") ||
			contains(errMsg, "access denied") ||
			contains(errMsg, "invalid security token"),
			"Should detect auth error in: %s", errMsg)
	}

	// Non-auth errors should not be detected
	nonAuthErrors := []string{
		"network timeout",
		"connection refused",
		"invalid resource type",
	}

	for _, errMsg := range nonAuthErrors {
		s.Assert().False(contains(errMsg, "credentials") &&
			contains(errMsg, "authentication") &&
			contains(errMsg, "access denied") &&
			contains(errMsg, "invalid security token"),
			"Should not detect auth error in: %s", errMsg)
	}
}

// Helper methods

func (s *DeploymentServiceAuthSuite) makeHTTPRequest(method, path, body string) *http.Response {
	req := s.createHTTPRequest(method, path, body)
	w := s.executeHTTPRequest(req)
	return w.Result()
}

func (s *DeploymentServiceAuthSuite) createHTTPRequest(method, path, body string) *http.Request {
	var bodyReader *strings.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}

	req, err := http.NewRequest(method, path, bodyReader)
	s.Require().NoError(err)

	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	return req
}

func (s *DeploymentServiceAuthSuite) executeHTTPRequest(req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	s.server.router.ServeHTTP(w, req)
	return w
}

func (s *DeploymentServiceAuthSuite) createDeploymentJSON(name string, config *DeploymentConfig, bucketName string) string {
	deployment := map[string]interface{}{
		"name": name,
		"terraform_json": map[string]interface{}{
			"resource": map[string]interface{}{
				helpers.ResourceTypeS3Bucket: map[string]interface{}{
					"test": map[string]interface{}{
						"bucket": bucketName,
					},
				},
			},
		},
	}

	if config != nil {
		deployment["config"] = map[string]interface{}{
			"aws_profile": config.AWSProfile,
			"aws_region":  config.AWSRegion,
		}
	}

	jsonBytes, err := json.Marshal(deployment)
	s.Require().NoError(err)
	return string(jsonBytes)
}

func (s *DeploymentServiceAuthSuite) createDeploymentJSONWithoutName(bucketName string) string {
	deployment := map[string]interface{}{
		"terraform_json": map[string]interface{}{
			"resource": map[string]interface{}{
				helpers.ResourceTypeS3Bucket: map[string]interface{}{
					"test": map[string]interface{}{
						"bucket": bucketName,
					},
				},
			},
		},
	}

	jsonBytes, err := json.Marshal(deployment)
	s.Require().NoError(err)
	return string(jsonBytes)
}

// contains checks if a string contains a substring (case-insensitive)
func contains(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}
