# Contributing to Lattiam

Welcome to the Lattiam project! This guide helps maintainers and contributors understand the development workflow.

## Getting Started

### Prerequisites

- Go 1.21+
- Make
- Git
- golangci-lint
- Docker (for integration tests)

### Setup

```bash
# Clone repository
git clone https://github.com/lattiam/lattiam.git
cd lattiam

# Install development tools
make install-tools

# Run tests to verify setup
make test
```

## Development Workflow

### Make Targets for Development

```bash
# Essential commands
make test              # Run unit tests with coverage
make check             # Verify code quality
make fix               # Auto-fix formatting/lint issues
make all               # Full pipeline: fix + test + build

# Development helpers
make test-race         # Run unit tests with race detector
make bench             # Run benchmarks
make security          # Run security vulnerability scans (govulncheck + nancy)

# Build and deployment
make build             # Build for current platform
make build-all-platforms # Build for all platforms (uses goreleaser)
make clean             # Clean build artifacts
```

## Project Structure

### Package Organization

- `cmd/` - CLI entry points and commands
- `pkg/` - Reusable packages for external consumption
- `internal/` - Private packages not for external use
- `docs/` - All project documentation
- `mk/` - Modular build system

## Testing Guide

Lattiam uses a multi-layered testing strategy:

- **Unit tests** - Test individual components in isolation
- **Integration tests** - Test against real cloud services
- **End-to-end tests** - Test complete workflows

In the context of Lattiam's vision as "Terraform as a Service," robust testing is paramount. It ensures the reliability of provider interactions, the consistency of state management across deployments, and the overall stability of the API. Comprehensive tests are critical for maintaining trust and enabling seamless infrastructure automation for users.

### Running Tests

```bash
# Run all unit tests with coverage
make test

# Run integration tests with LocalStack and coverage
make test-integration

# Run all tests (unit, integration, OAT) with merged coverage
make test-all

# Run specific test packages
go test ./pkg/provider/protocol/...

# Run unit tests with race detector
make test-race

# Run security scans
make security
```

### Writing Tests

#### Unit Test Example

```go
func TestProviderManager_GetProvider(t *testing.T) {
    tests := []struct {
        name     string
        provider string
        version  string
        wantErr  bool
    }{
        {
            name:     "valid provider",
            provider: "aws",
            version:  "5.0.0",
            wantErr:  false,
        },
        {
            name:     "invalid provider",
            provider: "unknown",
            version:  "1.0.0",
            wantErr:  true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Test implementation
        })
    }
}
```

#### Integration Test Example

```go
//go:build integration
// +build integration

func TestAWSResourceCreation(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping integration test")
    }

    // Setup
    ctx := context.Background()
    manager := setupTestManager(t)

    // Test resource creation
    resource, err := manager.CreateResource(ctx, config)
    require.NoError(t, err)

    // Cleanup
    t.Cleanup(func() {
        _ = manager.DeleteResource(ctx, resource.ID)
    })

    // Assertions
    assert.NotEmpty(t, resource.ID)
}
```

### LocalStack Testing

For local development, you can use LocalStack to emulate AWS services.

**1. Start LocalStack:**

```bash
docker-compose -f tests/docker-compose.localstack.yml up -d
```

**2. Configure Environment:**

```bash
export AWS_ENDPOINT_URL=http://localhost:4566
export AWS_ACCESS_KEY_ID=test
export AWS_SECRET_ACCESS_KEY=test
export AWS_REGION=us-east-1
```

**3. Run Tests:**

```bash
make integration-test
```



## Build Options

Lattiam can be built with or without LocalStack-specific code.

- **Production Build (default):** `make build`
- **Development Build (with LocalStack):** `make build-localstack`

The separation is controlled by the `localstack` build tag.

## Troubleshooting

### Enabling Debug Logging

For diagnosing provider issues, enable debug logging:

```bash
# Enable debug logging
LATTIAM_DEBUG=1 ./build/lattiam apply examples/simple-iam-role.yaml

# Enable full debug logging with provider logs
LATTIAM_DEBUG=1 \
LATTIAM_DEBUG_DIR=./debug-logs \
TF_LOG=DEBUG \
TF_LOG_PATH=./debug-logs/terraform.log \
./build/lattiam apply examples/simple-iam-role.yaml
```

Debug logs and raw msgpack data will be written to the specified directory, which is essential for diagnosing provider communication issues.

### Common Issues

- **Provider Configuration Marshal Error:** This often means the provider configuration is missing required block attributes. Ensure all block attributes are properly initialized.
- **Provider Crash During Resource Creation:** This can be caused by incorrect msgpack encoding. Ensure you are using the correct `cty/msgpack` library.
- **Missing AWS Credentials:** Ensure your AWS credentials are configured correctly.

## Documentation Standards

- Document all public functions and types.
- Include usage examples for complex APIs.
- Keep user and architecture documentation current and aligned with the project's vision as articulated in the `README.md`.

## Release Process

- Use semantic versioning (e.g., `v1.2.3`).
- Maintain a `CHANGELOG.md`.
- Use `make tag NEW_VERSION=1.2.3` and `make release` to automate the release.

## Issue and PR Guidelines

- Keep changes focused and atomic.
- Include tests for functionality.
- Update documentation as needed.
- Ensure all tests and quality checks pass.

Thank you for contributing to Lattiam!
