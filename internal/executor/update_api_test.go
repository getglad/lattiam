package executor

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/events"
	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/internal/mocks"
)

// TestUpdateDeploymentStateSync verifies that after an update,
// the original deployment's state is properly synchronized
//
//nolint:funlen,paralleltest // Comprehensive update state sync test with multiple scenarios
func TestUpdateDeploymentStateSync(t *testing.T) {
	// Create mocks
	mockProviderManager := new(mocks.ProviderLifecycleManager)
	mockTracker := new(mocks.DeploymentTracker)
	mockStateStore := mocks.NewMockStateStore()
	mockProvider := new(mocks.UnifiedProvider)

	// Setup state store mock - MockStateStore handles these methods internally

	// Create synchronous event bus for testing and connect tracker
	eventBus := events.NewSynchronousEventBus()
	events.ConnectTrackerToEventBus(eventBus, mockTracker)

	// Create executor
	executor := NewWithEventBus(mockProviderManager, mockStateStore, eventBus, 5*time.Minute)

	// Initial deployment state
	originalDeploymentID := "original-deployment"
	updateDeploymentID := "update-deployment"

	// Create update plan with tags
	updatePlan := &interfaces.UpdatePlan{
		DeploymentID: originalDeploymentID,
		PlanID:       "test-plan",
		Changes: []interfaces.ResourceChange{
			{
				ResourceKey: "aws_s3_bucket.demo",
				Action:      interfaces.ActionUpdate,
				Before: map[string]interface{}{
					"id":     "test-bucket",
					"bucket": "test-bucket",
					"tags":   nil,
				},
				After: map[string]interface{}{
					"id":     "test-bucket",
					"bucket": "test-bucket",
					"tags": map[string]interface{}{
						"Environment": "demo",
						"ManagedBy":   "lattiam",
						"Updated":     "true",
					},
				},
			},
		},
		ProviderVersions: map[string]string{
			"aws": "5.0.0",
		},
	}

	// Create update deployment
	deployment := &interfaces.QueuedDeployment{
		ID: updateDeploymentID,
		Request: &interfaces.DeploymentRequest{
			Resources: []interfaces.Resource{
				{
					Type: "aws_s3_bucket",
					Name: "demo",
					Properties: map[string]interface{}{
						"bucket": "test-bucket",
						"tags": map[string]interface{}{
							"Environment": "demo",
							"ManagedBy":   "lattiam",
							"Updated":     "true",
						},
					},
				},
			},
			Options: interfaces.DeploymentOptions{},
			Metadata: map[string]interface{}{
				"operation":     "update",
				"deployment_id": originalDeploymentID,
				"update_plan":   updatePlan,
			},
		},
	}

	// Expected updated state with tags
	updatedResourceState := map[string]interface{}{
		"id":     "test-bucket",
		"bucket": "test-bucket",
		"tags": map[string]interface{}{
			"Environment": "demo",
			"ManagedBy":   "lattiam",
			"Updated":     "true",
		},
		"region": "us-east-1",
	}

	// Setup expectations
	mockProviderManager.On("GetProvider", mock.Anything, originalDeploymentID, "aws", "5.0.0",
		mock.Anything, "aws_s3_bucket").Return(mockProvider, nil)
	mockProviderManager.On("ReleaseProvider", originalDeploymentID, "aws").Return(nil)
	mockProvider.On("ApplyResourceChange", mock.Anything, "aws_s3_bucket",
		mock.MatchedBy(func(plan *interfaces.ResourcePlan) bool {
			return plan.ResourceType == "aws_s3_bucket" && plan.Action == interfaces.PlanActionUpdate
		})).Return(updatedResourceState, nil)

	// State store will handle updates internally - no mocking needed

	// Track what gets stored in the tracker
	var updateResult *interfaces.DeploymentResult
	var originalResult *interfaces.DeploymentResult

	mockTracker.On("SetResult", updateDeploymentID, mock.MatchedBy(func(result *interfaces.DeploymentResult) bool {
		return result.DeploymentID == updateDeploymentID && result.Success == true
	})).Run(func(args mock.Arguments) {
		updateResult = args.Get(1).(*interfaces.DeploymentResult)
	}).Return(nil)

	mockTracker.On("SetStatus", updateDeploymentID, interfaces.DeploymentStatusCompleted).Return(nil)

	mockTracker.On("SetResult", originalDeploymentID, mock.MatchedBy(func(result *interfaces.DeploymentResult) bool {
		return result.DeploymentID == originalDeploymentID && result.Success == true
	})).Run(func(args mock.Arguments) {
		originalResult = args.Get(1).(*interfaces.DeploymentResult)
	}).Return(nil)

	// Execute update
	err := executor.Execute(context.Background(), deployment)
	require.NoError(t, err)

	// Verify all expectations
	mockProviderManager.AssertExpectations(t)
	mockProvider.AssertExpectations(t)
	mockTracker.AssertExpectations(t)
	// MockStateStore doesn't use expectations

	// Verify both results have the updated state with tags
	require.NotNil(t, updateResult)
	require.NotNil(t, originalResult)

	// Check update deployment has tags
	updateBucketState, ok := updateResult.Resources["aws_s3_bucket.demo"].(map[string]interface{})
	require.True(t, ok, "Update result should have bucket resource")
	updateTags, ok := updateBucketState["tags"].(map[string]interface{})
	require.True(t, ok, "Update result should have tags")
	assert.Equal(t, "demo", updateTags["Environment"])
	assert.Equal(t, "lattiam", updateTags["ManagedBy"])
	assert.Equal(t, "true", updateTags["Updated"])

	// Check original deployment also has tags
	originalBucketState, ok := originalResult.Resources["aws_s3_bucket.demo"].(map[string]interface{})
	require.True(t, ok, "Original result should have bucket resource")
	originalTags, ok := originalBucketState["tags"].(map[string]interface{})
	require.True(t, ok, "Original result should have tags")
	assert.Equal(t, "demo", originalTags["Environment"])
	assert.Equal(t, "lattiam", originalTags["ManagedBy"])
	assert.Equal(t, "true", originalTags["Updated"])
}
