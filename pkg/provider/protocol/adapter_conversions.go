package protocol

import (
	"encoding/json"
	"fmt"

	"github.com/hashicorp/terraform-plugin-go/tfprotov5"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/lattiam/lattiam/internal/proto/tfplugin5"
	"github.com/lattiam/lattiam/internal/proto/tfplugin6"
)

// Conversion helpers for v5 protocol

func convertSchemaFromProtoV5(proto *tfplugin5.Schema) *tfprotov5.Schema {
	if proto == nil {
		return nil
	}

	return &tfprotov5.Schema{
		Version: proto.Version,
		Block:   convertBlockFromProtoV5(proto.Block),
	}
}

func convertBlockFromProtoV5(proto *tfplugin5.Schema_Block) *tfprotov5.SchemaBlock {
	if proto == nil {
		return nil
	}

	block := &tfprotov5.SchemaBlock{
		Version:         proto.Version,
		Description:     proto.Description,
		DescriptionKind: tfprotov5.StringKind(proto.DescriptionKind),
		Deprecated:      proto.Deprecated,
	}

	// Convert attributes
	for _, attr := range proto.Attributes {
		// Convert the type from JSON
		attrType, err := convertJSONBytesToType(attr.Type)
		if err != nil {
			// Log error but continue - better to have partial schema than fail completely
			GetDebugLogger().Logf("schema-conversion", "Failed to convert type for attribute %s: %v", attr.Name, err)
			attrType = tftypes.DynamicPseudoType // Fallback to dynamic type
		}

		block.Attributes = append(block.Attributes, &tfprotov5.SchemaAttribute{
			Name:            attr.Name,
			Type:            attrType,
			Description:     attr.Description,
			Required:        attr.Required,
			Optional:        attr.Optional,
			Computed:        attr.Computed,
			Sensitive:       attr.Sensitive,
			DescriptionKind: tfprotov5.StringKind(attr.DescriptionKind),
			Deprecated:      attr.Deprecated,
		})
	}

	// Convert block types
	for _, bt := range proto.BlockTypes {
		blockType := &tfprotov5.SchemaNestedBlock{
			TypeName: bt.TypeName,
			Block:    convertBlockFromProtoV5(bt.Block),
			Nesting:  tfprotov5.SchemaNestedBlockNestingMode(bt.Nesting),
			MinItems: bt.MinItems,
			MaxItems: bt.MaxItems,
		}
		block.BlockTypes = append(block.BlockTypes, blockType)
	}

	return block
}

func convertDiagnosticsFromProtoV5(protos []*tfplugin5.Diagnostic) []*tfprotov5.Diagnostic {
	diags := make([]*tfprotov5.Diagnostic, 0, len(protos))
	for _, proto := range protos {
		diag := &tfprotov5.Diagnostic{
			Severity: tfprotov5.DiagnosticSeverity(proto.Severity),
			Summary:  proto.Summary,
			Detail:   proto.Detail,
		}
		if proto.Attribute != nil {
			diag.Attribute = convertAttributePathFromProtoV5(proto.Attribute)
		}
		diags = append(diags, diag)
	}
	return diags
}

func convertAttributePathFromProtoV5(proto *tfplugin5.AttributePath) *tftypes.AttributePath {
	if proto == nil {
		return nil
	}

	path := tftypes.NewAttributePath()
	for _, step := range proto.Steps {
		switch s := step.Selector.(type) {
		case *tfplugin5.AttributePath_Step_AttributeName:
			path = path.WithAttributeName(s.AttributeName)
		case *tfplugin5.AttributePath_Step_ElementKeyString:
			path = path.WithElementKeyString(s.ElementKeyString)
		case *tfplugin5.AttributePath_Step_ElementKeyInt:
			path = path.WithElementKeyInt(int(s.ElementKeyInt))
		}
	}
	return path
}

func convertDynamicValueToProtoV5(dv *tfprotov5.DynamicValue) *tfplugin5.DynamicValue {
	if dv == nil {
		return nil
	}
	return &tfplugin5.DynamicValue{
		Msgpack: dv.MsgPack,
		Json:    dv.JSON,
	}
}

//nolint:dupl // Similar to V6 version but handles different protocol types
func convertFunctionFromProtoV5(proto *tfplugin5.Function) *tfprotov5.Function {
	if proto == nil {
		return nil
	}

	fn := &tfprotov5.Function{
		Summary:            proto.Summary,
		Description:        proto.Description,
		DescriptionKind:    tfprotov5.StringKind(proto.DescriptionKind),
		DeprecationMessage: proto.DeprecationMessage,
	}

	// Convert parameters
	for _, param := range proto.Parameters {
		fn.Parameters = append(fn.Parameters, &tfprotov5.FunctionParameter{
			Name:               param.Name,
			Type:               nil, // Would need type conversion
			AllowNullValue:     param.AllowNullValue,
			AllowUnknownValues: param.AllowUnknownValues,
			Description:        param.Description,
			DescriptionKind:    tfprotov5.StringKind(param.DescriptionKind),
		})
	}

	// Convert variadic parameter
	if proto.VariadicParameter != nil {
		fn.VariadicParameter = &tfprotov5.FunctionParameter{
			Name:               proto.VariadicParameter.Name,
			Type:               nil, // Would need type conversion
			AllowNullValue:     proto.VariadicParameter.AllowNullValue,
			AllowUnknownValues: proto.VariadicParameter.AllowUnknownValues,
			Description:        proto.VariadicParameter.Description,
			DescriptionKind:    tfprotov5.StringKind(proto.VariadicParameter.DescriptionKind),
		}
	}

	// Return would also have a type that needs conversion

	return fn
}

// Conversion helpers for v6 protocol

func convertSchemaFromProtoV6(proto *tfplugin6.Schema) *tfprotov6.Schema {
	if proto == nil {
		return nil
	}

	return &tfprotov6.Schema{
		Version: proto.Version,
		Block:   convertBlockFromProtoV6(proto.Block),
	}
}

func convertBlockFromProtoV6(proto *tfplugin6.Schema_Block) *tfprotov6.SchemaBlock {
	if proto == nil {
		return nil
	}

	block := &tfprotov6.SchemaBlock{
		Version:         proto.Version,
		Description:     proto.Description,
		DescriptionKind: tfprotov6.StringKind(proto.DescriptionKind),
		Deprecated:      proto.Deprecated,
	}

	// Convert attributes
	for _, attr := range proto.Attributes {
		// Convert the type from JSON
		var attrType tftypes.Type
		if len(attr.Type) > 0 {
			convertedType, err := convertJSONBytesToType(attr.Type)
			if err != nil {
				// Log error but continue - better to have partial schema than fail completely
				GetDebugLogger().Logf("schema-conversion", "Failed to convert type for attribute %s: %v", attr.Name, err)
				attrType = tftypes.DynamicPseudoType // Fallback to dynamic type
			} else {
				attrType = convertedType
			}
		}

		schemaAttr := &tfprotov6.SchemaAttribute{
			Name:            attr.Name,
			Type:            attrType,
			Description:     attr.Description,
			Required:        attr.Required,
			Optional:        attr.Optional,
			Computed:        attr.Computed,
			Sensitive:       attr.Sensitive,
			DescriptionKind: tfprotov6.StringKind(attr.DescriptionKind),
			Deprecated:      attr.Deprecated,
		}

		// Handle nested attributes for v6
		if attr.NestedType != nil {
			schemaAttr.NestedType = convertNestedTypeFromProtoV6(attr.NestedType)
		}

		block.Attributes = append(block.Attributes, schemaAttr)
	}

	// Convert block types
	for _, bt := range proto.BlockTypes {
		blockType := &tfprotov6.SchemaNestedBlock{
			TypeName: bt.TypeName,
			Block:    convertBlockFromProtoV6(bt.Block),
			Nesting:  tfprotov6.SchemaNestedBlockNestingMode(bt.Nesting),
			MinItems: bt.MinItems,
			MaxItems: bt.MaxItems,
		}
		block.BlockTypes = append(block.BlockTypes, blockType)
	}

	return block
}

func convertNestedTypeFromProtoV6(proto *tfplugin6.Schema_Object) *tfprotov6.SchemaObject {
	if proto == nil {
		return nil
	}

	obj := &tfprotov6.SchemaObject{
		Nesting: tfprotov6.SchemaObjectNestingMode(proto.Nesting),
	}

	// Convert nested attributes
	for _, attr := range proto.Attributes {
		schemaAttr := &tfprotov6.SchemaAttribute{
			Name:            attr.Name,
			Type:            nil, // Would need type conversion
			Description:     attr.Description,
			Required:        attr.Required,
			Optional:        attr.Optional,
			Computed:        attr.Computed,
			Sensitive:       attr.Sensitive,
			DescriptionKind: tfprotov6.StringKind(attr.DescriptionKind),
			Deprecated:      attr.Deprecated,
		}

		if attr.NestedType != nil {
			schemaAttr.NestedType = convertNestedTypeFromProtoV6(attr.NestedType)
		}

		obj.Attributes = append(obj.Attributes, schemaAttr)
	}

	return obj
}

func convertDiagnosticsFromProtoV6(protos []*tfplugin6.Diagnostic) []*tfprotov6.Diagnostic {
	diags := make([]*tfprotov6.Diagnostic, 0, len(protos))
	for _, proto := range protos {
		diag := &tfprotov6.Diagnostic{
			Severity: tfprotov6.DiagnosticSeverity(proto.Severity),
			Summary:  proto.Summary,
			Detail:   proto.Detail,
		}
		if proto.Attribute != nil {
			diag.Attribute = convertAttributePathFromProtoV6(proto.Attribute)
		}
		diags = append(diags, diag)
	}
	return diags
}

func convertAttributePathFromProtoV6(proto *tfplugin6.AttributePath) *tftypes.AttributePath {
	if proto == nil {
		return nil
	}

	path := tftypes.NewAttributePath()
	for _, step := range proto.Steps {
		switch s := step.Selector.(type) {
		case *tfplugin6.AttributePath_Step_AttributeName:
			path = path.WithAttributeName(s.AttributeName)
		case *tfplugin6.AttributePath_Step_ElementKeyString:
			path = path.WithElementKeyString(s.ElementKeyString)
		case *tfplugin6.AttributePath_Step_ElementKeyInt:
			path = path.WithElementKeyInt(int(s.ElementKeyInt))
		}
	}
	return path
}

// convertAttributePathsFromProtoV6 converts multiple tfplugin6 AttributePaths to tftypes
func convertAttributePathsFromProtoV6(protoPaths []*tfplugin6.AttributePath) []*tftypes.AttributePath {
	if len(protoPaths) == 0 {
		return nil
	}

	paths := make([]*tftypes.AttributePath, len(protoPaths))
	for i, protoPath := range protoPaths {
		paths[i] = convertAttributePathFromProtoV6(protoPath)
	}
	return paths
}

// convertJSONBytesToType converts JSON type representation to tftypes.Type
//
//nolint:gocognit,funlen,gocyclo // Complex type conversion logic with recursive type handling
func convertJSONBytesToType(jsonBytes []byte) (tftypes.Type, error) {
	if len(jsonBytes) == 0 {
		return tftypes.DynamicPseudoType, nil
	}

	// Debug log the raw bytes
	GetDebugLogger().Logf("type-conversion", "Converting type bytes: %s", string(jsonBytes))

	// First, try to unmarshal as a simple string (primitive types)
	var simpleType string
	if err := json.Unmarshal(jsonBytes, &simpleType); err == nil {
		switch simpleType {
		case "string":
			return tftypes.String, nil
		case "number":
			return tftypes.Number, nil
		case "bool":
			return tftypes.Bool, nil
		case "dynamic":
			return tftypes.DynamicPseudoType, nil
		default:
			return nil, fmt.Errorf("unknown primitive type: %s", simpleType)
		}
	}

	// If not a simple type, try to unmarshal as an array (complex types)
	var typeArray []json.RawMessage
	if err := json.Unmarshal(jsonBytes, &typeArray); err != nil {
		return nil, fmt.Errorf("failed to parse type JSON: %w", err)
	}

	if len(typeArray) < 2 {
		return nil, fmt.Errorf("invalid type array length: %d", len(typeArray))
	}

	// Get the type name
	var typeName string
	if err := json.Unmarshal(typeArray[0], &typeName); err != nil {
		return nil, fmt.Errorf("failed to parse type name: %w", err)
	}

	switch typeName {
	case "list":
		// Parse element type
		elemType, err := convertJSONBytesToType(typeArray[1])
		if err != nil {
			return nil, fmt.Errorf("failed to parse list element type: %w", err)
		}
		return tftypes.List{ElementType: elemType}, nil

	case "set":
		// Parse element type
		elemType, err := convertJSONBytesToType(typeArray[1])
		if err != nil {
			return nil, fmt.Errorf("failed to parse set element type: %w", err)
		}
		return tftypes.Set{ElementType: elemType}, nil

	case "map":
		// Parse element type
		elemType, err := convertJSONBytesToType(typeArray[1])
		if err != nil {
			return nil, fmt.Errorf("failed to parse map element type: %w", err)
		}
		return tftypes.Map{ElementType: elemType}, nil

	case "object":
		// Parse object attributes
		var attrsMap map[string]json.RawMessage
		if err := json.Unmarshal(typeArray[1], &attrsMap); err != nil {
			return nil, fmt.Errorf("failed to parse object attributes: %w", err)
		}

		attrTypes := make(map[string]tftypes.Type)
		for name, attrJSON := range attrsMap {
			attrType, err := convertJSONBytesToType(attrJSON)
			if err != nil {
				return nil, fmt.Errorf("failed to parse object attribute %s: %w", name, err)
			}
			attrTypes[name] = attrType
		}

		return tftypes.Object{AttributeTypes: attrTypes}, nil

	case "tuple":
		// Parse tuple element types
		var elemTypes []tftypes.Type
		for i := 1; i < len(typeArray); i++ {
			elemType, err := convertJSONBytesToType(typeArray[i])
			if err != nil {
				return nil, fmt.Errorf("failed to parse tuple element %d: %w", i-1, err)
			}
			elemTypes = append(elemTypes, elemType)
		}
		return tftypes.Tuple{ElementTypes: elemTypes}, nil

	default:
		return nil, fmt.Errorf("unknown complex type: %s", typeName)
	}
}

func convertDynamicValueToProtoV6(dv *tfprotov6.DynamicValue) *tfplugin6.DynamicValue {
	if dv == nil {
		return nil
	}
	return &tfplugin6.DynamicValue{
		Msgpack: dv.MsgPack,
		Json:    dv.JSON,
	}
}

//nolint:dupl // Similar to V5 version but handles different protocol types
func convertFunctionFromProtoV6(proto *tfplugin6.Function) *tfprotov6.Function {
	if proto == nil {
		return nil
	}

	fn := &tfprotov6.Function{
		Summary:            proto.Summary,
		Description:        proto.Description,
		DescriptionKind:    tfprotov6.StringKind(proto.DescriptionKind),
		DeprecationMessage: proto.DeprecationMessage,
	}

	// Convert parameters
	for _, param := range proto.Parameters {
		fn.Parameters = append(fn.Parameters, &tfprotov6.FunctionParameter{
			Name:               param.Name,
			Type:               nil, // Would need type conversion
			AllowNullValue:     param.AllowNullValue,
			AllowUnknownValues: param.AllowUnknownValues,
			Description:        param.Description,
			DescriptionKind:    tfprotov6.StringKind(param.DescriptionKind),
		})
	}

	// Convert variadic parameter
	if proto.VariadicParameter != nil {
		fn.VariadicParameter = &tfprotov6.FunctionParameter{
			Name:               proto.VariadicParameter.Name,
			Type:               nil, // Would need type conversion
			AllowNullValue:     proto.VariadicParameter.AllowNullValue,
			AllowUnknownValues: proto.VariadicParameter.AllowUnknownValues,
			Description:        proto.VariadicParameter.Description,
			DescriptionKind:    tfprotov6.StringKind(proto.VariadicParameter.DescriptionKind),
		}
	}

	// Return would also have a type that needs conversion

	return fn
}
