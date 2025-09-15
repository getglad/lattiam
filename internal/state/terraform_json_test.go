package state_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/lattiam/lattiam/internal/state"
)

// TestTerraformStateJSONFields verifies that our JSON serialization
// produces exactly the fields that Terraform expects
//
//nolint:funlen,gocognit,gocyclo // Comprehensive JSON field validation test
func TestTerraformStateJSONFields(t *testing.T) {
	t.Parallel()
	// Create a state with all fields populated
	testState := &state.TerraformState{
		Version:          4,
		TerraformVersion: "1.5.0",
		Serial:           1,
		Lineage:          "test-lineage",
		Outputs:          map[string]interface{}{},
		Resources: []state.TerraformStateResource{
			{
				Mode:     "managed",
				Type:     "test_resource",
				Name:     "example",
				Provider: `provider["registry.terraform.io/test/test"]`,
				Instances: []state.TerraformStateResourceInstance{
					{
						SchemaVersion:         0,
						Attributes:            map[string]interface{}{"id": "test"},
						SensitiveAttributes:   []string{},
						IdentitySchemaVersion: 0,
						Private:               "",
						Dependencies:          []string{},
						CreateBeforeDestroy:   false,
					},
				},
			},
		},
		CheckResults: nil,
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(testState)
	if err != nil {
		t.Fatalf("Failed to marshal state: %v", err)
	}

	// Unmarshal to a map to inspect the JSON structure
	var jsonMap map[string]interface{}
	if err := json.Unmarshal(jsonData, &jsonMap); err != nil {
		t.Fatalf("Failed to unmarshal to map: %v", err)
	}

	// Check top-level fields
	expectedTopLevel := []string{
		"version",
		"terraform_version",
		"serial",
		"lineage",
		"outputs",
		"resources",
		"check_results",
	}

	for _, field := range expectedTopLevel {
		if _, exists := jsonMap[field]; !exists {
			t.Errorf("Missing required top-level field: %s", field)
		}
	}

	// Check that no unexpected fields are present
	for field := range jsonMap {
		found := false
		for _, expected := range expectedTopLevel {
			if field == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Unexpected top-level field: %s", field)
		}
	}

	// Check resource structure
	if resources, ok := jsonMap["resources"].([]interface{}); ok && len(resources) > 0 {
		resource := resources[0].(map[string]interface{})

		expectedResourceFields := []string{
			"mode", "type", "name", "provider", "instances",
		}

		for _, field := range expectedResourceFields {
			if _, exists := resource[field]; !exists {
				t.Errorf("Missing required resource field: %s", field)
			}
		}

		// Check instance structure
		if instances, ok := resource["instances"].([]interface{}); ok && len(instances) > 0 {
			instance := instances[0].(map[string]interface{})

			expectedInstanceFields := []string{
				"schema_version",
				"attributes",
				"sensitive_attributes",
				"identity_schema_version",
			}

			for _, field := range expectedInstanceFields {
				if _, exists := instance[field]; !exists {
					t.Errorf("Missing required instance field: %s", field)
				}
			}
		}
	}
}

// TestTerraformStateJSONRoundTrip verifies that our state can be
// serialized and deserialized without losing data
//
//nolint:funlen // Comprehensive JSON serialization round-trip test with complex state
func TestTerraformStateJSONRoundTrip(t *testing.T) {
	t.Parallel()
	original := &state.TerraformState{
		Version:          4,
		TerraformVersion: "1.5.7",
		Serial:           42,
		Lineage:          "unique-lineage-id",
		Outputs: map[string]interface{}{
			"test_output": map[string]interface{}{
				"value": "test_value",
				"type":  "string",
			},
		},
		Resources: []state.TerraformStateResource{
			{
				Mode:     "managed",
				Type:     "aws_instance",
				Name:     "web",
				Provider: `provider["registry.terraform.io/hashicorp/aws"]`,
				Instances: []state.TerraformStateResourceInstance{
					{
						SchemaVersion: 1,
						Attributes: map[string]interface{}{
							"id":            "i-1234567890abcdef0",
							"instance_type": "t2.micro",
							"tags": map[string]interface{}{
								"Name": "WebServer",
							},
						},
						SensitiveAttributes:   []string{"password"},
						IdentitySchemaVersion: 2,
						Private:               "private_data_here",
						Dependencies:          []string{"aws_vpc.main"},
						CreateBeforeDestroy:   true,
					},
				},
			},
		},
		CheckResults: nil,
	}

	// Marshal to JSON
	jsonData, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	// Unmarshal back
	var restored state.TerraformState
	if err := json.Unmarshal(jsonData, &restored); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	// Compare key fields
	if restored.Version != original.Version {
		t.Errorf("Version mismatch: got %d, want %d", restored.Version, original.Version)
	}
	if restored.TerraformVersion != original.TerraformVersion {
		t.Errorf("TerraformVersion mismatch: got %s, want %s", restored.TerraformVersion, original.TerraformVersion)
	}
	if restored.Serial != original.Serial {
		t.Errorf("Serial mismatch: got %d, want %d", restored.Serial, original.Serial)
	}
	if restored.Lineage != original.Lineage {
		t.Errorf("Lineage mismatch: got %s, want %s", restored.Lineage, original.Lineage)
	}

	// Check resources
	if len(restored.Resources) != len(original.Resources) {
		t.Fatalf("Resource count mismatch: got %d, want %d", len(restored.Resources), len(original.Resources))
	}

	if len(restored.Resources) > 0 && len(restored.Resources[0].Instances) > 0 {
		restoredInst := restored.Resources[0].Instances[0]
		originalInst := original.Resources[0].Instances[0]

		if restoredInst.SchemaVersion != originalInst.SchemaVersion {
			t.Errorf("SchemaVersion mismatch: got %d, want %d", restoredInst.SchemaVersion, originalInst.SchemaVersion)
		}
		if restoredInst.IdentitySchemaVersion != originalInst.IdentitySchemaVersion {
			t.Errorf("IdentitySchemaVersion mismatch: got %d, want %d", restoredInst.IdentitySchemaVersion, originalInst.IdentitySchemaVersion)
		}
		if !reflect.DeepEqual(restoredInst.SensitiveAttributes, originalInst.SensitiveAttributes) {
			t.Errorf("SensitiveAttributes mismatch: got %v, want %v", restoredInst.SensitiveAttributes, originalInst.SensitiveAttributes)
		}
		if restoredInst.CreateBeforeDestroy != originalInst.CreateBeforeDestroy {
			t.Errorf("CreateBeforeDestroy mismatch: got %v, want %v", restoredInst.CreateBeforeDestroy, originalInst.CreateBeforeDestroy)
		}
	}
}

// TestTerraformStateEmptyArraysVsNull verifies correct handling of
// empty arrays vs null values in JSON
func TestTerraformStateEmptyArraysVsNull(t *testing.T) {
	t.Parallel()
	testState := &state.TerraformState{
		Version:          4,
		TerraformVersion: "1.5.0",
		Serial:           0,
		Lineage:          "test",
		Outputs:          map[string]interface{}{},         // Should be {} not null
		Resources:        []state.TerraformStateResource{}, // Should be [] not null
		CheckResults:     nil,                              // Should be null
	}

	jsonData, err := json.Marshal(testState)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var jsonMap map[string]interface{}
	if err := json.Unmarshal(jsonData, &jsonMap); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	// outputs should be an empty object, not null
	if jsonMap["outputs"] == nil {
		t.Error("outputs should be empty object {}, not null")
	}

	// resources should be an empty array, not null
	if jsonMap["resources"] == nil {
		t.Error("resources should be empty array [], not null")
	}

	// check_results should be null
	if jsonMap["check_results"] != nil {
		t.Error("check_results should be null when not set")
	}
}
