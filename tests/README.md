# Test Helpers Documentation

This package provides comprehensive test helpers to eliminate duplication and improve test maintainability in the Lattiam project.

## Quick Start

### Basic Test Suite

```go
package mytest

import (
    "testing"
    "github.com/lattiam-io/lattiam-go/tests/helpers"
    "github.com/stretchr/testify/suite"
)

type MyTestSuite struct {
    helpers.BaseTestSuite
}

func TestMySuite(t *testing.T) {
    suite.Run(t, new(MyTestSuite))
}

func (s *MyTestSuite) TestDeployment() {
    // API availability is checked automatically in SetupSuite

    // Create deployment with automatic cleanup
    deployment := s.CreateAndTrackDeployment(
        helpers.NewDeploymentRequest("test-deploy",
            helpers.NewS3BucketResource("bucket"),
        ),
    )

    // Wait for success
    s.RequireDeploymentSuccess(deployment["id"].(string))
}
```

## Core Components

### 1. API Helpers (`api_helpers.go`)

Eliminates HTTP client duplication:

```go
// Skip test if API unavailable
helpers.SkipIfAPIUnavailable(t)

// Create API client
client := helpers.NewAPIClient(t)

// Common operations
deployment := client.CreateDeployment(req)
client.WaitForDeploymentStatus(id, "completed", timeout)
client.DeleteDeployment(id)
```

### 2. Test Data Builders (`test_data_builders.go`)

Create test resources without hardcoded JSON:

```go
// Single resource
req := helpers.NewDeploymentRequest("test",
    helpers.NewIAMRoleResource("role"),
)

// Multi-resource with dependencies
req := helpers.NewMultiResourceDeployment("test",
    helpers.NewVPCResource("vpc"),
    helpers.NewSubnetResource("subnet", "vpc"),
    helpers.NewEC2InstanceResource("instance"),
)

// With provider configuration
req = req.WithProvider("aws", "5.31.0").
    WithEnvironmentConfig(helpers.CreateLocalStackEnvironmentConfig())
```

### 3. Test Suite Base (`test_suite.go`)

Provides automatic setup/teardown and resource tracking:

```go
type MySuite struct {
    helpers.BaseTestSuite
}

// Automatic features:
// - API client initialization
// - Provider download
// - Deployment cleanup
// - Custom cleanup functions
```

### 4. Test Constants (`test_constants.go`)

Replace hardcoded values:

```go
// Instead of: time.Sleep(5)
time.Sleep(helpers.DefaultPollInterval)

// Instead of: "http://localhost:8084/api/v1/deployments"
url := helpers.DefaultAPIBaseURL + helpers.EndpointDeployments

// Instead of: "completed"
status := helpers.StatusCompleted
```

### 5. Test Categories (`test_categories.go`)

Enable selective test execution:

```go
// Mark test categories
helpers.MarkTestCategory(t, helpers.CategoryIntegration, helpers.CategoryAWS)

// Skip based on categories
helpers.RequireCategories(t, helpers.CategoryIntegration)
helpers.SkipIfCategory(t, helpers.CategorySlow)

// Run only specific categories:
// LATTIAM_TEST_CATEGORIES=unit,fast go test ./...
// LATTIAM_TEST_CATEGORIES=integration,localstack go test ./...
```

### 6. Fixture Constants (`test_fixtures.go`)

Use constants for all fixture paths:

```go
// Instead of: "deployments/aws/iam-role-basic.json"
helpers.LoadFixtureJSON(t, helpers.FixtureAWSIAMRoleBasic, &req)

// All fixtures have constants:
helpers.FixtureAPIMultiResource
helpers.FixtureDataSourceRegion
helpers.FixtureInvalidCircularDeps
```

## Best Practices

### 1. Always Use BaseTestSuite

```go
type MyTestSuite struct {
    helpers.BaseTestSuite  // Provides automatic cleanup
}
```

### 2. Track All Deployments

```go
// Good: Automatic tracking and cleanup
deployment := s.CreateAndTrackDeployment(req)

// Also good: Manual tracking
deployment := s.APIClient.CreateDeployment(req)
s.TrackDeployment(deployment["id"].(string))
```

### 3. Use Test Categories

```go
func (s *MySuite) TestSlowOperation() {
    helpers.MarkTestCategory(s.T(), helpers.CategorySlow)
    helpers.SkipIfCategory(s.T(), helpers.CategoryFast)
    // ... slow test
}
```

### 4. Enable Parallel Tests Where Possible

```go
func (s *MySuite) TestParallelSafe() {
    s.T().Parallel()

    // Use unique names
    name := helpers.UniqueName("test")
    // ... test with unique resources
}
```

### 5. Use Constants Instead of Magic Values

```go
// Bad
client.WaitForStatus(id, "completed", 2*time.Minute)

// Good
client.WaitForDeploymentStatus(id, helpers.StatusCompleted, helpers.DefaultAPITimeout)
```

## Migration Guide

### Before (Duplicated Code)

```go
func TestOldPattern(t *testing.T) {
    // Duplicated health check
    ctx := context.Background()
    req, _ := http.NewRequestWithContext(ctx, "GET", "http://localhost:8084/api/v1/health", nil)
    client := &http.Client{}
    resp, err := client.Do(req)
    if err != nil {
        t.Skip("API server not running")
    }
    resp.Body.Close()

    // Hardcoded deployment
    deploymentJSON := map[string]interface{}{
        "name": "test",
        "terraform_json": map[string]interface{}{
            "resource": map[string]interface{}{
                "aws_iam_role": map[string]interface{}{
                    "test": map[string]interface{}{
                        "name": "test-role",
                        // ... hardcoded JSON
                    },
                },
            },
        },
    }

    // Manual HTTP calls
    body, _ := json.Marshal(deploymentJSON)
    req, _ = http.NewRequest("POST", "http://localhost:8084/api/v1/deployments", bytes.NewReader(body))
    // ... more boilerplate

    // No cleanup
}
```

### After (Using Helpers)

```go
func (s *MyTestSuite) TestNewPattern() {
    // Health check handled by BaseTestSuite

    // Clean deployment creation
    deployment := s.CreateAndTrackDeployment(
        helpers.NewDeploymentRequest("test",
            helpers.NewIAMRoleResource("test"),
        ),
    )

    // Simple status check
    s.RequireDeploymentSuccess(deployment["id"].(string))

    // Automatic cleanup in TearDownTest
}
```

## Running Tests

### Run All Tests

```bash
make test-integration
```

### Run Specific Categories

```bash
# Only unit tests
LATTIAM_TEST_CATEGORIES=unit go test ./...

# Integration tests with LocalStack
LATTIAM_TEST_CATEGORIES=integration,localstack go test ./...

# Fast smoke tests
LATTIAM_TEST_CATEGORIES=smoke,fast go test ./...

# Parallel-safe tests only
LATTIAM_TEST_CATEGORIES=parallel go test -parallel 4 ./...
```

### Skip Categories

```bash
# Run all except slow tests
LATTIAM_TEST_CATEGORIES=integration go test ./... -run '!Slow'
```

## Debugging

### Enable Verbose Output

```bash
go test -v ./tests/integration/...
```

### Check Which Categories Are Running

Tests will log their categories:

```
=== RUN   TestDeployment
    test_categories.go:123: Test categories: integration, aws
```

### Verify API Availability

```bash
curl http://localhost:8084/api/v1/health
```

## Common Patterns

### Testing with Fixtures

```go
var req helpers.DeploymentRequest
helpers.LoadFixtureJSON(t, helpers.FixtureAPIMultiResource, &req)
req.Name = helpers.UniqueName("fixture-test")
deployment := s.CreateAndTrackDeployment(req)
```

### Testing Failures

```go
deployment := s.CreateAndTrackDeployment(invalidReq)
s.RequireDeploymentFailure(deployment["id"].(string))
s.Require().NotNil(deployment["error"])
```

### Extracting Resource Outputs

```go
bucketName := s.GetResourceOutput(deployment,
    helpers.ResourceTypeS3Bucket, "my-bucket", "bucket")
s.Require().NotEmpty(bucketName)
```

### Custom Cleanup

```go
s.AddCleanupFunc(func() {
    // Custom cleanup logic
    fmt.Println("Cleaning up custom resources")
})
```
