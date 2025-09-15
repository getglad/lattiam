//go:build !integration
// +build !integration

package interpolation

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//nolint:funlen // Comprehensive interpolation test covering multiple variable types
func TestHCLInterpolationResolver_BasicVariables(t *testing.T) {
	t.Parallel()
	resolver := NewHCLInterpolationResolver(false)

	t.Run("SimpleVariableReference", func(t *testing.T) {
		t.Parallel()
		properties := map[string]interface{}{
			"name": "${var.project_name}",
		}
		context := map[string]map[string]interface{}{
			"var": {
				"project_name": "test-project",
			},
		}

		result, err := resolver.ResolveInterpolations(properties, context)
		require.NoError(t, err)
		assert.Equal(t, "test-project", result["name"])
	})

	t.Run("ResourceReference", func(t *testing.T) {
		t.Parallel()
		properties := map[string]interface{}{
			"vpc_id": "${aws_vpc.main.id}",
		}
		context := map[string]map[string]interface{}{
			"aws_vpc": {
				"main": map[string]interface{}{
					"id": "vpc-12345",
				},
			},
		}

		result, err := resolver.ResolveInterpolations(properties, context)
		require.NoError(t, err)
		assert.Equal(t, "vpc-12345", result["vpc_id"])
	})

	t.Run("NestedProperties", func(t *testing.T) {
		t.Parallel()
		properties := map[string]interface{}{
			"config": map[string]interface{}{
				"region": "${var.region}",
				"bucket": "${aws_s3_bucket.main.id}",
			},
		}
		context := map[string]map[string]interface{}{
			"var": {
				"region": "us-west-2",
			},
			"aws_s3_bucket": {
				"main": map[string]interface{}{
					"id": "my-bucket",
				},
			},
		}

		result, err := resolver.ResolveInterpolations(properties, context)
		require.NoError(t, err)

		config := result["config"].(map[string]interface{})
		assert.Equal(t, "us-west-2", config["region"])
		assert.Equal(t, "my-bucket", config["bucket"])
	})
}

func TestHCLInterpolationResolver_Functions(t *testing.T) {
	t.Parallel()
	resolver := NewHCLInterpolationResolver(false)

	t.Run("FormatFunction", func(t *testing.T) {
		t.Parallel()
		properties := map[string]interface{}{
			"name": "${format(\"bucket-%s\", var.suffix)}",
		}
		context := map[string]map[string]interface{}{
			"var": {
				"suffix": "test",
			},
		}

		result, err := resolver.ResolveInterpolations(properties, context)
		require.NoError(t, err)
		assert.Equal(t, "bucket-test", result["name"])
	})

	t.Run("UUIDFunction", func(t *testing.T) {
		t.Parallel()
		properties := map[string]interface{}{
			"id": "${uuid()}",
		}
		context := map[string]map[string]interface{}{}

		result, err := resolver.ResolveInterpolations(properties, context)
		require.NoError(t, err)

		// UUID should be a valid string
		id, ok := result["id"].(string)
		assert.True(t, ok)
		assert.Len(t, id, 36) // Standard UUID length with hyphens
	})

	t.Run("LengthFunction", func(t *testing.T) {
		t.Parallel()
		properties := map[string]interface{}{
			"count": "${length(var.items)}",
		}
		context := map[string]map[string]interface{}{
			"var": {
				"items": []interface{}{"a", "b", "c"},
			},
		}

		result, err := resolver.ResolveInterpolations(properties, context)
		require.NoError(t, err)
		assert.Equal(t, int64(3), result["count"])
	})
}

func TestHCLInterpolationResolver_ErrorHandling(t *testing.T) {
	t.Parallel()

	t.Run("StrictMode", func(t *testing.T) {
		t.Parallel()
		resolver := NewHCLInterpolationResolver(true) // strict mode

		properties := map[string]interface{}{
			"name": "${var.undefined_variable}",
		}
		context := map[string]map[string]interface{}{}

		_, err := resolver.ResolveInterpolations(properties, context)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "Unknown variable")
	})

	t.Run("NonStrictMode", func(t *testing.T) {
		t.Parallel()
		resolver := NewHCLInterpolationResolver(false) // non-strict mode

		properties := map[string]interface{}{
			"name": "${var.undefined_variable}",
		}
		context := map[string]map[string]interface{}{}

		result, err := resolver.ResolveInterpolations(properties, context)
		require.NoError(t, err)
		// In non-strict mode, undefined variables are left as-is
		assert.Equal(t, "${var.undefined_variable}", result["name"])
	})

	t.Run("InvalidSyntax", func(t *testing.T) {
		t.Parallel()
		resolver := NewHCLInterpolationResolver(true) // use strict mode for syntax errors

		properties := map[string]interface{}{
			"name": "${invalid..syntax}",
		}
		context := map[string]map[string]interface{}{}

		_, err := resolver.ResolveInterpolations(properties, context)
		require.Error(t, err)
	})
}
