package helpers

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/tests/testconfig"
)

const (
	// DefaultAPITimeout is the default timeout for API requests
	DefaultAPITimeout = 2 * time.Minute
	// DefaultPollInterval is the default polling interval
	DefaultPollInterval = 5 * time.Second
	// DefaultHealthEndpoint is the default health check endpoint
	DefaultHealthEndpoint = "/api/v1/system/health"
	// DefaultAPIBaseURL is the default API base URL
	DefaultAPIBaseURL = "http://localhost:8084"

	// LocalStackEndpoint is the default LocalStack endpoint URL
	LocalStackEndpoint = testconfig.DefaultLocalStackURL
	// LocalStackRegion is the default AWS region for LocalStack
	LocalStackRegion = DefaultRegion
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
	defer func() { _ = resp.Body.Close() }() // Ignore error - body may already be closed

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
	defer func() { _ = resp.Body.Close() }() // Ignore error - body may already be closed

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
	defer func() { _ = resp.Body.Close() }() // Ignore error - body may already be closed

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
	defer func() { _ = resp.Body.Close() }() // Ignore error - body may already be closed

	require.Equal(c.t, http.StatusNoContent, resp.StatusCode)
}

// WaitForDeploymentStatus waits for a deployment to reach the specified status
func (c *APIClient) WaitForDeploymentStatus(deploymentID string, expectedStatus string, timeout time.Duration) {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	c.WaitForDeploymentStatusWithContext(ctx, deploymentID, expectedStatus)
}

// WaitForDeploymentStatusWithContext waits for a deployment to reach the specified status with context support
func (c *APIClient) WaitForDeploymentStatusWithContext(ctx context.Context, deploymentID string, expectedStatus string) {
	c.t.Helper()

	start := time.Now()
	attempt := 0

	for {
		// Check context before each attempt
		select {
		case <-ctx.Done():
			c.t.Errorf("Deployment did not reach status %s after %d attempts over %v: %v",
				expectedStatus, attempt, time.Since(start).Round(time.Second), ctx.Err())
			return
		default:
		}

		deployment := c.GetDeployment(deploymentID)

		status, ok := deployment["status"].(string)
		if !ok {
			c.t.Errorf("Deployment status not found")
			return
		}

		elapsed := time.Since(start)
		c.t.Logf("Deployment %s status: %s (attempt %d, elapsed: %v)", deploymentID, status, attempt+1, elapsed.Round(time.Second))

		if status == expectedStatus {
			c.t.Logf("Deployment %s reached status %s after %d attempts over %v", deploymentID, expectedStatus, attempt+1, elapsed.Round(time.Second))
			return
		}

		if status == "failed" && expectedStatus != "failed" {
			c.t.Errorf("Deployment failed unexpectedly after %d attempts: %v", attempt+1, deployment)
			return
		}

		// Exponential backoff: 1s, 2s, 4s, 8s, 16s, then cap at 30s
		backoff := time.Duration(1<<uint(attempt)) * time.Second // #nosec G115 - attempt is bounded by loop condition
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}

		c.t.Logf("Deployment %s waiting %v before next check (attempt %d, total elapsed: %v)", deploymentID, backoff, attempt+1, elapsed.Round(time.Second))

		select {
		case <-ctx.Done():
			c.t.Errorf("Deployment polling canceled during backoff: %v", ctx.Err())
			return
		case <-time.After(backoff):
		}

		attempt++
	}
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
	defer func() { _ = resp.Body.Close() }() // Ignore error - body may already be closed

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
	defer func() { _ = resp.Body.Close() }() // Ignore error - body may already be closed

	if resp.StatusCode != http.StatusOK {
		t.Skip("API server not healthy")
	}
}

// uniqueCounter is used to ensure uniqueness even with identical timestamps
var uniqueCounter int64

// UniqueName generates a unique name for test resources
// Uses a combination of timestamp, atomic counter, and random bytes for maximum uniqueness
func UniqueName(prefix string) string {
	// Get current time in microseconds for better precision than nanoseconds
	timestamp := time.Now().UnixMicro()

	// Atomic counter to handle rapid sequential calls
	counter := atomic.AddInt64(&uniqueCounter, 1)

	// Add some randomness to absolutely guarantee uniqueness
	randomBytes := make([]byte, 4)
	if _, err := rand.Read(randomBytes); err != nil {
		// Fallback to counter-only approach if random fails
		return fmt.Sprintf("%s-%d-%d", prefix, timestamp, counter)
	}

	randomHex := hex.EncodeToString(randomBytes)
	return fmt.Sprintf("%s-%d-%d-%s", prefix, timestamp, counter, randomHex)
}

// RawResponse wraps an HTTP response with JSON unmarshaling capability
type RawResponse struct {
	*http.Response
	BodyBytes []byte
}

// UnmarshalResponseJSON unmarshals the response body into the provided interface
func (r *RawResponse) UnmarshalResponseJSON(v interface{}) error {
	if err := json.Unmarshal(r.BodyBytes, v); err != nil {
		return fmt.Errorf("failed to unmarshal response JSON: %w", err)
	}
	return nil
}

// CreateDeploymentRaw creates a deployment and returns the raw HTTP response
func (c *APIClient) CreateDeploymentRaw(deploymentReq interface{}) *RawResponse {
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
	defer func() { _ = resp.Body.Close() }() // Ignore error - body may already be closed

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(c.t, err)

	return &RawResponse{
		Response:  resp,
		BodyBytes: respBody,
	}
}
