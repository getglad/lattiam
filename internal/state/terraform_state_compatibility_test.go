package state_test

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lattiam/lattiam/internal/state"
)

// TestTerraformStateCompatibility verifies that Lattiam-generated state files
// are compatible with Terraform CLI
//
//nolint:gocognit,funlen,gocyclo // Complex compatibility test with multiple formats
func TestTerraformStateCompatibility(t *testing.T) {
	t.Parallel()
	// Check if terraform is available
	if _, err := exec.LookPath("terraform"); err != nil {
		t.Skip("Terraform CLI not found, skipping compatibility test")
	}

	// Create temp directory for test
	tempDir, err := os.MkdirTemp("", "lattiam-tfstate-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer func() { _ = os.RemoveAll(tempDir) }() // Ignore error - test cleanup

	// Step 1: Create a simple Terraform configuration
	tfConfig := map[string]interface{}{
		"terraform": map[string]interface{}{
			"required_providers": map[string]interface{}{
				"random": map[string]interface{}{
					"source":  "hashicorp/random",
					"version": "3.5.1",
				},
			},
		},
		"provider": map[string]interface{}{
			"random": map[string]interface{}{},
		},
		"resource": map[string]interface{}{
			"random_id": map[string]interface{}{
				"test": map[string]interface{}{
					"byte_length": 4,
				},
			},
		},
		"output": map[string]interface{}{
			"test_id": map[string]interface{}{
				"value": "${random_id.test.hex}",
			},
		},
	}

	configData, err := json.MarshalIndent(tfConfig, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}

	configPath := filepath.Join(tempDir, "main.tf.json")
	if err := os.WriteFile(configPath, configData, 0o600); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	// Step 2: Initialize and apply with Terraform to get a reference state
	t.Log("Initializing Terraform...")
	initCmd := exec.Command("terraform", "init", "-input=false")
	initCmd.Dir = tempDir
	if output, err := initCmd.CombinedOutput(); err != nil {
		t.Fatalf("terraform init failed: %v\nOutput: %s", err, output)
	}

	t.Log("Applying Terraform configuration...")
	applyCmd := exec.Command("terraform", "apply", "-auto-approve", "-input=false")
	applyCmd.Dir = tempDir
	if output, err := applyCmd.CombinedOutput(); err != nil {
		t.Fatalf("terraform apply failed: %v\nOutput: %s", err, output)
	}

	// Read the Terraform-generated state
	tfStatePath := filepath.Join(tempDir, "terraform.tfstate")
	tfStateData, err := os.ReadFile(tfStatePath) // #nosec G304 - Test file with controlled path
	if err != nil {
		t.Fatalf("Failed to read Terraform state: %v", err)
	}

	var tfState map[string]interface{}
	if err := json.Unmarshal(tfStateData, &tfState); err != nil {
		t.Fatalf("Failed to parse Terraform state: %v", err)
	}

	// Extract resource attributes from Terraform state
	var resourceID string
	if resources, ok := tfState["resources"].([]interface{}); ok && len(resources) > 0 {
		resource := resources[0].(map[string]interface{})
		if instances, ok := resource["instances"].([]interface{}); ok && len(instances) > 0 {
			instance := instances[0].(map[string]interface{})
			if attrs, ok := instance["attributes"].(map[string]interface{}); ok {
				resourceID = attrs["hex"].(string)
			}
		}
	}

	if resourceID == "" {
		t.Fatal("Failed to extract resource ID from Terraform state")
	}

	// Step 3: Create a Lattiam state with the same resource
	t.Log("Creating Lattiam state...")
	lattiamState := &state.TerraformState{
		Version:          4,
		TerraformVersion: "1.5.0",
		Serial:           2,                           // Higher than Terraform's to avoid conflicts
		Lineage:          tfState["lineage"].(string), // Use same lineage
		Outputs: map[string]interface{}{
			"test_id": map[string]interface{}{
				"value": resourceID,
				"type":  "string",
			},
		},
		Resources: []state.TerraformStateResource{
			{
				Mode:     "managed",
				Type:     "random_id",
				Name:     "test",
				Provider: `provider["registry.terraform.io/hashicorp/random"]`,
				Instances: []state.TerraformStateResourceInstance{
					{
						SchemaVersion: 0,
						Attributes: map[string]interface{}{
							"b64_std":     tfState["resources"].([]interface{})[0].(map[string]interface{})["instances"].([]interface{})[0].(map[string]interface{})["attributes"].(map[string]interface{})["b64_std"],
							"b64_url":     tfState["resources"].([]interface{})[0].(map[string]interface{})["instances"].([]interface{})[0].(map[string]interface{})["attributes"].(map[string]interface{})["b64_url"],
							"byte_length": float64(4),
							"dec":         tfState["resources"].([]interface{})[0].(map[string]interface{})["instances"].([]interface{})[0].(map[string]interface{})["attributes"].(map[string]interface{})["dec"],
							"hex":         resourceID,
							"id":          tfState["resources"].([]interface{})[0].(map[string]interface{})["instances"].([]interface{})[0].(map[string]interface{})["attributes"].(map[string]interface{})["id"],
							"keepers":     nil,
							"prefix":      nil,
						},
						SensitiveAttributes:   []string{},
						IdentitySchemaVersion: 0,
					},
				},
			},
		},
		CheckResults: nil,
	}

	// Write Lattiam state directly (bypass the store for this test)
	lattiamStateData, err := json.MarshalIndent(lattiamState, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal Lattiam state: %v", err)
	}

	// Overwrite the terraform.tfstate with Lattiam's version
	if err := os.WriteFile(tfStatePath, lattiamStateData, 0o600); err != nil {
		t.Fatalf("Failed to write Lattiam state: %v", err)
	}

	// Step 4: Verify Terraform can work with the Lattiam state
	t.Log("Testing Terraform plan with Lattiam state...")
	planCmd := exec.Command("terraform", "plan", "-detailed-exitcode")
	planCmd.Dir = tempDir
	output, err := planCmd.CombinedOutput()
	// Exit code 0 means no changes, which is what we want
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if exitErr.ExitCode() == 2 {
				t.Errorf("Terraform detected changes with Lattiam state (should be none):\n%s", output)
			} else {
				t.Errorf("Terraform plan failed with exit code %d:\n%s", exitErr.ExitCode(), output)
			}
		} else {
			t.Errorf("Terraform plan failed: %v\n%s", err, output)
		}
	}

	// Verify plan shows no changes
	if strings.Contains(string(output), "Plan:") && !strings.Contains(string(output), "No changes") {
		t.Error("Terraform plan shows changes when it shouldn't")
	}

	// Test state list
	t.Log("Testing terraform state list...")
	listCmd := exec.Command("terraform", "state", "list")
	listCmd.Dir = tempDir
	listOutput, err := listCmd.CombinedOutput()
	if err != nil {
		t.Errorf("terraform state list failed: %v\n%s", err, listOutput)
	}
	if !strings.Contains(string(listOutput), "random_id.test") {
		t.Errorf("Resource not found in state list: %s", listOutput)
	}

	// Test output
	t.Log("Testing terraform output...")
	outputCmd := exec.Command("terraform", "output", "-raw", "test_id")
	outputCmd.Dir = tempDir
	outputValue, err := outputCmd.CombinedOutput()
	if err != nil {
		t.Errorf("terraform output failed: %v\n%s", err, outputValue)
	}
	if strings.TrimSpace(string(outputValue)) != resourceID {
		t.Errorf("Output mismatch: expected %s, got %s", resourceID, outputValue)
	}

	t.Log("✓ All compatibility tests passed")
}

// TestTerraformStateStructure verifies the structure matches Terraform's expectations
//
//nolint:funlen // Comprehensive Terraform state structure compatibility test
func TestTerraformStateStructure(t *testing.T) {
	t.Parallel()
	// Create a test state
	testState := &state.TerraformState{
		Version:          4,
		TerraformVersion: "1.5.0",
		Serial:           1,
		Lineage:          "test-lineage",
		Outputs:          map[string]interface{}{},
		Resources:        []state.TerraformStateResource{},
		CheckResults:     nil,
	}

	// Marshal to JSON
	data, err := json.Marshal(testState)
	if err != nil {
		t.Fatalf("Failed to marshal state: %v", err)
	}

	// Unmarshal to map to check structure
	var stateMap map[string]interface{}
	if err := json.Unmarshal(data, &stateMap); err != nil {
		t.Fatalf("Failed to unmarshal state: %v", err)
	}

	// Check required fields exist
	requiredFields := []string{
		"version",
		"terraform_version",
		"serial",
		"lineage",
		"outputs",
		"resources",
		"check_results",
	}

	for _, field := range requiredFields {
		if _, exists := stateMap[field]; !exists {
			t.Errorf("Required field '%s' is missing from state structure", field)
		}
	}

	// Test with a resource
	testState.Resources = append(testState.Resources, state.TerraformStateResource{
		Mode:     "managed",
		Type:     "test_resource",
		Name:     "test",
		Provider: "provider[\"test\"]",
		Instances: []state.TerraformStateResourceInstance{
			{
				SchemaVersion:         0,
				Attributes:            map[string]interface{}{"id": "test-id"},
				SensitiveAttributes:   []string{},
				IdentitySchemaVersion: 0,
			},
		},
	})

	// Marshal again with resource
	data, err = json.Marshal(testState)
	if err != nil {
		t.Fatalf("Failed to marshal state with resource: %v", err)
	}

	// Check it's valid JSON
	var checkState state.TerraformState
	if err := json.Unmarshal(data, &checkState); err != nil {
		t.Fatalf("Failed to unmarshal state with resource: %v", err)
	}

	// Verify instance fields
	if len(checkState.Resources) != 1 {
		t.Fatal("Expected 1 resource")
	}
	if len(checkState.Resources[0].Instances) != 1 {
		t.Fatal("Expected 1 instance")
	}

	instance := checkState.Resources[0].Instances[0]
	if instance.SensitiveAttributes == nil {
		t.Error("SensitiveAttributes should not be nil")
	}
	if instance.IdentitySchemaVersion != 0 {
		t.Error("IdentitySchemaVersion not preserved")
	}
}
