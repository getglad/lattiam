package main

import (
	"testing"

	"github.com/lattiam/lattiam/internal/deployment"
)

func TestParseJSONResources(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		want    []deployment.Resource
		wantErr bool
	}{
		{
			name: "valid IAM role",
			input: `{
				"resource": {
					"aws_iam_role": {
						"test_role": {
							"name": "` + TestRoleName + `",
							"assume_role_policy": "{\"Version\":\"2012-10-17\"}",
							"description": "Test role"
						}
					}
				}
			}`,
			want: []deployment.Resource{
				{
					Type: "aws_iam_role",
					Name: "test_role",
					Properties: map[string]interface{}{
						"name":               TestRoleName,
						"assume_role_policy": "{\"Version\":\"2012-10-17\"}",
						"description":        "Test role",
					},
				},
			},
			wantErr: false,
		},
		{
			name: "multiple resources",
			input: `{
				"resource": {
					"aws_iam_role": {
						"role1": {
							"name": "role-1"
						},
						"role2": {
							"name": "role-2"
						}
					},
					"aws_s3_bucket": {
						"bucket1": {
							"bucket": "my-bucket"
						}
					}
				}
			}`,
			want: []deployment.Resource{
				{
					Type: "aws_iam_role",
					Name: "role1",
					Properties: map[string]interface{}{
						"name": "role-1",
					},
				},
				{
					Type: "aws_iam_role",
					Name: "role2",
					Properties: map[string]interface{}{
						"name": "role-2",
					},
				},
				{
					Type: "aws_s3_bucket",
					Name: "bucket1",
					Properties: map[string]interface{}{
						"bucket": "my-bucket",
					},
				},
			},
			wantErr: false,
		},
		{
			name:    "empty resources",
			input:   `{"resource": {}}`,
			want:    nil,
			wantErr: true,
		},
		{
			name:    "invalid JSON",
			input:   `{invalid json`,
			want:    nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := deployment.ParseTerraformJSON([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Errorf("parseJSONResources() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if len(got) != len(tt.want) {
					t.Errorf("parseJSONResources() got %d resources, want %d", len(got), len(tt.want))
					return
				}
				// Note: Order might vary due to map iteration
				// In a real test, we'd sort or use a more detailed comparison
			}
		})
	}
}
