//go:build !integration
// +build !integration

package testing

import (
	"context"
	"regexp"
	"testing"
)

// Test constants to avoid duplication
const (
	testProviderName    = "test"
	testProviderVersion = "1.0.0"
	testAPIKey          = "test-key"
	testResourceName    = "test-instance"
	testResourceType    = "test_resource"
	testUpdatedName     = "updated-instance"
)

//nolint:funlen // Comprehensive test covering all basic provider operations
func TestMockProvider_BasicOperations(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	provider := NewSimpleMockProvider(testProviderName, testProviderVersion)
	defer provider.ResourceStore.Clear() // Cleanup resources

	// Test Configure
	err := provider.Configure(ctx, map[string]interface{}{
		"api_key": testAPIKey,
	})
	if err != nil {
		t.Fatalf("Configure failed: %v", err)
	}
	if !provider.ConfigureCalled {
		t.Error("Configure was not called")
	}

	// Test GetSchema
	schema, err := provider.GetSchema(ctx)
	if err != nil {
		t.Fatalf("GetSchema failed: %v", err)
	}
	if schema == nil {
		t.Error("GetSchema returned nil")
	}

	// Test CreateResource
	state, err := provider.CreateResource(ctx, testResourceType, map[string]interface{}{
		"name": testResourceName,
	})
	if err != nil {
		t.Fatalf("CreateResource failed: %v", err)
	}
	if state["id"] == nil {
		t.Error("CreateResource did not return an ID")
	}

	// Test ReadResource
	readState, err := provider.ReadResource(ctx, "test_resource", state)
	if err != nil {
		t.Fatalf("ReadResource failed: %v", err)
	}
	if readState["id"] != state["id"] {
		t.Error("ReadResource returned different ID")
	}

	// Test UpdateResource
	updatedState, err := provider.UpdateResource(ctx, testResourceType, state, map[string]interface{}{
		"name": testUpdatedName,
	})
	if err != nil {
		t.Fatalf("UpdateResource failed: %v", err)
	}
	if updatedState["name"] != testUpdatedName {
		t.Errorf("UpdateResource did not update the name, expected %s got %v", testUpdatedName, updatedState["name"])
	}

	// Test DeleteResource
	err = provider.DeleteResource(ctx, "test_resource", updatedState)
	if err != nil {
		t.Fatalf("DeleteResource failed: %v", err)
	}

	// Verify all methods were called
	err = AssertProviderCalls(provider, map[string]bool{
		"Configure":      true,
		"GetSchema":      true,
		"CreateResource": true,
		"ReadResource":   true,
		"UpdateResource": true,
		"DeleteResource": true,
		"ReadDataSource": false,
		"Close":          false,
	})
	if err != nil {
		t.Errorf("Provider call assertion failed: %v", err)
	}
}

func TestMockProvider_WithResourceStore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	provider := NewSimpleMockProvider(testProviderName, testProviderVersion)
	defer provider.ResourceStore.Clear() // Cleanup

	// Create a resource
	state1, err := provider.CreateResource(ctx, testResourceType, map[string]interface{}{
		"name": "resource-1",
	})
	if err != nil {
		t.Fatalf("CreateResource failed: %v", err)
	}

	// Create another resource
	_, err = provider.CreateResource(ctx, "test_resource", map[string]interface{}{
		"name": "resource-2",
	})
	if err != nil {
		t.Fatalf("CreateResource failed: %v", err)
	}

	// Verify both resources exist
	if provider.ResourceStore.Count() != 2 {
		t.Errorf("Expected 2 resources, got %d", provider.ResourceStore.Count())
	}

	// Read first resource
	read1, err := provider.ReadResource(ctx, "test_resource", state1)
	if err != nil {
		t.Fatalf("ReadResource failed: %v", err)
	}
	if read1["name"] != "resource-1" {
		t.Error("ReadResource returned wrong resource")
	}

	// Delete first resource
	err = provider.DeleteResource(ctx, "test_resource", state1)
	if err != nil {
		t.Fatalf("DeleteResource failed: %v", err)
	}

	// Verify only one resource remains
	if provider.ResourceStore.Count() != 1 {
		t.Errorf("Expected 1 resource after delete, got %d", provider.ResourceStore.Count())
	}

	// Try to read deleted resource
	_, err = provider.ReadResource(ctx, testResourceType, state1)
	if err == nil {
		t.Error("Expected error reading deleted resource")
	}
	// Verify it's the expected error type
	expectedErr := "resource .* not found"
	if !regexp.MustCompile(expectedErr).MatchString(err.Error()) {
		t.Errorf("Expected error matching %q, got %v", expectedErr, err)
	}
}

func TestAWSMockProvider(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	provider := NewAWSMockProvider()

	// Configure with AWS credentials
	err := provider.Configure(ctx, map[string]interface{}{
		"region": "us-east-1",
	})
	if err != nil {
		t.Fatalf("Configure failed: %v", err)
	}

	// Create S3 bucket
	bucket, err := provider.CreateResource(ctx, "aws_s3_bucket", map[string]interface{}{
		"bucket": "my-test-bucket",
	})
	if err != nil {
		t.Fatalf("CreateResource failed: %v", err)
	}
	if bucket["arn"] != "arn:aws:s3:::my-test-bucket" {
		t.Errorf("Unexpected ARN: %v", bucket["arn"])
	}

	// Create EC2 instance
	instance, err := provider.CreateResource(ctx, "aws_instance", map[string]interface{}{
		"ami":           "ami-12345678",
		"instance_type": "t2.micro",
	})
	if err != nil {
		t.Fatalf("CreateResource failed: %v", err)
	}
	if instance["public_ip"] == nil {
		t.Error("Instance missing public_ip")
	}

	// Read AMI data source
	ami, err := provider.ReadDataSource(ctx, "aws_ami", map[string]interface{}{
		"owners": []string{"amazon"},
	})
	if err != nil {
		t.Fatalf("ReadDataSource failed: %v", err)
	}
	if ami["id"] != "ami-12345678" {
		t.Errorf("Unexpected AMI ID: %v", ami["id"])
	}
}

func TestFailingMockProvider(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name     string
		failOn   string
		testFunc func(*MockProvider) error
	}{
		{
			name:   "FailOnConfigure",
			failOn: "configure",
			testFunc: func(p *MockProvider) error {
				return p.Configure(ctx, map[string]interface{}{})
			},
		},
		{
			name:   "FailOnCreate",
			failOn: "create",
			testFunc: func(p *MockProvider) error {
				_, err := p.CreateResource(ctx, "test", map[string]interface{}{})
				return err
			},
		},
		{
			name:   "FailOnSchema",
			failOn: "schema",
			testFunc: func(p *MockProvider) error {
				_, err := p.GetSchema(ctx)
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			provider := NewFailingMockProvider(tt.failOn)
			err := tt.testFunc(provider)
			if err == nil {
				t.Errorf("Expected error for %s, got nil", tt.failOn)
			}
		})
	}
}

func TestMockProvider_CustomBehavior(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	provider := NewSimpleMockProvider("test", "1.0.0")

	// Add custom create behavior
	createCount := 0
	provider.CreateResourceFn = func(_ context.Context, _ string, _ map[string]interface{}) (map[string]interface{}, error) {
		createCount++
		return map[string]interface{}{
			"id":    "custom-id",
			"count": createCount,
		}, nil
	}

	// Create resources and verify custom behavior
	for i := 1; i <= 3; i++ {
		state, err := provider.CreateResource(ctx, "test", map[string]interface{}{})
		if err != nil {
			t.Fatalf("CreateResource failed: %v", err)
		}
		if state["count"] != i {
			t.Errorf("Expected count %d, got %v", i, state["count"])
		}
	}
}
