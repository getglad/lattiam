package testing

import (
	"context"
	"fmt"
)

// Common resource type constants
const (
	resourceTypeS3Bucket = "aws_s3_bucket"
)

// NewSimpleMockProvider creates a basic mock provider with sensible defaults
func NewSimpleMockProvider(name, version string) *MockProvider {
	return &MockProvider{
		NameValue:     name,
		VersionValue:  version,
		ResourceStore: NewResourceStore(),
	}
}

// NewRandomMockProvider creates a mock random provider with proper resource handling
func NewRandomMockProvider() *MockProvider {
	provider := NewSimpleMockProvider("random", "3.6.0")

	// The ResourceStore in MockProvider already handles random_string resources
	// by adding the 'result' field, so we don't need custom CreateResourceFn

	return provider
}

// NewAWSMockProvider creates a mock AWS provider with common resource schemas
func NewAWSMockProvider() *MockProvider {
	provider := NewSimpleMockProvider("aws", "6.2.0")

	// Build schema using the builder pattern for better maintainability
	provider.GetSchemaResponse = buildAWSProviderSchema()

	// Add AWS-specific behavior
	provider.CreateResourceFn = func(_ context.Context, resourceType string, config map[string]interface{}) (map[string]interface{}, error) {
		switch resourceType {
		case resourceTypeS3Bucket:
			bucket := config["bucket"].(string)
			state := map[string]interface{}{
				"id":     bucket,
				"bucket": bucket,
				"arn":    fmt.Sprintf("arn:aws:s3:::%s", bucket),
				"region": "us-east-1",
			}
			// Store in ResourceStore for deletion simulation to work
			provider.ResourceStore.Put(bucket, state)
			return state, nil
		case "aws_instance":
			id := fmt.Sprintf("i-%d", provider.ResourceStore.NextID())
			state := map[string]interface{}{
				"id":            id,
				"ami":           config["ami"],
				"instance_type": config["instance_type"],
				"public_ip":     "54.123.45.67",
				"private_ip":    "10.0.1.123",
			}
			// Store in ResourceStore for deletion simulation to work
			provider.ResourceStore.Put(id, state)
			return state, nil
		default:
			return nil, fmt.Errorf("unknown resource type: %s", resourceType)
		}
	}

	provider.ReadDataSourceFn = func(_ context.Context, dataSourceType string, config map[string]interface{}) (map[string]interface{}, error) {
		switch dataSourceType {
		case "aws_ami":
			return map[string]interface{}{
				"id":     "ami-12345678",
				"name":   "ubuntu-20.04-amd64-server",
				"owners": config["owners"],
			}, nil
		default:
			return nil, fmt.Errorf("unknown data source type: %s", dataSourceType)
		}
	}

	return provider
}

// NewFailingMockProvider creates a mock provider that fails at specific operations
func NewFailingMockProvider(failOn string) *MockProvider {
	provider := NewSimpleMockProvider("test", "1.0.0")

	switch failOn {
	case "configure":
		provider.ConfigureError = fmt.Errorf("configuration failed")
	case "create":
		provider.CreateResourceError = fmt.Errorf("resource creation failed")
	case "read":
		provider.ReadResourceError = fmt.Errorf("resource read failed")
	case "update":
		provider.UpdateResourceError = fmt.Errorf("resource update failed")
	case "delete":
		provider.DeleteResourceError = fmt.Errorf("resource deletion failed")
	case "schema":
		provider.GetSchemaError = fmt.Errorf("schema retrieval failed")
	}

	return provider
}

// MockProviderWithDelay creates a provider that simulates delays
func MockProviderWithDelay(_ int) *MockProvider {
	provider := NewSimpleMockProvider("test", "1.0.0")

	// Add delay simulation via custom functions
	// Implementation would add time.Sleep(time.Duration(delayMs) * time.Millisecond)
	// in each operation

	return provider
}

// AssertProviderCalls verifies expected provider method calls
func AssertProviderCalls(p *MockProvider, expectedCalls map[string]bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	calls := map[string]bool{
		"Configure":      p.ConfigureCalled,
		"GetSchema":      p.GetSchemaCalled,
		"CreateResource": p.CreateResourceCalled,
		"ReadResource":   p.ReadResourceCalled,
		"UpdateResource": p.UpdateResourceCalled,
		"DeleteResource": p.DeleteResourceCalled,
		"ReadDataSource": p.ReadDataSourceCalled,
		"Close":          p.CloseCalled,
	}

	for method, expected := range expectedCalls {
		actual, exists := calls[method]
		if !exists {
			return fmt.Errorf("unknown method: %s", method)
		}
		if actual != expected {
			return fmt.Errorf("method %s: expected called=%v, got called=%v", method, expected, actual)
		}
	}

	return nil
}

// ResetMockProvider resets all call tracking and responses
func ResetMockProvider(p *MockProvider) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Reset call tracking
	p.ConfigureCalled = false
	p.GetSchemaCalled = false
	p.CreateResourceCalled = false
	p.ReadResourceCalled = false
	p.UpdateResourceCalled = false
	p.DeleteResourceCalled = false
	p.ReadDataSourceCalled = false
	p.CloseCalled = false

	// Reset requests
	p.ConfigureRequest = nil

	// Clear resource store if present
	if p.ResourceStore != nil {
		p.ResourceStore.Clear()
	}
}
