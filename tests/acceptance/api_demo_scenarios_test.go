// Package acceptance contains HTTP API acceptance tests that mirror real demo scenarios
// These tests validate the exact API endpoints and flows used in conference presentations
//
//go:build demo
// +build demo

package acceptance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/suite"

	"github.com/lattiam/lattiam/internal/config"
	"github.com/lattiam/lattiam/tests/helpers"
	"github.com/lattiam/lattiam/tests/testutil"
)

// APIDemoSuite tests HTTP API endpoints used in actual demos
// These tests follow the go-chi testing patterns and mirror the real demo flow
type APIDemoSuite struct {
	suite.Suite
	testServer         *helpers.TestServer
	localstack         *testutil.LocalStackContainer
	localstackEndpoint string
	s3Client           *s3.Client
	ec2Client          *ec2.Client
	iamClient          *iam.Client

	// capturedResources stores resource identifiers captured after creation
	// for post-deletion verification. Map key is "resourceType.resourceName"
	// e.g., "aws_s3_bucket.demo" -> bucket name
	capturedResources map[string]string
}

func TestAPIDemoSuite(t *testing.T) {
	// Skip if we don't have AWS credentials (these are acceptance tests)
	if os.Getenv("SKIP_AWS_TESTS") == "true" {
		t.Skip("Skipping AWS integration tests")
	}
	suite.Run(t, new(APIDemoSuite))
}

// Test timeout constants
const (
	shortTimeout  = 30 * time.Second
	mediumTimeout = 180 * time.Second // Increased from 60s to 180s to account for provider download time
	longTimeout   = 120 * time.Second
)

func (s *APIDemoSuite) SetupSuite() {
	// Start LocalStack container and get its endpoint
	s.localstack = testutil.SetupLocalStackWithServices(s.T(), "s3,ec2,iam,sts")
	s.localstackEndpoint = s.localstack.GetEndpoint()

	// Set environment variables for AWS SDK to use LocalStack
	s.T().Setenv("AWS_ENDPOINT_URL", s.localstackEndpoint)
	s.T().Setenv("LOCALSTACK_ENDPOINT", s.localstackEndpoint)
	s.T().Setenv("AWS_ACCESS_KEY_ID", "test")
	s.T().Setenv("AWS_SECRET_ACCESS_KEY", "test")
	s.T().Setenv("AWS_REGION", "us-east-1")
	s.T().Setenv("AWS_DEFAULT_REGION", "us-east-1")

	// Create temporary directory for state
	tempDir := s.T().TempDir()
	s.T().Setenv("LATTIAM_STATE_DIR", tempDir)

	// Start test server using existing helper
	s.testServer = helpers.StartTestServer(s.T())

	// Initialize AWS clients for LocalStack
	s.initializeAWSClients()

	// Initialize captured resources map
	s.capturedResources = make(map[string]string)

	s.T().Cleanup(func() {
		// Stop test server
		s.testServer.Stop(s.T())
		// Environment variables are automatically cleaned up by t.Setenv
	})
}

func (s *APIDemoSuite) TestDemo1_S3BucketSimple() {
	s.testS3BucketSimpleDemo()
}

func (s *APIDemoSuite) TestDemo2_MultiResourceDependencies() {
	s.testMultiResourceDependenciesDemo()
}

func (s *APIDemoSuite) TestDemo3_FunctionShowcase() {
	s.testFunctionShowcaseDemo()
}

func (s *APIDemoSuite) TestDemo4_DataSourceDemo() {
	s.testDataSourceDemo()
}

func (s *APIDemoSuite) TestDemo5_UpdateLifecycle() {
	s.testUpdateLifecycleDemo()
}

func (s *APIDemoSuite) TestDemo6_ReplaceLifecycle() {
	s.testReplaceLifecycleDemo()
}

func (s *APIDemoSuite) TestDemo7_EC2ComplexDependencies() {
	s.testEC2ComplexDependenciesDemo()
}

// testS3BucketSimpleDemo replicates Demo 1 from the presentation
func (s *APIDemoSuite) testS3BucketSimpleDemo() {
	// Step 1: Load demo JSON (same as presentation)
	demoJSON := s.loadDemoJSON("01-s3-bucket-simple-localstack.json")

	// Inject LocalStack endpoint into AWS provider configuration
	s.injectLocalStackEndpoint(demoJSON)

	// Step 2: Create deployment via API (exact curl equivalent)
	deploymentReq := map[string]interface{}{
		"name":           "demo-s3-bucket-simple",
		"terraform_json": demoJSON["terraform_json"],
	}

	createResp := s.createDeployment(deploymentReq)

	// Safe type assertion with proper error handling
	deploymentID, ok := createResp["id"].(string)
	s.Require().True(ok, "Deployment ID should be a string, got %T", createResp["id"])
	s.Require().NotEmpty(deploymentID, "Should receive non-empty deployment ID")

	// Step 3: Poll for completion (like demo script)
	// Using mediumTimeout (60s) for S3 bucket creation which can sometimes take longer
	deployment := s.waitForDeploymentCompletion(deploymentID, mediumTimeout)

	// Step 4: Validate results (like presentation validation)
	s.Assert().Equal(config.DeploymentStatusCompleted, deployment["status"], "Deployment MUST complete successfully")

	// Step 4.1: Capture all resource identifiers from the API describe endpoint response
	// The waitForDeploymentCompletion already used GET /api/v1/deployments/{id} to get this data
	// Now we systematically extract all AWS resource identifiers for post-deletion verification
	s.captureResourceIdentifiers(deployment)

	// Validate resources are in expected array format (not map format)
	resources := s.validateResourcesArrayFormat(deployment, 2)
	s.validateResourceTypes(resources, []string{"random_string", "aws_s3_bucket"})

	// Step 5: Extract bucket name (like demo script) - for backward compatibility
	bucketName := s.extractS3BucketName(resources)
	s.Assert().NotEmpty(bucketName, "Should have created S3 bucket")
	s.Assert().Contains(bucketName, "lattiam-simple-bucket-", "Bucket should have correct prefix")

	// Step 6: Verify bucket actually exists in LocalStack
	s.verifyS3BucketExists(bucketName)

	s.T().Logf("✓ Demo 1 Success: Created bucket %s", bucketName)

	// Step 7: Cleanup (delete deployment)
	s.deleteDeployment(deploymentID, mediumTimeout)

	// Step 8: Verify all captured resources are actually destroyed from LocalStack
	// This uses the identifiers captured from the deployment state after creation
	s.verifyCapturedResourcesDeleted()
}

// testMultiResourceDependenciesDemo replicates Demo 2 from the presentation
func (s *APIDemoSuite) testMultiResourceDependenciesDemo() {
	// Load demo JSON
	demoJSON := s.loadDemoJSON("02-multi-resource-dependencies-localstack.json")

	// Inject LocalStack endpoint into AWS provider configuration
	s.injectLocalStackEndpoint(demoJSON)

	// Create deployment
	deploymentReq := map[string]interface{}{
		"name":           "demo-multi-resource-dependencies",
		"terraform_json": demoJSON["terraform_json"],
	}

	createResp := s.createDeployment(deploymentReq)

	// Safe type assertion
	deploymentID, ok := createResp["id"].(string)
	s.Require().True(ok, "Deployment ID should be a string, got %T", createResp["id"])
	s.Require().NotEmpty(deploymentID, "Should receive non-empty deployment ID")

	// Wait for completion (dependencies take longer)
	deployment := s.waitForDeploymentCompletion(deploymentID, mediumTimeout)

	// Should complete successfully
	s.Assert().Equal(config.DeploymentStatusCompleted, deployment["status"], "Multi-resource deployment MUST complete successfully")

	// Validate resources are in expected array format
	resources := s.validateResourcesArrayFormat(deployment, 4)
	s.validateResourceTypes(resources, []string{"random_string", "aws_s3_bucket", "aws_iam_role"})

	// Validate dependency resolution
	// (random_string should complete, and IAM role should complete)
	s.validateDependencyAnalysis(resources)

	// Extract and verify S3 bucket exists in LocalStack
	bucketName := s.extractS3BucketName(resources)
	if bucketName != "" {
		s.verifyS3BucketExists(bucketName)
	}

	s.T().Logf("✓ Demo 2 Success: Multi-resource deployment with dependencies")

	// Cleanup
	s.deleteDeployment(deploymentID, time.Duration(config.DefaultTimeout30s)*time.Second)

	// Verify S3 bucket is destroyed from LocalStack
	if bucketName != "" {
		s.verifyS3BucketDestroyed(bucketName)
	}
}

// testErrorHandlingDemo validates error scenarios seen in demos
func (s *APIDemoSuite) testErrorHandlingDemo() {
	// Test 1: Invalid JSON
	invalidJSON := map[string]interface{}{
		"name": "invalid-demo",
		"terraform_json": map[string]interface{}{
			"resource": map[string]interface{}{
				"invalid_resource_type": map[string]interface{}{
					"test": map[string]interface{}{},
				},
			},
		},
	}

	resp := s.makeAPIRequest("POST", "/api/v1/deployments", invalidJSON)
	// Note: Invalid resource types are accepted by API but fail during deployment
	s.Assert().Equal(http.StatusCreated, resp.StatusCode, "API accepts invalid resource types")

	// Extract deployment ID and wait for it to fail
	var invalidResp map[string]interface{}
	err := json.NewDecoder(resp.Body).Decode(&invalidResp)
	s.Require().NoError(err, "Should decode response")
	invalidID := invalidResp["id"].(string)

	// Should fail during deployment due to invalid resource type
	invalidDeployment := s.waitForDeploymentCompletion(invalidID, time.Duration(config.DefaultTimeout30s)*time.Second)
	s.Assert().Equal(config.DeploymentStatusFailed, invalidDeployment["status"], "Should fail with invalid resource type")

	// Test 2: Missing required fields
	missingFieldsJSON := map[string]interface{}{
		"name": "missing-fields-demo",
		"terraform_json": map[string]interface{}{
			"resource": map[string]interface{}{
				"aws_s3_bucket": map[string]interface{}{
					"test": map[string]interface{}{
						// Missing required 'bucket' field
					},
				},
			},
		},
	}

	createResp := s.createDeployment(missingFieldsJSON)
	deploymentID := createResp["id"].(string)

	// Should fail during deployment
	deployment := s.waitForDeploymentCompletion(deploymentID, time.Duration(config.DefaultTimeout30s)*time.Second)
	s.Assert().Equal(config.DeploymentStatusFailed, deployment["status"], "Should fail with missing required fields")

	s.T().Log("✓ Error handling validation complete")
}

// testFunctionShowcaseDemo replicates Demo 3 from the presentation (Terraform functions)
func (s *APIDemoSuite) testFunctionShowcaseDemo() {
	// Load demo JSON
	demoJSON := s.loadDemoJSON("03-function-showcase-localstack.json")

	// Create deployment
	deploymentReq := map[string]interface{}{
		"name":           "demo-function-showcase",
		"terraform_json": demoJSON["terraform_json"],
	}

	createResp := s.createDeployment(deploymentReq)
	deploymentID := createResp["id"].(string)

	// Wait for completion
	deployment := s.waitForDeploymentCompletion(deploymentID, time.Duration(config.DefaultTimeout60s)*time.Second)

	// Should complete successfully
	s.Assert().Equal(config.DeploymentStatusCompleted, deployment["status"], "Function showcase deployment MUST complete successfully")

	// Capture all resource identifiers from the API response
	s.captureResourceIdentifiers(deployment)

	// Validate resources are in expected array format
	resources := s.validateResourcesArrayFormat(deployment, -1) // Don't validate count for function showcase

	// Validate that function evaluation worked
	s.validateFunctionEvaluation(resources)

	s.T().Logf("✓ Demo 3 Success: Function showcase deployment")

	// Cleanup
	s.deleteDeployment(deploymentID, time.Duration(config.DefaultTimeout30s)*time.Second)

	// Verify all captured resources are destroyed from LocalStack
	s.verifyCapturedResourcesDeleted()
}

// testDataSourceDemo replicates Demo 4 from the presentation (Data sources)
func (s *APIDemoSuite) testDataSourceDemo() {
	// Load demo JSON
	demoJSON := s.loadDemoJSON("04-data-source-demo-localstack.json")

	// Inject LocalStack endpoint into AWS provider configuration
	s.injectLocalStackEndpoint(demoJSON)

	// Create deployment
	deploymentReq := map[string]interface{}{
		"name":           "demo-data-source",
		"terraform_json": demoJSON["terraform_json"],
	}

	createResp := s.createDeployment(deploymentReq)
	deploymentID := createResp["id"].(string)

	// Wait for completion (data sources take time to query)
	deployment := s.waitForDeploymentCompletion(deploymentID, time.Duration(config.DefaultTimeout90s)*time.Second)

	// Should complete successfully
	s.Assert().Equal(config.DeploymentStatusCompleted, deployment["status"], "Data source deployment MUST complete successfully")

	// Capture all resource identifiers from the API response
	s.captureResourceIdentifiers(deployment)

	// Validate resources are in expected array format
	resources := s.validateResourcesArrayFormat(deployment, -1) // Variable count for data source demo

	// Validate that data source resolution worked
	s.validateDataSourceResolution(resources)

	// Extract and verify S3 bucket exists in LocalStack (if present)
	bucketName := s.extractS3BucketName(resources)
	if bucketName != "" {
		s.verifyS3BucketExists(bucketName)
	}

	s.T().Logf("✓ Demo 4 Success: Data source deployment")

	// Cleanup
	s.deleteDeployment(deploymentID, time.Duration(config.DefaultTimeout30s)*time.Second)

	// Verify all captured resources are destroyed from LocalStack
	s.verifyCapturedResourcesDeleted()
}

// testUpdateLifecycleDemo replicates Demo 5 from the presentation (Update lifecycle)
func (s *APIDemoSuite) testUpdateLifecycleDemo() {
	// Step 1: Create initial S3 bucket
	demoJSON := s.loadDemoJSON("01-s3-bucket-simple-localstack.json")

	// Inject LocalStack endpoint into AWS provider configuration
	s.injectLocalStackEndpoint(demoJSON)

	deploymentReq := map[string]interface{}{
		"name":           "demo-update-lifecycle",
		"terraform_json": demoJSON["terraform_json"],
	}

	createResp := s.createDeployment(deploymentReq)
	deploymentID := createResp["id"].(string)

	// Wait for initial creation
	initialDeployment := s.waitForDeploymentCompletion(deploymentID, time.Duration(config.DefaultTimeout60s)*time.Second)
	s.Assert().Equal(config.DeploymentStatusCompleted, initialDeployment["status"], "Initial deployment MUST succeed")

	// Step 2: Update with tags (in-place update)
	updateJSON := s.loadDemoJSON("05a-s3-bucket-update-tags-localstack.json")

	// Inject LocalStack endpoint into AWS provider configuration
	s.injectLocalStackEndpoint(updateJSON)

	updateReq := map[string]interface{}{
		"name":           "demo-update-lifecycle",
		"terraform_json": updateJSON["terraform_json"],
	}

	resp := s.makeAPIRequest("PUT", "/api/v1/deployments/"+deploymentID, updateReq)
	s.Assert().Equal(http.StatusOK, resp.StatusCode, "Should accept update request")

	// Parse update response to get update_id if available
	var updateResp map[string]interface{}
	err := json.NewDecoder(resp.Body).Decode(&updateResp)
	s.Require().NoError(err, "Should decode update response")

	// Log the update response
	if updateID, ok := updateResp["update_id"]; ok {
		s.T().Logf("Update operation started with ID: %s", updateID)
	}

	// Wait for update operation to complete
	// The update happens asynchronously, so we need to give it time to:
	// 1. Process the update deployment
	// 2. Apply the changes
	// 3. Update the original deployment's result
	s.T().Logf("Waiting for update operation to complete and sync state...")

	// Poll for a bit to ensure the update has propagated
	var updatedDeployment map[string]interface{}
	maxAttempts := 10
	for i := 0; i < maxAttempts; i++ {
		time.Sleep(2 * time.Second)

		// Get the deployment state
		resp := s.makeAPIRequest("GET", "/api/v1/deployments/"+deploymentID, nil)
		s.Require().Equal(http.StatusOK, resp.StatusCode, "Should get deployment")

		err := json.NewDecoder(resp.Body).Decode(&updatedDeployment)
		s.Require().NoError(err, "Should decode deployment response")

		// Check if we have resources with tags
		if resources, ok := updatedDeployment["resources"].([]interface{}); ok {
			for _, res := range resources {
				if resource, ok := res.(map[string]interface{}); ok {
					if resource["type"] == "aws_s3_bucket" {
						if state, ok := resource["state"].(map[string]interface{}); ok {
							if tags, ok := state["tags"].(map[string]interface{}); ok && len(tags) > 0 {
								s.T().Logf("Tags found after %d attempts", i+1)
								goto done
							}
						}
					}
				}
			}
		}
		s.T().Logf("Attempt %d: Tags not found yet, waiting...", i+1)
	}
done:

	// Validate update worked - MUST succeed, not fail
	if updatedDeployment["status"] != config.DeploymentStatusCompleted {
		// Debug: Print error if update failed
		if err, ok := updatedDeployment["error"]; ok {
			s.T().Logf("Update failed with error: %v", err)
		}
		if lastErr, ok := updatedDeployment["last_error"]; ok {
			s.T().Logf("Last error: %v", lastErr)
		}
		s.T().Logf("Full deployment state: %+v", updatedDeployment)
	}
	s.Assert().Equal(config.DeploymentStatusCompleted, updatedDeployment["status"], "Update MUST complete successfully")

	// Validate resources are in expected array format after update
	resources := s.validateResourcesArrayFormat(updatedDeployment, -1) // Variable count for update

	// Validate tags were added
	s.validateTagsAdded(resources)

	// Extract and verify S3 bucket exists in LocalStack after update
	bucketName := s.extractS3BucketName(resources)
	if bucketName != "" {
		s.verifyS3BucketExists(bucketName)
	}

	s.T().Logf("✓ Demo 5 Success: Update lifecycle (in-place)")

	// Cleanup
	s.deleteDeployment(deploymentID, time.Duration(config.DefaultTimeout30s)*time.Second)

	// Verify S3 bucket is destroyed from LocalStack
	if bucketName != "" {
		s.verifyS3BucketDestroyed(bucketName)
	}
}

// testReplaceLifecycleDemo replicates Demo 6 from the presentation (Replace lifecycle)
func (s *APIDemoSuite) testReplaceLifecycleDemo() {
	// Step 1: Create initial S3 bucket
	demoJSON := s.loadDemoJSON("01-s3-bucket-simple-localstack.json")

	// Inject LocalStack endpoint into AWS provider configuration
	s.injectLocalStackEndpoint(demoJSON)

	deploymentReq := map[string]interface{}{
		"name":           "demo-replace-lifecycle",
		"terraform_json": demoJSON["terraform_json"],
	}

	createResp := s.createDeployment(deploymentReq)
	deploymentID := createResp["id"].(string)

	// Wait for initial creation
	initialDeployment := s.waitForDeploymentCompletion(deploymentID, time.Duration(config.DefaultTimeout60s)*time.Second)
	s.Assert().Equal(config.DeploymentStatusCompleted, initialDeployment["status"], "Initial deployment MUST succeed")

	// Extract initial bucket name
	initialResources := s.validateResourcesArrayFormat(initialDeployment, -1)
	initialBucketName := s.extractS3BucketName(initialResources)

	// Step 2: Update with new bucket name (replace)
	replaceJSON := s.loadDemoJSON("05b-s3-bucket-rename-localstack.json")

	// Inject LocalStack endpoint into AWS provider configuration
	s.injectLocalStackEndpoint(replaceJSON)

	replaceReq := map[string]interface{}{
		"name":           "demo-replace-lifecycle",
		"terraform_json": replaceJSON["terraform_json"],
	}

	resp := s.makeAPIRequest("PUT", "/api/v1/deployments/"+deploymentID+"?force_update=true", replaceReq)

	// If not OK, log the error response
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		s.T().Logf("API error response (%d): %s", resp.StatusCode, string(bodyBytes))
		resp.Body = io.NopCloser(bytes.NewReader(bodyBytes)) // Reset body for assertion
	}

	s.Assert().Equal(http.StatusOK, resp.StatusCode, "Should accept replace request")

	// Parse update response to get update_id if available
	var updateResp map[string]interface{}
	err := json.NewDecoder(resp.Body).Decode(&updateResp)
	s.Require().NoError(err, "Should decode replace response")

	// Log the replace response
	if updateID, ok := updateResp["update_id"]; ok {
		s.T().Logf("Replace operation started with ID: %s", updateID)
	}

	// Wait for replace operation to complete
	// The replace happens asynchronously, so we need to give it time to:
	// 1. Process the replace deployment
	// 2. Delete and recreate resources
	// 3. Update the original deployment's result
	s.T().Logf("Waiting for replace operation to complete and sync state...")

	// Poll for a bit to ensure the replace has propagated
	var replacedDeployment map[string]interface{}
	maxAttempts := 15 // More attempts for replace since it takes longer
	for i := 0; i < maxAttempts; i++ {
		time.Sleep(2 * time.Second)

		// Get the deployment state
		resp := s.makeAPIRequest("GET", "/api/v1/deployments/"+deploymentID, nil)
		s.Require().Equal(http.StatusOK, resp.StatusCode, "Should get deployment")

		err := json.NewDecoder(resp.Body).Decode(&replacedDeployment)
		s.Require().NoError(err, "Should decode deployment response")

		// Check if the bucket name has changed (indicating replace completed)
		if resources, ok := replacedDeployment["resources"].([]interface{}); ok {
			newBucketName := s.extractS3BucketName(resources)
			if newBucketName != "" && newBucketName != initialBucketName {
				s.T().Logf("Replace completed after %d attempts - new bucket: %s", i+1, newBucketName)
				goto done
			}
		}
		s.T().Logf("Attempt %d: Replace not completed yet, waiting...", i+1)
	}
done:

	// Validate replacement worked - MUST succeed
	s.Assert().Equal(config.DeploymentStatusCompleted, replacedDeployment["status"], "Replacement MUST complete successfully")

	// Debug: check for errors
	if replacedDeployment["status"] != config.DeploymentStatusCompleted {
		if resourceErrors, ok := replacedDeployment["resource_errors"].([]interface{}); ok && len(resourceErrors) > 0 {
			s.T().Logf("Resource errors: %+v", resourceErrors)
		}
	}

	// Validate replaced deployment resources
	resources := s.validateResourcesArrayFormat(replacedDeployment, -1)

	// Debug: log all resources
	for _, res := range resources {
		resource := res.(map[string]interface{})
		s.T().Logf("Resource %s/%s status=%v error=%v state=%v",
			resource["type"], resource["name"], resource["status"], resource["error"], resource["state"])
	}

	// Validate bucket was actually replaced with new name
	newBucketName := s.extractS3BucketName(resources)
	s.Assert().NotEqual(initialBucketName, newBucketName, "Bucket should have been replaced with new name")
	s.Assert().Contains(newBucketName, "lattiam-renamed-bucket-", "New bucket should have 'renamed' in the name")

	// Verify new bucket exists in LocalStack
	s.verifyS3BucketExists(newBucketName)

	s.T().Logf("✓ Demo 6 Success: Replace lifecycle (destroy/recreate)")
	s.T().Logf("  Old bucket: %s → New bucket: %s", initialBucketName, newBucketName)

	// Cleanup
	s.deleteDeployment(deploymentID, time.Duration(config.DefaultTimeout30s)*time.Second)

	// Verify new bucket is destroyed from LocalStack
	s.verifyS3BucketDestroyed(newBucketName)
}

// testEC2ComplexDependenciesDemo replicates Demo 7 (most complex scenario)
func (s *APIDemoSuite) testEC2ComplexDependenciesDemo() {
	// Load demo JSON
	demoJSON := s.loadDemoJSON("06-ec2-complex-dependencies-localstack.json")

	// Inject LocalStack endpoint into AWS provider configuration
	s.injectLocalStackEndpoint(demoJSON)

	// Create deployment
	deploymentReq := map[string]interface{}{
		"name":           "demo-ec2-complex-dependencies",
		"terraform_json": demoJSON["terraform_json"],
	}

	createResp := s.createDeployment(deploymentReq)
	deploymentID := createResp["id"].(string)

	// Wait for completion (complex dependencies take longer) - increased timeout
	deployment := s.waitForDeploymentCompletion(deploymentID, 240*time.Second)

	// Should complete successfully even for complex scenarios
	s.Assert().Equal(config.DeploymentStatusCompleted, deployment["status"], "Complex deployment MUST complete successfully")

	// Validate resources are in expected array format
	resources := s.validateResourcesArrayFormat(deployment, -1) // Variable count for complex scenario

	// Validate complex dependency resolution worked
	s.validateComplexDependencyResolution(resources)

	// Extract resource names for LocalStack verification
	instanceName := s.extractResourceName(resources, "aws_instance")
	vpcName := s.extractResourceName(resources, "aws_vpc")

	// Verify resources exist in LocalStack
	if instanceName != "" {
		s.verifyEC2InstanceExists(instanceName)
	}
	if vpcName != "" {
		s.verifyVPCExists(vpcName)
	}

	s.T().Logf("✓ Demo 7 Success: Complex EC2 dependencies")

	// Cleanup with extended timeout for complex deployment
	s.deleteDeployment(deploymentID, 2*time.Duration(config.DefaultTimeout60s)*time.Second)

	// Verify resources are destroyed from LocalStack
	if instanceName != "" {
		s.verifyEC2InstanceDestroyed(instanceName)
	}
	if vpcName != "" {
		s.verifyVPCDestroyed(vpcName)
	}
}

// Helper methods

func (s *APIDemoSuite) loadDemoJSON(filename string) map[string]interface{} {
	// Get project root
	wd, err := os.Getwd()
	s.Require().NoError(err)

	// Navigate to demo directory (from tests/acceptance to project root)
	demoPath := fmt.Sprintf("%s/../../demo/terraform-json/%s", wd, filename)

	data, err := os.ReadFile(demoPath)
	s.Require().NoError(err, "Should load demo JSON file %s", filename)

	var demoJSON map[string]interface{}
	err = json.Unmarshal(data, &demoJSON)
	s.Require().NoError(err, "Should parse demo JSON")

	return demoJSON
}

func (s *APIDemoSuite) createDeployment(req map[string]interface{}) map[string]interface{} {
	resp := s.makeAPIRequest("POST", "/api/v1/deployments", req)
	s.Require().Equal(http.StatusCreated, resp.StatusCode, "Should create deployment successfully")

	var result map[string]interface{}
	err := json.NewDecoder(resp.Body).Decode(&result)
	s.Require().NoError(err, "Should decode response")

	return result
}

func (s *APIDemoSuite) waitForDeploymentCompletion(deploymentID string, timeout time.Duration) map[string]interface{} {
	start := time.Now()
	attempt := 0

	for time.Since(start) < timeout {
		resp := s.makeAPIRequest("GET", "/api/v1/deployments/"+deploymentID, nil)
		s.Require().Equal(http.StatusOK, resp.StatusCode, "Should get deployment status")

		var deployment map[string]interface{}
		err := json.NewDecoder(resp.Body).Decode(&deployment)
		s.Require().NoError(err, "Should decode deployment response")

		status, ok := deployment["status"].(string)
		s.Require().True(ok, "Should have status field")

		elapsed := time.Since(start)

		if status == config.DeploymentStatusCompleted || status == config.DeploymentStatusFailed || status == "success" {
			s.T().Logf("✓ Deployment %s [%s] completed after %d attempts in %v", deploymentID, status, attempt+1, elapsed.Round(time.Second))
			return deployment
		}

		// Exponential backoff: 1s, 2s, 4s, 8s, 16s, then cap at 30s
		backoff := time.Duration(1<<uint(attempt)) * time.Second
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}

		// Compact log: status, attempt, elapsed time, next backoff in one line
		s.T().Logf("⏳ Deployment %s [%s] attempt %d, elapsed %v, next check in %v",
			deploymentID, status, attempt+1, elapsed.Round(time.Second), backoff)

		time.Sleep(backoff)
		attempt++
	}

	s.T().Fatalf("Deployment %s did not complete within %v after %d attempts", deploymentID, timeout, attempt)
	return nil
}

func (s *APIDemoSuite) deleteDeployment(deploymentID string, timeout time.Duration) {
	resp := s.makeAPIRequest("DELETE", "/api/v1/deployments/"+deploymentID, nil)
	// Accept 200 (OK), 202 (Accepted), 204 (No Content), and 404 (already cleaned up)
	s.Assert().Contains([]int{http.StatusOK, http.StatusAccepted, http.StatusNoContent, http.StatusNotFound}, resp.StatusCode,
		"Should delete deployment or already be gone")

	// If we get 202, log that deletion is continuing in background
	if resp.StatusCode == http.StatusAccepted {
		s.T().Logf("Deployment %s deletion continuing in background due to provider issues", deploymentID)
	}

	// Verify the deployment is actually deleted by polling
	s.verifyDeploymentDeleted(deploymentID, timeout)
}

// verifyDeploymentDeleted ensures the deployment is actually deleted
func (s *APIDemoSuite) verifyDeploymentDeleted(deploymentID string, timeout time.Duration) {
	start := time.Now()
	attempt := 0

	for time.Since(start) < timeout {
		resp := s.makeAPIRequest("GET", "/api/v1/deployments/"+deploymentID, nil)

		elapsed := time.Since(start)

		if resp.StatusCode == http.StatusNotFound {
			// Good - deployment is deleted
			s.T().Logf("✓ Deployment %s successfully deleted after %d attempts over %v", deploymentID, attempt+1, elapsed.Round(time.Second))
			return
		}

		if resp.StatusCode == http.StatusOK {
			// Check if status is "destroyed"
			var deployment map[string]interface{}
			err := json.NewDecoder(resp.Body).Decode(&deployment)
			s.Require().NoError(err, "Should decode deployment response")

			status, ok := deployment["status"].(string)
			s.Require().True(ok, "Should have status field")

			if status == config.DeploymentStatusDestroyed {
				s.T().Logf("✓ Deployment %s marked as destroyed after %d attempts over %v", deploymentID, attempt+1, elapsed.Round(time.Second))
				return
			}

			// Status not destroyed yet, continue waiting
		}

		// Exponential backoff: 1s, 2s, 4s, 8s, then cap at 10s for deletion checks
		backoff := time.Duration(1<<uint(attempt)) * time.Second
		if backoff > 10*time.Second {
			backoff = 10 * time.Second
		}

		s.T().Logf("🗑️  Deployment %s deletion attempt %d, elapsed %v, next check in %v", deploymentID, attempt+1, elapsed.Round(time.Second), backoff)
		time.Sleep(backoff)
		attempt++
	}

	// If we get here, deployment was not deleted in time
	s.T().Errorf("Deployment %s was not deleted within %v after %d attempts", deploymentID, timeout, attempt)
}

func (s *APIDemoSuite) makeAPIRequest(method, path string, body interface{}) *http.Response {
	var reqBody *bytes.Buffer
	if body != nil {
		jsonBody, err := json.Marshal(body)
		s.Require().NoError(err, "Should marshal request body")
		reqBody = bytes.NewBuffer(jsonBody)
	} else {
		reqBody = bytes.NewBuffer(nil)
	}

	req, err := http.NewRequest(method, s.testServer.URL+path, reqBody)
	s.Require().NoError(err, "Should create request")

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	s.Require().NoError(err, "Should make API request")

	return resp
}

func (s *APIDemoSuite) extractS3BucketName(resources []interface{}) string {
	for _, res := range resources {
		resource := res.(map[string]interface{})
		if resource["type"] == "aws_s3_bucket" {
			state, ok := resource["state"].(map[string]interface{})
			s.Require().True(ok, "S3 bucket should have state")

			bucket, ok := state["bucket"].(string)
			s.Require().True(ok, "S3 bucket should have bucket name")

			return bucket
		}
	}
	return ""
}

func (s *APIDemoSuite) validateDependencyResolution(resources []interface{}) {
	var randomStringResult string
	var bucketNames []string

	// Extract random string result and bucket names
	for _, res := range resources {
		resource := res.(map[string]interface{})
		resourceType := resource["type"].(string)
		state := resource["state"].(map[string]interface{})

		switch resourceType {
		case "random_string":
			result, ok := state["result"].(string)
			s.Require().True(ok, "Random string should have result")
			randomStringResult = result
		case "aws_s3_bucket":
			bucket, ok := state["bucket"].(string)
			s.Require().True(ok, "S3 bucket should have bucket name")
			bucketNames = append(bucketNames, bucket)
		}
	}

	// Validate dependencies were resolved
	s.Require().NotEmpty(randomStringResult, "Should have random string result")
	s.Require().Len(bucketNames, 2, "Should have 2 S3 buckets")

	// Validate bucket names contain the random string
	for _, bucketName := range bucketNames {
		s.Assert().Contains(bucketName, randomStringResult,
			"Bucket name should contain random string result (dependency resolved)")
	}
}

func (s *APIDemoSuite) validateDependencyAnalysis(resources []interface{}) {
	// Track expected resources for dependency analysis
	expectedResources := map[string]bool{
		"random_string.stack_suffix": false,
		"aws_s3_bucket.app_data":     false,
		"aws_s3_bucket.app_logs":     false,
		"aws_iam_role.app_role":      false,
	}

	var randomStringResult string
	var appDataBucket, appLogsBucket, iamRoleName string

	// Process all resources and validate they completed successfully
	for _, res := range resources {
		resource := res.(map[string]interface{})
		resourceType := resource["type"].(string)
		name := resource["name"].(string)
		status := resource["status"].(string)

		s.Require().Equal("completed", status, "All resources must complete for dependency analysis: %s.%s", resourceType, name)

		resourceKey := resourceType + "." + name
		expectedResources[resourceKey] = true

		state, ok := resource["state"].(map[string]interface{})
		s.Require().True(ok, "Resource must have state: %s", resourceKey)

		// Extract values for dependency validation
		switch resourceKey {
		case "random_string.stack_suffix":
			result, ok := state["result"].(string)
			s.Require().True(ok, "Random string must have result")
			s.Assert().Len(result, 8, "Random string should be 8 characters")
			randomStringResult = result
		case "aws_s3_bucket.app_data":
			bucket, ok := state["bucket"].(string)
			s.Require().True(ok, "S3 bucket must have bucket name")
			appDataBucket = bucket
		case "aws_s3_bucket.app_logs":
			bucket, ok := state["bucket"].(string)
			s.Require().True(ok, "S3 bucket must have bucket name")
			appLogsBucket = bucket
		case "aws_iam_role.app_role":
			roleName, ok := state["name"].(string)
			s.Require().True(ok, "IAM role must have name")
			iamRoleName = roleName
		}
	}

	// Verify all expected resources were found
	for resourceKey, found := range expectedResources {
		s.Assert().True(found, "Required resource not found: %s", resourceKey)
	}

	// Validate dependency resolution - all dependent resources should contain the random string
	s.Assert().NotEmpty(randomStringResult, "Random string result required for dependency validation")

	s.Assert().Contains(appDataBucket, randomStringResult, "app_data bucket should use random string result")
	s.Assert().Contains(appDataBucket, "lattiam-app-data", "app_data bucket should have correct prefix")

	s.Assert().Contains(appLogsBucket, randomStringResult, "app_logs bucket should use random string result")
	s.Assert().Contains(appLogsBucket, "lattiam-app-logs", "app_logs bucket should have correct prefix")

	s.Assert().Contains(iamRoleName, randomStringResult, "IAM role should use random string result")
	s.Assert().Contains(iamRoleName, "lattiam-app-role", "IAM role should have correct prefix")

	// Validate that buckets have different names (using same suffix but different prefixes)
	s.Assert().NotEqual(appDataBucket, appLogsBucket, "Buckets should have different names")

	s.T().Logf("✓ Dependency validation complete:")
	s.T().Logf("  Random suffix: %s", randomStringResult)
	s.T().Logf("  Data bucket: %s", appDataBucket)
	s.T().Logf("  Logs bucket: %s", appLogsBucket)
	s.T().Logf("  IAM role: %s", iamRoleName)
}

func (s *APIDemoSuite) validateFunctionEvaluation(resources []interface{}) {
	// Track expected resources with their function validations
	expectedResources := map[string]bool{
		"uuid_example":         false,
		"timestamp_example":    false,
		"string_functions":     false,
		"hash_functions":       false,
		"math_functions":       false,
		"collection_functions": false,
	}

	for _, res := range resources {
		resource := res.(map[string]interface{})
		if resource["type"].(string) != "random_string" {
			continue
		}

		status := resource["status"].(string)
		s.Require().Equal("completed", status, "All function resources must complete successfully")

		name := resource["name"].(string)
		state, ok := resource["state"].(map[string]interface{})
		s.Require().True(ok, "Resource must have state: %s", name)

		keepers, ok := state["keepers"].(map[string]interface{})
		s.Require().True(ok, "Resource must have keepers with function results: %s", name)

		// Validate specific function outputs based on resource name
		switch name {
		case "uuid_example":
			s.validateUUIDFunctions(keepers)
			expectedResources[name] = true
		case "timestamp_example":
			s.validateTimestampFunctions(keepers)
			expectedResources[name] = true
		case "string_functions":
			s.validateStringFunctions(keepers)
			expectedResources[name] = true
		case "hash_functions":
			s.validateHashFunctions(keepers)
			expectedResources[name] = true
		case "math_functions":
			s.validateMathFunctions(keepers)
			expectedResources[name] = true
		case "collection_functions":
			s.validateCollectionFunctions(keepers)
			expectedResources[name] = true
		}
	}

	// Ensure all expected resources were found and validated
	for resource, validated := range expectedResources {
		s.Assert().True(validated, "Required function resource not found or validated: %s", resource)
	}
}

// validateUUIDFunctions validates UUID function outputs
func (s *APIDemoSuite) validateUUIDFunctions(keepers map[string]interface{}) {
	uniqueID, ok := keepers["unique_id"].(string)
	s.Require().True(ok, "unique_id should be a string")
	s.Assert().Len(uniqueID, 36, "UUID should be 36 characters long")
	s.Assert().Regexp("^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$", uniqueID, "Should be valid UUID format")
}

// validateTimestampFunctions validates timestamp function outputs
func (s *APIDemoSuite) validateTimestampFunctions(keepers map[string]interface{}) {
	createdAt, ok := keepers["created_at"].(string)
	s.Require().True(ok, "created_at should be a string")
	s.Assert().Regexp("^\\d{4}-\\d{2}-\\d{2}T\\d{2}:\\d{2}:\\d{2}Z$", createdAt, "Should be valid RFC3339 timestamp")

	formattedDate, ok := keepers["formatted_date"].(string)
	s.Require().True(ok, "formatted_date should be a string")
	s.Assert().Regexp("^\\d{4}-\\d{2}-\\d{2} \\d{2}:\\d{2}:\\d{2}$", formattedDate, "Should be formatted date")
}

// validateStringFunctions validates string manipulation function outputs
func (s *APIDemoSuite) validateStringFunctions(keepers map[string]interface{}) {
	s.Assert().Equal("LATTIAM ROCKS", keepers["upper_demo"], "upper() should convert to uppercase")
	s.Assert().Equal("terraform api", keepers["lower_demo"], "lower() should convert to lowercase")
	s.Assert().Equal("Hello World from Lattiam!", keepers["formatted"], "format() should substitute values")

	base64Val, ok := keepers["base64_encoded"].(string)
	s.Require().True(ok, "base64_encoded should be a string")
	s.Assert().Regexp("^[A-Za-z0-9+/]*={0,2}$", base64Val, "Should be valid base64")

	s.Assert().Equal("spaces removed", keepers["trimmed"], "trimspace() should remove leading/trailing spaces")
	s.Assert().Equal("Hello World", keepers["replaced"], "replace() should substitute characters")
}

// validateHashFunctions validates cryptographic hash function outputs
func (s *APIDemoSuite) validateHashFunctions(keepers map[string]interface{}) {
	md5Val, ok := keepers["md5_example"].(string)
	s.Require().True(ok, "md5_example should be a string")
	s.Assert().Len(md5Val, 32, "MD5 hash should be 32 characters")
	s.Assert().Regexp("^[a-f0-9]+$", md5Val, "MD5 should be lowercase hex")

	sha256Val, ok := keepers["sha256_example"].(string)
	s.Require().True(ok, "sha256_example should be a string")
	s.Assert().Len(sha256Val, 16, "Substr of SHA256 should be 16 characters")
	s.Assert().Regexp("^[a-f0-9]+$", sha256Val, "SHA256 should be lowercase hex")
}

// validateMathFunctions validates mathematical function outputs
func (s *APIDemoSuite) validateMathFunctions(keepers map[string]interface{}) {
	s.Assert().Equal("99", keepers["max_value"], "max() should return largest value")
	s.Assert().Equal("3", keepers["min_value"], "min() should return smallest value")
	s.Assert().Equal("100", keepers["abs_negative"], "abs() should return absolute value")
	s.Assert().Equal("4", keepers["ceil_example"], "ceil() should round up")
	s.Assert().Equal("9", keepers["floor_example"], "floor() should round down")
}

// validateCollectionFunctions validates collection manipulation function outputs
func (s *APIDemoSuite) validateCollectionFunctions(keepers map[string]interface{}) {
	s.Assert().Equal("4", keepers["list_length"], "length() should count list elements")
	s.Assert().Equal("lattiam-api-v1", keepers["joined"], "join() should concatenate with delimiter")
	s.Assert().Equal("second", keepers["element_at"], "element() should return item at index")
	s.Assert().Equal("true", keepers["contains_check"], "contains() should return boolean as string")
}

func (s *APIDemoSuite) validateDataSourceResolution(resources []interface{}) {
	// Track required resources (data sources may not be returned as resources)
	foundResources := map[string]bool{
		"random_string.suffix":  false,
		"aws_s3_bucket.example": false,
	}

	var bucketState map[string]interface{}

	for _, res := range resources {
		resource := res.(map[string]interface{})
		resourceType := resource["type"].(string)
		name := resource["name"].(string)
		status := resource["status"].(string)

		s.Assert().Equal("completed", status, "All resources must complete successfully: %s.%s", resourceType, name)

		resourceKey := resourceType + "." + name

		// Track regular resources
		if resourceType == "random_string" && name == "suffix" {
			foundResources[resourceKey] = true
		} else if resourceType == "aws_s3_bucket" && name == "example" {
			foundResources[resourceKey] = true
			bucketState = resource["state"].(map[string]interface{})
		}

		// Data sources might be included as resources - validate if found
		if resourceType == "aws_caller_identity" && name == "current" {
			s.validateCallerIdentityData(resource)
		} else if resourceType == "aws_region" && name == "current" {
			s.validateRegionData(resource)
		}
	}

	// Verify all expected resources were found
	for resName, found := range foundResources {
		s.Assert().True(found, "Required resource not found: %s", resName)
	}

	// Validate that the S3 bucket used data source values (primary test)
	s.validateDataSourceUsage(bucketState)
}

func (s *APIDemoSuite) validateCallerIdentityData(resource map[string]interface{}) {
	state, ok := resource["state"].(map[string]interface{})
	s.Require().True(ok, "aws_caller_identity should have state")

	accountID, ok := state["account_id"].(string)
	s.Require().True(ok, "aws_caller_identity should provide account_id")
	s.Assert().NotEmpty(accountID, "account_id should not be empty")
	s.Assert().Regexp("^\\d{12}$", accountID, "account_id should be 12 digits")
}

func (s *APIDemoSuite) validateRegionData(resource map[string]interface{}) {
	state, ok := resource["state"].(map[string]interface{})
	s.Require().True(ok, "aws_region should have state")

	regionName, ok := state["name"].(string)
	s.Require().True(ok, "aws_region should provide name")
	s.Assert().Equal("us-east-1", regionName, "region should be us-east-1")
}

func (s *APIDemoSuite) validateDataSourceUsage(bucketState map[string]interface{}) {
	s.Require().NotNil(bucketState, "S3 bucket should have state")

	bucket, ok := bucketState["bucket"].(string)
	s.Require().True(ok, "S3 bucket should have bucket name")

	// Verify bucket name contains region (us-east-1)
	s.Assert().Contains(bucket, "us-east-1", "Bucket name should contain region from data source")
	s.Assert().Contains(bucket, "lattiam-ds-demo", "Bucket name should contain expected prefix")

	// Verify tags contain data source values
	tags, ok := bucketState["tags"].(map[string]interface{})
	s.Require().True(ok, "S3 bucket should have tags")

	region, ok := tags["Region"].(string)
	s.Require().True(ok, "Region tag should exist")
	s.Assert().Equal("us-east-1", region, "Region tag should use data source value")

	accountID, ok := tags["Account"].(string)
	s.Require().True(ok, "Account tag should exist")
	s.Assert().Regexp("^\\d{12}$", accountID, "Account tag should be valid account ID from data source")
}

func (s *APIDemoSuite) validateTagsAdded(resources []interface{}) {
	// Look for S3 bucket with tags
	foundTags := false
	var bucketTags map[string]interface{}

	s.T().Logf("[DEBUG] validateTagsAdded: Checking %d resources", len(resources))
	for _, res := range resources {
		resource := res.(map[string]interface{})
		s.T().Logf("[DEBUG] Resource type: %s, name: %s", resource["type"], resource["name"])
		if resource["type"].(string) == "aws_s3_bucket" {
			s.T().Logf("[DEBUG] Found S3 bucket resource")
			if state, ok := resource["state"].(map[string]interface{}); ok {
				keys := make([]string, 0, len(state))
				for k := range state {
					keys = append(keys, k)
				}
				s.T().Logf("[DEBUG] S3 bucket state keys: %v", keys)
				if tags, ok := state["tags"].(map[string]interface{}); ok && len(tags) > 0 {
					foundTags = true
					bucketTags = tags
					s.T().Logf("Tags found on S3 bucket: %v", tags)
				} else {
					s.T().Logf("[DEBUG] No tags found in state, tags value: %v (type: %T)", state["tags"], state["tags"])
				}
			} else {
				s.T().Logf("[DEBUG] No state found in resource")
			}
		}
	}

	// MUST find tags - this is not optional
	s.Require().True(foundTags, "S3 bucket MUST have tags after update")
	s.Require().NotNil(bucketTags, "Tags must not be nil")

	// Verify specific tags that should be present
	s.Assert().Equal("demo", bucketTags["Environment"], "Environment tag must be 'demo'")
	s.Assert().Equal("lattiam", bucketTags["ManagedBy"], "ManagedBy tag must be 'lattiam'")
	s.Assert().Equal("true", bucketTags["Updated"], "Updated tag must be 'true'")
}

func (s *APIDemoSuite) validateComplexDependencyResolution(resources []interface{}) {
	var completedResources []string
	var failedResources []string

	// Categorize resources by status
	for _, res := range resources {
		resource := res.(map[string]interface{})
		resourceType := resource["type"].(string)
		status := resource["status"].(string)

		if status == "completed" {
			completedResources = append(completedResources, resourceType)
		} else if status == "failed" {
			failedResources = append(failedResources, resourceType)
		}
	}

	// All resources must complete successfully for complex deployment
	expectedCompletedCount := len(resources)
	s.Assert().Len(completedResources, expectedCompletedCount, "All %d resources must complete successfully in complex deployment", expectedCompletedCount)

	s.T().Logf("Complex deployment completed resources: %v", completedResources)
	s.T().Logf("Complex deployment failed resources: %v", failedResources)
	s.T().Logf("Total resources: %d, Completed: %d, Failed: %d",
		len(resources), len(completedResources), len(failedResources))
}

// validateResourcesArrayFormat validates that resources are returned as an array format
// This is essential for API compatibility - resources must be an array of resource objects,
// not a map of resource keys to resource data
func (s *APIDemoSuite) validateResourcesArrayFormat(deployment map[string]interface{}, expectedCount int) []interface{} {
	// Check that resources field exists
	resourcesField, exists := deployment["resources"]
	s.Require().True(exists, "Deployment response must include 'resources' field. Response: %+v", deployment)

	// Check that resources is an array, not a map
	resources, ok := resourcesField.([]interface{})
	s.Require().True(ok, "Resources must be an array, got %T. This indicates the deployment handler is not formatting resources correctly. Resource value: %+v", resourcesField, resourcesField)

	// Check expected count (if specified)
	if expectedCount >= 0 {
		s.Require().Len(resources, expectedCount, "Should have %d resources", expectedCount)
	}

	// Validate each resource has required fields
	for i, res := range resources {
		resource, ok := res.(map[string]interface{})
		s.Require().True(ok, "Resource %d must be a map/object, got %T", i, res)

		// Validate required fields
		s.Require().Contains(resource, "type", "Resource %d missing 'type' field", i)
		s.Require().Contains(resource, "name", "Resource %d missing 'name' field", i)
		s.Require().Contains(resource, "status", "Resource %d missing 'status' field", i)
		s.Require().Contains(resource, "state", "Resource %d missing 'state' field", i)

		// Validate field types
		resourceType, ok := resource["type"].(string)
		s.Require().True(ok, "Resource %d type must be string, got %T", i, resource["type"])
		s.Assert().NotEmpty(resourceType, "Resource %d type must not be empty", i)

		resourceName, ok := resource["name"].(string)
		s.Require().True(ok, "Resource %d name must be string, got %T", i, resource["name"])
		s.Assert().NotEmpty(resourceName, "Resource %d name must not be empty", i)

		status, ok := resource["status"].(string)
		s.Require().True(ok, "Resource %d status must be string, got %T", i, resource["status"])
		s.Assert().NotEmpty(status, "Resource %d status must not be empty", i)

		state, ok := resource["state"].(map[string]interface{})
		s.Require().True(ok, "Resource %d state must be a map, got %T", i, resource["state"])
		s.Assert().NotEmpty(state, "Resource %d state must not be empty", i)
	}

	s.T().Logf("✓ Validated %d resources in correct array format", len(resources))
	return resources
}

// validateResourceTypes checks that all expected resource types are present
func (s *APIDemoSuite) validateResourceTypes(resources []interface{}, expectedTypes []string) {
	foundTypes := make(map[string]bool)

	for _, res := range resources {
		resource := res.(map[string]interface{})
		resourceType := resource["type"].(string)
		foundTypes[resourceType] = true
	}

	for _, expectedType := range expectedTypes {
		s.Assert().True(foundTypes[expectedType], "Expected resource type '%s' not found in resources. Found types: %v", expectedType, getMapKeys(foundTypes))
	}

	s.T().Logf("✓ Validated all expected resource types: %v", expectedTypes)
}

// getMapKeys returns the keys of a map as a slice (helper function)
func getMapKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// initializeAWSClients sets up AWS clients for LocalStack
func (s *APIDemoSuite) initializeAWSClients() {
	// Use the dynamic LocalStack endpoint from the container
	endpoint := s.localstackEndpoint
	region := "us-east-1"

	s.T().Logf("Initializing AWS clients with endpoint: %s", endpoint)

	ctx := context.Background()

	// Create a custom endpoint resolver for LocalStack
	customResolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
		return aws.Endpoint{
			URL:               endpoint,
			HostnameImmutable: true,
			PartitionID:       "aws",
			SigningRegion:     region,
		}, nil
	})

	// Create AWS config with LocalStack configuration
	cfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(region),
		awsconfig.WithEndpointResolverWithOptions(customResolver),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
	)
	s.Require().NoError(err, "Should create AWS config for LocalStack")

	// Initialize service clients with path-style addressing for S3
	s.s3Client = s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.UsePathStyle = true // Required for LocalStack
	})
	s.ec2Client = ec2.NewFromConfig(cfg)
	s.iamClient = iam.NewFromConfig(cfg)

	s.T().Log("✓ AWS clients initialized for LocalStack")
}

// verifyS3BucketExists checks if an S3 bucket exists in LocalStack
func (s *APIDemoSuite) verifyS3BucketExists(bucketName string) {
	s.T().Logf("Verifying S3 bucket exists: %s", bucketName)

	ctx := context.Background()
	_, err := s.s3Client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(bucketName),
	})
	s.Assert().NoError(err, "S3 bucket %s should exist in LocalStack", bucketName)
	s.T().Logf("✓ S3 bucket %s exists in LocalStack", bucketName)
}

// verifyS3BucketDestroyed checks if an S3 bucket is deleted from LocalStack
func (s *APIDemoSuite) verifyS3BucketDestroyed(bucketName string) {
	s.T().Logf("Verifying S3 bucket is destroyed: %s", bucketName)

	// Try multiple times as destruction might take a moment
	maxAttempts := 10
	ctx := context.Background()
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		_, err := s.s3Client.HeadBucket(ctx, &s3.HeadBucketInput{
			Bucket: aws.String(bucketName),
		})
		if err != nil {
			// Check if it's a "not found" error
			if strings.Contains(err.Error(), "NotFound") || strings.Contains(err.Error(), "NoSuchBucket") {
				s.T().Logf("✓ S3 bucket %s successfully destroyed from LocalStack (attempt %d)", bucketName, attempt)
				return
			}
		}

		if attempt < maxAttempts {
			s.T().Logf("S3 bucket %s still exists, attempt %d/%d, retrying...", bucketName, attempt, maxAttempts)
			time.Sleep(2 * time.Second)
		}
	}

	s.T().Errorf("S3 bucket %s was not destroyed from LocalStack after %d attempts", bucketName, maxAttempts)
}

// verifyEC2InstanceExists checks if an EC2 instance exists in LocalStack
func (s *APIDemoSuite) verifyEC2InstanceExists(instanceName string) string {
	s.T().Logf("Verifying EC2 instance exists: %s", instanceName)

	ctx := context.Background()
	result, err := s.ec2Client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		Filters: []types.Filter{
			{
				Name:   aws.String("tag:Name"),
				Values: []string{instanceName},
			},
			{
				Name:   aws.String("instance-state-name"),
				Values: []string{"running", "pending"},
			},
		},
	})
	s.Require().NoError(err, "Should be able to describe EC2 instances")
	s.Require().NotEmpty(result.Reservations, "Should find EC2 reservation for %s", instanceName)
	s.Require().NotEmpty(result.Reservations[0].Instances, "Should find EC2 instance for %s", instanceName)

	instance := result.Reservations[0].Instances[0]
	instanceID := *instance.InstanceId
	s.T().Logf("✓ EC2 instance %s exists in LocalStack with ID: %s", instanceName, instanceID)
	return instanceID
}

// verifyEC2InstanceDestroyed checks if an EC2 instance is terminated in LocalStack
func (s *APIDemoSuite) verifyEC2InstanceDestroyed(instanceName string) {
	s.T().Logf("Verifying EC2 instance is destroyed: %s", instanceName)

	// Try multiple times as termination might take a moment
	maxAttempts := 15
	ctx := context.Background()
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		result, err := s.ec2Client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
			Filters: []types.Filter{
				{
					Name:   aws.String("tag:Name"),
					Values: []string{instanceName},
				},
			},
		})
		s.Require().NoError(err, "Should be able to describe EC2 instances")

		if len(result.Reservations) == 0 {
			s.T().Logf("✓ EC2 instance %s successfully destroyed from LocalStack (attempt %d)", instanceName, attempt)
			return
		}

		// Check if instance is terminated
		instance := result.Reservations[0].Instances[0]
		state := string(instance.State.Name)
		if state == "terminated" || state == "shutting-down" {
			s.T().Logf("✓ EC2 instance %s is %s in LocalStack (attempt %d)", instanceName, state, attempt)
			return
		}

		if attempt < maxAttempts {
			s.T().Logf("EC2 instance %s is still %s, attempt %d/%d, retrying...", instanceName, state, attempt, maxAttempts)
			time.Sleep(3 * time.Second)
		}
	}

	s.T().Errorf("EC2 instance %s was not destroyed from LocalStack after %d attempts", instanceName, maxAttempts)
}

// verifyVPCExists checks if a VPC exists in LocalStack
func (s *APIDemoSuite) verifyVPCExists(vpcName string) string {
	s.T().Logf("Verifying VPC exists: %s", vpcName)

	ctx := context.Background()
	result, err := s.ec2Client.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{
		Filters: []types.Filter{
			{
				Name:   aws.String("tag:Name"),
				Values: []string{vpcName},
			},
		},
	})
	s.Require().NoError(err, "Should be able to describe VPCs")
	s.Require().NotEmpty(result.Vpcs, "Should find VPC for %s", vpcName)

	vpc := result.Vpcs[0]
	vpcID := *vpc.VpcId
	s.T().Logf("✓ VPC %s exists in LocalStack with ID: %s", vpcName, vpcID)
	return vpcID
}

// verifyVPCDestroyed checks if a VPC is deleted from LocalStack
func (s *APIDemoSuite) verifyVPCDestroyed(vpcName string) {
	s.T().Logf("Verifying VPC is destroyed: %s", vpcName)

	// Try multiple times as VPC deletion might take a moment
	maxAttempts := 10
	ctx := context.Background()
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		result, err := s.ec2Client.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{
			Filters: []types.Filter{
				{
					Name:   aws.String("tag:Name"),
					Values: []string{vpcName},
				},
			},
		})
		s.Require().NoError(err, "Should be able to describe VPCs")

		if len(result.Vpcs) == 0 {
			s.T().Logf("✓ VPC %s successfully destroyed from LocalStack (attempt %d)", vpcName, attempt)
			return
		}

		if attempt < maxAttempts {
			s.T().Logf("VPC %s still exists, attempt %d/%d, retrying...", vpcName, attempt, maxAttempts)
			time.Sleep(2 * time.Second)
		}
	}

	s.T().Errorf("VPC %s was not destroyed from LocalStack after %d attempts", vpcName, maxAttempts)
}

// verifyEC2InstanceDestroyedByID checks if an EC2 instance is terminated by ID
func (s *APIDemoSuite) verifyEC2InstanceDestroyedByID(instanceID string) {
	s.T().Logf("Verifying EC2 instance is destroyed by ID: %s", instanceID)

	// Try multiple times as termination might take a moment
	maxAttempts := 15
	ctx := context.Background()
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		result, err := s.ec2Client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
			InstanceIds: []string{instanceID},
		})

		// If instance not found, it's deleted
		if err != nil && strings.Contains(err.Error(), "InvalidInstanceID.NotFound") {
			s.T().Logf("✓ EC2 instance %s successfully destroyed from LocalStack (attempt %d)", instanceID, attempt)
			return
		}

		s.Require().NoError(err, "Should be able to describe EC2 instances")

		if len(result.Reservations) == 0 || len(result.Reservations[0].Instances) == 0 {
			s.T().Logf("✓ EC2 instance %s successfully destroyed from LocalStack (attempt %d)", instanceID, attempt)
			return
		}

		// Check if instance is terminated
		instance := result.Reservations[0].Instances[0]
		state := string(instance.State.Name)
		if state == "terminated" {
			s.T().Logf("✓ EC2 instance %s is %s in LocalStack (attempt %d)", instanceID, state, attempt)
			return
		}

		if attempt < maxAttempts {
			s.T().Logf("EC2 instance %s is still %s, attempt %d/%d, retrying...", instanceID, state, attempt, maxAttempts)
			time.Sleep(3 * time.Second)
		}
	}

	s.T().Errorf("EC2 instance %s was not destroyed from LocalStack after %d attempts", instanceID, maxAttempts)
}

// verifyVPCDestroyedByID checks if a VPC is deleted by ID
func (s *APIDemoSuite) verifyVPCDestroyedByID(vpcID string) {
	s.T().Logf("Verifying VPC is destroyed by ID: %s", vpcID)

	// Try multiple times as VPC deletion might take a moment
	maxAttempts := 10
	ctx := context.Background()
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		result, err := s.ec2Client.DescribeVpcs(ctx, &ec2.DescribeVpcsInput{
			VpcIds: []string{vpcID},
		})

		// If VPC not found, it's deleted
		if err != nil && strings.Contains(err.Error(), "InvalidVpcID.NotFound") {
			s.T().Logf("✓ VPC %s successfully destroyed from LocalStack (attempt %d)", vpcID, attempt)
			return
		}

		s.Require().NoError(err, "Should be able to describe VPCs")

		if len(result.Vpcs) == 0 {
			s.T().Logf("✓ VPC %s successfully destroyed from LocalStack (attempt %d)", vpcID, attempt)
			return
		}

		if attempt < maxAttempts {
			s.T().Logf("VPC %s still exists, attempt %d/%d, retrying...", vpcID, attempt, maxAttempts)
			time.Sleep(2 * time.Second)
		}
	}

	s.T().Errorf("VPC %s was not destroyed from LocalStack after %d attempts", vpcID, maxAttempts)
}

// verifyIAMRoleDestroyed checks if an IAM role is deleted
func (s *APIDemoSuite) verifyIAMRoleDestroyed(roleName string) {
	s.T().Logf("Verifying IAM role is destroyed: %s", roleName)

	// Try multiple times as role deletion might take a moment
	maxAttempts := 10
	ctx := context.Background()
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		_, err := s.iamClient.GetRole(ctx, &iam.GetRoleInput{
			RoleName: aws.String(roleName),
		})

		// If role not found, it's deleted
		if err != nil && strings.Contains(err.Error(), "NoSuchEntity") {
			s.T().Logf("✓ IAM role %s successfully destroyed from LocalStack (attempt %d)", roleName, attempt)
			return
		}

		if attempt < maxAttempts {
			s.T().Logf("IAM role %s still exists, attempt %d/%d, retrying...", roleName, attempt, maxAttempts)
			time.Sleep(2 * time.Second)
		}
	}

	s.T().Errorf("IAM role %s was not destroyed from LocalStack after %d attempts", roleName, maxAttempts)
}

// verifySecurityGroupDestroyedByID checks if a security group is deleted by ID
func (s *APIDemoSuite) verifySecurityGroupDestroyedByID(sgID string) {
	s.T().Logf("Verifying security group is destroyed by ID: %s", sgID)

	// Try multiple times as SG deletion might take a moment
	maxAttempts := 10
	ctx := context.Background()
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		result, err := s.ec2Client.DescribeSecurityGroups(ctx, &ec2.DescribeSecurityGroupsInput{
			GroupIds: []string{sgID},
		})

		// If SG not found, it's deleted
		if err != nil && strings.Contains(err.Error(), "InvalidGroup.NotFound") {
			s.T().Logf("✓ Security group %s successfully destroyed from LocalStack (attempt %d)", sgID, attempt)
			return
		}

		s.Require().NoError(err, "Should be able to describe security groups")

		if len(result.SecurityGroups) == 0 {
			s.T().Logf("✓ Security group %s successfully destroyed from LocalStack (attempt %d)", sgID, attempt)
			return
		}

		if attempt < maxAttempts {
			s.T().Logf("Security group %s still exists, attempt %d/%d, retrying...", sgID, attempt, maxAttempts)
			time.Sleep(2 * time.Second)
		}
	}

	s.T().Errorf("Security group %s was not destroyed from LocalStack after %d attempts", sgID, maxAttempts)
}

// verifySubnetDestroyedByID checks if a subnet is deleted by ID
func (s *APIDemoSuite) verifySubnetDestroyedByID(subnetID string) {
	s.T().Logf("Verifying subnet is destroyed by ID: %s", subnetID)

	// Try multiple times as subnet deletion might take a moment
	maxAttempts := 10
	ctx := context.Background()
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		result, err := s.ec2Client.DescribeSubnets(ctx, &ec2.DescribeSubnetsInput{
			SubnetIds: []string{subnetID},
		})

		// If subnet not found, it's deleted
		if err != nil && strings.Contains(err.Error(), "InvalidSubnetID.NotFound") {
			s.T().Logf("✓ Subnet %s successfully destroyed from LocalStack (attempt %d)", subnetID, attempt)
			return
		}

		s.Require().NoError(err, "Should be able to describe subnets")

		if len(result.Subnets) == 0 {
			s.T().Logf("✓ Subnet %s successfully destroyed from LocalStack (attempt %d)", subnetID, attempt)
			return
		}

		if attempt < maxAttempts {
			s.T().Logf("Subnet %s still exists, attempt %d/%d, retrying...", subnetID, attempt, maxAttempts)
			time.Sleep(2 * time.Second)
		}
	}

	s.T().Errorf("Subnet %s was not destroyed from LocalStack after %d attempts", subnetID, maxAttempts)
}

// verifyInternetGatewayDestroyedByID checks if an internet gateway is deleted by ID
func (s *APIDemoSuite) verifyInternetGatewayDestroyedByID(igwID string) {
	s.T().Logf("Verifying internet gateway is destroyed by ID: %s", igwID)

	// Try multiple times as IGW deletion might take a moment
	maxAttempts := 10
	ctx := context.Background()
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		result, err := s.ec2Client.DescribeInternetGateways(ctx, &ec2.DescribeInternetGatewaysInput{
			InternetGatewayIds: []string{igwID},
		})

		// If IGW not found, it's deleted
		if err != nil && strings.Contains(err.Error(), "InvalidInternetGatewayID.NotFound") {
			s.T().Logf("✓ Internet gateway %s successfully destroyed from LocalStack (attempt %d)", igwID, attempt)
			return
		}

		s.Require().NoError(err, "Should be able to describe internet gateways")

		if len(result.InternetGateways) == 0 {
			s.T().Logf("✓ Internet gateway %s successfully destroyed from LocalStack (attempt %d)", igwID, attempt)
			return
		}

		if attempt < maxAttempts {
			s.T().Logf("Internet gateway %s still exists, attempt %d/%d, retrying...", igwID, attempt, maxAttempts)
			time.Sleep(2 * time.Second)
		}
	}

	s.T().Errorf("Internet gateway %s was not destroyed from LocalStack after %d attempts", igwID, maxAttempts)
}

// extractResourceName extracts the Name tag from a resource of the specified type
func (s *APIDemoSuite) extractResourceName(resources []interface{}, resourceType string) string {
	for _, res := range resources {
		resource := res.(map[string]interface{})
		if resource["type"].(string) == resourceType {
			if state, ok := resource["state"].(map[string]interface{}); ok {
				if tags, ok := state["tags"].(map[string]interface{}); ok {
					if name, ok := tags["Name"].(string); ok {
						return name
					}
				}
			}
		}
	}
	return ""
}

// captureResourceIdentifiers systematically captures all resource identifiers from the API response
// This uses the deployment data returned by GET /api/v1/deployments/{id} after creation completes
// The captured identifiers are stored for post-deletion verification against AWS/LocalStack
func (s *APIDemoSuite) captureResourceIdentifiers(deployment map[string]interface{}) {
	// Clear previous captured resources
	s.capturedResources = make(map[string]string)

	resources, ok := deployment["resources"].([]interface{})
	if !ok {
		s.T().Log("⚠️  Unable to capture resource identifiers - resources not in expected format")
		return
	}

	for _, res := range resources {
		resource, ok := res.(map[string]interface{})
		if !ok {
			continue
		}

		resourceType, _ := resource["type"].(string)
		resourceName, _ := resource["name"].(string)
		status, _ := resource["status"].(string)

		// Only capture identifiers for completed resources
		if status != "completed" {
			continue
		}

		state, ok := resource["state"].(map[string]interface{})
		if !ok {
			continue
		}

		// Build the resource key (e.g., "aws_s3_bucket.demo")
		resourceKey := resourceType + "." + resourceName

		// Extract resource-specific identifiers
		switch resourceType {
		case "aws_s3_bucket":
			if bucketName, ok := state["bucket"].(string); ok && bucketName != "" {
				s.capturedResources[resourceKey] = bucketName
				s.T().Logf("📸 Captured S3 bucket identifier: %s = %s", resourceKey, bucketName)
			}
		case "aws_instance":
			if instanceID, ok := state["id"].(string); ok && instanceID != "" {
				s.capturedResources[resourceKey] = instanceID
				s.T().Logf("📸 Captured EC2 instance identifier: %s = %s", resourceKey, instanceID)
			}
		case "aws_vpc":
			if vpcID, ok := state["id"].(string); ok && vpcID != "" {
				s.capturedResources[resourceKey] = vpcID
				s.T().Logf("📸 Captured VPC identifier: %s = %s", resourceKey, vpcID)
			}
		case "aws_iam_role":
			if roleName, ok := state["name"].(string); ok && roleName != "" {
				s.capturedResources[resourceKey] = roleName
				s.T().Logf("📸 Captured IAM role identifier: %s = %s", resourceKey, roleName)
			}
		case "aws_security_group":
			if sgID, ok := state["id"].(string); ok && sgID != "" {
				s.capturedResources[resourceKey] = sgID
				s.T().Logf("📸 Captured security group identifier: %s = %s", resourceKey, sgID)
			}
		case "aws_subnet":
			if subnetID, ok := state["id"].(string); ok && subnetID != "" {
				s.capturedResources[resourceKey] = subnetID
				s.T().Logf("📸 Captured subnet identifier: %s = %s", resourceKey, subnetID)
			}
		case "aws_internet_gateway":
			if igwID, ok := state["id"].(string); ok && igwID != "" {
				s.capturedResources[resourceKey] = igwID
				s.T().Logf("📸 Captured internet gateway identifier: %s = %s", resourceKey, igwID)
			}
		case "random_string":
			// Random strings don't have AWS identifiers - skip
			continue
		default:
			// For other resources, try generic "id" field
			if id, ok := state["id"].(string); ok && id != "" {
				s.capturedResources[resourceKey] = id
				s.T().Logf("📸 Captured %s identifier: %s = %s", resourceType, resourceKey, id)
			}
		}
	}

	s.T().Logf("✅ Captured %d resource identifiers for post-deletion verification", len(s.capturedResources))
}

// verifyCapturedResourcesDeleted verifies all captured resources are actually deleted from AWS/LocalStack
func (s *APIDemoSuite) verifyCapturedResourcesDeleted() {
	if len(s.capturedResources) == 0 {
		s.T().Log("⚠️  No captured resources to verify deletion")
		return
	}

	s.T().Logf("🔍 Verifying deletion of %d captured resources from AWS/LocalStack...", len(s.capturedResources))

	for resourceKey, identifier := range s.capturedResources {
		parts := strings.Split(resourceKey, ".")
		if len(parts) != 2 {
			continue
		}
		resourceType := parts[0]

		switch resourceType {
		case "aws_s3_bucket":
			s.verifyS3BucketDestroyed(identifier)
		case "aws_instance":
			s.verifyEC2InstanceDestroyedByID(identifier)
		case "aws_vpc":
			s.verifyVPCDestroyedByID(identifier)
		case "aws_iam_role":
			s.verifyIAMRoleDestroyed(identifier)
		case "aws_security_group":
			s.verifySecurityGroupDestroyedByID(identifier)
		case "aws_subnet":
			s.verifySubnetDestroyedByID(identifier)
		case "aws_internet_gateway":
			s.verifyInternetGatewayDestroyedByID(identifier)
		}
	}

	s.T().Log("✅ All captured resources verified as deleted from AWS/LocalStack")
}

// injectLocalStackEndpoint modifies the AWS provider configuration in the demo JSON
// to use the dynamic LocalStack endpoint from testcontainers
func (s *APIDemoSuite) injectLocalStackEndpoint(demoJSON map[string]interface{}) {
	// Get the terraform_json
	terraformJSON, ok := demoJSON["terraform_json"].(map[string]interface{})
	if !ok {
		return
	}

	// Get or create the provider section
	providerSection, ok := terraformJSON["provider"].(map[string]interface{})
	if !ok {
		providerSection = make(map[string]interface{})
		terraformJSON["provider"] = providerSection
	}

	// Get or create the AWS provider configuration
	awsProvider, ok := providerSection["aws"].(map[string]interface{})
	if !ok {
		awsProvider = make(map[string]interface{})
		providerSection["aws"] = awsProvider
	}

	// Delete the incorrectly formatted endpoints array if it exists
	delete(awsProvider, "endpoints")
	
	// Set the LocalStack endpoint configuration properly
	// The AWS provider expects endpoints as a single object, not an array
	awsProvider["endpoints"] = map[string]interface{}{
		"s3":             s.localstackEndpoint,
		"ec2":            s.localstackEndpoint,
		"iam":            s.localstackEndpoint,
		"sts":            s.localstackEndpoint,
		"dynamodb":       s.localstackEndpoint,
		"cloudformation": s.localstackEndpoint,
		"lambda":         s.localstackEndpoint,
	}

	// Ensure other LocalStack-required settings are present
	awsProvider["s3_use_path_style"] = true
	awsProvider["skip_credentials_validation"] = true
	awsProvider["skip_metadata_api_check"] = true
	awsProvider["skip_requesting_account_id"] = true
	
	// Ensure test credentials and region
	awsProvider["access_key"] = "test"
	awsProvider["secret_key"] = "test"
	awsProvider["region"] = "us-east-1"
}
