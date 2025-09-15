package interpolation

import (
	"fmt"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/function"

	"github.com/lattiam/lattiam/internal/interfaces"
)

// HCLInterpolationResolver implements InterpolationResolver using HCL and cty
type HCLInterpolationResolver struct {
	functions  map[string]function.Function
	strictMode bool
}

// NewHCLInterpolationResolver creates a new HCL-based interpolation resolver
func NewHCLInterpolationResolver(strictMode bool) *HCLInterpolationResolver {
	resolver := &HCLInterpolationResolver{
		functions:  make(map[string]function.Function),
		strictMode: strictMode,
	}

	// Register core functions
	resolver.registerCoreFunctions()

	return resolver
}

// ResolveInterpolations performs HCL-based variable substitution
func (r *HCLInterpolationResolver) ResolveInterpolations(properties map[string]interface{}, deployedResources map[string]map[string]interface{}) (map[string]interface{}, error) {
	// Convert deployedResources to cty values for evaluation context
	evalCtx, err := r.buildEvaluationContext(deployedResources)
	if err != nil {
		return nil, fmt.Errorf("failed to build evaluation context: %w", err)
	}

	// Process each property
	result := make(map[string]interface{})
	for key, value := range properties {
		resolved, err := r.resolveValue(value, evalCtx)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve property %s: %w", key, err)
		}
		result[key] = resolved
	}

	return result, nil
}

// resolveValue resolves a single value using HCL expression evaluation
func (r *HCLInterpolationResolver) resolveValue(value interface{}, evalCtx *hcl.EvalContext) (interface{}, error) {
	switch v := value.(type) {
	case string:
		return r.resolveStringValue(v, evalCtx)
	case map[string]interface{}:
		return r.resolveMapValue(v, evalCtx)
	case []interface{}:
		return r.resolveSliceValue(v, evalCtx)
	default:
		// Return other types as-is
		return value, nil
	}
}

// resolveStringValue resolves string values that may contain interpolations
func (r *HCLInterpolationResolver) resolveStringValue(v string, evalCtx *hcl.EvalContext) (interface{}, error) {
	// Check if it's a simple interpolation expression first
	if strings.HasPrefix(v, "${") && strings.HasSuffix(v, "}") {
		return r.resolveSimpleInterpolation(v, evalCtx)
	} else if strings.Contains(v, "${") {
		// Handle strings with embedded interpolations
		return r.resolveStringWithInterpolations(v, evalCtx)
	}
	return v, nil
}

// resolveSimpleInterpolation handles simple ${...} expressions
func (r *HCLInterpolationResolver) resolveSimpleInterpolation(v string, evalCtx *hcl.EvalContext) (interface{}, error) {
	// Extract the expression without ${...}
	exprStr := v[2 : len(v)-1]

	// Parse as HCL expression
	expr, diags := hclsyntax.ParseExpression([]byte(exprStr), "", hcl.Pos{})
	if diags.HasErrors() {
		if r.strictMode {
			return nil, fmt.Errorf("expression parse error: %s", diags.Error())
		}
		return v, nil // Return original value in non-strict mode
	}

	// Evaluate expression
	result, diags := expr.Value(evalCtx)
	if diags.HasErrors() {
		if r.strictMode {
			return nil, fmt.Errorf("expression evaluation error: %s", diags.Error())
		}
		return v, nil // Return original value in non-strict mode
	}

	// Convert cty.Value back to Go value
	return ctyToGoValue(result)
}

// resolveMapValue recursively resolves nested maps
func (r *HCLInterpolationResolver) resolveMapValue(v map[string]interface{}, evalCtx *hcl.EvalContext) (interface{}, error) {
	resolved := make(map[string]interface{})
	for k, val := range v {
		res, err := r.resolveValue(val, evalCtx)
		if err != nil {
			return nil, err
		}
		resolved[k] = res
	}
	return resolved, nil
}

// resolveSliceValue recursively resolves arrays
func (r *HCLInterpolationResolver) resolveSliceValue(v []interface{}, evalCtx *hcl.EvalContext) (interface{}, error) {
	resolved := make([]interface{}, len(v))
	for i, val := range v {
		res, err := r.resolveValue(val, evalCtx)
		if err != nil {
			return nil, err
		}
		resolved[i] = res
	}
	return resolved, nil
}

// buildEvaluationContext creates an HCL evaluation context from deployed resources
func (r *HCLInterpolationResolver) buildEvaluationContext(deployedResources map[string]map[string]interface{}) (*hcl.EvalContext, error) {
	variables := make(map[string]cty.Value)

	// Convert deployed resources to cty values
	for namespace, resources := range deployedResources {
		resourceValues := make(map[string]cty.Value)
		for name, data := range resources {
			val, err := goValueToCty(data)
			if err != nil {
				return nil, fmt.Errorf("failed to convert resource %s.%s to cty: %w", namespace, name, err)
			}
			resourceValues[name] = val
		}
		variables[namespace] = cty.ObjectVal(resourceValues)
	}

	return &hcl.EvalContext{
		Variables: variables,
		Functions: r.functions,
	}, nil
}

// RegisterFunction registers a custom interpolation function
func (r *HCLInterpolationResolver) RegisterFunction(name string, fn interfaces.InterpolationFunction) error {
	// Convert our function interface to cty function
	ctyFunc := function.New(&function.Spec{
		Params: []function.Parameter{
			{
				Name: "args",
				Type: cty.DynamicPseudoType,
			},
		},
		VarParam: &function.Parameter{
			Name: "varargs",
			Type: cty.DynamicPseudoType,
		},
		Type: function.StaticReturnType(cty.DynamicPseudoType),
		Impl: func(args []cty.Value, _ cty.Type) (cty.Value, error) {
			// Convert cty values to Go values for our function interface
			goArgs := make([]interface{}, len(args))
			for i, arg := range args {
				val, err := ctyToGoValue(arg)
				if err != nil {
					return cty.NilVal, err
				}
				goArgs[i] = val
			}

			// Call our function
			result, err := fn(goArgs)
			if err != nil {
				return cty.NilVal, err
			}

			// Convert result back to cty
			return goValueToCty(result)
		},
	})

	r.functions[name] = ctyFunc
	return nil
}

// UnregisterFunction removes a custom interpolation function
func (r *HCLInterpolationResolver) UnregisterFunction(name string) error {
	delete(r.functions, name)
	return nil
}

// ListFunctions lists all registered functions
func (r *HCLInterpolationResolver) ListFunctions() []string {
	functions := make([]string, 0, len(r.functions))
	for name := range r.functions {
		functions = append(functions, name)
	}
	return functions
}

// ResolveInterpolationsStrict performs strict variable substitution
func (r *HCLInterpolationResolver) ResolveInterpolationsStrict(properties map[string]interface{}, deployedResources map[string]map[string]interface{}) (map[string]interface{}, error) {
	// Use current resolver's strict mode setting
	oldStrictMode := r.strictMode
	r.strictMode = true
	defer func() { r.strictMode = oldStrictMode }()
	return r.ResolveInterpolations(properties, deployedResources)
}

// ValidateInterpolations validates interpolations and returns warnings
func (r *HCLInterpolationResolver) ValidateInterpolations(properties map[string]interface{}, deployedResources map[string]map[string]interface{}) []string {
	var warnings []string

	evalCtx, err := r.buildEvaluationContext(deployedResources)
	if err != nil {
		warnings = append(warnings, fmt.Sprintf("Failed to build evaluation context: %v", err))
		return warnings
	}

	// Validate each property
	for key, value := range properties {
		if err := r.validateValue(value, evalCtx); err != nil {
			warnings = append(warnings, fmt.Sprintf("Property %s: %v", key, err))
		}
	}

	return warnings
}

// validateValue validates a single value for interpolation issues
func (r *HCLInterpolationResolver) validateValue(value interface{}, evalCtx *hcl.EvalContext) error {
	switch v := value.(type) {
	case string:
		if strings.HasPrefix(v, "${") && strings.HasSuffix(v, "}") {
			expr, diags := hclsyntax.ParseExpression([]byte(v), "", hcl.Pos{})
			if diags.HasErrors() {
				return fmt.Errorf("parse error: %s", diags.Error())
			}

			_, diags = expr.Value(evalCtx)
			if diags.HasErrors() {
				return fmt.Errorf("evaluation error: %s", diags.Error())
			}
		}

	case map[string]interface{}:
		for _, val := range v {
			if err := r.validateValue(val, evalCtx); err != nil {
				return err
			}
		}

	case []interface{}:
		for _, val := range v {
			if err := r.validateValue(val, evalCtx); err != nil {
				return err
			}
		}
	}

	return nil
}

// HasInterpolations checks if properties contain interpolations
func (r *HCLInterpolationResolver) HasInterpolations(properties map[string]interface{}) bool {
	return r.hasInterpolationsInValue(properties)
}

// hasInterpolationsInValue recursively checks for interpolations
func (r *HCLInterpolationResolver) hasInterpolationsInValue(value interface{}) bool {
	switch v := value.(type) {
	case string:
		return strings.Contains(v, "${") && strings.Contains(v, "}")

	case map[string]interface{}:
		for _, val := range v {
			if r.hasInterpolationsInValue(val) {
				return true
			}
		}

	case []interface{}:
		for _, val := range v {
			if r.hasInterpolationsInValue(val) {
				return true
			}
		}
	}

	return false
}

// ExtractReferences extracts all references from properties
func (r *HCLInterpolationResolver) ExtractReferences(properties map[string]interface{}) []string {
	var references []string
	r.extractReferencesFromValue(properties, &references)
	return references
}

// extractReferencesFromValue recursively extracts references
func (r *HCLInterpolationResolver) extractReferencesFromValue(value interface{}, references *[]string) {
	switch v := value.(type) {
	case string:
		if strings.HasPrefix(v, "${") && strings.HasSuffix(v, "}") {
			expr, diags := hclsyntax.ParseExpression([]byte(v), "", hcl.Pos{})
			if !diags.HasErrors() {
				// Extract variable references from the expression
				for _, traversal := range expr.Variables() {
					*references = append(*references, traversalToString(traversal))
				}
			}
		}

	case map[string]interface{}:
		for _, val := range v {
			r.extractReferencesFromValue(val, references)
		}

	case []interface{}:
		for _, val := range v {
			r.extractReferencesFromValue(val, references)
		}
	}
}

// ExplainInterpolation provides explanation for an interpolation expression
func (r *HCLInterpolationResolver) ExplainInterpolation(expression string) (interfaces.InterpolationExplanation, error) {
	explanation := interfaces.InterpolationExplanation{
		Expression: expression,
		IsValid:    true,
	}

	if !strings.HasPrefix(expression, "${") || !strings.HasSuffix(expression, "}") {
		explanation.IsValid = false
		return explanation, fmt.Errorf("not a valid interpolation expression")
	}

	expr, diags := hclsyntax.ParseExpression([]byte(expression), "", hcl.Pos{})
	if diags.HasErrors() {
		explanation.IsValid = false
		return explanation, fmt.Errorf("parse error: %s", diags.Error())
	}

	// Extract references
	for _, traversal := range expr.Variables() {
		explanation.References = append(explanation.References, traversalToString(traversal))
	}

	// Extract function calls (this is more complex, would need expression visitor)
	explanation.Functions = r.extractFunctionCalls(expr)

	return explanation, nil
}

// GetInterpolationContext builds context for interpolations
func (r *HCLInterpolationResolver) GetInterpolationContext(deployedResources map[string]map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for namespace, resources := range deployedResources {
		result[namespace] = resources
	}
	return result
}

// resolveStringWithInterpolations handles strings that contain embedded interpolations
// For example: "demo-${random_string.bucket_suffix.result}"
func (r *HCLInterpolationResolver) resolveStringWithInterpolations(str string, evalCtx *hcl.EvalContext) (interface{}, error) {
	// Parse as HCL template expression
	expr, diags := hclsyntax.ParseTemplate([]byte(str), "", hcl.Pos{})
	if diags.HasErrors() {
		if r.strictMode {
			return nil, fmt.Errorf("template parse error: %s", diags.Error())
		}
		return str, nil // Return original value in non-strict mode
	}

	// Evaluate the template
	result, diags := expr.Value(evalCtx)
	if diags.HasErrors() {
		if r.strictMode {
			return nil, fmt.Errorf("template evaluation error: %s", diags.Error())
		}
		return str, nil // Return original value in non-strict mode
	}

	// Convert cty.Value back to Go value
	return ctyToGoValue(result)
}
