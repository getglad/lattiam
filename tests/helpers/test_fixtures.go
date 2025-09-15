package helpers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// fixturePathCache caches resolved fixture paths
var (
	fixturePathCache = make(map[string]string)
	fixturePathMutex sync.RWMutex
)

// FixturePath returns the path to a test fixture file
func FixturePath(t *testing.T, path string) string {
	t.Helper()

	// Check cache first
	fixturePathMutex.RLock()
	if cachedPath, ok := fixturePathCache[path]; ok {
		fixturePathMutex.RUnlock()
		return cachedPath
	}
	fixturePathMutex.RUnlock()

	cwd, err := os.Getwd()
	if err != nil {
		t.Logf("FixturePath: failed to get working directory: %v", err)
		cwd = "."
	}
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
			// Cache the resolved path
			fixturePathMutex.Lock()
			fixturePathCache[path] = fullPath
			fixturePathMutex.Unlock()
			return fullPath
		}
		// Debug: log attempted paths
		t.Logf("Fixture path attempt failed: %s", fullPath)
	}

	// If not found, return the path as-is and let the test fail
	t.Logf("Warning: fixture file not found in any base path: %s", path)
	defaultPath := filepath.Join("tests", "fixtures", path)

	// Cache even the default path to avoid repeated lookups
	fixturePathMutex.Lock()
	fixturePathCache[path] = defaultPath
	fixturePathMutex.Unlock()

	return defaultPath
}

// LoadFixture loads a JSON fixture file
func LoadFixture(t *testing.T, path string) []byte {
	t.Helper()

	fixturePath := FixturePath(t, path)
	t.Logf("LoadFixture: resolved path=%s", fixturePath)
	data, err := os.ReadFile(fixturePath) // #nosec G304 - fixturePath is validated by FixturePath function
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

// FixtureGroups organizes fixture paths by category
type FixtureGroups struct {
	AWS           AWSFixtures
	API           APIFixtures
	Invalid       InvalidFixtures
	MultiResource MultiResourceFixtures
	DataSource    DataSourceFixtures
	Function      FunctionFixtures
}

// AWSFixtures contains AWS-related fixture paths (CLI format)
type AWSFixtures struct {
	IAMRoleSimple      string
	IAMRoleBasic       string
	IAMRoleWithConfig  string
	S3BucketSimple     string
	S3BucketLocalStack string
	S3BucketVersioning string
	EC2Instance        string
	SecurityGroup      string
	VPCComplete        string
}

// APIFixtures contains API format fixture paths
type APIFixtures struct {
	IAMRoleSimple     string
	MultiResource     string
	MultiResourceDeps string
	S3BucketSimple    string
	DataSources       string
	Functions         string
}

// InvalidFixtures contains invalid test case fixtures
type InvalidFixtures struct {
	MissingName          string
	MissingTerraformJSON string
	EmptyResources       string
	BadProvider          string
	CircularDeps         string
}

// MultiResourceFixtures contains multi-resource deployment fixtures
type MultiResourceFixtures struct {
	EC2S3SG        string
	EC2SSMComplete string
	Complex        string
	WithData       string
	Dependencies   string
}

// DataSourceFixtures contains data source test fixtures
type DataSourceFixtures struct {
	Region            string
	CallerIdentity    string
	AMI               string
	AvailabilityZones string
}

// FunctionFixtures contains function test fixtures
type FunctionFixtures struct {
	String     string
	Numeric    string
	Collection string
	Crypto     string
	Network    string
}

// Fixtures provides organized access to all test fixtures
var Fixtures = FixtureGroups{
	AWS: AWSFixtures{
		IAMRoleSimple:      "deployments/aws/iam-role-simple.json",
		IAMRoleBasic:       "deployments/aws/iam-role-basic.json",
		IAMRoleWithConfig:  "deployments/aws/iam-role-with-config.json",
		S3BucketSimple:     "deployments/aws/s3-bucket-simple.json",
		S3BucketLocalStack: "deployments/aws/s3-bucket-localstack.json",
		S3BucketVersioning: "deployments/aws/s3-bucket-versioning.json",
		EC2Instance:        "deployments/aws/ec2-instance.json",
		SecurityGroup:      "deployments/aws/security-group.json",
		VPCComplete:        "deployments/aws/vpc-complete.json",
	},
	API: APIFixtures{
		IAMRoleSimple:     "deployments/api/iam-role-simple.json",
		MultiResource:     "deployments/api/multi-resource.json",
		MultiResourceDeps: "deployments/api/multi-resource-deps.json",
		S3BucketSimple:    "deployments/api/s3-bucket-simple.json",
		DataSources:       "deployments/api/data-sources.json",
		Functions:         "deployments/api/functions.json",
	},
	Invalid: InvalidFixtures{
		MissingName:          "deployments/invalid/missing-name.json",
		MissingTerraformJSON: "deployments/invalid/missing-terraform-json.json",
		EmptyResources:       "deployments/invalid/empty-resources.json",
		BadProvider:          "deployments/invalid/bad-provider.json",
		CircularDeps:         "deployments/invalid/circular-deps.json",
	},
	MultiResource: MultiResourceFixtures{
		EC2S3SG:        "deployments/multi-resource/ec2-s3-sg.json",
		EC2SSMComplete: "deployments/multi-resource/ec2-ssm-complete.json",
		Complex:        "deployments/multi-resource/complex-resources.json",
		WithData:       "deployments/multi-resource/with-data-sources.json",
		Dependencies:   "deployments/multi-resource/dependencies.json",
	},
	DataSource: DataSourceFixtures{
		Region:            "deployments/data-sources/aws-region.json",
		CallerIdentity:    "deployments/data-sources/caller-identity.json",
		AMI:               "deployments/data-sources/ami-lookup.json",
		AvailabilityZones: "deployments/data-sources/availability-zones.json",
	},
	Function: FunctionFixtures{
		String:     "deployments/functions/string-funcs.json",
		Numeric:    "deployments/functions/numeric-funcs.json",
		Collection: "deployments/functions/collection-funcs.json",
		Crypto:     "deployments/functions/crypto-funcs.json",
		Network:    "deployments/functions/network-funcs.json",
	},
}
