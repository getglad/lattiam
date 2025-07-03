package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/lattiam/lattiam/tests/helpers"
)

func TestMain(m *testing.M) {
	// Pre-download providers before running integration tests
	os.Exit(helpers.SetupProviders(m))
}

// waitForDeployment polls the deployment status until it reaches the expected state or times out
//
//nolint:gocognit,unused // polling loop with multiple conditions, utility function
func waitForDeployment(t *testing.T, deploymentID, expectedStatus string, timeout time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	apiServerURL := os.Getenv("LATTIAM_API_SERVER_URL")
	if apiServerURL == "" {
		t.Fatal("LATTIAM_API_SERVER_URL environment variable is not set. Please set it to the API server URL (e.g., http://localhost:8084).")
	}

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	client := &http.Client{Timeout: 10 * time.Second}

	for {
		select {
		case <-ctx.Done():
			t.Fatalf("Timeout waiting for deployment %s to reach status %s", deploymentID, expectedStatus)
		case <-ticker.C:
			req, err := http.NewRequestWithContext(ctx, http.MethodGet,
				fmt.Sprintf("%s/api/v1/deployments/%s", apiServerURL, deploymentID), nil)
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}

			resp, err := client.Do(req)
			if err != nil {
				continue // Retry on network errors
			}

			var result map[string]interface{}
			err = json.NewDecoder(resp.Body).Decode(&result)
			resp.Body.Close()
			if err != nil {
				continue
			}

			status, ok := result["status"].(string)
			if !ok {
				continue
			}

			if status == expectedStatus {
				return
			}

			if status == "failed" && expectedStatus != "failed" {
				t.Logf("Deployment %s failed - getting error details", deploymentID)
				if errorStr, exists := result["error"]; exists {
					t.Logf("Deployment error: %v", errorStr)
				}
				t.Fatalf("Deployment %s failed unexpectedly", deploymentID)
			}
		}
	}
}

// cleanupDeployment deletes a deployment
//
//nolint:unused // utility function for integration tests
func cleanupDeployment(t *testing.T, deploymentID string) {
	t.Helper()
	ctx := context.Background()
	apiServerURL := os.Getenv("LATTIAM_API_SERVER_URL")
	if apiServerURL == "" {
		t.Fatalf("LATTIAM_API_SERVER_URL environment variable is not set. Cannot cleanup deployment %s.", deploymentID)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		fmt.Sprintf("%s/api/v1/deployments/%s", apiServerURL, deploymentID), nil)
	if err != nil {
		t.Logf("Failed to create cleanup request: %v", err)
		return
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Logf("Failed to cleanup deployment: %v", err)
		return
	}
	resp.Body.Close()
}
