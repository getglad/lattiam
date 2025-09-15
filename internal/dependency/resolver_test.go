//go:build !integration
// +build !integration

package dependency

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/interfaces"
)

func TestProductionDependencyResolver_SimpleOrdering(t *testing.T) {
	t.Parallel()

	resolver := NewProductionDependencyResolver(false)

	// Create resources with dependencies:
	// bucket (no deps) -> instance (depends on bucket)
	resources := []interfaces.Resource{
		{
			Type: "aws_instance",
			Name: "web",
			Properties: map[string]interface{}{
				"ami":           "ami-12345",
				"instance_type": "t3.micro",
				// This creates a dependency on the bucket
				"user_data": "#!/bin/bash\necho 'Bucket: ${aws_s3_bucket.data.id}' > /tmp/info",
			},
		},
		{
			Type: "aws_s3_bucket",
			Name: "data",
			Properties: map[string]interface{}{
				"bucket": "my-test-bucket",
			},
		},
	}

	// Build dependency graph
	graph, err := resolver.BuildDependencyGraph(resources, []interfaces.DataSource{})
	require.NoError(t, err)

	// Check that dependencies were detected
	instanceKey := "aws_instance.web"
	bucketKey := "aws_s3_bucket.data"

	instanceDeps := graph.Dependencies[instanceKey]
	assert.Contains(t, instanceDeps, bucketKey, "Instance should depend on bucket")

	bucketDeps := graph.Dependencies[bucketKey]
	assert.Empty(t, bucketDeps, "Bucket should have no dependencies")

	// Get execution order
	order, err := resolver.GetExecutionOrder(graph)
	require.NoError(t, err)

	// Bucket should come before instance
	bucketIdx := -1
	instanceIdx := -1

	for i, resource := range order {
		if resource == bucketKey {
			bucketIdx = i
		}
		if resource == instanceKey {
			instanceIdx = i
		}
	}

	assert.NotEqual(t, -1, bucketIdx, "Bucket should be in execution order")
	assert.NotEqual(t, -1, instanceIdx, "Instance should be in execution order")
	assert.Less(t, bucketIdx, instanceIdx, "Bucket should be executed before instance")
}

func TestProductionDependencyResolver_CycleDetection(t *testing.T) {
	t.Parallel()

	resolver := NewProductionDependencyResolver(false)

	// Create a circular dependency:
	// resource_a depends on resource_b
	// resource_b depends on resource_a
	resources := []interfaces.Resource{
		{
			Type: "test_resource",
			Name: "a",
			Properties: map[string]interface{}{
				"value": "${test_resource.b.output}",
			},
		},
		{
			Type: "test_resource",
			Name: "b",
			Properties: map[string]interface{}{
				"value": "${test_resource.a.output}",
			},
		},
	}

	// Build dependency graph
	graph, err := resolver.BuildDependencyGraph(resources, []interfaces.DataSource{})
	require.NoError(t, err)

	// Validate should detect the cycle
	err = resolver.ValidateNoCycles(graph)
	require.Error(t, err, "Should detect cycle")
	assert.Contains(t, err.Error(), "cycle", "Error should mention cycle")

	// GetExecutionOrder should also fail
	_, err = resolver.GetExecutionOrder(graph)
	require.Error(t, err, "Should fail to get execution order due to cycle")
}

func TestProductionDependencyResolver_ComplexDependencies(t *testing.T) {
	t.Parallel()

	resolver := NewProductionDependencyResolver(false)

	// Create a more complex dependency chain:
	// vpc (no deps) -> subnet (depends on vpc) -> instance (depends on subnet)
	resources := []interfaces.Resource{
		{
			Type: "aws_instance",
			Name: "web",
			Properties: map[string]interface{}{
				"subnet_id": "${aws_subnet.main.id}",
			},
		},
		{
			Type: "aws_subnet",
			Name: "main",
			Properties: map[string]interface{}{
				"vpc_id": "${aws_vpc.main.id}",
			},
		},
		{
			Type: "aws_vpc",
			Name: "main",
			Properties: map[string]interface{}{
				"cidr_block": "10.0.0.0/16",
			},
		},
	}

	// Build dependency graph
	graph, err := resolver.BuildDependencyGraph(resources, []interfaces.DataSource{})
	require.NoError(t, err)

	// Validate no cycles
	err = resolver.ValidateNoCycles(graph)
	require.NoError(t, err, "Should not detect cycles in valid graph")

	// Get execution order
	order, err := resolver.GetExecutionOrder(graph)
	require.NoError(t, err)

	// Find positions
	positions := make(map[string]int)
	for i, resource := range order {
		positions[resource] = i
	}

	// VPC should come first, then subnet, then instance
	assert.Less(t, positions["aws_vpc.main"], positions["aws_subnet.main"], "VPC should come before subnet")
	assert.Less(t, positions["aws_subnet.main"], positions["aws_instance.web"], "Subnet should come before instance")
}

func TestProductionDependencyResolver_NoDependencies(t *testing.T) {
	t.Parallel()

	resolver := NewProductionDependencyResolver(false)

	// Create resources with no dependencies
	resources := []interfaces.Resource{
		{
			Type: "aws_s3_bucket",
			Name: "bucket1",
			Properties: map[string]interface{}{
				"bucket": "bucket1",
			},
		},
		{
			Type: "aws_s3_bucket",
			Name: "bucket2",
			Properties: map[string]interface{}{
				"bucket": "bucket2",
			},
		},
	}

	// Build dependency graph
	graph, err := resolver.BuildDependencyGraph(resources, []interfaces.DataSource{})
	require.NoError(t, err)

	// Should have no dependencies
	for _, deps := range graph.Dependencies {
		assert.Empty(t, deps, "Should have no dependencies")
	}

	// Should be able to get execution order
	order, err := resolver.GetExecutionOrder(graph)
	require.NoError(t, err)
	assert.Len(t, order, 2, "Should have both resources in order")
}
