package deployment

import (
	"testing"
)

//nolint:gocognit,gocyclo // comprehensive dependency testing
func TestAnalyzeResourceDependencies(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		resources []Resource
		// Instead of exact order, validate dependency constraints
		validateOrder func(t *testing.T, ordered []Resource)
	}{
		{
			name: "simple dependency chain",
			resources: []Resource{
				{
					Type: "aws_security_group",
					Name: "sg",
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
			},
			validateOrder: func(t *testing.T, ordered []Resource) {
				t.Helper()
				// Build a position map
				pos := make(map[string]int)
				for i, r := range ordered {
					pos[r.Name] = i
				}

				// Validate: main must come before sg
				if pos["main"] >= pos["sg"] {
					t.Errorf("Expected 'main' before 'sg', but got positions %d and %d", pos["main"], pos["sg"])
				}
			},
		},
		{
			name: "multiple dependencies",
			resources: []Resource{
				{
					Type: "aws_instance",
					Name: "web",
					Properties: map[string]interface{}{
						"subnet_id":       "${aws_subnet.public.id}",
						"security_groups": []interface{}{"${aws_security_group.web.id}"},
					},
				},
				{
					Type: "aws_subnet",
					Name: "public",
					Properties: map[string]interface{}{
						"vpc_id": "${aws_vpc.main.id}",
					},
				},
				{
					Type: "aws_security_group",
					Name: "web",
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
			},
			validateOrder: func(t *testing.T, ordered []Resource) {
				t.Helper()
				// Build a position map
				pos := make(map[string]int)
				for i, r := range ordered {
					key := r.Name
					// Handle duplicate names (like "web" for both instance and security group)
					if r.Type == "aws_instance" && r.Name == "web" {
						key = "web-instance"
					} else if r.Type == "aws_security_group" && r.Name == "web" {
						key = "web-sg"
					}
					pos[key] = i
				}

				// Validate dependency constraints:
				// 1. main must come before public and web-sg
				if pos["main"] >= pos["public"] {
					t.Errorf("Expected 'main' before 'public', but got positions %d and %d", pos["main"], pos["public"])
				}
				if pos["main"] >= pos["web-sg"] {
					t.Errorf("Expected 'main' before 'web-sg', but got positions %d and %d", pos["main"], pos["web-sg"])
				}

				// 2. public must come before web-instance
				if pos["public"] >= pos["web-instance"] {
					t.Errorf("Expected 'public' before 'web-instance', but got positions %d and %d", pos["public"], pos["web-instance"])
				}

				// 3. web-sg must come before web-instance
				if pos["web-sg"] >= pos["web-instance"] {
					t.Errorf("Expected 'web-sg' before 'web-instance', but got positions %d and %d", pos["web-sg"], pos["web-instance"])
				}

				// Note: The relative order of 'public' and 'web-sg' is not constrained
				// since they both only depend on 'main'. Either order is valid.
			},
		},
		{
			name: "no dependencies",
			resources: []Resource{
				{
					Type: "aws_s3_bucket",
					Name: "bucket1",
					Properties: map[string]interface{}{
						"bucket": "my-bucket-1",
					},
				},
				{
					Type: "aws_s3_bucket",
					Name: "bucket2",
					Properties: map[string]interface{}{
						"bucket": "my-bucket-2",
					},
				},
			},
			validateOrder: func(t *testing.T, ordered []Resource) {
				t.Helper()
				// For resources with no dependencies, just check they are all present
				if len(ordered) != 2 {
					t.Errorf("Expected 2 resources, got %d", len(ordered))
				}

				found := make(map[string]bool)
				for _, r := range ordered {
					found[r.Name] = true
				}

				if !found["bucket1"] || !found["bucket2"] {
					t.Errorf("Expected both bucket1 and bucket2 to be present")
				}
			},
		},
		{
			name: "nested property dependencies",
			resources: []Resource{
				{
					Type: "aws_iam_role_policy",
					Name: "policy",
					Properties: map[string]interface{}{
						"role": "${aws_iam_role.role.name}",
						"policy": map[string]interface{}{
							"Statement": []interface{}{
								map[string]interface{}{
									"Resource": "${aws_s3_bucket.data.arn}",
								},
							},
						},
					},
				},
				{
					Type: "aws_iam_role",
					Name: "role",
					Properties: map[string]interface{}{
						"name": "my-role",
					},
				},
				{
					Type: "aws_s3_bucket",
					Name: "data",
					Properties: map[string]interface{}{
						"bucket": "my-data-bucket",
					},
				},
			},
			validateOrder: func(t *testing.T, ordered []Resource) {
				t.Helper()
				// Build a position map
				pos := make(map[string]int)
				for i, r := range ordered {
					pos[r.Name] = i
				}

				// Validate: both role and data must come before policy
				if pos["role"] >= pos["policy"] {
					t.Errorf("Expected 'role' before 'policy', but got positions %d and %d", pos["role"], pos["policy"])
				}
				if pos["data"] >= pos["policy"] {
					t.Errorf("Expected 'data' before 'policy', but got positions %d and %d", pos["data"], pos["policy"])
				}

				// Note: The relative order of 'role' and 'data' is not constrained
				// since neither depends on the other. Either order is valid.
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ordered := AnalyzeResourceDependencies(tt.resources)

			// Validate the order using the test-specific validation function
			tt.validateOrder(t, ordered)
		})
	}
}

func TestFindInterpolationDependencies(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		properties map[string]interface{}
		expected   []string
	}{
		{
			name: "simple interpolation",
			properties: map[string]interface{}{
				"vpc_id": "${aws_vpc.main.id}",
			},
			expected: []string{"aws_vpc/main"},
		},
		{
			name: "multiple interpolations",
			properties: map[string]interface{}{
				"vpc_id":    "${aws_vpc.main.id}",
				"subnet_id": "${aws_subnet.public.id}",
			},
			expected: []string{"aws_vpc/main", "aws_subnet/public"},
		},
		{
			name: "nested interpolations",
			properties: map[string]interface{}{
				"policy": map[string]interface{}{
					"Statement": []interface{}{
						map[string]interface{}{
							"Resource": "${aws_s3_bucket.data.arn}",
						},
					},
				},
			},
			expected: []string{"aws_s3_bucket/data"},
		},
		{
			name: "no interpolations",
			properties: map[string]interface{}{
				"name": "my-resource",
				"tags": map[string]interface{}{
					"Environment": "production",
				},
			},
			expected: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			deps := FindInterpolationDependencies(tt.properties)

			// Sort for consistent comparison
			if len(deps) != len(tt.expected) {
				t.Errorf("Expected %d dependencies, got %d", len(tt.expected), len(deps))
			}

			// Check all expected dependencies are present
			depMap := make(map[string]bool)
			for _, d := range deps {
				depMap[d] = true
			}

			for _, exp := range tt.expected {
				if !depMap[exp] {
					t.Errorf("Expected dependency %s not found", exp)
				}
			}
		})
	}
}
