package deployment

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/internal/mocks"
)

func TestService_UpdateDeployment(t *testing.T) { //nolint:funlen // Test function with comprehensive test cases
	t.Parallel()

	// Create mocks
	queue := mocks.NewDeploymentQueue(t)
	tracker := &mockTracker{
		deployments: make(map[string]*interfaces.QueuedDeployment),
		statuses:    make(map[string]interfaces.DeploymentStatus),
		results:     make(map[string]*interfaces.DeploymentResult),
	}
	stateStore := mocks.NewMockStateStore()
	providerManager := mocks.NewProviderLifecycleManager(t)
	interpolator := mocks.NewMockInterpolationResolver()

	// Set up queue expectations
	queue.On("Enqueue", mock.Anything, mock.Anything).Return(nil)

	// Set up provider manager expectations
	mockProvider := mocks.NewUnifiedProvider(t)
	providerManager.On("GetProvider", mock.Anything, mock.Anything, "aws", "~> 5.0", mock.Anything, "aws_s3_bucket").Return(mockProvider, nil)

	// Set up provider expectations for planning
	mockProvider.On("PlanResourceChange", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(&interfaces.ResourcePlan{
		ResourceType: "aws_s3_bucket",
		Action:       interfaces.PlanActionUpdate,
		CurrentState: map[string]interface{}{
			"bucket": "test-bucket",
		},
		PlannedState: map[string]interface{}{
			"bucket": "test-bucket",
			"tags": map[string]interface{}{
				"Environment": "test",
			},
		},
		BeforeState: map[string]interface{}{
			"bucket": "test-bucket",
		},
		AfterState: map[string]interface{}{
			"bucket": "test-bucket",
			"tags": map[string]interface{}{
				"Environment": "test",
			},
		},
		RequiresReplace: []string{},
	}, nil)

	// Create service with full config
	serviceConfig := ServiceConfig{
		Queue:           queue,
		Tracker:         tracker,
		StateStore:      stateStore,
		ProviderManager: providerManager,
		Interpolator:    interpolator,
	}
	service, err := NewServiceWithConfig(serviceConfig)
	require.NoError(t, err)

	// Create initial deployment
	initialRequest := &interfaces.DeploymentRequest{
		Resources: []interfaces.Resource{
			{
				Type: "aws_s3_bucket",
				Name: "test",
				Properties: map[string]interface{}{
					"bucket": "test-bucket",
				},
			},
		},
		Options: interfaces.DeploymentOptions{},
	}

	deployment, err := service.CreateDeployment(initialRequest)
	require.NoError(t, err)
	require.NotNil(t, deployment)

	// Mark deployment as completed
	err = tracker.SetStatus(deployment.ID, interfaces.DeploymentStatusCompleted)
	require.NoError(t, err)
	err = tracker.SetResult(deployment.ID, &interfaces.DeploymentResult{
		DeploymentID: deployment.ID,
		Success:      true,
		Resources: map[string]interface{}{
			"aws_s3_bucket.test": map[string]interface{}{
				"id":     "test-bucket",
				"bucket": "test-bucket",
			},
		},
		CompletedAt: time.Now(),
	})
	require.NoError(t, err)

	// Set deployment state in state store for update planning
	err = stateStore.UpdateDeploymentState(deployment.ID, map[string]interface{}{
		"resources": map[string]interface{}{
			"aws_s3_bucket.test": map[string]interface{}{
				"id":     "test-bucket",
				"bucket": "test-bucket",
			},
		},
	})
	require.NoError(t, err)

	// Also set resource states for update planning (GenerateUpdatePlan uses GetAllResourceStates)
	// Use slash notation and include deployment ID prefix
	resourceKey := fmt.Sprintf("%s_%s", deployment.ID, string(interfaces.MakeResourceKey("aws_s3_bucket", "test")))
	err = stateStore.UpdateResourceState(resourceKey, map[string]interface{}{
		"id":     "test-bucket",
		"bucket": "test-bucket",
	})
	require.NoError(t, err)

	// Set up Terraform state for update planning (GenerateUpdatePlan calls LoadTerraformState)
	terraformState := map[string]interface{}{
		"version":           4,
		"terraform_version": "1.0",
		"resources": []map[string]interface{}{
			{
				"type":     "aws_s3_bucket",
				"name":     "test",
				"provider": "provider[\"registry.terraform.io/hashicorp/aws\"]",
				"instances": []map[string]interface{}{
					{
						"attributes": map[string]interface{}{
							"id":     "test-bucket",
							"bucket": "test-bucket",
						},
					},
				},
			},
		},
	}
	terraformStateBytes, err := json.Marshal(terraformState)
	require.NoError(t, err)
	err = stateStore.SaveTerraformState(context.Background(), deployment.ID, terraformStateBytes)
	require.NoError(t, err)

	// Test update with new configuration
	updateRequest := &interfaces.UpdateRequest{
		DeploymentID: deployment.ID,
		NewTerraformJSON: map[string]interface{}{
			"terraform": map[string]interface{}{
				"required_providers": map[string]interface{}{
					"aws": map[string]interface{}{
						"source":  "hashicorp/aws",
						"version": "~> 5.0",
					},
				},
			},
			"resource": map[string]interface{}{
				"aws_s3_bucket": map[string]interface{}{
					"test": map[string]interface{}{
						"bucket": "test-bucket",
						"tags": map[string]interface{}{
							"Environment": "test",
						},
					},
				},
			},
		},
		PlanFirst:   true,
		AutoApprove: false,
	}

	// Perform update
	queuedUpdate, err := service.UpdateDeployment(deployment.ID, updateRequest)
	require.NoError(t, err)
	require.NotNil(t, queuedUpdate)

	// Verify update was created
	assert.NotEmpty(t, queuedUpdate.ID)
	assert.Equal(t, deployment.ID, queuedUpdate.DeploymentID)
	assert.Equal(t, interfaces.UpdateStatusPending, queuedUpdate.Status)
	assert.NotNil(t, queuedUpdate.Plan)

	// Verify plan was generated
	plan := queuedUpdate.Plan
	assert.NotEmpty(t, plan.PlanID)
	assert.Equal(t, deployment.ID, plan.DeploymentID)
	assert.Len(t, plan.Changes, 1)

	// Verify the change is an update
	change := plan.Changes[0]
	assert.Equal(t, interfaces.ResourceKey("aws_s3_bucket.test"), change.ResourceKey)
	assert.Equal(t, interfaces.ActionUpdate, change.Action)
	assert.NotNil(t, change.Before)
	assert.NotNil(t, change.After)

	// Verify update deployment was queued by checking the mock was called
	queue.AssertCalled(t, "Enqueue", mock.Anything, mock.MatchedBy(func(d *interfaces.QueuedDeployment) bool {
		// Verify it's an update deployment
		return d.ID != deployment.ID &&
			d.Request.Metadata[interfaces.MetadataKeyOperation] == interfaces.OperationUpdate &&
			d.Request.Metadata[interfaces.MetadataKeyDeploymentID] == deployment.ID &&
			d.Request.Metadata[interfaces.MetadataKeyUpdateID] == queuedUpdate.ID &&
			d.Request.Metadata[interfaces.MetadataKeyUpdatePlan] != nil
	}))
}

func TestService_UpdateDeployment_ValidationErrors(t *testing.T) { // Test function with comprehensive test cases
	t.Parallel()

	// Create mocks
	queue := mocks.NewDeploymentQueue(t)
	tracker := &mockTracker{
		deployments: make(map[string]*interfaces.QueuedDeployment),
		statuses:    make(map[string]interfaces.DeploymentStatus),
		results:     make(map[string]*interfaces.DeploymentResult),
	}

	// Create service with full config (NewService doesn't have all deps)
	serviceConfig := ServiceConfig{
		Queue:           queue,
		Tracker:         tracker,
		StateStore:      mocks.NewMockStateStore(),
		ProviderManager: mocks.NewProviderLifecycleManager(t),
		Interpolator:    mocks.NewMockInterpolationResolver(),
	}
	service, err := NewServiceWithConfig(serviceConfig)
	require.NoError(t, err)

	// Note: These subtests share the same service and tracker instances,
	// so they cannot run in parallel
	t.Run("EmptyDeploymentID", func(t *testing.T) { //nolint:paralleltest // shares service and tracker instances
		_, err := service.UpdateDeployment("", &interfaces.UpdateRequest{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "deployment ID is required")
	})

	t.Run("NilRequest", func(t *testing.T) { //nolint:paralleltest // shares service and tracker instances
		_, err := service.UpdateDeployment("test-id", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "update request is required")
	})

	t.Run("NonExistentDeployment", func(t *testing.T) { //nolint:paralleltest // shares service and tracker instances
		_, err := service.UpdateDeployment("non-existent", &interfaces.UpdateRequest{})
		require.Error(t, err)
	})

	t.Run("DeploymentInProgress", func(t *testing.T) { //nolint:paralleltest // shares service and tracker instances
		// Create deployment
		deployment := &interfaces.QueuedDeployment{
			ID:     "test-deployment",
			Status: interfaces.DeploymentStatusProcessing,
		}
		err := tracker.Register(deployment)
		require.NoError(t, err)
		err = tracker.SetStatus(deployment.ID, interfaces.DeploymentStatusProcessing)
		require.NoError(t, err)

		_, err = service.UpdateDeployment(deployment.ID, &interfaces.UpdateRequest{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot update a deployment that is currently in progress")
	})
}

// mockTracker is a simple in-memory tracker implementation for testing
type mockTracker struct {
	deployments map[string]*interfaces.QueuedDeployment
	statuses    map[string]interfaces.DeploymentStatus
	results     map[string]*interfaces.DeploymentResult
}

func (m *mockTracker) Register(deployment *interfaces.QueuedDeployment) error {
	m.deployments[deployment.ID] = deployment
	m.statuses[deployment.ID] = deployment.Status
	return nil
}

func (m *mockTracker) GetStatus(deploymentID string) (*interfaces.DeploymentStatus, error) {
	if status, ok := m.statuses[deploymentID]; ok {
		return &status, nil
	}
	return nil, fmt.Errorf("deployment %s not found", deploymentID)
}

func (m *mockTracker) SetStatus(deploymentID string, status interfaces.DeploymentStatus) error {
	m.statuses[deploymentID] = status
	return nil
}

func (m *mockTracker) GetResult(deploymentID string) (*interfaces.DeploymentResult, error) {
	if result, ok := m.results[deploymentID]; ok {
		return result, nil
	}
	return nil, nil
}

func (m *mockTracker) SetResult(deploymentID string, result *interfaces.DeploymentResult) error {
	m.results[deploymentID] = result
	return nil
}

func (m *mockTracker) Remove(deploymentID string) error {
	delete(m.deployments, deploymentID)
	delete(m.statuses, deploymentID)
	delete(m.results, deploymentID)
	return nil
}

func (m *mockTracker) Load(_ interfaces.StateStore) error {
	// No-op for test mock
	return nil
}

func (m *mockTracker) GetByID(deploymentID string) (*interfaces.QueuedDeployment, error) {
	if deployment, ok := m.deployments[deploymentID]; ok {
		return deployment, nil
	}
	return nil, fmt.Errorf("deployment %s not found", deploymentID)
}

func (m *mockTracker) List(_ interfaces.DeploymentFilter) ([]*interfaces.QueuedDeployment, error) {
	deployments := make([]*interfaces.QueuedDeployment, 0, len(m.deployments))
	for _, d := range m.deployments {
		deployments = append(deployments, d)
	}
	return deployments, nil
}
