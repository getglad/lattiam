//go:build !integration
// +build !integration

package dependency

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/interfaces"
)

// ==============================================================================
// ESSENTIAL DEPENDENCY RESOLVER TESTS
// ==============================================================================

// Test 1: Basic Dependency Ordering - Validates core topological sorting
func TestDependencyResolver_BasicOrdering(t *testing.T) {
	t.Parallel()

	resolver := NewProductionDependencyResolver(false)

	// Simple scenario: S3 bucket → EC2 instance that references it
	// This is the scenario that breaks with the mock resolver's random ordering
	resources := []interfaces.Resource{
		{
			Type: "aws_instance",
			Name: "web",
			Properties: map[string]interface{}{
				"ami":           "ami-12345678",
				"instance_type": "t3.micro",
				// This creates a dependency on the S3 bucket
				"user_data": "#!/bin/bash\naws s3 cp s3://${aws_s3_bucket.data.bucket}/setup.sh /tmp/",
			},
		},
		{
			Type: "aws_s3_bucket",
			Name: "data",
			Properties: map[string]interface{}{
				"bucket": "my-app-data-bucket",
			},
		},
	}

	// Build dependency graph
	graph, err := resolver.BuildDependencyGraph(resources, nil)
	require.NoError(t, err, "Should build dependency graph successfully")

	// Verify dependency was detected
	instanceDeps := graph.Dependencies["aws_instance.web"]
	assert.Contains(t, instanceDeps, "aws_s3_bucket.data",
		"Instance should depend on S3 bucket")

	bucketDeps := graph.Dependencies["aws_s3_bucket.data"]
	assert.Empty(t, bucketDeps, "S3 bucket should have no dependencies")

	// Get execution order
	order, err := resolver.GetExecutionOrder(graph)
	require.NoError(t, err, "Should get execution order successfully")
	require.Len(t, order, 2, "Should have both resources in order")

	// Verify correct ordering: bucket MUST come before instance
	bucketPos := -1
	instancePos := -1
	for i, resource := range order {
		if resource == "aws_s3_bucket.data" { //nolint:staticcheck // Simple comparison preferred over switch for clarity
			bucketPos = i
		} else if resource == "aws_instance.web" {
			instancePos = i
		}
	}

	require.NotEqual(t, -1, bucketPos, "Bucket should be in execution order")
	require.NotEqual(t, -1, instancePos, "Instance should be in execution order")
	assert.Less(t, bucketPos, instancePos,
		"S3 bucket MUST be created before EC2 instance to prevent deployment failure")
}

// Test 2: Cycle Detection - Validates system fails fast on impossible configurations
func TestDependencyResolver_RejectsCycles(t *testing.T) {
	t.Parallel()

	resolver := NewProductionDependencyResolver(false)

	// Create circular dependency: A → B → A
	// This should be detected and rejected
	resources := []interfaces.Resource{
		{
			Type: "test_resource",
			Name: "a",
			Properties: map[string]interface{}{
				"depends_on": "${test_resource.b.output}",
			},
		},
		{
			Type: "test_resource",
			Name: "b",
			Properties: map[string]interface{}{
				"depends_on": "${test_resource.a.output}",
			},
		},
	}

	// Build dependency graph - this should succeed
	graph, err := resolver.BuildDependencyGraph(resources, nil)
	require.NoError(t, err, "Building graph should succeed")

	// Cycle validation should fail
	err = resolver.ValidateNoCycles(graph)
	require.Error(t, err, "Should detect and reject circular dependency")
	assert.Contains(t, err.Error(), "cycle", "Error message should mention cycle")

	// Getting execution order should also fail
	_, err = resolver.GetExecutionOrder(graph)
	require.Error(t, err, "Should fail to get execution order due to cycle")
	assert.Contains(t, err.Error(), "cycle", "Error message should mention cycle")
}

// Test 3: Real Deployment Scenario - Validates multi-level dependencies work correctly
func TestDependencyResolver_AWSVPCScenario(t *testing.T) { //nolint:funlen // Test function with comprehensive test cases
	t.Parallel()

	resolver := NewProductionDependencyResolver(false)

	// Realistic AWS scenario: VPC → Subnet → Instance
	// This is one of the most common dependency patterns that must work correctly
	resources := []interfaces.Resource{
		{
			Type: "aws_instance",
			Name: "web",
			Properties: map[string]interface{}{
				"ami":                    "ami-12345678",
				"instance_type":          "t3.micro",
				"subnet_id":              "${aws_subnet.public.id}",
				"vpc_security_group_ids": []interface{}{"${aws_security_group.web.id}"},
			},
		},
		{
			Type: "aws_subnet",
			Name: "public",
			Properties: map[string]interface{}{
				"vpc_id":     "${aws_vpc.main.id}",
				"cidr_block": "10.0.1.0/24",
			},
		},
		{
			Type: "aws_vpc",
			Name: "main",
			Properties: map[string]interface{}{
				"cidr_block": "10.0.0.0/16",
			},
		},
		{
			Type: "aws_security_group",
			Name: "web",
			Properties: map[string]interface{}{
				"vpc_id": "${aws_vpc.main.id}",
			},
		},
	}

	// Build dependency graph
	graph, err := resolver.BuildDependencyGraph(resources, nil)
	require.NoError(t, err, "Should build complex dependency graph")

	// Validate no cycles in realistic scenario
	err = resolver.ValidateNoCycles(graph)
	require.NoError(t, err, "AWS VPC scenario should have no cycles")

	// Get execution order
	order, err := resolver.GetExecutionOrder(graph)
	require.NoError(t, err, "Should get execution order for AWS scenario")
	require.Len(t, order, 4, "Should have all 4 AWS resources in order")

	// Build position map for easier checking
	positions := make(map[string]int)
	for i, resource := range order {
		positions[resource] = i
	}

	// Verify critical AWS dependency constraints
	// These MUST be satisfied or AWS deployment will fail
	assert.Less(t, positions["aws_vpc.main"], positions["aws_subnet.public"],
		"VPC must be created before subnet")
	assert.Less(t, positions["aws_vpc.main"], positions["aws_security_group.web"],
		"VPC must be created before security group")
	assert.Less(t, positions["aws_subnet.public"], positions["aws_instance.web"],
		"Subnet must be created before instance")
	assert.Less(t, positions["aws_security_group.web"], positions["aws_instance.web"],
		"Security group must be created before instance")

	// Log the order for debugging if needed
	t.Logf("AWS VPC deployment order: %v", order)
}
