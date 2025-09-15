// Package helpers provides common test scenarios and data patterns
package helpers

import (
	"errors"
	"fmt"
	"regexp"
	"testing"
)

// Common error patterns for S3 bucket validation
var (
	ErrBucketNameRequired = errors.New("bucket name is required")
	ErrInvalidBucketName  = errors.New("invalid bucket name")
	ErrBucketNameTooLong  = errors.New("bucket name too long")

	// Error patterns as compiled regexes for better performance
	bucketRequiredPattern = regexp.MustCompile(`bucket.*required`)
	invalidBucketPattern  = regexp.MustCompile(`invalid.*bucket.*name`)
	bucketTooLongPattern  = regexp.MustCompile(`bucket name.*too long`)
)

// S3BucketScenario represents a test scenario for S3 bucket operations
type S3BucketScenario struct {
	Name         string
	Config       map[string]interface{}
	ExpectError  bool
	ErrorPattern *regexp.Regexp // Use compiled regex instead of string
	Validate     func(*testing.T, map[string]interface{})
}

// emptyValidation provides a no-op validation function
func emptyValidation(t *testing.T, _ map[string]interface{}) {
	t.Helper()
	// No validation needed for error scenarios
}

// S3BucketScenarios returns common S3 bucket test scenarios
//
//nolint:funlen // Comprehensive test scenarios with multiple S3 bucket configurations
func S3BucketScenarios() []S3BucketScenario {
	return []S3BucketScenario{
		{
			Name: "simple_bucket",
			Config: map[string]interface{}{
				"bucket": "test-bucket-simple",
			},
			ExpectError: false,
			Validate: func(t *testing.T, state map[string]interface{}) {
				t.Helper()
				bucketName, ok := state["bucket"].(string)
				if !ok || bucketName != "test-bucket-simple" {
					t.Errorf("expected bucket name 'test-bucket-simple', got %v", state["bucket"])
				}

				id, ok := state["id"].(string)
				if !ok || id == "" {
					t.Error("bucket should have a non-empty ID")
				}
			},
		},
		{
			Name: "bucket_with_tags",
			Config: map[string]interface{}{
				"bucket": "test-bucket-tags",
				"tags": map[string]interface{}{
					"Environment": "test",
					"ManagedBy":   "lattiam",
				},
			},
			ExpectError: false,
			Validate: func(t *testing.T, state map[string]interface{}) {
				t.Helper()
				bucketName, ok := state["bucket"].(string)
				if !ok || bucketName != "test-bucket-tags" {
					t.Errorf("expected bucket name 'test-bucket-tags', got %v", state["bucket"])
				}

				tags, ok := state["tags"].(map[string]interface{})
				if !ok {
					t.Fatal("tags should be a map")
				}

				envTag, ok := tags["Environment"].(string)
				if !ok || envTag != "test" {
					t.Errorf("expected Environment tag 'test', got %v", tags["Environment"])
				}

				managedBy, ok := tags["ManagedBy"].(string)
				if !ok || managedBy != "lattiam" {
					t.Errorf("expected ManagedBy tag 'lattiam', got %v", tags["ManagedBy"])
				}
			},
		},
		{
			Name:         "missing_bucket_name",
			Config:       map[string]interface{}{},
			ExpectError:  true,
			ErrorPattern: bucketRequiredPattern,
			Validate:     emptyValidation,
		},
		{
			Name: "invalid_bucket_name",
			Config: map[string]interface{}{
				"bucket": "INVALID_BUCKET_NAME", // S3 buckets must be lowercase
			},
			ExpectError:  true,
			ErrorPattern: invalidBucketPattern,
			Validate:     emptyValidation,
		},
		{
			Name: "bucket_name_too_long",
			Config: map[string]interface{}{
				"bucket": "this-bucket-name-is-way-too-long-and-exceeds-the-63-character-limit-for-s3-bucket-names",
			},
			ExpectError:  true,
			ErrorPattern: bucketTooLongPattern,
			Validate:     emptyValidation,
		},
	}
}

// StateTransitionScenario represents a test scenario for state transitions
type StateTransitionScenario struct {
	Name         string
	From         string
	To           string
	ShouldError  bool
	ErrorPattern *regexp.Regexp // Use compiled regex for consistency
}

// StateTransitionScenarios returns common state transition test cases
func StateTransitionScenarios() []StateTransitionScenario {
	return []StateTransitionScenario{
		// Valid forward transitions
		{"pending_to_validating", "pending", "validating", false, nil},
		{"validating_to_resolving_deps", "validating", "resolving_dependencies", false, nil},
		{"resolving_to_planning", "resolving_dependencies", "planning", false, nil},
		{"planning_to_applying", "planning", "applying", false, nil},
		{"applying_to_completed", "applying", "completed", false, nil},

		// Valid error transitions
		{"validating_to_failed", "validating", "failed", false, nil},
		{"planning_to_failed", "planning", "failed", false, nil},
		{"applying_to_failed", "applying", "failed", false, nil},

		// Invalid backward transitions
		{"completed_to_pending", "completed", "pending", true, regexp.MustCompile("invalid transition.*completed.*pending")},
		{"applying_to_validating", "applying", "validating", true, regexp.MustCompile("cannot go backward")},

		// Invalid skip transitions
		{"pending_to_completed", "pending", "completed", true, regexp.MustCompile("cannot skip states")},
		{"validating_to_applying", "validating", "applying", true, regexp.MustCompile("must go through planning")},

		// Invalid from terminal states
		{"failed_to_applying", "failed", "applying", true, regexp.MustCompile("cannot transition from failed")},
		{"completed_to_applying", "completed", "applying", true, regexp.MustCompile("cannot transition from completed")},
	}
}

// DependencyGraphScenario represents a test scenario for dependency resolution
type DependencyGraphScenario struct {
	Name        string
	Resources   []ResourceDefinition
	WantOrder   []string
	ShouldError bool
	ErrorMatch  string
}

// ResourceDefinition represents a resource for dependency tests
type ResourceDefinition struct {
	ID        string
	Type      string
	DependsOn []string
}

// DependencyGraphScenarios returns common dependency resolution test cases
func DependencyGraphScenarios() []DependencyGraphScenario {
	return []DependencyGraphScenario{
		{
			Name: "simple_chain",
			Resources: []ResourceDefinition{
				{ID: "vpc", Type: "aws_vpc", DependsOn: []string{}},
				{ID: "subnet", Type: "aws_subnet", DependsOn: []string{"vpc"}},
				{ID: "instance", Type: "aws_instance", DependsOn: []string{"subnet"}},
			},
			WantOrder:   []string{"vpc", "subnet", "instance"},
			ShouldError: false,
		},
		{
			Name: "parallel_resources",
			Resources: []ResourceDefinition{
				{ID: "bucket1", Type: "aws_s3_bucket", DependsOn: []string{}},
				{ID: "bucket2", Type: "aws_s3_bucket", DependsOn: []string{}},
				{ID: "bucket3", Type: "aws_s3_bucket", DependsOn: []string{}},
			},
			WantOrder:   []string{"bucket1", "bucket2", "bucket3"}, // Any order is valid
			ShouldError: false,
		},
		{
			Name: "multi_dependency",
			Resources: []ResourceDefinition{
				{ID: "vpc", Type: "aws_vpc", DependsOn: []string{}},
				{ID: "sg", Type: "aws_security_group", DependsOn: []string{"vpc"}},
				{ID: "subnet", Type: "aws_subnet", DependsOn: []string{"vpc"}},
				{ID: "instance", Type: "aws_instance", DependsOn: []string{"subnet", "sg"}},
			},
			WantOrder:   []string{"vpc", "sg|subnet", "instance"}, // sg and subnet can be in any order
			ShouldError: false,
		},
		{
			Name: "circular_dependency",
			Resources: []ResourceDefinition{
				{ID: "a", Type: "test", DependsOn: []string{"b"}},
				{ID: "b", Type: "test", DependsOn: []string{"c"}},
				{ID: "c", Type: "test", DependsOn: []string{"a"}},
			},
			WantOrder:   nil,
			ShouldError: true,
			ErrorMatch:  "circular dependency",
		},
		{
			Name: "missing_dependency",
			Resources: []ResourceDefinition{
				{ID: "subnet", Type: "aws_subnet", DependsOn: []string{"vpc"}},
			},
			WantOrder:   nil,
			ShouldError: true,
			ErrorMatch:  "missing dependency.*vpc",
		},
		{
			Name: "self_dependency",
			Resources: []ResourceDefinition{
				{ID: "resource", Type: "test", DependsOn: []string{"resource"}},
			},
			WantOrder:   nil,
			ShouldError: true,
			ErrorMatch:  "self-referential dependency",
		},
	}
}

// ProviderErrorScenario represents a test scenario for provider errors
type ProviderErrorScenario struct {
	Name         string
	ErrorMessage string
	Retryable    bool
	ErrorType    string // "connection", "auth", "crash", "timeout"
	MaxRetries   int
}

// ProviderErrorScenarios returns common provider error test cases
func ProviderErrorScenarios() []ProviderErrorScenario {
	return []ProviderErrorScenario{
		{
			Name:         "rpc_unavailable",
			ErrorMessage: "rpc error: code = Unavailable",
			Retryable:    true,
			ErrorType:    "connection",
			MaxRetries:   3,
		},
		{
			Name:         "connection_reset",
			ErrorMessage: "connection reset by peer",
			Retryable:    true,
			ErrorType:    "connection",
			MaxRetries:   3,
		},
		{
			Name:         "provider_crash",
			ErrorMessage: "provider process crashed",
			Retryable:    true,
			ErrorType:    "crash",
			MaxRetries:   2,
		},
		{
			Name:         "auth_failure",
			ErrorMessage: "no valid credential sources found",
			Retryable:    false,
			ErrorType:    "auth",
			MaxRetries:   0,
		},
		{
			Name:         "permission_denied",
			ErrorMessage: "AccessDenied: User is not authorized",
			Retryable:    false,
			ErrorType:    "auth",
			MaxRetries:   0,
		},
		{
			Name:         "timeout",
			ErrorMessage: "context deadline exceeded",
			Retryable:    true,
			ErrorType:    "timeout",
			MaxRetries:   1,
		},
		{
			Name:         "rate_limit",
			ErrorMessage: "rate limit exceeded",
			Retryable:    true,
			ErrorType:    "rate_limit",
			MaxRetries:   5,
		},
	}
}

// EC2InstanceScenario represents a test scenario for EC2 instance operations
type EC2InstanceScenario struct {
	Name        string
	Config      map[string]interface{}
	ExpectError bool
	ErrorMatch  string
	Validate    func(*testing.T, map[string]interface{})
}

// EC2InstanceScenarios returns common EC2 instance test scenarios
func EC2InstanceScenarios() []EC2InstanceScenario {
	return []EC2InstanceScenario{
		{
			Name: "simple_instance",
			Config: map[string]interface{}{
				"instance_type": "t2.micro",
				"ami":           "ami-12345678",
			},
			ExpectError: false,
			Validate: func(t *testing.T, state map[string]interface{}) {
				t.Helper()
				if state["instance_type"] != "t2.micro" {
					t.Errorf("expected instance_type 't2.micro', got %v", state["instance_type"])
				}
				if state["id"] == nil || state["id"] == "" {
					t.Error("instance should have an ID")
				}
			},
		},
		{
			Name: "instance_with_vpc",
			Config: map[string]interface{}{
				"instance_type":          "t2.micro",
				"ami":                    "ami-12345678",
				"subnet_id":              "subnet-12345678",
				"vpc_security_group_ids": []string{"sg-12345678"},
			},
			ExpectError: false,
			Validate: func(t *testing.T, state map[string]interface{}) {
				t.Helper()
				if state["subnet_id"] != "subnet-12345678" {
					t.Errorf("expected subnet_id 'subnet-12345678', got %v", state["subnet_id"])
				}
				sgIDs, ok := state["vpc_security_group_ids"].([]interface{})
				if !ok || len(sgIDs) != 1 {
					t.Error("expected one security group")
				}
			},
		},
		{
			Name: "missing_required_ami",
			Config: map[string]interface{}{
				"instance_type": "t2.micro",
			},
			ExpectError: true,
			ErrorMatch:  "ami.*required",
			Validate:    nil,
		},
		{
			Name: "invalid_instance_type",
			Config: map[string]interface{}{
				"instance_type": "invalid.type",
				"ami":           "ami-12345678",
			},
			ExpectError: true,
			ErrorMatch:  "invalid.*instance.*type",
			Validate:    nil,
		},
	}
}

// ComplexDeploymentScenario represents a complex multi-resource deployment
type ComplexDeploymentScenario struct {
	Name              string
	Description       string
	ResourceCount     int
	ExpectedResources []string
	ValidateFunc      func(*testing.T, []interface{})
}

// ComplexDeploymentScenarios returns scenarios for testing complex deployments
func ComplexDeploymentScenarios() []ComplexDeploymentScenario {
	return []ComplexDeploymentScenario{
		{
			Name:          "vpc_with_subnets",
			Description:   "VPC with public and private subnets",
			ResourceCount: 8,
			ExpectedResources: []string{
				"aws_vpc.main",
				"aws_internet_gateway.main",
				"aws_subnet.public",
				"aws_subnet.private",
				"aws_route_table.public",
				"aws_route.internet",
				"aws_route_table_association.public",
				"aws_nat_gateway.main",
			},
			ValidateFunc: func(t *testing.T, resources []interface{}) {
				t.Helper()
				// Validate all resources completed
				for _, res := range resources {
					resource := res.(map[string]interface{})
					if resource["status"] != "completed" {
						t.Errorf("Resource %s/%s failed with status %s",
							resource["type"], resource["name"], resource["status"])
					}
				}
			},
		},
		{
			Name:          "multi_tier_app",
			Description:   "Multi-tier application with web, app, and database tiers",
			ResourceCount: 15,
			ExpectedResources: []string{
				"aws_vpc.main",
				"aws_subnet.web", "aws_subnet.app", "aws_subnet.db",
				"aws_security_group.web", "aws_security_group.app", "aws_security_group.db",
				"aws_instance.web", "aws_instance.app",
				"aws_db_instance.main",
				"aws_lb.main",
				"aws_lb_target_group.web",
			},
			ValidateFunc: func(t *testing.T, resources []interface{}) {
				t.Helper()
				// Validate dependency order
				resourceOrder := make(map[string]int)
				for i, res := range resources {
					resource := res.(map[string]interface{})
					key := fmt.Sprintf("%s.%s", resource["type"], resource["name"])
					resourceOrder[key] = i
				}

				// VPC must come before subnets
				if resourceOrder["aws_vpc.main"] >= resourceOrder["aws_subnet.web"] {
					t.Error("VPC must be created before subnets")
				}
			},
		},
	}
}
