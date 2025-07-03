package integration_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/apiserver"
	"github.com/lattiam/lattiam/tests/helpers"
)

// TestStructuredErrorsInAPIResponse verifies that the API returns structured error information
//
//nolint:paralleltest // test uses shared resources
func TestStructuredErrorsInAPIResponse(t *testing.T) {
	// Start test server
	server := helpers.StartTestServer(t)
	defer server.Stop(t)
	// Note: Process cleanup is handled by server.Stop()

	// Track deployment for cleanup
	var deploymentID string
	defer func() {
		if deploymentID != "" {
			// Clean up deployment even if test fails
			client := helpers.NewAPIClient(t, server.URL)
			client.DeleteDeployment(deploymentID)
		}
	}()

	// Create a deployment that will fail with multiple errors
	// Using invalid S3 bucket configurations to trigger immediate errors
	deployment := map[string]interface{}{
		"name": "test-structured-errors",
		"terraform_json": map[string]interface{}{
			"terraform": map[string]interface{}{
				"required_providers": map[string]interface{}{
					"aws": map[string]interface{}{
						"source":  "hashicorp/aws",
						"version": "5.31.0",
					},
				},
			},
			"provider": map[string]interface{}{
				"aws": map[string]interface{}{
					"region": "us-east-1",
				},
			},
			"resource": map[string]interface{}{
				"aws_iam_role": map[string]interface{}{
					"test1": map[string]interface{}{
						// Missing required assume_role_policy
						// This should trigger a validation error
						"name": "test-role-1",
					},
					"test2": map[string]interface{}{
						// Invalid assume_role_policy JSON
						"name":               "test-role-2",
						"assume_role_policy": "this is not valid JSON",
					},
				},
			},
		},
	}

	// Create deployment
	body, _ := json.Marshal(deployment)
	resp, err := http.Post(server.URL+"/api/v1/deployments", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var createResp map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&createResp)
	require.NoError(t, err)
	resp.Body.Close()

	deploymentID = createResp["id"].(string)

	// Create API client for proper status polling
	client := helpers.NewAPIClient(t, server.URL)

	// Wait for deployment to fail or complete with proper timeout
	// Using 30 seconds timeout for LocalStack operations
	client.WaitForDeploymentStatus(deploymentID, "failed", 30*time.Second)

	// Get deployment status
	resp, err = http.Get(server.URL + "/api/v1/deployments/" + deploymentID)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var deploymentResp struct {
		ID             string                    `json:"id"`
		Status         string                    `json:"status"`
		ResourceErrors []apiserver.ResourceError `json:"resource_errors"`
	}
	err = json.NewDecoder(resp.Body).Decode(&deploymentResp)
	require.NoError(t, err)
	resp.Body.Close()

	// Verify the deployment failed (or at least has errors)
	if deploymentResp.Status != "failed" {
		// If still applying, that's okay as long as we have resource errors
		t.Logf("Deployment status: %s (expected failed, but will check for errors)", deploymentResp.Status)
	}

	// Verify we have structured errors
	assert.NotEmpty(t, deploymentResp.ResourceErrors)

	// Verify error details
	foundTest1Error := false
	foundTest2Error := false

	for _, resErr := range deploymentResp.ResourceErrors {
		switch resErr.Resource {
		case "aws_iam_role/test1":
			foundTest1Error = true
			assert.Equal(t, "aws_iam_role", resErr.Type)
			assert.Equal(t, "test1", resErr.Name)
			// Missing assume_role_policy should trigger a required attribute error
			assert.Contains(t, strings.ToLower(resErr.Error), "required")
			assert.Equal(t, "apply", resErr.Phase)
		case "aws_iam_role/test2":
			foundTest2Error = true
			assert.Equal(t, "aws_iam_role", resErr.Type)
			assert.Equal(t, "test2", resErr.Name)
			// Invalid JSON in assume_role_policy should trigger validation error
			assert.Contains(t, strings.ToLower(resErr.Error), "json")
			assert.Equal(t, "apply", resErr.Phase)
		}
	}

	assert.True(t, foundTest1Error, "Should have error for test1 IAM role")
	assert.True(t, foundTest2Error, "Should have error for test2 IAM role")
}

// TestDependencyResolutionError verifies that dependency resolution failures are properly reported
//
//nolint:paralleltest // test uses shared resources
func TestDependencyResolutionError(t *testing.T) {
	// Start test server
	server := helpers.StartTestServer(t)
	defer server.Stop(t)
	// Note: Process cleanup is handled by server.Stop()

	// Track deployment for cleanup
	var deploymentID string
	defer func() {
		if deploymentID != "" {
			// Clean up deployment even if test fails
			client := helpers.NewAPIClient(t, server.URL)
			client.DeleteDeployment(deploymentID)
		}
	}()

	// Create a deployment with unresolvable dependency
	deployment := map[string]interface{}{
		"name": "test-dependency-error",
		"terraform_json": map[string]interface{}{
			"terraform": map[string]interface{}{
				"required_providers": map[string]interface{}{
					"aws": map[string]interface{}{
						"source":  "hashicorp/aws",
						"version": "6.0.0",
					},
				},
			},
			"provider": map[string]interface{}{
				"aws": map[string]interface{}{
					"region": "us-east-1",
				},
			},
			"resource": map[string]interface{}{
				"aws_instance": map[string]interface{}{
					"web": map[string]interface{}{
						"ami":           "ami-12345678",
						"instance_type": "t2.micro",
						// Reference to non-existent resource
						"subnet_id": "${aws_subnet.nonexistent.id}",
						"timeouts":  map[string]interface{}{},
					},
				},
			},
		},
	}

	// Create deployment
	body, _ := json.Marshal(deployment)
	resp, err := http.Post(server.URL+"/api/v1/deployments", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	require.Equal(t, http.StatusCreated, resp.StatusCode)

	var createResp map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&createResp)
	require.NoError(t, err)
	resp.Body.Close()

	deploymentID = createResp["id"].(string)

	// Create API client for proper status polling
	client := helpers.NewAPIClient(t, server.URL)

	// Wait for deployment to fail or complete with proper timeout
	// Using 30 seconds timeout for LocalStack operations
	client.WaitForDeploymentStatus(deploymentID, "failed", 30*time.Second)

	// Get deployment status
	resp, err = http.Get(server.URL + "/api/v1/deployments/" + deploymentID)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var deploymentResp struct {
		ID             string                    `json:"id"`
		Status         string                    `json:"status"`
		ResourceErrors []apiserver.ResourceError `json:"resource_errors"`
	}
	err = json.NewDecoder(resp.Body).Decode(&deploymentResp)
	require.NoError(t, err)
	resp.Body.Close()

	// Verify the deployment failed (or at least has errors)
	if deploymentResp.Status != "failed" {
		// If still applying, that's okay as long as we have resource errors
		t.Logf("Deployment status: %s (expected failed, but will check for errors)", deploymentResp.Status)
	}

	// Verify we have structured errors
	assert.NotEmpty(t, deploymentResp.ResourceErrors)

	// Find the dependency error
	found := false
	for _, resErr := range deploymentResp.ResourceErrors {
		if resErr.Resource == "aws_instance/web" {
			found = true
			assert.Equal(t, "aws_instance", resErr.Type)
			assert.Equal(t, "web", resErr.Name)
			assert.Contains(t, resErr.Error, "dependency")
			assert.Equal(t, "dependency_resolution", resErr.Phase)
			break
		}
	}

	assert.True(t, found, "Should have dependency resolution error for aws_instance/web")
}
