package embedded

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/internal/mocks"
)

const (
	methodCreateDeployment       = "CreateDeployment"
	methodUpdateDeploymentStatus = "UpdateDeploymentStatus"
)

func TestTracker_serializeDeployment(t *testing.T) { //nolint:funlen // Test function with comprehensive test cases
	t.Parallel()

	tracker := NewTracker()

	t.Run("SerializeDeploymentWithoutResult", func(t *testing.T) {
		t.Parallel()

		deployment := &interfaces.QueuedDeployment{
			ID:        "test-dep-1",
			RequestID: "req-123",
			Status:    interfaces.DeploymentStatusQueued,
			CreatedAt: time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
			Request: &interfaces.DeploymentRequest{
				Resources: []interfaces.Resource{
					{
						Name:       "test-resource",
						Type:       "aws_instance",
						Properties: map[string]interface{}{"instance_type": "t2.micro"},
					},
				},
				Options: interfaces.DeploymentOptions{
					DryRun: false,
				},
			},
			RetryCount: 0,
		}

		data, err := tracker.serializeDeployment(deployment, nil)
		require.NoError(t, err)
		assert.NotNil(t, data)

		// Verify deployment fields are present
		assert.Equal(t, "test-dep-1", data["id"])
		assert.Equal(t, "req-123", data["request_id"])
		assert.Equal(t, "queued", data["status"])
		assert.NotNil(t, data["request"])
		// Check retry_count is 0 (can't use InEpsilon with expected value of 0)
		retryCount, ok := data["retry_count"].(float64)
		assert.True(t, ok, "retry_count should be a float64")
		assert.Zero(t, retryCount)

		// Verify result is not present
		assert.Nil(t, data["result"])
	})

	t.Run("SerializeDeploymentWithResult", func(t *testing.T) {
		t.Parallel()

		deployment := &interfaces.QueuedDeployment{
			ID:        "test-dep-2",
			RequestID: "req-456",
			Status:    interfaces.DeploymentStatusCompleted,
			CreatedAt: time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
			Request: &interfaces.DeploymentRequest{
				Resources: []interfaces.Resource{
					{
						Name:       "test-resource",
						Type:       "aws_instance",
						Properties: map[string]interface{}{"instance_type": "t2.micro"},
					},
				},
				Options: interfaces.DeploymentOptions{
					DryRun: false,
				},
			},
			RetryCount: 0,
		}

		completedTime := time.Date(2023, 1, 1, 12, 30, 0, 0, time.UTC)
		result := &interfaces.DeploymentResult{
			DeploymentID: "test-dep-2",
			Success:      true,
			Resources: map[string]interface{}{
				"test-resource": map[string]interface{}{
					"id":            "i-1234567890abcdef0",
					"instance_type": "t2.micro",
					"state":         "running",
				},
			},
			Outputs: map[string]interface{}{
				"instance_ip": "192.168.1.100",
			},
			CompletedAt: completedTime,
		}

		data, err := tracker.serializeDeployment(deployment, result)
		require.NoError(t, err)
		assert.NotNil(t, data)

		// Verify deployment fields are present
		assert.Equal(t, "test-dep-2", data["id"])
		assert.Equal(t, "req-456", data["request_id"])
		assert.Equal(t, "completed", data["status"])

		// Verify result is present and properly structured
		assert.NotNil(t, data["result"])
		resultData, ok := data["result"].(map[string]interface{})
		require.True(t, ok, "result should be a map")
		assert.Equal(t, "test-dep-2", resultData["deployment_id"])
		assert.Equal(t, true, resultData["success"])
		assert.NotNil(t, resultData["resources"])
		assert.NotNil(t, resultData["outputs"])
	})

	t.Run("SerializeNilDeployment", func(t *testing.T) {
		t.Parallel()

		data, err := tracker.serializeDeployment(nil, nil)
		require.NoError(t, err) // json.Marshal(nil) succeeds, creates null
		assert.NotNil(t, data)
		// The embedded deployment should be null, but the map should exist
		assert.Nil(t, data["id"]) // Because the embedded deployment is nil
	})
}

func TestTracker_deserializeDeployment(t *testing.T) { //nolint:funlen // Test function with comprehensive test cases
	t.Parallel()

	tracker := NewTracker()

	t.Run("DeserializeValidDeployment", func(t *testing.T) {
		t.Parallel()

		data := map[string]interface{}{
			"id":         "test-dep-1",
			"request_id": "req-123",
			"status":     "queued",
			"created_at": "2023-01-01T12:00:00Z",
			"request": map[string]interface{}{
				"resources": []interface{}{
					map[string]interface{}{
						"name":       "test-resource",
						"type":       "aws_instance",
						"properties": map[string]interface{}{"instance_type": "t2.micro"},
					},
				},
				"options": map[string]interface{}{
					"dry_run": false,
				},
			},
			"retry_count": 0,
		}

		deployment, err := tracker.deserializeDeployment(data)
		require.NoError(t, err)
		assert.NotNil(t, deployment)

		assert.Equal(t, "test-dep-1", deployment.ID)
		assert.Equal(t, "req-123", deployment.RequestID)
		assert.Equal(t, interfaces.DeploymentStatusQueued, deployment.Status)
		assert.Equal(t, 0, deployment.RetryCount)
		assert.NotNil(t, deployment.Request)
		assert.Len(t, deployment.Request.Resources, 1)
		assert.Equal(t, "test-resource", deployment.Request.Resources[0].Name)
	})

	t.Run("DeserializeDeploymentWithTimestamps", func(t *testing.T) {
		t.Parallel()

		data := map[string]interface{}{
			"id":           "test-dep-2",
			"status":       "processing",
			"created_at":   "2023-01-01T12:00:00Z",
			"started_at":   "2023-01-01T12:05:00Z",
			"completed_at": "2023-01-01T12:30:00Z",
			"request": map[string]interface{}{
				"resources": []interface{}{
					map[string]interface{}{
						"name":       "test-resource",
						"type":       "aws_instance",
						"properties": map[string]interface{}{},
					},
				},
				"options": map[string]interface{}{
					"dry_run": false,
				},
			},
			"retry_count": 1,
		}

		deployment, err := tracker.deserializeDeployment(data)
		require.NoError(t, err)
		assert.NotNil(t, deployment)

		assert.Equal(t, "test-dep-2", deployment.ID)
		assert.Equal(t, interfaces.DeploymentStatusProcessing, deployment.Status)
		assert.Equal(t, 1, deployment.RetryCount)
		assert.NotNil(t, deployment.StartedAt)
		assert.NotNil(t, deployment.CompletedAt)

		expectedTime := time.Date(2023, 1, 1, 12, 5, 0, 0, time.UTC)
		assert.True(t, deployment.StartedAt.Equal(expectedTime))
	})

	t.Run("DeserializeEmptyData", func(t *testing.T) {
		t.Parallel()

		data := map[string]interface{}{}

		deployment, err := tracker.deserializeDeployment(data)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no deployment data found in persistence")
		assert.Nil(t, deployment)
	})

	t.Run("DeserializeInvalidData", func(t *testing.T) {
		t.Parallel()

		data := map[string]interface{}{
			"id":         "test-dep-invalid",
			"status":     "invalid-status", // Invalid enum value
			"created_at": "invalid-time",   // Invalid time format
		}

		_, err := tracker.deserializeDeployment(data)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to unmarshal deployment")
	})
}

func TestTracker_deserializeResult(t *testing.T) { //nolint:funlen // Test function with comprehensive test cases
	t.Parallel()

	tracker := NewTracker()

	t.Run("DeserializeNilResult", func(t *testing.T) {
		t.Parallel()

		result, err := tracker.deserializeResult(nil)
		require.NoError(t, err)
		assert.Nil(t, result)
	})

	t.Run("DeserializeResultPointer", func(t *testing.T) {
		t.Parallel()

		originalResult := &interfaces.DeploymentResult{
			DeploymentID: "test-dep-1",
			Success:      true,
			Resources: map[string]interface{}{
				"resource1": map[string]interface{}{"state": "created"},
			},
			CompletedAt: time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
		}

		result, err := tracker.deserializeResult(originalResult)
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, "test-dep-1", result.DeploymentID)
		assert.True(t, result.Success)
		assert.Equal(t, originalResult.Resources, result.Resources)
	})

	t.Run("DeserializeResultValue", func(t *testing.T) {
		t.Parallel()

		originalResult := interfaces.DeploymentResult{
			DeploymentID: "test-dep-2",
			Success:      false,
			Error:        errors.New("deployment failed"),
			CompletedAt:  time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC),
		}

		result, err := tracker.deserializeResult(originalResult)
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, "test-dep-2", result.DeploymentID)
		assert.False(t, result.Success)
		assert.Error(t, result.Error)
	})

	t.Run("DeserializeResultFromMap", func(t *testing.T) {
		t.Parallel()

		resultMap := map[string]interface{}{
			"deployment_id": "test-dep-3",
			"success":       true,
			"resources": map[string]interface{}{
				"resource1": map[string]interface{}{
					"id":    "res-123",
					"state": "active",
				},
			},
			"outputs": map[string]interface{}{
				"output1": "value1",
				"output2": 42,
			},
			"completed_at": "2023-01-01T12:00:00Z",
		}

		result, err := tracker.deserializeResult(resultMap)
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, "test-dep-3", result.DeploymentID)
		assert.True(t, result.Success)
		assert.NotNil(t, result.Resources)
		assert.NotNil(t, result.Outputs)
		assert.Equal(t, "value1", result.Outputs["output1"])
		assert.InEpsilon(t, 42.0, result.Outputs["output2"], 0.001) // JSON numbers become float64
	})

	t.Run("DeserializeResultFromUnknownType", func(t *testing.T) {
		t.Parallel()

		// Test with an integer (unknown type that should still work via JSON marshaling)
		intValue := 42

		result, err := tracker.deserializeResult(intValue)
		require.Error(t, err) // This should fail because an integer can't be unmarshaled as DeploymentResult
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "failed to unmarshal result")
	})

	t.Run("DeserializeInvalidResultData", func(t *testing.T) {
		t.Parallel()

		invalidData := map[string]interface{}{
			"deployment_id": "test-dep-invalid",
			"success":       "not-a-boolean", // Invalid boolean value
			"completed_at":  "invalid-time",  // Invalid time format
		}

		_, err := tracker.deserializeResult(invalidData)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to unmarshal result")
	})

	t.Run("DeserializeResultWithComplexTypes", func(t *testing.T) {
		t.Parallel()

		complexData := map[string]interface{}{
			"deployment_id": "test-dep-complex",
			"success":       true,
			"resources": map[string]interface{}{
				"nested_resource": map[string]interface{}{
					"metadata": map[string]interface{}{
						"tags": []interface{}{"tag1", "tag2"},
						"config": map[string]interface{}{
							"setting1": true,
							"setting2": 123.45,
						},
					},
				},
			},
			"completed_at": "2023-01-01T12:00:00Z",
		}

		result, err := tracker.deserializeResult(complexData)
		require.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, "test-dep-complex", result.DeploymentID)

		// Verify complex nested structure is preserved
		nestedResource, ok := result.Resources["nested_resource"].(map[string]interface{})
		require.True(t, ok)
		metadata, ok := nestedResource["metadata"].(map[string]interface{})
		require.True(t, ok)
		tags, ok := metadata["tags"].([]interface{})
		require.True(t, ok)
		assert.Len(t, tags, 2)
	})
}

//nolint:gocognit,gocyclo // Comprehensive test with multiple scenarios
func TestTracker_PersistenceCalls(t *testing.T) { //nolint:funlen // Test function with comprehensive test cases
	t.Parallel()

	t.Run("RegisterCallsPersistence", func(t *testing.T) {
		t.Parallel()

		mockStore := mocks.NewMockStateStore()
		tracker := NewTracker()
		err := tracker.Load(mockStore)
		require.NoError(t, err)

		deployment := &interfaces.QueuedDeployment{
			ID:        "test-dep-1",
			Status:    interfaces.DeploymentStatusQueued,
			CreatedAt: time.Now(),
			Request: &interfaces.DeploymentRequest{
				Resources: []interfaces.Resource{
					{
						Name:       "test-resource",
						Type:       "aws_instance",
						Properties: map[string]interface{}{},
					},
				},
				Options: interfaces.DeploymentOptions{
					DryRun: false,
				},
			},
		}

		err = tracker.Register(deployment)
		require.NoError(t, err)

		// Verify persistence was called
		calls := mockStore.GetCalls()
		found := false
		for _, call := range calls {
			if call.Method == methodCreateDeployment && len(call.Args) >= 2 {
				if deploymentID, ok := call.Args[0].(string); ok && deploymentID == "test-dep-1" {
					found = true
					break
				}
			}
		}
		assert.True(t, found, methodCreateDeployment+" should be called during Register")
	})

	t.Run("SetStatusCallsPersistence", func(t *testing.T) {
		t.Parallel()

		mockStore := mocks.NewMockStateStore()
		tracker := NewTracker()
		err := tracker.Load(mockStore)
		require.NoError(t, err)

		deployment := &interfaces.QueuedDeployment{
			ID:        "test-dep-2",
			Status:    interfaces.DeploymentStatusQueued,
			CreatedAt: time.Now(),
			Request: &interfaces.DeploymentRequest{
				Resources: []interfaces.Resource{
					{
						Name:       "test-resource",
						Type:       "aws_instance",
						Properties: map[string]interface{}{},
					},
				},
				Options: interfaces.DeploymentOptions{
					DryRun: false,
				},
			},
		}

		err = tracker.Register(deployment)
		require.NoError(t, err)

		err = tracker.SetStatus("test-dep-2", interfaces.DeploymentStatusProcessing)
		require.NoError(t, err)

		// Verify persistence was called - count persistence calls for this deployment
		calls := mockStore.GetCalls()
		persistenceCount := 0
		for _, call := range calls {
			if (call.Method == methodCreateDeployment || call.Method == methodUpdateDeploymentStatus) && len(call.Args) > 0 {
				if call.Method == methodCreateDeployment { //nolint:staticcheck // String comparison preferred over switch for test clarity
					if deploymentID, ok := call.Args[0].(string); ok && deploymentID == "test-dep-2" {
						persistenceCount++
					}
				} else if call.Method == methodUpdateDeploymentStatus {
					if deploymentID, ok := call.Args[0].(string); ok && deploymentID == "test-dep-2" {
						persistenceCount++
					}
				}
			}
		}
		// Should have at least 2 calls: one from Register, one from SetStatus
		assert.GreaterOrEqual(t, persistenceCount, 2, "Persistence methods should be called at least twice (Register + SetStatus)")
	})

	t.Run("SetResultCallsPersistence", func(t *testing.T) {
		t.Parallel()

		mockStore := mocks.NewMockStateStore()
		tracker := NewTracker()
		err := tracker.Load(mockStore)
		require.NoError(t, err)

		deployment := &interfaces.QueuedDeployment{
			ID:        "test-dep-3",
			Status:    interfaces.DeploymentStatusQueued,
			CreatedAt: time.Now(),
			Request: &interfaces.DeploymentRequest{
				Resources: []interfaces.Resource{
					{
						Name:       "test-resource",
						Type:       "aws_instance",
						Properties: map[string]interface{}{},
					},
				},
				Options: interfaces.DeploymentOptions{
					DryRun: false,
				},
			},
		}

		err = tracker.Register(deployment)
		require.NoError(t, err)

		result := &interfaces.DeploymentResult{
			DeploymentID: "test-dep-3",
			Success:      true,
			Resources: map[string]interface{}{
				"test-resource": map[string]interface{}{
					"id":    "i-123",
					"state": "running",
				},
			},
			CompletedAt: time.Now(),
		}

		err = tracker.SetResult("test-dep-3", result)
		require.NoError(t, err)

		// Verify persistence was called - count persistence calls for this deployment
		calls := mockStore.GetCalls()
		persistenceCount := 0
		for _, call := range calls {
			if (call.Method == methodCreateDeployment || call.Method == methodUpdateDeploymentStatus) && len(call.Args) > 0 {
				if call.Method == methodCreateDeployment { //nolint:staticcheck // String comparison preferred over switch for test clarity
					if deploymentID, ok := call.Args[0].(string); ok && deploymentID == "test-dep-3" {
						persistenceCount++
					}
				} else if call.Method == methodUpdateDeploymentStatus {
					if deploymentID, ok := call.Args[0].(string); ok && deploymentID == "test-dep-3" {
						persistenceCount++
					}
				}
			}
		}
		// Should have at least 2 calls: one from Register, one from SetResult
		assert.GreaterOrEqual(t, persistenceCount, 2, "Persistence methods should be called at least twice (Register + SetResult)")
	})

	t.Run("SetErrorCallsPersistence", func(t *testing.T) {
		t.Parallel()

		mockStore := mocks.NewMockStateStore()
		tracker := NewTracker()
		err := tracker.Load(mockStore)
		require.NoError(t, err)

		deployment := &interfaces.QueuedDeployment{
			ID:        "test-dep-4",
			Status:    interfaces.DeploymentStatusQueued,
			CreatedAt: time.Now(),
			Request: &interfaces.DeploymentRequest{
				Resources: []interfaces.Resource{
					{
						Name:       "test-resource",
						Type:       "aws_instance",
						Properties: map[string]interface{}{},
					},
				},
				Options: interfaces.DeploymentOptions{
					DryRun: false,
				},
			},
		}

		err = tracker.Register(deployment)
		require.NoError(t, err)

		testError := errors.New("deployment failed")
		err = tracker.SetError("test-dep-4", testError)
		require.NoError(t, err)

		// Verify persistence was called - count persistence calls for this deployment
		calls := mockStore.GetCalls()
		t.Logf("DEBUG SetError: Total calls made: %d", len(calls))
		for i, call := range calls {
			t.Logf("DEBUG SetError: Call %d: Method=%s, Args=%+v", i, call.Method, call.Args)
		}

		persistenceCount := 0
		for _, call := range calls {
			if (call.Method == methodCreateDeployment || call.Method == methodUpdateDeploymentStatus) && len(call.Args) > 0 {
				if call.Method == methodCreateDeployment { //nolint:staticcheck // String comparison preferred over switch for test clarity
					if deploymentID, ok := call.Args[0].(string); ok && deploymentID == "test-dep-4" {
						persistenceCount++
					}
				} else if call.Method == methodUpdateDeploymentStatus {
					if deploymentID, ok := call.Args[0].(string); ok && deploymentID == "test-dep-4" {
						persistenceCount++
					}
				}
			}
		}
		// Should have at least 2 calls: one from Register, one from SetError
		assert.GreaterOrEqual(t, persistenceCount, 2, "Persistence methods should be called at least twice (Register + SetError)")
	})

	t.Run("RemoveCallsPersistence", func(t *testing.T) {
		t.Parallel()

		mockStore := mocks.NewMockStateStore()
		tracker := NewTracker()
		err := tracker.Load(mockStore)
		require.NoError(t, err)

		deployment := &interfaces.QueuedDeployment{
			ID:        "test-dep-5",
			Status:    interfaces.DeploymentStatusQueued,
			CreatedAt: time.Now(),
			Request: &interfaces.DeploymentRequest{
				Resources: []interfaces.Resource{
					{
						Name:       "test-resource",
						Type:       "aws_instance",
						Properties: map[string]interface{}{},
					},
				},
				Options: interfaces.DeploymentOptions{
					DryRun: false,
				},
			},
		}

		err = tracker.Register(deployment)
		require.NoError(t, err)

		err = tracker.Remove("test-dep-5")
		require.NoError(t, err)

		// Verify deletion was called
		calls := mockStore.GetCalls()
		found := false
		for _, call := range calls {
			if call.Method == "DeleteDeployment" && len(call.Args) > 0 && call.Args[0] == "test-dep-5" {
				found = true
				break
			}
		}
		assert.True(t, found, "DeleteDeployment should be called during Remove")
	})

	t.Run("PersistenceFailureIsHandledGracefully", func(t *testing.T) {
		t.Parallel()

		mockStore := mocks.NewMockStateStore()
		// Configure mock to fail persistence operations
		mockStore.SetShouldFail(methodCreateDeployment, errors.New("persistence failed"))

		tracker := NewTracker()
		err := tracker.Load(mockStore)
		require.NoError(t, err)

		deployment := &interfaces.QueuedDeployment{
			ID:        "test-dep-fail",
			Status:    interfaces.DeploymentStatusQueued,
			CreatedAt: time.Now(),
			Request: &interfaces.DeploymentRequest{
				Resources: []interfaces.Resource{
					{
						Name:       "test-resource",
						Type:       "aws_instance",
						Properties: map[string]interface{}{},
					},
				},
				Options: interfaces.DeploymentOptions{
					DryRun: false,
				},
			},
		}

		// Register should still succeed even if persistence fails
		err = tracker.Register(deployment)
		require.NoError(t, err)

		// Verify deployment is still in memory
		status, err := tracker.GetStatus("test-dep-fail")
		require.NoError(t, err)
		assert.Equal(t, interfaces.DeploymentStatusQueued, *status)
	})
}

func TestTracker_LoadPreservesDeploymentResults(t *testing.T) {
	t.Parallel()

	t.Run("RestartScenarioPreservesResults", func(t *testing.T) {
		t.Parallel()

		// Setup: Create first tracker with StateStore
		mockStore := mocks.NewMockStateStore()
		tracker1 := NewTracker()
		err := tracker1.Load(mockStore)
		require.NoError(t, err)

		// Register deployment
		deployment := &interfaces.QueuedDeployment{
			ID:        "restart-test-1",
			Status:    interfaces.DeploymentStatusProcessing,
			CreatedAt: time.Now(),
			Request: &interfaces.DeploymentRequest{
				Resources: []interfaces.Resource{
					{
						Name:       "web",
						Type:       "aws_instance",
						Properties: map[string]interface{}{"instance_type": "t2.micro"},
					},
				},
			},
		}
		err = tracker1.Register(deployment)
		require.NoError(t, err)

		// Set result with resource details
		result := &interfaces.DeploymentResult{
			DeploymentID: "restart-test-1",
			Success:      true,
			Resources: map[string]interface{}{
				"aws_instance.web": map[string]interface{}{
					"id":            "i-123456",
					"public_ip":     "10.0.0.1",
					"instance_type": "t2.micro",
					"state":         "running",
				},
			},
			Outputs: map[string]interface{}{
				"instance_ip": "10.0.0.1",
			},
			CompletedAt: time.Now(),
		}
		err = tracker1.SetResult("restart-test-1", result)
		require.NoError(t, err)

		// Mark as completed
		err = tracker1.SetStatus("restart-test-1", interfaces.DeploymentStatusCompleted)
		require.NoError(t, err)

		// Simulate server restart: Create NEW tracker
		tracker2 := NewTracker()

		err = tracker2.Load(mockStore) // Load from same StateStore
		require.NoError(t, err)

		// Verify deployment metadata was loaded
		loadedDeployment, err := tracker2.GetByID("restart-test-1")
		require.NoError(t, err)
		require.NotNil(t, loadedDeployment)
		assert.Equal(t, interfaces.DeploymentStatusCompleted, loadedDeployment.Status)

		// Verify that deployment results are preserved after restart
		// This tests that the embedded tracker correctly persists and loads deployment results
		loadedResult, err := tracker2.GetResult("restart-test-1")
		require.NoError(t, err)

		// Results should be preserved after restart through the StateStore's Configuration field
		require.NotNil(t, loadedResult, "Deployment results should be preserved after restart")
		assert.Equal(t, result.Resources, loadedResult.Resources)
		assert.Equal(t, result.Outputs, loadedResult.Outputs)
	})

	t.Run("MultipleDeploymentsWithResultsAfterRestart", func(t *testing.T) {
		t.Parallel()

		mockStore := mocks.NewMockStateStore()
		tracker1 := NewTracker()
		err := tracker1.Load(mockStore)
		require.NoError(t, err)

		// Create multiple deployments with results
		for i := 1; i <= 3; i++ {
			deploymentID := fmt.Sprintf("multi-restart-%d", i)

			deployment := &interfaces.QueuedDeployment{
				ID:        deploymentID,
				Status:    interfaces.DeploymentStatusCompleted,
				CreatedAt: time.Now(),
				Request: &interfaces.DeploymentRequest{
					Resources: []interfaces.Resource{
						{
							Name: fmt.Sprintf("resource-%d", i),
							Type: "test_resource",
						},
					},
				},
			}

			err = tracker1.Register(deployment)
			require.NoError(t, err)

			result := &interfaces.DeploymentResult{
				DeploymentID: deploymentID,
				Success:      true,
				Resources: map[string]interface{}{
					fmt.Sprintf("test_resource.resource-%d", i): map[string]interface{}{
						"id":    fmt.Sprintf("id-%d", i),
						"value": fmt.Sprintf("value-%d", i),
					},
				},
			}
			err = tracker1.SetResult(deploymentID, result)
			require.NoError(t, err)
		}

		// Note: MockStateStore doesn't pre-populate deployments for Load()
		// The Load() method calls ListDeployments() on the store
		// Since we're simulating a restart, the store should already have the deployments
		// but our mock doesn't persist between tracker1 and tracker2 instances

		// Simulate restart
		tracker2 := NewTracker()
		err = tracker2.Load(mockStore)
		require.NoError(t, err)

		// Verify all deployments are loaded and results SHOULD be preserved
		for i := 1; i <= 3; i++ {
			deploymentID := fmt.Sprintf("multi-restart-%d", i)

			// Deployment metadata should be present
			deployment, err := tracker2.GetByID(deploymentID)
			require.NoError(t, err)
			require.NotNil(t, deployment)

			// Verify results are preserved after restart
			result, err := tracker2.GetResult(deploymentID)
			require.NoError(t, err)

			require.NotNil(t, result, "Results for deployment %s should be preserved after restart", deploymentID)
			assert.Contains(t, result.Resources, fmt.Sprintf("test_resource.resource-%d", i))
		}
	})
}
