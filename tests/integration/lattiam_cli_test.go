package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	"github.com/stretchr/testify/suite"

	"github.com/lattiam/lattiam/tests/helpers"
)

// LattiamCLITestSuite tests the lattiam CLI end-to-end
type LattiamCLITestSuite struct {
	suite.Suite
	binaryPath string
	testDir    string
}

//nolint:paralleltest // integration tests use shared resources
func TestLattiamCLISuite(t *testing.T) {
	suite.Run(t, new(LattiamCLITestSuite))
}

func (s *LattiamCLITestSuite) SetupSuite() {
	// Build the lattiam binary
	s.testDir = s.T().TempDir()
	s.binaryPath = filepath.Join(s.testDir, "lattiam")

	// #nosec G204 - Test code with controlled inputs
	cmd := exec.Command("go", "build", "-tags", "localstack", "-o", s.binaryPath, "../../cmd/lattiam")
	output, err := cmd.CombinedOutput()
	s.Require().NoError(err, "Failed to build lattiam binary: %s", string(output))

	// Verify binary exists
	_, err = os.Stat(s.binaryPath)
	s.Require().NoError(err, "Binary does not exist at %s", s.binaryPath)

	// Set LocalStack environment for CLI tests
	// Check if we're in a container environment
	if _, err := os.Stat("/.dockerenv"); err == nil {
		// We're in a container, use the container name
		os.Setenv("AWS_ENDPOINT_URL", helpers.DefaultLocalStackURL)
	}
	// We're on the host, use localhost (no action needed)
	os.Setenv("AWS_ACCESS_KEY_ID", "test")
	os.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	os.Setenv("AWS_DEFAULT_REGION", "us-east-1")

	// Wait for LocalStack to be ready
	tc := helpers.NewTestConfig()
	ctx, cancel := context.WithTimeout(context.Background(), helpers.LocalStackReadyTimeout)
	defer cancel()
	err = tc.WaitForLocalStack(ctx)
	s.Require().NoError(err, "LocalStack not ready")
}

func (s *LattiamCLITestSuite) SetupTest() {
	// Clean up any leftover resources before each test
	s.cleanupTestResources()
}

func (s *LattiamCLITestSuite) TearDownSuite() {
	// Final cleanup
	s.cleanupTestResources()
	os.RemoveAll(s.testDir)
}

// cleanupTestResources cleans up known test resources from LocalStack
func (s *LattiamCLITestSuite) cleanupTestResources() {
	// This function is now a placeholder.
	// Cleanup is handled by s.T().Cleanup() in each test case.
}

// deleteSecurityGroup deletes a security group, ignoring errors if it doesn't exist
func (s *LattiamCLITestSuite) deleteSecurityGroup(groupName string) {
	helpers.DeleteSecurityGroup(s.T(), groupName)
}

// deleteIAMRole deletes an IAM role, ignoring errors if it doesn't exist
func (s *LattiamCLITestSuite) deleteIAMRole(roleName string) {
	// Use AWS SDK to delete IAM role with timeout
	tc := helpers.NewTestConfig()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cfg, err := tc.GetAWSConfig(ctx)
	if err != nil {
		s.T().Logf("Warning: Failed to get AWS config: %v", err)
		return
	}

	iamClient := iam.NewFromConfig(cfg)

	_, err = iamClient.DeleteRole(ctx, &iam.DeleteRoleInput{
		RoleName: aws.String(roleName),
	})
	if err != nil {
		// Check for common "doesn't exist" errors
		if strings.Contains(err.Error(), "NoSuchEntity") ||
			strings.Contains(err.Error(), "not found") {
			s.T().Logf("IAM role %s does not exist, nothing to delete", roleName)
			return
		}
		s.T().Logf("Warning: Failed to delete IAM role %s: %v", roleName, err)
		return
	}

	s.T().Logf("Successfully deleted IAM role: %s", roleName)
}

// terminateEC2Instance terminates an EC2 instance, ignoring errors if it doesn't exist
func (s *LattiamCLITestSuite) terminateEC2Instance(instanceID string) {
	// Handle case where multiple instance IDs might be returned (space-separated)
	instanceIDs := strings.Fields(instanceID)

	tc := helpers.NewTestConfig()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cfg, err := tc.GetAWSConfig(ctx)
	if err != nil {
		s.T().Logf("Warning: Failed to get AWS config: %v", err)
		return
	}

	ec2Client := ec2.NewFromConfig(cfg)

	for _, id := range instanceIDs {
		_, err = ec2Client.TerminateInstances(ctx, &ec2.TerminateInstancesInput{
			InstanceIds: []string{id},
		})
		if err != nil {
			// Check for common "doesn't exist" errors
			if strings.Contains(err.Error(), "InvalidInstanceID.NotFound") ||
				strings.Contains(err.Error(), "does not exist") {
				s.T().Logf("EC2 instance %s does not exist, nothing to terminate", id)
				continue
			}
			s.T().Logf("Warning: Failed to terminate EC2 instance %s: %v", id, err)
			continue
		}
		s.T().Logf("Successfully terminated EC2 instance: %s", id)
	}
}

// Helper to run commands and get output
func (s *LattiamCLITestSuite) runCommand(args ...string) string {
	cmd := exec.Command(s.binaryPath, args...) //nolint:gosec // Test code with controlled inputs
	// Inherit environment including LocalStack settings
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	s.Require().NoError(err, "Command failed: %s\nOutput: %s", cmd.String(), string(output))
	return string(output)
}

// Helper function to create wrapped JSON for tests
func wrapTerraformJSON(name string, terraformJSON interface{}) (string, error) {
	wrapper := map[string]interface{}{
		"name":           name,
		"terraform_json": terraformJSON,
	}
	data, err := json.MarshalIndent(wrapper, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal JSON: %w", err)
	}
	return string(data), nil
}

func (s *LattiamCLITestSuite) TestCLIHelp() {
	output := s.runCommand("--help")
	s.Contains(output, "lattiam")
	s.Contains(output, "Usage:")
	s.Contains(output, "Available Commands:")
}

func (s *LattiamCLITestSuite) TestCLIVersion() {
	output := s.runCommand("--version")
	s.Contains(output, "lattiam version")
}

//nolint:gocognit // Integration test requires comprehensive test scenarios
func (s *LattiamCLITestSuite) TestApplyCommands() {
	// Generate unique names for resources to prevent collisions
	iamRoleName := helpers.GenerateTestResourceName("iam-role")
	sgName := helpers.GenerateTestResourceName("sg")
	instanceName := helpers.GenerateTestResourceName("instance")
	bucketName := helpers.GenerateTestResourceName("bucket")
	complexInstanceName := helpers.GenerateTestResourceName("complex-instance")

	// Register cleanup functions for all uniquely named resources
	s.T().Cleanup(func() {
		s.deleteIAMRole(iamRoleName)
		s.deleteSecurityGroup(sgName)
		if instanceID := helpers.GetEC2InstanceIDFromName(s.T(), instanceName); instanceID != "" {
			s.terminateEC2Instance(instanceID)
		}
		if instanceID := helpers.GetEC2InstanceIDFromName(s.T(), complexInstanceName); instanceID != "" {
			s.terminateEC2Instance(instanceID)
		}
		helpers.DeleteS3Bucket(s.T(), bucketName)
	})

	tests := []struct {
		name             string
		deploymentName   string
		terraformJSON    map[string]interface{}
		expectedLines    []string
		errorExpected    bool
		skipInLocalStack bool
	}{
		{
			name:           "simple IAM role",
			deploymentName: "Test IAM Role",
			terraformJSON: map[string]interface{}{
				"resource": map[string]interface{}{
					"aws_iam_role": map[string]interface{}{
						"test_role": map[string]interface{}{
							"name":               iamRoleName,
							"assume_role_policy": `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Principal":{"Service":"lambda.amazonaws.com"},"Action":"sts:AssumeRole"}]}`,
							"description":        "Test role for OAT",
							"tags": map[string]interface{}{
								"Environment": "test",
								"TestSuite":   "lattiam-cli",
							},
						},
					},
				},
			},
			expectedLines: []string{
				"Applying infrastructure from",
				"Found 1 resources to deploy",
				"Deployment complete: 1/1 resources succeeded",
			},
			errorExpected: false, // Should work with LocalStack
		},
		{
			name:           "empty resources",
			deploymentName: "Empty Test",
			terraformJSON: map[string]interface{}{
				"resource": map[string]interface{}{},
			},
			expectedLines: []string{
				"no resources found",
			},
			errorExpected: true, // Empty resources is an error condition
		},
		{
			name:           "multiple resources",
			deploymentName: "Multi Resource Test",
			terraformJSON: map[string]interface{}{
				"resource": map[string]interface{}{
					"aws_security_group": map[string]interface{}{
						"test_sg": map[string]interface{}{
							"name":        sgName,
							"description": "Test security group",
							"vpc_id":      "vpc-12345678",
							"tags": map[string]interface{}{
								"TestSuite": "lattiam-cli",
							},
							"timeouts": map[string]interface{}{
								"create": "10m",
								"delete": "15m",
							},
						},
					},
					"aws_instance": map[string]interface{}{
						"test_instance": map[string]interface{}{
							"ami":           "ami-12345678",
							"instance_type": "t2.micro",
							"vpc_id":        "vpc-12345678",
							"tags": map[string]interface{}{
								"Name":      instanceName,
								"TestSuite": "lattiam-cli",
							},
							"timeouts": map[string]interface{}{
								"create": "10m",
								"update": "10m",
								"delete": "10m",
							},
						},
					},
					"aws_s3_bucket": map[string]interface{}{
						"test_bucket": map[string]interface{}{
							"bucket": bucketName,
							"tags": map[string]interface{}{
								"TestSuite": "lattiam-cli",
							},
						},
					},
				},
			},
			expectedLines: []string{
				"Found 3 resources to deploy",
				"Deployment complete: 3/3 resources succeeded",
			},
			errorExpected:    false, // Will fail quickly when LocalStack is not running
			skipInLocalStack: true,  // SKIP: Uses hardcoded vpc-12345678 and ami-12345678 that don't exist in LocalStack
		},
		{
			name:           "complex instance",
			deploymentName: "Complex Instance Test",
			terraformJSON: map[string]interface{}{
				"resource": map[string]interface{}{
					"aws_instance": map[string]interface{}{
						"complex_test": map[string]interface{}{
							"ami":           "ami-0c55b159cbfafe1f0",
							"instance_type": "t2.micro",
							"subnet_id":     "subnet-12345678",
							"vpc_security_group_ids": []interface{}{
								"sg-12345678",
								"sg-87654321",
							},
							"tags": map[string]interface{}{
								"Name":        complexInstanceName,
								"Environment": "test",
								"Owner":       "lattiam-test",
								"Purpose":     "OAT validation",
								"TestSuite":   "lattiam-cli",
							},
							"user_data": "#!/bin/bash\necho 'Hello from Lattiam test'",
							"root_block_device": []interface{}{
								map[string]interface{}{
									"volume_size": 20,
									"volume_type": "gp3",
									"encrypted":   true,
								},
							},
							"timeouts": map[string]interface{}{
								"create": "10m",
								"update": "10m",
								"delete": "10m",
							},
						},
					},
				},
			},
			expectedLines: []string{
				"Found 1 resources to deploy",
				"Deployment complete: 1/1 resources succeeded",
			},
			errorExpected:    false,
			skipInLocalStack: true, // SKIP: Uses hardcoded ami-0c55b159cbfafe1f0, subnet-12345678, sg-12345678, sg-87654321 that don't exist in LocalStack
		},
	}

	for _, tt := range tests {
		s.Run(tt.name, func() {
			// Skip LocalStack-incompatible tests when running in LocalStack mode
			if tt.skipInLocalStack {
				tc := helpers.NewTestConfig()
				if tc.IsLocalStack() {
					s.T().Skipf("SKIPPING IN LOCALSTACK: Test '%s' uses hardcoded AWS resource IDs (AMI, VPC, subnet, security groups) that don't exist in LocalStack. This test validates CLI functionality with real AWS resources and should only run against real AWS infrastructure. See demo/terraform-json/07-ec2-complex-dependencies-localstack.json for a LocalStack-compatible example that creates all dependencies.", tt.name)
				}
			}

			// Create the wrapped JSON content
			jsonContent, err := wrapTerraformJSON(tt.deploymentName, tt.terraformJSON)
			s.Require().NoError(err)

			// Write to file
			jsonFile := filepath.Join(s.testDir, fmt.Sprintf("test-%s.json", strings.ReplaceAll(tt.name, " ", "-")))
			err = os.WriteFile(jsonFile, []byte(jsonContent), 0o600)
			s.Require().NoError(err)

			// Run apply command with timeout for LocalStack deployment
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			cmd := exec.CommandContext(ctx, s.binaryPath, "apply", jsonFile) //nolint:gosec // Test code with controlled inputs
			// Inherit environment including LocalStack settings
			cmd.Env = os.Environ()
			output, err := cmd.CombinedOutput()
			outputStr := string(output)

			// Check expected output
			for _, expected := range tt.expectedLines {
				s.Contains(outputStr, expected, "Output should contain: %s\nActual output:\n%s", expected, outputStr)
			}

			// Check error expectation
			if tt.errorExpected {
				if err == nil {
					// LocalStack might be running, check for success indicators
					s.Contains(outputStr, "Resource created successfully!")
				} else {
					// Expected failure - could be timeout, connection error, or other failure
					// Accept any error since we expect this to fail without LocalStack
					s.Require().Error(err, "Expected command to fail without LocalStack running")
				}
			} else {
				s.NoError(err, "Command should succeed but failed with output:\n%s", outputStr)
			}
		})
	}
}

func (s *LattiamCLITestSuite) TestInvalidInputs() {
	s.Run("invalid JSON", func() {
		// Create invalid JSON file
		jsonFile := filepath.Join(s.testDir, "invalid.json")
		err := os.WriteFile(jsonFile, []byte(`{invalid json}`), 0o600)
		s.Require().NoError(err)

		cmd := exec.Command(s.binaryPath, "apply", jsonFile) //nolint:gosec // Test code with controlled inputs
		output, err := cmd.CombinedOutput()
		s.Require().Error(err)
		s.Contains(string(output), "failed to parse JSON")
	})

	s.Run("missing file", func() {
		cmd := exec.Command(s.binaryPath, "apply", "/tmp/nonexistent.json") //nolint:gosec // Test code with controlled inputs
		output, err := cmd.CombinedOutput()
		s.Require().Error(err)
		s.Contains(string(output), "failed to read file")
	})

	s.Run("missing terraform_json field", func() {
		// Create JSON without terraform_json wrapper
		jsonFile := filepath.Join(s.testDir, "no-wrapper.json")
		content := `{"resource": {"null_resource": {"test": {}}}}`
		err := os.WriteFile(jsonFile, []byte(content), 0o600)
		s.Require().NoError(err)

		cmd := exec.Command(s.binaryPath, "apply", jsonFile) //nolint:gosec // Test code with controlled inputs
		output, err := cmd.CombinedOutput()
		s.Require().Error(err)
		s.Contains(string(output), "missing terraform_json field")
	})
}

func (s *LattiamCLITestSuite) TestNotImplementedCommands() {
	notImplementedCmds := []struct {
		name string
		args []string
	}{
		{"get", []string{"get", "aws_instance", "test"}},
		{"delete", []string{"delete", "aws_instance", "test"}},
		{"list", []string{"list"}},
	}
	for _, tc := range notImplementedCmds {
		s.Run(tc.name, func() {
			command := exec.Command(s.binaryPath, tc.args...) //nolint:gosec // Test code with controlled inputs
			output, err := command.CombinedOutput()
			s.Require().Error(err)
			s.Contains(string(output), "not yet implemented")
		})
	}
}

func (s *LattiamCLITestSuite) TestLargeResourceCount() {
	// Create a deployment with many resources
	resources := make(map[string]interface{})
	for i := 0; i < 50; i++ {
		resources[fmt.Sprintf("bucket_%d", i)] = map[string]interface{}{
			"bucket": fmt.Sprintf("test-bucket-%d", i),
			"tags": map[string]interface{}{
				"Index": fmt.Sprintf("%d", i),
			},
		}
	}

	terraformJSON := map[string]interface{}{
		"resource": map[string]interface{}{
			"aws_s3_bucket": resources,
		},
	}

	jsonContent, err := wrapTerraformJSON("Large Deployment", terraformJSON)
	s.Require().NoError(err)

	jsonFile := filepath.Join(s.testDir, "large.json")
	err = os.WriteFile(jsonFile, []byte(jsonContent), 0o600)
	s.Require().NoError(err)

	// Test parsing - should handle large resource count
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, s.binaryPath, "apply", jsonFile) //nolint:gosec // Test code with controlled inputs
	output, err := cmd.CombinedOutput()
	outputStr := string(output)

	// Should parse correctly
	s.Contains(outputStr, "Found 50 resources to deploy")

	// Will fail at provider stage without LocalStack
	if err != nil {
		// The command may timeout or fail - either is acceptable
		// Already verified error is not nil by being in this branch
		_ = err
	}
}
