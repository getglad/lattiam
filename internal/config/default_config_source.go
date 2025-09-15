package config

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/internal/provider"
)

const (
	trueValue = "true"
)

// DefaultConfigSource provides production configuration for providers.
// It reads from standard environment variables and applies sensible defaults,
// but has no awareness of testing infrastructure like LocalStack.
type DefaultConfigSource struct {
	configurator *provider.Configurator
}

// NewDefaultConfigSource creates a new production configuration source
func NewDefaultConfigSource() *DefaultConfigSource {
	return &DefaultConfigSource{
		configurator: provider.NewProviderConfigurator(),
	}
}

// GetProviderConfig returns configuration for the specified provider
func (dcs *DefaultConfigSource) GetProviderConfig(
	ctx context.Context,
	providerName string,
	_ string,
	schema *tfprotov6.SchemaBlock,
) (interfaces.ProviderConfig, error) {
	// Build configuration based purely on schema and environment variables
	// No detection of testing infrastructure
	config := make(map[string]interface{})

	// Only use configurator if schema is provided
	if schema != nil {
		// Let the configurator build schema-based configuration
		baseConfig, err := dcs.configurator.ConfigureProvider(
			ctx,
			providerName,
			schema,
			nil, // No custom config at this layer
			nil, // No environment-specific overrides
		)
		if err != nil {
			return nil, fmt.Errorf("failed to configure provider: %w", err)
		}

		// Copy base configuration
		for k, v := range baseConfig {
			config[k] = v
		}
	}

	// Apply standard environment variables for AWS provider
	if providerName == "aws" || strings.Contains(providerName, "aws") {
		dcs.applyAWSEnvironment(config)
	}

	// Return provider config in the expected format
	return interfaces.ProviderConfig{
		providerName: config,
	}, nil
}

// GetEnvironmentConfig returns environment variables for the provider process
func (dcs *DefaultConfigSource) GetEnvironmentConfig(
	_ context.Context,
	_ string,
) (map[string]string, error) {
	env := make(map[string]string)

	// Pass through standard AWS environment variables
	awsEnvVars := []string{
		"AWS_REGION",
		"AWS_DEFAULT_REGION",
		"AWS_ACCESS_KEY_ID",
		"AWS_SECRET_ACCESS_KEY",
		"AWS_SESSION_TOKEN",
		"AWS_PROFILE",
		"AWS_CONFIG_FILE",
		"AWS_SHARED_CREDENTIALS_FILE",
		"AWS_ROLE_ARN",
		"AWS_ROLE_SESSION_NAME",
		"AWS_EXTERNAL_ID",
		"AWS_MFA_SERIAL",
		"AWS_STS_REGIONAL_ENDPOINTS",
		"AWS_SDK_LOAD_CONFIG",
		"AWS_CA_BUNDLE",
		"AWS_METADATA_SERVICE_TIMEOUT",
		"AWS_METADATA_SERVICE_NUM_ATTEMPTS",
		"AWS_EC2_METADATA_SERVICE_ENDPOINT",
		"AWS_EC2_METADATA_SERVICE_ENDPOINT_MODE",
		"AWS_EC2_METADATA_DISABLED",
		"AWS_ECS_SERVICE_CONNECT_ENDPOINT",
		"AWS_USE_FIPS_ENDPOINT",
		"AWS_USE_DUALSTACK_ENDPOINT",
		"AWS_CONTAINER_CREDENTIALS_FULL_URI",
		"AWS_CONTAINER_CREDENTIALS_RELATIVE_URI",
		"AWS_CONTAINER_AUTHORIZATION_TOKEN",
		"AWS_CONTAINER_AUTHORIZATION_TOKEN_FILE",
	}

	for _, key := range awsEnvVars {
		if value := os.Getenv(key); value != "" {
			env[key] = value
		}
	}

	// Add standard proxy settings
	proxyVars := []string{
		"HTTP_PROXY",
		"HTTPS_PROXY",
		"NO_PROXY",
		"http_proxy",
		"https_proxy",
		"no_proxy",
	}

	for _, key := range proxyVars {
		if value := os.Getenv(key); value != "" {
			env[key] = value
		}
	}

	return env, nil
}

// applyAWSEnvironment applies standard AWS environment configuration
func (dcs *DefaultConfigSource) applyAWSEnvironment(config map[string]interface{}) {
	// Region configuration
	if region := os.Getenv("AWS_REGION"); region != "" {
		config["region"] = region
	} else if region := os.Getenv("AWS_DEFAULT_REGION"); region != "" {
		config["region"] = region
	}

	// Credentials configuration
	if accessKey := os.Getenv("AWS_ACCESS_KEY_ID"); accessKey != "" {
		config["access_key"] = accessKey
	}
	if secretKey := os.Getenv("AWS_SECRET_ACCESS_KEY"); secretKey != "" {
		config["secret_key"] = secretKey
	}
	if token := os.Getenv("AWS_SESSION_TOKEN"); token != "" {
		config["token"] = token
	}

	// Profile configuration
	if profile := os.Getenv("AWS_PROFILE"); profile != "" {
		config["profile"] = profile
	}

	// Assume role configuration
	if roleArn := os.Getenv("AWS_ROLE_ARN"); roleArn != "" {
		// AWS provider expects assume_role as a list with a single map
		assumeRole := map[string]interface{}{
			"role_arn": roleArn,
		}
		if sessionName := os.Getenv("AWS_ROLE_SESSION_NAME"); sessionName != "" {
			assumeRole["session_name"] = sessionName
		}
		if externalID := os.Getenv("AWS_EXTERNAL_ID"); externalID != "" {
			assumeRole["external_id"] = externalID
		}
		config["assume_role"] = []interface{}{assumeRole}
	}

	// Advanced settings from environment
	if useFips := os.Getenv("AWS_USE_FIPS_ENDPOINT"); useFips == trueValue {
		config["use_fips_endpoint"] = true
	}
	if useDualstack := os.Getenv("AWS_USE_DUALSTACK_ENDPOINT"); useDualstack == trueValue {
		config["use_dualstack_endpoint"] = true
	}
}
