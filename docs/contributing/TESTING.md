# Testing Guide

## LocalStack Testing

For testing AWS provider functionality without real AWS resources:

### Setup LocalStack

```bash
# 1. Start LocalStack
docker run -d \
  --name localstack \
  -p 4566:4566 \
  -e SERVICES=s3,ec2,iam \
  localstack/localstack

# 2. Configure environment
export AWS_ACCESS_KEY_ID=test
export AWS_SECRET_ACCESS_KEY=test
export AWS_ENDPOINT_URL=http://localhost:4566
export LOCALSTACK_ENDPOINT=http://localhost:4566
export LATTIAM_AWS_S3_ENDPOINT=http://localhost:4566
export LATTIAM_AWS_DYNAMODB_ENDPOINT=http://localhost:4566

# 3. Start Lattiam with LocalStack configuration
lattiam server start
```

### Example AWS Deployment

```bash
curl -X POST http://localhost:8084/api/v1/deployments \
  -H "Content-Type: application/json" \
  -d '{
    "name": "aws-demo",
    "terraform_json": {
      "terraform": {
        "required_providers": {
          "aws": {
            "source": "hashicorp/aws",
            "version": "~> 5.0"
          }
        }
      },
      "provider": {
        "aws": {
          "region": "us-east-1",
          "endpoints": {
            "s3": "http://localhost:4566",
            "ec2": "http://localhost:4566",
            "iam": "http://localhost:4566"
          }
        }
      },
      "resource": {
        "aws_s3_bucket": {
          "demo": {
            "bucket": "my-demo-bucket"
          }
        }
      }
    }
  }'
```

## Running Tests

```bash
# Unit tests
make test

# Integration tests (requires LocalStack)
make test-integration

# All tests with coverage
make test-all

# Generate coverage report
make test-coverage
```

## Test Structure

- `tests/unit/` - Unit tests for individual components
- `tests/integration/` - Integration tests with providers
- `tests/fixtures/` - Test data and configurations
- `tests/provider-isolation/` - Provider isolation tests

## Known Test Issues

- Provider process management tests may fail due to timing issues
- Distributed mode tests are incomplete
- Only Random and AWS (LocalStack) providers are tested
