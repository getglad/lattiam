//go:build integration
// +build integration

package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderConfigurator_EnvironmentConfigWithEndpoints(t *testing.T) {
	t.Parallel()
	configurator := NewProviderConfigurator()

	// For LocalStack testing, we need to configure which services should use the endpoint
	// Override the default AWS config to include endpoint services
	configurator.defaultConfigs["aws"] = Defaults{
		RequiredAttributes: map[string]interface{}{
			"region": "us-east-1",
		},
		OptionalAttributes: map[string]interface{}{},
		EndpointServices:   []string{"iam", "s3", "sts"}, // Services that should use LocalStack endpoint
	}

	schema := createAWSSchemaWithEndpoints()

	envConfig := &EnvironmentConfig{
		AttributeOverrides: map[string]interface{}{
			"access_key":                  "test",
			"secret_key":                  "test",
			"skip_credentials_validation": true,
			"insecure":                    true,
		},
		EndpointURL: "http://localstack:4566",
		DisableSSL:  true,
	}

	providerConfig, err := configurator.ConfigureProvider(
		context.Background(),
		"aws",
		schema,
		map[string]interface{}{},
		envConfig,
	)

	require.NoError(t, err)

	// Verify expected attributes
	expectedAttrs := map[string]interface{}{
		"access_key":                  "test",
		"secret_key":                  "test",
		"skip_credentials_validation": true,
		"insecure":                    true,
	}
	for key, expectedValue := range expectedAttrs {
		assert.Equal(t, expectedValue, providerConfig[key], "attribute %s mismatch", key)
	}

	// Verify endpoints
	assert.Contains(t, providerConfig, "endpoints")
	endpointsList, ok := providerConfig["endpoints"].([]interface{})
	require.True(t, ok, "endpoints should be a list for AWS provider v6")
	require.Len(t, endpointsList, 1, "endpoints list should contain one element")

	endpointConfig, ok := endpointsList[0].(map[string]interface{})
	require.True(t, ok, "endpoints list should contain a map")

	expectedEndpoints := map[string]string{
		"iam": "http://localstack:4566",
		"s3":  "http://localstack:4566",
		"sts": "http://localstack:4566",
	}
	for service, expectedURL := range expectedEndpoints {
		assert.Equal(t, expectedURL, endpointConfig[service], "endpoint %s mismatch", service)
	}
}

func TestProviderConfigurator_ProductionDefaults(t *testing.T) {
	t.Parallel()
	configurator := NewProviderConfigurator()
	schema := createBasicAWSSchema()

	config, err := configurator.ConfigureProvider(
		context.Background(),
		"aws",
		schema,
		map[string]interface{}{},
		nil,
	)

	require.NoError(t, err)

	expectedAttrs := map[string]interface{}{
		"region":              "us-east-1",
		"profile":             "",
		"shared_config_files": []interface{}{},
		"max_retries":         25,
	}
	for key, expectedValue := range expectedAttrs {
		assert.Equal(t, expectedValue, config[key], "attribute %s mismatch", key)
	}
}

func TestProviderConfigurator_CustomConfigOverrides(t *testing.T) {
	t.Parallel()
	configurator := NewProviderConfigurator()
	schema := createMinimalAWSSchema()

	customConfig := map[string]interface{}{
		"region":     "eu-west-1",
		"access_key": "custom-key",
	}
	envConfig := &EnvironmentConfig{
		AttributeOverrides: map[string]interface{}{
			"access_key": "env-key",
		},
	}

	config, err := configurator.ConfigureProvider(
		context.Background(),
		"aws",
		schema,
		customConfig,
		envConfig,
	)

	require.NoError(t, err)

	expectedAttrs := map[string]interface{}{
		"region":     "eu-west-1",
		"access_key": "custom-key", // Custom wins over env
	}
	for key, expectedValue := range expectedAttrs {
		assert.Equal(t, expectedValue, config[key], "attribute %s mismatch", key)
	}
}

// Helper functions
func createAWSSchemaWithEndpoints() *tfprotov6.SchemaBlock {
	return &tfprotov6.SchemaBlock{
		Attributes: []*tfprotov6.SchemaAttribute{
			{Name: "region", Required: true},
			{Name: "access_key", Optional: true},
			{Name: "secret_key", Optional: true},
			{Name: "skip_credentials_validation", Optional: true},
		},
		BlockTypes: []*tfprotov6.SchemaNestedBlock{
			{
				TypeName: "endpoints",
				Block: &tfprotov6.SchemaBlock{
					Attributes: []*tfprotov6.SchemaAttribute{
						{Name: "iam", Optional: true},
						{Name: "s3", Optional: true},
						{Name: "sts", Optional: true},
					},
				},
			},
		},
	}
}

func createBasicAWSSchema() *tfprotov6.SchemaBlock {
	return &tfprotov6.SchemaBlock{
		Attributes: []*tfprotov6.SchemaAttribute{
			{Name: "region", Required: true},
			{Name: "profile", Optional: true},
			{Name: "shared_config_files", Optional: true},
			{Name: "max_retries", Optional: true},
		},
	}
}

func createMinimalAWSSchema() *tfprotov6.SchemaBlock {
	return &tfprotov6.SchemaBlock{
		Attributes: []*tfprotov6.SchemaAttribute{
			{Name: "region", Required: true},
			{Name: "access_key", Optional: true},
		},
	}
}
