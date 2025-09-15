// Package testing provides test utilities and mock provider schemas
package testing

import (
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

// buildAWSProviderSchema creates AWS provider schema using the builder pattern
func buildAWSProviderSchema() *tfprotov6.GetProviderSchemaResponse {
	return NewSchemaBuilder().
		WithProvider().
		WithStringAttribute("region", true).
		WithStringAttribute("access_key", false).
		WithStringAttribute("secret_key", false).
		End().
		WithResource("aws_s3_bucket").
		WithStringAttribute("id", Computed()).
		WithStringAttribute("bucket", Required()).
		WithStringAttribute("acl", Optional()).
		WithBoolAttribute("versioning", Optional()).
		WithStringAttribute("arn", Computed()).
		End().
		WithResource("aws_instance").
		WithStringAttribute("id", Computed()).
		WithStringAttribute("ami", Required()).
		WithStringAttribute("instance_type", Required()).
		WithStringAttribute("public_ip", Computed()).
		WithStringAttribute("private_ip", Computed()).
		End().
		WithDataSource("aws_ami").
		WithStringAttribute("id", Computed()).
		WithBoolAttribute("most_recent", Optional()).
		WithListStringAttribute("owners", Required()).
		WithStringAttribute("name", Computed()).
		End().
		Build()
}
