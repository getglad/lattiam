# Development Guide

## Prerequisites

- Go 1.23+
- Docker (for LocalStack testing)
- Make

## Building from Source

```bash
git clone https://github.com/lattiam/lattiam.git
cd lattiam

# Build the binary
make build
```

## Development Workflow

### Make Commands

View all available commands with descriptions:

```bash
make help
```

Common development commands:

```bash
make build              # Build the binary
make test               # Run unit tests
make test-all           # Run complete test suite
make qa                 # Run quality checks
make fix                # Auto-fix formatting issues
```

### Development Environment

#### Debug Mode

Enable detailed logging for development:

```bash
# Option 1: Using environment variables
export LATTIAM_DEBUG=1
export LATTIAM_DEBUG_DIR=/tmp/lattiam-debug
lattiam server start

# Option 2: Using command line flag
lattiam server start --debug --debug-dir /tmp/lattiam-debug
```

Debug files created:

- Provider communication logs
- gRPC message traces
- State transitions
- Configuration dumps

#### Using LocalStack

See [Testing Guide](./TESTING.md) for LocalStack setup.

### Debugging Common Issues

#### Provider Process Management

Common provider problems:

1. **EOF errors**: Provider process died
   - Provider binary not found
   - Protocol version mismatch
   - Provider crashed
2. **Plugin incompatible**: Protocol version mismatch
3. **Timeout**: Provider hung or slow
4. **Permission denied**: Can't execute provider binary

Debug with:

```bash
export LATTIAM_DEBUG=1
tail -f /tmp/lattiam-debug/provider-*.log
```

#### State Corruption

If state gets corrupted:

1. Check `~/.lattiam/state/deployments/`
2. State files are JSON - can manually edit
3. No built-in recovery tools
4. No migration tools despite error messages

```bash
# Inspect state
cat ~/.lattiam/state/deployments/deploy-*.json | jq .

# Reset state (data loss!)
rm -rf ~/.lattiam/state/deployments/*
```

#### Queue Issues

Check queue metrics:

```bash
curl http://localhost:8084/api/v1/queue/metrics
```
