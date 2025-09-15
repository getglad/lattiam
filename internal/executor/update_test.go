package executor

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/events"
	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/internal/mocks"
)

//nolint:paralleltest,funlen // test uses shared mocks and requires complex update scenarios
func TestDeploymentExecutor_ExecuteUpdate(t *testing.T) {
	// Create mocks
	mockProviderManager := new(mocks.ProviderLifecycleManager)
	mockTracker := new(mocks.DeploymentTracker)
	mockProvider := new(mocks.UnifiedProvider)
	mockStateStore := mocks.NewMockStateStore()

	// MockStateStore handles these methods internally

	// Create synchronous event bus for testing and connect tracker
	eventBus := events.NewSynchronousEventBus()
	events.ConnectTrackerToEventBus(eventBus, mockTracker)

	// Create executor
	executor := NewWithEventBus(mockProviderManager, mockStateStore, eventBus, 5*time.Minute)

	// Create update plan
	updatePlan := &interfaces.UpdatePlan{
		DeploymentID: "test-deployment",
		PlanID:       "test-plan",
		Changes: []interfaces.ResourceChange{
			{
				ResourceKey: "aws_s3_bucket.test",
				Action:      interfaces.ActionUpdate,
				Before: map[string]interface{}{
					"bucket": "test-bucket",
					"tags":   map[string]interface{}{},
				},
				After: map[string]interface{}{
					"bucket": "test-bucket",
					"tags": map[string]interface{}{
						"Environment": "test",
					},
				},
			},
		},
		ProviderConfigs: map[string]map[string]interface{}{
			"aws": {
				"region": "us-east-1",
			},
		},
		ProviderVersions: map[string]string{
			"aws": "5.0.0",
		},
	}

	// Create deployment with update plan
	deployment := &interfaces.QueuedDeployment{
		ID: "update-test-deployment",
		Request: &interfaces.DeploymentRequest{
			Resources: []interfaces.Resource{
				{
					Type: "aws_s3_bucket",
					Name: "test",
					Properties: map[string]interface{}{
						"bucket": "test-bucket",
						"tags": map[string]interface{}{
							"Environment": "test",
						},
					},
				},
			},
			Options: interfaces.DeploymentOptions{},
			Metadata: map[string]interface{}{
				"operation":     "update",
				"deployment_id": "test-deployment",
				"update_plan":   updatePlan,
			},
		},
	}

	// Setup provider expectations - using test-deployment (original deployment ID)
	mockProviderManager.On("GetProvider", mock.Anything, "test-deployment", "aws", "5.0.0", mock.Anything, "aws_s3_bucket").Return(mockProvider, nil)
	mockProviderManager.On("ReleaseProvider", "test-deployment", "aws").Return(nil)

	// Apply resource change
	mockProvider.On("ApplyResourceChange", mock.Anything, "aws_s3_bucket", mock.AnythingOfType("*interfaces.ResourcePlan")).Return(
		map[string]interface{}{
			"id":     "test-bucket",
			"bucket": "test-bucket",
			"tags": map[string]interface{}{
				"Environment": "test",
			},
		},
		nil,
	)

	// Setup tracker expectations
	mockTracker.On("SetResult", "update-test-deployment", mock.AnythingOfType("*interfaces.DeploymentResult")).Return(nil)
	mockTracker.On("SetStatus", "update-test-deployment", interfaces.DeploymentStatusCompleted).Return(nil)
	mockTracker.On("SetResult", "test-deployment", mock.AnythingOfType("*interfaces.DeploymentResult")).Return(nil)

	// Execute update
	err := executor.Execute(context.Background(), deployment)
	require.NoError(t, err)

	// Verify expectations
	mockProviderManager.AssertExpectations(t)
	mockProvider.AssertExpectations(t)
	mockTracker.AssertExpectations(t)

	// Verify the ApplyResourceChange was called with correct parameters
	mockProvider.AssertCalled(t, "ApplyResourceChange", mock.Anything, "aws_s3_bucket", mock.MatchedBy(func(plan *interfaces.ResourcePlan) bool {
		return plan.Action == interfaces.PlanActionUpdate &&
			plan.CurrentState != nil &&
			plan.ProposedState != nil
	}))
}

//nolint:paralleltest,funlen // test uses shared mocks and requires complex metadata scenarios
func TestDeploymentExecutor_ExecuteUpdate_WithMapMetadata(t *testing.T) {
	// This test simulates what happens when the update plan is deserialized from JSON
	// Create mocks
	mockProviderManager := new(mocks.ProviderLifecycleManager)
	mockTracker := new(mocks.DeploymentTracker)
	mockProvider := new(mocks.UnifiedProvider)
	mockStateStore := mocks.NewMockStateStore()

	// MockStateStore handles these methods internally

	// Create synchronous event bus for testing and connect tracker
	eventBus := events.NewSynchronousEventBus()
	events.ConnectTrackerToEventBus(eventBus, mockTracker)

	// Create executor
	executor := NewWithEventBus(mockProviderManager, mockStateStore, eventBus, 5*time.Minute)

	// Create update plan as a map (simulating JSON deserialization)
	updatePlanMap := map[string]interface{}{
		"deployment_id": "test-deployment",
		"plan_id":       "test-plan",
		"changes": []interface{}{
			map[string]interface{}{
				"resource_key": "aws_s3_bucket.test",
				"action":       "update",
				"before": map[string]interface{}{
					"bucket": "test-bucket",
					"tags":   map[string]interface{}{},
				},
				"after": map[string]interface{}{
					"bucket": "test-bucket",
					"tags": map[string]interface{}{
						"Environment": "test",
					},
				},
			},
		},
		"provider_configs": map[string]interface{}{
			"aws": map[string]interface{}{
				"region": "us-east-1",
			},
		},
		"provider_versions": map[string]interface{}{
			"aws": "5.0.0",
		},
	}

	// Create deployment with update plan as map
	deployment := &interfaces.QueuedDeployment{
		ID: "update-test-deployment",
		Request: &interfaces.DeploymentRequest{
			Resources: []interfaces.Resource{
				{
					Type: "aws_s3_bucket",
					Name: "test",
					Properties: map[string]interface{}{
						"bucket": "test-bucket",
						"tags": map[string]interface{}{
							"Environment": "test",
						},
					},
				},
			},
			Options: interfaces.DeploymentOptions{},
			Metadata: map[string]interface{}{
				"operation":     "update",
				"deployment_id": "test-deployment",
				"update_plan":   updatePlanMap,
			},
		},
	}

	// Setup provider expectations - using test-deployment (original deployment ID)
	mockProviderManager.On("GetProvider", mock.Anything, "test-deployment", "aws", "5.0.0", mock.Anything, "aws_s3_bucket").Return(mockProvider, nil)
	mockProviderManager.On("ReleaseProvider", "test-deployment", "aws").Return(nil)

	// Apply resource change
	mockProvider.On("ApplyResourceChange", mock.Anything, "aws_s3_bucket", mock.AnythingOfType("*interfaces.ResourcePlan")).Return(
		map[string]interface{}{
			"id":     "test-bucket",
			"bucket": "test-bucket",
			"tags": map[string]interface{}{
				"Environment": "test",
			},
		},
		nil,
	)

	// Setup tracker expectations
	mockTracker.On("SetResult", "update-test-deployment", mock.AnythingOfType("*interfaces.DeploymentResult")).Return(nil)
	mockTracker.On("SetStatus", "update-test-deployment", interfaces.DeploymentStatusCompleted).Return(nil)
	mockTracker.On("SetResult", "test-deployment", mock.AnythingOfType("*interfaces.DeploymentResult")).Return(nil)

	// Execute update
	err := executor.Execute(context.Background(), deployment)
	require.NoError(t, err)

	// Verify expectations
	mockProviderManager.AssertExpectations(t)
	mockProvider.AssertExpectations(t)
	mockTracker.AssertExpectations(t)
}
