package interpolation

import (
	"fmt"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
)

// ctyToGoValue converts a cty.Value to a Go value
func ctyToGoValue(val cty.Value) (interface{}, error) {
	if val.IsNull() {
		return nil, nil
	}

	if !val.IsKnown() {
		// Return special marker for unknown values
		return "__unknown__", nil
	}

	ty := val.Type()

	switch {
	case ty == cty.String:
		return val.AsString(), nil
	case ty == cty.Number:
		return ctyNumberToGo(val)
	case ty == cty.Bool:
		return val.True(), nil
	case ty.IsListType() || ty.IsSetType() || ty.IsTupleType():
		return ctyCollectionToGoSlice(val)
	case ty.IsMapType() || ty.IsObjectType():
		return ctyMapToGoMap(val)
	default:
		return nil, fmt.Errorf("unsupported type: %s", ty.FriendlyName())
	}
}

// ctyNumberToGo converts a cty.Number to appropriate Go numeric type
func ctyNumberToGo(val cty.Value) (interface{}, error) {
	if val.AsBigFloat().IsInt() {
		i, _ := val.AsBigFloat().Int64()
		return i, nil
	}
	f, _ := val.AsBigFloat().Float64()
	return f, nil
}

// ctyCollectionToGoSlice converts a cty collection (list/set/tuple) to Go slice
func ctyCollectionToGoSlice(val cty.Value) (interface{}, error) {
	var result []interface{}
	for it := val.ElementIterator(); it.Next(); {
		_, elemVal := it.Element()
		elem, err := ctyToGoValue(elemVal)
		if err != nil {
			return nil, err
		}
		result = append(result, elem)
	}
	return result, nil
}

// ctyMapToGoMap converts a cty map/object to Go map
func ctyMapToGoMap(val cty.Value) (interface{}, error) {
	result := make(map[string]interface{})
	for it := val.ElementIterator(); it.Next(); {
		keyVal, elemVal := it.Element()
		key, err := ctyToGoValue(keyVal)
		if err != nil {
			return nil, err
		}
		keyStr, ok := key.(string)
		if !ok {
			return nil, fmt.Errorf("map key must be string, got %T", key)
		}
		elem, err := ctyToGoValue(elemVal)
		if err != nil {
			return nil, err
		}
		result[keyStr] = elem
	}
	return result, nil
}

// goValueToCty converts a Go value to a cty.Value
func goValueToCty(val interface{}) (cty.Value, error) { //nolint:gocyclo // Complex type conversion logic
	if val == nil {
		return cty.NullVal(cty.DynamicPseudoType), nil
	}

	switch v := val.(type) {
	case string:
		return goStringToCty(v)
	case int:
		return cty.NumberIntVal(int64(v)), nil
	case int32:
		return cty.NumberIntVal(int64(v)), nil
	case int64:
		return cty.NumberIntVal(v), nil
	case uint:
		return cty.NumberUIntVal(uint64(v)), nil
	case uint32:
		return cty.NumberUIntVal(uint64(v)), nil
	case uint64:
		return cty.NumberUIntVal(v), nil
	case float32:
		return cty.NumberFloatVal(float64(v)), nil
	case float64:
		return cty.NumberFloatVal(v), nil
	case bool:
		return cty.BoolVal(v), nil
	case []interface{}:
		return goInterfaceSliceToCty(v)
	case []string:
		return goStringSliceToCty(v)
	case []int:
		return goIntSliceToCty(v)
	case map[string]interface{}:
		return goInterfaceMapToCty(v)
	case map[string]string:
		return goStringMapToCty(v)
	default:
		return cty.NilVal, fmt.Errorf("unsupported Go type: %T", val)
	}
}

// goStringToCty converts a string to cty.Value
func goStringToCty(v string) (cty.Value, error) {
	// Check for unknown marker
	if v == "__unknown__" {
		return cty.UnknownVal(cty.String), nil
	}
	return cty.StringVal(v), nil
}

// goInterfaceSliceToCty converts []interface{} to cty.Value
func goInterfaceSliceToCty(v []interface{}) (cty.Value, error) {
	if len(v) == 0 {
		return cty.ListValEmpty(cty.DynamicPseudoType), nil
	}

	ctyVals := make([]cty.Value, 0, len(v))
	for _, elem := range v {
		ctyVal, err := goValueToCty(elem)
		if err != nil {
			return cty.NilVal, err
		}
		ctyVals = append(ctyVals, ctyVal)
	}
	return cty.ListVal(ctyVals), nil
}

// goStringSliceToCty converts []string to cty.Value
func goStringSliceToCty(v []string) (cty.Value, error) {
	if len(v) == 0 {
		return cty.ListValEmpty(cty.String), nil
	}

	ctyVals := make([]cty.Value, 0, len(v))
	for _, elem := range v {
		ctyVals = append(ctyVals, cty.StringVal(elem))
	}
	return cty.ListVal(ctyVals), nil
}

// goIntSliceToCty converts []int to cty.Value
func goIntSliceToCty(v []int) (cty.Value, error) {
	if len(v) == 0 {
		return cty.ListValEmpty(cty.Number), nil
	}

	ctyVals := make([]cty.Value, 0, len(v))
	for _, elem := range v {
		ctyVals = append(ctyVals, cty.NumberIntVal(int64(elem)))
	}
	return cty.ListVal(ctyVals), nil
}

// goInterfaceMapToCty converts map[string]interface{} to cty.Value
func goInterfaceMapToCty(v map[string]interface{}) (cty.Value, error) {
	if len(v) == 0 {
		return cty.EmptyObjectVal, nil
	}

	ctyVals := make(map[string]cty.Value, len(v))
	for key, elem := range v {
		ctyVal, err := goValueToCty(elem)
		if err != nil {
			return cty.NilVal, err
		}
		ctyVals[key] = ctyVal
	}
	return cty.ObjectVal(ctyVals), nil
}

// goStringMapToCty converts map[string]string to cty.Value
func goStringMapToCty(v map[string]string) (cty.Value, error) {
	if len(v) == 0 {
		return cty.EmptyObjectVal, nil
	}

	ctyVals := make(map[string]cty.Value, len(v))
	for key, elem := range v {
		ctyVals[key] = cty.StringVal(elem)
	}
	return cty.ObjectVal(ctyVals), nil
}

// traversalToString converts an HCL traversal to a string representation
func traversalToString(traversal hcl.Traversal) string {
	var parts []string

	for _, step := range traversal {
		switch s := step.(type) {
		case hcl.TraverseRoot:
			parts = append(parts, s.Name)
		case hcl.TraverseAttr:
			parts = append(parts, s.Name)
		case hcl.TraverseIndex:
			// Convert index to string representation
			switch s.Key.Type() {
			case cty.String:
				parts = append(parts, fmt.Sprintf("[%q]", s.Key.AsString()))
			case cty.Number:
				if s.Key.AsBigFloat().IsInt() {
					i, _ := s.Key.AsBigFloat().Int64()
					parts = append(parts, fmt.Sprintf("[%d]", i))
				} else {
					f, _ := s.Key.AsBigFloat().Float64()
					parts = append(parts, fmt.Sprintf("[%g]", f))
				}
			default:
				parts = append(parts, "[?]")
			}
		case hcl.TraverseSplat:
			parts = append(parts, "*")
		}
	}

	return strings.Join(parts, ".")
}

// extractFunctionCalls extracts function names from an HCL expression
func (r *HCLInterpolationResolver) extractFunctionCalls(expr hcl.Expression) []string {
	var functions []string

	// This is a simplified implementation
	// In practice, you'd need to walk the expression tree to find function calls
	switch e := expr.(type) {
	case *hclsyntax.FunctionCallExpr:
		functions = append(functions, e.Name)

		// Recursively check arguments
		for _, arg := range e.Args {
			functions = append(functions, r.extractFunctionCalls(arg)...)
		}

	case *hclsyntax.ConditionalExpr:
		functions = append(functions, r.extractFunctionCalls(e.Condition)...)
		functions = append(functions, r.extractFunctionCalls(e.TrueResult)...)
		functions = append(functions, r.extractFunctionCalls(e.FalseResult)...)

	case *hclsyntax.BinaryOpExpr:
		functions = append(functions, r.extractFunctionCalls(e.LHS)...)
		functions = append(functions, r.extractFunctionCalls(e.RHS)...)

	case *hclsyntax.UnaryOpExpr:
		functions = append(functions, r.extractFunctionCalls(e.Val)...)

	case *hclsyntax.ParenthesesExpr:
		functions = append(functions, r.extractFunctionCalls(e.Expression)...)

	case *hclsyntax.IndexExpr:
		functions = append(functions, r.extractFunctionCalls(e.Collection)...)
		functions = append(functions, r.extractFunctionCalls(e.Key)...)

	case *hclsyntax.SplatExpr:
		functions = append(functions, r.extractFunctionCalls(e.Source)...)
		if e.Each != nil {
			functions = append(functions, r.extractFunctionCalls(e.Each)...)
		}

	case *hclsyntax.ObjectConsExpr:
		for _, item := range e.Items {
			functions = append(functions, r.extractFunctionCalls(item.KeyExpr)...)
			functions = append(functions, r.extractFunctionCalls(item.ValueExpr)...)
		}

	case *hclsyntax.TupleConsExpr:
		for _, elem := range e.Exprs {
			functions = append(functions, r.extractFunctionCalls(elem)...)
		}
	}

	return functions
}
