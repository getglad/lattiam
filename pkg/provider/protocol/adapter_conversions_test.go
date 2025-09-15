//go:build !integration
// +build !integration

package protocol

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-go/tfprotov5"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lattiam/lattiam/internal/proto/tfplugin5"
	"github.com/lattiam/lattiam/internal/proto/tfplugin6"
)

// TestSchemaTypeConversion tests the conversion of type information from proto to tftypes
//
//nolint:funlen,gocognit // Comprehensive schema type conversion test covering multiple protocol versions
func TestSchemaTypeConversion(t *testing.T) {
	t.Parallel()
	t.Run("V5_BasicTypes", func(t *testing.T) {
		t.Parallel()
		// Test basic type conversions for v5
		tests := []struct {
			name     string
			typeJSON []byte
			expected tftypes.Type
		}{
			{
				name:     "string",
				typeJSON: []byte(`"string"`),
				expected: tftypes.String,
			},
			{
				name:     "number",
				typeJSON: []byte(`"number"`),
				expected: tftypes.Number,
			},
			{
				name:     "bool",
				typeJSON: []byte(`"bool"`),
				expected: tftypes.Bool,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				// Test convertJSONBytesToType function
				convertedType, err := convertJSONBytesToType(tt.typeJSON)
				require.NoError(t, err, "convertJSONBytesToType should not error for %s", tt.name)
				assert.Equal(t, tt.expected, convertedType, "Type should match for %s", tt.name)
			})
		}
	})

	t.Run("V5_ComplexTypes", func(t *testing.T) {
		t.Parallel()
		// Test complex type conversions
		tests := []struct {
			name         string
			typeJSON     []byte
			validateType func(t *testing.T, typ tftypes.Type)
		}{
			{
				name:     "list_string",
				typeJSON: []byte(`["list", "string"]`),
				validateType: func(t *testing.T, typ tftypes.Type) {
					t.Helper()
					listType, ok := typ.(tftypes.List)
					require.True(t, ok, "Should be a list type")
					assert.Equal(t, tftypes.String, listType.ElementType, "List element should be string")
				},
			},
			{
				name:     "set_string",
				typeJSON: []byte(`["set", "string"]`),
				validateType: func(t *testing.T, typ tftypes.Type) {
					t.Helper()
					setType, ok := typ.(tftypes.Set)
					require.True(t, ok, "Should be a set type")
					assert.Equal(t, tftypes.String, setType.ElementType, "Set element should be string")
				},
			},
			{
				name:     "map_string",
				typeJSON: []byte(`["map", "string"]`),
				validateType: func(t *testing.T, typ tftypes.Type) {
					t.Helper()
					mapType, ok := typ.(tftypes.Map)
					require.True(t, ok, "Should be a map type")
					assert.Equal(t, tftypes.String, mapType.ElementType, "Map element should be string")
				},
			},
			{
				name:     "map_number",
				typeJSON: []byte(`["map", "number"]`),
				validateType: func(t *testing.T, typ tftypes.Type) {
					t.Helper()
					mapType, ok := typ.(tftypes.Map)
					require.True(t, ok, "Should be a map type")
					assert.Equal(t, tftypes.Number, mapType.ElementType, "Map element should be number")
				},
			},
			{
				name:     "object",
				typeJSON: []byte(`["object", {"name": "string", "age": "number", "active": "bool"}]`),
				validateType: func(t *testing.T, typ tftypes.Type) {
					t.Helper()
					objType, ok := typ.(tftypes.Object)
					require.True(t, ok, "Should be an object type")
					assert.Len(t, objType.AttributeTypes, 3, "Object should have 3 attributes")
					assert.Equal(t, tftypes.String, objType.AttributeTypes["name"], "name should be string")
					assert.Equal(t, tftypes.Number, objType.AttributeTypes["age"], "age should be number")
					assert.Equal(t, tftypes.Bool, objType.AttributeTypes["active"], "active should be bool")
				},
			},
			{
				name:     "list_object",
				typeJSON: []byte(`["list", ["object", {"id": "string", "value": "number"}]]`),
				validateType: func(t *testing.T, typ tftypes.Type) {
					t.Helper()
					listType, ok := typ.(tftypes.List)
					require.True(t, ok, "Should be a list type")
					objType, ok := listType.ElementType.(tftypes.Object)
					require.True(t, ok, "List element should be object type")
					assert.Len(t, objType.AttributeTypes, 2, "Object should have 2 attributes")
					assert.Equal(t, tftypes.String, objType.AttributeTypes["id"], "id should be string")
					assert.Equal(t, tftypes.Number, objType.AttributeTypes["value"], "value should be number")
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				// Test convertJSONBytesToType function
				convertedType, err := convertJSONBytesToType(tt.typeJSON)
				require.NoError(t, err, "convertJSONBytesToType should not error for %s", tt.name)
				tt.validateType(t, convertedType)
			})
		}
	})

	t.Run("V6_BasicTypes", func(t *testing.T) {
		t.Parallel()
		// Test basic type conversions for v6
		tests := []struct {
			name     string
			typeJSON []byte
			expected tftypes.Type
		}{
			{
				name:     "string",
				typeJSON: []byte(`"string"`),
				expected: tftypes.String,
			},
			{
				name:     "number",
				typeJSON: []byte(`"number"`),
				expected: tftypes.Number,
			},
			{
				name:     "bool",
				typeJSON: []byte(`"bool"`),
				expected: tftypes.Bool,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				// Test convertJSONBytesToType function for v6 (same logic as v5)
				convertedType, err := convertJSONBytesToType(tt.typeJSON)
				require.NoError(t, err, "convertJSONBytesToType should not error for %s", tt.name)
				assert.Equal(t, tt.expected, convertedType, "Type should match for %s", tt.name)
			})
		}
	})

	t.Run("ConvertJSONBytesToType_EdgeCases", func(t *testing.T) {
		t.Parallel()
		// Test edge cases and error conditions
		tests := []struct {
			name        string
			typeJSON    []byte
			shouldError bool
			expected    tftypes.Type
		}{
			{
				name:        "empty_bytes",
				typeJSON:    []byte{},
				shouldError: false,
				expected:    tftypes.DynamicPseudoType,
			},
			{
				name:        "dynamic_type",
				typeJSON:    []byte(`"dynamic"`),
				shouldError: false,
				expected:    tftypes.DynamicPseudoType,
			},
			{
				name:        "tuple_type",
				typeJSON:    []byte(`["tuple", "string", "number", "bool"]`),
				shouldError: false,
				expected:    nil, // Will validate tuple structure
			},
			{
				name:        "nested_list",
				typeJSON:    []byte(`["list", ["list", "string"]]`),
				shouldError: false,
				expected:    nil, // Will validate nested structure
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()
				convertedType, err := convertJSONBytesToType(tt.typeJSON)

				if tt.shouldError {
					require.Error(t, err, "Should error for %s", tt.name)
				} else {
					require.NoError(t, err, "Should not error for %s", tt.name)

					// Special validation for complex types
					switch tt.name {
					case "tuple_type":
						tupleType, ok := convertedType.(tftypes.Tuple)
						require.True(t, ok, "Should be a tuple type")
						assert.Len(t, tupleType.ElementTypes, 3, "Tuple should have 3 elements")
						assert.Equal(t, tftypes.String, tupleType.ElementTypes[0])
						assert.Equal(t, tftypes.Number, tupleType.ElementTypes[1])
						assert.Equal(t, tftypes.Bool, tupleType.ElementTypes[2])
					case "nested_list":
						outerList, ok := convertedType.(tftypes.List)
						require.True(t, ok, "Should be a list type")
						innerList, ok := outerList.ElementType.(tftypes.List)
						require.True(t, ok, "Inner type should be a list")
						assert.Equal(t, tftypes.String, innerList.ElementType)
					default:
						if tt.expected != nil {
							assert.Equal(t, tt.expected, convertedType, "Type should match for %s", tt.name)
						}
					}
				}
			})
		}
	})

	t.Run("ConvertAttributePath", func(t *testing.T) {
		t.Parallel()
		// Test attribute path conversion
		t.Run("V5_AttributePath", func(t *testing.T) {
			t.Parallel()
			// Create a v5 attribute path
			path := &tfplugin5.AttributePath{
				Steps: []*tfplugin5.AttributePath_Step{
					{
						Selector: &tfplugin5.AttributePath_Step_AttributeName{
							AttributeName: "bucket",
						},
					},
					{
						Selector: &tfplugin5.AttributePath_Step_ElementKeyInt{
							ElementKeyInt: 0,
						},
					},
					{
						Selector: &tfplugin5.AttributePath_Step_AttributeName{
							AttributeName: "name",
						},
					},
				},
			}

			// Convert
			tfPath := convertAttributePathFromProtoV5(path)
			require.NotNil(t, tfPath)

			// The path should represent: bucket[0].name
			// We can't easily inspect tftypes.AttributePath internals,
			// but we can verify it was created without error
		})

		t.Run("V6_AttributePath", func(t *testing.T) {
			t.Parallel()
			// Create a v6 attribute path
			path := &tfplugin6.AttributePath{
				Steps: []*tfplugin6.AttributePath_Step{
					{
						Selector: &tfplugin6.AttributePath_Step_AttributeName{
							AttributeName: "tags",
						},
					},
					{
						Selector: &tfplugin6.AttributePath_Step_ElementKeyString{
							ElementKeyString: "environment",
						},
					},
				},
			}

			// Convert
			tfPath := convertAttributePathFromProtoV6(path)
			require.NotNil(t, tfPath)

			// The path should represent: tags["environment"]
		})
	})

	t.Run("ConvertDiagnostics", func(t *testing.T) {
		t.Parallel()
		t.Run("V5_Diagnostics", func(t *testing.T) {
			t.Parallel()
			// Create v5 diagnostics
			diags := []*tfplugin5.Diagnostic{
				{
					Severity: tfplugin5.Diagnostic_ERROR,
					Summary:  "Invalid configuration",
					Detail:   "The bucket name is required",
					Attribute: &tfplugin5.AttributePath{
						Steps: []*tfplugin5.AttributePath_Step{
							{
								Selector: &tfplugin5.AttributePath_Step_AttributeName{
									AttributeName: "bucket",
								},
							},
						},
					},
				},
				{
					Severity: tfplugin5.Diagnostic_WARNING,
					Summary:  "Deprecated attribute",
					Detail:   "The 'force_destroy' attribute is deprecated",
				},
			}

			// Convert
			tfDiags := convertDiagnosticsFromProtoV5(diags)
			require.Len(t, tfDiags, 2)

			// Check first diagnostic
			assert.Equal(t, tfprotov5.DiagnosticSeverityError, tfDiags[0].Severity)
			assert.Equal(t, "Invalid configuration", tfDiags[0].Summary)
			assert.Equal(t, "The bucket name is required", tfDiags[0].Detail)
			assert.NotNil(t, tfDiags[0].Attribute)

			// Check second diagnostic
			assert.Equal(t, tfprotov5.DiagnosticSeverityWarning, tfDiags[1].Severity)
			assert.Equal(t, "Deprecated attribute", tfDiags[1].Summary)
			assert.Nil(t, tfDiags[1].Attribute)
		})

		t.Run("V6_Diagnostics", func(t *testing.T) {
			t.Parallel()
			// Create v6 diagnostics
			diags := []*tfplugin6.Diagnostic{
				{
					Severity: tfplugin6.Diagnostic_ERROR,
					Summary:  "Resource not found",
					Detail:   "The S3 bucket does not exist",
				},
			}

			// Convert
			tfDiags := convertDiagnosticsFromProtoV6(diags)
			require.Len(t, tfDiags, 1)

			assert.Equal(t, tfprotov6.DiagnosticSeverityError, tfDiags[0].Severity)
			assert.Equal(t, "Resource not found", tfDiags[0].Summary)
			assert.Equal(t, "The S3 bucket does not exist", tfDiags[0].Detail)
		})
	})

	t.Run("ConvertDynamicValue", func(t *testing.T) {
		t.Parallel()
		t.Run("V5_DynamicValue", func(t *testing.T) {
			t.Parallel()
			// Test conversion from tfprotov5.DynamicValue to proto
			dv := &tfprotov5.DynamicValue{
				MsgPack: []byte{0x81, 0xa4, 0x6e, 0x61, 0x6d, 0x65, 0xa4, 0x74, 0x65, 0x73, 0x74},
				JSON:    []byte(`{"name":"test"}`),
			}

			protoDv := convertDynamicValueToProtoV5(dv)
			require.NotNil(t, protoDv)
			assert.Equal(t, dv.MsgPack, protoDv.Msgpack)
			assert.Equal(t, dv.JSON, protoDv.Json)
		})

		t.Run("V6_DynamicValue", func(t *testing.T) {
			t.Parallel()
			// Test conversion from tfprotov6.DynamicValue to proto
			dv := &tfprotov6.DynamicValue{
				MsgPack: []byte{0x81, 0xa2, 0x69, 0x64, 0xa3, 0x31, 0x32, 0x33},
				JSON:    []byte(`{"id":"123"}`),
			}

			protoDv := convertDynamicValueToProtoV6(dv)
			require.NotNil(t, protoDv)
			assert.Equal(t, dv.MsgPack, protoDv.Msgpack)
			assert.Equal(t, dv.JSON, protoDv.Json)
		})
	})

	t.Run("SchemaConversion", func(t *testing.T) {
		t.Parallel()
		t.Run("V5_Schema", func(t *testing.T) {
			t.Parallel()
			// Create a complete v5 schema
			protoSchema := &tfplugin5.Schema{
				Version: 1,
				Block: &tfplugin5.Schema_Block{
					Version:     1,
					Description: "Test resource schema",
					Attributes: []*tfplugin5.Schema_Attribute{
						{
							Name:        "id",
							Type:        []byte(`"string"`),
							Description: "Resource ID",
							Computed:    true,
						},
						{
							Name:        "name",
							Type:        []byte(`"string"`),
							Description: "Resource name",
							Required:    true,
						},
						{
							Name:        "tags",
							Type:        []byte(`["map", "string"]`),
							Description: "Resource tags",
							Optional:    true,
						},
					},
					BlockTypes: []*tfplugin5.Schema_NestedBlock{
						{
							TypeName: "lifecycle",
							Nesting:  tfplugin5.Schema_NestedBlock_SINGLE,
							Block: &tfplugin5.Schema_Block{
								Attributes: []*tfplugin5.Schema_Attribute{
									{
										Name:     "create_before_destroy",
										Type:     []byte(`"bool"`),
										Optional: true,
									},
								},
							},
						},
					},
				},
			}

			// Convert
			tfSchema := convertSchemaFromProtoV5(protoSchema)
			require.NotNil(t, tfSchema)
			assert.Equal(t, int64(1), tfSchema.Version)
			assert.NotNil(t, tfSchema.Block)
			assert.Equal(t, "Test resource schema", tfSchema.Block.Description)
			assert.Len(t, tfSchema.Block.Attributes, 3)
			assert.Len(t, tfSchema.Block.BlockTypes, 1)
		})

		t.Run("V6_Schema", func(t *testing.T) {
			t.Parallel()
			// Create a complete v6 schema
			protoSchema := &tfplugin6.Schema{
				Version: 2,
				Block: &tfplugin6.Schema_Block{
					Version:     2,
					Description: "Advanced resource schema",
					Attributes: []*tfplugin6.Schema_Attribute{
						{
							Name:        "id",
							Type:        []byte(`"string"`),
							Description: "Unique identifier",
							Computed:    true,
						},
						{
							Name:        "config",
							Description: "Nested configuration",
							NestedType: &tfplugin6.Schema_Object{
								Nesting: tfplugin6.Schema_Object_SINGLE,
								Attributes: []*tfplugin6.Schema_Attribute{
									{
										Name:     "enabled",
										Type:     []byte(`"bool"`),
										Optional: true,
									},
									{
										Name:     "timeout",
										Type:     []byte(`"number"`),
										Optional: true,
									},
								},
							},
							Optional: true,
						},
					},
				},
			}

			// Convert
			tfSchema := convertSchemaFromProtoV6(protoSchema)
			require.NotNil(t, tfSchema)
			assert.Equal(t, int64(2), tfSchema.Version)
			assert.NotNil(t, tfSchema.Block)
			assert.Len(t, tfSchema.Block.Attributes, 2)

			// Check that nested type was converted
			configAttr := tfSchema.Block.Attributes[1]
			assert.Equal(t, "config", configAttr.Name)
			assert.NotNil(t, configAttr.NestedType)
		})
	})
}
