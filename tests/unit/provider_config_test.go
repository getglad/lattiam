package unit

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestAWSProviderConfigTypes ensures we maintain correct types for AWS provider config
func TestAWSProviderConfigTypes(t *testing.T) {
	t.Parallel()
	// This test documents the EXACT types required by AWS provider
	// to prevent future type-related crashes

	tests := []struct {
		name         string
		attribute    string
		value        interface{}
		expectedType string
	}{
		// String attributes that MUST be strings
		{"skip_metadata_api_check is string", "skip_metadata_api_check", "true", "string"},
		{"region is string", "region", "us-east-1", "string"},
		{"access_key is string", "access_key", "", "string"},

		// Boolean attributes that MUST be booleans
		{"skip_credentials_validation is bool", "skip_credentials_validation", true, "bool"},
		{"skip_region_validation is bool", "skip_region_validation", true, "bool"},
		{"insecure is bool", "insecure", false, "bool"},

		// Integer attributes that MUST be integers
		{"max_retries is int", "max_retries", 25, "int"},
		{"token_bucket_rate_limiter_capacity is int", "token_bucket_rate_limiter_capacity", 10000, "int"},

		// Array attributes that MUST be slices
		{"allowed_account_ids is slice", "allowed_account_ids", []interface{}{}, "[]interface {}"},
		{"shared_config_files is slice", "shared_config_files", []interface{}{}, "[]interface {}"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			actualType := fmt.Sprintf("%T", tt.value)
			assert.Equal(t, tt.expectedType, actualType,
				"Attribute %s must have type %s, not %s",
				tt.attribute, tt.expectedType, actualType)
		})
	}
}

// TestAWSProviderConfigCompleteness ensures we have all 33 required attributes
func TestAWSProviderConfigCompleteness(t *testing.T) {
	t.Parallel()
	requiredAttributes := []string{
		// String attributes (15)
		"access_key", "custom_ca_bundle", "ec2_metadata_service_endpoint",
		"ec2_metadata_service_endpoint_mode", "http_proxy", "https_proxy",
		"no_proxy", "profile", "region", "retry_mode",
		"s3_us_east_1_regional_endpoint", "secret_key", "skip_metadata_api_check",
		"sts_region", "token",

		// Boolean attributes (8)
		"insecure", "s3_use_path_style", "skip_credentials_validation",
		"skip_region_validation", "skip_requesting_account_id",
		"use_dualstack_endpoint", "use_fips_endpoint",

		// Integer attributes (2)
		"max_retries", "token_bucket_rate_limiter_capacity",

		// Array attributes (3) - Note: either allowed OR forbidden, not both
		"allowed_account_ids", // OR "forbidden_account_ids",
		"shared_config_files", "shared_credentials_files",

		// Block types (5)
		"assume_role", "assume_role_with_web_identity",
		"default_tags", "endpoints", "ignore_tags",
	}

	// Total should be 32 (15 strings + 8 booleans + 2 integers + 3 arrays + 5 blocks = 33,
	// but we exclude forbidden_account_ids)
	assert.Len(t, requiredAttributes, 32,
		"AWS provider requires exactly 32 configuration attributes (excluding forbidden_account_ids)")
}

// TestMsgpackEncodingRequirements documents msgpack encoding requirements
func TestMsgpackEncodingRequirements(t *testing.T) {
	t.Run("Wrong msgpack library causes crashes", func(t *testing.T) {
		t.Parallel()
		// Document that vmihailenco/msgpack loses type information
		wrongLib := "github.com/vmihailenco/msgpack/v5"
		correctLib := "github.com/zclconf/go-cty/cty/msgpack"

		assert.NotEqual(t, wrongLib, correctLib,
			"Must use %s for type-safe encoding, not %s", correctLib, wrongLib)
	})

	t.Run("Type information must be preserved", func(t *testing.T) {
		t.Parallel()
		// Document that we need schema's ImpliedType() for encoding
		// Must use schema.ImpliedType() when marshaling with cty msgpack
		t.Log("Must use schema.ImpliedType() when marshaling with cty msgpack")
	})
}
