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

## Documentation

For complete usage guide including file formats, naming conventions, and testing examples, see:

**[docs/contributing/testing.md](/docs/contributing/testing.md#test-fixtures)**
