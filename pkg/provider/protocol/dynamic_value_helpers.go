package protocol

import (
	"fmt"
	"math/big"

	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// CreateDynamicValueWithSchema creates a DynamicValue from data using the complete resource schema.
// This implementation uses the official HashiCorp libraries to handle all the complex serialization logic.
func CreateDynamicValueWithSchema(data map[string]interface{}, block *tfprotov6.SchemaBlock) (*tfprotov6.DynamicValue, error) {
	// Get the complete schema type - this includes ALL attributes
	schemaType := block.ValueType()

	// Create a properly typed value that matches the schema
	typedValue, err := CreateValueFromData(data, schemaType)
	if err != nil {
		return nil, fmt.Errorf("failed to create typed value: %w", err)
	}

	// Create the DynamicValue using the complete schema type
	dynamicValue, err := tfprotov6.NewDynamicValue(schemaType, typedValue)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic value: %w", err)
	}

	return &dynamicValue, nil
}

// CreateValueFromData creates a typed tftypes.Value from raw data according to the schema type.
// This function handles all Terraform types including objects, lists, sets, maps, and primitives.
//
//nolint:gocognit,gocyclo,funlen // Type conversion requires handling multiple types
func CreateValueFromData(data interface{}, schemaType tftypes.Type) (tftypes.Value, error) {
	// Handle nil data
	if data == nil {
		return tftypes.NewValue(schemaType, nil), nil
	}

	// Use type switch to handle different type categories
	switch concreteType := schemaType.(type) {
	case tftypes.Object:
		// Handle object types (most common for resources)
		dataMap, ok := data.(map[string]interface{})
		if !ok {
			return tftypes.Value{}, fmt.Errorf("expected map for object type, got %T", data)
		}

		// Create values for ALL attributes in the schema
		values := make(map[string]tftypes.Value)
		for attrName, attrType := range concreteType.AttributeTypes {
			if attrValue, exists := dataMap[attrName]; exists {
				// We have data for this attribute
				typedValue, err := CreateValueFromData(attrValue, attrType)
				if err != nil {
					return tftypes.Value{}, fmt.Errorf("failed to create value for attribute %s: %w", attrName, err)
				}
				values[attrName] = typedValue
			} else {
				// No data provided - create null value of the correct type
				values[attrName] = tftypes.NewValue(attrType, nil)
			}
		}

		return tftypes.NewValue(schemaType, values), nil

	case tftypes.Map:
		// Handle map types
		dataMap, ok := data.(map[string]interface{})
		if !ok {
			return tftypes.Value{}, fmt.Errorf("expected map, got %T", data)
		}

		mapValues := make(map[string]tftypes.Value)
		for k, v := range dataMap {
			elemValue, err := CreateValueFromData(v, concreteType.ElementType)
			if err != nil {
				return tftypes.Value{}, fmt.Errorf("failed to create map element for key %s: %w", k, err)
			}
			mapValues[k] = elemValue
		}

		return tftypes.NewValue(schemaType, mapValues), nil

	case tftypes.List:
		// Handle list types
		dataList, ok := data.([]interface{})
		if !ok {
			return tftypes.Value{}, fmt.Errorf("expected list, got %T", data)
		}

		listValues := make([]tftypes.Value, len(dataList))
		for i, v := range dataList {
			elemValue, err := CreateValueFromData(v, concreteType.ElementType)
			if err != nil {
				return tftypes.Value{}, fmt.Errorf("failed to create list element at index %d: %w", i, err)
			}
			listValues[i] = elemValue
		}

		return tftypes.NewValue(schemaType, listValues), nil

	case tftypes.Set:
		// Handle set types (similar to list)
		dataList, ok := data.([]interface{})
		if !ok {
			return tftypes.Value{}, fmt.Errorf("expected list for set type, got %T", data)
		}

		setValues := make([]tftypes.Value, len(dataList))
		for i, v := range dataList {
			elemValue, err := CreateValueFromData(v, concreteType.ElementType)
			if err != nil {
				return tftypes.Value{}, fmt.Errorf("failed to create set element at index %d: %w", i, err)
			}
			setValues[i] = elemValue
		}

		return tftypes.NewValue(schemaType, setValues), nil

	default:
		// Handle primitive types
		// Note: We can't use direct comparison, so we check using string representation
		typeStr := schemaType.String()

		switch typeStr {
		case "tftypes.String":
			// Handle string type with type coercion
			switch v := data.(type) {
			case string:
				return tftypes.NewValue(tftypes.String, v), nil
			case bool:
				// Coerce bool to string for compatibility with provider schemas
				if v {
					return tftypes.NewValue(tftypes.String, "true"), nil
				}
				return tftypes.NewValue(tftypes.String, "false"), nil
			case int, int64, float64:
				// Coerce numbers to string
				return tftypes.NewValue(tftypes.String, fmt.Sprintf("%v", v)), nil
			default:
				// Try to convert anything else to string
				return tftypes.NewValue(tftypes.String, fmt.Sprintf("%v", v)), nil
			}

		case "tftypes.Number":
			switch v := data.(type) {
			case int:
				return tftypes.NewValue(tftypes.Number, int64(v)), nil
			case int64:
				return tftypes.NewValue(tftypes.Number, v), nil
			case float64:
				return tftypes.NewValue(tftypes.Number, v), nil
			default:
				return tftypes.Value{}, fmt.Errorf("expected number, got %T", data)
			}

		case "tftypes.Bool":
			b, ok := data.(bool)
			if !ok {
				return tftypes.Value{}, fmt.Errorf("expected bool, got %T", data)
			}
			return tftypes.NewValue(tftypes.Bool, b), nil

		default:
			// Check if it's a DynamicPseudoType
			if schemaType.Is(tftypes.DynamicPseudoType) {
				// For dynamic types, infer the type from data
				switch v := data.(type) {
				case string:
					return tftypes.NewValue(tftypes.String, v), nil
				case bool:
					return tftypes.NewValue(tftypes.Bool, v), nil
				case int, int64, float64:
					return tftypes.NewValue(tftypes.Number, v), nil
				case map[string]interface{}:
					// Create a map of string to string for simple maps
					mapVals := make(map[string]tftypes.Value)
					for k, val := range v {
						if strVal, ok := val.(string); ok {
							mapVals[k] = tftypes.NewValue(tftypes.String, strVal)
						}
					}
					return tftypes.NewValue(tftypes.Map{ElementType: tftypes.String}, mapVals), nil
				default:
					return tftypes.NewValue(tftypes.String, fmt.Sprintf("%v", v)), nil
				}
			}

			// Try to create value directly as last resort
			return tftypes.NewValue(schemaType, data), nil
		}
	}
}

// ConvertDynamicValueToMap converts a tfprotov6.DynamicValue back to a map[string]interface{}.
// This is used after Apply operations to convert the provider's response back to our internal format.
func ConvertDynamicValueToMap(dv *tfprotov6.DynamicValue, schemaType tftypes.Type) (map[string]interface{}, error) {
	if dv == nil || len(dv.MsgPack) == 0 {
		return nil, nil
	}

	// Unmarshal the msgpack bytes into a tftypes.Value
	val, err := dv.Unmarshal(schemaType)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal dynamic value: %w", err)
	}

	// Convert the tftypes.Value to a Go value
	result, err := ConvertTftypesValueToInterface(val)
	if err != nil {
		return nil, fmt.Errorf("failed to convert value to interface: %w", err)
	}

	// Ensure we return a map
	if result == nil {
		return nil, nil
	}

	resultMap, ok := result.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("expected map[string]interface{}, got %T", result)
	}

	return resultMap, nil
}

// ConvertTftypesValueToInterface converts a tftypes.Value to a Go interface{}.
// This handles all Terraform types including objects, lists, sets, maps, and primitives.
//
//nolint:gocognit,funlen,gocyclo // Complex type conversion from terraform types to interface
func ConvertTftypesValueToInterface(val tftypes.Value) (interface{}, error) {
	// Handle null/unknown values
	if val.IsNull() || !val.IsKnown() {
		return nil, nil
	}

	typ := val.Type()

	// Use type switch on the type
	switch typ.(type) {
	case tftypes.Object:
		// Convert object to map
		result := make(map[string]interface{})
		attrs := make(map[string]tftypes.Value)

		err := val.As(&attrs)
		if err != nil {
			return nil, fmt.Errorf("failed to convert object value: %w", err)
		}

		for key, attrVal := range attrs {
			convertedVal, err := ConvertTftypesValueToInterface(attrVal)
			if err != nil {
				return nil, fmt.Errorf("failed to convert attribute %s: %w", key, err)
			}
			result[key] = convertedVal
		}

		return result, nil

	case tftypes.Map:
		// Convert map to map[string]interface{}
		result := make(map[string]interface{})
		mapVals := make(map[string]tftypes.Value)

		err := val.As(&mapVals)
		if err != nil {
			return nil, fmt.Errorf("failed to convert map value: %w", err)
		}

		for key, elemVal := range mapVals {
			convertedVal, err := ConvertTftypesValueToInterface(elemVal)
			if err != nil {
				return nil, fmt.Errorf("failed to convert map element %s: %w", key, err)
			}
			result[key] = convertedVal
		}

		return result, nil

	case tftypes.List:
		// Convert list to []interface{}
		var listVals []tftypes.Value
		err := val.As(&listVals)
		if err != nil {
			return nil, fmt.Errorf("failed to convert list value: %w", err)
		}

		result := make([]interface{}, len(listVals))
		for i, elemVal := range listVals {
			convertedVal, err := ConvertTftypesValueToInterface(elemVal)
			if err != nil {
				return nil, fmt.Errorf("failed to convert list element %d: %w", i, err)
			}
			result[i] = convertedVal
		}

		return result, nil

	case tftypes.Set:
		// Convert set to []interface{} (sets become lists in our representation)
		var setVals []tftypes.Value
		err := val.As(&setVals)
		if err != nil {
			return nil, fmt.Errorf("failed to convert set value: %w", err)
		}

		result := make([]interface{}, len(setVals))
		for i, elemVal := range setVals {
			convertedVal, err := ConvertTftypesValueToInterface(elemVal)
			if err != nil {
				return nil, fmt.Errorf("failed to convert set element %d: %w", i, err)
			}
			result[i] = convertedVal
		}

		return result, nil

	default:
		// Handle primitive types
		typeStr := typ.String()

		switch typeStr {
		case "tftypes.String":
			var str string
			err := val.As(&str)
			if err != nil {
				return nil, fmt.Errorf("failed to convert string value: %w", err)
			}
			return str, nil

		case "tftypes.Number":
			// Try to get as int64 first, then float64
			var i int64
			if err := val.As(&i); err == nil {
				return i, nil
			}

			var f float64
			if err := val.As(&f); err == nil {
				return f, nil
			}

			// Fall back to big.Float for very large numbers
			var bf *big.Float
			err := val.As(&bf)
			if err != nil {
				return nil, fmt.Errorf("failed to convert number value: %w", err)
			}

			// Try to convert to int64 or float64
			if bf.IsInt() {
				i, _ := bf.Int64()
				return i, nil
			}
			f, _ = bf.Float64()
			return f, nil

		case "tftypes.Bool":
			var b bool
			err := val.As(&b)
			if err != nil {
				return nil, fmt.Errorf("failed to convert bool value: %w", err)
			}
			return b, nil

		default:
			// For dynamic pseudo type or unknown types, try generic conversion
			var result interface{}
			err := val.As(&result)
			if err != nil {
				return nil, fmt.Errorf("failed to convert value of type %s: %w", typeStr, err)
			}
			return result, nil
		}
	}
}
