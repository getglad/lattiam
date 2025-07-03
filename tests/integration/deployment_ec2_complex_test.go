//go:build integration
// +build integration

package integration

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"

	"github.com/lattiam/lattiam/tests/helpers"
)

type DeploymentEC2ComplexTestSuite struct {
	helpers.BaseTestSuite
}

func TestDeploymentEC2ComplexTestSuite(t *testing.T) {
	suite.Run(t, new(DeploymentEC2ComplexTestSuite))
}

// TestDeploymentEC2Complex tests the EC2 complex dependencies demo
// This validates the fix for IAM policy attachment interpolation issues
//
//nolint:paralleltest // integration tests use shared resources
func (s *DeploymentEC2ComplexTestSuite) TestDeploymentEC2Complex() {
	s.T().Run("ec2 complex dependencies deployment", func(t *testing.T) {
		// Load the EC2 complex dependencies demo
		var deploymentReq map[string]interface{}
		helpers.LoadFixtureJSON(s.T(), "../../demo/terraform-json/07-ec2-complex-dependencies-localstack.json", &deploymentReq)

		// Create deployment
		deployment := s.APIClient.CreateDeployment(deploymentReq)

		deploymentID, ok := deployment["id"].(string)
		s.Require().True(ok, "deployment ID should be a string")
		s.Require().NotEmpty(deploymentID)

		// Wait for deployment to complete (EC2 takes longer)
		s.APIClient.WaitForDeploymentStatus(deploymentID, "completed", 180*time.Second)

		// Get deployment details to verify all resources were created
		result := s.APIClient.GetDeployment(deploymentID)

		// Verify deployment summary
		summary, ok := result["summary"].(map[string]interface{})
		s.Require().True(ok, "deployment should have summary")

		totalResources, ok := summary["total_resources"].(float64)
		s.Require().True(ok, "summary should have total_resources")
		assert.Equal(s.T(), float64(16), totalResources, "should have 16 resources")

		successfulResources, ok := summary["successful_resources"].(float64)
		s.Require().True(ok, "summary should have successful_resources")
		assert.Equal(s.T(), float64(16), successfulResources, "all 16 resources should be successful")

		// Verify specific resources exist
		resources := result["resources"].([]interface{})
		s.Require().Len(resources, 16, "should have exactly 16 resources")

		// Track which resource types we found
		resourceTypes := make(map[string]bool)
		var ec2Instance map[string]interface{}
		var iamRole map[string]interface{}
		var securityGroup map[string]interface{}

		for _, res := range resources {
			resource := res.(map[string]interface{})
			resourceType := resource["type"].(string)
			resourceTypes[resourceType] = true

			// Capture key resources for detailed validation
			switch resourceType {
			case "aws_instance":
				ec2Instance = resource
			case "aws_iam_role":
				iamRole = resource
			case "aws_security_group":
				securityGroup = resource
			}
		}

		// Verify all expected resource types were created
		expectedTypes := []string{
			"random_string",
			"aws_vpc",
			"aws_subnet",
			"aws_internet_gateway",
			"aws_security_group",
			"aws_iam_role",
			"aws_iam_role_policy", // This is the inline policy fix
			"aws_iam_instance_profile",
			"aws_instance",
		}

		for _, expectedType := range expectedTypes {
			assert.True(s.T(), resourceTypes[expectedType],
				"should have created %s resource", expectedType)
		}

		// Detailed validation of EC2 instance
		s.Require().NotNil(ec2Instance, "should have created EC2 instance")

		instanceState, ok := ec2Instance["state"].(map[string]interface{})
		s.Require().True(ok, "EC2 instance should have state")

		// Verify EC2 instance has required properties
		// Check for instance ID (could be "id" or "instance_id" depending on provider version)
		instanceID := instanceState["id"]
		if instanceID == nil {
			instanceID = instanceState["instance_id"]
		}
		assert.NotEmpty(s.T(), instanceID, "should have instance ID")
		assert.Equal(s.T(), "t3.micro", instanceState["instance_type"], "should have correct instance type")

		// Verify security group is attached
		vpcSecurityGroupIDs, ok := instanceState["vpc_security_group_ids"].([]interface{})
		s.Require().True(ok, "should have vpc_security_group_ids")
		assert.Len(s.T(), vpcSecurityGroupIDs, 1, "should have exactly one security group")

		// Verify IAM instance profile is attached
		iamInstanceProfile, ok := instanceState["iam_instance_profile"].(string)
		s.Require().True(ok, "should have IAM instance profile")
		assert.Contains(s.T(), iamInstanceProfile, "lattiam-demo-ec2-profile", "should have correct instance profile name pattern")

		// Verify IAM role has inline policy (the fix for the interpolation issue)
		s.Require().NotNil(iamRole, "should have created IAM role")

		roleState, ok := iamRole["state"].(map[string]interface{})
		s.Require().True(ok, "IAM role should have state")

		roleName, ok := roleState["name"].(string)
		s.Require().True(ok, "IAM role should have name")
		assert.Contains(s.T(), roleName, "lattiam-demo-ec2-role", "should have correct role name pattern")

		// Verify security group has correct configuration
		s.Require().NotNil(securityGroup, "should have created security group")

		sgState, ok := securityGroup["state"].(map[string]interface{})
		s.Require().True(ok, "security group should have state")

		sgName, ok := sgState["name"].(string)
		s.Require().True(ok, "security group should have name")
		assert.Contains(s.T(), sgName, "lattiam-demo-web-sg", "should have correct security group name pattern")

		// Verify security group rules exist as separate resources
		// Count security group rules (should have HTTP, HTTPS, SSH ingress + egress)
		securityGroupRules := 0
		for _, resource := range resources {
			resourceMap := resource.(map[string]interface{})
			if resourceMap["type"] == "aws_security_group_rule" {
				securityGroupRules++
			}
		}
		assert.GreaterOrEqual(s.T(), securityGroupRules, 4, "should have at least 4 security group rules (HTTP, HTTPS, SSH, egress)")

		// Clean up
		s.APIClient.DeleteDeployment(deploymentID)
	})

	s.T().Run("dependency resolution validation", func(t *testing.T) {
		// This test specifically validates that the dependency resolution
		// correctly handles the complex EC2 dependency chain without
		// the IAM policy attachment interpolation issues

		// Load and parse the demo file to verify structure
		var deployment map[string]interface{}
		helpers.LoadFixtureJSON(s.T(), "../../demo/terraform-json/07-ec2-complex-dependencies-localstack.json", &deployment)

		terraformJSON, ok := deployment["terraform_json"].(map[string]interface{})
		s.Require().True(ok, "should have terraform_json")

		resources, ok := terraformJSON["resource"].(map[string]interface{})
		s.Require().True(ok, "should have resources")

		// Verify we're using inline policies instead of policy attachments
		_, hasInlinePolicy := resources["aws_iam_role_policy"]
		assert.True(s.T(), hasInlinePolicy, "should use aws_iam_role_policy (inline) for better dependency resolution")

		_, hasPolicyAttachment := resources["aws_iam_role_policy_attachment"]
		assert.False(s.T(), hasPolicyAttachment, "should NOT use aws_iam_role_policy_attachment (prevents interpolation issues)")

		// Verify EC2 instance references other resources correctly
		ec2Resource, ok := resources["aws_instance"].(map[string]interface{})
		s.Require().True(ok, "should have aws_instance")

		webServer, ok := ec2Resource["web_server"].(map[string]interface{})
		s.Require().True(ok, "should have web_server instance")

		// Verify dependency references
		vpcSecurityGroupIDs, ok := webServer["vpc_security_group_ids"].([]interface{})
		s.Require().True(ok, "should have vpc_security_group_ids")
		s.Require().Len(vpcSecurityGroupIDs, 1, "should reference one security group")
		assert.Contains(s.T(), vpcSecurityGroupIDs[0].(string), "${aws_security_group.web_server.id}",
			"should reference security group via interpolation")

		iamInstanceProfile, ok := webServer["iam_instance_profile"].(string)
		s.Require().True(ok, "should have iam_instance_profile")
		assert.Contains(s.T(), iamInstanceProfile, "${aws_iam_instance_profile.ec2_profile.name}",
			"should reference instance profile via interpolation")

		// Verify random string configuration for IAM compatibility
		randomString, ok := resources["random_string"].(map[string]interface{})
		s.Require().True(ok, "should have random_string")

		stackSuffix, ok := randomString["stack_suffix"].(map[string]interface{})
		s.Require().True(ok, "should have stack_suffix")

		// Verify IAM-safe random string configuration
		assert.Equal(s.T(), false, stackSuffix["special"], "should not use special characters for IAM compatibility")
		assert.Equal(s.T(), false, stackSuffix["upper"], "should not use uppercase for consistency")
		assert.Equal(s.T(), true, stackSuffix["numeric"], "should allow numeric characters")
		assert.Equal(s.T(), true, stackSuffix["lower"], "should allow lowercase characters")
	})
}
