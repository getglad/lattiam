# Example Configurations

This directory contains example Terraform JSON configurations formatted for the Lattiam API.

## Important Note

All examples in this directory are formatted as **complete API requests** ready to be sent to the Lattiam API server. They include:

- `name`: The deployment name
- `terraform_json`: The Terraform configuration in JSON format
- `config`: Optional deployment configuration (AWS region, profile, etc.)

## Demo Examples

For step-by-step demos with the Random provider for testing, see the [`demo/`](../demo/) directory.

## Production Examples

These examples show real-world AWS infrastructure configurations:

### S3 Bucket with Logging

[`s3-bucket-with-logging.json`](s3-bucket-with-logging.json) - Creates an S3 bucket with server access logging configured.

Features:

- S3 bucket with unique naming using timestamp
- Server access logging to the same bucket
- Proper resource dependencies

### SQS Queue

[`sqs-queue.json`](sqs-queue.json) - Creates a standard Amazon SQS queue with custom settings.

Features:

- Standard SQS queue with unique naming
- Custom message retention and delay settings
- Long polling configuration

### VPC with Public Subnet

[`vpc-with-public-subnet.json`](vpc-with-public-subnet.json) - Creates a complete VPC setup with public subnet.

Features:

- VPC with DNS support enabled
- Public subnet with internet gateway
- Route table and associations
- Uses data sources for availability zones

### EC2 with SSM Session Manager

[`ec2-ssm-complete.json`](ec2-ssm-complete.json) - Complete EC2 instance setup with SSM Session Manager access (no SSH required).

Features:

- IAM role and instance profile for SSM
- Security group with proper egress rules
- Latest Amazon Linux 2023 AMI via data source
- Encrypted EBS volume
- All resources deployed together with proper dependencies

### Multi-Resource Dependencies

[`demo-dependencies.json`](demo-dependencies.json) - Demonstrates complex resource dependencies with VPC, subnet, security group, and EC2 instance.

Features:

- Complete VPC setup with all networking components
- Proper dependency chain between resources
- Dynamic AMI selection
- Unique resource naming with timestamps

### Terraform Functions Demo

[`demo-functions.json`](demo-functions.json) - Showcases Lattiam's support for 80+ Terraform functions.

Features:

- UUID generation
- Timestamp functions
- String manipulation (upper, lower, format)
- Base64 encoding
- Hash functions (MD5, SHA256)
- Math functions (max, min, abs, ceil)

## Using These Examples

### With the CLI

```bash
# Deploy directly using the example file
lattiam apply examples/s3-bucket-with-logging.json

# Or with a custom name
lattiam apply examples/vpc-with-public-subnet.json --name my-vpc-deployment
```

### With the API

The examples are already formatted for direct use with the API:

```bash
# Deploy using the API
curl -X POST http://localhost:8084/api/v1/deployments \
  -H "Content-Type: application/json" \
  -d @examples/s3-bucket-with-logging.json

# Check deployment status
DEPLOYMENT_ID=$(curl -s -X POST http://localhost:8084/api/v1/deployments \
  -H "Content-Type: application/json" \
  -d @examples/s3-bucket-with-logging.json | jq -r '.id')

curl http://localhost:8084/api/v1/deployments/$DEPLOYMENT_ID
```

### Customizing Examples

Before deploying, you may want to customize:

1. **AWS Region**: Update the `config.aws_region` field
2. **AWS Profile**: Add `config.aws_profile` if using named profiles
3. **Resource Names**: Many examples use timestamps for unique naming
4. **VPC/Subnet IDs**: For examples that reference existing infrastructure
5. **Instance Types/Sizes**: Adjust based on your needs

Example customization:

```json
{
  "name": "my-custom-deployment",
  "terraform_json": {
    // ... existing terraform configuration ...
  },
  "config": {
    "aws_region": "eu-west-1",
    "aws_profile": "production"
  }
}
```

## Testing with LocalStack

For local testing without AWS costs, see the LocalStack examples in the [`demo/terraform-json/`](../demo/terraform-json/) directory. Files ending with `-localstack.json` are configured for LocalStack testing.

## Notes

- All examples use Terraform functions for unique resource naming to avoid conflicts
- The `ManagedBy: Lattiam` tag is added to help identify resources created by these examples
- Examples use the latest supported provider versions
- Data sources are used where appropriate to make examples more portable
