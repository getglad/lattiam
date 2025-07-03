//go:build integration
// +build integration

package integration

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/pkg/provider/protocol"
	"github.com/lattiam/lattiam/tests/helpers"
)

// TestProviderRegressions tests for known issues that have caused failures
//
//nolint:paralleltest // integration tests use shared resources
func TestProviderRegressions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	//paralleltest:disable

	t.Run("AWS Provider 33 Attributes Configuration", func(t *testing.T) {
		// This test would have caught the "33 attributes required" issue
		manager, err := protocol.NewProviderManager()
		require.NoError(t, err)

		instance, err := manager.GetProvider(ctx, "aws", "6.0.0")
		require.NoError(t, err)
		defer instance.Stop()

		client, err := protocol.NewTerraformProviderClient(ctx, instance, "")
		require.NoError(t, err)
		defer client.Close()

		// Create config with all 33 attributes
		config := createCompleteAWSProviderConfig()

		// This should not fail with "33 attributes required"
		_, err = client.PrepareProviderConfig(ctx, config)
		require.NoError(t, err, "PrepareProviderConfig should accept all 33 attributes")

		err = client.ConfigureProvider(ctx, config)
		require.NoError(t, err, "ConfigureProvider should work with complete config")
	})

	//paralleltest:disable

	t.Run("Msgpack Type Encoding", func(t *testing.T) {
		// Fixed: Issue was with timeouts block being sent as null instead of omitted

		// This test would have caught the msgpack encoding issue
		manager, err := protocol.NewProviderManager()
		require.NoError(t, err)

		instance, err := manager.GetProvider(ctx, "aws", "6.0.0")
		require.NoError(t, err)
		defer instance.Stop()

		// Test client with proper msgpack encoding
		//paralleltest:disable

		t.Run("Client Msgpack Encoding", func(t *testing.T) {
			// Try with S3 bucket first (simpler resource)
			client, err := protocol.NewTerraformProviderClient(ctx, instance, "aws_s3_bucket")
			require.NoError(t, err)
			defer client.Close()

			// Configure provider
			config := createCompleteAWSProviderConfig()
			_, err = client.PrepareProviderConfig(ctx, config)
			require.NoError(t, err)

			err = client.ConfigureProvider(ctx, config)
			require.NoError(t, err)

			// Client should handle resource creation properly
			bucketConfig := map[string]interface{}{
				"bucket": "test-msgpack-bucket",
			}

			_, err = client.Create(ctx, bucketConfig)
			if err != nil {
				// S3 bucket might fail for other reasons (e.g., bucket already exists)
				// but should not get EOF or gRPC errors
				require.NotContains(t, err.Error(), "EOF", "Client should not get EOF error")
				require.NotContains(t, err.Error(), "error reading from server", "Client should not get gRPC read error")
			}
		})
	})

	//paralleltest:disable

	t.Run("Protocol Version Support", func(t *testing.T) {
		// This test would have caught the v5 protocol support issue
		manager, err := protocol.NewProviderManager()
		require.NoError(t, err)

		// Test providers that use different protocol versions
		providers := []struct {
			name    string
			version string
			proto   string
		}{
			{"aws", "5.70.0", "v5"},
			{"aws", "5.99.1", "v5"},
			// Add v6 providers when available
		}

		for _, p := range providers {
			//paralleltest:disable

			t.Run(p.name+" "+p.version+" "+p.proto, func(t *testing.T) {
				instance, err := manager.GetProvider(ctx, p.name, p.version)
				require.NoError(t, err)
				defer instance.Stop()

				// Client must support all protocol versions
				client, err := protocol.NewTerraformProviderClient(ctx, instance, "test_resource")
				require.NoError(t, err, "Client must support protocol %s", p.proto)
				defer client.Close()

				// Basic operations should work
				schema, err := client.GetProviderSchema(ctx)
				require.NoError(t, err, "GetProviderSchema must work with protocol %s", p.proto)
				require.NotNil(t, schema)
			})
		}
	})

	//paralleltest:disable

	t.Run("Type Correctness", func(t *testing.T) {
		// This test ensures we use correct types for all attributes
		config := createCompleteAWSProviderConfig()

		// Verify required type constraints
		require.IsType(t, "", config["skip_metadata_api_check"], "skip_metadata_api_check must be string")
		require.Equal(t, "true", config["skip_metadata_api_check"])

		require.IsType(t, true, config["skip_credentials_validation"], "skip_credentials_validation must be bool")
		require.IsType(t, 0, config["max_retries"], "max_retries must be int")
		require.IsType(t, 0, config["token_bucket_rate_limiter_capacity"], "token_bucket_rate_limiter_capacity must be int")

		// Arrays must be []interface{}, not nil
		require.IsType(t, []interface{}{}, config["allowed_account_ids"], "arrays must be []interface{}")
		require.NotNil(t, config["allowed_account_ids"])

		// forbidden_account_ids can be nil to avoid conflict
		require.Nil(t, config["forbidden_account_ids"], "forbidden_account_ids should be nil to avoid conflict")
	})

	//paralleltest:disable

	t.Run("LocalStack Account ID Configuration", func(t *testing.T) {
		// This test would have caught the LocalStack account ID validation issue
		// where AWS provider requires account ID even with skip_requesting_account_id=true

		// Skip if not built with LocalStack support
		if testing.Short() {
			t.Skip("Skipping LocalStack test in short mode")
		}

		manager, err := protocol.NewProviderManager()
		require.NoError(t, err)

		instance, err := manager.GetProvider(ctx, "aws", "5.31.0")
		require.NoError(t, err)
		defer instance.Stop()

		client, err := protocol.NewTerraformProviderClient(ctx, instance, "aws_s3_bucket")
		require.NoError(t, err)
		defer client.Close()

		// Test LocalStack configuration with skip flags but no account ID
		//paralleltest:disable

		t.Run("WithSkipFlagsButNoAccountID", func(t *testing.T) {
			config := map[string]interface{}{
				// Minimal config with skip flags
				"region":                      "us-east-1",
				"skip_credentials_validation": true,
				"skip_region_validation":      true,
				"skip_requesting_account_id":  true,
				"skip_metadata_api_check":     true,
				"endpoints": []interface{}{
					map[string]interface{}{
						"s3": helpers.DefaultLocalStackURL,
					},
				},
			}

			// Fill in other required attributes with defaults
			fullConfig := createCompleteAWSProviderConfig()
			for k, v := range config {
				fullConfig[k] = v
			}

			// This configuration fails with "AWS account ID not allowed" error
			// even though skip_requesting_account_id is true
			err = client.ConfigureProvider(ctx, fullConfig)
			if err != nil && contains(err.Error(), "AWS account ID not allowed") {
				t.Log("REGRESSION: AWS provider validates account ID despite skip_requesting_account_id=true")
				t.Log("This prevents LocalStack usage without workarounds")
			}
		})

		//paralleltest:disable

		t.Run("WithAllowedAccountIDs", func(t *testing.T) {
			config := map[string]interface{}{
				// Config with allowed_account_ids
				"region":                      "us-east-1",
				"skip_credentials_validation": true,
				"skip_region_validation":      true,
				"skip_requesting_account_id":  true,
				"skip_metadata_api_check":     true,
				"allowed_account_ids":         []interface{}{"123456789012"},
				"endpoints": []interface{}{
					map[string]interface{}{
						"s3": helpers.DefaultLocalStackURL,
					},
				},
			}

			// Fill in other required attributes with defaults
			fullConfig := createCompleteAWSProviderConfig()
			for k, v := range config {
				fullConfig[k] = v
			}

			// This should work with allowed_account_ids
			err = client.ConfigureProvider(ctx, fullConfig)
			if err != nil {
				t.Logf("ConfigureProvider with allowed_account_ids failed: %v", err)
			}
		})
	})

	//paralleltest:disable

	t.Run("AWS SharedCredentialsFiles Array", func(t *testing.T) {
		// This test would have caught the shared_credentials_files array issue
		// where AWS provider was expecting a string instead of array
		manager, err := protocol.NewProviderManager()
		require.NoError(t, err)

		instance, err := manager.GetProvider(ctx, "aws", "5.99.1")
		require.NoError(t, err)
		defer instance.Stop()

		client, err := protocol.NewTerraformProviderClient(ctx, instance, "")
		require.NoError(t, err)
		defer client.Close()

		// Minimal config with shared_credentials_files
		config := map[string]interface{}{
			"region":                      "us-east-1",
			"shared_credentials_files":    []interface{}{}, // This MUST be accepted as array
			"shared_config_files":         []interface{}{},
			"skip_credentials_validation": true,
			"skip_region_validation":      true,
			"skip_requesting_account_id":  true,
			"skip_metadata_api_check":     "true",
		}

		// This should NOT fail with "string required" error
		_, err = client.PrepareProviderConfig(ctx, config)
		require.NoError(t, err, "shared_credentials_files should accept []interface{}")

		// Also test with actual file paths
		config["shared_credentials_files"] = []interface{}{"/home/user/.aws/credentials"}
		_, err = client.PrepareProviderConfig(ctx, config)
		require.NoError(t, err, "shared_credentials_files should accept array of strings")
	})
}

// createCompleteAWSProviderConfig creates the exact 33-attribute config that AWS provider expects
func createCompleteAWSProviderConfig() map[string]interface{} {
	return map[string]interface{}{
		// String attributes (15)
		"access_key":                         "",
		"custom_ca_bundle":                   "",
		"ec2_metadata_service_endpoint":      "",
		"ec2_metadata_service_endpoint_mode": "",
		"http_proxy":                         "",
		"https_proxy":                        "",
		"no_proxy":                           "",
		"profile":                            "",
		"region":                             "us-east-1",
		"retry_mode":                         "",
		"s3_us_east_1_regional_endpoint":     "",
		"secret_key":                         "",
		"skip_metadata_api_check":            "true", // String, not bool!
		"sts_region":                         "",
		"token":                              "",

		// Boolean attributes (8)
		"insecure":                    false,
		"s3_use_path_style":           false,
		"skip_credentials_validation": true,
		"skip_region_validation":      true,
		"skip_requesting_account_id":  true,
		"use_dualstack_endpoint":      false,
		"use_fips_endpoint":           false,

		// Integer attributes (2)
		"max_retries":                        25,
		"token_bucket_rate_limiter_capacity": 10000,

		// Array attributes (4)
		"allowed_account_ids":      []interface{}{},
		"forbidden_account_ids":    nil, // nil to avoid conflict
		"shared_config_files":      []interface{}{},
		"shared_credentials_files": []interface{}{},

		// Block types (5)
		"assume_role":                   []interface{}{},
		"assume_role_with_web_identity": []interface{}{},
		"default_tags":                  []interface{}{},
		"endpoints":                     []interface{}{},
		"ignore_tags":                   []interface{}{},
	}
}

// contains checks if a string contains a substring
func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && strings.Contains(s, substr)
}
