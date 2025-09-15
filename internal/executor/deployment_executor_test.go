package executor // Test file

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/events"
	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/internal/mocks"
)

func TestDeploymentExecutor_Execute(t *testing.T) { //nolint:funlen // Test function with comprehensive test cases
	t.Parallel()
	tests := []struct {
		name          string
		deployment    *interfaces.QueuedDeployment
		setupMocks    func(*mocks.ProviderLifecycleManager, *mocks.DeploymentTracker, *mocks.UnifiedProvider)
		expectedError string
	}{
		{
			name: "successful resource creation",
			deployment: &interfaces.QueuedDeployment{
				ID: "deploy-1",
				Request: &interfaces.DeploymentRequest{
					Resources: []interfaces.Resource{
						{
							Type:       "aws_instance",
							Name:       "web-server",
							Properties: map[string]interface{}{"instance_type": "t2.micro"},
						},
					},
				},
			},
			setupMocks: func(pm *mocks.ProviderLifecycleManager, dt *mocks.DeploymentTracker, up *mocks.UnifiedProvider) {
				// Provider manager returns provider
				pm.On("GetProvider", mock.Anything, "deploy-1", "aws", "", mock.Anything, "aws_instance").Return(up, nil)
				pm.On("ReleaseProvider", "deploy-1", "aws").Return(nil)

				// Create succeeds (no ReadResource call since no state store)
				up.On("CreateResource", mock.Anything, "aws_instance", mock.Anything).Return(
					map[string]interface{}{"id": "i-12345", "state": "running"},
					nil,
				)

				// Tracker updates
				dt.On("SetResult", "deploy-1", mock.AnythingOfType("*interfaces.DeploymentResult")).Return(nil)
				dt.On("SetStatus", "deploy-1", interfaces.DeploymentStatusCompleted).Return(nil)
			},
		},
		{
			name: "data source processing",
			deployment: &interfaces.QueuedDeployment{
				ID: "deploy-3",
				Request: &interfaces.DeploymentRequest{
					DataSources: []interfaces.DataSource{
						{
							Type:       "aws_ami",
							Name:       "ubuntu",
							Properties: map[string]interface{}{"most_recent": true},
						},
					},
					Resources: []interfaces.Resource{
						{
							Type:       "aws_instance",
							Name:       "web-server",
							Properties: map[string]interface{}{"ami": "${data.aws_ami.ubuntu.id}"},
						},
					},
				},
			},
			setupMocks: func(pm *mocks.ProviderLifecycleManager, dt *mocks.DeploymentTracker, up *mocks.UnifiedProvider) {
				// For data source
				pm.On("GetProvider", mock.Anything, "deploy-3", "aws", "", mock.Anything, "aws_ami").Return(up, nil).Once()
				pm.On("ReleaseProvider", "deploy-3", "aws").Return(nil).Once()
				up.On("ReadDataSource", mock.Anything, "aws_ami", mock.Anything).Return(
					map[string]interface{}{"id": "ami-12345"},
					nil,
				).Once()

				// For resource
				pm.On("GetProvider", mock.Anything, "deploy-3", "aws", "", mock.Anything, "aws_instance").Return(up, nil).Once()
				pm.On("ReleaseProvider", "deploy-3", "aws").Return(nil).Once()
				up.On("CreateResource", mock.Anything, "aws_instance", mock.Anything).Return(
					map[string]interface{}{"id": "i-67890"},
					nil,
				).Once()

				dt.On("SetResult", "deploy-3", mock.AnythingOfType("*interfaces.DeploymentResult")).Return(nil)
				dt.On("SetStatus", "deploy-3", interfaces.DeploymentStatusCompleted).Return(nil)
			},
		},
		{
			name: "dry run mode",
			deployment: &interfaces.QueuedDeployment{
				ID: "deploy-4",
				Request: &interfaces.DeploymentRequest{
					Resources: []interfaces.Resource{
						{
							Type:       "aws_instance",
							Name:       "web-server",
							Properties: map[string]interface{}{"instance_type": "t2.micro"},
						},
					},
					Options: interfaces.DeploymentOptions{
						DryRun: true,
					},
				},
			},
			setupMocks: func(_ *mocks.ProviderLifecycleManager, dt *mocks.DeploymentTracker, _ *mocks.UnifiedProvider) {
				// No provider calls in dry-run mode
				dt.On("SetResult", "deploy-4", mock.AnythingOfType("*interfaces.DeploymentResult")).Return(nil)
				dt.On("SetStatus", "deploy-4", interfaces.DeploymentStatusCompleted).Return(nil)
			},
		},
		{
			name: "nil deployment request",
			deployment: &interfaces.QueuedDeployment{
				ID:      "deploy-5",
				Request: nil,
			},
			setupMocks: func(_ *mocks.ProviderLifecycleManager, _ *mocks.DeploymentTracker, _ *mocks.UnifiedProvider) {
				// No mocks needed - deployment request is nil
			},
			expectedError: "deployment request is nil",
		},
		{
			name: "provider get failure",
			deployment: &interfaces.QueuedDeployment{
				ID: "deploy-6",
				Request: &interfaces.DeploymentRequest{
					Resources: []interfaces.Resource{
						{
							Type:       "aws_instance",
							Name:       "web-server",
							Properties: map[string]interface{}{},
						},
					},
				},
			},
			setupMocks: func(pm *mocks.ProviderLifecycleManager, dt *mocks.DeploymentTracker, _ *mocks.UnifiedProvider) {
				pm.On("GetProvider", mock.Anything, "deploy-6", "aws", "", mock.Anything, "aws_instance").Return(nil, errors.New("provider not found"))
				dt.On("SetResult", "deploy-6", mock.AnythingOfType("*interfaces.DeploymentResult")).Return(nil)
				dt.On("SetStatus", "deploy-6", interfaces.DeploymentStatusFailed).Return(nil)
			},
			expectedError: "resource aws_instance.web-server failed: failed to get provider aws: provider not found",
		},
		{
			name: "resource creation failure",
			deployment: &interfaces.QueuedDeployment{
				ID: "deploy-7",
				Request: &interfaces.DeploymentRequest{
					Resources: []interfaces.Resource{
						{
							Type:       "aws_instance",
							Name:       "web-server",
							Properties: map[string]interface{}{},
						},
					},
				},
			},
			setupMocks: func(pm *mocks.ProviderLifecycleManager, dt *mocks.DeploymentTracker, up *mocks.UnifiedProvider) {
				pm.On("GetProvider", mock.Anything, "deploy-7", "aws", "", mock.Anything, "aws_instance").Return(up, nil)
				pm.On("ReleaseProvider", "deploy-7", "aws").Return(nil)
				up.On("CreateResource", mock.Anything, "aws_instance", mock.Anything).Return(nil, errors.New("insufficient permissions"))

				dt.On("SetResult", "deploy-7", mock.AnythingOfType("*interfaces.DeploymentResult")).Return(nil)
				dt.On("SetStatus", "deploy-7", interfaces.DeploymentStatusFailed).Return(nil)
			},
			expectedError: "resource aws_instance.web-server failed: failed to create resource: insufficient permissions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Create mocks
			mockProviderManager := new(mocks.ProviderLifecycleManager)
			mockTracker := new(mocks.DeploymentTracker)
			mockProvider := new(mocks.UnifiedProvider)
			mockStateStore := mocks.NewMockStateStore()

			// Setup mocks
			tt.setupMocks(mockProviderManager, mockTracker, mockProvider)

			// Create synchronous event bus for testing and connect tracker
			eventBus := events.NewSynchronousEventBus()
			events.ConnectTrackerToEventBus(eventBus, mockTracker)

			// Create executor with event bus
			executor := NewWithEventBus(mockProviderManager, mockStateStore, eventBus, 5*time.Minute)

			// Execute deployment
			err := executor.Execute(context.Background(), tt.deployment)

			// Check expectations
			if tt.expectedError != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.expectedError)
			} else {
				require.NoError(t, err)
			}

			// Verify mock expectations
			mockProviderManager.AssertExpectations(t)
			mockTracker.AssertExpectations(t)
			mockProvider.AssertExpectations(t)
		})
	}
}

func TestDeploymentExecutor_Timeout(t *testing.T) {
	t.Parallel()
	// Create a deployment that will timeout
	deployment := &interfaces.QueuedDeployment{
		ID: "deploy-timeout",
		Request: &interfaces.DeploymentRequest{
			Resources: []interfaces.Resource{
				{
					Type:       "slow_resource",
					Name:       "test",
					Properties: map[string]interface{}{},
				},
			},
		},
	}

	// Create mocks
	mockProviderManager := new(mocks.ProviderLifecycleManager)
	mockTracker := new(mocks.DeploymentTracker)
	mockProvider := new(mocks.UnifiedProvider)
	mockStateStore := mocks.NewMockStateStore()

	// Setup provider to block
	mockProviderManager.On("GetProvider", mock.Anything, "deploy-timeout", "slow", "", mock.Anything, "slow_resource").Return(mockProvider, nil)
	mockProviderManager.On("ReleaseProvider", "deploy-timeout", "slow").Return(nil).Maybe()

	// Make ReadResource block until context is canceled
	mockProvider.On("ReadResource", mock.Anything, "slow_resource", mock.Anything).Run(func(args mock.Arguments) {
		ctx := args.Get(0).(context.Context)
		<-ctx.Done() // Block until context is canceled
	}).Return(nil, context.DeadlineExceeded)

	// In case ReadResource times out, CreateResource might be called
	mockProvider.On("CreateResource", mock.Anything, "slow_resource", mock.Anything).Return(nil, context.DeadlineExceeded).Maybe()

	mockTracker.On("SetResult", "deploy-timeout", mock.AnythingOfType("*interfaces.DeploymentResult")).Return(nil)
	mockTracker.On("SetStatus", "deploy-timeout", interfaces.DeploymentStatusFailed).Return(nil)

	// Create synchronous event bus for testing and connect tracker
	eventBus := events.NewSynchronousEventBus()
	events.ConnectTrackerToEventBus(eventBus, mockTracker)

	// Create executor with very short timeout
	executor := NewWithEventBus(mockProviderManager, mockStateStore, eventBus, 100*time.Millisecond)

	// Execute should timeout
	err := executor.Execute(context.Background(), deployment)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context deadline exceeded")
}
