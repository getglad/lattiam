// Package localstack provides test helpers for LocalStack integration
package localstack

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"

	"github.com/lattiam/lattiam/internal/interfaces"
	"github.com/lattiam/lattiam/tests/testconfig"
)

// ConfigSource provides LocalStack-specific configuration for testing.
// This keeps all LocalStack awareness in the test infrastructure,
// allowing the core application to remain environment-agnostic.
type ConfigSource struct {
	endpoint string
}

// NewConfigSource creates a new LocalStack configuration source
func NewConfigSource() *ConfigSource {
	endpoint := os.Getenv("AWS_ENDPOINT_URL")
	if endpoint == "" {
		endpoint = testconfig.GetLocalStackEndpoint()
	}
	return &ConfigSource{
		endpoint: endpoint,
	}
}

// NewConfigSourceWithEndpoint creates a LocalStack configuration source with a specific endpoint
func NewConfigSourceWithEndpoint(endpoint string) *ConfigSource {
	return &ConfigSource{
		endpoint: endpoint,
	}
}

// GetProviderConfig returns LocalStack-specific configuration for providers
func (cs *ConfigSource) GetProviderConfig(
	_ context.Context,
	providerName string,
	_ string,
	_ *tfprotov6.SchemaBlock,
) (interfaces.ProviderConfig, error) {
	// Only configure AWS provider for LocalStack
	if !strings.Contains(strings.ToLower(providerName), "aws") {
		return nil, nil
	}

	// Build LocalStack-specific AWS configuration
	awsConfig := map[string]interface{}{
		"region":                      "us-east-1",
		"access_key":                  "test",
		"secret_key":                  "test",
		"skip_credentials_validation": true,
		"skip_region_validation":      true,
		"skip_requesting_account_id":  true,
		"skip_metadata_api_check":     true,
		"s3_use_path_style":           true,
		"insecure":                    true,
	}

	// Configure service endpoints
	endpointsConfig := cs.buildEndpointsConfig()
	if len(endpointsConfig) > 0 {
		// AWS provider expects endpoints as a list containing a single map
		awsConfig["endpoints"] = []interface{}{endpointsConfig}
	}

	return interfaces.ProviderConfig{
		providerName: awsConfig,
	}, nil
}

// GetEnvironmentConfig returns LocalStack-specific environment variables
func (cs *ConfigSource) GetEnvironmentConfig(
	_ context.Context,
	_ string,
) (map[string]string, error) {
	env := make(map[string]string)

	// Set LocalStack endpoint
	env["AWS_ENDPOINT_URL"] = cs.endpoint

	// LocalStack test credentials
	env["AWS_ACCESS_KEY_ID"] = "test"
	env["AWS_SECRET_ACCESS_KEY"] = "test"
	env["AWS_DEFAULT_REGION"] = "us-east-1"

	// LocalStack-specific settings
	env["LOCALSTACK_HOSTNAME"] = "localstack"
	env["EDGE_PORT"] = "4566"

	// Disable AWS validations
	const trueValue = "true"
	env["AWS_SKIP_CREDENTIALS_VALIDATION"] = trueValue
	env["AWS_SKIP_METADATA_API_CHECK"] = trueValue
	env["AWS_SKIP_REQUESTING_ACCOUNT_ID"] = trueValue

	return env, nil
}

// buildEndpointsConfig creates the endpoints configuration for LocalStack
func (cs *ConfigSource) buildEndpointsConfig() map[string]interface{} {
	// LocalStack services - comprehensive list based on LocalStack documentation
	services := []string{
		"apigateway", "apigatewayv2", "cloudformation", "cloudwatch",
		"dynamodb", "ec2", "es", "elasticache", "firehose", "iam",
		"kinesis", "lambda", "rds", "redshift", "route53", "s3",
		"secretsmanager", "ses", "sns", "sqs", "ssm", "stepfunctions", "sts",
		// Additional services that might be used in tests
		"autoscaling", "cloudwatchlogs", "events", "kms", "logs",
		"organizations", "resourcegroupstaggingapi", "route53resolver",
		"servicediscovery", "transcribe",
	}

	endpoints := make(map[string]interface{})
	for _, service := range services {
		endpoints[service] = cs.endpoint
	}

	return endpoints
}

// IsHealthy checks if LocalStack is responding
func (cs *ConfigSource) IsHealthy(_ context.Context) error {
	// This could be extended to actually check LocalStack health endpoint
	// For now, just verify the endpoint is set
	if cs.endpoint == "" {
		return fmt.Errorf("LocalStack endpoint not configured")
	}
	return nil
}
