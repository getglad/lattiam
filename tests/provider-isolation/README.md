# Provider Isolation Integration Tests

This directory contains comprehensive integration tests that verify the new per-deployment provider isolation feature works correctly and prevents the Demo1 S3 failure issue where providers interfere with each other.

## What This Tests

The provider isolation system ensures that:

1. **Per-Deployment Provider Instances**: Each deployment gets its own provider instance, even when using the same provider type and version
2. **Working Directory Isolation**: Each deployment uses separate working directories for provider state and Terraform data
3. **Configuration Isolation**: Provider configurations for one deployment don't affect other deployments
4. **Lifecycle Isolation**: Shutting down one deployment doesn't affect providers in other deployments
5. **Race Condition Prevention**: Long-running operations on one deployment aren't interrupted by other deployments reconfiguring the same provider type

## Problem Solved

This addresses the **Demo1 S3 failure scenario** where:
- Multiple deployments use the same provider type (e.g., AWS provider)
- One deployment starts a long-running operation (e.g., S3 bucket creation)  
- Another deployment reconfigures the provider or starts provider operations
- The first deployment's operation fails due to provider interference

## Test Structure

### TestProviderIsolationIntegration

The main test suite contains these subtests:

#### 1. ConcurrentProviderAccess
- **Purpose**: Validates multiple deployments can use the same provider type concurrently
- **Method**: Creates 4 deployments simultaneously, all using the random provider
- **Verification**: Each deployment gets unique resources without interference

#### 2. ProviderReconfigurationIsolation  
- **Purpose**: Tests that reconfiguring providers for one deployment doesn't affect others
- **Method**: Creates two deployments, configures them differently, verifies isolation
- **Verification**: Original deployment maintains its configuration and state

#### 3. DeploymentCleanup
- **Purpose**: Verifies shutting down one deployment doesn't affect others
- **Method**: Creates 3 deployments, shuts down the middle one
- **Verification**: Other deployments remain functional

#### 4. WorkingDirectoryIsolation
- **Purpose**: Ensures each deployment uses separate working directories
- **Method**: Creates providers for different deployments, verifies directory separation  
- **Verification**: Resources are independent, directories are properly isolated

#### 5. RaceConditionPrevention
- **Purpose**: Simulates the Demo1 scenario to verify the fix works
- **Method**: Starts long-running operation on deployment 1, starts deployment 2 concurrently
- **Verification**: Both deployments complete successfully without interference

## Running the Tests

### Basic Usage
```bash
# Run all provider isolation tests
go test -v -tags="integration" ./tests/provider-isolation -timeout=5m
```

### Individual Test Cases
```bash
# Test concurrent provider access only
go test -v -tags="integration" ./tests/provider-isolation -run="ConcurrentProviderAccess" -timeout=2m

# Test race condition prevention only  
go test -v -tags="integration" ./tests/provider-isolation -run="RaceConditionPrevention" -timeout=2m
```

### Verbose Output
The tests provide detailed logging showing:
- Provider configuration and lifecycle events
- Resource creation/deletion operations
- Isolation verification checkpoints
- Success confirmations with checkmarks (✅)

## Test Dependencies

### Build Tags
- `integration`: Required build tag for all tests
- Tests are excluded from unit test runs

### Provider Requirements  
- Uses the **random provider** (v3.6.0) for predictable testing
- Downloads provider binary automatically on first run
- Subsequent runs reuse cached provider binary

### Environment
- Creates temporary directories for each test run
- Cleans up all resources automatically
- No external service dependencies (unlike AWS provider tests)

## Integration with CI/CD

### Make Targets
```bash
# These tests are included in:
make test-integration    # Unit + integration tests  
make test-all           # Unit + integration + OAT tests
```

### Performance
- **Fast execution**: ~2.3 seconds for full suite
- **Minimal downloads**: Provider binary cached after first download
- **Parallel-safe**: Each test uses unique temporary directories

## Verification Criteria

Each test verifies:
- ✅ **No interference**: Operations on one deployment don't affect others
- ✅ **Resource isolation**: Each deployment creates unique resources
- ✅ **Provider health**: All providers remain functional throughout tests
- ✅ **Proper cleanup**: Resources and directories are cleaned up correctly
- ✅ **Race condition immunity**: Concurrent operations complete successfully

## Troubleshooting

### Common Issues

1. **Provider download timeouts**: 
   - Increase timeout: `-timeout=10m`
   - Check internet connectivity

2. **Test failures on race conditions**:
   - Usually indicates a real concurrency issue
   - Check provider isolation implementation

3. **Resource cleanup failures**:
   - Temporary directories may need manual cleanup
   - Check `/tmp/lattiam-*` directories

### Debug Mode
```bash
# Enable debug logging
LATTIAM_DEBUG=true go test -v -tags="integration" ./tests/provider-isolation
```

## Implementation Details

The tests validate the implementation changes in:
- `pkg/provider/protocol/manager.go`: Per-deployment provider caching
- `pkg/provider/protocol/provider_wrapper_*.go`: Provider instance isolation
- Working directory creation: `/tmp/lattiam-deployments/{deploymentID}/`

These tests serve as regression tests to ensure the Demo1 S3 failure scenario doesn't reoccur in future changes.