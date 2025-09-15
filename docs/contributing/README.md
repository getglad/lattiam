# Contributing Documentation

Documentation for developers and maintainers working on Lattiam.

## Guides

- [Development Setup](./DEVELOPMENT.md) - Building, testing, and development workflow
- [Architecture](./ARCHITECTURE.md) - System design and internal implementation
- [Testing Guide](./TESTING.md) - Running tests and using LocalStack

## Quick Start for Contributors

```bash
# Clone and build
git clone https://github.com/lattiam/lattiam.git
cd lattiam

# Run tests
make test

# Build and start the server
make build
./build/lattiam server start
```

## Key Make Commands

### Building

- `make build` - Build the binary
- `make build-dev` - Build with race detection and LocalStack support

### Testing

- `make test` - Run unit tests
- `make test-integration` - Run integration tests (requires LocalStack)
- `make test-demo` - Run demo acceptance tests
- `make test-oat` - Run operational acceptance tests
- `make test-all` - Run complete test suite with coverage
- `make test-coverage` - Generate HTML coverage report

### Quality

- `make qa` - Run all quality checks
- `make fix` - Auto-fix formatting issues
- `make lint` - Run linters

See [Development Guide](./DEVELOPMENT.md) for complete workflow.
