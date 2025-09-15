# Test Helpers

This package provides utilities to make testing easier and more consistent across the Lattiam project.

## Provider Helper

The provider helper manages Terraform provider downloads for tests, avoiding timeouts and race conditions.

### Usage

```go
import "github.com/lattiam/lattiam/tests/helpers"

func TestMyFeature(t *testing.T) {
    // Skip test if AWS provider can't be downloaded
    helpers.SkipIfAWSUnavailable(t)

    // Your test code here...
}

// In TestMain for pre-downloading providers
func TestMain(m *testing.M) {
    os.Exit(helpers.SetupProviders(m))
}
```

### Functions

- `EnsureAWSProvider(t)` - Downloads AWS provider if needed, returns error
- `SkipIfAWSUnavailable(t)` - Skips test if AWS provider unavailable
- `SetupProviders(m)` - Use in TestMain to pre-download providers
- `NewProviderDownloader(dir)` - Create custom downloader for specific providers

## Fixture Helper

The fixture helper provides easy access to test JSON files organized in `tests/fixtures/`.

### Usage

```go
import "github.com/lattiam/lattiam/tests/helpers"

func TestAPIEndpoint(t *testing.T) {
    // Load raw fixture data
    data := helpers.LoadFixture(t, helpers.FixtureAWSIAMRoleSimple)

    // Or load and unmarshal JSON
    var deployment Deployment
    helpers.LoadFixtureJSON(t, helpers.FixtureAWSIAMRoleWithConfig, &deployment)
}
```

### Pre-defined Constants

```go
// AWS fixtures
helpers.FixtureAWSIAMRoleSimple
helpers.FixtureAWSIAMRoleWithConfig
helpers.FixtureAWSS3BucketSimple

// Invalid fixtures for error testing
helpers.FixtureInvalidMissingName
helpers.FixtureInvalidEmptyResources

// Multi-resource fixtures
helpers.FixtureMultiResourceEC2S3SG
helpers.FixtureMultiResourceComplex
```

### Functions

- `FixturePath(t, path)` - Returns full path to fixture file
- `LoadFixture(t, path)` - Loads fixture file as bytes
- `LoadFixtureJSON(t, path, v)` - Loads and unmarshals JSON fixture

## Test Configuration

Additional helpers for common test configurations:

```go
// Get LocalStack endpoint if available
endpoint := helpers.GetLocalStackEndpoint()

// Check if running in LocalStack mode
if helpers.IsLocalStackMode() {
    // Configure for LocalStack
}

// Get test provider configuration
config := helpers.GetTestProviderConfig()
```

## Benefits

1. **No More Timeouts**: Provider downloads are managed centrally
2. **Consistent Fixtures**: All tests use the same test data
3. **Easy Maintenance**: Update fixtures in one place
4. **Better Error Messages**: Clear skip messages when prerequisites missing
5. **Parallel Test Support**: Thread-safe provider downloads

## Example Test

```go
package mypackage

import (
    "testing"
    "github.com/lattiam/lattiam/tests/helpers"
)

func TestMain(m *testing.M) {
    // Pre-download providers before tests
    os.Exit(helpers.SetupProviders(m))
}

func TestDeployment(t *testing.T) {
    // Skip if provider unavailable
    helpers.SkipIfAWSUnavailable(t)

    // Load test fixture
    data := helpers.LoadFixture(t, helpers.FixtureAWSIAMRoleSimple)

    // Run your test...
}
```
