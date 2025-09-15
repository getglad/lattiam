package testing

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/lattiam/lattiam/internal/provider"
)

var _ provider.Provider = (*MockProvider)(nil)

// Default constants for mock responses
const (
	DefaultMockResourceID = "test-id"
	DefaultMockDataValue  = "test-value"
)

// MockProvider implements provider.Provider for testing purposes
type MockProvider struct {
	mu sync.Mutex

	// Provider identity
	NameValue    string
	VersionValue string

	// Call tracking
	ConfigureCalled      bool
	ConfigureRequest     map[string]interface{}
	GetSchemaCalled      bool
	CreateResourceCalled bool
	ReadResourceCalled   bool
	UpdateResourceCalled bool
	DeleteResourceCalled bool
	ReadDataSourceCalled bool
	CloseCalled          bool

	// Delete call tracking for destruction testing
	deleteCalls []string

	// Validation settings
	ValidateBucketNames bool

	// Deletion behavior settings
	SimulateFailedDeletion     bool            // If true, DeleteResource will simulate returning non-null state
	FailedDeletionResources    map[string]bool // Specific resources that should fail deletion
	ReturnNonNullStateOnDelete bool            // If true, returns the existing state instead of null after delete

	// Configurable responses
	ConfigureError         error
	GetSchemaResponse      *tfprotov6.GetProviderSchemaResponse
	GetSchemaError         error
	CreateResourceResponse map[string]interface{}
	CreateResourceError    error
	ReadResourceResponse   map[string]interface{}
	ReadResourceError      error
	UpdateResourceResponse map[string]interface{}
	UpdateResourceError    error
	DeleteResourceError    error
	ReadDataSourceResponse map[string]interface{}
	ReadDataSourceError    error
	CloseError             error

	// Custom behavior functions (optional)
	ConfigureFn      func(ctx context.Context, config map[string]interface{}) error
	CreateResourceFn func(ctx context.Context, resourceType string, config map[string]interface{}) (map[string]interface{}, error)
	ReadResourceFn   func(ctx context.Context, resourceType string, state map[string]interface{}) (map[string]interface{}, error)
	UpdateResourceFn func(ctx context.Context, resourceType string, priorState, config map[string]interface{}) (map[string]interface{}, error)
	DeleteResourceFn func(ctx context.Context, resourceType string, state map[string]interface{}) error
	ReadDataSourceFn func(ctx context.Context, dataSourceType string, config map[string]interface{}) (map[string]interface{}, error)

	// Resource state tracking (simulates remote state)
	ResourceStore *ResourceStore
}

// Name returns the provider name
func (p *MockProvider) Name() string {
	return p.NameValue
}

// Version returns the provider version
func (p *MockProvider) Version() string {
	return p.VersionValue
}

// Configure configures the provider
func (p *MockProvider) Configure(ctx context.Context, config map[string]interface{}) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.ConfigureCalled = true
	p.ConfigureRequest = config

	if p.ConfigureFn != nil {
		return p.ConfigureFn(ctx, config)
	}

	return p.ConfigureError
}

// GetSchema returns the provider schema (for original provider.Provider interface)
//
//nolint:funlen // Mock provider schema definition requires comprehensive field definitions
func (p *MockProvider) GetSchema(_ context.Context) (*tfprotov6.GetProviderSchemaResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.GetSchemaCalled = true

	if p.GetSchemaError != nil {
		return nil, p.GetSchemaError
	}

	if p.GetSchemaResponse != nil {
		return p.GetSchemaResponse, nil
	}

	// Return a minimal default schema if none configured
	return &tfprotov6.GetProviderSchemaResponse{
		Provider: &tfprotov6.Schema{
			Block: &tfprotov6.SchemaBlock{
				Attributes: []*tfprotov6.SchemaAttribute{
					{
						Name:     "test_attr",
						Type:     tftypes.String,
						Optional: true,
					},
				},
			},
		},
		ResourceSchemas: map[string]*tfprotov6.Schema{
			"test_resource": {
				Block: &tfprotov6.SchemaBlock{
					Attributes: []*tfprotov6.SchemaAttribute{
						{
							Name:     "id",
							Type:     tftypes.String,
							Computed: true,
						},
						{
							Name:     "name",
							Type:     tftypes.String,
							Required: true,
						},
					},
				},
			},
		},
		DataSourceSchemas: map[string]*tfprotov6.Schema{
			"test_data": {
				Block: &tfprotov6.SchemaBlock{
					Attributes: []*tfprotov6.SchemaAttribute{
						{
							Name:     "id",
							Type:     tftypes.String,
							Required: true,
						},
						{
							Name:     "value",
							Type:     tftypes.String,
							Computed: true,
						},
					},
				},
			},
		},
	}, nil
}

// SetValidateBucketNames enables bucket name validation
func (p *MockProvider) SetValidateBucketNames(validate bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ValidateBucketNames = validate
}

// CreateResource creates a resource
func (p *MockProvider) CreateResource(ctx context.Context, resourceType string, config map[string]interface{}) (map[string]interface{}, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.CreateResourceCalled = true

	// Validate bucket names if enabled and this is an S3 bucket
	if p.ValidateBucketNames && resourceType == "aws_s3_bucket" {
		if bucketName, ok := config["bucket"].(string); ok {
			// Check for unresolved interpolation
			if containsUnresolvedInterpolation(bucketName) {
				return nil, fmt.Errorf("validating S3 Bucket (%s) name: only alphanumeric characters, hyphens, periods, and underscores allowed in %q", bucketName, bucketName)
			}
		}
	}

	if p.CreateResourceFn != nil {
		return p.CreateResourceFn(ctx, resourceType, config)
	}

	if p.CreateResourceError != nil {
		return nil, p.CreateResourceError
	}

	// If using ResourceStore, simulate creating the resource
	if p.ResourceStore != nil {
		id := fmt.Sprintf("%s-%d", resourceType, p.ResourceStore.NextID())
		state := make(map[string]interface{})
		for k, v := range config {
			state[k] = v
		}
		state["id"] = id

		// Add resource-specific attributes
		if resourceType == "random_string" {
			// Generate a mock random string result
			length := 8
			switch l := config["length"].(type) {
			case int:
				length = l
			case float64:
				length = int(l)
			}
			state["result"] = generateMockRandomString(length)
		}

		p.ResourceStore.Put(id, state)
		return state, nil
	}

	if p.CreateResourceResponse != nil {
		return p.CreateResourceResponse, nil
	}

	// Default response
	return map[string]interface{}{
		"id":   DefaultMockResourceID,
		"name": config["name"],
	}, nil
}

// ReadResource reads a resource
func (p *MockProvider) ReadResource(ctx context.Context, resourceType string, state map[string]interface{}) (map[string]interface{}, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.ReadResourceCalled = true

	if p.ReadResourceFn != nil {
		return p.ReadResourceFn(ctx, resourceType, state)
	}

	if p.ReadResourceError != nil {
		return nil, p.ReadResourceError
	}

	// If using ResourceStore, read from it
	if p.ResourceStore != nil && state["id"] != nil {
		id, ok := state["id"].(string)
		if !ok {
			return nil, fmt.Errorf("resource id must be a string, got %T", state["id"])
		}
		if stored := p.ResourceStore.Get(id); stored != nil {
			return stored, nil
		}
		return nil, fmt.Errorf("resource %s not found", id)
	}

	if p.ReadResourceResponse != nil {
		return p.ReadResourceResponse, nil
	}

	return state, nil
}

// UpdateResource updates a resource
func (p *MockProvider) UpdateResource(ctx context.Context, resourceType string, priorState, config map[string]interface{}) (map[string]interface{}, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.UpdateResourceCalled = true

	if p.UpdateResourceFn != nil {
		return p.UpdateResourceFn(ctx, resourceType, priorState, config)
	}

	if p.UpdateResourceError != nil {
		return nil, p.UpdateResourceError
	}

	// If using ResourceStore, update the resource
	if p.ResourceStore != nil && priorState["id"] != nil {
		id, ok := priorState["id"].(string)
		if !ok {
			return nil, fmt.Errorf("resource id must be a string, got %T", priorState["id"])
		}
		state := make(map[string]interface{})
		for k, v := range config {
			state[k] = v
		}
		state["id"] = id
		p.ResourceStore.Put(id, state)
		return state, nil
	}

	if p.UpdateResourceResponse != nil {
		return p.UpdateResourceResponse, nil
	}

	// Default: merge config into prior state
	result := make(map[string]interface{})
	for k, v := range priorState {
		result[k] = v
	}
	for k, v := range config {
		if k != "id" { // preserve ID
			result[k] = v
		}
	}
	return result, nil
}

// DeleteResource deletes a resource
func (p *MockProvider) DeleteResource(ctx context.Context, resourceType string, state map[string]interface{}) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.DeleteResourceCalled = true

	// Track delete calls for destruction testing
	var resourceName string
	var resourceID string
	if state != nil {
		if name, ok := state["name"].(string); ok {
			resourceName = name
		} else if id, ok := state["id"].(string); ok {
			resourceName = id
			resourceID = id
		}
		if id, ok := state["id"].(string); ok {
			resourceID = id
		}
	}
	p.deleteCalls = append(p.deleteCalls, fmt.Sprintf("%s.%s", resourceType, resourceName))

	if p.DeleteResourceFn != nil {
		return p.DeleteResourceFn(ctx, resourceType, state)
	}

	// Check if we should simulate failed deletion
	if p.SimulateFailedDeletion || (resourceID != "" && p.FailedDeletionResources[resourceID]) {
		// For provider interface compatibility, we return nil (success) but the resource still exists
		// This simulates what happens when AWS S3 bucket deletion "succeeds" but the bucket remains
		// The actual check should be done by the caller via state verification
		return nil
	}

	// If using ResourceStore, delete from it
	if p.ResourceStore != nil && state["id"] != nil {
		id, ok := state["id"].(string)
		if !ok {
			return fmt.Errorf("resource id must be a string, got %T", state["id"])
		}
		// Only actually delete if not simulating failed deletion
		if !p.SimulateFailedDeletion && !p.FailedDeletionResources[id] {
			p.ResourceStore.Delete(id)
		}
	}

	return p.DeleteResourceError
}

// ReadDataSource reads a data source
func (p *MockProvider) ReadDataSource(ctx context.Context, dataSourceType string, config map[string]interface{}) (map[string]interface{}, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.ReadDataSourceCalled = true

	if p.ReadDataSourceFn != nil {
		return p.ReadDataSourceFn(ctx, dataSourceType, config)
	}

	if p.ReadDataSourceError != nil {
		return nil, p.ReadDataSourceError
	}

	if p.ReadDataSourceResponse != nil {
		return p.ReadDataSourceResponse, nil
	}

	// Default response
	return map[string]interface{}{
		"id":    config["id"],
		"value": DefaultMockDataValue,
	}, nil
}

// PlanResourceChange plans changes to a resource
func (p *MockProvider) PlanResourceChange(_ context.Context, resourceType string, priorState, config map[string]interface{}) (*tfprotov6.PlanResourceChangeResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Simple implementation for testing
	// Check if bucket name changed for S3 bucket (requires replacement)
	var requiresReplace []*tftypes.AttributePath
	if resourceType == "aws_s3_bucket" {
		priorBucket, priorOk := priorState["bucket"].(string)
		configBucket, configOk := config["bucket"].(string)
		if priorOk && configOk && priorBucket != configBucket {
			// Bucket name change requires replacement
			requiresReplace = append(requiresReplace, tftypes.NewAttributePath().WithAttributeName("bucket"))
		}
	}

	return &tfprotov6.PlanResourceChangeResponse{
		PlannedState: &tfprotov6.DynamicValue{
			// For testing, we'll just return empty msgpack
			MsgPack: []byte{0x80}, // Empty msgpack map
		},
		RequiresReplace: requiresReplace,
	}, nil
}

// Close closes the provider
func (p *MockProvider) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.CloseCalled = true
	return p.CloseError
}

// ClearDeleteCalls clears the tracked delete calls
func (p *MockProvider) ClearDeleteCalls() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.deleteCalls = p.deleteCalls[:0]
}

// GetDeleteCalls returns the tracked delete calls
func (p *MockProvider) GetDeleteCalls() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	calls := make([]string, len(p.deleteCalls))
	copy(calls, p.deleteCalls)
	return calls
}

// ResourceStore manages mock resources (simulates remote state)
type ResourceStore struct {
	mu        sync.RWMutex
	resources map[string]map[string]interface{}
	idCounter int
}

// NewResourceStore creates a new resource store
func NewResourceStore() *ResourceStore {
	return &ResourceStore{
		resources: make(map[string]map[string]interface{}),
	}
}

// NextID returns the next available ID
func (rs *ResourceStore) NextID() int {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.idCounter++
	return rs.idCounter
}

// Put stores a resource
func (rs *ResourceStore) Put(id string, resource map[string]interface{}) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.resources[id] = resource
}

// Get retrieves a resource
func (rs *ResourceStore) Get(id string) map[string]interface{} {
	rs.mu.RLock()
	defer rs.mu.RUnlock()
	return rs.resources[id]
}

// Delete removes a resource
func (rs *ResourceStore) Delete(id string) {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	delete(rs.resources, id)
}

// Count returns the number of resources
func (rs *ResourceStore) Count() int {
	rs.mu.RLock()
	defer rs.mu.RUnlock()
	return len(rs.resources)
}

// Clear removes all resources
func (rs *ResourceStore) Clear() {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	rs.resources = make(map[string]map[string]interface{})
}

// containsUnresolvedInterpolation checks if a string contains unresolved interpolation patterns
func containsUnresolvedInterpolation(s string) bool {
	// Check for ${...} pattern which indicates unresolved interpolation
	return strings.Contains(s, "${") && strings.Contains(s, "}")
}

// generateMockRandomString generates a mock random string of the specified length
func generateMockRandomString(length int) string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	result := make([]byte, length)
	for i := 0; i < length; i++ {
		// Use a deterministic pattern for tests
		result[i] = chars[i%len(chars)]
	}
	return string(result)
}

// NewMockProvider creates a new mock provider with default configuration
func NewMockProvider(name, version string) *MockProvider {
	return &MockProvider{
		NameValue:               name,
		VersionValue:            version,
		FailedDeletionResources: make(map[string]bool),
		ResourceStore:           NewResourceStore(),
	}
}

// SimulateDeleteFailure configures the provider to simulate failed deletion for specific resources
func (p *MockProvider) SimulateDeleteFailure(resourceID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.FailedDeletionResources == nil {
		p.FailedDeletionResources = make(map[string]bool)
	}
	p.FailedDeletionResources[resourceID] = true
}

// SetReturnNonNullStateOnDelete configures whether delete operations should return non-null state
func (p *MockProvider) SetReturnNonNullStateOnDelete(returnNonNull bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ReturnNonNullStateOnDelete = returnNonNull
}

// SetSimulateFailedDeletion enables or disables failed deletion simulation
func (p *MockProvider) SetSimulateFailedDeletion(simulate bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.SimulateFailedDeletion = simulate
}
