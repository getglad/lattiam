package interfaces

import (
	"time"
)

// InterpolationResolver defines the interface for resolving variable interpolations
type InterpolationResolver interface {
	// Core interpolation
	ResolveInterpolations(properties map[string]interface{}, deployedResources map[string]map[string]interface{}) (map[string]interface{}, error)
	ResolveInterpolationsStrict(properties map[string]interface{}, deployedResources map[string]map[string]interface{}) (map[string]interface{}, error)

	// Validation and analysis
	ValidateInterpolations(properties map[string]interface{}, deployedResources map[string]map[string]interface{}) []string
	HasInterpolations(properties map[string]interface{}) bool
	ExtractReferences(properties map[string]interface{}) []string

	// Function support
	RegisterFunction(name string, fn InterpolationFunction) error
	UnregisterFunction(name string) error
	ListFunctions() []string

	// Debugging
	ExplainInterpolation(expression string) (InterpolationExplanation, error)
	GetInterpolationContext(deployedResources map[string]map[string]interface{}) map[string]interface{}
}

// InterpolationFunction represents a custom function that can be used in interpolations
type InterpolationFunction func(args []interface{}) (interface{}, error)

// InterpolationExplanation provides detailed information about an interpolation expression
type InterpolationExplanation struct {
	Expression string
	References []string
	Functions  []string
	IsValid    bool
	Error      error
	Result     interface{}
}

// InterpolationCall represents a call to the InterpolationResolver for tracking in mocks
type InterpolationCall struct {
	Method     string
	Expression string
	Timestamp  time.Time
	Error      error
}
