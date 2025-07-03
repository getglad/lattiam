# Test Fixtures

This directory contains test data files used by unit and integration tests.

## Directory Structure

```
fixtures/
├── deployments/         # Deployment JSON files for testing
│   ├── api/            # API format (wrapped with name/terraform_json)
│   ├── aws/            # CLI format AWS resource deployments
│   ├── invalid/        # Invalid deployments for error testing
│   └── multi-resource/ # CLI format multi-resource deployments
└── README.md           # This file
```

## Usage

### In Tests

```go
// Load a CLI format fixture
cliFixture := "tests/fixtures/deployments/aws/iam-role-simple.json"

// Load an API format fixture
apiFixture := "tests/fixtures/deployments/api/iam-role-simple.json"
```

### For Manual Testing

```bash
# Test with API (use API format fixtures)
curl -X POST http://localhost:8084/api/v1/deployments \
  -H "Content-Type: application/json" \
  -d @tests/fixtures/deployments/api/iam-role-simple.json

# Test with CLI (use CLI format fixtures)
./build/lattiam apply tests/fixtures/deployments/aws/s3-bucket-simple.json
```

## File Naming Convention

- Use lowercase with hyphens: `resource-type-description.json`
- Include resource type in filename: `iam-role-lambda.json`, `s3-bucket-public.json`
- Use descriptive suffixes: `-simple`, `-complex`, `-with-config`, `-invalid`

## Categories

### API Format (`deployments/api/`)

- Deployments wrapped with `name` and `terraform_json` fields
- Used by API endpoints (`POST /api/v1/deployments`)
- Example: `{"name": "my-deployment", "terraform_json": {...}}`

### CLI Format AWS Resources (`deployments/aws/`)

- Direct Terraform JSON format
- Used by CLI (`lattiam apply`)
- IAM roles, policies, users
- S3 buckets
- EC2 instances, security groups

### Invalid Deployments (`deployments/invalid/`)

- Missing required fields
- Invalid JSON syntax
- Malformed resource configurations

### CLI Format Multi-Resource (`deployments/multi-resource/`)

- Direct Terraform JSON with multiple resources
- Complex dependency scenarios
