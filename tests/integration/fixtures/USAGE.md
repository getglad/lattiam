# Test Fixtures Usage Guide

This directory contains all test JSON files used throughout the project.

## Directory Structure

```
tests/fixtures/
├── README.md              # Overview of fixtures
├── USAGE.md              # This file - how to use fixtures
└── deployments/          # Deployment JSON files
    ├── aws/              # AWS resource deployments
    │   ├── iam-role-*.json
    │   └── s3-bucket-*.json
    ├── invalid/          # Invalid deployments for error testing
    │   ├── missing-name.json
    │   ├── missing-terraform-json.json
    │   └── empty-resources.json
    └── multi-resource/   # Multi-resource deployments
        ├── ec2-s3-sg.json
        ├── ec2-ssm-complete.json
        └── complex-resources.json
```

## Using Fixtures in Tests

### Go Tests

```go
// Unit tests
func TestDeployment(t *testing.T) {
    // Use relative path from test file
    fixture := "../../fixtures/deployments/aws/iam-role-simple.json"

    // Or use testdata symlink (if created)
    fixture := "testdata/aws/iam-role-simple.json"
}

// Integration tests
func TestAPIDeployment(t *testing.T) {
    // Load fixture file
    data, err := os.ReadFile("../fixtures/deployments/aws/s3-bucket-simple.json")
    require.NoError(t, err)

    // Use in API request
    resp, err := http.Post(apiURL, "application/json", bytes.NewReader(data))
}
```

### Manual Testing

```bash
# Test with CLI (direct Terraform JSON)
./build/lattiam apply tests/fixtures/deployments/aws/iam-role-simple.json

# Test with API (wrapped format)
curl -X POST http://localhost:8084/api/v1/deployments \
  -H "Content-Type: application/json" \
  -d @tests/fixtures/deployments/aws/iam-role-with-config.json

# Run test script
./scripts/test-api.sh
```

### Shell Scripts

```bash
#!/bin/bash
FIXTURES_DIR="tests/fixtures/deployments"

# Use in loops
for fixture in $FIXTURES_DIR/aws/*.json; do
    echo "Testing $fixture"
    curl -X POST $API_URL -d @"$fixture"
done
```

## File Format Types

### 1. Direct Terraform JSON (for CLI)

Used by: `lattiam apply` command

```json
{
  "resource": {
    "aws_iam_role": {
      "example": {
        "name": "my-role",
        "assume_role_policy": "..."
      }
    }
  }
}
```

### 2. API Deployment Format (for API)

Used by: API endpoints

```json
{
  "name": "deployment-name",
  "config": {
    "aws_profile": "optional-profile",
    "aws_region": "optional-region"
  },
  "terraform_json": {
    "resource": {
      "aws_iam_role": {
        "example": {
          "name": "my-role",
          "assume_role_policy": "..."
        }
      }
    }
  }
}
```

## Naming Conventions

- **Resource type prefix**: `iam-role-`, `s3-bucket-`, `ec2-instance-`
- **Descriptive suffix**: `-simple`, `-complex`, `-with-config`, `-invalid`
- **Test scenarios**: `-localstack`, `-production`, `-multi-region`

## Adding New Fixtures

1. Choose appropriate directory based on content
2. Follow naming convention
3. Include comments for complex configurations
4. Update this documentation if adding new categories

## Migrating from Old Structure

Old locations → New locations:

- `examples/*.json` → `tests/fixtures/deployments/aws/`
- `tests/integration/*.json` → `tests/fixtures/deployments/`
- Root `deployment-*.json` → `tests/fixtures/deployments/`

## Best Practices

1. **Reuse fixtures** - Don't create duplicate test data
2. **Keep it simple** - Each fixture should test one thing
3. **Document complex cases** - Add comments in JSON or companion .md file
4. **Version control** - All fixtures should be in git
5. **No secrets** - Never include real credentials or sensitive data
