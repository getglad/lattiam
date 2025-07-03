package helpers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// FixturePath returns the path to a test fixture file
func FixturePath(t *testing.T, path string) string {
	t.Helper()

	cwd, _ := os.Getwd()
	t.Logf("FixturePath: working directory=%s", cwd)

	// Try multiple possible base paths
	basePaths := []string{
		"tests/fixtures",       // From project root
		"../fixtures",          // From tests/integration
		"../../tests/fixtures", // From internal packages
		"fixtures",             // From tests directory
		"../../",               // From integration tests to project root (for demo files)
		"",                     // Direct path
	}

	for _, base := range basePaths {
		fullPath := filepath.Join(base, path)
		if _, err := os.Stat(fullPath); err == nil {
			return fullPath
		}
		// Debug: log attempted paths
		t.Logf("Fixture path attempt failed: %s", fullPath)
	}

	// If not found, return the path as-is and let the test fail
	t.Logf("Warning: fixture file not found in any base path: %s", path)
	return filepath.Join("tests", "fixtures", path)
}

// LoadFixture loads a JSON fixture file
func LoadFixture(t *testing.T, path string) []byte {
	t.Helper()

	fixturePath := FixturePath(t, path)
	t.Logf("LoadFixture: resolved path=%s", fixturePath)
	data, err := os.ReadFile(fixturePath)
	if err != nil {
		t.Fatalf("Failed to read fixture file %s: %v", fixturePath, err)
	}
	if len(data) == 0 {
		t.Fatalf("Fixture file %s is empty", fixturePath)
	}
	t.Logf("LoadFixture: successfully loaded %d bytes", len(data))

	return data
}

// LoadFixtureJSON loads and unmarshals a JSON fixture
func LoadFixtureJSON(t *testing.T, path string, v interface{}) {
	t.Helper()

	data := LoadFixture(t, path)
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("Failed to unmarshal fixture %s: %v. Content: %s", path, err, string(data))
	}
}

// Common fixture paths
const (
	// AWS fixtures (CLI format - direct Terraform JSON)
	FixtureAWSIAMRoleSimple      = "deployments/aws/iam-role-simple.json"
	FixtureAWSIAMRoleBasic       = "deployments/aws/iam-role-basic.json"
	FixtureAWSIAMRoleWithConfig  = "deployments/aws/iam-role-with-config.json"
	FixtureAWSS3BucketSimple     = "deployments/aws/s3-bucket-simple.json"
	FixtureAWSS3BucketLocalStack = "deployments/aws/s3-bucket-localstack.json"
	FixtureAWSS3BucketVersioning = "deployments/aws/s3-bucket-versioning.json"
	FixtureAWSEC2Instance        = "deployments/aws/ec2-instance.json"
	FixtureAWSSecurityGroup      = "deployments/aws/security-group.json"
	FixtureAWSVPCComplete        = "deployments/aws/vpc-complete.json"

	// API format fixtures (wrapped with name and terraform_json)
	FixtureAPIIAMRoleSimple     = "deployments/api/iam-role-simple.json"
	FixtureAPIMultiResource     = "deployments/api/multi-resource.json"
	FixtureAPIMultiResourceDeps = "deployments/api/multi-resource-deps.json"
	FixtureAPIS3BucketSimple    = "deployments/api/s3-bucket-simple.json"
	FixtureAPIDataSources       = "deployments/api/data-sources.json"
	FixtureAPIFunctions         = "deployments/api/functions.json"

	// Invalid fixtures
	FixtureInvalidMissingName          = "deployments/invalid/missing-name.json"
	FixtureInvalidMissingTerraformJSON = "deployments/invalid/missing-terraform-json.json"
	FixtureInvalidEmptyResources       = "deployments/invalid/empty-resources.json"
	FixtureInvalidBadProvider          = "deployments/invalid/bad-provider.json"
	FixtureInvalidCircularDeps         = "deployments/invalid/circular-deps.json"

	// Multi-resource fixtures (CLI format)
	FixtureMultiResourceEC2S3SG        = "deployments/multi-resource/ec2-s3-sg.json"
	FixtureMultiResourceEC2SSMComplete = "deployments/multi-resource/ec2-ssm-complete.json"
	FixtureMultiResourceComplex        = "deployments/multi-resource/complex-resources.json"
	FixtureMultiResourceWithData       = "deployments/multi-resource/with-data-sources.json"
	FixtureMultiResourceDependencies   = "deployments/multi-resource/dependencies.json"

	// Data source fixtures
	FixtureDataSourceRegion         = "deployments/data-sources/aws-region.json"
	FixtureDataSourceCallerIdentity = "deployments/data-sources/caller-identity.json"
	FixtureDataSourceAMI            = "deployments/data-sources/ami-lookup.json"
	FixtureDataSourceAZs            = "deployments/data-sources/availability-zones.json"

	// Function fixtures
	FixtureFunctionString     = "deployments/functions/string-funcs.json"
	FixtureFunctionNumeric    = "deployments/functions/numeric-funcs.json"
	FixtureFunctionCollection = "deployments/functions/collection-funcs.json"
	FixtureFunctionCrypto     = "deployments/functions/crypto-funcs.json"
	FixtureFunctionNetwork    = "deployments/functions/network-funcs.json"
)
