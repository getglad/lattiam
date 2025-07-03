package deployment

import (
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/lattiam/lattiam/internal/expressions"
)

// interpolationPattern matches ${...} patterns
var interpolationPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

// ResolveInterpolations resolves Terraform-style interpolations in properties
// by using the expression evaluator, which supports both functions and variables.
func ResolveInterpolations(properties map[string]interface{}, deployedResources map[string]map[string]interface{}) (map[string]interface{}, error) {
	log.Printf("Starting interpolation resolution with %d properties and %d deployed resources", len(properties), len(deployedResources))

	// The expression evaluator will handle both functions (uuid(), etc.) and variables (resource references).
	evaluator := expressions.NewEvaluator()

	// The variables for the evaluator will be the deployed resources.
	// We need to structure the variables correctly for the HCL evaluator.
	// e.g., "aws_s3_bucket.test" becomes a nested map: { "aws_s3_bucket": { "test": { ... } } }
	vars := make(map[string]interface{})
	for key, value := range deployedResources {
		parts := strings.Split(strings.ReplaceAll(key, "/", "."), ".")
		if len(parts) < 2 {
			continue
		}

		current := vars
		for i, part := range parts {
			if i == len(parts)-1 {
				current[part] = value
			} else {
				if _, ok := current[part]; !ok {
					current[part] = make(map[string]interface{})
				}
				current = current[part].(map[string]interface{})
			}
		}
	}

	resolved := make(map[string]interface{})
	for key, value := range properties {
		var err error
		resolved[key], err = resolveValue(value, evaluator, vars)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve value for key '%s': %w", key, err)
		}
	}

	return resolved, nil
}

// ResolveInterpolationsStrict resolves interpolations but returns an error if any are unresolvable.
// This now relies on the evaluator to handle strictness.
func ResolveInterpolationsStrict(properties map[string]interface{}, deployedResources map[string]map[string]interface{}) (map[string]interface{}, error) {
	// The evaluator's `EvaluateWithVariables` will return an error for unresolved interpolations.
	// We can leverage this for our strict check.
	resolved, err := ResolveInterpolations(properties, deployedResources)
	if err != nil {
		return nil, err
	}

	// To validate, we can check if any unresolved `${...}` patterns remain.
	// A more robust check would be to have the evaluator return the list of unresolved vars.
	// For now, we'll rely on the fact that a failed evaluation will likely leave the original string.
	unresolved := ValidateInterpolations(resolved, deployedResources)
	if len(unresolved) > 0 {
		return nil, fmt.Errorf("unresolved interpolations found: %v", unresolved)
	}

	return resolved, nil
}

// resolveValue recursively resolves interpolations in a value using the expression evaluator.
func resolveValue(value interface{}, evaluator *expressions.Evaluator, variables map[string]interface{}) (interface{}, error) {
	switch v := value.(type) {
	case string:
		// The evaluator handles strings containing interpolations.
		resolved, err := evaluator.EvaluateWithVariables(v, variables)
		if err != nil {
			// If evaluation fails, return the error.
			return nil, fmt.Errorf("failed to evaluate expression '%s': %w", v, err)
		}
		return resolved, nil
	case map[string]interface{}:
		resolvedMap := make(map[string]interface{})
		for k, val := range v {
			var err error
			resolvedMap[k], err = resolveValue(val, evaluator, variables)
			if err != nil {
				return nil, err
			}
		}
		return resolvedMap, nil
	case []interface{}:
		resolvedSlice := make([]interface{}, len(v))
		for i, val := range v {
			var err error
			resolvedSlice[i], err = resolveValue(val, evaluator, variables)
			if err != nil {
				return nil, err
			}
		}
		return resolvedSlice, nil
	default:
		// For other types (int, bool, etc.), return as-is.
		return value, nil
	}
}

// ValidateInterpolations checks if all interpolations in properties can be resolved.
// Returns a list of unresolvable references.
func ValidateInterpolations(properties map[string]interface{}, deployedResources map[string]map[string]interface{}) []string {
	var unresolvable []string
	validateValue(properties, deployedResources, &unresolvable)
	return unresolvable
}

// validateValue recursively validates interpolations in a value.
func validateValue(value interface{}, deployedResources map[string]map[string]interface{}, unresolvable *[]string) {
	switch v := value.(type) {
	case string:
		validateStringValue(v, deployedResources, unresolvable)
	case map[string]interface{}:
		for _, val := range v {
			validateValue(val, deployedResources, unresolvable)
		}
	case []interface{}:
		for _, val := range v {
			validateValue(val, deployedResources, unresolvable)
		}
	}
}

// validateStringValue validates interpolations in a string value
func validateStringValue(v string, deployedResources map[string]map[string]interface{}, unresolvable *[]string) {
	matches := interpolationPattern.FindAllStringSubmatch(v, -1)
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		validateReference(match[1], deployedResources, unresolvable)
	}
}

// validateReference validates a single reference
func validateReference(reference string, deployedResources map[string]map[string]interface{}, unresolvable *[]string) {
	// Skip function calls, as we can't validate them without full evaluation.
	if strings.Contains(reference, "(") {
		return
	}

	resourceKey := extractResourceKey(reference)
	if resourceKey == "" {
		*unresolvable = append(*unresolvable, reference)
		return
	}

	if _, exists := deployedResources[resourceKey]; !exists {
		*unresolvable = append(*unresolvable, reference)
	}
}

// extractResourceKey extracts the resource key from a reference
func extractResourceKey(reference string) string {
	parts := strings.Split(reference, ".")
	const dataPrefix = "data"
	switch {
	case parts[0] == dataPrefix && len(parts) >= 3:
		return fmt.Sprintf("data.%s.%s", parts[1], parts[2])
	case len(parts) >= 2:
		return fmt.Sprintf("%s/%s", parts[0], parts[1])
	default:
		return ""
	}
}
