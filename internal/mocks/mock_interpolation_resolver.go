package mocks

import (
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/lattiam/lattiam/internal/interfaces"
)

// MockInterpolationResolver implements interfaces.InterpolationResolver for testing
type MockInterpolationResolver struct {
	mu         sync.RWMutex
	shouldFail bool
	functions  map[string]func() interface{}
}

// NewMockInterpolationResolver creates a new mock interpolation resolver
func NewMockInterpolationResolver() *MockInterpolationResolver {
	resolver := &MockInterpolationResolver{
		functions: make(map[string]func() interface{}),
	}

	// Add default mock functions
	resolver.functions["uuid"] = func() interface{} {
		return "mock-uuid-12345"
	}
	resolver.functions["timestamp"] = func() interface{} {
		return "2023-01-01T00:00:00Z"
	}

	return resolver
}

// SetShouldFail configures the mock to fail operations
func (m *MockInterpolationResolver) SetShouldFail(shouldFail bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.shouldFail = shouldFail
}

// AddFunction adds a mock function
func (m *MockInterpolationResolver) AddFunction(name string, fn func() interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.functions[name] = fn
}

// ResolveInterpolations resolves Terraform-style interpolations in properties
func (m *MockInterpolationResolver) ResolveInterpolations(properties map[string]interface{}, deployedResources map[string]map[string]interface{}) (map[string]interface{}, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.shouldFail {
		return nil, fmt.Errorf("mock interpolation resolver failure")
	}

	resolved := make(map[string]interface{})
	for key, value := range properties {
		resolvedValue, err := m.resolveValue(value, deployedResources)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve value for key '%s': %w", key, err)
		}
		resolved[key] = resolvedValue
	}

	return resolved, nil
}

// ResolveInterpolationsStrict resolves interpolations but returns an error if any are unresolvable
func (m *MockInterpolationResolver) ResolveInterpolationsStrict(properties map[string]interface{}, deployedResources map[string]map[string]interface{}) (map[string]interface{}, error) {
	// For mock purposes, this behaves the same as ResolveInterpolations
	return m.ResolveInterpolations(properties, deployedResources)
}

// ValidateInterpolations checks if all interpolations in properties can be resolved
func (m *MockInterpolationResolver) ValidateInterpolations(properties map[string]interface{}, deployedResources map[string]map[string]interface{}) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var unresolvable []string
	m.validateValue(properties, deployedResources, &unresolvable)
	return unresolvable
}

// HasInterpolations checks if properties contain any interpolations
func (m *MockInterpolationResolver) HasInterpolations(properties map[string]interface{}) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return m.hasInterpolationsInValue(properties)
}

// ExtractReferences extracts all resource references from properties
func (m *MockInterpolationResolver) ExtractReferences(properties map[string]interface{}) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var references []string
	m.extractReferencesFromValue(properties, &references)
	return references
}

// RegisterFunction registers a custom function
func (m *MockInterpolationResolver) RegisterFunction(name string, fn interfaces.InterpolationFunction) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.shouldFail {
		return fmt.Errorf("mock register function failure")
	}

	// Convert to our internal function type
	m.functions[name] = func() interface{} {
		result, _ := fn([]interface{}{})
		return result
	}

	return nil
}

// UnregisterFunction unregisters a function
func (m *MockInterpolationResolver) UnregisterFunction(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.shouldFail {
		return fmt.Errorf("mock unregister function failure")
	}

	delete(m.functions, name)
	return nil
}

// ListFunctions lists all registered functions
func (m *MockInterpolationResolver) ListFunctions() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	functions := make([]string, 0, len(m.functions))
	for name := range m.functions {
		functions = append(functions, name)
	}
	return functions
}

// ExplainInterpolation provides detailed information about an interpolation expression
func (m *MockInterpolationResolver) ExplainInterpolation(expression string) (interfaces.InterpolationExplanation, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.shouldFail {
		return interfaces.InterpolationExplanation{}, fmt.Errorf("mock explain interpolation failure")
	}

	// Mock implementation
	var references []string
	var functions []string

	if strings.Contains(expression, "(") {
		// Contains function call
		parenIdx := strings.Index(expression, "(")
		funcName := strings.TrimSpace(expression[:parenIdx])
		functions = append(functions, funcName)
	} else {
		// Resource reference
		references = append(references, expression)
	}

	return interfaces.InterpolationExplanation{
		Expression: expression,
		References: references,
		Functions:  functions,
		IsValid:    true,
		Error:      nil,
		Result:     "mock-result",
	}, nil
}

// GetInterpolationContext gets the context for interpolation evaluation
func (m *MockInterpolationResolver) GetInterpolationContext(deployedResources map[string]map[string]interface{}) map[string]interface{} {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Convert deployed resources to context format
	context := make(map[string]interface{})
	for key, resource := range deployedResources {
		context[key] = resource
	}

	return context
}

// resolveValue recursively resolves interpolations in a value
func (m *MockInterpolationResolver) resolveValue(value interface{}, deployedResources map[string]map[string]interface{}) (interface{}, error) {
	switch v := value.(type) {
	case string:
		return m.resolveString(v, deployedResources)
	case map[string]interface{}:
		resolvedMap := make(map[string]interface{})
		for k, val := range v {
			resolvedValue, err := m.resolveValue(val, deployedResources)
			if err != nil {
				return nil, err
			}
			resolvedMap[k] = resolvedValue
		}
		return resolvedMap, nil
	case []interface{}:
		resolvedSlice := make([]interface{}, len(v))
		for i, val := range v {
			resolvedValue, err := m.resolveValue(val, deployedResources)
			if err != nil {
				return nil, err
			}
			resolvedSlice[i] = resolvedValue
		}
		return resolvedSlice, nil
	default:
		return value, nil
	}
}

// resolveString resolves interpolations in a string value
func (m *MockInterpolationResolver) resolveString(input string, deployedResources map[string]map[string]interface{}) (interface{}, error) {
	// Pattern to match ${...} interpolations
	pattern := regexp.MustCompile(`\$\{([^}]+)\}`)

	result := input
	matches := pattern.FindAllStringSubmatch(input, -1)

	for _, match := range matches {
		if len(match) < 2 {
			continue
		}

		expression := match[1]
		placeholder := match[0]

		resolved, err := m.resolveExpression(expression, deployedResources)
		if err != nil {
			return nil, err
		}

		// If the entire string is just this interpolation, return the resolved value directly
		if input == placeholder {
			return resolved, nil
		}

		// Otherwise, replace in the string
		resolvedStr := fmt.Sprintf("%v", resolved)
		result = strings.ReplaceAll(result, placeholder, resolvedStr)
	}

	return result, nil
}

// resolveExpression resolves a single expression
func (m *MockInterpolationResolver) resolveExpression(expression string, deployedResources map[string]map[string]interface{}) (interface{}, error) {
	// Handle function calls
	if strings.Contains(expression, "(") {
		return m.resolveFunction(expression)
	}

	// Handle resource references
	return m.resolveResourceReference(expression, deployedResources)
}

// resolveFunction resolves function calls like uuid() or timestamp()
func (m *MockInterpolationResolver) resolveFunction(expression string) (interface{}, error) {
	// Extract function name (simple implementation)
	parenIdx := strings.Index(expression, "(")
	if parenIdx == -1 {
		return nil, fmt.Errorf("invalid function expression: %s", expression)
	}

	funcName := strings.TrimSpace(expression[:parenIdx])

	if fn, exists := m.functions[funcName]; exists {
		return fn(), nil
	}

	return nil, fmt.Errorf("unknown function: %s", funcName)
}

// resolveResourceReference resolves resource references like aws_s3_bucket.test.id
func (m *MockInterpolationResolver) resolveResourceReference(expression string, deployedResources map[string]map[string]interface{}) (interface{}, error) {
	parts := strings.Split(expression, ".")

	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid resource reference: %s", expression)
	}

	// Build resource key
	var resourceKey string
	var attributePath []string

	if parts[0] == "data" && len(parts) >= 3 {
		// Data source reference: data.type.name.attribute
		resourceKey = fmt.Sprintf("data.%s.%s", parts[1], parts[2])
		attributePath = parts[3:]
	} else if len(parts) >= 2 {
		// Resource reference: type.name.attribute
		resourceKey = fmt.Sprintf("%s.%s", parts[0], parts[1])
		attributePath = parts[2:]
	}

	// Look up resource using nested format
	// deployedResources is map[string]map[string]interface{} where:
	// - First level key is resource type (e.g., "random_string")
	// - Second level key is resource name (e.g., "bucket_suffix")
	// - Value is the resource state

	var resource interface{}
	var exists bool

	// Split the resource key to get type and name
	keyParts := strings.Split(resourceKey, ".")
	if len(keyParts) >= 2 {
		resourceType := keyParts[0]
		resourceName := keyParts[1]

		// Check if we have this resource type
		if resourceTypeMap, hasType := deployedResources[resourceType]; hasType {
			// Check if we have this specific resource
			if resourceState, hasResource := resourceTypeMap[resourceName]; hasResource {
				resource = resourceState
				exists = true
			}
		}
	}

	if !exists {
		// Return a mock value for missing resources
		return fmt.Sprintf("mock-%s", strings.ReplaceAll(resourceKey, ".", "-")), nil
	}

	// Navigate to the specific attribute
	current := resource
	for _, attr := range attributePath {
		if currentMap, ok := current.(map[string]interface{}); ok {
			if value, exists := currentMap[attr]; exists {
				current = value
			} else {
				// Return a mock value for missing attributes
				return fmt.Sprintf("mock-%s-%s", strings.ReplaceAll(resourceKey, ".", "-"), attr), nil
			}
		} else {
			return nil, fmt.Errorf("cannot access attribute %s on non-map value", attr)
		}
	}

	return current, nil
}

// validateValue recursively validates interpolations in a value
func (m *MockInterpolationResolver) validateValue(value interface{}, deployedResources map[string]map[string]interface{}, unresolvable *[]string) {
	switch v := value.(type) {
	case string:
		m.validateString(v, deployedResources, unresolvable)
	case map[string]interface{}:
		for _, val := range v {
			m.validateValue(val, deployedResources, unresolvable)
		}
	case []interface{}:
		for _, val := range v {
			m.validateValue(val, deployedResources, unresolvable)
		}
	}
}

// validateString validates interpolations in a string value
func (m *MockInterpolationResolver) validateString(input string, deployedResources map[string]map[string]interface{}, unresolvable *[]string) {
	pattern := regexp.MustCompile(`\$\{([^}]+)\}`)
	matches := pattern.FindAllStringSubmatch(input, -1)

	for _, match := range matches {
		if len(match) < 2 {
			continue
		}

		expression := match[1]

		// Skip function calls for validation
		if strings.Contains(expression, "(") {
			continue
		}

		// Check if resource reference can be resolved
		parts := strings.Split(expression, ".")
		if len(parts) < 2 {
			*unresolvable = append(*unresolvable, expression)
			continue
		}

		var resourceKey string
		if parts[0] == "data" && len(parts) >= 3 {
			resourceKey = fmt.Sprintf("data.%s.%s", parts[1], parts[2])
		} else {
			resourceKey = fmt.Sprintf("%s.%s", parts[0], parts[1])
		}

		if _, exists := deployedResources[resourceKey]; !exists {
			*unresolvable = append(*unresolvable, expression)
		}
	}
}

// hasInterpolationsInValue checks if a value contains interpolations
func (m *MockInterpolationResolver) hasInterpolationsInValue(value interface{}) bool {
	switch v := value.(type) {
	case string:
		return strings.Contains(v, "${")
	case map[string]interface{}:
		for _, val := range v {
			if m.hasInterpolationsInValue(val) {
				return true
			}
		}
	case []interface{}:
		for _, val := range v {
			if m.hasInterpolationsInValue(val) {
				return true
			}
		}
	}
	return false
}

// extractReferencesFromValue extracts references from a value
func (m *MockInterpolationResolver) extractReferencesFromValue(value interface{}, references *[]string) {
	switch v := value.(type) {
	case string:
		pattern := regexp.MustCompile(`\$\{([^}]+)\}`)
		matches := pattern.FindAllStringSubmatch(v, -1)
		for _, match := range matches {
			if len(match) >= 2 {
				expression := match[1]
				// Skip function calls
				if !strings.Contains(expression, "(") {
					*references = append(*references, expression)
				}
			}
		}
	case map[string]interface{}:
		for _, val := range v {
			m.extractReferencesFromValue(val, references)
		}
	case []interface{}:
		for _, val := range v {
			m.extractReferencesFromValue(val, references)
		}
	}
}
