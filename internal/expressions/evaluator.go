package expressions

import (
	"fmt"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"
	"github.com/zclconf/go-cty/cty/function/stdlib"
)

// Evaluator evaluates Terraform expressions including functions and interpolations
type Evaluator struct {
	functions map[string]function.Function
}

// NewEvaluator creates a new expression evaluator with Terraform-compatible functions
func NewEvaluator() *Evaluator {
	return &Evaluator{
		functions: GetCoreFunctions(),
	}
}

// EvaluateExpression evaluates a string that may contain interpolations and function calls
func (e *Evaluator) EvaluateExpression(exprStr string) (string, error) {
	// If no interpolation pattern, return as-is
	if !strings.Contains(exprStr, "${") {
		return exprStr, nil
	}

	// Find all interpolations with proper brace matching
	result := exprStr
	start := 0
	for {
		startIdx := strings.Index(exprStr[start:], "${")
		if startIdx == -1 {
			break
		}
		startIdx += start

		// Find matching closing brace, handling nested braces
		endIdx := e.findMatchingBrace(exprStr, startIdx+2)
		if endIdx == -1 {
			break
		}

		// Extract the expression content
		fullMatch := exprStr[startIdx : endIdx+1]
		exprContent := exprStr[startIdx+2 : endIdx]

		// Parse and evaluate the expression
		evaluated, err := e.evaluateSingleExpression(exprContent)
		if err != nil {
			// Skip warning but continue with other expressions
			start = endIdx + 1
			continue
		}

		// Replace the expression with the result
		result = strings.Replace(result, fullMatch, evaluated, 1)
		start = endIdx + 1
	}

	return result, nil
}

// findMatchingBrace finds the closing brace that matches the opening brace,
// handling nested braces correctly
func (e *Evaluator) findMatchingBrace(str string, start int) int {
	braceCount := 1
	for i := start; i < len(str); i++ {
		switch str[i] {
		case '{':
			braceCount++
		case '}':
			braceCount--
			if braceCount == 0 {
				return i
			}
		}
	}
	return -1 // No matching brace found
}

// evaluateSingleExpression evaluates a single expression (without the ${ } wrapper)
func (e *Evaluator) evaluateSingleExpression(exprContent string) (string, error) {
	// Parse as HCL expression
	expr, diags := hclsyntax.ParseExpression([]byte(exprContent), "expression", hcl.Pos{Line: 1, Column: 1})
	if diags.HasErrors() {
		return "", fmt.Errorf("failed to parse expression: %s", diags.Error())
	}

	// Create evaluation context
	ctx := &hcl.EvalContext{
		Functions: e.functions,
		Variables: make(map[string]cty.Value),
	}

	// Evaluate the expression
	val, evalDiags := expr.Value(ctx)
	if evalDiags.HasErrors() {
		return "", fmt.Errorf("failed to evaluate expression: %s", evalDiags.Error())
	}

	// Convert result to string
	switch val.Type() {
	case cty.String:
		return val.AsString(), nil
	case cty.Number:
		return val.AsBigFloat().String(), nil
	case cty.Bool:
		if val.True() {
			return "true", nil
		}
		return "false", nil
	default:
		// For complex types, encode as JSON
		jsonBytes, err := stdlib.JSONEncodeFunc.Call([]cty.Value{val})
		if err != nil {
			return "", fmt.Errorf("failed to encode result: %w", err)
		}
		return jsonBytes.AsString(), nil
	}
}

// EvaluateWithVariables evaluates an expression with variables available
//
//nolint:gocognit // expression evaluation logic
func (e *Evaluator) EvaluateWithVariables(exprStr string, variables map[string]interface{}) (string, error) {
	// Convert variables to cty values
	ctyVars := make(map[string]cty.Value)
	for k, v := range variables {
		ctyVal, err := convertToCty(v)
		if err != nil {
			return "", fmt.Errorf("failed to convert variable %s: %w", k, err)
		}
		ctyVars[k] = ctyVal
	}

	// If no interpolation pattern, return as-is
	if !strings.Contains(exprStr, "${") {
		return exprStr, nil
	}

	// Find all interpolations and evaluate with variables
	result := exprStr
	start := 0
	for {
		startIdx := strings.Index(exprStr[start:], "${")
		if startIdx == -1 {
			break
		}
		startIdx += start

		endIdx := e.findMatchingBrace(exprStr, startIdx+2)
		if endIdx == -1 {
			break
		}

		// Extract the expression content
		fullMatch := exprStr[startIdx : endIdx+1]
		exprContent := exprStr[startIdx+2 : endIdx]

		// Parse as HCL expression
		expr, diags := hclsyntax.ParseExpression([]byte(exprContent), "expression", hcl.Pos{Line: 1, Column: 1})
		if diags.HasErrors() {
			// TODO: Add logger support for warnings
			// fmt.Printf("Warning: Failed to parse expression %s: %s\n", exprContent, diags.Error())
			start = endIdx + 1
			continue
		}

		// Create evaluation context with variables
		ctx := &hcl.EvalContext{
			Functions: e.functions,
			Variables: ctyVars,
		}

		// Evaluate the expression
		val, evalDiags := expr.Value(ctx)
		if evalDiags.HasErrors() {
			// TODO: Add logger support for warnings
			// fmt.Printf("Warning: Failed to evaluate expression %s: %s\n", exprContent, evalDiags.Error())
			start = endIdx + 1
			continue
		}

		// Convert result to string
		var evaluated string
		switch val.Type() {
		case cty.String:
			evaluated = val.AsString()
		case cty.Number:
			evaluated = val.AsBigFloat().String()
		case cty.Bool:
			if val.True() {
				evaluated = "true"
			} else {
				evaluated = "false"
			}
		default:
			// For complex types, encode as JSON
			jsonBytes, err := stdlib.JSONEncodeFunc.Call([]cty.Value{val})
			if err != nil {
				// TODO: Add logger support for warnings
				// fmt.Printf("Warning: Failed to encode result for %s: %v\n", exprContent, err)
				start = endIdx + 1
				continue
			}
			evaluated = jsonBytes.AsString()
		}

		// Replace the expression with the result
		result = strings.Replace(result, fullMatch, evaluated, 1)
		start = endIdx + 1
	}

	return result, nil
}

// convertToCty converts a Go value to a cty value
func convertToCty(val interface{}) (cty.Value, error) {
	switch v := val.(type) {
	case string:
		return cty.StringVal(v), nil
	case int:
		return cty.NumberIntVal(int64(v)), nil
	case int64:
		return cty.NumberIntVal(v), nil
	case float64:
		return cty.NumberFloatVal(v), nil
	case bool:
		return cty.BoolVal(v), nil
	case nil:
		return cty.NullVal(cty.DynamicPseudoType), nil
	case map[string]interface{}:
		vals := make(map[string]cty.Value)
		for k, innerVal := range v {
			ctyVal, err := convertToCty(innerVal)
			if err != nil {
				return cty.NilVal, err
			}
			vals[k] = ctyVal
		}
		if len(vals) == 0 {
			return cty.ObjectVal(make(map[string]cty.Value)), nil
		}
		return cty.ObjectVal(vals), nil
	case []interface{}:
		vals := make([]cty.Value, len(v))
		for i, elem := range v {
			ctyVal, err := convertToCty(elem)
			if err != nil {
				return cty.NilVal, err
			}
			vals[i] = ctyVal
		}
		if len(vals) == 0 {
			return cty.ListValEmpty(cty.DynamicPseudoType), nil
		}
		return cty.ListVal(vals), nil
	default:
		return cty.NilVal, fmt.Errorf("unsupported type: %T", val)
	}
}
