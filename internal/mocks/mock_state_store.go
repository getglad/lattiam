package mocks

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/lattiam/lattiam/internal/interfaces"
)

// MockStateStore implements interfaces.StateStore for testing
type MockStateStore struct {
	deployments     map[string]*interfaces.DeploymentMetadata
	terraformStates map[string][]byte
	locks           map[string]interfaces.StateLock
	shouldFail      map[string]error
	mutex           sync.RWMutex

	// Legacy state store fields for backward compatibility
	legacyDeploymentStates map[string]map[string]interface{}
	legacyResourceStates   map[string]map[string]interface{}
	calls                  []MethodCall
	storageInfo            *interfaces.StorageInfo
}

// MethodCall represents a method call for testing purposes
type MethodCall struct {
	Method string
	Args   []interface{}
}

// NewMockStateStore creates a new mock state store
func NewMockStateStore() *MockStateStore {
	return &MockStateStore{
		deployments:            make(map[string]*interfaces.DeploymentMetadata),
		terraformStates:        make(map[string][]byte),
		locks:                  make(map[string]interfaces.StateLock),
		shouldFail:             make(map[string]error),
		legacyDeploymentStates: make(map[string]map[string]interface{}),
		legacyResourceStates:   make(map[string]map[string]interface{}),
		calls:                  make([]MethodCall, 0),
	}
}

// SetShouldFail configures the mock to fail for specific methods
func (m *MockStateStore) SetShouldFail(method string, err error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.shouldFail[method] = err
}

// checkShouldFail checks if a method should fail
func (m *MockStateStore) checkShouldFail(method string) error {
	if err, ok := m.shouldFail[method]; ok {
		return err
	}
	return nil
}

// CreateDeployment creates a new deployment
func (m *MockStateStore) CreateDeployment(_ context.Context, deployment *interfaces.DeploymentMetadata) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.recordCall("CreateDeployment", deployment.DeploymentID, deployment)

	if err := m.checkShouldFail("CreateDeployment"); err != nil {
		return err
	}

	m.deployments[deployment.DeploymentID] = deployment
	return nil
}

// GetDeployment retrieves a deployment by ID
func (m *MockStateStore) GetDeployment(_ context.Context, deploymentID string) (*interfaces.DeploymentMetadata, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	if err := m.checkShouldFail("GetDeployment"); err != nil {
		return nil, err
	}

	deployment, ok := m.deployments[deploymentID]
	if !ok {
		return nil, fmt.Errorf("deployment %s not found", deploymentID)
	}

	return deployment, nil
}

// ListDeployments returns all deployments
func (m *MockStateStore) ListDeployments(_ context.Context) ([]*interfaces.DeploymentMetadata, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	if err := m.checkShouldFail("ListDeployments"); err != nil {
		return nil, err
	}

	deployments := make([]*interfaces.DeploymentMetadata, 0, len(m.deployments))
	for _, deployment := range m.deployments {
		deployments = append(deployments, deployment)
	}

	return deployments, nil
}

// UpdateDeploymentStatus updates the status of a deployment
func (m *MockStateStore) UpdateDeploymentStatus(_ context.Context, deploymentID string, status interfaces.DeploymentStatus) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.recordCall("UpdateDeploymentStatus", deploymentID, status)

	if err := m.checkShouldFail("UpdateDeploymentStatus"); err != nil {
		return err
	}

	deployment, ok := m.deployments[deploymentID]
	if !ok {
		return fmt.Errorf("deployment %s not found", deploymentID)
	}

	deployment.Status = status
	return nil
}

// DeleteDeployment deletes a deployment
func (m *MockStateStore) DeleteDeployment(_ context.Context, deploymentID string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.recordCall("DeleteDeployment", deploymentID)

	if err := m.checkShouldFail("DeleteDeployment"); err != nil {
		return err
	}

	delete(m.deployments, deploymentID)
	delete(m.terraformStates, deploymentID)
	return nil
}

// SaveTerraformState saves Terraform state data
func (m *MockStateStore) SaveTerraformState(_ context.Context, deploymentID string, stateData []byte) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if err := m.checkShouldFail("SaveTerraformState"); err != nil {
		return err
	}

	m.terraformStates[deploymentID] = stateData
	return nil
}

// LoadTerraformState loads Terraform state data
func (m *MockStateStore) LoadTerraformState(_ context.Context, deploymentID string) ([]byte, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	if err := m.checkShouldFail("LoadTerraformState"); err != nil {
		return nil, err
	}

	stateData, ok := m.terraformStates[deploymentID]
	if !ok {
		return nil, fmt.Errorf("terraform state not found for deployment %s", deploymentID)
	}

	return stateData, nil
}

// DeleteTerraformState deletes Terraform state data
func (m *MockStateStore) DeleteTerraformState(_ context.Context, deploymentID string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if err := m.checkShouldFail("DeleteTerraformState"); err != nil {
		return err
	}

	delete(m.terraformStates, deploymentID)
	return nil
}

// LockDeployment creates a deployment lock
func (m *MockStateStore) LockDeployment(_ context.Context, deploymentID string) (interfaces.StateLock, error) {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.recordCall("LockDeployment", deploymentID)

	if err := m.checkShouldFail("LockDeployment"); err != nil {
		return nil, err
	}

	if _, exists := m.locks[deploymentID]; exists {
		return nil, fmt.Errorf("deployment %s is already locked", deploymentID)
	}

	lock := &MockStateLock{
		id:           fmt.Sprintf("lock-%s", deploymentID),
		deploymentID: deploymentID,
		createdAt:    time.Now(),
		store:        m,
	}
	m.locks[deploymentID] = lock
	return lock, nil
}

// UnlockDeployment removes a deployment lock
func (m *MockStateStore) UnlockDeployment(_ context.Context, lock interfaces.StateLock) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.recordCall("UnlockDeployment", lock)

	if err := m.checkShouldFail("UnlockDeployment"); err != nil {
		return err
	}

	mockLock, ok := lock.(*MockStateLock)
	if !ok {
		return fmt.Errorf("invalid lock type")
	}

	delete(m.locks, mockLock.deploymentID)
	return nil
}

// Ping checks if the store is accessible
func (m *MockStateStore) Ping(_ context.Context) error {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	if err := m.checkShouldFail("Ping"); err != nil {
		return err
	}

	return nil
}

// MockStateLock implements interfaces.StateLock for testing
type MockStateLock struct {
	id           string
	deploymentID string
	createdAt    time.Time
	store        *MockStateStore
}

// ID returns the lock ID
func (l *MockStateLock) ID() string {
	return l.id
}

// DeploymentID returns the deployment ID
func (l *MockStateLock) DeploymentID() string {
	return l.deploymentID
}

// CreatedAt returns when the lock was created
func (l *MockStateLock) CreatedAt() time.Time {
	return l.createdAt
}

// Release releases the lock by calling the store's UnlockDeployment method
func (l *MockStateLock) Release() error {
	ctx := context.Background()
	return l.store.UnlockDeployment(ctx, l)
}

// Legacy methods for backward compatibility with old StateStore interface

// GetCalls returns all method calls made to this mock
func (m *MockStateStore) GetCalls() []MethodCall {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.calls
}

// recordCall records a method call for testing purposes
func (m *MockStateStore) recordCall(method string, args ...interface{}) {
	m.calls = append(m.calls, MethodCall{
		Method: method,
		Args:   args,
	})
}

// GetDeploymentState retrieves deployment state (legacy method)
func (m *MockStateStore) GetDeploymentState(deploymentID string) (map[string]interface{}, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	m.recordCall("GetDeploymentState", deploymentID)

	if err := m.checkShouldFail("GetDeploymentState"); err != nil {
		return nil, err
	}

	state, ok := m.legacyDeploymentStates[deploymentID]
	if !ok {
		return nil, fmt.Errorf("deployment %s not found", deploymentID)
	}

	return state, nil
}

// UpdateDeploymentState updates deployment state (legacy method)
func (m *MockStateStore) UpdateDeploymentState(deploymentID string, state map[string]interface{}) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.recordCall("UpdateDeploymentState", deploymentID, state)

	if err := m.checkShouldFail("UpdateDeploymentState"); err != nil {
		return err
	}

	m.legacyDeploymentStates[deploymentID] = state
	return nil
}

// DeleteDeploymentState removes deployment state (legacy method)
func (m *MockStateStore) DeleteDeploymentState(deploymentID string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.recordCall("DeleteDeploymentState", deploymentID)

	if err := m.checkShouldFail("DeleteDeploymentState"); err != nil {
		return err
	}

	delete(m.legacyDeploymentStates, deploymentID)
	return nil
}

// GetResourceState retrieves resource state (legacy method)
func (m *MockStateStore) GetResourceState(resourceKey string) (map[string]interface{}, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	m.recordCall("GetResourceState", resourceKey)

	if err := m.checkShouldFail("GetResourceState"); err != nil {
		return nil, err
	}

	state, ok := m.legacyResourceStates[resourceKey]
	if !ok {
		return nil, fmt.Errorf("resource %s not found", resourceKey)
	}

	return state, nil
}

// UpdateResourceState updates resource state (legacy method)
func (m *MockStateStore) UpdateResourceState(resourceKey string, state map[string]interface{}) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.recordCall("UpdateResourceState", resourceKey, state)

	if err := m.checkShouldFail("UpdateResourceState"); err != nil {
		return err
	}

	m.legacyResourceStates[resourceKey] = state
	return nil
}

// UpdateResourceStatesBatch updates multiple resource states (legacy method)
func (m *MockStateStore) UpdateResourceStatesBatch(states map[string]map[string]interface{}) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.recordCall("UpdateResourceStatesBatch", states)

	if err := m.checkShouldFail("UpdateResourceStatesBatch"); err != nil {
		return err
	}

	for key, state := range states {
		m.legacyResourceStates[key] = state
	}
	return nil
}

// GetResourceStatesBatch retrieves multiple resource states (legacy method)
func (m *MockStateStore) GetResourceStatesBatch(keys []string) (map[string]map[string]interface{}, error) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	m.recordCall("GetResourceStatesBatch", keys)

	if err := m.checkShouldFail("GetResourceStatesBatch"); err != nil {
		return nil, err
	}

	result := make(map[string]map[string]interface{})
	for _, key := range keys {
		if state, ok := m.legacyResourceStates[key]; ok {
			result[key] = state
		}
	}

	return result, nil
}

// Cleanup removes old states (legacy method)
func (m *MockStateStore) Cleanup(olderThan time.Duration) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	m.recordCall("Cleanup", olderThan)

	if err := m.checkShouldFail("Cleanup"); err != nil {
		return err
	}

	// No-op for mock, could implement if needed for tests
	return nil
}

// SetupGetStorageInfo configures the mock to return specific storage info
func (m *MockStateStore) SetupGetStorageInfo(info *interfaces.StorageInfo) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.storageInfo = info
}

// GetStorageInfo returns configured storage info (for testing handlers)
func (m *MockStateStore) GetStorageInfo() *interfaces.StorageInfo {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	if m.storageInfo != nil {
		return m.storageInfo
	}

	// Return default storage info if none configured
	return &interfaces.StorageInfo{
		Type:            "mock",
		Exists:          true,
		Writable:        true,
		DeploymentCount: len(m.deployments),
		TotalSizeBytes:  1024,
		UsedPercent:     25.0,
	}
}
