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

//nolint:paralleltest,funlen // test uses shared mocks and requires comprehensive interpolation scenarios
func TestDeploymentExecutor_ResourceInterpolation(t *testing.T) {
	// Test that resources can reference other resources using interpolation
	mockProviderManager := new(mocks.ProviderLifecycleManager)
	mockTracker := new(mocks.DeploymentTracker)
	mockProvider := new(mocks.UnifiedProvider)
	mockStateStore := mocks.NewMockStateStore()

	// Setup state store mock to return empty states for interpolation test
	// No additional setup needed for the MockStateStore as it handles these methods internally

	// Create synchronous event bus for testing and connect tracker
	eventBus := events.NewSynchronousEventBus()
	events.ConnectTrackerToEventBus(eventBus, mockTracker)

	executor := NewWithEventBus(mockProviderManager, mockStateStore, eventBus, 5*time.Minute)

	deployment := &interfaces.QueuedDeployment{
		ID: "deploy-interpolation-test",
		Request: &interfaces.DeploymentRequest{
			Resources: []interfaces.Resource{
				{
					Type: "random_string",
					Name: "bucket_suffix",
					Properties: map[string]interface{}{
						"length":  8,
						"special": false,
					},
				},
				{
					Type: "aws_s3_bucket",
					Name: "demo",
					Properties: map[string]interface{}{
						"bucket": "test-bucket-${random_string.bucket_suffix.result}",
					},
				},
			},
		},
	}

	// Setup mocks for random_string
	mockProviderManager.On("GetProvider", mock.Anything, "deploy-interpolation-test", "random", "", mock.Anything, "random_string").Return(mockProvider, nil).Once()
	mockProviderManager.On("ReleaseProvider", "deploy-interpolation-test", "random").Return(nil).Once()

	mockProvider.On("CreateResource", mock.Anything, "random_string", mock.Anything).Return(
		map[string]interface{}{
			"id":     "abc123",
			"result": "xyz789",
			"length": 8,
		},
		nil,
	).Once()

	// Setup mocks for aws_s3_bucket - should receive interpolated value
	mockProviderManager.On("GetProvider", mock.Anything, "deploy-interpolation-test", "aws", "", mock.Anything, "aws_s3_bucket").Return(mockProvider, nil).Once()
	mockProviderManager.On("ReleaseProvider", "deploy-interpolation-test", "aws").Return(nil).Once()

	// This is the key assertion - the bucket name should be interpolated
	mockProvider.On("CreateResource", mock.Anything, "aws_s3_bucket", mock.MatchedBy(func(props map[string]interface{}) bool {
		bucket, ok := props["bucket"].(string)
		return ok && bucket == "test-bucket-xyz789"
	})).Return(
		map[string]interface{}{
			"id":     "test-bucket-xyz789",
			"bucket": "test-bucket-xyz789",
			"arn":    "arn:aws:s3:::test-bucket-xyz789",
		},
		nil,
	).Once()

	// Setup tracker expectations
	mockTracker.On("SetResult", "deploy-interpolation-test", mock.AnythingOfType("*interfaces.DeploymentResult")).Return(nil)
	mockTracker.On("SetStatus", "deploy-interpolation-test", interfaces.DeploymentStatusCompleted).Return(nil)

	// Execute
	err := executor.Execute(context.Background(), deployment)
	require.NoError(t, err)

	// Verify all expectations
	mockProviderManager.AssertExpectations(t)
	mockProvider.AssertExpectations(t)
	mockTracker.AssertExpectations(t)
}

//nolint:funlen // test requires comprehensive property interpolation scenarios
func TestDeploymentExecutor_InterpolateProperties(t *testing.T) {
	t.Parallel()
	// Direct test of interpolateProperties method
	executor := New(nil, nil, 5*time.Minute)

	tests := []struct {
		name               string
		properties         map[string]interface{}
		deployedResources  map[string]interface{}
		expectedProperties map[string]interface{}
	}{
		{
			name: "interpolate with dot notation keys",
			properties: map[string]interface{}{
				"bucket": "my-bucket-${random_string.suffix.result}",
			},
			deployedResources: map[string]interface{}{
				"random_string.suffix": map[string]interface{}{
					"id":     "abc123",
					"result": "xyz789",
				},
			},
			expectedProperties: map[string]interface{}{
				"bucket": "my-bucket-xyz789",
			},
		},
		{
			name: "interpolate with slash notation keys (should fail)",
			properties: map[string]interface{}{
				"bucket": "my-bucket-${random_string.suffix.result}",
			},
			deployedResources: map[string]interface{}{
				"random_string/suffix": map[string]interface{}{
					"id":     "abc123",
					"result": "xyz789",
				},
			},
			expectedProperties: map[string]interface{}{
				"bucket": "my-bucket-${random_string.suffix.result}", // Should not interpolate
			},
		},
		{
			name: "interpolate data source references",
			properties: map[string]interface{}{
				"ami": "${data.aws_ami.ubuntu.id}",
			},
			deployedResources: map[string]interface{}{
				"data.aws_ami.ubuntu": map[string]interface{}{
					"id": "ami-12345",
				},
			},
			expectedProperties: map[string]interface{}{
				"ami": "ami-12345",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := executor.interpolateProperties(tt.properties, tt.deployedResources)
			require.NoError(t, err)
			assert.Equal(t, tt.expectedProperties, result)
		})
	}
}
