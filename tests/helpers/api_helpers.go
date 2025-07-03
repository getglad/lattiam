package helpers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	// API configuration constants
	DefaultAPITimeout     = 2 * time.Minute
	DefaultPollInterval   = 5 * time.Second
	DefaultHealthEndpoint = "/api/v1/health"
	DefaultAPIBaseURL     = "http://localhost:8084"

	// LocalStack configuration
	LocalStackEndpoint = DefaultLocalStackURL
	LocalStackRegion   = DefaultRegion
)

// APIClient wraps HTTP client with common functionality
type APIClient struct {
	BaseURL    string
	HTTPClient *http.Client
	t          *testing.T
}

// NewAPIClient creates a new API client for testing
func NewAPIClient(t *testing.T, baseURL string) *APIClient {
	t.Helper()
	return &APIClient{
		BaseURL:    baseURL,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
		t:          t,
	}
}

// CreateDeployment creates a deployment and returns the response
func (c *APIClient) CreateDeployment(deploymentReq interface{}) map[string]interface{} {
	return c.CreateDeploymentWithStatus(deploymentReq, http.StatusCreated)
}

// CreateDeploymentWithStatus creates a deployment and returns the response, checking for a specific status code
func (c *APIClient) CreateDeploymentWithStatus(deploymentReq interface{}, expectedStatus int) map[string]interface{} {
	c.t.Helper()

	body, err := json.Marshal(deploymentReq)
	require.NoError(c.t, err)

	ctx, cancel := context.WithTimeout(context.Background(), DefaultAPITimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/v1/deployments", bytes.NewReader(body))
	require.NoError(c.t, err)

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	require.NoError(c.t, err)
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(c.t, err)

	require.Equal(c.t, expectedStatus, resp.StatusCode, "Failed to create deployment: %s", string(respBody))

	var result map[string]interface{}
	err = json.Unmarshal(respBody, &result)
	if err != nil && len(respBody) > 0 {
		// If there's an error but the body is not empty, it might be an error response
		// that is not a JSON object. We can return it as a string in the "error" field.
		return map[string]interface{}{"error": string(respBody)}
	}
	require.NoError(c.t, err)

	return result
}

// GetDeployment retrieves a deployment by ID
func (c *APIClient) GetDeployment(deploymentID string) map[string]interface{} {
	c.t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/v1/deployments/"+deploymentID, http.NoBody)
	require.NoError(c.t, err)

	resp, err := c.HTTPClient.Do(req)
	require.NoError(c.t, err)
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(c.t, err)

	require.Equal(c.t, http.StatusOK, resp.StatusCode, "Failed to get deployment: %s", string(respBody))

	var result map[string]interface{}
	err = json.Unmarshal(respBody, &result)
	require.NoError(c.t, err)

	return result
}

// UpdateDeployment updates an existing deployment
func (c *APIClient) UpdateDeployment(deploymentID string, updateReq interface{}) map[string]interface{} {
	c.t.Helper()

	body, err := json.Marshal(updateReq)
	require.NoError(c.t, err)

	ctx, cancel := context.WithTimeout(context.Background(), DefaultAPITimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.BaseURL+"/api/v1/deployments/"+deploymentID, bytes.NewReader(body))
	require.NoError(c.t, err)

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	require.NoError(c.t, err)
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(c.t, err)

	require.Equal(c.t, http.StatusOK, resp.StatusCode, "Failed to update deployment: %s", string(respBody))

	var result map[string]interface{}
	err = json.Unmarshal(respBody, &result)
	require.NoError(c.t, err)

	return result
}

// DeleteDeployment deletes a deployment
func (c *APIClient) DeleteDeployment(deploymentID string) {
	c.t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), DefaultAPITimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.BaseURL+"/api/v1/deployments/"+deploymentID, http.NoBody)
	require.NoError(c.t, err)

	resp, err := c.HTTPClient.Do(req)
	require.NoError(c.t, err)
	defer resp.Body.Close()

	require.Equal(c.t, http.StatusNoContent, resp.StatusCode)
}

// WaitForDeploymentStatus waits for a deployment to reach the specified status
func (c *APIClient) WaitForDeploymentStatus(deploymentID string, expectedStatus string, timeout time.Duration) {
	c.t.Helper()

	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		deployment := c.GetDeployment(deploymentID)

		status, ok := deployment["status"].(string)
		if !ok {
			c.t.Errorf("Deployment status not found")
			return
		}

		if status == expectedStatus {
			return
		}

		if status == "failed" && expectedStatus != "failed" {
			c.t.Errorf("Deployment failed unexpectedly: %v", deployment)
			return
		}

		time.Sleep(DefaultPollInterval)
	}

	c.t.Errorf("Deployment did not reach status %s within %v", expectedStatus, timeout)
}

// GetDeploymentStatus returns the current status of a deployment
func (c *APIClient) GetDeploymentStatus(deploymentID string) string {
	c.t.Helper()

	deployment := c.GetDeployment(deploymentID)
	status, ok := deployment["status"].(string)
	require.True(c.t, ok, "Deployment status not found")

	return status
}

// ListDeployments returns all deployments
func (c *APIClient) ListDeployments() []map[string]interface{} {
	c.t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/v1/deployments", http.NoBody)
	require.NoError(c.t, err)

	resp, err := c.HTTPClient.Do(req)
	require.NoError(c.t, err)
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(c.t, err)

	require.Equal(c.t, http.StatusOK, resp.StatusCode, "Failed to list deployments: %s", string(respBody))

	var result struct {
		Deployments []map[string]interface{} `json:"deployments"`
	}
	err = json.Unmarshal(respBody, &result)
	require.NoError(c.t, err)

	return result.Deployments
}

// CleanupDeployments deletes all deployments (useful for test cleanup)
func (c *APIClient) CleanupDeployments() {
	c.t.Helper()

	deployments := c.ListDeployments()
	for _, deployment := range deployments {
		if id, ok := deployment["id"].(string); ok {
			c.DeleteDeployment(id)
		}
	}
}

// PlanDeployment gets the plan for a deployment
func (c *APIClient) PlanDeployment(deploymentID string) map[string]interface{} {
	c.t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), DefaultAPITimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/v1/deployments/"+deploymentID+"/plan", http.NoBody)
	require.NoError(c.t, err)

	resp, err := c.HTTPClient.Do(req)
	require.NoError(c.t, err)
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(c.t, err)

	if resp.StatusCode != http.StatusOK {
		c.t.Logf("Plan request failed with status %d: %s", resp.StatusCode, string(respBody))
		return map[string]interface{}{"error": string(respBody)}
	}

	var result map[string]interface{}
	err = json.Unmarshal(respBody, &result)
	require.NoError(c.t, err)

	return result
}

// SkipIfAPIUnavailable skips the test if the API server is not available
func SkipIfAPIUnavailable(t *testing.T, baseURL ...string) {
	t.Helper()

	apiURL := DefaultAPIBaseURL
	if len(baseURL) > 0 {
		apiURL = baseURL[0]
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL+DefaultHealthEndpoint, http.NoBody)
	if err != nil {
		t.Skip("API server not available: failed to create request")
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Skip("API server not running")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Skip("API server not healthy")
	}
}

// UniqueName generates a unique name for test resources
func UniqueName(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}
