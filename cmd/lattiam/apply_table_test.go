package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/deployment"
)

func TestParseJSONResourcesBasic(t *testing.T) {
	t.Run("valid single IAM role", func(t *testing.T) {
		t.Parallel()
		input := `{
			"resource": {
				"aws_iam_role": {
					"test_role": {
						"name": "test-role",
						"assume_role_policy": "{\"Version\":\"2012-10-17\"}"
					}
				}
			}
		}`
		want := []deployment.Resource{
			{
				Type: "aws_iam_role",
				Name: "test_role",
				Properties: map[string]interface{}{
					"name":               "test-role",
					"assume_role_policy": "{\"Version\":\"2012-10-17\"}",
				},
			},
		}

		got, err := deployment.ParseTerraformJSON([]byte(input))
		require.NoError(t, err)
		assert.Equal(t, want, got)
	})

	t.Run("multiple resources of different types", func(t *testing.T) {
		t.Parallel()
		input := `{
			"resource": {
				"aws_iam_role": {
					"role1": {"name": "role-1"}
				},
				"aws_s3_bucket": {
					"bucket1": {"bucket": "my-bucket"}
				}
			}
		}`

		got, err := deployment.ParseTerraformJSON([]byte(input))
		require.NoError(t, err)
		assert.Len(t, got, 2)
	})

	t.Run("multiple resources of same type", func(t *testing.T) {
		t.Parallel()
		input := `{
			"resource": {
				"aws_iam_role": {
					"role1": {"name": "role-1"},
					"role2": {"name": "role-2"}
				}
			}
		}`

		got, err := deployment.ParseTerraformJSON([]byte(input))
		require.NoError(t, err)
		assert.Len(t, got, 2)
	})
}

func TestParseJSONResourcesErrors(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name        string
		input       string
		errContains string
	}{
		{
			name:        "empty resources",
			input:       `{"resource": {}}`,
			errContains: "no resources found",
		},
		{
			name:        "invalid JSON",
			input:       `{"resource": invalid}`,
			errContains: "invalid character",
		},
		{
			name:        "missing resource key",
			input:       `{"not_resource": {}}`,
			errContains: "no resources found",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := deployment.ParseTerraformJSON([]byte(tc.input))
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.errContains)
		})
	}
}

func TestParseJSONComplexResources(t *testing.T) {
	t.Parallel()
	input := `{
		"resource": {
			"aws_instance": {
				"web": {
					"ami": "ami-123456",
					"instance_type": "t2.micro",
					"tags": {
						"Name": "WebServer",
						"Environment": "Test"
					}
				}
			}
		}
	}`

	got, err := deployment.ParseTerraformJSON([]byte(input))
	require.NoError(t, err)
	require.Len(t, got, 1)

	resource := got[0]
	assert.Equal(t, "aws_instance", resource.Type)
	assert.Equal(t, "web", resource.Name)
	assert.Equal(t, "ami-123456", resource.Properties["ami"])
}
